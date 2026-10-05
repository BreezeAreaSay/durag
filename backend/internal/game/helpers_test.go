package game

import (
	"encoding/json"
	"testing"
)

// cards builds a slice of cards from IDs ("H_7", "RJ", ...).
func cards(t *testing.T, ids ...string) []Card {
	t.Helper()
	out := make([]Card, 0, len(ids))
	for _, id := range ids {
		c, ok := CardByID(id)
		if !ok {
			t.Fatalf("bad card id %q", id)
		}
		out = append(out, c)
	}
	return out
}

func card(t *testing.T, id string) Card {
	t.Helper()
	return cards(t, id)[0]
}

// newPlaying starts a deterministic game with the given players.
func newPlaying(t *testing.T, ids ...string) (*Engine, *GameState) {
	t.Helper()
	e := NewEngine(NewRand(7, 11))
	s := NewGameState("room")
	for _, id := range ids {
		if err := e.Join(s, id, "Player "+id, ""); err != nil {
			t.Fatalf("join %s: %v", id, err)
		}
	}
	if err := e.Start(s); err != nil {
		t.Fatalf("start: %v", err)
	}
	return e, s
}

// setHand replaces a player's hand.
func setHand(t *testing.T, s *GameState, id string, ids ...string) {
	t.Helper()
	p := s.Player(id)
	if p == nil {
		t.Fatalf("no player %s", id)
	}
	p.Hand = cards(t, ids...)
}

// setStump replaces a player's stump.
func setStump(t *testing.T, s *GameState, id string, ids ...string) {
	t.Helper()
	s.Player(id).Stump = cards(t, ids...)
}

// setTrump installs a trump card (hidden unless revealed).
func setTrump(t *testing.T, s *GameState, id string, revealed bool) {
	t.Helper()
	c := card(t, id)
	s.TrumpCard = &c
	s.TrumpSuit = c.Suit
	s.TrumpRevealed = revealed
}

// setRoles sets the lead attacker and the defender and empties the table.
func setRoles(s *GameState, attacker, defender string) {
	s.CurrentTurn = attacker
	s.DefenderID = defender
	s.clearTable()
}

// countCards counts every card in the game and checks for duplicates.
func countCards(t *testing.T, s *GameState) int {
	t.Helper()
	seen := map[string]bool{}
	add := func(c Card) {
		if seen[c.ID] {
			t.Fatalf("duplicate card %s", c.ID)
		}
		seen[c.ID] = true
	}
	for _, p := range s.Players {
		for _, c := range p.Hand {
			add(c)
		}
		for _, c := range p.Stump {
			add(c)
		}
	}
	for _, c := range s.Deck {
		add(c)
	}
	if s.TrumpInDeck() { // once drawn the trump card is counted in a hand
		add(*s.TrumpCard)
	}
	for _, id := range s.TableOrder {
		add(card(t, id))
		for _, d := range s.TableCards[id] {
			add(d)
		}
	}
	return len(seen) + s.DiscardCount
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func wantErr(t *testing.T, got, want error) {
	t.Helper()
	if got != want {
		t.Fatalf("got error %v, want %v", got, want)
	}
}

// rebuildDeck makes a hand-crafted state consistent: hands, the trump and
// the table are authoritative; stumps are re-dealt (same sizes) from the
// unused cards and the deck becomes everything that is left.
func rebuildDeck(t *testing.T, s *GameState) {
	t.Helper()
	used := map[string]bool{}
	for _, p := range s.Players {
		for _, c := range p.Hand {
			used[c.ID] = true
		}
	}
	if s.TrumpInDeck() {
		used[s.TrumpCard.ID] = true
	}
	for _, id := range s.TableOrder {
		used[id] = true
		for _, d := range s.TableCards[id] {
			used[d.ID] = true
		}
	}
	pool := []Card{}
	for _, c := range NewDeck() {
		if !used[c.ID] {
			pool = append(pool, c)
		}
	}
	for i := range s.Players {
		n := len(s.Players[i].Stump)
		s.Players[i].Stump = append([]Card{}, pool[:n]...)
		pool = pool[n:]
	}
	s.Deck = pool
	s.DiscardCount = 0
}
