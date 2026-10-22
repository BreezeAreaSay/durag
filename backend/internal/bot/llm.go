package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// LLM asks a chat model (OpenRouter, DeepSeek, Qwen or anything speaking the
// OpenAI-compatible /chat/completions API) for a move, in JSON. Every answer
// is validated against the legal moves; the caller falls back to the
// Heuristic on any error.
//
// Models are tried in order: an unknown or unavailable model (404, 5xx) or
// an answer that is not a legal move moves on to the next one. A 429 (rate
// limit) or 402 (no credits) pauses the model for a while — free tiers are
// capped per account and per day, and there is no point in paying the
// timeout on every move while the quota is gone.
type LLM struct {
	BaseURL string // e.g. https://openrouter.ai/api/v1, https://api.deepseek.com/v1
	APIKey  string
	Models  []string // tried in order; "openrouter/free" picks any free model currently available
	Timeout time.Duration
	Client  *http.Client
	// Referer and Title are OpenRouter's optional attribution headers.
	Referer string
	Title   string

	// Discover adds the free models OpenRouter currently lists (GET /models)
	// after the configured ones, so a renamed ":free" id never leaves the
	// bots without a model.
	Discover bool

	mu          sync.Mutex
	pausedUntil time.Time
	pauseReason string
	discovered  []string
	cooling     map[string]time.Time // model -> skipped until (upstream limit, 404, 5xx, too slow)
	calls       int
	ok          int
	failed      int
	lastError   string
	lastModel   string
	lastOK      time.Time
}

// Status is a snapshot for monitoring: which models are in play, whether the
// brain is resting and how the calls went.
type Status struct {
	Configured  []string  `json:"configured"`
	Discovered  []string  `json:"discovered"`
	Paused      bool      `json:"paused"`
	PauseReason string    `json:"pause_reason,omitempty"`
	PausedUntil time.Time `json:"paused_until,omitempty"`
	Calls       int       `json:"calls"`
	OK          int       `json:"ok"`
	Failed      int       `json:"failed"`
	LastError   string    `json:"last_error,omitempty"`
	LastModel   string    `json:"last_model,omitempty"`
	LastOK      time.Time `json:"last_ok,omitempty"`
	// Cooling lists models skipped for a while after an upstream rate limit,
	// a 404/5xx or a timeout (the others are tried instead).
	Cooling []string `json:"cooling,omitempty"`
}

// Status implements monitoring.
func (l *LLM) Status() Status {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	return Status{
		Configured:  append([]string{}, l.Models...),
		Discovered:  append([]string{}, l.discovered...),
		Cooling:     l.coolingLocked(time.Now()),
		Paused:      now.Before(l.pausedUntil),
		PauseReason: l.pauseReason,
		PausedUntil: l.pausedUntil,
		Calls:       l.calls,
		OK:          l.ok,
		Failed:      l.failed,
		LastError:   l.lastError,
		LastModel:   l.lastModel,
		LastOK:      l.lastOK,
	}
}

// candidates returns the models to try in order: the configured ones, then
// the discovered free models. The generic free router goes last once the
// catalog has been read: it picks a random free model, thinking ones
// included, while the discovered list is ranked for quick JSON answers.
func (l *LLM) candidates() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	router := false
	for _, m := range append(append([]string{}, l.Models...), l.discovered...) {
		if m == FreeRouter && len(l.discovered) > 0 {
			router = true
			continue
		}
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	if router {
		out = append(out, FreeRouter)
	}
	return out
}

// NewLLM builds a client; models is a comma-separated list tried in order.
func NewLLM(baseURL, apiKey, models string, timeout time.Duration) *LLM {
	var list []string
	for _, m := range strings.Split(models, ",") {
		if m = strings.TrimSpace(m); m != "" {
			list = append(list, m)
		}
	}
	return &LLM{BaseURL: baseURL, APIKey: apiKey, Models: list, Timeout: timeout, Referer: "https://github.com/breezeareasay/durag", Title: "DURAG"}
}

