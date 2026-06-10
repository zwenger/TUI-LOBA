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

// TestEngineDisconnectedAutoPlayRepeated verifies that a disconnected player
// keeps getting auto-played every time the turn returns to them, until they
// reconnect or the game ends.
func TestEngineDisconnectedAutoPlayRepeated(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "Alice"},
		{"p2", "Bob"},
		{"p3", "Carol"},
	}
	g, err := NewGame(players, 42)
	if err != nil {
		t.Fatal(err)
	}

	// Alice disconnects immediately.
	g.Players[0].Connected = false

	// Run 6 auto-plays of Alice (2 full laps around the 3-player table).
	autoPlaysForAlice := 0
	for i := 0; i < 12; i++ {
		if g.Phase == PhaseRoundEnd || g.Phase == PhaseGameOver {
			break
		}
		active := g.activePlayer()
		if active.ID == "p1" {
			// Alice's turn: must auto-play.
			if active.Connected {
				t.Fatal("Alice should still be disconnected")
			}
			if err := g.AutoPlayDisconnected(); err != nil {
				t.Fatalf("AutoPlayDisconnected on turn %d: %v", i, err)
			}
			autoPlaysForAlice++
		} else {
			// Bob or Carol: draw from stock and discard the drawn card.
			// Try each card index in reverse until one is discardable (jokers
			// cannot be discarded while other non-joker cards remain in hand).
			if err := g.DrawStock(active.ID); err != nil {
				t.Fatalf("DrawStock %s turn %d: %v", active.ID, i, err)
			}
			discarded := false
			for idx := len(active.Hand) - 1; idx >= 0; idx-- {
				if err := g.Discard(active.ID, idx); err == nil {
					discarded = true
					break
				}
			}
			if !discarded {
				if g.Phase == PhaseRoundEnd || g.Phase == PhaseGameOver {
					break
				}
				t.Fatalf("could not find a discardable card for %s on turn %d", active.ID, i)
			}
			if g.Phase == PhaseRoundEnd || g.Phase == PhaseGameOver {
				break
			}
		}
	}

	if autoPlaysForAlice < 2 {
		t.Errorf("expected at least 2 auto-plays for Alice, got %d", autoPlaysForAlice)
	}

	// Now reconnect Alice: auto-play must stop.
	g.Players[0].Connected = true
	// Advance until it is Alice's turn.
	for g.activePlayer().ID != "p1" {
		if g.Phase == PhaseRoundEnd || g.Phase == PhaseGameOver {
			return // game ended before Alice's turn — acceptable
		}
		active := g.activePlayer()
		_ = g.DrawStock(active.ID)
		// Discard first non-joker card.
		for idx := 0; idx < len(active.Hand); idx++ {
			if err := g.Discard(active.ID, idx); err == nil {
				break
			}
		}
		if g.Phase == PhaseRoundEnd || g.Phase == PhaseGameOver {
			return
		}
	}
	// AutoPlayDisconnected must now refuse.
	if err := g.AutoPlayDisconnected(); err == nil {
		t.Error("expected error when calling AutoPlayDisconnected on a connected player")
	}
}

// TestEngineAutoPlayEventLog verifies that the event log records the correct
// Spanish message when a disconnected player is auto-played.
func TestEngineAutoPlayEventLog(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "Alice"},
		{"p2", "Bob"},
	}
	g, err := NewGame(players, 0)
	if err != nil {
		t.Fatal(err)
	}

	g.Players[0].Connected = false
	_ = g.AutoPlayDisconnected()

	// One of the events must mention the player name and "desconectado".
	if len(g.Events) == 0 {
		t.Fatal("expected events after auto-play, got none")
	}
	found := false
	for _, ev := range g.Events {
		if contains(ev, "Alice") && contains(ev, "desconectado") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no auto-play event found for Alice; events: %v", g.Events)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsHelper(s, sub))
}

func containsHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
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

// ─── Round-result reveal tests ────────────────────────────────────────────────

