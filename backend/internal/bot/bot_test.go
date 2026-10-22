package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// Bots of every mood play whole games against each other, on their own
// sanitized views, and every decision is a legal move.
func TestHeuristicPlaysWholeGames(t *testing.T) {
	moods := []Mood{MoodNeutral, MoodAggressive, MoodDefensive}
	for seed := uint64(1); seed <= 15; seed++ {
		e := game.NewEngine(game.NewRand(seed, 11))
		s := game.NewGameState("BOTS")
		n := 1 + int(seed%5) // 1..5 bots + one human: a room keeps a human seat
		ids := []string{"b1", "b2", "b3", "b4", "b5"}[:n]
		for _, id := range ids {
			if err := e.AddBot(s, id, id); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.Join(s, "h", "Human", ""); err != nil {
			t.Fatal(err)
		}
		if err := e.SetReady(s, "h", true); err != nil {
			t.Fatal(err)
		}
		if s.Status != game.StatusPlaying {
			t.Fatalf("seed %d: not started", seed)
		}
		brain := NewHeuristic(rand.New(rand.NewPCG(seed, 5)))
		steps := 0
		for s.Status == game.StatusPlaying && steps < 3000 {
			steps++
			e.ResolveIfDue(s, time.Now())
			e.StumpsIfDue(s, time.Now())
			actors := append([]string{}, s.Actors()...)
			actors = append(actors, s.StumpPending...)
			if len(actors) == 0 {
				continue
			}
			id := actors[steps%len(actors)]
			moves := s.LegalMoves(id)
			if len(moves) == 0 {
				t.Fatalf("seed %d step %d: actor %s has no legal move", seed, steps, id)
			}
			d, err := brain.Decide(context.Background(), Context{State: s.Sanitized(id), BotID: id, Moves: moves, Mood: moods[steps%3], AdminJokeDue: steps == 10})
			if err != nil {
				t.Fatal(err)
			}
			legal := false
			for _, m := range moves {
				if m == d.Move {
					legal = true
				}
			}
			if !legal {
				t.Fatalf("seed %d: heuristic chose an illegal move %s", seed, d.Move)
			}
			if err := e.ApplyMove(s, id, d.Move); err != nil {
				t.Fatalf("seed %d: apply %s by %s: %v", seed, d.Move, id, err)
			}
		}
		if s.Status != game.StatusFinished {
			t.Fatalf("seed %d: game did not finish in %d steps", seed, steps)
		}
	}
}

func TestMoodsAndReplies(t *testing.T) {
	if MoodFor("😡") != MoodAggressive || MoodFor("🔥") != MoodAggressive {
		t.Fatal("provocations must make the bot aggressive")
	}
	if MoodFor("😂") != MoodDefensive || MoodFor("🤡") != MoodDefensive {
		t.Fatal("mockery must make the bot careful")
	}
	if MoodFor("👍") != MoodNeutral {
		t.Fatal("friendly emoji calm the bot down")
	}
	rng := rand.New(rand.NewPCG(1, 2))
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		seen[Reply(rng, "😡")] = true
	}
	for _, want := range []string{"😎", "🔥", "😡", ""} {
		if !seen[want] {
			t.Fatalf("reply pool missing %q", want)
		}
	}
	if Reply(rng, "🤔") != "" {
		t.Fatal("a neutral emoji gets no reply")
	}
}

