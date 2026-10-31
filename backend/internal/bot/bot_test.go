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
		n := 1 + int(seed%3) // 1..3 bots + one human: a room keeps a human seat
		ids := []string{"b1", "b2", "b3"}[:n]
		for _, id := range ids {
			if err := e.AddBot(s, id, id, ""); err != nil {
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

func TestBanterCadenceIsEvenButIrregular(t *testing.T) {
	spoke := 0
	silence, longest := 0, 0
	var pattern []bool
	for i := 1; i <= 100; i++ {
		d := Due(i, 2)
		pattern = append(pattern, d)
		if d {
			spoke++
			silence = 0
		} else {
			silence++
			if silence > longest {
				longest = silence
			}
		}
	}
	if spoke < 45 || spoke > 55 {
		t.Fatalf("expected about every second move, spoke %d/100", spoke)
	}
	if longest > 2 {
		t.Fatalf("silence of %d moves in a row", longest)
	}
	// not a plain alternation: the golden rotation never repeats
	alternating := true
	for i := 2; i < len(pattern); i++ {
		if pattern[i] != pattern[i-2] {
			alternating = false
			break
		}
	}
	if alternating {
		t.Fatal("cadence must not be a fixed every-other-move pattern")
	}
	if !Due(5, 1) {
		t.Fatal("every=1 always talks")
	}
	if SpeakEvery < 3 {
		t.Fatalf("a bot must not commentate every other move: SpeakEvery=%d", SpeakEvery)
	}
}

func TestLastEventAndJabs(t *testing.T) {
	in := sampleContext(t)
	s := in.State
	human := "h"
	if in.BotID == "h" {
		human = "b"
	}
	s.Log = append(s.Log, game.LogEntry{Type: "took", PlayerID: human, Text: "4"})
	ev := LastEvent(s, in.BotID)
	if ev.Kind != "took" || ev.Name != human || ev.Count != 4 {
		t.Fatalf("event %+v", ev)
	}
	if !strings.Contains(ev.String(), "взял 4") {
		t.Fatalf("prompt text %q", ev.String())
	}
	rng := rand.New(rand.NewPCG(3, 4))
	line := Jab(rng, ev)
	if !strings.Contains(line, human) && !strings.Contains(line, "4") {
		t.Fatalf("jab %q must mention the player or the count", line)
	}
	if strings.Contains(line, "{") {
		t.Fatalf("unfilled placeholder in %q", line)
	}
	// the bot's own entries are skipped; nothing notable gives the generic pool
	s.Log = []game.LogEntry{{Type: "took", PlayerID: in.BotID, Text: "6"}}
	if ev := LastEvent(s, in.BotID); ev.Kind != "" {
		t.Fatalf("own move must not be an event: %+v", ev)
	}
	if Jab(rng, Event{}) == "" {
		t.Fatal("generic jab pool is empty")
	}
	// the prompt carries the event and the permission to speak
	in.Event = Event{Kind: "lonely", Name: human}
	in.Turn = 5 // frac(5φ) ≈ 0.09 < 1/SpeakEvery: due
	p := Prompt(in)
	if !strings.Contains(p, "одна карта") || !strings.Contains(p, "очередь что-то сказать") {
		t.Fatalf("prompt lacks event or permission:\n%s", p)
	}
	in.Turn = 1 // frac(φ) ≈ 0.618: silent
	if !strings.Contains(Prompt(in), "Сейчас молчи") {
		t.Fatal("prompt must ask for silence off-cadence")
	}
}

func TestLLMTalkAsksForALineOnly(t *testing.T) {
	in := sampleContext(t)
	var system, user string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		for _, m := range req.Messages {
			if m.Role == "system" {
				system = m.Content
			} else {
				user = m.Content
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "google/gemma-4-31b-it:free",
			"choices": []map[string]any{{"message": map[string]any{"content": "Вот:\n{\"say\": \"Вася, ты уверен?\", \"emoji\": \"😎\"}"}}},
		})
	}))
	defer srv.Close()
	l := NewLLM(srv.URL, "k", "openrouter/free", 2*time.Second)
	line, err := l.Talk(context.Background(), in, in.Moves[0])
	if err != nil || line.Say != "Вася, ты уверен?" || line.Emoji != "😎" || line.Model != "google/gemma-4-31b-it:free" {
		t.Fatalf("line %+v %v", line, err)
	}
	if strings.Contains(system, "ПРОНУМЕРОВАННЫЙ") || !strings.Contains(system, "не называй карты соперников") {
		t.Fatalf("talk mode must use the talk prompt, got %q", system)
	}
	if !strings.Contains(user, "Твой ход только что") || strings.Contains(user, "Легальные ходы") {
		t.Fatalf("talk prompt must describe the move made, not offer moves:\n%s", user)
	}
	if st := l.Status(); st.Calls != 1 || st.OK != 1 {
		t.Fatalf("status %+v", st)
	}
}

