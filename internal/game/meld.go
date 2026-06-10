package game

import (
	"errors"
	"sort"
)

// MeldType identifies the kind of meld.
type MeldType int

const (
	MeldPierna   MeldType = iota // three-of-a-kind (different suits on creation)
	MeldEscalera                 // same-suit run
)

// Meld is a set of cards laid on the table.
type Meld struct {
	Type  MeldType
	Cards []Card
	// OwnerID is the player who created the meld.
	OwnerID string
}

// ─── Validation helpers ───────────────────────────────────────────────────────

// ValidatePierna checks whether cards form a valid new pierna.
// Rules: exactly 3 cards, same rank, all DIFFERENT suits, no jokers.
func ValidatePierna(cards []Card) error {
	if len(cards) != 3 {
		return errors.New("pierna requires exactly 3 cards")
	}
	rank := cards[0].Rank
	if rank == Joker {
		return errors.New("jokers are not allowed in a pierna")
	}
	suits := make(map[Suit]bool)
	for _, c := range cards {
		if c.Rank == Joker {
			return errors.New("jokers are not allowed in a pierna")
		}
		if c.Rank != rank {
			return errors.New("all cards in a pierna must share the same rank")
		}
		if suits[c.Suit] {
			return errors.New("all cards in a pierna must have different suits")
		}
		suits[c.Suit] = true
	}
	return nil
}

// ValidateEscalera checks whether cards form a valid new escalera.
// Rules: 3+ cards, same suit, consecutive ranks. Ace may be low (A-2-3) or
// high (Q-K-A). No wrap-around. Max 1 joker, fixed to a position.
func ValidateEscalera(cards []Card) error {
	if len(cards) < 3 {
		return errors.New("escalera requires at least 3 cards")
	}

	jokerCount := 0
	for _, c := range cards {
		if c.IsJoker() {
			jokerCount++
		}
	}
	if jokerCount > 1 {
		return errors.New("escalera allows at most 1 joker")
	}

	return validateEscaleraSequence(cards)
}

// validateEscaleraSequence is the core run-check used for both creation and
// lay-off. It verifies suit consistency and consecutiveness.
func validateEscaleraSequence(cards []Card) error {
	if len(cards) == 0 {
		return errors.New("empty escalera")
	}

	// Identify the suit from the first non-joker card.
	suit := NoSuit
	for _, c := range cards {
		if !c.IsJoker() {
			suit = c.Suit
			break
		}
	}
	if suit == NoSuit {
		return errors.New("escalera cannot consist entirely of jokers")
	}

	// All non-joker cards must share the same suit.
	for _, c := range cards {
		if !c.IsJoker() && c.Suit != suit {
			return errors.New("all non-joker cards in an escalera must share the same suit")
		}
	}

	// Build a sorted rank list (replace joker with -1 placeholder for now).
	// We try both ace-low and ace-high interpretations.
	ranks := make([]int, len(cards))
	jokerIdx := -1
	for i, c := range cards {
		if c.IsJoker() {
			jokerIdx = i
			ranks[i] = -1
		} else {
			ranks[i] = int(c.Rank)
		}
	}

	if jokerIdx == -1 {
		// No joker — just check consecutiveness.
		return checkConsecutive(ranks, false)
	}

	// With a joker: try filling it at each possible gap.
	return checkConsecutiveWithJoker(ranks, jokerIdx)
}

// checkConsecutive verifies that a sorted list of ranks is consecutive.
// If tryAceHigh is true, Ace (1) is treated as 14.
func checkConsecutive(ranks []int, tryAceHigh bool) error {
	r := make([]int, len(ranks))
	copy(r, ranks)
	if tryAceHigh {
		for i, v := range r {
			if v == 1 {
				r[i] = 14
			}
		}
	}
	sort.Ints(r)

	for i := 1; i < len(r); i++ {
		if r[i] != r[i-1]+1 {
			if !tryAceHigh {
				return checkConsecutive(ranks, true)
			}
			return errors.New("escalera cards are not consecutive")
		}
	}
	return nil
}

// checkConsecutiveWithJoker tries each possible rank for the joker slot.
func checkConsecutiveWithJoker(ranks []int, jokerIdx int) error {
	nonJokerRanks := make([]int, 0, len(ranks)-1)
	for i, r := range ranks {
		if i != jokerIdx {
			nonJokerRanks = append(nonJokerRanks, r)
		}
	}

	// Try Ace-low, then Ace-high.
	for _, aceHigh := range []bool{false, true} {
		adjusted := make([]int, len(nonJokerRanks))
		copy(adjusted, nonJokerRanks)
		if aceHigh {
			for i, v := range adjusted {
				if v == 1 {
					adjusted[i] = 14
				}
			}
		}
		sort.Ints(adjusted)

		// Find which gap the joker can fill.
		filled := fillJokerGap(adjusted)
		if filled {
			return nil
		}
	}
	return errors.New("joker cannot complete a consecutive run in this escalera")
}