func sampleContext(t *testing.T) Context {
	t.Helper()
	e := game.NewEngine(game.NewRand(4, 4))
	s := game.NewGameState("LLM")
	for _, id := range []string{"h", "b"} {
		if err := e.Join(s, id, id, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"h", "b"} {
		if err := e.SetReady(s, id, true); err != nil {
			t.Fatal(err)
		}
	}
	bot := s.CurrentTurn
	return Context{State: s.Sanitized(bot), BotID: bot, Moves: s.LegalMoves(bot), Mood: MoodNeutral, AdminJokeDue: true}
}

func TestParseValidatesTheAnswer(t *testing.T) {
	in := sampleContext(t)
	d, err := Parse("```json\n{\"move\": 2, \"say\": \"Ну, погнали\", \"emoji\": \"😎\"}\n```", in)
	if err != nil {
		t.Fatal(err)
	}
	if d.Move != in.Moves[1] || d.Say != "Ну, погнали" || d.Emoji != "😎" || d.AdminJoke {
		t.Fatalf("decision %+v", d)
	}
	if _, err := Parse(`{"move": 0}`, in); err == nil {
		t.Fatal("move 0 is not legal")
	}
	if _, err := Parse(`{"move": 99}`, in); err == nil {
		t.Fatal("out of range move accepted")
	}
	if _, err := Parse(`not json`, in); err == nil {
		t.Fatal("garbage accepted")
	}
	d, err = Parse(`Вот мой ход: {"move": 1, "say": "Админ, привет, это я, бот"}`, in)
	if err != nil || !d.AdminJoke {
		t.Fatalf("fourth-wall line not recognised: %+v %v", d, err)
	}
	// a thinking model: drafts, nested braces and the final answer at the end
	thought := "Hmm, {move 1} is weak. Options: {\"a\": {\"b\": 1}}. I'll go with\n```json\n{\"move\": 2, \"say\": \"\"}\n```"
	d, err = Parse(thought, in)
	if err != nil || d.Move != in.Moves[1] {
		t.Fatalf("answer after the thoughts not found: %+v %v", d, err)
	}
	if _, err := Parse("", in); !errors.Is(err, ErrEmptyAnswer) {
		t.Fatalf("empty content must be ErrEmptyAnswer, got %v", err)
	}
	if _, err := Parse(`{"say": "no move here"}`, in); err == nil {
		t.Fatal("an object without a move accepted")
	}
	if !strings.Contains(Prompt(in), "Легальные ходы") || !strings.Contains(Prompt(in), "четвёртую стену") {
		t.Fatal("prompt must list the moves and offer the admin joke")
	}
}

func TestLLMDecideOverHTTP(t *testing.T) {
	in := sampleContext(t)
	var gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		gotModel, _ = req["model"].(string)
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "wrong path "+r.URL.Path, 404)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": `{"move": 1, "say": "Зашёл", "emoji": ""}`}}},
		})
	}))
	defer srv.Close()
	l := NewLLM(srv.URL+"/v1", "sk-test", "deepseek/deepseek-chat", 2*time.Second)
	d, err := l.Decide(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if d.Move != in.Moves[0] || d.Say != "Зашёл" || gotAuth != "Bearer sk-test" || gotModel != "deepseek/deepseek-chat" {
		t.Fatalf("decision %+v auth %q model %q", d, gotAuth, gotModel)
	}
}

func TestLLMReadsTheAnswerFromTheThoughts(t *testing.T) {
	in := sampleContext(t)
	var reasoningOff []bool
	mode := "thoughts"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		rs, _ := req["reasoning"].(map[string]any)
		reasoningOff = append(reasoningOff, rs != nil && rs["enabled"] == false)
		if mt, _ := req["max_tokens"].(float64); mt < 500 {
			http.Error(w, "max_tokens too small for a thinking model", 500)
			return
		}
		msg := map[string]any{"role": "assistant", "content": ""}
		switch mode {
		case "thoughts":
			msg["reasoning"] = "Let me think... the first move is fine. {\"move\": 1, \"say\": \"Погнали\"}"
		case "nothing":
			// the budget ran out while thinking: no text anywhere
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "deepseek/deepseek-r1:free",
			"choices": []map[string]any{{"message": msg, "finish_reason": "length"}},
		})
	}))
	defer srv.Close()
	l := NewLLM(srv.URL, "k", "openrouter/free", 2*time.Second)
	d, err := l.Decide(context.Background(), in)
	if err != nil || d.Move != in.Moves[0] || d.Say != "Погнали" || d.Model != "deepseek/deepseek-r1:free" {
		t.Fatalf("answer inside the reasoning not used: %+v %v", d, err)
	}
	if len(reasoningOff) != 1 || !reasoningOff[0] {
		t.Fatalf("thinking must be switched off in the request: %v", reasoningOff)
	}
	mode = "nothing"
	_, err = l.Decide(context.Background(), in)
	if !errors.Is(err, ErrEmptyAnswer) || !strings.Contains(err.Error(), "deepseek/deepseek-r1:free") || !strings.Contains(err.Error(), "length") {
		t.Fatalf("an empty answer must name the model and the finish reason: %v", err)
	}
	if st := l.Status(); st.Calls != 2 || st.OK != 1 || st.Failed != 1 || !strings.Contains(st.LastError, "sent no text") {
		t.Fatalf("status %+v", st)
	}
}

