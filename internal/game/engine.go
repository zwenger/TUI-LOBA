package game

import (
	"errors"
	"fmt"
	"math/rand"
)

// Phase represents the current sub-phase of a player's turn.
type Phase int

const (
	PhaseDrawing  Phase = iota // waiting for the active player to draw
	PhaseMelding               // player has drawn; may meld/lay-off, must discard
	PhaseRoundEnd              // round over, scoring done
	PhaseGameOver              // cumulative score > 101 limit reached
)

// String returns a human-readable phase label.
func (p Phase) String() string {
	switch p {
	case PhaseDrawing:
		return "draw"
	case PhaseMelding:
		return "meld"
	case PhaseRoundEnd:
		return "round_end"
	case PhaseGameOver:
		return "game_over"
	default:
		return "unknown"
	}
}

const (
	// MaxScore is the elimination threshold. When any player's cumulative total
	// exceeds this, the game ends and the LOWEST total wins.
	MaxScore = 101

	// HandSize is the number of cards dealt to each player at round start.
	HandSize = 9
)

// Player holds all per-player state.
type Player struct {
	ID            string
	Name          string
	Hand          Hand
	TotalScore    int  // cumulative across rounds
	RoundScore    int  // penalty for the most recently completed round
	HasMelded     bool // true once the player has successfully laid a meld this round
	Connected     bool
	MeldedIndexes []int // indexes into Game.Melds that this player created (informational)

	// PickedUpDiscard tracks the card taken from the discard pile this turn.
	// It must be used in a meld or lay-off before the player may discard.
	// Nil when the player drew from stock.
	PickedUpDiscard *Card
}

// PlayerRoundResult holds the reveal data for one player at round end.
// It is captured before hands are cleared so the snapshot can show everyone's
// remaining cards in the round-summary and game-over screens.
type PlayerRoundResult struct {
	PlayerID   string
	PlayerName string
	Hand       Hand // cards still held at round end (empty for the round winner)
	RoundScore int  // penalty for this round (0 for the round winner)
	TotalScore int  // cumulative total after this round
}

// RoundResult is captured once per round at the moment endRound fires.
// It is the authoritative source for the round-summary reveal.
type RoundResult struct {
	Round   int
	Results []PlayerRoundResult
}

// RoundScores holds the per-player scores for a single completed round.
// It is appended to ScoreHistory in the Game when endRound runs.
type RoundScores struct {
	Round  int
	// Scores maps player ID → points earned that round (0 = round winner).
	Scores map[string]int
	// Names maps player ID → display name (snapshot at round end; stable across rounds).
	Names  map[string]string
}

// maxEventLog is the cap for the game-level event log. The log grows without
// bound during a round (startRound clears it), but on reconnect we only send
// the tail (see EventLogTail). Keeping a hard cap here avoids unbounded growth
// if a very long round with many chat messages occurs.
const maxEventLog = 500

// Game is the authoritative server-side game state.
type Game struct {
	Players         []*Player
	Melds           []Meld
	Stock           []Card
	DiscardPile     []Card // top of pile is last element
	Phase           Phase
	ActiveIndex     int // index into Players of whose turn it is
	Round           int
	Events          []string // per-round event log; cleared at startRound
	// FullEventLog is the lifetime event log across all rounds, capped at
	// maxEventLog. The server uses this to send a backlog to reconnecting clients
	// so they have context even if the current-round Events slice was just cleared.
	FullEventLog    []string
	ScoreHistory    []RoundScores // one entry per completed round, oldest first
	LastRoundResult *RoundResult  // reveal data for the most recently completed round
	rng             *rand.Rand
}

// EventLogTail returns the last n entries from FullEventLog. If n <= 0 all
// entries are returned. The returned slice is a copy; callers may mutate it.
func (g *Game) EventLogTail(n int) []string {
	src := g.FullEventLog
	if n <= 0 || n >= len(src) {
		result := make([]string, len(src))
		copy(result, src)
		return result
	}
	result := make([]string, n)
	copy(result, src[len(src)-n:])
	return result
}