func TestParseSayIsLenient(t *testing.T) {
	d, err := ParseSay("```json\n{\"say\": \"Ну-ну\", \"emoji\": \"\"}\n```")
	if err != nil || d.Say != "Ну-ну" {
		t.Fatalf("%+v %v", d, err)
	}
	if d, err := ParseSay(`{"say": ""}`); err != nil || d.Say != "" {
		t.Fatalf("an empty say is silence, not an error: %+v %v", d, err)
	}
	if _, err := ParseSay(`{"move": 2}`); err == nil {
		t.Fatal("an object without say is not a line")
	}
	if _, err := ParseSay(""); !errors.Is(err, ErrEmptyAnswer) {
		t.Fatalf("empty content: %v", err)
	}
	long := strings.Repeat("а", 200)
	if d, _ := ParseSay(`{"say": "` + long + `"}`); len([]rune(d.Say)) > 141 {
		t.Fatalf("lines are trimmed: %d runes", len([]rune(d.Say)))
	}
}

// leadTest builds a context where the bot attacks with the given hand.
func leadTest(t *testing.T, hand []string, deck int) Context {
	t.Helper()
	s := game.NewGameState("LEAD")
	s.Status = game.StatusPlaying
	s.MaxPlayers = 4
	s.Players = []game.Player{{ID: "b", Name: "Крест", IsBot: true, Connected: true}, {ID: "h", Name: "Вася", Connected: true}}
	for _, id := range hand {
		c, _ := game.CardByID(id)
		s.Players[0].Hand = append(s.Players[0].Hand, c)
	}
	s.Players[1].Hand = []game.Card{}
	for _, id := range []string{"H_2", "H_3", "H_4", "H_5", "H_6", "H_8"} {
		c, _ := game.CardByID(id)
		s.Players[1].Hand = append(s.Players[1].Hand, c)
	}
	s.CurrentTurn, s.DefenderID = "b", "h"
	s.TableCards = map[string][]game.Card{}
	s.Deck = game.NewDeck()[:deck]
	s.DeckCount = deck
	s.TrumpSuit = game.SuitSpades
	s.TrumpRevealed = deck == 0
	view := s.Sanitized("b")
	return Context{State: view, BotID: "b", Moves: s.LegalMoves("b"), Level: LevelHard, Plan: PlanPress, Mood: MoodAggressive}
}

func TestNoAcesOverSevensWhileTheDeckLasts(t *testing.T) {
	h := NewHeuristic(rand.New(rand.NewPCG(1, 2)))
	for seed := 0; seed < 20; seed++ {
		in := leadTest(t, []string{"C_14", "D_14", "H_14", "C_7", "D_9", "S_12"}, 20)
		d, _ := h.Decide(context.Background(), in)
		if d.Move.Type != game.MoveAttack || d.Move.CardID == "C_14" || d.Move.CardID == "D_14" || d.Move.CardID == "H_14" {
			t.Fatalf("led %s with a seven in hand and the deck alive", d.Move)
		}
	}
	// with the deck gone and a defender who cannot beat an ace of a plain suit, the aces may go
	in := leadTest(t, []string{"C_14", "D_14", "C_7"}, 0)
	in.Memory = NewMemory()
	d, _ := h.Decide(context.Background(), in)
	if d.Move.Type != game.MoveAttack {
		t.Fatalf("expected an attack, got %s", d.Move)
	}
}

