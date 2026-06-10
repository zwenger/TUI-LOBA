package game

import "math/rand"

// newDeck returns a fresh 108-card Loba deck (two French 52-card decks + 4 jokers).
func newDeck() []Card {
	deck := make([]Card, 0, 108)

	suits := []Suit{Spades, Hearts, Diamonds, Clubs}
	for pass := 0; pass < 2; pass++ {
		for _, suit := range suits {
			for rank := Ace; rank <= King; rank++ {
				deck = append(deck, Card{Rank: rank, Suit: suit})
			}
		}
	}

	for i := 0; i < 4; i++ {
		deck = append(deck, Card{Rank: Joker, Suit: NoSuit, JokerIndex: i})
	}

	return deck
}

// shuffle shuffles a deck in-place using a provided random source.
func shuffle(deck []Card, rng *rand.Rand) {
	rng.Shuffle(len(deck), func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})
}