var (
	// ErrBadAnswer is returned when the model's JSON does not name a legal move.
	ErrBadAnswer = errors.New("llm: answer is not a legal move")
	// ErrPaused is returned while the model is resting after a rate limit or an empty balance.
	ErrPaused = errors.New("llm: paused")
	// ErrEmptyAnswer is returned when the model sent no text at all (typically a
	// thinking model that spent the whole token budget on its thoughts).
	ErrEmptyAnswer = errors.New("llm: empty answer")
)

// httpError carries the status so Decide can tell a dead model from a dead quota.
type httpError struct {
	status int
	body   string
}

func (e *httpError) Error() string { return fmt.Sprintf("llm: http %d: %s", e.status, e.body) }

const (
	pauseAfterRateLimit = 5 * time.Minute
	pauseAfterNoCredits = time.Hour
)

// FreeRouter is OpenRouter's router over whatever free model is currently up.
const FreeRouter = "openrouter/free"

// IsFree reports whether a model id is one OpenRouter serves for free.
func IsFree(model string) bool {
	return model == FreeRouter || strings.HasSuffix(model, ":free")
}

// FreeOnly keeps the free models of a list (the guard behind LLM_FREE_ONLY).
// With nothing free left it falls back to the free router rather than to a
// paid model.
func FreeOnly(models []string) (kept, dropped []string) {
	for _, m := range models {
		if IsFree(m) {
			kept = append(kept, m)
		} else {
			dropped = append(dropped, m)
		}
	}
	if len(kept) == 0 {
		kept = []string{FreeRouter}
	}
	return kept, dropped
}

// pauseForRateLimit reads OpenRouter's reason out of a 429: the per-minute
// cap clears in a minute, the per-day cap at UTC midnight.
func pauseForRateLimit(body string, now time.Time) time.Duration {
	l := strings.ToLower(body)
	switch {
	case strings.Contains(l, "per-min") || strings.Contains(l, "per minute"):
		return time.Minute
	case strings.Contains(l, "per-day") || strings.Contains(l, "per day") || strings.Contains(l, "daily"):
		y, m, d := now.UTC().Date()
		midnight := time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC)
		return midnight.Sub(now) + time.Minute
	}
	return pauseAfterRateLimit
}

// Paused reports whether the model is resting and why.
const (
	// modelCooldown is how long a model is skipped after an upstream rate
	// limit, a 5xx or a timeout; a 404/400 (the id is gone or rejected)
	// keeps it out for modelCooldownLong.
	modelCooldown     = 2 * time.Minute
	modelCooldownLong = 10 * time.Minute
)

// isUpstreamLimit tells an OpenRouter 429 that concerns one model's provider
// ("temporarily rate-limited upstream", limit_source upstream_provider_shared_pool)
// from the account-wide free-tier caps: the former means "try another model".
func isUpstreamLimit(body string) bool {
	b := strings.ToLower(body)
	return strings.Contains(b, "upstream") || strings.Contains(b, "provider returned error") || strings.Contains(b, "temporarily rate-limited")
}

// coolDown skips a model for d.
func (l *LLM) coolDown(model string, d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cooling == nil {
		l.cooling = map[string]time.Time{}
	}
	l.cooling[model] = time.Now().Add(d)
}

