package bot

import (
	"context"
	"encoding/json"
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
	l := &LLM{BaseURL: srv.URL + "/v1", APIKey: "sk-test", Model: "deepseek-chat", Timeout: 2 * time.Second}
	d, err := l.Decide(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if d.Move != in.Moves[0] || d.Say != "Зашёл" || gotAuth != "Bearer sk-test" || gotModel != "deepseek-chat" {
		t.Fatalf("decision %+v auth %q model %q", d, gotAuth, gotModel)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"quota"}}`, 429)
	}))
	defer bad.Close()
	if _, err := (&LLM{BaseURL: bad.URL, Model: "m"}).Decide(context.Background(), in); err == nil {
		t.Fatal("http error must surface so the caller falls back")
	}
}
