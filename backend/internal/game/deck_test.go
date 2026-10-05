package game

import "testing"

func TestNewDeckHas55UniqueCards(t *testing.T) {
	deck := NewDeck()
	if len(deck) != DeckSize {
		t.Fatalf("deck size = %d, want %d", len(deck), DeckSize)
	}
	seen := map[string]bool{}
	base, jokers, supers := 0, 0, 0
	for _, c := range deck {
		if seen[c.ID] {
			t.Fatalf("duplicate card id %s", c.ID)
		}
		seen[c.ID] = true
		switch {
		case c.IsSuper():
			supers++
		case c.IsJoker():
			jokers++
		default:
			base++
			if c.Rank < RankTwo || c.Rank > RankAce {
				t.Fatalf("bad rank %d for %s", c.Rank, c.ID)
			}
			if c.Suit == SuitNone {
				t.Fatalf("base card %s has no suit", c.ID)
			}
		}
	}
	if base != 52 || jokers != 2 || supers != 1 {
		t.Fatalf("base=%d jokers=%d supers=%d", base, jokers, supers)
	}
	for _, suit := range BaseSuits {
		n := 0
		for _, c := range deck {
			if c.Suit == suit {
				n++
			}
		}
		if n != 13 {
			t.Fatalf("suit %s has %d cards", suit, n)
		}
	}
}

func TestShuffleIsDeterministicForSeed(t *testing.T) {
	a := NewDeck()
	b := NewDeck()
	Shuffle(a, NewRand(1, 2))
	Shuffle(b, NewRand(1, 2))
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("shuffles differ at %d", i)
		}
	}
	c := NewDeck()
	Shuffle(c, NewRand(3, 4))
	same := true
	for i := range a {
		if a[i] != c[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("different seeds produced identical shuffles")
	}
}

func TestCardByIDRoundTrip(t *testing.T) {
	for _, c := range NewDeck() {
		got, ok := CardByID(c.ID)
		if !ok || got != c {
			t.Fatalf("CardByID(%s) = %v, %v", c.ID, got, ok)
		}
	}
	for _, bad := range []string{"", "X_2", "H_1", "H_15", "H2", "H_", "H_x", "ZZ"} {
		if _, ok := CardByID(bad); ok {
			t.Fatalf("CardByID(%q) unexpectedly ok", bad)
		}
	}
}

func TestCardColours(t *testing.T) {
	if !card(t, "H_2").IsRed() || !card(t, "D_14").IsRed() || !RedJoker.IsRed() {
		t.Fatal("red cards not red")
	}
	if !card(t, "S_2").IsBlack() || !card(t, "C_14").IsBlack() || !BlackJoker.IsBlack() {
		t.Fatal("black cards not black")
	}
	if SuperCard.IsRed() || SuperCard.IsBlack() {
		t.Fatal("super card has a colour")
	}
	if card(t, "H_10").String() != "10♥" || card(t, "S_14").String() != "A♠" || SuperCard.String() != "SC" {
		t.Fatal("bad String()")
	}
}