// fillJokerGap returns true if exactly one gap of exactly 1 exists in the sorted list,
// or if the joker can extend at either end.
func fillJokerGap(sorted []int) bool {
	// Check internal gap.
	gaps := 0
	for i := 1; i < len(sorted); i++ {
		diff := sorted[i] - sorted[i-1]
		if diff == 2 {
			gaps++
		} else if diff != 1 {
			return false // gap too large or duplicate
		}
	}
	if gaps == 1 {
		return true
	}
	if gaps == 0 {
		// Joker extends at one end.
		low := sorted[0]
		high := sorted[len(sorted)-1]
		// Can extend low (if low > 1 or ace-low already handled) or high (if high < 13 or ace-high).
		return low > 1 || high < 13 || high == 13 // high==13 allows ace-high extension but we've already adjusted
	}
	return false
}

// ─── Discard-pickup usability check ──────────────────────────────────────────

// CanUsePickedCard reports whether card can be immediately used (melded or laid
// off) given the current hand (without the card itself) and existing table melds.
// It tries:
//  1. Forming a new pierna with cards from hand.
//  2. Forming a new escalera with cards from hand.
//  3. Laying off onto any existing meld.
func CanUsePickedCard(card Card, hand Hand, melds []Meld) bool {
	// Check lay-off onto existing melds (fast path, no enumeration needed).
	for i := range melds {
		m := &melds[i]
		switch m.Type {
		case MeldPierna:
			if CanLayOffPierna(m, card) == nil {
				return true
			}
		case MeldEscalera:
			if CanLayOffEscalera(m, card) == nil {
				return true
			}
		}
	}

	// Try to form a new pierna: need 2 more cards of the same rank in hand
	// (all different suits from each other and from the picked card).
	if !card.IsJoker() {
		if canFormPiernaWith(card, hand) {
			return true
		}
	}

	// Try to form a new escalera: need 2+ cards of the same suit and consecutive.
	if canFormEscaleraWith(card, hand) {
		return true
	}

	return false
}

// canFormPiernaWith checks if card + two cards from hand form a valid pierna.
func canFormPiernaWith(card Card, hand Hand) bool {
	// Collect hand cards with same rank.
	sameRank := make([]Card, 0, len(hand))
	for _, hc := range hand {
		if hc.Rank == card.Rank && !hc.Equal(card) {
			sameRank = append(sameRank, hc)
		}
	}
	if len(sameRank) < 2 {
		return false
	}
	// Try every pair from sameRank.
	for i := 0; i < len(sameRank); i++ {
		for j := i + 1; j < len(sameRank); j++ {
			trial := []Card{card, sameRank[i], sameRank[j]}
			if ValidatePierna(trial) == nil {
				return true
			}
		}
	}
	return false
}

// canFormEscaleraWith checks if card + cards from hand form a valid escalera.
func canFormEscaleraWith(card Card, hand Hand) bool {
	// Build candidate pool: the card plus hand cards of matching suit (or jokers).
	sameSuit := []Card{card}
	for _, hc := range hand {
		if hc.Equal(card) {
			continue
		}
		if hc.IsJoker() || hc.Suit == card.Suit || card.IsJoker() {
			sameSuit = append(sameSuit, hc)
		}
	}
	if len(sameSuit) < 3 {
		return false
	}
	// Try all combinations of length 3..N that include card at index 0.
	return tryEscaleraCombos(card, sameSuit[1:])
}

// tryEscaleraCombos tries all combinations of n cards (n=2..len(pool)) from pool,
// combined with fixed, returning true if any set forms a valid escalera.
func tryEscaleraCombos(fixed Card, pool []Card) bool {
	// Try combination sizes: fixed + n pool cards → n+1 total (need ≥ 3, so n ≥ 2).
	for n := 2; n <= len(pool); n++ {
		combo := make([]Card, n)
		if tryEscaleraCombosN(fixed, pool, combo, 0, 0, n) {
			return true
		}
	}
	return false
}

func tryEscaleraCombosN(fixed Card, pool, combo []Card, start, pos, need int) bool {
	if pos == need {
		trial := make([]Card, 0, need+1)
		trial = append(trial, fixed)
		trial = append(trial, combo...)
		return ValidateEscalera(trial) == nil
	}
	for i := start; i <= len(pool)-need+pos; i++ {
		combo[pos] = pool[i]
		if tryEscaleraCombosN(fixed, pool, combo, i+1, pos+1, need) {
			return true
		}
	}
	return false
}

// ─── Lay-off ──────────────────────────────────────────────────────────────────

