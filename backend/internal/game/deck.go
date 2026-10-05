package game

import "math/rand/v2"

// DeckSize is the total number of cards: 52 base cards + 2 Jokers + 1 Super card.
const DeckSize = 55

// NewDeck returns all 55 cards in canonical (unshuffled) order.
func NewDeck() []Card {
	deck := make([]Card, 0, DeckSize)
	for _, suit := range BaseSuits {
		for rank := RankTwo; rank <= RankAce; rank++ {
			deck = append(deck, NewCard(suit, rank))
		}
	}
	deck = append(deck, RedJoker, BlackJoker, SuperCard)
	return deck
}

// Shuffle shuffles cards in place using the supplied random source.
func Shuffle(cards []Card, rng *rand.Rand) {
	rng.Shuffle(len(cards), func(i, j int) { cards[i], cards[j] = cards[j], cards[i] })
}

// NewRand returns a random source seeded from two 64-bit values. Tests use
// fixed seeds to make deals reproducible.
func NewRand(seed1, seed2 uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed1, seed2))
}