func TestNoQueensThrownAtABeatingDefender(t *testing.T) {
	s := game.NewGameState("THROW")
	s.Status = game.StatusPlaying
	s.MaxPlayers = 4
	s.Players = []game.Player{{ID: "a", Name: "Вася", Connected: true}, {ID: "h", Name: "Бабуля", Connected: true}, {ID: "b", Name: "Крест", IsBot: true, Connected: true}}
	give := func(i int, ids ...string) {
		for _, id := range ids {
			c, _ := game.CardByID(id)
			s.Players[i].Hand = append(s.Players[i].Hand, c)
		}
	}
	give(0, "C_2", "C_3", "C_4", "C_5", "C_6")
	give(1, "H_2", "H_3", "H_4", "H_5", "H_6", "H_8")
	give(2, "D_12", "H_12", "S_7", "C_9", "D_10")
	s.CurrentTurn, s.DefenderID = "a", "h"
	s.TableOrder = []string{"C_12"}
	s.TableCards = map[string][]game.Card{"C_12": {}}
	s.Deck = game.NewDeck()[:20]
	s.DeckCount = 20
	s.TrumpSuit = game.SuitSpades
	view := s.Sanitized("b")
	in := Context{State: view, BotID: "b", Moves: s.LegalMoves("b"), Level: LevelHard, Mood: MoodAggressive, Plan: PlanSave}
	h := NewHeuristic(rand.New(rand.NewPCG(3, 4)))
	d, _ := h.Decide(context.Background(), in)
	if d.Move.Type == game.MoveThrowIn {
		t.Fatalf("threw %s at a defender who is still beating with the deck alive", d.Move)
	}
}

func TestTrimSayCutsAtASentenceNeverInAWord(t *testing.T) {
	short := "Хорошо, Бабуля, я заходил. Только голову мою мочить не надо."
	if got := trimSay(short); got != short {
		t.Fatalf("a short line is kept whole: %q", got)
	}
	long := strings.Repeat("Это первая фраза про стол. ", 4) + "А это длинный хвост, который никому не нужен и который можно смело отрезать вместе со всем остальным"
	got := trimSay(long)
	if len([]rune(got)) > 141 || !strings.HasSuffix(got, ".") {
		t.Fatalf("a long line ends at a sentence: %q", got)
	}
	words := strings.Repeat("слово ", 40)
	got = trimSay(words)
	if !strings.HasSuffix(got, "слово…") {
		t.Fatalf("no sentence end: cut at a space: %q", got)
	}
}

func TestPaidFallbackAnswersWhenTheFreeModelsAreLimited(t *testing.T) {
	in := sampleContext(t)
	var served []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		served = append(served, req.Model)
		if strings.HasSuffix(req.Model, ":free") {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded: free-models-per-day"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   req.Model,
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": `{"say": "Платно, но сказал"}`}}},
		})
	}))
	defer srv.Close()
	l := NewLLM(srv.URL, "k", "qwen/qwen3:free", 2*time.Second)
	l.Paid = []string{"deepseek/deepseek-chat"}
	l.PaidDaily = 2
	line, err := l.Talk(context.Background(), in, in.Moves[0])
	if err != nil || line.Say != "Платно, но сказал" || line.Model != "deepseek/deepseek-chat" {
		t.Fatalf("paid fallback: %+v %v", line, err)
	}
	if paused, _ := l.Paused(time.Now()); !paused {
		t.Fatal("the free side is paused after the daily limit")
	}
	// the brain is paused: the second call goes straight to the paid model, the third hits the daily cap
	if _, err := l.Talk(context.Background(), in, in.Moves[0]); err != nil {
		t.Fatalf("second paid call: %v", err)
	}
	if _, err := l.Talk(context.Background(), in, in.Moves[0]); !errors.Is(err, ErrPaused) {
		t.Fatalf("over the paid budget the brain rests: %v", err)
	}
	if st := l.Status(); st.PaidToday != 2 || st.PaidCalls != 2 || len(served) != 3 || served[0] != "qwen/qwen3:free" || served[1] != "deepseek/deepseek-chat" || served[2] != "deepseek/deepseek-chat" {
		t.Fatalf("status %+v served %v", st, served)
	}
}