func TestLLMRetriesWithoutTheReasoningSwitch(t *testing.T) {
	in := sampleContext(t)
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		_, hasReasoning := req["reasoning"]
		_, hasFormat := req["response_format"]
		seen = append(seen, fmt.Sprintf("reasoning=%v format=%v", hasReasoning, hasFormat))
		if hasReasoning {
			http.Error(w, `{"error":{"message":"Unrecognized request argument supplied: reasoning"}}`, 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]any{"content": `{"move": 1}`}}}})
	}))
	defer srv.Close()
	d, err := NewLLM(srv.URL, "k", "gpt-4o-mini", 2*time.Second).Decide(context.Background(), in)
	if err != nil || d.Move != in.Moves[0] {
		t.Fatalf("decision %+v %v", d, err)
	}
	if len(seen) != 2 || seen[0] != "reasoning=true format=true" || seen[1] != "reasoning=false format=true" {
		t.Fatalf("expected one retry without the reasoning option, got %v", seen)
	}
}

func TestCandidatesTryTheFreeRouterLast(t *testing.T) {
	l := NewLLM("http://x", "k", "openrouter/free", time.Second)
	if got := strings.Join(l.candidates(), ","); got != "openrouter/free" {
		t.Fatalf("before discovery: %s", got)
	}
	l.discovered = []string{"a/one:free", "b/two:free"}
	if got := strings.Join(l.candidates(), ","); got != "a/one:free,b/two:free,openrouter/free" {
		t.Fatalf("after discovery the router must come last: %s", got)
	}
	l.Models = []string{"b/two:free", "openrouter/free"}
	if got := strings.Join(l.candidates(), ","); got != "b/two:free,a/one:free,openrouter/free" {
		t.Fatalf("explicit models first: %s", got)
	}
}