// coolingLocked returns the models still cooling down (l.mu held).
func (l *LLM) coolingLocked(now time.Time) []string {
	var out []string
	for m, until := range l.cooling {
		if until.After(now) {
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out
}

// ready filters out the cooling models; when every candidate is cooling they
// are all tried anyway rather than giving up.
func (l *LLM) ready(models []string, now time.Time) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, m := range models {
		if until, ok := l.cooling[m]; !ok || !until.After(now) {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return models
	}
	return out
}

func (l *LLM) Paused(now time.Time) (bool, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Before(l.pausedUntil) {
		return true, l.pauseReason
	}
	return false, ""
}

func (l *LLM) pause(d time.Duration, reason string) {
	l.mu.Lock()
	l.pausedUntil = time.Now().Add(d)
	l.pauseReason = reason
	l.mu.Unlock()
}

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Temperature    float64       `json:"temperature"`
	MaxTokens      int           `json:"max_tokens"`
	ResponseFormat *respFormat   `json:"response_format,omitempty"`
	// OpenRouter's unified reasoning control: hybrid "thinking" models
	// (Qwen3, DeepSeek V3.1, Gemini Flash) answer directly when it is off.
	Reasoning *reasoningOpt `json:"reasoning,omitempty"`
}

type reasoningOpt struct {
	Enabled bool `json:"enabled"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Reasoning models put their thoughts here (OpenRouter: reasoning,
	// other OpenAI-compatible servers: reasoning_content). When the token
	// budget runs out before the answer, content is empty and the JSON we
	// want may only exist in the thoughts.
	Reasoning        string            `json:"reasoning,omitempty"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	ReasoningDetails []reasoningDetail `json:"reasoning_details,omitempty"`
}

type reasoningDetail struct {
	Text string `json:"text"`
}

// thoughts returns the reasoning text, whichever field carries it.
func (m chatMessage) thoughts() string {
	if m.Reasoning != "" {
		return m.Reasoning
	}
	if m.ReasoningContent != "" {
		return m.ReasoningContent
	}
	var b strings.Builder
	for _, d := range m.ReasoningDetails {
		b.WriteString(d.Text)
		b.WriteString("\n")
	}
	return b.String()
}

// maxAnswerTokens leaves room for a model that thinks before it answers:
// with a small budget the thoughts eat it all and the answer never comes.
const maxAnswerTokens = 700

type respFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Model   string `json:"model"` // the model that actually answered (routers resolve it)
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// answer is what the model is asked to produce.
type answer struct {
	Move  *int   `json:"move"`
	Say   string `json:"say"`
	Emoji string `json:"emoji"`
}

const systemPrompt = `Ты играешь в «Дурака» (вариант DURAG) за одного из игроков. Правила кратко: атакующий кладёт карту, защищающийся бьёт её старшей картой той же масти или козырем (козырь известен только после вскрытия), может перевести картой того же достоинства или взять весь стол. Подкидывать можно карты тех достоинств, что уже на столе, но не больше, чем карт в руке у защищающегося. Джокер бьёт любую карту своего цвета, Супер-карта бьёт всё. Кто остался с картами последним — проиграл.
Тебе дают твою руку, стол и ПРОНУМЕРОВАННЫЙ список легальных ходов. Выбери лучший ход и ответь СТРОГО одним JSON-объектом вида {"move": <номер хода>, "say": "<короткая реплика до 60 символов или пустая строка>", "emoji": "<один эмодзи из списка 👍 👎 😂 😡 😎 🤔 🔥 💀 🤡 ❤️ 👏 🙏 или пустая строка>"}. Реплика — по-русски, в характере дворового игрока, без оскорблений, не чаще чем в каждом третьем ходе. Никакого текста вне JSON.`

// Decide implements Brain: the first model that answers with a legal move wins.
func (l *LLM) Decide(ctx context.Context, in Context) (Decision, error) {
	if len(in.Moves) == 0 {
		return Decision{}, ErrBadAnswer
	}
	if paused, why := l.Paused(time.Now()); paused {
		return Decision{}, fmt.Errorf("%w: %s", ErrPaused, why)
	}
	models := l.candidates()
	if len(models) == 0 {
		models = []string{FreeRouter}
	}
	models = l.ready(models, time.Now())
	l.mu.Lock()
	l.calls++
	l.mu.Unlock()
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var lastErr error
	for _, model := range models {
		opts := askOptions{jsonMode: true, noThink: true}
		d, err := l.ask(ctx, model, in, opts)
		for retry := 0; retry < 2; retry++ {
			var he *httpError
			if !errors.As(err, &he) {
				break
			}
			body := strings.ToLower(he.body)
			switch {
			case he.status == http.StatusBadRequest && opts.jsonMode && strings.Contains(body, "response_format"):
				// the provider behind this model does not do JSON mode: the prompt asks for JSON anyway
				opts.jsonMode = false
				d, err = l.ask(ctx, model, in, opts)
				continue
			case he.status == http.StatusBadRequest && opts.noThink && strings.Contains(body, "reasoning"):
				// a server that does not know the reasoning switch
				opts.noThink = false
				d, err = l.ask(ctx, model, in, opts)
				continue
			case he.status == http.StatusPaymentRequired:
				l.pause(pauseAfterNoCredits, "no credits (HTTP 402)")
				return Decision{}, l.noteFailure(err)
			case he.status == http.StatusTooManyRequests && isUpstreamLimit(he.body):
				// one model's provider is saturated: rest it, the next model may answer
				l.coolDown(model, modelCooldown)
			case he.status == http.StatusTooManyRequests:
				l.pause(pauseForRateLimit(he.body, time.Now()), "rate limited (HTTP 429)")
				return Decision{}, l.noteFailure(err)
			case he.status == http.StatusNotFound || he.status == http.StatusBadRequest:
				l.coolDown(model, modelCooldownLong) // the id is gone or this model rejects our request
			case he.status >= 500:
				l.coolDown(model, modelCooldown)
			}
			break
		}
		if err == nil {
			l.mu.Lock()
			l.ok++
			l.lastModel = d.Model
			l.lastOK = time.Now()
			l.mu.Unlock()
			return d, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			l.coolDown(model, modelCooldown)      // too slow for a move: let the others go first next time
			return Decision{}, l.noteFailure(err) // out of time: the rules take over
		}
	}
	return Decision{}, l.noteFailure(lastErr)
}

func (l *LLM) noteFailure(err error) error {
	l.mu.Lock()
	l.failed++
	if err != nil {
		l.lastError = err.Error()
	}
	l.mu.Unlock()
	return err
}

// modelEntry is the part of GET /models the discovery needs.
type modelEntry struct {
	ID            string `json:"id"`
	ContextLength int    `json:"context_length"`
	Pricing       struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
	Architecture struct {
		Modality         string   `json:"modality"` // e.g. "text->text", "text+image->text", "text->audio"
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
	SupportedParameters []string `json:"supported_parameters"`
}

// notChat lists id fragments of models that do not play cards: vision, audio,
// music and image generators, safety classifiers, embeddings and the like.
var notChat = []string{"vision", "-vl", "image", "audio", "music", "lyria", "video", "veo", "imagen", "flux", "diffusion", "whisper", "transcri", "tts", "speech", "embed", "guard", "safety", "moderation", "rerank", "ocr"}

// isChatModel keeps catalog entries that take text and answer with text.
func isChatModel(m modelEntry) bool {
	id := strings.ToLower(m.ID)
	for _, k := range notChat {
		if strings.Contains(id, k) {
			return false
		}
	}
	if parts := strings.SplitN(m.Architecture.Modality, "->", 2); len(parts) == 2 && !strings.Contains(parts[1], "text") {
		return false
	}
	if len(m.Architecture.InputModalities) > 0 && !contains(m.Architecture.InputModalities, "text") {
		return false
	}
	if len(m.Architecture.OutputModalities) > 0 && !contains(m.Architecture.OutputModalities, "text") {
		return false
	}
	if len(m.SupportedParameters) > 0 && !contains(m.SupportedParameters, "max_tokens") {
		return false
	}
	return true
}

// activeSize estimates the billions of parameters a model runs per token
// from its id ("26b-a4b" -> 4, "31b" -> 31, "lightning"/"flash" -> 8), so
// quicker models go first inside a family; unknown sizes count as 30.
func activeSize(id string) int {
	id = strings.ToLower(id)
	active, total := 0, 0
	for i := 0; i+1 < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			continue
		}
		j := i
		for j < len(id) && id[j] >= '0' && id[j] <= '9' {
			j++
		}
		if j < len(id) && id[j] == 'b' && (j+1 == len(id) || !(id[j+1] >= 'a' && id[j+1] <= 'z')) {
			n, _ := strconv.Atoi(id[i:j])
			if i > 0 && id[i-1] == 'a' {
				active = n
			} else if n > total {
				total = n
			}
		}
		i = j
	}
	switch {
	case active > 0:
		return active
	case total > 0:
		return total
	}
	for _, k := range []string{"lightning", "flash", "lite", "mini", "small", "nano"} {
		if strings.Contains(id, k) {
			return 8
		}
	}
	return 30
}

