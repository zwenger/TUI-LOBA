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
		return errors.New("una pierna requiere exactamente 3 cartas")
	}
	rank := cards[0].Rank
	if rank == Joker {
		return errors.New("los comodines no están permitidos en una pierna")
	}
	suits := make(map[Suit]bool)
	for _, c := range cards {
		if c.Rank == Joker {
			return errors.New("los comodines no están permitidos en una pierna")
		}
		if c.Rank != rank {
			return errors.New("todas las cartas de una pierna deben tener el mismo valor")
		}
		if suits[c.Suit] {
			return errors.New("todas las cartas de una pierna deben tener palos diferentes")
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
		return errors.New("una escalera requiere al menos 3 cartas")
	}

	jokerCount := 0
	for _, c := range cards {
		if c.IsJoker() {
			jokerCount++
		}
	}
	if jokerCount > 1 {
		return errors.New("una escalera admite como máximo 1 comodín")
	}

	return validateEscaleraSequence(cards)
}

// validateEscaleraSequence is the core run-check used for both creation and
// lay-off. It verifies suit consistency and consecutiveness.
func validateEscaleraSequence(cards []Card) error {
	if len(cards) == 0 {
		return errors.New("escalera vacía")
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
		return errors.New("una escalera no puede consistir únicamente en comodines")
	}

	// All non-joker cards must share the same suit.
	for _, c := range cards {
		if !c.IsJoker() && c.Suit != suit {
			return errors.New("todas las cartas de una escalera deben ser del mismo palo")
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

// SortEscaleraCards returns a new slice of escalera cards sorted in sequence
// order (low-to-high), with the joker placed at its represented position.
// The input must already be validated as a valid escalera (ValidateEscalera ok).
// Pierna cards are returned as-is (sorted by suit for visual consistency).
func SortEscaleraCards(cards []Card) []Card {
	if len(cards) == 0 {
		return cards
	}

	// Separate joker from non-jokers.
	var jokerCard *Card
	nonJokers := make([]Card, 0, len(cards))
	for i := range cards {
		if cards[i].IsJoker() {
			jokerCard = &cards[i]
		} else {
			nonJokers = append(nonJokers, cards[i])
		}
	}

	if jokerCard == nil {
		// No joker: sort non-jokers by rank (ace-low by default; handle ace-high).
		return sortNonJokers(nonJokers)
	}

	// Sort non-jokers first.
	sorted := sortNonJokers(nonJokers)

	// Find where the joker belongs: it must fill the one gap.
	// Determine ace-high context.
	aceHigh := false
	for _, c := range sorted {
		if c.Rank == King {
			aceHigh = true
			break
		}
	}

	ranks := make([]int, len(sorted))
	for i, c := range sorted {
		ranks[i] = int(c.Rank)
		if aceHigh && ranks[i] == 1 {
			ranks[i] = 14
		}
	}

	// Find the joker's position in the sorted rank list.
	result := make([]Card, 0, len(cards))
	placed := false
	for i := range ranks {
		if !placed && i > 0 && ranks[i]-ranks[i-1] == 2 {
			// Internal gap: joker goes between i-1 and i.
			result = append(result, *jokerCard)
			placed = true
		}
		result = append(result, sorted[i])
	}
	if !placed {
		// Joker extends an end. Determine which end.
		lowRank := ranks[0]
		highRank := ranks[len(ranks)-1]
		// canGoLow: joker represents lowRank-1 (must be ≥ 1)
		canGoLow := lowRank > 1
		// canGoHigh: joker represents highRank+1 (must be ≤ 13, or 14 for ace-high)
		maxHigh := 13
		if aceHigh {
			maxHigh = 14
		}
		canGoHigh := highRank < maxHigh

		switch {
		case canGoLow && !canGoHigh:
			// Only low end is possible (high already at King/Ace-high boundary).
			result = append([]Card{*jokerCard}, result...)
		case !canGoLow && canGoHigh:
			// Only high end is possible (low already at Ace, can't go below).
			result = append(result, *jokerCard)
		case aceHigh && highRank == 13:
			// High end: joker represents Ace-high (rank 14) in a run ending at King.
			result = append(result, *jokerCard)
		case highRank+1 == 13:
			// High end: joker represents King — a natural boundary card.
			// Covers J-Q-Joker and similar runs ending just before King.
			result = append(result, *jokerCard)
		default:
			// Both ends possible, no special boundary: place at low end.
			result = append([]Card{*jokerCard}, result...)
		}
	}

	return result
}

// sortNonJokers sorts cards by rank (ace-low by default; ace treated as 14 when
// a King is present in the set).
func sortNonJokers(cards []Card) []Card {
	if len(cards) == 0 {
		return cards
	}
	result := make([]Card, len(cards))
	copy(result, cards)

	// Check for ace-high context.
	aceHigh := false
	for _, c := range result {
		if c.Rank == King {
			aceHigh = true
			break
		}
	}

	sort.Slice(result, func(i, j int) bool {
		ri, rj := int(result[i].Rank), int(result[j].Rank)
		if aceHigh {
			if ri == 1 {
				ri = 14
			}
			if rj == 1 {
				rj = 14
			}
		}
		return ri < rj
	})
	return result
}

// SortPiernaCards returns a new slice of pierna cards sorted by suit.
func SortPiernaCards(cards []Card) []Card {
	result := make([]Card, len(cards))
	copy(result, cards)
	sort.Slice(result, func(i, j int) bool {
		return int(result[i].Suit) < int(result[j].Suit)
	})
	return result
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
			return errors.New("las cartas de la escalera no son consecutivas")
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
	return errors.New("el comodín no puede completar una secuencia válida en esta escalera")
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

// CanUsePickedCard reports whether card can be immediately used in a new meld
// formed from the current hand (without the card itself).
// It tries:
//  1. Forming a new pierna with cards from hand.
//  2. Forming a new escalera with cards from hand.
func CanUsePickedCard(card Card, hand Hand, melds []Meld) bool {
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
		return errors.New("la combinación no es una pierna")
	}
	if card.IsJoker() {
		return errors.New("los comodines no se pueden agregar a una pierna")
	}
	if len(meld.Cards) == 0 {
		return errors.New("pierna vacía")
	}
	existing := meld.Cards[0].Rank
	if card.Rank != existing {
		return errors.New("el valor de la carta no coincide con esta pierna")
	}
	// Limit: max 8 cards (two decks, 4 suits × 2)
	if len(meld.Cards) >= 8 {
		return errors.New("la pierna está completa")
	}
	return nil
}

// CanLayOffEscalera checks if a card can be added to either end of an escalera.
// Joker rules: already placed joker is fixed; a new joker can be added only if
// the escalera has no joker yet.
func CanLayOffEscalera(meld *Meld, card Card) error {
	if meld.Type != MeldEscalera {
		return errors.New("la combinación no es una escalera")
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
	return errors.New("la carta no puede extender esta escalera")
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
