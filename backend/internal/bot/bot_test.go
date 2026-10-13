package bot

import (
	"context"
	"encoding/json"
	"errors"
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
		http.Error(w, `{"error":{"message":"Rate limit exceeded: free-models-per-day"}}`, 429)
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
	if paused, _ := l.Paused(time.Now().Add(pauseAfterRateLimit + time.Second)); paused {
		t.Fatal("the pause must end")
	}
}
