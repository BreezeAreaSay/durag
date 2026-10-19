package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

	mu          sync.Mutex
	pausedUntil time.Time
	pauseReason string
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
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type respFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Model   string `json:"model"` // the model that actually answered (routers resolve it)
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// answer is what the model is asked to produce.
type answer struct {
	Move  int    `json:"move"`
	Say   string `json:"say"`
	Emoji string `json:"emoji"`
}

const systemPrompt = `Ты играешь в «Дурака» (вариант DURAG) за одного из игроков. Правила кратко: атакующий кладёт карту, защищающийся бьёт её старшей картой той же масти или козырем (козырь известен только после вскрытия), может перевести картой того же достоинства или взять весь стол. Подкидывать можно карты тех достоинств, что уже на столе, но не больше, чем карт в руке у защищающегося. Джокер бьёт любую карту своего цвета и Джокера другого цвета, Супер-карта бьёт всё. Когда защищающийся берёт, подкидывать можно без лимита по руке. Кто остался с картами последним — проиграл.
Тебе дают твою руку, стол и ПРОНУМЕРОВАННЫЙ список легальных ходов. Выбери лучший ход и ответь СТРОГО одним JSON-объектом вида {"move": <номер хода>, "say": "<реплика до 40 символов или пустая строка>", "emoji": "<один эмодзи из списка 👍 👎 😂 😡 😎 🤔 🔥 💀 🤡 ❤️ 👏 🙏 👊 🚬 или пустая строка; 👊 — стукнуть кулаком по столу, 🚬 — закурить>"}. Играй по линии, которую тебе называют (пресс, сброс, счёт, охота, экономия): это твоя стратегия на текущую ситуацию, меняй ход только если видишь явную ошибку. В конце игры не отдавай сильные карты без нужды: козырь и спецкарта — на защиту или на последний ход. Тебе могут назвать карты, которые ты точно помнишь на руках соперников, и карты, которые соперник сам тебе подкидывал: используй это и в игре, и в репликах.
Реплика — по-русски, одна короткая фраза в характере дворового игрока: подкалывай, провоцируй, шути про последний ход соперника, обращайся по имени, без мата и оскорблений. Если ходишь картой, которую тебе подкинул соперник, подколи его этим («помнишь, ты мне это подкидывал?»). Если соперник сыграл карту, которую ты помнил у него на руке, отметь это («а, та самая»). Говори только когда тебе разрешили в сообщении, иначе say — пустая строка. Никакого текста вне JSON.`

// Decide implements Brain: the first model that answers with a legal move wins.
func (l *LLM) Decide(ctx context.Context, in Context) (Decision, error) {
	if len(in.Moves) == 0 {
		return Decision{}, ErrBadAnswer
	}
	if paused, why := l.Paused(time.Now()); paused {
		return Decision{}, fmt.Errorf("%w: %s", ErrPaused, why)
	}
	models := l.Models
	if len(models) == 0 {
		models = []string{"openrouter/free"}
	}
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var lastErr error
	for _, model := range models {
		d, err := l.ask(ctx, model, in, true)
		var he *httpError
		if errors.As(err, &he) {
			switch {
			case he.status == http.StatusBadRequest && strings.Contains(strings.ToLower(he.body), "response_format"):
				// the provider behind this model does not do JSON mode: the prompt asks for JSON anyway
				d, err = l.ask(ctx, model, in, false)
			case he.status == http.StatusPaymentRequired:
				l.pause(pauseAfterNoCredits, "no credits (HTTP 402)")
				return Decision{}, err
			case he.status == http.StatusTooManyRequests:
				l.pause(pauseForRateLimit(he.body, time.Now()), "rate limited (HTTP 429)")
				return Decision{}, err
			}
		}
		if err == nil {
			return d, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return Decision{}, err // out of time: the rules take over
		}
	}
	return Decision{}, lastErr
}