// families lists the model families bots play best with, most preferred first.
var families = []string{"deepseek/", "qwen/", "meta-llama/", "mistralai/", "google/gemma", "openai/gpt-oss", "z-ai/", "moonshotai/", "nvidia/", "microsoft/"}

// DiscoverFree lists the free chat models of an OpenRouter-compatible
// catalog (GET /models, no key needed), best candidates first.
func DiscoverFree(ctx context.Context, client *http.Client, baseURL string) ([]string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("llm: models: http %d", resp.StatusCode)
	}
	var catalog struct {
		Data []modelEntry `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&catalog); err != nil {
		return nil, fmt.Errorf("llm: models: %w", err)
	}
	return rankFree(catalog.Data), nil
}

func rankFree(entries []modelEntry) []string {
	type cand struct {
		id   string
		fam  int
		size int
		ctxL int
	}
	var cands []cand
	for _, m := range entries {
		if m.ID == "" || m.ID == FreeRouter {
			continue
		}
		if p, err := strconv.ParseFloat(m.Pricing.Prompt, 64); err != nil || p != 0 {
			continue
		}
		if c, err := strconv.ParseFloat(m.Pricing.Completion, 64); err != nil || c != 0 {
			continue
		}
		if !isChatModel(m) {
			continue
		}
		id := strings.ToLower(m.ID)
		fam := len(families)
		for i, f := range families {
			if strings.HasPrefix(id, f) {
				fam = i
				break
			}
		}
		if thinksOnly(id) {
			fam += len(families) + 1 // after every family: slow, and the answer hides in the thoughts
		}
		cands = append(cands, cand{id: m.ID, fam: fam, size: activeSize(id), ctxL: m.ContextLength})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].fam != cands[j].fam {
			return cands[i].fam < cands[j].fam
		}
		if cands[i].size != cands[j].size {
			return cands[i].size < cands[j].size // quicker first: a move has seconds, not minutes
		}
		return cands[i].ctxL > cands[j].ctxL
	})
	var out []string
	for i, c := range cands {
		if i >= 8 {
			break
		}
		out = append(out, c.id)
	}
	return out
}

// thinksOnly recognises models that always reason before answering.
func thinksOnly(id string) bool {
	for _, k := range []string{"-r1", "/r1", "qwq", "thinking", "reason", "-think"} {
		if strings.Contains(id, k) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// RefreshModels re-reads the free models from the catalog (when Discover is
// on) and returns the full candidate list.
func (l *LLM) RefreshModels(ctx context.Context) ([]string, error) {
	if !l.Discover {
		return l.candidates(), nil
	}
	found, err := DiscoverFree(ctx, l.Client, l.BaseURL)
	if err != nil {
		return l.candidates(), err
	}
	l.mu.Lock()
	l.discovered = found
	l.mu.Unlock()
	return l.candidates(), nil
}

// askOptions are the request features a provider may reject; Decide retries
// without them.
type askOptions struct {
	jsonMode bool // response_format: json_object
	noThink  bool // reasoning: {enabled: false}
}

// ask sends one request for one model and validates the answer.
func (l *LLM) ask(ctx context.Context, model string, in Context, opts askOptions) (Decision, error) {
	req := chatRequest{
		Model:       model,
		Messages:    []chatMessage{{Role: "system", Content: systemPrompt}, {Role: "user", Content: Prompt(in)}},
		Temperature: 0.7,
		MaxTokens:   maxAnswerTokens,
	}
	if opts.jsonMode {
		req.ResponseFormat = &respFormat{Type: "json_object"}
	}
	if opts.noThink {
		req.Reasoning = &reasoningOpt{Enabled: false}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Decision{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(l.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Decision{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+l.APIKey)
	if l.Referer != "" {
		httpReq.Header.Set("HTTP-Referer", l.Referer)
	}
	if l.Title != "" {
		httpReq.Header.Set("X-Title", l.Title)
	}
	client := l.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return Decision{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Decision{}, err
	}
	if resp.StatusCode/100 != 2 {
		return Decision{}, &httpError{status: resp.StatusCode, body: strings.TrimSpace(string(raw))}
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return Decision{}, fmt.Errorf("llm: decode response: %w", err)
	}
	if cr.Error != nil {
		return Decision{}, errors.New("llm: " + cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return Decision{}, errors.New("llm: no choices")
	}
	answered := cr.Model
	if answered == "" {
		answered = model
	}
	choice := cr.Choices[0]
	content := strings.TrimSpace(choice.Message.Content)
	thoughts := strings.TrimSpace(choice.Message.thoughts())
	if content == "" && thoughts == "" {
		return Decision{}, fmt.Errorf("%w: %s sent no text (finish_reason=%q)", ErrEmptyAnswer, answered, choice.FinishReason)
	}
	d, err := Parse(content, in)
	if err != nil && thoughts != "" {
		// the answer may only exist in the thoughts of a reasoning model
		if d2, err2 := Parse(thoughts, in); err2 == nil {
			d, err = d2, nil
		}
	}
	if err != nil {
		if content == "" {
			return Decision{}, fmt.Errorf("%w: %s only thought, %d chars, no JSON in it (finish_reason=%q)", ErrEmptyAnswer, answered, len([]rune(thoughts)), choice.FinishReason)
		}
		return Decision{}, fmt.Errorf("%w from %s (finish_reason=%q): %s", err, answered, choice.FinishReason, snippet(content))
	}
	d.Model = answered
	return d, nil
}

// snippet shortens model output for an error message.
func snippet(s string) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > 90 {
		return string(r[:90]) + "…"
	}
	return string(r)
}

// Probe sends one tiny request so a deployment can see in its logs that the
// key, the endpoint and at least one model work. It returns the model that
// answered.
func (l *LLM) Probe(ctx context.Context) (string, error) {
	e := game.NewEngine(game.NewRand(1, 1))
	s := game.NewGameState("PROBE")
	for _, id := range []string{"a", "b"} {
		if err := e.Join(s, id, id, ""); err != nil {
			return "", err
		}
	}
	for _, id := range []string{"a", "b"} {
		if err := e.SetReady(s, id, true); err != nil {
			return "", err
		}
	}
	bot := s.CurrentTurn
	d, err := l.Decide(ctx, Context{State: s.Sanitized(bot), BotID: bot, Moves: s.LegalMoves(bot), Mood: MoodNeutral})
	if err != nil {
		return "", err
	}
	return d.Model, nil
}

// Prompt renders the bot's view of the table for the model.
func Prompt(in Context) string {
	s := in.State
	var b strings.Builder
	me := s.Player(in.BotID)
	fmt.Fprintf(&b, "Ты — %s. ", nameOf(s, in.BotID))
	switch in.Mood {
	case MoodAggressive:
		b.WriteString("Тебя провоцируют эмодзи: играй агрессивно, дави, переводи, подкидывай больше.\n")
	case MoodDefensive:
		b.WriteString("Над тобой насмехаются: играй осторожно, береги козыри, не рискуй.\n")
	default:
		b.WriteString("Настроение ровное: играй разумно.\n")
	}
	if me != nil {
		b.WriteString("Твоя рука: ")
		for i, c := range me.Hand {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(cardText(c))
		}
		b.WriteString("\n")
	}
	if s.TrumpRevealed && s.TrumpSuit != "" {
		fmt.Fprintf(&b, "Козырь: %s. ", suitText(s.TrumpSuit))
	} else {
		b.WriteString("Козырь ещё не вскрыт. ")
	}
	fmt.Fprintf(&b, "В колоде %d карт, в отбое %d, кон №%d.\n", s.DeckCount, s.DiscardCount, s.BoutNumber)
	b.WriteString("Игроки: ")
	for i, p := range s.Players {
		if i > 0 {
			b.WriteString("; ")
		}
		role := ""
		switch {
		case p.Out:
			role = ", вышел"
		case p.ID == s.DefenderID:
			role = ", защищается"
			if s.DefenderTaking {
				role = ", берёт"
			}
		case p.ID == s.CurrentTurn:
			role = ", ходит"
		}
		fmt.Fprintf(&b, "%s (%d карт%s)", p.Name, p.HandCount, role)
	}
	b.WriteString("\nСтол: ")
	if len(s.TableOrder) == 0 {
		b.WriteString("пусто")
	}
	for i, id := range s.TableOrder {
		if i > 0 {
			b.WriteString("; ")
		}
		if c, ok := game.CardByID(id); ok {
			b.WriteString(cardText(c))
		}
		if d := s.TableCards[id]; len(d) > 0 {
			b.WriteString(" побита " + cardText(d[0]))
		} else {
			b.WriteString(" (не побита)")
		}
	}
	if len(in.Recent) > 0 {
		b.WriteString("\nНедавние эмодзи: ")
		for i, e := range in.Recent {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s %s", e.FromName, e.Emoji)
		}
	}
	if in.AdminJokeDue {
		b.WriteString("\nОдин раз за игру ты можешь сломать четвёртую стену — пошутить, что ты бот и привет админу. Можешь сделать это сейчас в поле say.")
	}
	b.WriteString("\nЛегальные ходы:\n")
	for i, m := range in.Moves {
		fmt.Fprintf(&b, "%d. %s\n", i+1, moveText(m))
	}
	b.WriteString("Ответь JSON, без рассуждений. /no_think")
	return b.String()
}

// Parse validates the model's JSON against the legal moves. The answer object
// may sit inside prose, code fences or a reasoning model's thoughts, so the
// last JSON object that has a "move" field counts.
func Parse(content string, in Context) (Decision, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Decision{}, ErrEmptyAnswer
	}
	a, ok := findAnswer(content)
	if !ok {
		return Decision{}, errors.New(`llm: bad json: no {"move": n} object in the answer`)
	}
	if *a.Move < 1 || *a.Move > len(in.Moves) {
		return Decision{}, ErrBadAnswer
	}
	d := Decision{Move: in.Moves[*a.Move-1], Say: trimSay(a.Say), Emoji: a.Emoji}
	if d.Say != "" && in.AdminJokeDue && mentionsAdmin(d.Say) {
		d.AdminJoke = true
	}
	return d, nil
}

// findAnswer tries every "{" from the last one backwards and returns the
// first object that decodes and names a move; the final answer of a thinking
// model comes after its drafts.
func findAnswer(content string) (answer, bool) {
	for i := strings.LastIndex(content, "{"); i >= 0; i = strings.LastIndex(content[:i], "{") {
		var a answer
		dec := json.NewDecoder(strings.NewReader(content[i:]))
		if err := dec.Decode(&a); err == nil && a.Move != nil {
			return a, true
		}
	}
	return answer{}, false
}

func trimSay(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return s
}

func mentionsAdmin(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "админ") || strings.Contains(l, "admin") || strings.Contains(l, "бот") || strings.Contains(l, "скрипт")
}

func nameOf(s *game.GameState, id string) string {
	if p := s.Player(id); p != nil {
		return p.Name
	}
	return id
}

var suitNames = map[string]string{game.SuitHearts: "червы", game.SuitDiamonds: "бубны", game.SuitClubs: "трефы", game.SuitSpades: "пики"}

func suitText(suit string) string {
	if n, ok := suitNames[suit]; ok {
		return n
	}
	return suit
}

func cardText(c game.Card) string {
	switch {
	case c.IsSuper():
		return "СУПЕР-КАРТА"
	case c.ID == "RJ":
		return "КРАСНЫЙ ДЖОКЕР"
	case c.ID == "BJ":
		return "ЧЁРНЫЙ ДЖОКЕР"
	}
	rank := map[int]string{11: "В", 12: "Д", 13: "К", 14: "Т"}[c.Rank]
	if rank == "" {
		rank = fmt.Sprint(c.Rank)
	}
	return rank + " " + suitText(c.Suit)
}

func moveText(m game.Move) string {
	card := func(id string) string {
		if c, ok := game.CardByID(id); ok {
			return cardText(c)
		}
		return id
	}
	switch m.Type {
	case game.MoveAttack:
		return "зайти " + card(m.CardID)
	case game.MoveThrowIn:
		return "подкинуть " + card(m.CardID)
	case game.MoveDefend:
		return "побить " + card(m.TargetID) + " картой " + card(m.CardID)
	case game.MoveTransfer:
		return "перевести картой " + card(m.CardID)
	case game.MoveTake:
		return "взять весь стол"
	case game.MovePass:
		return "бито (больше не подкидывать)"
	case game.MoveTakeStump:
		return "взять пенёк"
	}
	return string(m.Type)
}