// TestRoundResultRevealCapture verifies that LastRoundResult is set at round end
// with the correct hands and per-player penalties.
func TestRoundResultRevealCapture(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "Alice"},
		{"p2", "Bob"},
	}
	g, err := NewGame(players, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Rig hands: Alice has one card (she'll discard it and go out),
	// Bob holds K♠ (10 pts) and 5♣ (5 pts) = 15 total.
	g.Players[0].Hand = Hand{c(Seven, Hearts)}
	g.Players[1].Hand = Hand{c(King, Spades), c(Five, Clubs)}
	g.Phase = PhaseMelding

	if g.LastRoundResult != nil {
		t.Error("LastRoundResult should be nil before round ends")
	}

	// Alice discards her last card → round ends.
	if err := g.Discard("p1", 0); err != nil {
		t.Fatal(err)
	}

	if g.Phase != PhaseRoundEnd && g.Phase != PhaseGameOver {
		t.Fatalf("expected round_end or game_over, got %s", g.Phase)
	}
	if g.LastRoundResult == nil {
		t.Fatal("LastRoundResult must be set after round ends")
	}

	rr := g.LastRoundResult

	// Find Alice's and Bob's results.
	var aliceResult, bobResult *PlayerRoundResult
	for i := range rr.Results {
		switch rr.Results[i].PlayerID {
		case "p1":
			aliceResult = &rr.Results[i]
		case "p2":
			bobResult = &rr.Results[i]
		}
	}
	if aliceResult == nil || bobResult == nil {
		t.Fatal("missing player results in LastRoundResult")
	}

	// Alice went out: empty hand, round score = 0.
	if len(aliceResult.Hand) != 0 {
		t.Errorf("Alice's reveal hand = %v, want empty", aliceResult.Hand)
	}
	if aliceResult.RoundScore != 0 {
		t.Errorf("Alice's round score = %d, want 0", aliceResult.RoundScore)
	}

	// Bob holds K♠ + 5♣ = 10 + 5 = 15.
	if len(bobResult.Hand) != 2 {
		t.Errorf("Bob's reveal hand len = %d, want 2", len(bobResult.Hand))
	}
	if bobResult.RoundScore != 15 {
		t.Errorf("Bob's round score = %d, want 15", bobResult.RoundScore)
	}

	// Verify the exact cards in Bob's reveal hand.
	hasKing := false
	hasFive := false
	for _, card := range bobResult.Hand {
		if card.Rank == King && card.Suit == Spades {
			hasKing = true
		}
		if card.Rank == Five && card.Suit == Clubs {
			hasFive = true
		}
	}
	if !hasKing {
		t.Error("K♠ missing from Bob's reveal hand")
	}
	if !hasFive {
		t.Error("5♣ missing from Bob's reveal hand")
	}
}

// TestRoundResultRevealPreservedAfterNextRound verifies that calling NextRound
// does NOT wipe LastRoundResult (it is only replaced on the next endRound).
func TestRoundResultRevealPreservedAfterNextRound(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "Alice"},
		{"p2", "Bob"},
	}
	g, err := NewGame(players, 42)
	if err != nil {
		t.Fatal(err)
	}

	// Rig Alice to win round 1.
	g.Players[0].Hand = Hand{c(Three, Spades)}
	g.Players[1].Hand = Hand{c(King, Hearts)}
	g.Phase = PhaseMelding

	_ = g.Discard("p1", 0)
	if g.LastRoundResult == nil {
		t.Fatal("LastRoundResult should be set after round 1")
	}
	round1Result := g.LastRoundResult

	// Start round 2 (only valid if not game over).
	if g.Phase == PhaseRoundEnd {
		if err := g.NextRound(); err != nil {
			t.Fatalf("NextRound: %v", err)
		}
		// LastRoundResult should still point to round 1's result until round 2 ends.
		if g.LastRoundResult != round1Result {
			t.Error("LastRoundResult should not change until the next round ends")
		}
	}
}

// TestRoundResultJokerPenalty verifies that a joker left in hand is captured
// in the reveal with penalty value 25.
// ─── ScoreHistory tests ───────────────────────────────────────────────────────

// TestScoreHistoryAccumulates verifies that ScoreHistory grows by one entry per
// completed round and that per-player scores match the engine's per-round tallies.
// The test drives a 2-player game through 3 rounds using seeded, rigged hands.
func TestScoreHistoryAccumulates(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "Alice"},
		{"p2", "Bob"},
	}
	g, err := NewGame(players, 42)
	if err != nil {
		t.Fatal(err)
	}

	type roundSetup struct {
		aliceHand Hand
		bobHand   Hand
	}

	// Alice wins each round by holding one cheap card, Bob always holds one
	// card whose penalty we can predict.
	rounds := []roundSetup{
		{Hand{c(Two, Spades)}, Hand{c(King, Hearts)}},        // Bob penalty = 10
		{Hand{c(Three, Clubs)}, Hand{c(Five, Diamonds)}},     // Bob penalty = 5
		{Hand{c(Four, Hearts)}, Hand{c(Ace, Spades)}},        // Bob penalty = 15
	}

	bobAccum := 0
	for i, setup := range rounds {
		if g.Phase == PhaseGameOver {
			break
		}
		g.Players[0].Hand = setup.aliceHand
		g.Players[1].Hand = setup.bobHand
		g.Phase = PhaseMelding
		g.ActiveIndex = 0 // Alice's turn

		if err := g.Discard("p1", 0); err != nil {
			t.Fatalf("round %d Discard: %v", i+1, err)
		}

		// Verify ScoreHistory grew.
		if len(g.ScoreHistory) != i+1 {
			t.Fatalf("round %d: ScoreHistory len = %d, want %d", i+1, len(g.ScoreHistory), i+1)
		}
		rs := g.ScoreHistory[i]
		if rs.Round != i+1 {
			t.Errorf("round %d: ScoreHistory[%d].Round = %d, want %d", i+1, i, rs.Round, i+1)
		}
		if rs.Scores["p1"] != 0 {
			t.Errorf("round %d: Alice's score = %d, want 0 (round winner)", i+1, rs.Scores["p1"])
		}
		// Bob's expected penalty for this round.
		wantBob := setup.bobHand.Score()
		if rs.Scores["p2"] != wantBob {
			t.Errorf("round %d: Bob's score = %d, want %d", i+1, rs.Scores["p2"], wantBob)
		}
		bobAccum += wantBob
		// Names must be captured.
		if rs.Names["p1"] != "Alice" {
			t.Errorf("round %d: Names[p1] = %q, want Alice", i+1, rs.Names["p1"])
		}

		// Advance to next round if not game over.
		if g.Phase == PhaseRoundEnd {
			if err := g.NextRound(); err != nil {
				t.Fatalf("round %d NextRound: %v", i+1, err)
			}
		}
	}
}

