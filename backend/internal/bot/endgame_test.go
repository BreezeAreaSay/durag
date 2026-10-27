package bot

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// endgamePosition: the deck is gone, the trump is spades, the bot leads.
// Bot: 6♥ A♥. Opponent: 7♥ 10♥. Leading the 6 gets beaten by the 7, then the
// 10 comes and the ace covers it: both empty together, a draw at best.
// Leading the ace forces a take; the 6 then gets beaten by the 7 and the
// bot is out first with two cards still in the other hand: a win.
func endgamePosition(t *testing.T) Context {
	t.Helper()
	s := game.NewGameState("END")
	s.Status = game.StatusPlaying
	s.MaxPlayers = 4
	s.Players = []game.Player{{ID: "b", Name: "Крест", IsBot: true, Connected: true}, {ID: "h", Name: "Вася", Connected: true}}
	give := func(i int, ids ...string) {
		for _, id := range ids {
			c, _ := game.CardByID(id)
			s.Players[i].Hand = append(s.Players[i].Hand, c)
		}
	}
	give(0, "H_6", "H_14")
	give(1, "H_7", "H_10")
	for i := range s.Players {
		s.Players[i].Stump = []game.Card{}
	}
	s.CurrentTurn, s.DefenderID = "b", "h"
	s.TableCards = map[string][]game.Card{}
	s.Deck = []game.Card{}
	trump, _ := game.CardByID("S_2")
	s.TrumpCard = &trump
	s.TrumpSuit = game.SuitSpades
	s.TrumpRevealed = true
	s.TrumpDrawnBy = "h"
	m := NewMemory()
	// the memory has seen every other card go to the discard except the two in the opponent's hand
	for _, c := range game.NewDeck() {
		switch c.ID {
		case "H_6", "H_14", "H_7", "H_10", "S_2":
			continue
		}
		m.seen[c.ID] = true
	}
	m.seen["S_2"] = true
	view := s.Sanitized("b")
	return Context{State: view, BotID: "b", Moves: s.LegalMoves("b"), Level: LevelHard, Memory: m, Plan: PlanCount}
}

func TestEndgameSearchFindsTheForcedWin(t *testing.T) {
	in := endgamePosition(t)
	h := NewHeuristic(rand.New(rand.NewPCG(1, 2)))
	if m, ok := h.endgame(in); !ok || m.CardID != "H_14" {
		t.Fatalf("search: ok=%v move=%s", ok, m)
	}
	d, err := h.Decide(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if d.Move.Type != game.MoveAttack || d.Move.CardID != "H_14" {
		t.Fatalf("the search must lead the ace to force a take, chose %s", d.Move)
	}
}

func TestEndgameSearchSkipsWhenTheDeckIsAlive(t *testing.T) {
	in := endgamePosition(t)
	in.State.DeckCount = 3
	h := NewHeuristic(rand.New(rand.NewPCG(1, 2)))
	if _, ok := h.endgame(in); ok {
		t.Fatal("no search while cards are still drawn")
	}
}
