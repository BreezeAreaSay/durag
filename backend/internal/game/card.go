// Package game contains the authoritative rules engine of "Durag": a dynamic
// variant of the Russian card game Durak with unlimited throw-ins, a hidden
// trump, two Jokers, a Super card and per-player "stumps".
//
// Everything in this package is pure Go without any I/O so that the rules can
// be unit-tested exhaustively (see validator_test.go and engine_test.go).
package game

import "fmt"

// Suits. Special cards (Jokers, Super card) have SuitNone.
const (
	SuitHearts   = "Hearts"
	SuitDiamonds = "Diamonds"
	SuitClubs    = "Clubs"
	SuitSpades   = "Spades"
	SuitNone     = "None"
)

// Ranks. 2..10 are pips, 11 = Jack, 12 = Queen, 13 = King, 14 = Ace.
const (
	RankTwo   = 2
	RankJack  = 11
	RankQueen = 12
	RankKing  = 13
	RankAce   = 14
	RankJoker = 15 // both Jokers
	RankSuper = 16 // the Super card
)

// Identifiers of the three special cards.
const (
	RedJokerID   = "RJ"
	BlackJokerID = "BJ"
	SuperCardID  = "SC"
)

// Card is a single playing card. The ID is stable and unique within a deck:
// "H_10" (10 of Hearts), "S_14" (Ace of Spades), "RJ", "BJ", "SC".
type Card struct {
	ID   string `json:"id"`
	Suit string `json:"suit"`
	Rank int    `json:"rank"`
}

// BaseSuits lists the four regular suits in canonical order.
var BaseSuits = []string{SuitHearts, SuitDiamonds, SuitClubs, SuitSpades}

var suitLetters = map[string]string{
	SuitHearts:   "H",
	SuitDiamonds: "D",
	SuitClubs:    "C",
	SuitSpades:   "S",
}

// NewCard builds a regular (non-special) card and derives its ID.
func NewCard(suit string, rank int) Card {
	letter, ok := suitLetters[suit]
	if !ok {
		panic(fmt.Sprintf("game: NewCard called with unknown suit %q", suit))
	}
	if rank < RankTwo || rank > RankAce {
		panic(fmt.Sprintf("game: NewCard called with invalid rank %d", rank))
	}
	return Card{ID: fmt.Sprintf("%s_%d", letter, rank), Suit: suit, Rank: rank}
}

// Special cards.
var (
	RedJoker   = Card{ID: RedJokerID, Suit: SuitNone, Rank: RankJoker}
	BlackJoker = Card{ID: BlackJokerID, Suit: SuitNone, Rank: RankJoker}
	SuperCard  = Card{ID: SuperCardID, Suit: SuitNone, Rank: RankSuper}
)

// IsJoker reports whether the card is one of the two Jokers.
func (c Card) IsJoker() bool { return c.Rank == RankJoker }

// IsSuper reports whether the card is the Super card.
func (c Card) IsSuper() bool { return c.Rank == RankSuper }

// IsSpecial reports whether the card is a Joker or the Super card.
func (c Card) IsSpecial() bool { return c.Rank >= RankJoker }

// IsRed reports whether the card counts as "red": Hearts, Diamonds or the Red
// Joker.
func (c Card) IsRed() bool {
	return c.Suit == SuitHearts || c.Suit == SuitDiamonds || c.ID == RedJokerID
}

// IsBlack reports whether the card counts as "black": Clubs, Spades or the
// Black Joker.
func (c Card) IsBlack() bool {
	return c.Suit == SuitClubs || c.Suit == SuitSpades || c.ID == BlackJokerID
}

// String returns a short human readable label, e.g. "10♥", "RJ".
func (c Card) String() string {
	if c.IsSpecial() {
		return c.ID
	}
	symbols := map[string]string{SuitHearts: "♥", SuitDiamonds: "♦", SuitClubs: "♣", SuitSpades: "♠"}
	ranks := map[int]string{RankJack: "J", RankQueen: "Q", RankKing: "K", RankAce: "A"}
	r, ok := ranks[c.Rank]
	if !ok {
		r = fmt.Sprint(c.Rank)
	}
	return r + symbols[c.Suit]
}
