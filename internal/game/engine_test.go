package game

import (
	"testing"
)

// ─── Card helpers ─────────────────────────────────────────────────────────────

func c(rank Rank, suit Suit) Card { return Card{Rank: rank, Suit: suit} }
func joker(idx int) Card          { return Card{Rank: Joker, Suit: NoSuit, JokerIndex: idx} }

// ─── Pierna validation ────────────────────────────────────────────────────────

func TestValidatePierna(t *testing.T) {
	tests := []struct {
		name    string
		cards   []Card
		wantErr bool
	}{
		{
			name:  "valid pierna",
			cards: []Card{c(Seven, Spades), c(Seven, Hearts), c(Seven, Diamonds)},
		},
		{
			name:    "too few cards",
			cards:   []Card{c(Seven, Spades), c(Seven, Hearts)},
			wantErr: true,
		},
		{
			name:    "too many cards",
			cards:   []Card{c(Seven, Spades), c(Seven, Hearts), c(Seven, Diamonds), c(Seven, Clubs)},
			wantErr: true,
		},
		{
			name:    "duplicate suit",
			cards:   []Card{c(Seven, Spades), c(Seven, Spades), c(Seven, Hearts)},
			wantErr: true,
		},
		{
			name:    "mixed ranks",
			cards:   []Card{c(Seven, Spades), c(Eight, Hearts), c(Seven, Diamonds)},
			wantErr: true,
		},
		{
			name:    "joker not allowed",
			cards:   []Card{c(Seven, Spades), c(Seven, Hearts), joker(0)},
			wantErr: true,
		},
		{
			name:  "aces with different suits",
			cards: []Card{c(Ace, Spades), c(Ace, Hearts), c(Ace, Clubs)},
		},
		{
			name:  "kings with different suits",
			cards: []Card{c(King, Spades), c(King, Hearts), c(King, Diamonds)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePierna(tt.cards)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePierna() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ─── Escalera validation ──────────────────────────────────────────────────────

func TestValidateEscalera(t *testing.T) {
	tests := []struct {
		name    string
		cards   []Card
		wantErr bool
	}{
		{
			name:  "simple 3-card run",
			cards: []Card{c(Five, Spades), c(Six, Spades), c(Seven, Spades)},
		},
		{
			name:  "5-card run",
			cards: []Card{c(Three, Hearts), c(Four, Hearts), c(Five, Hearts), c(Six, Hearts), c(Seven, Hearts)},
		},
		{
			name:    "too short",
			cards:   []Card{c(Five, Spades), c(Six, Spades)},
			wantErr: true,
		},
		{
			name:    "mixed suits",
			cards:   []Card{c(Five, Spades), c(Six, Hearts), c(Seven, Spades)},
			wantErr: true,
		},
		{
			name:    "non-consecutive",
			cards:   []Card{c(Five, Spades), c(Seven, Spades), c(Eight, Spades)},
			wantErr: true,
		},
		{
			name:  "ace-low A-2-3",
			cards: []Card{c(Ace, Clubs), c(Two, Clubs), c(Three, Clubs)},
		},
		{
			name:  "ace-high Q-K-A",
			cards: []Card{c(Queen, Diamonds), c(King, Diamonds), c(Ace, Diamonds)},
		},
		{
			name:    "wrap-around K-A-2 not allowed",
			cards:   []Card{c(King, Spades), c(Ace, Spades), c(Two, Spades)},
			wantErr: true,
		},
		{
			name:  "joker filling gap",
			cards: []Card{c(Five, Spades), joker(0), c(Seven, Spades)},
		},
		{
			name:    "two jokers not allowed",
			cards:   []Card{c(Five, Spades), joker(0), joker(1), c(Eight, Spades)},
			wantErr: true,
		},
		{
			name:  "joker extending high end",
			cards: []Card{c(Jack, Hearts), c(Queen, Hearts), joker(0)},
		},
		{
			name:  "joker extending low end",
			cards: []Card{joker(0), c(Three, Diamonds), c(Four, Diamonds)},
		},
		{
			name:    "joker cannot span gap of 2",
			cards:   []Card{c(Five, Spades), joker(0), c(Eight, Spades)},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEscalera(tt.cards)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEscalera() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ─── Pierna lay-off ───────────────────────────────────────────────────────────

func TestCanLayOffPierna(t *testing.T) {
	meld := &Meld{
		Type:  MeldPierna,
		Cards: []Card{c(Seven, Spades), c(Seven, Hearts), c(Seven, Diamonds)},
	}

	// Same rank, any suit allowed after creation (even a fourth seven of spades).
	if err := CanLayOffPierna(meld, c(Seven, Clubs)); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	// Second spade allowed after creation.
	if err := CanLayOffPierna(meld, c(Seven, Spades)); err != nil {
		t.Errorf("expected nil for duplicate suit after creation, got %v", err)
	}
	// Wrong rank.
	if err := CanLayOffPierna(meld, c(Eight, Clubs)); err == nil {
		t.Error("expected error for wrong rank")
	}
	// Joker.
	if err := CanLayOffPierna(meld, joker(0)); err == nil {
		t.Error("expected error for joker on pierna")
	}
}

// ─── Escalera lay-off ─────────────────────────────────────────────────────────

func TestCanLayOffEscalera(t *testing.T) {
	meld := &Meld{
		Type:  MeldEscalera,
		Cards: []Card{c(Five, Spades), c(Six, Spades), c(Seven, Spades)},
	}

	// Extend high.
	if err := CanLayOffEscalera(meld, c(Eight, Spades)); err != nil {
		t.Errorf("extend high: %v", err)
	}
	// Extend low.
	if err := CanLayOffEscalera(meld, c(Four, Spades)); err != nil {
		t.Errorf("extend low: %v", err)
	}
	// Wrong suit.
	if err := CanLayOffEscalera(meld, c(Eight, Hearts)); err == nil {
		t.Error("expected error for wrong suit")
	}
	// Non-adjacent.
	if err := CanLayOffEscalera(meld, c(Nine, Spades)); err == nil {
		// 9 is not adjacent to 5-6-7 meld on low end; it IS adjacent on high end (7→8 is adjacent, 9 is not — wait, 8 would be adjacent, 9 would not if we extend 7→8→9 but meld is only 5-6-7)
		// Actually 9 is NOT directly adjacent to 7 with no gap; it would skip 8.
		t.Error("expected error for non-adjacent rank")
	}
}

// ─── Scoring ──────────────────────────────────────────────────────────────────

func TestHandScore(t *testing.T) {
	tests := []struct {
		name  string
		cards []Card
		want  int
	}{
		{
			name:  "empty hand",
			cards: nil,
			want:  0,
		},
		{
			name:  "joker = 25",
			cards: []Card{joker(0)},
			want:  25,
		},
		{
			name:  "ace = 15",
			cards: []Card{c(Ace, Spades)},
			want:  15,
		},
		{
			name:  "face cards = 10 each",
			cards: []Card{c(Jack, Spades), c(Queen, Hearts), c(King, Diamonds)},
			want:  30,
		},
		{
			name:  "numeric cards = face value",
			cards: []Card{c(Two, Spades), c(Seven, Hearts), c(Ten, Clubs)},
			want:  19,
		},
		{
			name:  "mixed hand",
			cards: []Card{joker(0), c(Ace, Spades), c(King, Hearts), c(Five, Diamonds)},
			want:  55,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := Hand(tt.cards)
			if got := h.Score(); got != tt.want {
				t.Errorf("Hand.Score() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ─── Full engine smoke test ───────────────────────────────────────────────────

func TestEngineBasicTurn(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "Alice"},
		{"p2", "Bob"},
	}
	g, err := NewGame(players, 42)
	if err != nil {
		t.Fatal(err)
	}

	if g.Phase != PhaseDrawing {
		t.Fatalf("expected drawing phase, got %s", g.Phase)
	}
	if len(g.Players[0].Hand) != HandSize {
		t.Fatalf("expected %d cards, got %d", HandSize, len(g.Players[0].Hand))
	}

	// Alice draws from stock.
	if err := g.DrawStock("p1"); err != nil {
		t.Fatal(err)
	}
	if g.Phase != PhaseMelding {
		t.Fatalf("expected melding phase after draw, got %s", g.Phase)
	}
	if len(g.Players[0].Hand) != HandSize+1 {
		t.Fatalf("expected %d cards after draw, got %d", HandSize+1, len(g.Players[0].Hand))
	}

	// Alice discards the last card (index 9 after draw).
	if err := g.Discard("p1", len(g.Players[0].Hand)-1); err != nil {
		t.Fatal(err)
	}
	if g.Phase != PhaseDrawing {
		t.Fatalf("expected drawing phase after discard, got %s", g.Phase)
	}
	// Should be Bob's turn now.
	if g.activePlayer().ID != "p2" {
		t.Fatalf("expected Bob's turn, got %s", g.activePlayer().ID)
	}
}

func TestEngineDrawFromDiscard(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "Alice"},
		{"p2", "Bob"},
	}
	g, err := NewGame(players, 42)
	if err != nil {
		t.Fatal(err)
	}

	// Rig the discard to a Seven of Spades and give Alice two more Sevens so she
	// can form a valid pierna — satisfying the pickup rule.
	discardCard := c(Seven, Spades)
	g.DiscardPile = []Card{discardCard}
	g.Players[0].Hand = Hand{
		c(Seven, Hearts), c(Seven, Diamonds),
		c(Two, Clubs), c(Three, Clubs), c(Four, Clubs),
		c(King, Spades), c(Queen, Hearts), c(Jack, Diamonds), c(Ten, Clubs),
	}

	top, ok := g.DiscardTop()
	if !ok {
		t.Fatal("discard pile empty at start")
	}

	if err := g.DrawDiscard("p1"); err != nil {
		t.Fatalf("DrawDiscard with usable card: %v", err)
	}

	// Alice's hand should contain the former discard top.
	found := false
	for _, card := range g.Players[0].Hand {
		if card.Equal(top) {
			found = true
			break
		}
	}
	if !found {
		t.Error("discard top not found in Alice's hand after DrawDiscard")
	}
	// PickedUpDiscard must be set.
	if g.Players[0].PickedUpDiscard == nil {
		t.Error("PickedUpDiscard should be set after DrawDiscard")
	}
}

func TestEngineWrongTurnRejected(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "Alice"},
		{"p2", "Bob"},
	}
	g, err := NewGame(players, 42)
	if err != nil {
		t.Fatal(err)
	}

	// Bob tries to draw when it's Alice's turn.
	if err := g.DrawStock("p2"); err == nil {
		t.Error("expected error when drawing out of turn")
	}
}

func TestEngineMeldAndLayOff(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "Alice"},
		{"p2", "Bob"},
	}
	g, err := NewGame(players, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Manually set Alice's hand to something we control.
	g.Players[0].Hand = Hand{
		c(Seven, Spades), c(Seven, Hearts), c(Seven, Diamonds), // valid pierna
		c(Two, Clubs), c(Three, Clubs), c(Four, Clubs), // valid escalera
		c(King, Spades), c(Queen, Hearts), c(Jack, Diamonds), c(Ten, Clubs),
	}

	// Draw so we're in PhaseMelding.
	_ = g.DrawStock("p1")

	// Meld the pierna (indexes 0,1,2).
	if err := g.Meld("p1", []int{0, 1, 2}, MeldPierna); err != nil {
		t.Fatalf("meld pierna: %v", err)
	}
	if len(g.Melds) != 1 {
		t.Fatalf("expected 1 meld, got %d", len(g.Melds))
	}
	if !g.Players[0].HasMelded {
		t.Error("expected HasMelded = true after melding")
	}

	// Now hand is [2♣ 3♣ 4♣ K♠ Q♥ J♦ 10♣ + drawn card] = 8 cards.
	// Meld escalera (indexes 0,1,2 of updated hand = 2♣ 3♣ 4♣).
	if err := g.Meld("p1", []int{0, 1, 2}, MeldEscalera); err != nil {
		t.Fatalf("meld escalera: %v", err)
	}
	if len(g.Melds) != 2 {
		t.Fatalf("expected 2 melds, got %d", len(g.Melds))
	}

	// Now lay off onto the escalera (meld index 1): add 5♣ if Alice has it.
	// Since we're working with controlled hand, let's add 5♣ directly.
	g.Players[0].Hand = append(g.Players[0].Hand, c(Five, Clubs))
	lastIdx := len(g.Players[0].Hand) - 1
	if err := g.LayOff("p1", []int{lastIdx}, 1); err != nil {
		t.Fatalf("lay off 5♣ on escalera: %v", err)
	}
	escalera := g.Melds[1]
	if len(escalera.Cards) != 4 {
		t.Fatalf("expected 4 cards in escalera, got %d", len(escalera.Cards))
	}
}

func TestEngineLayOffRequiresMeld(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "Alice"},
		{"p2", "Bob"},
	}
	g, err := NewGame(players, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Create a meld manually owned by Bob so there's something to lay off on.
	g.Melds = []Meld{{
		Type:    MeldPierna,
		Cards:   []Card{c(Seven, Spades), c(Seven, Hearts), c(Seven, Diamonds)},
		OwnerID: "p2",
	}}

	_ = g.DrawStock("p1")

	// Alice tries to lay off without having melded.
	g.Players[0].Hand = append(g.Players[0].Hand, c(Seven, Clubs))
	if err := g.LayOff("p1", []int{len(g.Players[0].Hand) - 1}, 0); err == nil {
		t.Error("expected error for lay-off without prior meld")
	}
}

func TestEngineRoundEndScoring(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "Alice"},
		{"p2", "Bob"},
	}
	g, err := NewGame(players, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Rig: Alice has exactly one card, Bob has two face-value cards.
	g.Players[0].Hand = Hand{c(Five, Spades)}
	g.Players[1].Hand = Hand{c(Three, Hearts), c(Four, Diamonds)}
	g.Phase = PhaseMelding

	// Alice discards her last card → round ends.
	if err := g.Discard("p1", 0); err != nil {
		t.Fatal(err)
	}
	if g.Phase != PhaseRoundEnd && g.Phase != PhaseGameOver {
		t.Fatalf("expected round_end or game_over, got %s", g.Phase)
	}

	// Alice's round score = 0 (empty hand).
	if g.Players[0].RoundScore != 0 {
		t.Errorf("Alice's round score = %d, want 0", g.Players[0].RoundScore)
	}
	// Bob's round score = 3+4 = 7.
	if g.Players[1].RoundScore != 7 {
		t.Errorf("Bob's round score = %d, want 7", g.Players[1].RoundScore)
	}
}

func TestEngineDisconnectedAutoPlay(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "Alice"},
		{"p2", "Bob"},
	}
	g, err := NewGame(players, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Mark Alice disconnected.
	g.Players[0].Connected = false

	if err := g.AutoPlayDisconnected(); err != nil {
		t.Fatal(err)
	}
	// Should be Bob's turn now.
	if g.activePlayer().ID != "p2" {
		t.Fatalf("expected Bob's turn after auto-play, got %s", g.activePlayer().ID)
	}
}

// ─── Discard-pickup rule tests ────────────────────────────────────────────────

// makeGame builds a 2-player game with Alice as active player in PhaseDrawing.
func makeGame() *Game {
	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}
	g, _ := NewGame(players, 0)
	g.Phase = PhaseDrawing
	g.ActiveIndex = 0
	return g
}

func TestDrawDiscardRejectedWhenUnusable(t *testing.T) {
	g := makeGame()
	// Give Alice a hand with no cards that combine with the discard.
	g.Players[0].Hand = Hand{
		c(Two, Clubs), c(Three, Hearts), c(Jack, Spades),
		c(King, Diamonds), c(Queen, Clubs), c(Ten, Hearts),
		c(Four, Diamonds), c(Six, Spades), c(Eight, Clubs),
	}
	// Discard top: Ace of Spades — nothing in Alice's hand forms a meld with it.
	g.DiscardPile = []Card{c(Ace, Spades)}

	err := g.DrawDiscard("p1")
	if err == nil {
		t.Error("expected rejection: discard card not usable in any meld")
	}
}

func TestDrawDiscardAcceptedWhenMeldable(t *testing.T) {
	g := makeGame()
	// Alice has two Sevens — taking 7♠ gives her three Sevens (pierna).
	g.Players[0].Hand = Hand{
		c(Seven, Hearts), c(Seven, Diamonds),
		c(Two, Clubs), c(Three, Clubs), c(Four, Clubs),
		c(King, Spades), c(Queen, Hearts), c(Jack, Diamonds), c(Ten, Clubs),
	}
	g.DiscardPile = []Card{c(Seven, Spades)}

	if err := g.DrawDiscard("p1"); err != nil {
		t.Errorf("expected acceptance (pierna possible): %v", err)
	}
	if g.Players[0].PickedUpDiscard == nil {
		t.Error("PickedUpDiscard should be set")
	}
}

func TestDrawDiscardAcceptedWhenLayOffable(t *testing.T) {
	g := makeGame()
	// Put a 5-6-7 escalera on the table; Alice picks up 8♣ to extend it.
	g.Melds = []Meld{{
		Type:  MeldEscalera,
		Cards: []Card{c(Five, Clubs), c(Six, Clubs), c(Seven, Clubs)},
	}}
	g.Players[0].Hand = Hand{
		c(Two, Hearts), c(Three, Hearts), c(Jack, Spades),
		c(King, Diamonds), c(Queen, Clubs), c(Ten, Hearts),
		c(Four, Diamonds), c(Six, Spades), c(Nine, Clubs),
	}
	g.DiscardPile = []Card{c(Eight, Clubs)}
	g.Players[0].HasMelded = true // needed for LayOff eligibility check

	if err := g.DrawDiscard("p1"); err != nil {
		t.Errorf("expected acceptance (lay-off possible): %v", err)
	}
}

func TestEndTurnBlockedUntilPickedCardUsed(t *testing.T) {
	g := makeGame()
	g.Players[0].Hand = Hand{
		c(Seven, Hearts), c(Seven, Diamonds),
		c(Two, Clubs), c(Three, Clubs), c(Four, Clubs),
		c(King, Spades), c(Queen, Hearts), c(Jack, Diamonds), c(Ten, Clubs),
	}
	g.DiscardPile = []Card{c(Seven, Spades)}
	_ = g.DrawDiscard("p1")

	// Try discarding without playing the picked card.
	// Alice's hand now has 7♠ at the end; discard something else.
	err := g.Discard("p1", 0) // discard 7♥ (safe card, not the picked one)
	if err == nil {
		t.Error("expected rejection: picked-up discard not played yet")
	}
}

func TestEndTurnAllowedAfterPickedCardUsed(t *testing.T) {
	g := makeGame()
	g.Players[0].Hand = Hand{
		c(Seven, Hearts), c(Seven, Diamonds),
		c(Two, Clubs), c(Three, Clubs), c(Four, Clubs),
		c(King, Spades), c(Queen, Hearts), c(Jack, Diamonds), c(Ten, Clubs),
	}
	g.DiscardPile = []Card{c(Seven, Spades)}
	_ = g.DrawDiscard("p1")
	// Picked up 7♠; hand is now [..., 7♠]. Meld the three Sevens (indexes 0,1 and last).
	lastIdx := len(g.Players[0].Hand) - 1
	if err := g.Meld("p1", []int{0, 1, lastIdx}, MeldPierna); err != nil {
		t.Fatalf("meld three Sevens: %v", err)
	}
	// PickedUpDiscard should now be nil (card was played).
	if g.Players[0].PickedUpDiscard != nil {
		t.Error("PickedUpDiscard should be cleared after the card was melded")
	}
	// Now discard a remaining card — should succeed.
	if err := g.Discard("p1", 0); err != nil {
		t.Errorf("expected successful discard after playing picked card: %v", err)
	}
}

func TestCannotDiscardPickedCard(t *testing.T) {
	g := makeGame()
	g.Players[0].Hand = Hand{
		c(Seven, Hearts), c(Seven, Diamonds),
		c(Two, Clubs), c(Three, Clubs), c(Four, Clubs),
		c(King, Spades), c(Queen, Hearts), c(Jack, Diamonds), c(Ten, Clubs),
	}
	g.DiscardPile = []Card{c(Seven, Spades)}
	_ = g.DrawDiscard("p1")

	// The picked 7♠ is the last card in hand — try to discard it directly.
	lastIdx := len(g.Players[0].Hand) - 1
	err := g.Discard("p1", lastIdx)
	if err == nil {
		t.Error("expected rejection: cannot discard the picked-up card itself")
	}
}

func TestDeckSize(t *testing.T) {
	deck := newDeck()
	if len(deck) != 108 {
		t.Errorf("expected 108 cards, got %d", len(deck))
	}

	jokers := 0
	for _, c := range deck {
		if c.IsJoker() {
			jokers++
		}
	}
	if jokers != 4 {
		t.Errorf("expected 4 jokers, got %d", jokers)
	}
}

func TestNewGamePlayerCount(t *testing.T) {
	one := []struct{ ID, Name string }{{"p1", "A"}}
	_, err := NewGame(one, 0)
	if err == nil {
		t.Error("expected error for 1 player")
	}

	seven := []struct{ ID, Name string }{
		{"p1", "A"}, {"p2", "B"}, {"p3", "C"},
		{"p4", "D"}, {"p5", "E"}, {"p6", "F"}, {"p7", "G"},
	}
	_, err = NewGame(seven, 0)
	if err == nil {
		t.Error("expected error for 7 players")
	}

	two := []struct{ ID, Name string }{{"p1", "A"}, {"p2", "B"}}
	_, err = NewGame(two, 0)
	if err != nil {
		t.Errorf("unexpected error for 2 players: %v", err)
	}
}
