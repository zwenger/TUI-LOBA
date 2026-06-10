// Package game implements the pure Loba card game engine (no I/O).
package game

import "fmt"

// Suit represents a playing card suit.
type Suit int

const (
	Spades   Suit = iota // ♠
	Hearts               // ♥
	Diamonds             // ♦
	Clubs                // ♣
	NoSuit               // used for jokers
)

// String returns the single-rune suit symbol.
func (s Suit) String() string {
	switch s {
	case Spades:
		return "♠"
	case Hearts:
		return "♥"
	case Diamonds:
		return "♦"
	case Clubs:
		return "♣"
	default:
		return "★"
	}
}

// IsRed reports whether the suit is red.
func (s Suit) IsRed() bool {
	return s == Hearts || s == Diamonds
}

// Rank represents a card rank (1=Ace … 13=King, 0=Joker).
type Rank int

const (
	Joker Rank = 0
	Ace   Rank = 1
	Two   Rank = 2
	Three Rank = 3
	Four  Rank = 4
	Five  Rank = 5
	Six   Rank = 6
	Seven Rank = 7
	Eight Rank = 8
	Nine  Rank = 9
	Ten   Rank = 10
	Jack  Rank = 11
	Queen Rank = 12
	King  Rank = 13
)

// String returns the short rank label.
func (r Rank) String() string {
	switch r {
	case Joker:
		return "★"
	case Ace:
		return " A"
	case Jack:
		return " J"
	case Queen:
		return " Q"
	case King:
		return " K"
	default:
		return fmt.Sprintf("%2d", int(r))
	}
}

// Score returns the point value of a rank when left in hand.
func (r Rank) Score() int {
	switch {
	case r == Joker:
		return 25
	case r == Ace:
		return 15
	case r >= Jack:
		return 10
	default:
		return int(r)
	}
}

// Card is a single playing card.
type Card struct {
	Rank Rank
	Suit Suit
	// JokerIndex uniquely identifies a joker (0–3) within the deck,
	// so two jokers are never mistaken for the same card.
	JokerIndex int
}

// IsJoker reports whether the card is a joker.
func (c Card) IsJoker() bool {
	return c.Rank == Joker
}

// Score returns the card's penalty score when held in hand at round end.
func (c Card) Score() int {
	return c.Rank.Score()
}

// String returns a short human-readable card label.
func (c Card) String() string {
	if c.IsJoker() {
		return "★JKR"
	}
	return fmt.Sprintf("%s%s", c.Rank.String(), c.Suit.String())
}

// Equal reports whether two cards are identical (same rank, suit, and joker index).
func (c Card) Equal(other Card) bool {
	return c.Rank == other.Rank && c.Suit == other.Suit && c.JokerIndex == other.JokerIndex
}

// Hand is an ordered collection of cards held by a player.
type Hand []Card

// Remove removes the card at index i from the hand.
func (h *Hand) Remove(i int) Card {
	removed := (*h)[i]
	*h = append((*h)[:i], (*h)[i+1:]...)
	return removed
}

// Add appends a card to the hand.
func (h *Hand) Add(c Card) {
	*h = append(*h, c)
}

// Score sums the penalty score of all cards remaining in the hand.
func (h Hand) Score() int {
	total := 0
	for _, c := range h {
		total += c.Score()
	}
	return total
}

// FindIndex returns the first index of a card matching c, or -1.
func (h Hand) FindIndex(c Card) int {
	for i, card := range h {
		if card.Equal(c) {
			return i
		}
	}
	return -1
}