// NewGame creates and shuffles a new game for the given player IDs/names.
// Callers must provide 2–6 players.
func NewGame(players []struct{ ID, Name string }, seed int64) (*Game, error) {
	if len(players) < 2 || len(players) > 6 {
		return nil, errors.New("loba requiere entre 2 y 6 jugadores")
	}

	g := &Game{
		rng:   rand.New(rand.NewSource(seed)),
		Round: 1,
	}
	for _, p := range players {
		g.Players = append(g.Players, &Player{
			ID:        p.ID,
			Name:      p.Name,
			Connected: true,
		})
	}

	g.startRound()
	return g, nil
}

// startRound deals cards and resets per-round state.
func (g *Game) startRound() {
	deck := newDeck()
	shuffle(deck, g.rng)

	for _, p := range g.Players {
		p.Hand = make(Hand, 0, HandSize)
		p.HasMelded = false
		p.RoundScore = 0
		p.PickedUpDiscard = nil
	}
	g.Melds = nil
	g.Events = nil
	// NOTE: g.FullEventLog and g.ScoreHistory are NOT cleared here — they
	// accumulate across rounds so clients can view the full game history.

	// Deal hands.
	cursor := 0
	for i := 0; i < HandSize; i++ {
		for _, p := range g.Players {
			p.Hand.Add(deck[cursor])
			cursor++
		}
	}

	// Flip one card to start the discard pile.
	g.DiscardPile = []Card{deck[cursor]}
	cursor++

	// Remainder is the stock.
	g.Stock = deck[cursor:]

	g.Phase = PhaseDrawing
	g.addEvent(fmt.Sprintf("Ronda %d iniciada. %s comienza.", g.Round, g.activePlayer().Name))
}

// ─── Accessors ────────────────────────────────────────────────────────────────

func (g *Game) activePlayer() *Player {
	return g.Players[g.ActiveIndex]
}