// ask sends one request for one model and validates the answer.
func (l *LLM) ask(ctx context.Context, model string, in Context, jsonMode bool) (Decision, error) {
	req := chatRequest{
		Model:       model,
		Messages:    []chatMessage{{Role: "system", Content: systemPrompt}, {Role: "user", Content: Prompt(in)}},
		Temperature: 0.7,
		MaxTokens:   120,
	}
	if jsonMode {
		req.ResponseFormat = &respFormat{Type: "json_object"}
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
	d, err := Parse(cr.Choices[0].Message.Content, in)
	if err != nil {
		return Decision{}, err
	}
	d.Model = cr.Model
	if d.Model == "" {
		d.Model = model
	}
	return d, nil
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
	if ev := in.Event.String(); ev != "" {
		b.WriteString("\nПоследнее: " + ev + ".")
	}
	plan := in.Plan
	if plan == "" {
		plan = ChoosePlan(s, in.BotID)
	}
	b.WriteString("\nТвоя линия сейчас — " + PlanText(plan) + ".")
	if in.Memory != nil && in.level().Counts() {
		for _, p := range s.Players {
			if p.ID == in.BotID {
				continue
			}
			if known := in.Memory.Known(p.ID); len(known) > 0 {
				fmt.Fprintf(&b, "\nТы точно помнишь на руке у %s: %s.", p.Name, cardsText(known))
			}
		}
		if n := in.Memory.Gone(); n > 0 {
			fmt.Fprintf(&b, "\nВ отбое %d карт, ты их помнишь.", n)
		}
		if r := in.Memory.Recall; r.PlayerID != "" && r.Version == s.Version {
			fmt.Fprintf(&b, "\n%s только что сыграл %s — ты помнил эту карту у него на руке.", r.Name, cardText(r.Card))
		}
		var returned []string
		for _, m := range in.Moves {
			if from := in.Memory.TookFrom(m.CardID); from != "" {
				if c, ok := game.CardByID(m.CardID); ok {
					returned = append(returned, cardText(c)+" (подкинул "+nameOf(s, from)+")")
				}
			}
		}
		if len(returned) > 0 {
			b.WriteString("\nКарты у тебя на руке, которые тебе подкидывали соперники: " + strings.Join(returned, ", ") + ".")
		}
	}
	for _, p := range s.Players {
		if p.IsBot || p.ID == in.BotID {
			continue
		}
		if d, ok := in.Dossiers[p.ID]; ok {
			if text := d.String(); text != "" {
				fmt.Fprintf(&b, "\nДосье на %s (ты помнишь его с прошлых игр): %s.", p.Name, text)
			}
		}
	}
	if in.Overheard.Text != "" && in.Overheard.BotID != in.BotID {
		fmt.Fprintf(&b, "\nДругой бот, %s, только что сказал: «%s». Можешь ответить ему в say, если есть что.", in.Overheard.Name, in.Overheard.Text)
	}
	if in.Promoted != "" {
		b.WriteString("\nТы проиграл прошлую игру за этим столом и вернулся сильнее: скажи об этом в say одной фразой, с характером.")
	}
	if in.Hint != nil {
		for i, m := range in.Moves {
			if m == *in.Hint {
				fmt.Fprintf(&b, "\nСовет по линии: ход №%d.", i+1)
				if in.level() == LevelHard {
					b.WriteString(" Следуй ему, если нет явной причины иначе.")
				}
				break
			}
		}
	}
	if in.SpeakDue() {
		b.WriteString("\nСейчас твоя очередь что-то сказать: одна колкая фраза в say.")
	} else {
		b.WriteString("\nСейчас молчи: say пустая.")
	}
	if in.AdminJokeDue {
		b.WriteString("\nОдин раз за игру ты можешь сломать четвёртую стену — пошутить, что ты бот и привет админу. Можешь сделать это сейчас в поле say.")
	}
	b.WriteString("\nЛегальные ходы:\n")
	for i, m := range in.Moves {
		fmt.Fprintf(&b, "%d. %s\n", i+1, moveText(m))
	}
	b.WriteString("Ответь JSON.")
	return b.String()
}

// Parse validates the model's JSON against the legal moves.
func Parse(content string, in Context) (Decision, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	if i := strings.Index(content, "{"); i > 0 {
		content = content[i:]
	}
	if i := strings.LastIndex(content, "}"); i >= 0 && i < len(content)-1 {
		content = content[:i+1]
	}
	var a answer
	if err := json.Unmarshal([]byte(content), &a); err != nil {
		return Decision{}, fmt.Errorf("llm: bad json: %w", err)
	}
	if a.Move < 1 || a.Move > len(in.Moves) {
		return Decision{}, ErrBadAnswer
	}
	d := Decision{Move: in.Moves[a.Move-1], Say: trimSay(a.Say), Emoji: a.Emoji}
	if d.Say != "" && in.AdminJokeDue && mentionsAdmin(d.Say) {
		d.AdminJoke = true
	}
	return d, nil
}

func trimSay(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > 48 {
		return string(r[:48]) + "…"
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

func cardsText(cards []game.Card) string {
	parts := make([]string, 0, len(cards))
	for _, c := range cards {
		parts = append(parts, cardText(c))
	}
	return strings.Join(parts, ", ")
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