func TestUpstreamRateLimitRestsOneModelOnly(t *testing.T) {
	in := sampleContext(t)
	var asked []string
	// the exact shape OpenRouter returned on the production server
	upstream := `{"error":{"message":"Provider returned error","code":429,"metadata":{"raw":"google/gemma-4-26b-a4b-it:free is temporarily rate-limited upstream. Please retry shortly, or add your own key to accumulate your rate limits: https://openrouter.ai/settings/integrations","provider_name":"Google AI Studio","is_byok":false,"provider_error_code":"429","limit_source":"upstream_provider_shared_pool"}},"user_id":"user_x"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		model, _ := req["model"].(string)
		asked = append(asked, model)
		switch model {
		case "google/gemma-4-26b-a4b-it:free":
			http.Error(w, upstream, 429)
		case "gone/model:free":
			http.Error(w, `{"error":{"message":"No endpoints found for gone/model:free"}}`, 404)
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"model": model, "choices": []map[string]any{{"message": map[string]any{"content": `{"move": 1}`}}}})
		}
	}))
	defer srv.Close()
	l := NewLLM(srv.URL, "k", "google/gemma-4-26b-a4b-it:free,gone/model:free,nvidia/nemotron-3.5-lightning:free", 2*time.Second)
	d, err := l.Decide(context.Background(), in)
	if err != nil || d.Model != "nvidia/nemotron-3.5-lightning:free" {
		t.Fatalf("the next model must answer: %+v %v", d, err)
	}
	if paused, _ := l.Paused(time.Now()); paused {
		t.Fatal("an upstream limit of one model must not pause the whole brain")
	}
	if st := l.Status(); strings.Join(st.Cooling, ",") != "gone/model:free,google/gemma-4-26b-a4b-it:free" {
		t.Fatalf("both failing models should be cooling: %v", st.Cooling)
	}
	if _, err := l.Decide(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 4 || asked[3] != "nvidia/nemotron-3.5-lightning:free" {
		t.Fatalf("cooling models must be skipped on the next decision: %v", asked)
	}
	if st := l.Status(); st.Calls != 2 || st.OK != 2 || st.Failed != 0 {
		t.Fatalf("status %+v", st)
	}
}

func TestActiveSizeAndChatFilter(t *testing.T) {
	for id, want := range map[string]int{
		"google/gemma-4-26b-a4b-it:free":           4,
		"google/gemma-4-31b-it:free":               31,
		"nvidia/nemotron-3-ultra-550b-a55b:free":   55,
		"nvidia/nemotron-3-super-120b-a12b:free":   12,
		"nvidia/nemotron-3.5-lightning:free":       8,
		"deepseek/deepseek-chat-v3-0324:free":      30,
		"meta-llama/llama-3.3-70b-instruct:free":   70,
		"qwen/qwen3-235b-a22b:free":                22,
		"mistralai/mistral-small-3.2-24b-instruct": 24,
	} {
		if got := activeSize(id); got != want {
			t.Errorf("activeSize(%s) = %d, want %d", id, got, want)
		}
	}
	entry := func(id, modality string, params ...string) modelEntry {
		var m modelEntry
		m.ID = id
		m.Architecture.Modality = modality
		m.SupportedParameters = params
		return m
	}
	if isChatModel(entry("google/lyria-3-pro-preview", "text->audio")) || isChatModel(entry("nvidia/nemotron-3.5-content-safety:free", "text->text")) || isChatModel(entry("x/chatty", "text->text", "temperature")) {
		t.Fatal("music, safety classifiers and models without max_tokens are not chat models")
	}
	if !isChatModel(entry("google/gemma-4-31b-it:free", "text+image->text", "max_tokens", "temperature")) || !isChatModel(entry("x/plain", "")) {
		t.Fatal("text-answering models must pass")
	}
}

func TestLLMFallsThroughTheModelList(t *testing.T) {
	in := sampleContext(t)
	var asked []string
	var jsonModes []bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		model, _ := req["model"].(string)
		asked = append(asked, model)
		_, hasFormat := req["response_format"]
		jsonModes = append(jsonModes, hasFormat)
		switch model {
		case "gone/model:free":
			http.Error(w, `{"error":{"message":"No endpoints found"}}`, 404)
		case "picky/model:free":
			if hasFormat {
				http.Error(w, `{"error":{"message":"response_format is not supported by this provider"}}`, 400)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]any{"content": `{"move": 1}`}}}})
		default:
			http.Error(w, "unexpected model "+model, 500)
		}
	}))
	defer srv.Close()
	l := NewLLM(srv.URL, "k", " gone/model:free, picky/model:free ", 2*time.Second)
	d, err := l.Decide(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if d.Move != in.Moves[0] {
		t.Fatalf("decision %+v", d)
	}
	if len(asked) != 3 || asked[0] != "gone/model:free" || asked[1] != "picky/model:free" || asked[2] != "picky/model:free" || !jsonModes[1] || jsonModes[2] {
		t.Fatalf("expected 404 -> next model, then a retry without JSON mode; got %v %v", asked, jsonModes)
	}
}

func TestLLMPausesAfterRateLimitOrEmptyBalance(t *testing.T) {
	in := sampleContext(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		http.Error(w, `{"error":{"message":"Rate limit exceeded: free-models-per-min"}}`, 429)
	}))
	defer srv.Close()
	l := NewLLM(srv.URL, "k", "openrouter/free,deepseek/deepseek-chat", 2*time.Second)
	if _, err := l.Decide(context.Background(), in); err == nil {
		t.Fatal("a rate limit must surface so the rules take over")
	}
	if calls != 1 {
		t.Fatalf("a 429 is per account: no other model should be tried, got %d calls", calls)
	}
	if paused, why := l.Paused(time.Now()); !paused || !strings.Contains(why, "429") {
		t.Fatalf("expected a pause after 429: %v %q", paused, why)
	}
	if _, err := l.Decide(context.Background(), in); !errors.Is(err, ErrPaused) || calls != 1 {
		t.Fatalf("while paused the model must not be called: err=%v calls=%d", err, calls)
	}
	if paused, _ := l.Paused(time.Now().Add(time.Minute + time.Second)); paused {
		t.Fatal("a per-minute cap pauses for a minute only")
	}
}

func TestFreeOnlyAndRateLimitPauses(t *testing.T) {
	kept, dropped := FreeOnly([]string{"openrouter/free", "deepseek/deepseek-chat", "qwen/qwen3-235b-a22b:free"})
	if len(kept) != 2 || kept[0] != "openrouter/free" || kept[1] != "qwen/qwen3-235b-a22b:free" || len(dropped) != 1 || dropped[0] != "deepseek/deepseek-chat" {
		t.Fatalf("kept %v dropped %v", kept, dropped)
	}
	if kept, _ := FreeOnly([]string{"deepseek/deepseek-chat"}); len(kept) != 1 || kept[0] != FreeRouter {
		t.Fatalf("nothing free left must fall back to the free router, got %v", kept)
	}
	now := time.Date(2026, 10, 13, 20, 30, 0, 0, time.UTC)
	if d := pauseForRateLimit("Rate limit exceeded: free-models-per-min", now); d != time.Minute {
		t.Fatalf("per-minute cap: %v", d)
	}
	if d := pauseForRateLimit("Rate limit exceeded: free-models-per-day", now); d != 3*time.Hour+31*time.Minute {
		t.Fatalf("per-day cap should rest until UTC midnight: %v", d)
	}
	if d := pauseForRateLimit("slow down", now); d != pauseAfterRateLimit {
		t.Fatalf("unknown reason: %v", d)
	}
}

func TestProbeReportsTheAnsweringModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "qwen/qwen3-235b-a22b:free",
			"choices": []map[string]any{{"message": map[string]any{"content": `{"move": 1}`}}},
		})
	}))
	defer srv.Close()
	model, err := NewLLM(srv.URL, "k", "openrouter/free", 2*time.Second).Probe(context.Background())
	if err != nil || model != "qwen/qwen3-235b-a22b:free" {
		t.Fatalf("probe: %q %v", model, err)
	}
}

func TestDiscoverFreeRanksTheCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[
		  {"id":"openai/gpt-4o","context_length":128000,"pricing":{"prompt":"0.0000025","completion":"0.00001"}},
		  {"id":"qwen/qwen3-235b-a22b:free","context_length":131072,"pricing":{"prompt":"0","completion":"0"}},
		  {"id":"deepseek/deepseek-chat-v3-0324:free","context_length":163840,"pricing":{"prompt":"0","completion":"0"}},
		  {"id":"deepseek/deepseek-r1:free","context_length":64000,"pricing":{"prompt":"0","completion":"0"}},
		  {"id":"qwen/qwen2.5-vl-72b-instruct:free","context_length":32768,"pricing":{"prompt":"0","completion":"0"}},
		  {"id":"openrouter/free","context_length":0,"pricing":{"prompt":"0","completion":"0"}},
		  {"id":"meta-llama/llama-3.3-70b-instruct:free","context_length":131072,"pricing":{"prompt":"0","completion":"0"},"architecture":{"output_modalities":["text"]}},
		  {"id":"some/image-model:free","context_length":4096,"pricing":{"prompt":"0","completion":"0"},"architecture":{"output_modalities":["image"]}},
		  {"id":"google/gemma-4-31b-it:free","context_length":131072,"pricing":{"prompt":"0","completion":"0"},"architecture":{"modality":"text+image->text"}},
		  {"id":"google/gemma-4-26b-a4b-it:free","context_length":131072,"pricing":{"prompt":"0","completion":"0"},"architecture":{"modality":"text+image->text"}},
		  {"id":"nvidia/nemotron-3-ultra-550b-a55b:free","context_length":262144,"pricing":{"prompt":"0","completion":"0"}},
		  {"id":"nvidia/nemotron-3.5-lightning:free","context_length":131072,"pricing":{"prompt":"0","completion":"0"}},
		  {"id":"nvidia/nemotron-3.5-content-safety:free","context_length":32768,"pricing":{"prompt":"0","completion":"0"}},
		  {"id":"google/lyria-3-pro-preview","context_length":0,"pricing":{"prompt":"0","completion":"0"},"architecture":{"modality":"text->audio","output_modalities":["audio"]}}
		]}`))
	}))
	defer srv.Close()
	got, err := DiscoverFree(context.Background(), nil, srv.URL+"/api/v1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"deepseek/deepseek-chat-v3-0324:free", "qwen/qwen3-235b-a22b:free", "meta-llama/llama-3.3-70b-instruct:free", "google/gemma-4-26b-a4b-it:free", "google/gemma-4-31b-it:free", "nvidia/nemotron-3.5-lightning:free", "nvidia/nemotron-3-ultra-550b-a55b:free", "deepseek/deepseek-r1:free"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
	l := NewLLM(srv.URL+"/api/v1", "k", "openrouter/free", time.Second)
	l.Discover = true
	models, err := l.RefreshModels(context.Background())
	if err != nil || len(models) != len(want)+1 || models[0] != want[0] || models[len(want)] != "openrouter/free" {
		t.Fatalf("discovered models first, the router last: %v %v", models, err)
	}
	st := l.Status()
	if len(st.Discovered) != len(want) || st.Calls != 0 {
		t.Fatalf("status %+v", st)
	}
}