// CanLayOffPierna checks if a card can be added to an existing pierna.
// After creation, any card of the same rank may be added regardless of suit.
func CanLayOffPierna(meld *Meld, card Card) error {
	if meld.Type != MeldPierna {
		return errors.New("meld is not a pierna")
	}
	if card.IsJoker() {
		return errors.New("jokers cannot be added to a pierna")
	}
	if len(meld.Cards) == 0 {
		return errors.New("empty pierna")
	}
	existing := meld.Cards[0].Rank
	if card.Rank != existing {
		return errors.New("card rank does not match this pierna")
	}
	// Limit: max 8 cards (two decks, 4 suits × 2)
	if len(meld.Cards) >= 8 {
		return errors.New("pierna is full")
	}
	return nil
}

// CanLayOffEscalera checks if a card can be added to either end of an escalera.
// Joker rules: already placed joker is fixed; a new joker can be added only if
// the escalera has no joker yet.
func CanLayOffEscalera(meld *Meld, card Card) error {
	if meld.Type != MeldEscalera {
		return errors.New("meld is not an escalera")
	}

	// Build trial cards at low end and high end.
	lowTrial := append([]Card{card}, meld.Cards...)
	highTrial := append(append([]Card{}, meld.Cards...), card)

	if validateEscaleraSequence(lowTrial) == nil {
		return nil
	}
	if validateEscaleraSequence(highTrial) == nil {
		return nil
	}
	return errors.New("card cannot extend this escalera")
}

// LayOffEscalera adds a card to the correct end of an escalera in-place.
// Placement is determined by comparing the card's effective rank against the
// meld's boundary ranks, not by re-running sequence validation (which sorts
// ranks internally and cannot distinguish high from low placement).
func LayOffEscalera(meld *Meld, card Card) {
	// Determine the effective low and high boundary ranks of the existing meld.
	// Use ace-high (rank 14) when the meld ends with an ace or a joker
	// placeholder that sits after a King.
	lowBound, highBound := meldBoundaryRanks(meld)

	cardRank := effectiveRank(card, lowBound, highBound)

	if cardRank <= lowBound {
		// Card extends the low end.
		meld.Cards = append([]Card{card}, meld.Cards...)
	} else {
		// Card extends the high end.
		meld.Cards = append(meld.Cards, card)
	}
}

// meldBoundaryRanks returns the effective low and high rank of the meld's
// boundary cards. Jokers are treated as the rank needed to make the sequence
// valid (i.e. one step outside the interior non-joker range). Ace is treated
// as 14 when the meld contains a King (ace-high context).
func meldBoundaryRanks(meld *Meld) (low, high int) {
	// Collect non-joker ranks.
	nonJoker := make([]int, 0, len(meld.Cards))
	for _, c := range meld.Cards {
		if !c.IsJoker() {
			nonJoker = append(nonJoker, int(c.Rank))
		}
	}
	if len(nonJoker) == 0 {
		return 1, 13
	}

	// Determine if we're in ace-high context (meld contains a King).
	aceHigh := false
	for _, r := range nonJoker {
		if r == int(King) {
			aceHigh = true
			break
		}
	}

	// Adjust ace if ace-high.
	adjusted := make([]int, len(nonJoker))
	copy(adjusted, nonJoker)
	if aceHigh {
		for i, r := range adjusted {
			if r == int(Ace) {
				adjusted[i] = 14
			}
		}
	}

	minR, maxR := adjusted[0], adjusted[0]
	for _, r := range adjusted[1:] {
		if r < minR {
			minR = r
		}
		if r > maxR {
			maxR = r
		}
	}

	// Account for jokers at the boundaries: if the meld has a joker at the
	// low end, the effective low boundary is one below minR; similarly for high.
	jokerAtLow := meld.Cards[0].IsJoker()
	jokerAtHigh := meld.Cards[len(meld.Cards)-1].IsJoker()
	if jokerAtLow {
		minR--
	}
	if jokerAtHigh {
		maxR++
	}

	return minR, maxR
}

// effectiveRank returns the card's rank adjusted for ace-high/low context.
// A joker placed as a lay-off card is treated as extending the nearest end.
func effectiveRank(card Card, lowBound, highBound int) int {
	if card.IsJoker() {
		// A joker can extend either end; prefer the end that needs it less.
		// Since CanLayOffEscalera already validated the move, just pick the end.
		// We extend the high end by default (the caller checks the low-end case).
		return highBound + 1
	}
	r := int(card.Rank)
	// Ace: determine whether to treat as 1 or 14 based on the meld context.
	if r == int(Ace) {
		// Use ace-high (14) when the meld's high boundary is ≥ 13 (King present).
		if highBound >= 13 {
			return 14
		}
		// Use ace-low (1) when the meld's low boundary is ≤ 3 (ace-low context).
		return 1
	}
	return r
}