func TestTheDogOnlyBarksAndTheCastIsFull(t *testing.T) {
	names := map[string]bool{}
	for _, c := range Characters {
		if c.Name == "" || c.Avatar == "" || c.Persona == "" || len(c.Greet) == 0 || len(c.Took) == 0 || len(c.Retort) == 0 {
			t.Fatalf("character %q is missing pieces", c.Name)
		}
		if names[c.Name] {
			t.Fatalf("duplicate character %q", c.Name)
		}
		names[c.Name] = true
	}
	if len(Characters) < 8 {
		t.Fatalf("only %d characters", len(Characters))
	}
	dog := CharacterOf("Шарик")
	if !dog.Barks {
		t.Fatal("the dog must bark")
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for _, line := range []string{"Привет, я модель, вот длинная реплика", GreetAs(dog, rng, Dossier{Name: "Alice"}), RetortAs(dog, rng, "Вася", 5)} {
		if v := dog.Voice(rng, line); !strings.Contains(v, "ав") && !strings.Contains(v, "Р-р") {
			t.Fatalf("the dog said %q", v)
		}
	}
	if CharacterOf("Вася").Voice(rng, "Ик") != "Ик" {
		t.Fatal("people keep their words")
	}
}

func TestNoJacksAndKingsLedOverSmallCards(t *testing.T) {
	for _, lvl := range []Level{LevelNormal, LevelHard} {
		for seed := uint64(1); seed <= 20; seed++ {
			in := leadTest(t, []string{"H_7", "C_9", "S_11", "D_11", "C_13", "H_12"}, 30)
			in.Level = lvl
			in.Memory = NewMemory()
			h := NewHeuristic(rand.New(rand.NewPCG(seed, 3)))
			d, _ := h.Decide(context.Background(), in)
			c, _ := game.CardByID(d.Move.CardID)
			if c.Rank > 10 {
				t.Fatalf("%s led %s over a seven and a nine", lvl, d.Move.CardID)
			}
		}
	}
}

func TestTheStumpPlanBeatsRatherThanTakes(t *testing.T) {
	// two cards in hand, a stump waiting, the cover costs a big trump: the stump plan still covers
	s := game.NewGameState("STUMP")
	s.Status = game.StatusPlaying
	s.MaxPlayers = 4
	s.Players = []game.Player{{ID: "b", Name: "Крест", IsBot: true, Connected: true}, {ID: "h", Name: "Вася", Connected: true}}
	for _, id := range []string{"S_13", "H_7"} {
		c, _ := game.CardByID(id)
		s.Players[0].Hand = append(s.Players[0].Hand, c)
	}
	s.Players[0].Stump = []game.Card{game.NewCard(game.SuitClubs, 9), game.NewCard(game.SuitClubs, 10)}
	for _, id := range []string{"D_6", "D_8", "C_6", "H_9", "H_10", "D_12"} {
		c, _ := game.CardByID(id)
		s.Players[1].Hand = append(s.Players[1].Hand, c)
	}
	s.CurrentTurn, s.DefenderID = "h", "b"
	a, _ := game.CardByID("D_6")
	s.TableOrder = []string{a.ID}
	s.TableCards = map[string][]game.Card{a.ID: {}}
	s.Deck = game.NewDeck()[:10]
	s.DeckCount = 10
	s.TrumpSuit = game.SuitSpades
	s.TrumpRevealed = true
	view := s.Sanitized("b")
	if p := ChoosePlan(view, "b"); p != PlanStump {
		t.Fatalf("plan %s, want stump", p)
	}
	in := Context{State: view, BotID: "b", Moves: s.LegalMoves("b"), Level: LevelHard, Plan: PlanStump, Memory: NewMemory()}
	h := NewHeuristic(rand.New(rand.NewPCG(4, 4)))
	d, _ := h.Decide(context.Background(), in)
	if d.Move.Type != game.MoveDefend {
		t.Fatalf("with the stump waiting the bot covers, got %s", d.Move)
	}
}

func TestHoardingKeepsTheTopsWhileTheTrumpIsHidden(t *testing.T) {
	in := leadTest(t, []string{"C_14", "D_13", "H_7", "S_9", "C_10", "D_8"}, 30)
	in.Level, in.Plan = LevelHard, PlanHoard
	in.Memory = NewMemory()
	h := NewHeuristic(rand.New(rand.NewPCG(2, 2)))
	for i := 0; i < 10; i++ {
		d, _ := h.Decide(context.Background(), in)
		c, _ := game.CardByID(d.Move.CardID)
		if c.Rank >= 13 {
			t.Fatalf("hoarding led %s", d.Move.CardID)
		}
	}
}

func TestTheChronicleTellsTheStoryAndCallsBack(t *testing.T) {
	e, s := twoSeats(t)
	c := NewChronicle()
	c.Observe(s)
	if c.Topic == "" {
		t.Fatal("no topic of the evening")
	}
	takes := 0
	for steps := 0; s.Status == game.StatusPlaying && takes < 3 && steps < 400; steps++ {
		e.ResolveIfDue(s, time.Now().Add(time.Hour))
		e.StumpsIfDue(s, time.Now().Add(time.Hour))
		c.Observe(s)
		actors := append([]string{}, s.Actors()...)
		actors = append(actors, s.StumpPending...)
		if len(actors) == 0 {
			continue
		}
		id := actors[0]
		moves := s.LegalMoves(id)
		var pick game.Move
		for _, m := range moves {
			if m.Type == game.MoveTake && id == "H" {
				pick = m
				break
			}
			pick = moves[0]
		}
		_ = e.ApplyMove(s, id, pick)
		takes = c.Takes["H"]
	}
	c.Observe(s)
	if c.Takes["H"] < 3 {
		t.Fatalf("the human took %d times", c.Takes["H"])
	}
	found := false
	for _, n := range c.Notes {
		found = found || strings.Contains(n, "третий раз")
	}
	if !found {
		t.Fatalf("no note about the third take: %v", c.Notes)
	}
	line := Callback(rand.New(rand.NewPCG(1, 1)), c.Snapshot(), "H", "Вася")
	if !strings.Contains(line, "Вася") {
		t.Fatalf("callback %q", line)
	}
	c.Said("Крест", "Тихо")
	if len(c.Lines) != 1 || c.Lines[0] != "Крест: Тихо" {
		t.Fatalf("lines %v", c.Lines)
	}
	if st := PickStoryline(rand.New(rand.NewPCG(1, 1)), []string{"Вася", "Петрович"}); st == nil || st.Steps[0].Who != "Вася" {
		t.Fatalf("storyline %+v", st)
	}
	if PickStoryline(rand.New(rand.NewPCG(1, 1)), []string{"Вася"}) != nil {
		t.Fatal("a storyline needs both characters")
	}
}

func TestNoKingsOrJokersOnAThreeInTheFirstBouts(t *testing.T) {
	// a bot defending a three with nothing but a king of the suit, a big
	// trump or the joker takes the single card while the deck is alive
	for _, tc := range []struct {
		hand []string
		name string
	}{
		{[]string{"C_13", "D_9", "H_8", "D_6", "H_10", "D_4"}, "a king on a three"},
		{[]string{"S_13", "D_9", "H_8", "D_6", "H_10", "D_4"}, "a king of trumps on a three"},
		{[]string{"BJ", "D_9", "H_8", "D_6", "H_10", "D_4"}, "a joker on a three"},
	} {
		s := game.NewGameState("EARLY")
		s.Status = game.StatusPlaying
		s.MaxPlayers = 4
		s.Players = []game.Player{{ID: "a", Name: "Вася", Connected: true}, {ID: "b", Name: "Крест", IsBot: true, Connected: true}}
		for _, id := range tc.hand {
			c, ok := game.CardByID(id)
			if !ok {
				t.Fatalf("no card %s", id)
			}
			s.Players[1].Hand = append(s.Players[1].Hand, c)
		}
		s.Players[0].Hand = []game.Card{{ID: "H_2", Suit: game.SuitHearts, Rank: 2}, {ID: "H_5", Suit: game.SuitHearts, Rank: 5}, {ID: "D_7", Suit: game.SuitDiamonds, Rank: 7}, {ID: "C_8", Suit: game.SuitClubs, Rank: 8}, {ID: "S_9", Suit: game.SuitSpades, Rank: 9}}
		s.CurrentTurn, s.DefenderID = "a", "b"
		s.TableOrder = []string{"C_3"}
		s.TableCards = map[string][]game.Card{"C_3": {}}
		s.Deck = game.NewDeck()[:20]
		s.DeckCount = 20
		s.TrumpSuit = game.SuitSpades
		s.TrumpRevealed = true
		view := s.Sanitized("b")
		in := Context{State: view, BotID: "b", Moves: s.LegalMoves("b"), Level: LevelHard, Plan: PlanSave}
		h := NewHeuristic(rand.New(rand.NewPCG(5, 6)))
		d, _ := h.Decide(context.Background(), in)
		if d.Move.Type != game.MoveTake {
			t.Fatalf("%s with the deck alive: %s", tc.name, d.Move)
		}
	}
}

func TestCleanSayDropsPromptEchoes(t *testing.T) {
	for _, bad := range []string{"Respond with JSON only", `{"say": "Вася, бери"}`, "Крест: think step by step", "<think>ну</think>", "Вася, you took five"} {
		if got := trimSay(bad); got != "" {
			t.Fatalf("kept %q as %q", bad, got)
		}
	}
	for _, good := range []string{"Вася, ок, бери", "«Бабуля, 5 карт. Запишу»", "Крест, за базар ответишь"} {
		if got := trimSay(good); got == "" {
			t.Fatalf("dropped a fine line %q", good)
		}
	}
}

func TestFreeAnswersNeverSpendThePaidBudget(t *testing.T) {
	in := sampleContext(t)
	var served []string
	limited := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		served = append(served, req.Model)
		if strings.HasSuffix(req.Model, ":free") && limited {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded: free-models-per-day"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   req.Model,
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": `{"say": "сказал"}`}}},
		})
	}))
	defer srv.Close()
	l := NewLLM(srv.URL, "k", "qwen/qwen3:free", 2*time.Second)
	l.Paid = []string{"deepseek/deepseek-chat"}
	l.PaidDaily = 1
	for i := 0; i < 5; i++ { // five free answers: the one paid call of the day is still there
		if line, err := l.Talk(context.Background(), in, in.Moves[0]); err != nil || line.Model != "qwen/qwen3:free" {
			t.Fatalf("free call %d: %+v %v", i, line, err)
		}
	}
	if st := l.Status(); st.PaidToday != 0 {
		t.Fatalf("free answers spent the paid budget: %+v", st)
	}
	limited = true
	line, err := l.Talk(context.Background(), in, in.Moves[0])
	if err != nil || line.Model != "deepseek/deepseek-chat" {
		t.Fatalf("the paid fallback after the free cap: %+v %v", line, err)
	}
	if st := l.Status(); st.PaidToday != 1 {
		t.Fatalf("one paid call booked: %+v", st)
	}
}

func TestAProviderLimitRestsOneModelNotTheBrain(t *testing.T) {
	in := sampleContext(t)
	var served []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		served = append(served, req.Model)
		if req.Model == "a:free" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"a:free is temporarily rate-limited upstream"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   req.Model,
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": `{"say": "сказал"}`}}},
		})
	}))
	defer srv.Close()
	l := NewLLM(srv.URL, "k", "a:free,b:free", 2*time.Second)
	line, err := l.Talk(context.Background(), in, in.Moves[0])
	if err != nil || line.Model != "b:free" {
		t.Fatalf("the next free model answers: %+v %v", line, err)
	}
	if paused, _ := l.Paused(time.Now()); paused {
		t.Fatal("one provider's limit must not pause the brain")
	}
	if _, err := l.Talk(context.Background(), in, in.Moves[0]); err != nil || served[len(served)-1] != "b:free" || len(served) != 3 {
		t.Fatalf("the limited model rests, the other answers directly: %v %v", served, err)
	}
}