// TestScoreHistoryPreservedAcrossRounds verifies that calling NextRound does NOT
// clear ScoreHistory — it must survive across the whole game.
func TestScoreHistoryPreservedAcrossRounds(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "Alice"},
		{"p2", "Bob"},
	}
	g, err := NewGame(players, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Complete round 1.
	g.Players[0].Hand = Hand{c(Two, Spades)}
	g.Players[1].Hand = Hand{c(Seven, Hearts)}
	g.Phase = PhaseMelding
	g.ActiveIndex = 0
	if err := g.Discard("p1", 0); err != nil {
		t.Fatalf("round 1 discard: %v", err)
	}
	if len(g.ScoreHistory) != 1 {
		t.Fatalf("expected 1 entry after round 1, got %d", len(g.ScoreHistory))
	}
	round1 := g.ScoreHistory[0]

	if g.Phase != PhaseRoundEnd {
		t.Skip("game over after round 1, not checking preservation")
	}
	if err := g.NextRound(); err != nil {
		t.Fatalf("NextRound: %v", err)
	}
	// History must still contain round 1's entry.
	if len(g.ScoreHistory) != 1 {
		t.Fatalf("ScoreHistory should still have 1 entry after NextRound, got %d", len(g.ScoreHistory))
	}
	if g.ScoreHistory[0].Round != round1.Round {
		t.Errorf("ScoreHistory[0] changed after NextRound")
	}
}

// TestFullEventLogAccumulates verifies that FullEventLog grows across rounds
// (not cleared by startRound) and that EventLogTail returns a correctly-bounded
// subset.
func TestFullEventLogAccumulates(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "Alice"},
		{"p2", "Bob"},
	}
	g, err := NewGame(players, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Events should be seeded by startRound.
	if len(g.FullEventLog) == 0 {
		t.Fatal("FullEventLog should be non-empty after NewGame")
	}
	initialLen := len(g.FullEventLog)

	// Add a few events manually.
	g.addEvent("test-event-A")
	g.addEvent("test-event-B")
	if len(g.FullEventLog) != initialLen+2 {
		t.Fatalf("FullEventLog len = %d, want %d", len(g.FullEventLog), initialLen+2)
	}

	// Tail(1) returns the last 1 entry.
	tail1 := g.EventLogTail(1)
	if len(tail1) != 1 || tail1[0] != "test-event-B" {
		t.Errorf("EventLogTail(1) = %v, want [test-event-B]", tail1)
	}

	// Tail(0) returns all.
	tailAll := g.EventLogTail(0)
	if len(tailAll) != len(g.FullEventLog) {
		t.Errorf("EventLogTail(0) len = %d, want %d", len(tailAll), len(g.FullEventLog))
	}

	// Complete round 1 and start round 2 — FullEventLog must NOT be cleared.
	g.Players[0].Hand = Hand{c(Two, Spades)}
	g.Players[1].Hand = Hand{c(Seven, Hearts)}
	g.Phase = PhaseMelding
	g.ActiveIndex = 0
	_ = g.Discard("p1", 0)
	lenAfterRoundEnd := len(g.FullEventLog)
	if lenAfterRoundEnd <= initialLen+2 {
		t.Errorf("FullEventLog should grow at round end; got %d", lenAfterRoundEnd)
	}

	if g.Phase == PhaseRoundEnd {
		_ = g.NextRound()
		// After NextRound (which calls startRound), FullEventLog must still have
		// everything plus the new round-start event.
		if len(g.FullEventLog) < lenAfterRoundEnd {
			t.Errorf("FullEventLog shrunk after NextRound: %d < %d", len(g.FullEventLog), lenAfterRoundEnd)
		}
	}
}

func TestRoundResultJokerPenalty(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "Alice"},
		{"p2", "Bob"},
	}
	g, err := NewGame(players, 0)
	if err != nil {
		t.Fatal(err)
	}

	g.Players[0].Hand = Hand{c(Two, Spades)}
	g.Players[1].Hand = Hand{joker(0)}
	g.Phase = PhaseMelding

	_ = g.Discard("p1", 0) // Alice wins

	rr := g.LastRoundResult
	if rr == nil {
		t.Fatal("no LastRoundResult")
	}
	for _, pr := range rr.Results {
		if pr.PlayerID == "p2" {
			if pr.RoundScore != 25 {
				t.Errorf("Bob joker penalty = %d, want 25", pr.RoundScore)
			}
			if len(pr.Hand) != 1 || !pr.Hand[0].IsJoker() {
				t.Errorf("Bob's reveal hand should contain 1 joker, got %v", pr.Hand)
			}
		}
	}
}