// PlayerByID returns the player with the given ID, or nil.
func (g *Game) PlayerByID(id string) *Player {
	for _, p := range g.Players {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (g *Game) addEvent(msg string) {
	g.Events = append(g.Events, msg)
	g.FullEventLog = append(g.FullEventLog, msg)
	// Cap the lifetime log to avoid unbounded growth in very long games.
	if len(g.FullEventLog) > maxEventLog {
		g.FullEventLog = g.FullEventLog[len(g.FullEventLog)-maxEventLog:]
	}
}

// AddEvent appends a message to the event log. Use this from outside the
// package (e.g. the server) to record events that have no corresponding
// engine action.
func (g *Game) AddEvent(msg string) {
	g.addEvent(msg)
}

// DiscardTop returns the top card of the discard pile.
func (g *Game) DiscardTop() (Card, bool) {
	if len(g.DiscardPile) == 0 {
		return Card{}, false
	}
	return g.DiscardPile[len(g.DiscardPile)-1], true
}

// ─── Turn actions ─────────────────────────────────────────────────────────────

// DrawStock draws the top card of the stock for the active player.
func (g *Game) DrawStock(playerID string) error {
	if err := g.checkTurn(playerID, PhaseDrawing); err != nil {
		return err
	}
	if len(g.Stock) == 0 {
		if err := g.reshuffleDiscard(); err != nil {
			return err
		}
	}
	card := g.Stock[len(g.Stock)-1]
	g.Stock = g.Stock[:len(g.Stock)-1]
	g.activePlayer().Hand.Add(card)
	g.Phase = PhaseMelding
	g.addEvent(fmt.Sprintf("%s robó del mazo.", g.activePlayer().Name))
	return nil
}

// DrawDiscard draws the top discard card for the active player.
// The player may only take the discard if they can immediately use it in a new
// meld formed from their own hand.
func (g *Game) DrawDiscard(playerID string) error {
	if err := g.checkTurn(playerID, PhaseDrawing); err != nil {
		return err
	}
	if len(g.DiscardPile) == 0 {
		return errors.New("el pozo está vacío")
	}
	card := g.DiscardPile[len(g.DiscardPile)-1]

	// Validate: card must be usable in a new meld this turn.
	if !CanUsePickedCard(card, g.activePlayer().Hand, g.Melds) {
		return fmt.Errorf("solo se puede tomar del pozo si la carta %s se puede usar en una bajada con tu mano en este turno", card.String())
	}

	g.DiscardPile = g.DiscardPile[:len(g.DiscardPile)-1]
	p := g.activePlayer()
	p.Hand.Add(card)
	// Track which card was picked up — the player must play it before discarding.
	cardCopy := card
	p.PickedUpDiscard = &cardCopy
	g.Phase = PhaseMelding
	g.addEvent(fmt.Sprintf("%s tomó del pozo (%s).", p.Name, card.String()))
	return nil
}

// Meld creates a new meld from cards in the active player's hand.
// cardIndexes are indexes into the player's hand slice.
func (g *Game) Meld(playerID string, cardIndexes []int, meldType MeldType) error {
	if err := g.checkTurn(playerID, PhaseMelding); err != nil {
		return err
	}
	player := g.activePlayer()

	cards, err := g.extractCards(player, cardIndexes)
	if err != nil {
		return err
	}

	var validateErr error
	switch meldType {
	case MeldPierna:
		validateErr = ValidatePierna(cards)
	case MeldEscalera:
		validateErr = ValidateEscalera(cards)
	default:
		return errors.New("tipo de combinación desconocido")
	}
	if validateErr != nil {
		// Return cards to hand.
		player.Hand = append(player.Hand, cards...)
		return validateErr
	}

	// Sort cards into visual/sequence order before storing.
	switch meldType {
	case MeldEscalera:
		cards = SortEscaleraCards(cards)
	case MeldPierna:
		cards = SortPiernaCards(cards)
	}

	meld := Meld{Type: meldType, Cards: cards, OwnerID: playerID}
	g.Melds = append(g.Melds, meld)
	player.HasMelded = true
	// If the picked-up discard was included in this meld, it has been played.
	if player.PickedUpDiscard != nil {
		for _, mc := range cards {
			if mc.Equal(*player.PickedUpDiscard) {
				player.PickedUpDiscard = nil
				break
			}
		}
	}
	g.addEvent(fmt.Sprintf("%s bajó %s.", player.Name, describeCards(cards)))

	if len(player.Hand) == 0 {
		return g.endRound(player)
	}
	return nil
}

// LayOff adds cards from the active player's hand to an existing meld.
func (g *Game) LayOff(playerID string, cardIndexes []int, meldIndex int) error {
	if err := g.checkTurn(playerID, PhaseMelding); err != nil {
		return err
	}
	player := g.activePlayer()
	if !player.HasMelded {
		return errors.New("debés bajar al menos una combinación antes de agregar cartas")
	}
	if meldIndex < 0 || meldIndex >= len(g.Melds) {
		return errors.New("índice de combinación inválido")
	}
	if len(cardIndexes) != 1 {
		return errors.New("solo se puede agregar una carta a la vez")
	}

	cards, err := g.extractCards(player, cardIndexes)
	if err != nil {
		return err
	}
	card := cards[0]
	meld := &g.Melds[meldIndex]

	var layErr error
	switch meld.Type {
	case MeldPierna:
		layErr = CanLayOffPierna(meld, card)
		if layErr == nil {
			meld.Cards = append(meld.Cards, card)
		}
	case MeldEscalera:
		layErr = CanLayOffEscalera(meld, card)
		if layErr == nil {
			LayOffEscalera(meld, card)
		}
	}
	if layErr != nil {
		player.Hand = append(player.Hand, card)
		return layErr
	}

	// If the picked-up discard was laid off, it has been played.
	if player.PickedUpDiscard != nil && card.Equal(*player.PickedUpDiscard) {
		player.PickedUpDiscard = nil
	}
	g.addEvent(fmt.Sprintf("%s agregó %s a la combinación #%d.", player.Name, card.String(), meldIndex+1))

	if len(player.Hand) == 0 {
		return g.endRound(player)
	}
	return nil
}

// Discard discards one card from the active player's hand, ending their turn.
func (g *Game) Discard(playerID string, cardIndex int) error {
	if err := g.checkTurn(playerID, PhaseMelding); err != nil {
		return err
	}
	player := g.activePlayer()
	if cardIndex < 0 || cardIndex >= len(player.Hand) {
		return errors.New("índice de carta inválido")
	}

	card := player.Hand[cardIndex]

	// Joker discard rule: a joker cannot be discarded, UNLESS it is the only card
	// remaining in hand (forced discard with no other option).
	if card.IsJoker() {
		allJokers := true
		for _, c := range player.Hand {
			if !c.IsJoker() {
				allJokers = false
				break
			}
		}
		if !allJokers {
			return errors.New("no se puede descartar un comodín")
		}
	}

	// If the player picked up the discard this turn, enforce usage rules.
	if player.PickedUpDiscard != nil {
		pickedIdx := player.Hand.FindIndex(*player.PickedUpDiscard)
		if pickedIdx >= 0 {
			// The picked card is still in hand — it hasn't been played yet.
			// Safety valve: if no legal action can consume the picked card right now
			// (validator/state inconsistency or game state changed since pickup),
			// allow the player to return it to the discard pile rather than hard-locking.
			if !pickedCardHasLegalUse(player, g.Melds) {
				// Return the picked card to the discard pile (not the card being discarded).
				picked := *player.PickedUpDiscard
				player.Hand.Remove(pickedIdx)
				g.DiscardPile = append(g.DiscardPile, picked)
				g.addEvent(fmt.Sprintf("%s devuelve %s al pozo al no poder usarla.", player.Name, picked.String()))
				player.PickedUpDiscard = nil
				// The player still needs to discard the card they chose — recalculate
				// the index since hand shrank by one position.
				newIdx := player.Hand.FindIndex(card)
				if newIdx < 0 {
					// card was the picked card itself (edge case: shouldn't happen here but be safe).
					g.advanceTurn()
					return nil
				}
				cardIndex = newIdx
			} else if player.Hand[cardIndex].Equal(*player.PickedUpDiscard) {
				// Normal rule: cannot directly discard the picked-up card.
				return fmt.Errorf("no podés descartar %s — la tomaste del pozo y debés usarla en una bajada nueva", player.PickedUpDiscard.String())
			} else {
				// Normal rule: must play the picked card first.
				return fmt.Errorf("debés jugar %s (tomada del pozo) en una bajada antes de terminar tu turno", player.PickedUpDiscard.String())
			}
		}
	}

	player.Hand.Remove(cardIndex)
	g.DiscardPile = append(g.DiscardPile, card)
	g.addEvent(fmt.Sprintf("%s descartó %s.", player.Name, card.String()))

	if len(player.Hand) == 0 {
		return g.endRound(player)
	}

	g.advanceTurn()
	return nil
}

// AutoPlayDisconnected auto-plays for a disconnected player: draw stock, discard it.
func (g *Game) AutoPlayDisconnected() error {
	p := g.activePlayer()
	if p.Connected {
		return errors.New("active player is connected")
	}

	if g.Phase == PhaseDrawing {
		if len(g.Stock) == 0 {
			if err := g.reshuffleDiscard(); err != nil {
				return err
			}
		}
		if len(g.Stock) > 0 {
			card := g.Stock[len(g.Stock)-1]
			g.Stock = g.Stock[:len(g.Stock)-1]
			p.Hand.Add(card)
		}
		g.Phase = PhaseMelding
	}

	if g.Phase == PhaseMelding && len(p.Hand) > 0 {
		card := p.Hand.Remove(len(p.Hand) - 1)
		g.DiscardPile = append(g.DiscardPile, card)
		g.addEvent(fmt.Sprintf("%s está desconectado — turno automático.", p.Name))
		g.advanceTurn()
	}
	return nil
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func (g *Game) checkTurn(playerID string, phase Phase) error {
	if g.Phase == PhaseRoundEnd || g.Phase == PhaseGameOver {
		return errors.New("el juego no está en una fase jugable")
	}
	if g.activePlayer().ID != playerID {
		return errors.New("no es tu turno")
	}
	if g.Phase != phase {
		return fmt.Errorf("fase esperada %s, fase actual %s", phase, g.Phase)
	}
	return nil
}

// extractCards removes cards at the given indexes from the player's hand and
// returns them. If any index is invalid it returns an error and does NOT modify
// the hand.
func (g *Game) extractCards(player *Player, indexes []int) ([]Card, error) {
	// Validate all indexes first.
	seen := make(map[int]bool)
	for _, i := range indexes {
		if i < 0 || i >= len(player.Hand) {
			return nil, fmt.Errorf("índice de carta %d fuera de rango", i)
		}
		if seen[i] {
			return nil, fmt.Errorf("índice de carta duplicado %d", i)
		}
		seen[i] = true
	}

	// Sort descending so removal doesn't shift earlier indexes.
	sorted := make([]int, len(indexes))
	copy(sorted, indexes)
	sortDesc(sorted)

	cards := make([]Card, len(indexes))
	for pos, i := range sorted {
		cards[len(indexes)-1-pos] = player.Hand.Remove(i)
	}
	return cards, nil
}

func (g *Game) advanceTurn() {
	// Clear the picked-up discard tracker for the player who just finished.
	g.activePlayer().PickedUpDiscard = nil
	next := (g.ActiveIndex + 1) % len(g.Players)
	g.ActiveIndex = next
	g.Phase = PhaseDrawing
	g.addEvent(fmt.Sprintf("Es el turno de %s.", g.activePlayer().Name))
}

func (g *Game) endRound(winner *Player) error {
	winner.PickedUpDiscard = nil
	g.Phase = PhaseRoundEnd
	g.addEvent(fmt.Sprintf("¡%s se fue! Ronda %d terminada.", winner.Name, g.Round))

	// Score remaining hands.
	for _, p := range g.Players {
		p.RoundScore = p.Hand.Score()
		p.TotalScore += p.RoundScore
	}

	// Capture the round reveal AFTER scoring so RoundScore/TotalScore are final,
	// but while the hands are still intact (startRound clears them later).
	rr := &RoundResult{Round: g.Round}
	rs := RoundScores{
		Round:  g.Round,
		Scores: make(map[string]int, len(g.Players)),
		Names:  make(map[string]string, len(g.Players)),
	}
	for _, p := range g.Players {
		handCopy := make(Hand, len(p.Hand))
		copy(handCopy, p.Hand)
		rr.Results = append(rr.Results, PlayerRoundResult{
			PlayerID:   p.ID,
			PlayerName: p.Name,
			Hand:       handCopy,
			RoundScore: p.RoundScore,
			TotalScore: p.TotalScore,
		})
		rs.Scores[p.ID] = p.RoundScore
		rs.Names[p.ID] = p.Name
	}
	g.LastRoundResult = rr
	g.ScoreHistory = append(g.ScoreHistory, rs)

	// Check if game is over.
	for _, p := range g.Players {
		if p.TotalScore > MaxScore {
			g.Phase = PhaseGameOver
			break
		}
	}
	return nil
}

// NextRound advances to the next round. May only be called when Phase == PhaseRoundEnd.
func (g *Game) NextRound() error {
	if g.Phase != PhaseRoundEnd {
		return errors.New("la ronda no ha terminado")
	}
	if g.Phase == PhaseGameOver {
		return errors.New("el juego ha terminado")
	}

	// Check again — might have been set by endRound.
	for _, p := range g.Players {
		if p.TotalScore > MaxScore {
			g.Phase = PhaseGameOver
			return nil
		}
	}

	g.Round++
	// Rotate starting player.
	g.ActiveIndex = (g.ActiveIndex + 1) % len(g.Players)
	g.startRound()
	return nil
}

// Winner returns the player with the lowest total score (only valid when
// Phase == PhaseGameOver).
func (g *Game) Winner() *Player {
	var best *Player
	for _, p := range g.Players {
		if best == nil || p.TotalScore < best.TotalScore {
			best = p
		}
	}
	return best
}

func (g *Game) reshuffleDiscard() error {
	if len(g.DiscardPile) <= 1 {
		return errors.New("no hay suficientes cartas para reiniciar el mazo")
	}
	top := g.DiscardPile[len(g.DiscardPile)-1]
	pile := g.DiscardPile[:len(g.DiscardPile)-1]
	shuffle(pile, g.rng)
	g.Stock = pile
	g.DiscardPile = []Card{top}
	g.addEvent("Mazo agotado — el pozo fue mezclado y reiniciado.")
	return nil
}

// ─── Small utilities ──────────────────────────────────────────────────────────

func sortDesc(a []int) {
	for i := 0; i < len(a); i++ {
		for j := i + 1; j < len(a); j++ {
			if a[j] > a[i] {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}

func describeCards(cards []Card) string {
	s := ""
	for i, c := range cards {
		if i > 0 {
			s += " "
		}
		s += c.String()
	}
	return s
}
