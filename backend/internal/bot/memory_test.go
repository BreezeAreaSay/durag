package bot

import (
	"context"
	"fmt"
	"math/rand/v2"
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
	if err := e.AddBot(s, "B", "Нейронка"); err != nil {
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
	if p := ChoosePlan(view, "B"); p != PlanSave {
		t.Fatalf("fresh deal: %s, want save", p)
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

func TestReturnLineWhenGivingBackAThrownCard(t *testing.T) {
	_, s := twoSeats(t)
	bot := s.CurrentTurn
	view := s.Sanitized(bot)
	m := NewMemory()
	card := view.Player(bot).Hand[0]
	m.tookFrom[card.ID] = "H"
	hits := 0
	for seed := uint64(1); seed <= 30; seed++ {
		h := NewHeuristic(rand.New(rand.NewPCG(seed, 9)))
		in := Context{State: view, BotID: bot, Moves: []game.Move{{Type: game.MoveAttack, CardID: card.ID}}, Level: LevelHard, Memory: m}
		d, _ := h.Decide(context.Background(), in)
		if d.Say != "" && containsName(d.Say, "Вася") {
			hits++
		}
	}
	if hits < 15 {
		t.Fatalf("the bot returned a thrown card 30 times and mentioned it only %d times", hits)
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
