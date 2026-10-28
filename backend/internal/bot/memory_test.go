package bot

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// twoSeats deals a fixed game between a human (H) and a bot (B) and hands
// the deal over with the human holding a known card to attack with.
func twoSeats(t *testing.T) (*game.Engine, *game.GameState) {
	t.Helper()
	e := game.NewEngine(game.NewRand(3, 3))
	s := game.NewGameState("MEM")
	if err := e.Join(s, "H", "Вася", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.AddBot(s, "B", "Крест", "🕶"); err != nil {
		t.Fatal(err)
	}
	if err := e.SetReady(s, "H", true); err != nil {
		t.Fatal(err)
	}
	return e, s
}

func TestMemoryRemembersTakenCardsAndWhoThrewThem(t *testing.T) {
	for _, delayed := range []bool{true, false} {
		t.Run(fmt.Sprint("delayed=", delayed), func(t *testing.T) { testMemoryTake(t, delayed) })
	}
}

func testMemoryTake(t *testing.T, delayed bool) {
	e, s := twoSeats(t)
	if delayed {
		e.ResolveDelay = 3 * time.Second
	}
	m := NewMemory()
	m.Observe(s)
	attacker, defender := s.CurrentTurn, s.DefenderID
	card := s.Player(attacker).Hand[0]
	if err := e.PlayCard(s, attacker, card.ID, ""); err != nil {
		t.Fatal(err)
	}
	m.Observe(s)
	if m.origin[card.ID] != attacker {
		t.Fatalf("origin of %s: %q, want %q", card.ID, m.origin[card.ID], attacker)
	}
	if err := e.TakeCards(s, defender); err != nil {
		t.Fatal(err)
	}
	if s.DefenderTaking && s.Phase != game.PhaseResolving {
		if err := e.Pass(s, attacker); err != nil {
			t.Fatal(err)
		}
	}
	if delayed {
		if s.Phase != game.PhaseResolving || s.ResolveOutcome != game.OutcomeTook {
			t.Fatalf("expected a resolving took bout, got phase %q outcome %q", s.Phase, s.ResolveOutcome)
		}
		m.Observe(s)
		m.Observe(s) // the same state twice must not double count
		if err := e.ResolveBout(s); err != nil {
			t.Fatal(err)
		}
	} else if len(s.TableOrder) != 0 {
		t.Fatalf("without a resolve delay the table clears at once")
	}
	m.Observe(s)
	known := m.Known(defender)
	if len(known) != 1 || known[0].ID != card.ID {
		t.Fatalf("known in %s's hand: %v, want [%s]", defender, known, card.ID)
	}
	if defender == "B" {
		if got := m.TookFrom(card.ID); got != "H" {
			t.Fatalf("took from %q, want H", got)
		}
	} else if got := m.TookFrom(card.ID); got != "" {
		t.Fatalf("a human taking is not a bot's memory, got %q", got)
	}
}

func TestBeatProbIsOneForAKnownCoverAndFallsWithRank(t *testing.T) {
	_, s := twoSeats(t)
	view := s.Sanitized("B")
	trump := view.ActiveTrump()
	// nothing known: an Ace is harder to beat than a seven of the same suit
	suit := game.SuitHearts
	if trump == suit {
		suit = game.SuitClubs
	}
	var m *Memory
	low := m.BeatProb(view, "H", game.NewCard(suit, 7))
	high := m.BeatProb(view, "H", game.NewCard(suit, game.RankAce))
	if !(low > high) || low <= 0 || low > 1 {
		t.Fatalf("P(beat 7)=%.3f should exceed P(beat A)=%.3f", low, high)
	}
	// a Super card known in the hand beats everything: P = 1
	mem := NewMemory()
	mem.holder[game.SuperCardID] = "H"
	if p := mem.BeatProb(view, "H", game.NewCard(suit, game.RankAce)); p != 1 {
		t.Fatalf("known Super card: P=%.3f, want 1", p)
	}
	// a player with no cards beats nothing
	empty := view.Clone()
	empty.Player("H").HandCount = 0
	if p := NewMemory().BeatProb(empty, "H", game.NewCard(suit, 7)); p != 0 {
		t.Fatalf("empty hand: P=%.3f, want 0", p)
	}
}

func TestChoosePlanReadsTheTable(t *testing.T) {
	_, s := twoSeats(t)
	view := s.Sanitized("B")
	if p := ChoosePlan(view, "B"); p != PlanHoard {
		t.Fatalf("fresh deal, trump hidden: %s, want hoard", p)
	}
	view.TrumpRevealed = true
	if p := ChoosePlan(view, "B"); p != PlanSave {
		t.Fatalf("trump known: %s, want save", p)
	}
	view.DefenderTaking = true
	view.DefenderID = "H"
	if p := ChoosePlan(view, "B"); p != PlanPress {
		t.Fatalf("defender taking: %s, want press", p)
	}
	view.DefenderTaking = false
	view.DeckCount = 0
	if p := ChoosePlan(view, "B"); p != PlanCount {
		t.Fatalf("empty deck: %s, want count", p)
	}
	view.Player("H").HandCount = 2
	if p := ChoosePlan(view, "B"); p != PlanHunt {
		t.Fatalf("empty deck, two cards: %s, want hunt", p)
	}
}

func TestLevelsBlunderDifferently(t *testing.T) {
	if ParseLevel("HARD") != LevelHard || ParseLevel("") != LevelNormal || ParseLevel("easy") != LevelEasy || ParseLevel("x") != LevelNormal {
		t.Fatal("ParseLevel")
	}
	if !(LevelEasy.Blunder() > LevelNormal.Blunder() && LevelNormal.Blunder() > LevelHard.Blunder()) {
		t.Fatal("blunder rates are not ordered")
	}
	// the hard bot always returns a legal move, the easy one too (only a different one at times)
	_, s := twoSeats(t)
	for _, lvl := range Levels {
		h := NewHeuristic(rand.New(rand.NewPCG(1, 2)))
		for i := 0; i < 20; i++ {
			bot := s.CurrentTurn
			in := Context{State: s.Sanitized(bot), BotID: bot, Moves: s.LegalMoves(bot), Level: lvl, Memory: NewMemory()}
			d, err := h.Decide(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			legal := false
			for _, m := range in.Moves {
				if m == d.Move {
					legal = true
				}
			}
			if !legal {
				t.Fatalf("%s: illegal move %s", lvl, d.Move)
			}
			if d.Plan == "" {
				t.Fatalf("%s: no plan", lvl)
			}
		}
	}
}

func TestTheMemoryPlaysButNeverTalks(t *testing.T) {
	_, s := twoSeats(t)
	bot := s.CurrentTurn
	view := s.Sanitized(bot)
	m := NewMemory()
	card := view.Player(bot).Hand[0]
	m.tookFrom[card.ID] = "H"
	m.Recall = Recall{Card: card, PlayerID: "H", Name: "Вася", Version: view.Version}
	for seed := uint64(1); seed <= 30; seed++ {
		h := NewHeuristic(rand.New(rand.NewPCG(seed, 9)))
		in := Context{State: view, BotID: bot, Moves: []game.Move{{Type: game.MoveAttack, CardID: card.ID}}, Level: LevelHard, Memory: m, Turn: int(seed)}
		d, _ := h.Decide(context.Background(), in)
		if strings.Contains(d.Say, "подкидывал") || strings.Contains(d.Say, "та самая") || strings.Contains(d.Say, "Считаю") {
			t.Fatalf("lines about remembered cards are gone: %q", d.Say)
		}
	}
}

func containsName(s, name string) bool {
	return len(s) > 0 && (indexOf(s, name) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestMemoryTracksTheDiscardExactly plays whole games with the memory
// watching and checks after every move that it counts the discard to the
// card and never places a card in a hand that does not hold it.
func TestMemoryTracksTheDiscardExactly(t *testing.T) {
	for seed := uint64(1); seed <= 12; seed++ {
		e := game.NewEngine(game.NewRand(seed, 11))
		s := game.NewGameState("MEMX")
		_ = e.AddBot(s, "b1", "b1", "")
		_ = e.Join(s, "h", "Human", "")
		if seed%2 == 0 {
			_ = e.AddBot(s, "b2", "b2", "")
		}
		_ = e.SetReady(s, "h", true)
		brain := NewHeuristic(rand.New(rand.NewPCG(seed, 5)))
		memory := NewMemory()
		for steps := 0; s.Status == game.StatusPlaying && steps < 4000; steps++ {
			e.ResolveIfDue(s, time.Now().Add(time.Hour))
			e.StumpsIfDue(s, time.Now().Add(time.Hour))
			memory.Observe(s)
			if memory.Gone() != s.DiscardCount {
				t.Fatalf("seed %d step %d: memory sees %d gone, discard holds %d", seed, steps, memory.Gone(), s.DiscardCount)
			}
			for id, who := range memory.holder {
				p := s.Player(who)
				held := false
				for _, c := range p.Hand {
					held = held || c.ID == id
				}
				if !held {
					t.Fatalf("seed %d step %d: memory places %s with %s, who does not hold it", seed, steps, id, who)
				}
			}
			actors := append([]string{}, s.Actors()...)
			actors = append(actors, s.StumpPending...)
			if len(actors) == 0 {
				continue
			}
			id := actors[steps%len(actors)]
			moves := s.LegalMoves(id)
			if len(moves) == 0 {
				continue
			}
			in := Context{State: s.Sanitized(id), BotID: id, Moves: moves, Level: LevelNormal, Memory: memory.Snapshot()}
			d, _ := brain.Decide(context.Background(), in)
			if err := e.ApplyMove(s, id, d.Move); err != nil {
				t.Fatal(err)
			}
		}
	}
}
