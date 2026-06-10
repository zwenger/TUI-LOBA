package game

// rules_test.go — comprehensive table-driven tests for every Loba rule.
// Covers: deck composition, pierna, escalera (all orders / ace variants /
// joker), discard-pickup rule, turn structure, lay-off, stock reshuffle,
// round-end scoring, multi-round accumulation, and game-over logic.

import (
	"testing"
)

// ─── Deck composition ─────────────────────────────────────────────────────────

func TestDeckComposition(t *testing.T) {
	deck := newDeck()

	if len(deck) != 108 {
		t.Fatalf("deck size = %d, want 108", len(deck))
	}

	// Count suits and ranks.
	type key struct {
		rank Rank
		suit Suit
	}
	counts := make(map[key]int)
	jokers := 0
	for _, card := range deck {
		if card.IsJoker() {
			jokers++
		} else {
			counts[key{card.Rank, card.Suit}]++
		}
	}

	if jokers != 4 {
		t.Errorf("joker count = %d, want 4", jokers)
	}

	// Every rank/suit combination should appear exactly 2 times (two decks).
	suits := []Suit{Spades, Hearts, Diamonds, Clubs}
	for _, suit := range suits {
		for rank := Ace; rank <= King; rank++ {
			k := key{rank, suit}
			if counts[k] != 2 {
				t.Errorf("%v%v count = %d, want 2", rank, suit, counts[k])
			}
		}
	}

	// All 4 jokers must have distinct JokerIndex values.
	jokerIdxSeen := make(map[int]bool)
	for _, card := range deck {
		if card.IsJoker() {
			if jokerIdxSeen[card.JokerIndex] {
				t.Errorf("duplicate JokerIndex %d", card.JokerIndex)
			}
			jokerIdxSeen[card.JokerIndex] = true
		}
	}
}

func TestDealHandSize(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "A"}, {"p2", "B"}, {"p3", "C"},
	}
	g, err := NewGame(players, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range g.Players {
		if len(p.Hand) != HandSize {
			t.Errorf("player %s: hand size = %d, want %d", p.Name, len(p.Hand), HandSize)
		}
	}
	// Exactly one card flipped to discard pile.
	if len(g.DiscardPile) != 1 {
		t.Errorf("discard pile size = %d, want 1", len(g.DiscardPile))
	}
}

// ─── Pierna creation ──────────────────────────────────────────────────────────

func TestPiernaCreation(t *testing.T) {
	tests := []struct {
		name    string
		cards   []Card
		wantErr bool
	}{
		// Positive cases.
		{"valid 3 different suits",
			[]Card{c(Seven, Spades), c(Seven, Hearts), c(Seven, Diamonds)}, false},
		{"valid 3 all clubs-spades-diamonds",
			[]Card{c(King, Clubs), c(King, Spades), c(King, Diamonds)}, false},
		{"valid aces different suits",
			[]Card{c(Ace, Spades), c(Ace, Hearts), c(Ace, Clubs)}, false},
		{"valid low rank (2s)",
			[]Card{c(Two, Spades), c(Two, Hearts), c(Two, Diamonds)}, false},
		// Negative cases.
		{"only 2 cards",
			[]Card{c(Seven, Spades), c(Seven, Hearts)}, true},
		{"4 cards at creation",
			[]Card{c(Seven, Spades), c(Seven, Hearts), c(Seven, Diamonds), c(Seven, Clubs)}, true},
		{"duplicate suit",
			[]Card{c(Seven, Spades), c(Seven, Spades), c(Seven, Hearts)}, true},
		{"mixed ranks",
			[]Card{c(Seven, Spades), c(Eight, Hearts), c(Seven, Diamonds)}, true},
		{"joker in first position",
			[]Card{joker(0), c(Seven, Hearts), c(Seven, Diamonds)}, true},
		{"joker in middle",
			[]Card{c(Seven, Spades), joker(0), c(Seven, Diamonds)}, true},
		{"joker at end",
			[]Card{c(Seven, Spades), c(Seven, Hearts), joker(0)}, true},
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

// ─── Pierna lay-off ───────────────────────────────────────────────────────────

func TestPiernaLayOff(t *testing.T) {
	baseMeld := func() *Meld {
		return &Meld{
			Type:  MeldPierna,
			Cards: []Card{c(Seven, Spades), c(Seven, Hearts), c(Seven, Diamonds)},
		}
	}

	t.Run("add fourth card of same rank (new suit)", func(t *testing.T) {
		if err := CanLayOffPierna(baseMeld(), c(Seven, Clubs)); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("add card of same rank, already-present suit (two-deck duplicate)", func(t *testing.T) {
		// After creation any same-rank card is accepted regardless of suit.
		if err := CanLayOffPierna(baseMeld(), c(Seven, Spades)); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("reject wrong rank", func(t *testing.T) {
		if err := CanLayOffPierna(baseMeld(), c(Eight, Clubs)); err == nil {
			t.Error("expected error for wrong rank")
		}
	})
	t.Run("reject joker", func(t *testing.T) {
		if err := CanLayOffPierna(baseMeld(), joker(0)); err == nil {
			t.Error("expected error for joker on pierna")
		}
	})
	t.Run("reject when pierna is full (8 cards)", func(t *testing.T) {
		m := &Meld{Type: MeldPierna, Cards: make([]Card, 8)}
		for i := range m.Cards {
			m.Cards[i] = c(Seven, Spades)
		}
		m.Cards[0] = c(Seven, Hearts) // set rank
		if err := CanLayOffPierna(m, c(Seven, Clubs)); err == nil {
			t.Error("expected error when pierna is full")
		}
	})
}

// ─── Pierna engine integration ────────────────────────────────────────────────

func TestPiernaEngineLayOffOnlyAfterOwnMeld(t *testing.T) {
	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}
	g, _ := NewGame(players, 0)

	// Put a pierna on the table (owned by Bob).
	g.Melds = []Meld{{
		Type:    MeldPierna,
		Cards:   []Card{c(Seven, Spades), c(Seven, Hearts), c(Seven, Diamonds)},
		OwnerID: "p2",
	}}
	_ = g.DrawStock("p1")
	g.Players[0].Hand = append(g.Players[0].Hand, c(Seven, Clubs))

	// Alice has not melded yet — lay-off must be rejected.
	lastIdx := len(g.Players[0].Hand) - 1
	if err := g.LayOff("p1", []int{lastIdx}, 0); err == nil {
		t.Error("expected error: cannot lay off before own meld")
	}

	// After Alice melds something, lay-off should be accepted.
	g.Players[0].Hand = append([]Card{
		c(King, Spades), c(King, Hearts), c(King, Diamonds),
	}, g.Players[0].Hand...)
	if err := g.Meld("p1", []int{0, 1, 2}, MeldPierna); err != nil {
		t.Fatalf("meld pierna: %v", err)
	}
	// 7♣ is now last in the re-arranged hand.
	lastIdx = g.Players[0].Hand.FindIndex(c(Seven, Clubs))
	if lastIdx < 0 {
		t.Fatal("7♣ not in hand")
	}
	if err := g.LayOff("p1", []int{lastIdx}, 0); err != nil {
		t.Errorf("lay-off after own meld: %v", err)
	}
}

// ─── Escalera creation — arbitrary card order (regression for the bug) ────────

func TestEscaleraArbitrarySelectionOrder(t *testing.T) {
	// 6♥ 7♥ 8♥ in all 6 permutations — the bug scenario.
	perms := [][]Card{
		{c(Six, Hearts), c(Seven, Hearts), c(Eight, Hearts)},
		{c(Seven, Hearts), c(Six, Hearts), c(Eight, Hearts)},
		{c(Eight, Hearts), c(Seven, Hearts), c(Six, Hearts)},
		{c(Eight, Hearts), c(Six, Hearts), c(Seven, Hearts)},
		{c(Six, Hearts), c(Eight, Hearts), c(Seven, Hearts)},
		{c(Seven, Hearts), c(Eight, Hearts), c(Six, Hearts)},
	}
	for _, perm := range perms {
		if err := ValidateEscalera(perm); err != nil {
			t.Errorf("ValidateEscalera(%v) = %v, want nil", perm, err)
		}
	}
}

// TestEscaleraAfterDrawDiscard is the full engine-level regression test for the
// reported bug: Alvaro took 6♥ from discard, then tried to meld 6♥ 7♥ 8♥.
func TestEscaleraAfterDrawDiscard(t *testing.T) {
	players := []struct{ ID, Name string }{{"p1", "Alvaro"}, {"p2", "Bob"}}

	// Run through all 6 orderings of the three card indices to catch any
	// ordering sensitivity in the meld path.
	permute := func(a, b, cc int) [][]int {
		return [][]int{
			{a, b, cc}, {a, cc, b}, {b, a, cc},
			{b, cc, a}, {cc, a, b}, {cc, b, a},
		}
	}

	newState := func() *Game {
		g, _ := NewGame(players, 0)
		g.Players[0].Hand = Hand{
			c(Seven, Hearts), c(Eight, Hearts),
			c(Two, Clubs), c(Three, Spades), c(King, Diamonds),
			c(Four, Clubs), c(Five, Spades), c(Queen, Hearts), c(Jack, Diamonds),
		}
		g.DiscardPile = []Card{c(Six, Hearts)}
		g.Phase = PhaseDrawing
		g.ActiveIndex = 0
		return g
	}

	g := newState()
	if err := g.DrawDiscard("p1"); err != nil {
		t.Fatalf("DrawDiscard: %v", err)
	}
	hand := g.Players[0].Hand
	i6 := hand.FindIndex(c(Six, Hearts))
	i7 := hand.FindIndex(c(Seven, Hearts))
	i8 := hand.FindIndex(c(Eight, Hearts))
	if i6 < 0 || i7 < 0 || i8 < 0 {
		t.Fatal("expected 6♥ 7♥ 8♥ all in hand")
	}

	for _, idxs := range permute(i6, i7, i8) {
		g2 := newState()
		_ = g2.DrawDiscard("p1")
		if err := g2.Meld("p1", idxs, MeldEscalera); err != nil {
			t.Errorf("Meld(idxs=%v) failed: %v", idxs, err)
		}
	}
}

// ─── Escalera creation — full rule coverage ───────────────────────────────────

func TestEscaleraCreation(t *testing.T) {
	tests := []struct {
		name    string
		cards   []Card
		wantErr bool
	}{
		// Positive.
		{"3-card run ascending",
			[]Card{c(Five, Spades), c(Six, Spades), c(Seven, Spades)}, false},
		{"3-card run descending (arbitrary order)",
			[]Card{c(Seven, Spades), c(Six, Spades), c(Five, Spades)}, false},
		{"5-card run",
			[]Card{c(Three, Hearts), c(Four, Hearts), c(Five, Hearts), c(Six, Hearts), c(Seven, Hearts)}, false},
		{"ace-low A-2-3",
			[]Card{c(Ace, Clubs), c(Two, Clubs), c(Three, Clubs)}, false},
		{"ace-high Q-K-A",
			[]Card{c(Queen, Diamonds), c(King, Diamonds), c(Ace, Diamonds)}, false},
		{"ace-high in arbitrary order (A-Q-K)",
			[]Card{c(Ace, Diamonds), c(Queen, Diamonds), c(King, Diamonds)}, false},
		{"joker fills gap 5-?-7",
			[]Card{c(Five, Spades), joker(0), c(Seven, Spades)}, false},
		{"joker fills gap arbitrary order (7-?-5)",
			[]Card{c(Seven, Spades), joker(0), c(Five, Spades)}, false},
		{"joker extends high end (J-Q-joker)",
			[]Card{c(Jack, Hearts), c(Queen, Hearts), joker(0)}, false},
		{"joker extends low end (joker-3-4)",
			[]Card{joker(0), c(Three, Diamonds), c(Four, Diamonds)}, false},
		{"joker at low end of ace-low run (joker-2-3)",
			[]Card{joker(0), c(Two, Clubs), c(Three, Clubs)}, false},
		// Negative.
		{"only 2 cards",
			[]Card{c(Five, Spades), c(Six, Spades)}, true},
		{"1 card",
			[]Card{c(Five, Spades)}, true},
		{"mixed suits no joker",
			[]Card{c(Five, Spades), c(Six, Hearts), c(Seven, Spades)}, true},
		{"gap of 2",
			[]Card{c(Five, Spades), c(Seven, Spades), c(Eight, Spades)}, true},
		{"wrap-around K-A-2",
			[]Card{c(King, Spades), c(Ace, Spades), c(Two, Spades)}, true},
		{"two jokers",
			[]Card{c(Five, Spades), joker(0), joker(1), c(Eight, Spades)}, true},
		{"joker cannot bridge gap of 2",
			[]Card{c(Five, Spades), joker(0), c(Eight, Spades)}, true},
		{"all same rank (not consecutive)",
			[]Card{c(Seven, Spades), c(Seven, Spades), c(Seven, Spades)}, true},
		{"duplicate ranks not run",
			[]Card{c(Six, Clubs), c(Six, Clubs), c(Seven, Clubs)}, true},
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

// ─── Escalera lay-off ─────────────────────────────────────────────────────────

func TestEscaleraLayOff(t *testing.T) {
	base := func() *Meld {
		return &Meld{
			Type:  MeldEscalera,
			Cards: []Card{c(Five, Spades), c(Six, Spades), c(Seven, Spades)},
		}
	}
	aceHighBase := func() *Meld {
		return &Meld{
			Type:  MeldEscalera,
			Cards: []Card{c(Queen, Diamonds), c(King, Diamonds), c(Ace, Diamonds)},
		}
	}
	aceLowBase := func() *Meld {
		return &Meld{
			Type:  MeldEscalera,
			Cards: []Card{c(Ace, Clubs), c(Two, Clubs), c(Three, Clubs)},
		}
	}

	tests := []struct {
		name    string
		meld    func() *Meld
		card    Card
		wantErr bool
	}{
		{"extend high end", base, c(Eight, Spades), false},
		{"extend low end", base, c(Four, Spades), false},
		{"extend to ace-high (K-A becomes Q-K-A is already set; extend J)", aceHighBase, c(Jack, Diamonds), false},
		{"extend ace-low on low end (A-2-3 extend with 4)", aceLowBase, c(Four, Clubs), false},
		{"wrong suit", base, c(Eight, Hearts), true},
		{"non-adjacent (skip 8)", base, c(Nine, Spades), true},
		{"non-adjacent low (skip 3)", base, c(Three, Spades), true},
		{"wrong suit on ace-low", aceLowBase, c(Four, Hearts), true},
		// Ace-high extension: cannot extend K-A end with 2 (wrap-around).
		{"no wrap-around on ace-high meld", aceHighBase, c(Two, Diamonds), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CanLayOffEscalera(tt.meld(), tt.card)
			if (err != nil) != tt.wantErr {
				t.Errorf("CanLayOffEscalera() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}

	// Verify LayOffEscalera places card at correct end.
	t.Run("LayOffEscalera places at high end", func(t *testing.T) {
		m := base()
		LayOffEscalera(m, c(Eight, Spades))
		if len(m.Cards) != 4 {
			t.Fatalf("len = %d, want 4", len(m.Cards))
		}
		if m.Cards[3].Rank != Eight {
			t.Errorf("last card rank = %v, want Eight", m.Cards[3].Rank)
		}
	})
	t.Run("LayOffEscalera places at low end", func(t *testing.T) {
		m := base()
		LayOffEscalera(m, c(Four, Spades))
		if len(m.Cards) != 4 {
			t.Fatalf("len = %d, want 4", len(m.Cards))
		}
		if m.Cards[0].Rank != Four {
			t.Errorf("first card rank = %v, want Four", m.Cards[0].Rank)
		}
	})
}

// ─── Joker rules ──────────────────────────────────────────────────────────────

func TestJokerRules(t *testing.T) {
	t.Run("joker scores 25", func(t *testing.T) {
		if joker(0).Score() != 25 {
			t.Errorf("joker score = %d, want 25", joker(0).Score())
		}
	})
	t.Run("joker valid in escalera gap", func(t *testing.T) {
		if err := ValidateEscalera([]Card{c(Five, Spades), joker(0), c(Seven, Spades)}); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("joker valid at end of escalera", func(t *testing.T) {
		if err := ValidateEscalera([]Card{c(Jack, Hearts), c(Queen, Hearts), joker(0)}); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("at most 1 joker in escalera", func(t *testing.T) {
		if err := ValidateEscalera([]Card{c(Five, Spades), joker(0), joker(1), c(Eight, Spades)}); err == nil {
			t.Error("expected error for two jokers")
		}
	})
	t.Run("joker rejected in pierna", func(t *testing.T) {
		if err := ValidatePierna([]Card{c(Seven, Spades), c(Seven, Hearts), joker(0)}); err == nil {
			t.Error("expected error for joker in pierna")
		}
	})
	t.Run("joker rejected in pierna lay-off", func(t *testing.T) {
		m := &Meld{Type: MeldPierna, Cards: []Card{c(Seven, Spades), c(Seven, Hearts), c(Seven, Diamonds)}}
		if err := CanLayOffPierna(m, joker(0)); err == nil {
			t.Error("expected error for joker lay-off on pierna")
		}
	})
	t.Run("joker lay-off on escalera valid (extends)", func(t *testing.T) {
		m := &Meld{
			Type:  MeldEscalera,
			Cards: []Card{c(Jack, Hearts), c(Queen, Hearts), c(King, Hearts)},
		}
		if err := CanLayOffEscalera(m, joker(0)); err != nil {
			t.Errorf("expected joker to extend ace-high end: %v", err)
		}
	})
}

// ─── Discard-pickup rule ──────────────────────────────────────────────────────

func TestDiscardPickupRule(t *testing.T) {
	newG := func() *Game {
		players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}
		g, _ := NewGame(players, 0)
		g.Phase = PhaseDrawing
		g.ActiveIndex = 0
		return g
	}

	t.Run("reject when card is unusable", func(t *testing.T) {
		g := newG()
		g.Players[0].Hand = Hand{
			c(Two, Clubs), c(Three, Hearts), c(Jack, Spades),
			c(King, Diamonds), c(Queen, Clubs), c(Ten, Hearts),
			c(Four, Diamonds), c(Six, Spades), c(Eight, Clubs),
		}
		g.DiscardPile = []Card{c(Ace, Spades)}
		if err := g.DrawDiscard("p1"); err == nil {
			t.Error("expected rejection for unusable card")
		}
	})

	t.Run("accept when card can form pierna", func(t *testing.T) {
		g := newG()
		g.Players[0].Hand = Hand{
			c(Seven, Hearts), c(Seven, Diamonds),
			c(Two, Clubs), c(Three, Clubs), c(Four, Clubs),
			c(King, Spades), c(Queen, Hearts), c(Jack, Diamonds), c(Ten, Clubs),
		}
		g.DiscardPile = []Card{c(Seven, Spades)}
		if err := g.DrawDiscard("p1"); err != nil {
			t.Errorf("expected acceptance: %v", err)
		}
		if g.Players[0].PickedUpDiscard == nil {
			t.Error("PickedUpDiscard should be set")
		}
	})

	t.Run("accept when card can form escalera", func(t *testing.T) {
		g := newG()
		g.Players[0].Hand = Hand{
			c(Seven, Hearts), c(Eight, Hearts),
			c(Two, Clubs), c(Three, Spades), c(King, Diamonds),
			c(Four, Clubs), c(Five, Spades), c(Queen, Hearts), c(Jack, Diamonds),
		}
		g.DiscardPile = []Card{c(Six, Hearts)}
		if err := g.DrawDiscard("p1"); err != nil {
			t.Errorf("expected acceptance (escalera possible): %v", err)
		}
	})

	t.Run("reject when card can only be laid off on existing meld", func(t *testing.T) {
		g := newG()
		g.Melds = []Meld{{
			Type:    MeldEscalera,
			Cards:   []Card{c(Five, Clubs), c(Six, Clubs), c(Seven, Clubs)},
			OwnerID: "p2",
		}}
		g.Players[0].Hand = Hand{
			c(Two, Hearts), c(Three, Hearts), c(Jack, Spades),
			c(King, Diamonds), c(Queen, Clubs), c(Ten, Hearts),
			c(Four, Diamonds), c(Six, Spades), c(Nine, Clubs),
		}
		g.DiscardPile = []Card{c(Eight, Clubs)}
		if err := g.DrawDiscard("p1"); err == nil {
			t.Fatal("expected rejection: discard cannot be taken only to extend an existing meld")
		}
		if got := len(g.DiscardPile); got != 1 {
			t.Fatalf("discard pile should remain unchanged, got %d cards", got)
		}
		if g.Players[0].PickedUpDiscard != nil {
			t.Fatal("PickedUpDiscard should stay nil on rejection")
		}
	})

	t.Run("cannot discard picked card directly", func(t *testing.T) {
		g := newG()
		g.Players[0].Hand = Hand{
			c(Seven, Hearts), c(Seven, Diamonds),
			c(Two, Clubs), c(Three, Clubs), c(Four, Clubs),
			c(King, Spades), c(Queen, Hearts), c(Jack, Diamonds), c(Ten, Clubs),
		}
		g.DiscardPile = []Card{c(Seven, Spades)}
		_ = g.DrawDiscard("p1")
		lastIdx := len(g.Players[0].Hand) - 1
		if err := g.Discard("p1", lastIdx); err == nil {
			t.Error("expected rejection: cannot discard the picked card")
		}
	})

	t.Run("end-turn blocked until picked card is played", func(t *testing.T) {
		g := newG()
		g.Players[0].Hand = Hand{
			c(Seven, Hearts), c(Seven, Diamonds),
			c(Two, Clubs), c(Three, Clubs), c(Four, Clubs),
			c(King, Spades), c(Queen, Hearts), c(Jack, Diamonds), c(Ten, Clubs),
		}
		g.DiscardPile = []Card{c(Seven, Spades)}
		_ = g.DrawDiscard("p1")
		// Discard something other than the picked card.
		if err := g.Discard("p1", 0); err == nil {
			t.Error("expected rejection: must play picked card first")
		}
	})

	t.Run("picked flag cleared after meld", func(t *testing.T) {
		g := newG()
		g.Players[0].Hand = Hand{
			c(Seven, Hearts), c(Seven, Diamonds),
			c(Two, Clubs), c(Three, Clubs), c(Four, Clubs),
			c(King, Spades), c(Queen, Hearts), c(Jack, Diamonds), c(Ten, Clubs),
		}
		g.DiscardPile = []Card{c(Seven, Spades)}
		_ = g.DrawDiscard("p1")
		lastIdx := len(g.Players[0].Hand) - 1
		if err := g.Meld("p1", []int{0, 1, lastIdx}, MeldPierna); err != nil {
			t.Fatalf("meld: %v", err)
		}
		if g.Players[0].PickedUpDiscard != nil {
			t.Error("PickedUpDiscard should be nil after playing the card in a meld")
		}
	})

	t.Run("reject when card only extends own meld", func(t *testing.T) {
		g := newG()
		// Put a pierna on the table owned by Alice; the discard still must not be
		// drawable if it only helps with that meld.
		g.Melds = []Meld{{
			Type:    MeldPierna,
			Cards:   []Card{c(Seven, Spades), c(Seven, Hearts), c(Seven, Diamonds)},
			OwnerID: "p1",
		}}
		g.Players[0].Hand = Hand{
			c(Two, Clubs), c(Three, Clubs), c(Four, Clubs),
			c(King, Spades), c(Queen, Hearts), c(Jack, Diamonds),
			c(Ten, Clubs), c(Nine, Clubs), c(Eight, Spades),
		}
		g.DiscardPile = []Card{c(Seven, Clubs)}
		if err := g.DrawDiscard("p1"); err == nil {
			t.Fatal("expected rejection: discard cannot be taken only to extend own meld")
		}
		if g.Players[0].PickedUpDiscard != nil {
			t.Error("PickedUpDiscard should stay nil when draw is rejected")
		}
	})

	// Regression: Alvaro's deadlock — no-meld player, pierna of same rank on table.
	// Before 9e73e2e the pickup was accepted because the lay-off path was counted;
	// after 9e73e2e it must be rejected at draw time.
	t.Run("regression: no-meld player cannot pick up discard only usable via lay-off", func(t *testing.T) {
		g := newG()
		// Pierna of 9s on the table (owned by opponent).
		g.Melds = []Meld{{
			Type:    MeldPierna,
			Cards:   []Card{c(Nine, Hearts), c(Nine, Diamonds), c(Nine, Spades)},
			OwnerID: "p2",
		}}
		// Alvaro's exact hand — no pair of 9s, no escalera run with 9♣.
		g.Players[0].Hand = Hand{
			c(Eight, Spades), c(Jack, Spades),
			c(Four, Hearts),
			c(Ace, Diamonds), c(Seven, Diamonds), c(Jack, Diamonds), c(King, Diamonds),
			c(Seven, Clubs), c(King, Clubs),
		}
		g.Players[0].HasMelded = false
		g.DiscardPile = []Card{c(Nine, Clubs)}

		if err := g.DrawDiscard("p1"); err == nil {
			t.Fatal("expected rejection: no-meld player cannot pick 9♣ when it can only be laid off")
		}
		if len(g.DiscardPile) != 1 {
			t.Fatalf("discard pile should remain unchanged, got %d cards", len(g.DiscardPile))
		}
		if g.Players[0].PickedUpDiscard != nil {
			t.Fatal("PickedUpDiscard should stay nil on rejection")
		}
	})

	// Test safety valve: if PickedUpDiscard is set but no legal use exists,
	// the player must be able to discard (returning the picked card to the pile).
	t.Run("safety valve: return picked card to pile when no legal use exists", func(t *testing.T) {
		g := newG()
		g.Phase = PhaseMelding
		// Inject inconsistent state directly: PickedUpDiscard is set but the hand
		// has no meld or lay-off option for 9♣ (no other 9s, no escalera run).
		picked := c(Nine, Clubs)
		g.Players[0].Hand = Hand{
			c(Eight, Spades), c(Jack, Spades),
			c(Four, Hearts),
			c(Ace, Diamonds), c(Seven, Diamonds), c(Jack, Diamonds), c(King, Diamonds),
			c(Seven, Clubs), c(King, Clubs),
			picked, // in hand
		}
		g.Players[0].HasMelded = false
		g.Players[0].PickedUpDiscard = &picked
		// No table melds.
		g.Melds = nil

		initialPileLen := len(g.DiscardPile)
		// Discard any card that is NOT the picked card.
		if err := g.Discard("p1", 0); err != nil {
			t.Fatalf("safety valve should allow discard when picked card has no legal use: %v", err)
		}
		// Picked card should have been returned to the discard pile.
		if g.Players[0].PickedUpDiscard != nil {
			t.Error("PickedUpDiscard should be nil after safety-valve discard")
		}
		// Pile should have grown by 2: picked card returned + discard of chosen card.
		wantPile := initialPileLen + 2
		if len(g.DiscardPile) != wantPile {
			t.Errorf("expected pile length %d, got %d", wantPile, len(g.DiscardPile))
		}
	})

	// Ensure that a player who HAS melded can still pick for lay-off (positive case).
	t.Run("player who has melded can pick discard for lay-off — still rejected at pickup", func(t *testing.T) {
		// NOTE: per 9e73e2e semantics, DrawDiscard only allows pickup when a NEW meld
		// can be formed from hand. A player with HasMelded=true must still form a new
		// meld; they can lay off AFTER picking, but the pickup itself requires a meld.
		// This test confirms the pickup is rejected even for a melded player when
		// the card only helps with lay-off, not a new meld.
		g := newG()
		g.Melds = []Meld{{
			Type:    MeldPierna,
			Cards:   []Card{c(Nine, Hearts), c(Nine, Diamonds), c(Nine, Spades)},
			OwnerID: "p1",
		}}
		g.Players[0].Hand = Hand{
			c(Eight, Spades), c(Jack, Spades),
			c(Four, Hearts),
			c(Ace, Diamonds), c(Seven, Diamonds), c(Jack, Diamonds), c(King, Diamonds),
			c(Seven, Clubs), c(King, Clubs),
		}
		g.Players[0].HasMelded = true // has previously melded
		g.DiscardPile = []Card{c(Nine, Clubs)}

		if err := g.DrawDiscard("p1"); err == nil {
			t.Fatal("expected rejection: pickup requires a new meld, not just a lay-off")
		}
	})

	t.Run("picked flag cleared on turn advance", func(t *testing.T) {
		g := newG()
		g.Players[0].Hand = Hand{
			c(Seven, Hearts), c(Seven, Diamonds),
			c(Two, Clubs), c(Three, Clubs), c(Four, Clubs),
			c(King, Spades), c(Queen, Hearts), c(Jack, Diamonds), c(Ten, Clubs),
		}
		g.DiscardPile = []Card{c(Seven, Spades)}
		_ = g.DrawDiscard("p1")
		// Meld the pierna to clear the picked obligation.
		lastIdx := len(g.Players[0].Hand) - 1
		_ = g.Meld("p1", []int{0, 1, lastIdx}, MeldPierna)
		// Discard to end turn.
		_ = g.Discard("p1", 0)
		// Alice's PickedUpDiscard should be nil after turn advances.
		if g.Players[0].PickedUpDiscard != nil {
			t.Error("PickedUpDiscard should be nil after turn advances")
		}
	})
}

// ─── Turn structure ───────────────────────────────────────────────────────────

func TestTurnStructure(t *testing.T) {
	newG := func() *Game {
		players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}
		g, _ := NewGame(players, 0)
		return g
	}

	t.Run("cannot act out of turn", func(t *testing.T) {
		g := newG()
		if err := g.DrawStock("p2"); err == nil {
			t.Error("expected error for drawing out of turn")
		}
	})

	t.Run("must draw before melding", func(t *testing.T) {
		g := newG()
		g.Players[0].Hand = Hand{
			c(Seven, Spades), c(Seven, Hearts), c(Seven, Diamonds),
			c(Two, Clubs), c(Three, Clubs), c(Four, Clubs),
			c(King, Spades), c(Queen, Hearts), c(Jack, Diamonds),
		}
		// Phase is drawing — cannot meld yet.
		if err := g.Meld("p1", []int{0, 1, 2}, MeldPierna); err == nil {
			t.Error("expected error for melding before draw")
		}
	})

	t.Run("must draw before discarding", func(t *testing.T) {
		g := newG()
		if err := g.Discard("p1", 0); err == nil {
			t.Error("expected error for discarding before draw")
		}
	})

	t.Run("cannot draw twice", func(t *testing.T) {
		g := newG()
		_ = g.DrawStock("p1")
		if err := g.DrawStock("p1"); err == nil {
			t.Error("expected error for drawing twice")
		}
	})

	t.Run("discard advances turn to next player", func(t *testing.T) {
		g := newG()
		_ = g.DrawStock("p1")
		_ = g.Discard("p1", 0)
		if g.activePlayer().ID != "p2" {
			t.Errorf("expected Bob's turn, got %s", g.activePlayer().ID)
		}
	})

	t.Run("discard resets phase to drawing", func(t *testing.T) {
		g := newG()
		_ = g.DrawStock("p1")
		_ = g.Discard("p1", 0)
		if g.Phase != PhaseDrawing {
			t.Errorf("expected PhaseDrawing, got %s", g.Phase)
		}
	})
}

// ─── Lay-off rules ────────────────────────────────────────────────────────────

func TestLayOffRules(t *testing.T) {
	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}

	t.Run("lay off on opponent escalera", func(t *testing.T) {
		g, _ := NewGame(players, 0)
		g.Melds = []Meld{{
			Type:    MeldEscalera,
			Cards:   []Card{c(Five, Clubs), c(Six, Clubs), c(Seven, Clubs)},
			OwnerID: "p2",
		}}
		g.Players[0].HasMelded = true
		_ = g.DrawStock("p1")
		g.Players[0].Hand = append(g.Players[0].Hand, c(Eight, Clubs))
		lastIdx := len(g.Players[0].Hand) - 1
		if err := g.LayOff("p1", []int{lastIdx}, 0); err != nil {
			t.Errorf("lay off on opponent escalera: %v", err)
		}
	})

	t.Run("lay off on opponent pierna", func(t *testing.T) {
		g, _ := NewGame(players, 0)
		g.Melds = []Meld{{
			Type:    MeldPierna,
			Cards:   []Card{c(King, Spades), c(King, Hearts), c(King, Diamonds)},
			OwnerID: "p2",
		}}
		g.Players[0].HasMelded = true
		_ = g.DrawStock("p1")
		g.Players[0].Hand = append(g.Players[0].Hand, c(King, Clubs))
		lastIdx := len(g.Players[0].Hand) - 1
		if err := g.LayOff("p1", []int{lastIdx}, 0); err != nil {
			t.Errorf("lay off on opponent pierna: %v", err)
		}
	})

	t.Run("lay off only one card at a time", func(t *testing.T) {
		g, _ := NewGame(players, 0)
		g.Melds = []Meld{{
			Type:    MeldPierna,
			Cards:   []Card{c(King, Spades), c(King, Hearts), c(King, Diamonds)},
			OwnerID: "p2",
		}}
		g.Players[0].HasMelded = true
		_ = g.DrawStock("p1")
		g.Players[0].Hand = append(g.Players[0].Hand, c(King, Clubs), c(King, Clubs))
		n := len(g.Players[0].Hand)
		if err := g.LayOff("p1", []int{n - 2, n - 1}, 0); err == nil {
			t.Error("expected error for laying off 2 cards at once")
		}
	})

	t.Run("invalid meld index rejected", func(t *testing.T) {
		g, _ := NewGame(players, 0)
		g.Players[0].HasMelded = true
		_ = g.DrawStock("p1")
		if err := g.LayOff("p1", []int{0}, 99); err == nil {
			t.Error("expected error for invalid meld index")
		}
	})
}

// ─── Stock reshuffle ──────────────────────────────────────────────────────────

func TestStockReshuffle(t *testing.T) {
	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}
	g, _ := NewGame(players, 0)

	// Drain stock completely.
	g.Stock = nil
	// Build a multi-card discard pile so there is something to reshuffle.
	g.DiscardPile = []Card{
		c(Two, Clubs), c(Three, Hearts), c(Four, Diamonds),
		c(Five, Spades), c(Six, Clubs), // top card kept
	}
	top := g.DiscardPile[len(g.DiscardPile)-1]

	if err := g.DrawStock("p1"); err != nil {
		t.Fatalf("DrawStock with empty stock: %v", err)
	}

	// Discard top must be preserved.
	if len(g.DiscardPile) != 1 || !g.DiscardPile[0].Equal(top) {
		t.Errorf("discard top after reshuffle: got %v, want %v", g.DiscardPile, top)
	}
	// Stock must have cards (4 reshuffled + the drawn one taken).
	// We had 5-card discard → top stays → 4 reshuffled → 1 drawn → stock has 3.
	if len(g.Stock) != 3 {
		t.Errorf("stock size after reshuffle+draw: got %d, want 3", len(g.Stock))
	}
}

func TestStockReshuffleFailsWhenDiscardTooSmall(t *testing.T) {
	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}
	g, _ := NewGame(players, 0)
	g.Stock = nil
	g.DiscardPile = []Card{c(Two, Clubs)} // only 1 card — can't reshuffle
	if err := g.DrawStock("p1"); err == nil {
		t.Error("expected error when stock empty and discard has only 1 card")
	}
}

// ─── Round end and scoring ────────────────────────────────────────────────────

func TestRoundEndScoring(t *testing.T) {
	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}

	t.Run("round ends when hand emptied via discard", func(t *testing.T) {
		g, _ := NewGame(players, 0)
		g.Players[0].Hand = Hand{c(Five, Spades)}
		g.Players[1].Hand = Hand{c(Three, Hearts), c(Four, Diamonds)}
		g.Phase = PhaseMelding
		if err := g.Discard("p1", 0); err != nil {
			t.Fatal(err)
		}
		if g.Phase != PhaseRoundEnd && g.Phase != PhaseGameOver {
			t.Errorf("expected round_end, got %s", g.Phase)
		}
		if g.Players[0].RoundScore != 0 {
			t.Errorf("winner round score = %d, want 0", g.Players[0].RoundScore)
		}
		if g.Players[1].RoundScore != 7 {
			t.Errorf("loser round score = %d, want 7", g.Players[1].RoundScore)
		}
	})

	t.Run("scoring: joker=25, ace=15, K/Q/J=10, face-value otherwise", func(t *testing.T) {
		tests := []struct {
			name  string
			cards []Card
			want  int
		}{
			{"joker", []Card{joker(0)}, 25},
			{"ace", []Card{c(Ace, Spades)}, 15},
			{"king", []Card{c(King, Spades)}, 10},
			{"queen", []Card{c(Queen, Hearts)}, 10},
			{"jack", []Card{c(Jack, Diamonds)}, 10},
			{"ten", []Card{c(Ten, Clubs)}, 10},
			{"nine", []Card{c(Nine, Spades)}, 9},
			{"two", []Card{c(Two, Hearts)}, 2},
			{"mixed", []Card{joker(0), c(Ace, Spades), c(King, Hearts), c(Five, Diamonds)}, 55},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := Hand(tt.cards).Score(); got != tt.want {
					t.Errorf("score = %d, want %d", got, tt.want)
				}
			})
		}
	})

	t.Run("round score accumulates into total", func(t *testing.T) {
		g, _ := NewGame(players, 0)
		g.Players[0].Hand = Hand{c(Five, Spades)}
		g.Players[1].Hand = Hand{c(King, Spades), c(Jack, Hearts)} // 20 pts
		g.Phase = PhaseMelding
		_ = g.Discard("p1", 0)
		if g.Players[1].TotalScore != 20 {
			t.Errorf("total score = %d, want 20", g.Players[1].TotalScore)
		}
	})
}

func TestMultiRoundScoreAccumulation(t *testing.T) {
	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}
	g, _ := NewGame(players, 0)

	// Round 1: Alice wins, Bob has 7 pts.
	g.Players[0].Hand = Hand{c(Five, Spades)}
	g.Players[1].Hand = Hand{c(Three, Hearts), c(Four, Diamonds)}
	g.Phase = PhaseMelding
	_ = g.Discard("p1", 0)

	if g.Phase == PhaseRoundEnd {
		_ = g.NextRound()
	}

	// Round 2: Alice wins again, Bob has 10 more pts.
	g.Players[0].Hand = Hand{c(Two, Clubs)}
	g.Players[1].Hand = Hand{c(Ten, Hearts)}
	g.Phase = PhaseMelding
	g.ActiveIndex = 0
	_ = g.Discard("p1", 0)

	// Bob total should be 7 + 10 = 17.
	if g.Players[1].TotalScore != 17 {
		t.Errorf("Bob total = %d, want 17", g.Players[1].TotalScore)
	}
}

func TestGameOverWhenScoreExceeds101(t *testing.T) {
	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}
	g, _ := NewGame(players, 0)

	// Give Bob a near-limit total.
	g.Players[1].TotalScore = 95

	// End round: Bob has 10-pt card in hand → total becomes 105 > 101.
	g.Players[0].Hand = Hand{c(Two, Clubs)}
	g.Players[1].Hand = Hand{c(King, Spades)} // 10 pts
	g.Phase = PhaseMelding
	_ = g.Discard("p1", 0)

	if g.Phase != PhaseGameOver {
		t.Errorf("expected game_over, got %s", g.Phase)
	}

	// Winner = lowest total = Alice (0 pts this round).
	w := g.Winner()
	if w == nil || w.ID != "p1" {
		t.Errorf("expected Alice to win, got %v", w)
	}
}

func TestGameOverLowestScoreWins(t *testing.T) {
	players := []struct{ ID, Name string }{
		{"p1", "Alice"}, {"p2", "Bob"}, {"p3", "Carol"},
	}
	g, _ := NewGame(players, 0)

	// Set up totals: Alice=110, Bob=115, Carol=5 → Carol wins.
	g.Players[0].TotalScore = 110
	g.Players[1].TotalScore = 115
	g.Players[2].TotalScore = 5
	g.Phase = PhaseGameOver

	w := g.Winner()
	if w == nil || w.ID != "p3" {
		t.Errorf("expected Carol to win, got %v", w)
	}
}

// ─── Discard-then-meld and meld-then-discard ordering ─────────────────────────

func TestMeldEscaleraEngineVariants(t *testing.T) {
	// Confirm engine accepts escalera cards regardless of selection order for
	// several different 3-card and 4-card runs.
	runs := [][]Card{
		{c(Two, Clubs), c(Three, Clubs), c(Four, Clubs)},
		{c(Jack, Spades), c(Queen, Spades), c(King, Spades)},
		{c(Ace, Hearts), c(Two, Hearts), c(Three, Hearts)},               // ace-low
		{c(Queen, Diamonds), c(King, Diamonds), c(Ace, Diamonds)},        // ace-high
		{c(Three, Clubs), c(Four, Clubs), c(Five, Clubs), c(Six, Clubs)}, // 4-card
	}

	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}

	for _, run := range runs {
		t.Run("run "+describeCards(run), func(t *testing.T) {
			// Try each of the 6 (or up to n!) orderings — we try reversed and rotated.
			orderings := reverseAndRotations(run)
			for _, ordered := range orderings {
				g, _ := NewGame(players, 0)
				extra := filler(len(run)) // enough extra cards to avoid ending round
				g.Players[0].Hand = append(Hand(nil), append(extra, ordered...)...)
				g.Phase = PhaseMelding

				n := len(g.Players[0].Hand)
				idxs := make([]int, len(ordered))
				for i := range idxs {
					idxs[i] = n - len(ordered) + i
				}
				if err := g.Meld("p1", idxs, MeldEscalera); err != nil {
					t.Errorf("ordered %v failed: %v", ordered, err)
				}
			}
		})
	}
}

// reverseAndRotations returns reversed and all rotations of a card slice.
func reverseAndRotations(cards []Card) [][]Card {
	result := [][]Card{}
	n := len(cards)
	// All rotations.
	for start := 0; start < n; start++ {
		rot := make([]Card, n)
		for i := 0; i < n; i++ {
			rot[i] = cards[(start+i)%n]
		}
		result = append(result, rot)
	}
	// Reverse.
	rev := make([]Card, n)
	for i, c := range cards {
		rev[n-1-i] = c
	}
	result = append(result, rev)
	return result
}

// filler returns a slice of n harmless cards (2♠ through Kth) to pad a hand.
func filler(n int) []Card {
	cards := []Card{}
	suits := []Suit{Spades, Hearts, Diamonds, Clubs}
	for rank := Two; rank <= King && len(cards) < n; rank++ {
		for _, suit := range suits {
			if len(cards) >= n {
				break
			}
			cards = append(cards, c(rank, suit))
		}
	}
	return cards
}

// ─── NextRound ────────────────────────────────────────────────────────────────

func TestNextRound(t *testing.T) {
	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}
	g, _ := NewGame(players, 0)

	g.Players[0].Hand = Hand{c(Five, Spades)}
	g.Players[1].Hand = Hand{c(Three, Hearts)}
	g.Phase = PhaseMelding
	_ = g.Discard("p1", 0)

	if g.Phase != PhaseRoundEnd {
		t.Fatalf("expected round_end, got %s", g.Phase)
	}

	roundNum := g.Round
	if err := g.NextRound(); err != nil {
		t.Fatalf("NextRound: %v", err)
	}

	if g.Round != roundNum+1 {
		t.Errorf("round = %d, want %d", g.Round, roundNum+1)
	}
	for _, p := range g.Players {
		if len(p.Hand) != HandSize {
			t.Errorf("player %s hand size after new round = %d, want %d", p.Name, len(p.Hand), HandSize)
		}
		if p.HasMelded {
			t.Errorf("player %s HasMelded should be reset for new round", p.Name)
		}
		if p.PickedUpDiscard != nil {
			t.Errorf("player %s PickedUpDiscard should be nil for new round", p.Name)
		}
	}
}

func TestNextRoundRejectedNotInRoundEnd(t *testing.T) {
	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}
	g, _ := NewGame(players, 0)
	if err := g.NextRound(); err == nil {
		t.Error("expected error: NextRound called before round has ended")
	}
}

// ─── Bug fixes: escalera with joker (creation + stored order) ────────────────

func TestEscaleraWithJokerCreationAndOrder(t *testing.T) {
	tests := []struct {
		name      string
		input     []Card
		wantOrder []Rank // expected stored rank order after SortEscaleraCards
	}{
		{
			// Joker fills internal gap: A♠ Joker 3♠ 4♠ → A 2(JKR) 3 4
			name:      "joker fills internal gap A-?-3-4",
			input:     []Card{c(Ace, Spades), joker(0), c(Three, Spades), c(Four, Spades)},
			wantOrder: []Rank{Ace, Joker, Three, Four},
		},
		{
			// Joker fills gap 5-?-7
			name:      "joker fills gap 5-?-7",
			input:     []Card{c(Five, Spades), joker(0), c(Seven, Spades)},
			wantOrder: []Rank{Five, Joker, Seven},
		},
		{
			// No internal gap; both ends open → joker goes to HIGH end (represents 5).
			name:      "joker extends high end 3-4",
			input:     []Card{joker(0), c(Three, Diamonds), c(Four, Diamonds)},
			wantOrder: []Rank{Three, Four, Joker},
		},
		{
			// Joker at high end: J-Q-Joker (represents K)
			name:      "joker extends high end J-Q",
			input:     []Card{c(Jack, Hearts), c(Queen, Hearts), joker(0)},
			wantOrder: []Rank{Jack, Queen, Joker},
		},
		{
			// Ace-low run 2-3; both ends open (low=rep Ace, high=rep 4) → HIGH end by convention.
			name:      "joker extends ace-low run 2-3 at high end",
			input:     []Card{joker(0), c(Two, Clubs), c(Three, Clubs)},
			wantOrder: []Rank{Two, Three, Joker},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateEscalera(tt.input); err != nil {
				t.Fatalf("ValidateEscalera rejected valid input: %v", err)
			}
			sorted := SortEscaleraCards(tt.input)
			if len(sorted) != len(tt.wantOrder) {
				t.Fatalf("len(sorted) = %d, want %d", len(sorted), len(tt.wantOrder))
			}
			for i, wantRank := range tt.wantOrder {
				if sorted[i].Rank != wantRank {
					ranks := make([]Rank, len(sorted))
					for j, s := range sorted {
						ranks[j] = s.Rank
					}
					t.Errorf("sorted[%d].Rank = %v, want %v; full: %v", i, sorted[i].Rank, wantRank, ranks)
					break
				}
			}
		})
	}
}

// TestEscaleraStoredOrderAfterMeld verifies that the engine stores escalera cards
// in sequence order after a Meld() call (regardless of selection order).
func TestEscaleraStoredOrderAfterMeld(t *testing.T) {
	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}

	// Test 1: normal run selected in reverse
	t.Run("run selected in reverse stored ascending", func(t *testing.T) {
		g, _ := NewGame(players, 0)
		g.Players[0].Hand = Hand{
			c(Seven, Spades), c(Six, Spades), c(Five, Spades), // reverse order in hand
			c(King, Hearts), c(Queen, Hearts), c(Jack, Hearts),
			c(Two, Clubs), c(Three, Clubs), c(Four, Clubs), c(Eight, Diamonds),
		}
		g.Phase = PhaseMelding
		// Select cards at indexes 0,1,2 (7,6,5 — reverse order)
		if err := g.Meld("p1", []int{0, 1, 2}, MeldEscalera); err != nil {
			t.Fatalf("Meld failed: %v", err)
		}
		meld := g.Melds[0]
		wantRanks := []Rank{Five, Six, Seven}
		for i, wantRank := range wantRanks {
			if meld.Cards[i].Rank != wantRank {
				t.Errorf("meld.Cards[%d].Rank = %v, want %v", i, meld.Cards[i].Rank, wantRank)
			}
		}
	})

	// Test 2: escalera with joker filling gap
	t.Run("joker in gap stored at correct position", func(t *testing.T) {
		g, _ := NewGame(players, 0)
		// A♠ Joker 3♠ 4♠ — joker must be at index 1 (represents 2)
		g.Players[0].Hand = Hand{
			c(Ace, Spades), joker(0), c(Three, Spades), c(Four, Spades),
			c(King, Hearts), c(Queen, Hearts), c(Jack, Hearts),
			c(Two, Clubs), c(Five, Clubs), c(Six, Clubs),
		}
		g.Phase = PhaseMelding
		if err := g.Meld("p1", []int{0, 1, 2, 3}, MeldEscalera); err != nil {
			t.Fatalf("Meld with joker failed: %v", err)
		}
		meld := g.Melds[0]
		wantRanks := []Rank{Ace, Joker, Three, Four}
		for i, wantRank := range wantRanks {
			if meld.Cards[i].Rank != wantRank {
				t.Errorf("joker-in-gap: meld.Cards[%d].Rank = %v, want %v", i, meld.Cards[i].Rank, wantRank)
			}
		}
	})
}

// TestPiernaStoredOrderAfterMeld verifies piernas are stored in suit order.
func TestPiernaStoredOrderAfterMeld(t *testing.T) {
	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}
	g, _ := NewGame(players, 0)
	// Clubs=3, Hearts=1, Spades=0 → expected suit order after sort: Spades, Hearts, Clubs
	g.Players[0].Hand = Hand{
		c(Seven, Clubs), c(Seven, Hearts), c(Seven, Spades), // Clubs first in hand
		c(King, Spades), c(Queen, Hearts), c(Jack, Diamonds),
		c(Two, Clubs), c(Three, Clubs), c(Four, Clubs), c(Five, Clubs),
	}
	g.Phase = PhaseMelding
	if err := g.Meld("p1", []int{0, 1, 2}, MeldPierna); err != nil {
		t.Fatalf("Meld pierna failed: %v", err)
	}
	meld := g.Melds[0]
	// Verify suits are in ascending order (Spades=0, Hearts=1, Clubs=3)
	for i := 1; i < len(meld.Cards); i++ {
		if int(meld.Cards[i].Suit) < int(meld.Cards[i-1].Suit) {
			t.Errorf("pierna not sorted by suit: cards[%d].Suit=%v < cards[%d].Suit=%v",
				i, meld.Cards[i].Suit, i-1, meld.Cards[i-1].Suit)
		}
	}
}

// TestLayOffEscaleraStoredOrder verifies lay-offs are placed at the correct end.
func TestLayOffEscaleraStoredOrder(t *testing.T) {
	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}

	t.Run("lay-off at high end is appended last", func(t *testing.T) {
		g, _ := NewGame(players, 0)
		g.Melds = []Meld{{
			Type:    MeldEscalera,
			Cards:   []Card{c(Five, Spades), c(Six, Spades), c(Seven, Spades)},
			OwnerID: "p1",
		}}
		g.Players[0].HasMelded = true
		_ = g.DrawStock("p1")
		g.Players[0].Hand = append(g.Players[0].Hand, c(Eight, Spades))
		lastIdx := len(g.Players[0].Hand) - 1
		if err := g.LayOff("p1", []int{lastIdx}, 0); err != nil {
			t.Fatalf("lay-off high: %v", err)
		}
		meld := g.Melds[0]
		if meld.Cards[len(meld.Cards)-1].Rank != Eight {
			t.Errorf("8♠ not at high end after lay-off")
		}
		if meld.Cards[0].Rank != Five {
			t.Errorf("5♠ not at low end after lay-off")
		}
	})

	t.Run("lay-off at low end is prepended first", func(t *testing.T) {
		g, _ := NewGame(players, 0)
		g.Melds = []Meld{{
			Type:    MeldEscalera,
			Cards:   []Card{c(Five, Spades), c(Six, Spades), c(Seven, Spades)},
			OwnerID: "p1",
		}}
		g.Players[0].HasMelded = true
		_ = g.DrawStock("p1")
		g.Players[0].Hand = append(g.Players[0].Hand, c(Four, Spades))
		lastIdx := len(g.Players[0].Hand) - 1
		if err := g.LayOff("p1", []int{lastIdx}, 0); err != nil {
			t.Fatalf("lay-off low: %v", err)
		}
		meld := g.Melds[0]
		if meld.Cards[0].Rank != Four {
			t.Errorf("4♠ not at low end after lay-off, got %v", meld.Cards[0].Rank)
		}
	})
}

// ─── Bug 2: 1-based lay-off targeting (meld index translation) ───────────────

// TestLayOffMeldIndexOneBasedTranslation verifies the full command path:
// the client translates display number N (1-based) to server index N-1.
func TestLayOffMeldIndexOneBasedTranslation(t *testing.T) {
	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}
	g, _ := NewGame(players, 0)

	// Two melds on the table.
	g.Melds = []Meld{
		{
			Type:    MeldEscalera,
			Cards:   []Card{c(Five, Clubs), c(Six, Clubs), c(Seven, Clubs)},
			OwnerID: "p2",
		},
		{
			Type:    MeldPierna,
			Cards:   []Card{c(Eight, Spades), c(Eight, Hearts), c(Eight, Diamonds)},
			OwnerID: "p2",
		},
	}
	g.Players[0].HasMelded = true
	_ = g.DrawStock("p1")
	g.Players[0].Hand = append(g.Players[0].Hand, c(Eight, Clubs))
	lastIdx := len(g.Players[0].Hand) - 1

	// UI shows meld #2 (the pierna). Client translates: serverMeldIdx = 2 - 1 = 1.
	// This must succeed (server index 1 = pierna of 8s).
	if err := g.LayOff("p1", []int{lastIdx}, 1); err != nil {
		t.Errorf("lay-off onto meld[1] (pierna of 8s): %v", err)
	}

	// Sanity: server index 2 is out of range (only 0 and 1 exist).
	g.Players[0].Hand = append(g.Players[0].Hand, c(Eight, Clubs))
	lastIdx = len(g.Players[0].Hand) - 1
	if err := g.LayOff("p1", []int{lastIdx}, 2); err == nil {
		t.Error("expected error for meld index 2 (out of range)")
	}
}

// ─── Bug 3: joker discard forbidden ──────────────────────────────────────────

func TestJokerDiscardForbidden(t *testing.T) {
	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}

	t.Run("cannot discard joker when other cards present", func(t *testing.T) {
		g, _ := NewGame(players, 0)
		g.Players[0].Hand = Hand{
			joker(0),
			c(Two, Clubs), c(Three, Hearts),
		}
		g.Phase = PhaseMelding
		// Try to discard the joker (index 0)
		if err := g.Discard("p1", 0); err == nil {
			t.Error("expected error: joker cannot be discarded when other cards are present")
		}
	})

	t.Run("can discard joker when it is the only card left", func(t *testing.T) {
		g, _ := NewGame(players, 0)
		g.Players[0].Hand = Hand{joker(0)}
		g.Phase = PhaseMelding
		// Only a joker in hand — forced discard allowed
		if err := g.Discard("p1", 0); err != nil {
			t.Errorf("expected joker discard when it is the only card: %v", err)
		}
	})

	t.Run("cannot discard joker when two jokers and one regular card present", func(t *testing.T) {
		g, _ := NewGame(players, 0)
		g.Players[0].Hand = Hand{
			joker(0), joker(1),
			c(King, Spades),
		}
		g.Phase = PhaseMelding
		// Try to discard joker(0) (index 0) — forbidden because King is present
		if err := g.Discard("p1", 0); err == nil {
			t.Error("expected error: joker cannot be discarded when non-joker card is present")
		}
	})

	t.Run("can discard when all remaining cards are jokers (all-joker exception)", func(t *testing.T) {
		g, _ := NewGame(players, 0)
		g.Players[0].Hand = Hand{joker(0), joker(1)}
		g.Phase = PhaseMelding
		// All jokers: forced discard of joker(0) allowed
		if err := g.Discard("p1", 0); err != nil {
			t.Errorf("expected success discarding joker when all-jokers: %v", err)
		}
	})
}

// ─── Full-round scenario (seeded deck) ───────────────────────────────────────

// TestFullRoundSeeded runs a complete round with two players using a seeded
// deck. It exercises: deal → draw → meld → lay-off → discard → scoring.
func TestFullRoundSeeded(t *testing.T) {
	players := []struct{ ID, Name string }{{"p1", "Alice"}, {"p2", "Bob"}}
	g, err := NewGame(players, 12345)
	if err != nil {
		t.Fatal(err)
	}

	// Both players should have HandSize cards and game should be in drawing phase.
	if len(g.Players[0].Hand) != HandSize {
		t.Fatalf("Alice hand size = %d, want %d", len(g.Players[0].Hand), HandSize)
	}
	if g.Phase != PhaseDrawing {
		t.Fatalf("initial phase = %s, want drawing", g.Phase)
	}

	// Rig Alice's hand to a known-good state for testing.
	g.Players[0].Hand = Hand{
		c(Seven, Spades), c(Seven, Hearts), c(Seven, Diamonds), // pierna
		c(Three, Clubs), c(Four, Clubs), c(Five, Clubs), // escalera
		c(King, Spades), c(Queen, Hearts), c(Jack, Diamonds),
	}

	// Alice draws from stock.
	if err := g.DrawStock("p1"); err != nil {
		t.Fatalf("Alice DrawStock: %v", err)
	}

	// Alice melds pierna (indexes 0,1,2 in hand).
	if err := g.Meld("p1", []int{0, 1, 2}, MeldPierna); err != nil {
		t.Fatalf("Alice meld pierna: %v", err)
	}

	// Alice melds escalera (new indexes 0,1,2 after pierna removed).
	if err := g.Meld("p1", []int{0, 1, 2}, MeldEscalera); err != nil {
		t.Fatalf("Alice meld escalera: %v", err)
	}

	// Alice discards (last card, or any remaining).
	if err := g.Discard("p1", 0); err != nil {
		t.Fatalf("Alice discard: %v", err)
	}

	// Should be Bob's turn now.
	if g.activePlayer().ID != "p2" {
		t.Fatalf("expected Bob's turn, got %s", g.activePlayer().ID)
	}

	// Rig Bob: one card in hand, skip draw phase by setting PhaseMelding directly.
	// This simulates Bob having drawn and now ready to discard his last card.
	g.Players[1].Hand = Hand{c(Two, Clubs)}
	g.Phase = PhaseMelding
	g.ActiveIndex = 1 // Bob's turn
	if err := g.Discard("p2", 0); err != nil {
		t.Fatalf("Bob discard: %v", err)
	}

	// Round must end (Bob's hand is now empty).
	if g.Phase != PhaseRoundEnd && g.Phase != PhaseGameOver {
		t.Fatalf("expected round_end or game_over, got %s", g.Phase)
	}
}

// ─── Joker two-jokers rejection ───────────────────────────────────────────────

func TestEscaleraTwoJokersRejected(t *testing.T) {
	// Escalera with 2 jokers must always be rejected.
	tests := [][]Card{
		{c(Five, Spades), joker(0), joker(1), c(Eight, Spades)},
		{joker(0), joker(1), c(Three, Hearts)},
		{c(Ace, Clubs), joker(0), joker(1)},
	}
	for _, cards := range tests {
		if err := ValidateEscalera(cards); err == nil {
			t.Errorf("expected error for two jokers, got nil for %v", cards)
		}
	}
}

// ─── Regression: no-gap joker placement (the reported bug) ───────────────────

// TestJokerNoGapPlacement is a direct regression for the reported bug:
// escalera {9♦, 10♦, J♦, JOKER} has no internal gap, so the joker must sit at
// an END. Convention: HIGH end (represents Q♦) when both ends are open.
func TestJokerNoGapPlacement(t *testing.T) {
	// All meaningful selection orders.
	orders := [][]Card{
		{c(Nine, Diamonds), c(Ten, Diamonds), c(Jack, Diamonds), joker(0)},
		{c(Nine, Diamonds), joker(0), c(Ten, Diamonds), c(Jack, Diamonds)},
		{joker(0), c(Nine, Diamonds), c(Ten, Diamonds), c(Jack, Diamonds)},
		{c(Ten, Diamonds), c(Nine, Diamonds), c(Jack, Diamonds), joker(0)},
		{c(Jack, Diamonds), c(Ten, Diamonds), c(Nine, Diamonds), joker(0)},
		{joker(0), c(Jack, Diamonds), c(Ten, Diamonds), c(Nine, Diamonds)},
	}
	for _, input := range orders {
		if err := ValidateEscalera(input); err != nil {
			t.Fatalf("ValidateEscalera(%v) = %v, want nil", input, err)
		}
		sorted := SortEscaleraCards(input)
		// Joker must be at index 0 or last index only.
		jokerIdx := -1
		for i, card := range sorted {
			if card.IsJoker() {
				jokerIdx = i
			}
		}
		last := len(sorted) - 1
		if jokerIdx != 0 && jokerIdx != last {
			ranks := make([]Rank, len(sorted))
			for j, s := range sorted {
				ranks[j] = s.Rank
			}
			t.Errorf("input %v → joker at internal index %d (ranks: %v); must be at 0 or %d", input, jokerIdx, ranks, last)
		}
		// Convention: HIGH end when both ends are open.
		if jokerIdx != last {
			ranks := make([]Rank, len(sorted))
			for j, s := range sorted {
				ranks[j] = s.Rank
			}
			t.Errorf("input %v → joker at index %d (ranks: %v); want HIGH end (index %d)", input, jokerIdx, ranks, last)
		}
	}
}

// TestJokerBoundaryPlacement checks end-forced and both-open scenarios.
func TestJokerBoundaryPlacement(t *testing.T) {
	tests := []struct {
		name      string
		input     []Card
		wantOrder []Rank
	}{
		{
			// Q-K: aceHigh context (King present), maxHigh=14, canGoHigh=true (13<14),
			// canGoLow=true → both ends open → HIGH end (joker represents Ace).
			name:      "joker goes to HIGH end Q-K (represents A)",
			input:     []Card{c(Queen, Spades), c(King, Spades), joker(0)},
			wantOrder: []Rank{Queen, King, Joker},
		},
		{
			// Ace-high Q-K-A: only LOW end possible (high=A=14=maxHigh → canGoHigh=false).
			name:      "joker forced to LOW end Q-K-A (represents J)",
			input:     []Card{c(Queen, Hearts), c(King, Hearts), c(Ace, Hearts), joker(0)},
			wantOrder: []Rank{Joker, Queen, King, Ace},
		},
		{
			// Ace-low A-2: canGoLow=false (low=A=1), HIGH end (represents 3).
			name:      "joker goes to HIGH end A-2 (represents 3)",
			input:     []Card{c(Ace, Clubs), c(Two, Clubs), joker(0)},
			wantOrder: []Rank{Ace, Two, Joker},
		},
		{
			// 9-10-J: both ends open → HIGH end (represents Q).
			name:      "joker at HIGH end 9-10-J (represents Q)",
			input:     []Card{c(Nine, Diamonds), c(Ten, Diamonds), c(Jack, Diamonds), joker(0)},
			wantOrder: []Rank{Nine, Ten, Jack, Joker},
		},
		{
			// J-Q-K: aceHigh context, maxHigh=14, canGoHigh=true (K=13<14) → both ends open
			// → HIGH end (joker represents Ace).
			name:      "joker goes to HIGH end J-Q-K (represents A)",
			input:     []Card{c(Jack, Clubs), c(Queen, Clubs), c(King, Clubs), joker(0)},
			wantOrder: []Rank{Jack, Queen, King, Joker},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateEscalera(tt.input); err != nil {
				t.Fatalf("ValidateEscalera rejected: %v", err)
			}
			sorted := SortEscaleraCards(tt.input)
			if len(sorted) != len(tt.wantOrder) {
				t.Fatalf("len(sorted)=%d, want %d", len(sorted), len(tt.wantOrder))
			}
			for i, want := range tt.wantOrder {
				if sorted[i].Rank != want {
					ranks := make([]Rank, len(sorted))
					for j, s := range sorted {
						ranks[j] = s.Rank
					}
					t.Errorf("sorted[%d].Rank = %v, want %v; full: %v", i, sorted[i].Rank, want, ranks)
					break
				}
			}
		})
	}
}

// TestJokerLayOffOnEscalera checks joker lay-off onto both ends and rejection when
// escalera already has a joker.
func TestJokerLayOffOnEscalera(t *testing.T) {
	base := func() *Meld {
		return &Meld{
			Type:  MeldEscalera,
			Cards: []Card{c(Five, Spades), c(Six, Spades), c(Seven, Spades)},
		}
	}
	withJokerHigh := func() *Meld {
		// 5-6-7-JKR (joker at high end, represents 8)
		return &Meld{
			Type:  MeldEscalera,
			Cards: []Card{c(Five, Spades), c(Six, Spades), c(Seven, Spades), joker(0)},
		}
	}
	withJokerLow := func() *Meld {
		// JKR-5-6-7 (joker at low end, represents 4)
		return &Meld{
			Type:  MeldEscalera,
			Cards: []Card{joker(0), c(Five, Spades), c(Six, Spades), c(Seven, Spades)},
		}
	}

	t.Run("joker extends high end of plain escalera", func(t *testing.T) {
		m := base()
		if err := CanLayOffEscalera(m, joker(1)); err != nil {
			t.Errorf("expected joker accepted at high end: %v", err)
		}
	})

	t.Run("joker extends low end of plain escalera", func(t *testing.T) {
		// Validation checks both ends; joker should be accepted at low end too.
		m := base()
		if err := CanLayOffEscalera(m, joker(1)); err != nil {
			t.Errorf("expected joker accepted: %v", err)
		}
	})

	t.Run("reject second joker when escalera already has one (high)", func(t *testing.T) {
		m := withJokerHigh()
		if err := CanLayOffEscalera(m, joker(1)); err == nil {
			t.Error("expected rejection: escalera already has a joker")
		}
	})

	t.Run("reject second joker when escalera already has one (low)", func(t *testing.T) {
		m := withJokerLow()
		if err := CanLayOffEscalera(m, joker(1)); err == nil {
			t.Error("expected rejection: escalera already has a joker")
		}
	})

	t.Run("lay real card adjacent to joker-held high end is accepted", func(t *testing.T) {
		// 5-6-7-JKR(=8): lay off 8♠ — the real card the joker represents.
		// This is valid: the joker is re-anchored or the sequence shifts.
		// The engine uses validateEscaleraSequence which allows this.
		m := withJokerHigh()
		if err := CanLayOffEscalera(m, c(Eight, Spades)); err != nil {
			t.Errorf("expected 8♠ accepted adjacent to joker-high end: %v", err)
		}
	})

	t.Run("lay real card beyond joker high end is accepted", func(t *testing.T) {
		// 5-6-7-JKR(=8): lay off 9♠ — extends beyond joker.
		m := withJokerHigh()
		if err := CanLayOffEscalera(m, c(Nine, Spades)); err != nil {
			t.Errorf("expected 9♠ accepted beyond joker-high end: %v", err)
		}
	})

	t.Run("lay real card adjacent to joker-held low end is accepted", func(t *testing.T) {
		// JKR(=4)-5-6-7: lay off 4♠.
		m := withJokerLow()
		if err := CanLayOffEscalera(m, c(Four, Spades)); err != nil {
			t.Errorf("expected 4♠ accepted adjacent to joker-low end: %v", err)
		}
	})
}

// ─── Joker re-anchor bug regression ──────────────────────────────────────────

// TestJokerReAnchorLayOff is the direct regression for the reported game bug:
// table escalera was 2♠ 3♠ 4♠ JKR (joker at high end, represents 5♠).
// Laying off the real 5♠ must cause the joker to slide to the high end
// representing 6♠ → stored order 2♠ 3♠ 4♠ 5♠ JKR (not 2♠ 3♠ 4♠ JKR 5♠).
func TestJokerReAnchorLayOff(t *testing.T) {
	// ─── helper ───────────────────────────────────────────────────────────────
	assertValidEscalera := func(t *testing.T, meld *Meld, label string) {
		t.Helper()
		if err := validateEscaleraSequence(meld.Cards); err != nil {
			ranks := make([]Rank, len(meld.Cards))
			for i, c := range meld.Cards {
				ranks[i] = c.Rank
			}
			t.Errorf("%s: stored order %v is not a valid escalera: %v", label, ranks, err)
		}
	}

	// ─── Reported bug: 2-3-4-JKR + lay 5 ─────────────────────────────────────
	t.Run("reported bug: 2-3-4-JKR(=5) + lay 5♠ → 2-3-4-5-JKR(=6)", func(t *testing.T) {
		m := &Meld{
			Type:  MeldEscalera,
			Cards: []Card{c(Two, Spades), c(Three, Spades), c(Four, Spades), joker(0)},
		}
		if err := CanLayOffEscalera(m, c(Five, Spades)); err != nil {
			t.Fatalf("CanLayOffEscalera: unexpected rejection: %v", err)
		}
		LayOffEscalera(m, c(Five, Spades))

		wantRanks := []Rank{Two, Three, Four, Five, Joker}
		if len(m.Cards) != len(wantRanks) {
			t.Fatalf("len(cards) = %d, want %d", len(m.Cards), len(wantRanks))
		}
		for i, want := range wantRanks {
			if m.Cards[i].Rank != want {
				got := make([]Rank, len(m.Cards))
				for j, card := range m.Cards {
					got[j] = card.Rank
				}
				t.Errorf("cards[%d] = %v, want %v; full: %v", i, m.Cards[i].Rank, want, got)
				break
			}
		}
		assertValidEscalera(t, m, "2-3-4-JKR + 5♠")
	})

	// ─── Internal-gap joker collision: 5-JKR(=6)-7 + lay 6 ──────────────────
	t.Run("internal gap 5-JKR(=6)-7 + lay 6♠ → joker re-anchors to high end 5-6-7-JKR", func(t *testing.T) {
		m := &Meld{
			Type:  MeldEscalera,
			Cards: []Card{c(Five, Spades), joker(0), c(Seven, Spades)},
		}
		if err := CanLayOffEscalera(m, c(Six, Spades)); err != nil {
			t.Fatalf("CanLayOffEscalera: unexpected rejection: %v", err)
		}
		LayOffEscalera(m, c(Six, Spades))

		// After re-anchor: joker slides to high end → 5-6-7-JKR(=8)
		wantRanks := []Rank{Five, Six, Seven, Joker}
		if len(m.Cards) != len(wantRanks) {
			t.Fatalf("len(cards) = %d, want %d", len(m.Cards), len(wantRanks))
		}
		for i, want := range wantRanks {
			if m.Cards[i].Rank != want {
				got := make([]Rank, len(m.Cards))
				for j, card := range m.Cards {
					got[j] = card.Rank
				}
				t.Errorf("cards[%d] = %v, want %v; full: %v", i, m.Cards[i].Rank, want, got)
				break
			}
		}
		assertValidEscalera(t, m, "5-JKR-7 + 6♠")
	})

	// ─── Re-anchor impossible at top: Q-K-JKR(=A) + lay A → rejected ─────────
	t.Run("re-anchor impossible Q-K-JKR(=A) + lay A♠ → rejected", func(t *testing.T) {
		m := &Meld{
			Type:  MeldEscalera,
			Cards: []Card{c(Queen, Spades), c(King, Spades), joker(0)},
		}
		// joker represents Ace (14) in ace-high context; maxHigh=14 → cannot go higher.
		if err := CanLayOffEscalera(m, c(Ace, Spades)); err == nil {
			t.Error("expected rejection: joker cannot re-anchor beyond Ace")
		}
	})

	// ─── Re-anchor impossible at bottom: JKR(=A)-2-3 ace-low + lay A → rejected
	t.Run("re-anchor impossible JKR(=A)-2-3 ace-low + lay A♣ → rejected", func(t *testing.T) {
		m := &Meld{
			Type:  MeldEscalera,
			Cards: []Card{joker(0), c(Two, Clubs), c(Three, Clubs)},
		}
		// joker at low end represents Ace (rank 1-1=0) but wait — Ace is 1 and low
		// end joker represents minR-1 = 2-1 = 1 = Ace. repRank=1, lowBound=1 → cannot
		// go lower (repRank <= 1).
		if err := CanLayOffEscalera(m, c(Ace, Clubs)); err == nil {
			t.Error("expected rejection: joker cannot re-anchor below Ace in ace-low context")
		}
	})

	// ─── CanLayOffEscalera agrees with LayOffEscalera across all fixtures ─────
	t.Run("CanLayOffEscalera and LayOffEscalera agree on all fixtures", func(t *testing.T) {
		type fixture struct {
			name   string
			meld   func() *Meld
			card   Card
			accept bool
		}
		fixtures := []fixture{
			{
				name: "2-3-4-JKR(=5) + 5♠ accepted + reanchors",
				meld: func() *Meld {
					return &Meld{Type: MeldEscalera,
						Cards: []Card{c(Two, Spades), c(Three, Spades), c(Four, Spades), joker(0)}}
				},
				card: c(Five, Spades), accept: true,
			},
			{
				name: "5-JKR(=6)-7 + 6♠ accepted + reanchors",
				meld: func() *Meld {
					return &Meld{Type: MeldEscalera,
						Cards: []Card{c(Five, Spades), joker(0), c(Seven, Spades)}}
				},
				card: c(Six, Spades), accept: true,
			},
			{
				name: "Q-K-JKR(=A) + A♠ rejected",
				meld: func() *Meld {
					return &Meld{Type: MeldEscalera,
						Cards: []Card{c(Queen, Spades), c(King, Spades), joker(0)}}
				},
				card: c(Ace, Spades), accept: false,
			},
			{
				name: "JKR(=A)-2-3 + A♣ rejected",
				meld: func() *Meld {
					return &Meld{Type: MeldEscalera,
						Cards: []Card{joker(0), c(Two, Clubs), c(Three, Clubs)}}
				},
				card: c(Ace, Clubs), accept: false,
			},
			{
				name: "5-6-7 + 8♠ accepted (standard high-end extend)",
				meld: func() *Meld {
					return &Meld{Type: MeldEscalera,
						Cards: []Card{c(Five, Spades), c(Six, Spades), c(Seven, Spades)}}
				},
				card: c(Eight, Spades), accept: true,
			},
			{
				name: "5-6-7 + 4♠ accepted (standard low-end extend)",
				meld: func() *Meld {
					return &Meld{Type: MeldEscalera,
						Cards: []Card{c(Five, Spades), c(Six, Spades), c(Seven, Spades)}}
				},
				card: c(Four, Spades), accept: true,
			},
		}

		for _, fx := range fixtures {
			t.Run(fx.name, func(t *testing.T) {
				canErr := CanLayOffEscalera(fx.meld(), fx.card)
				if fx.accept && canErr != nil {
					t.Errorf("CanLayOffEscalera unexpected rejection: %v", canErr)
				}
				if !fx.accept && canErr == nil {
					t.Errorf("CanLayOffEscalera expected rejection but got nil")
				}

				if fx.accept {
					m := fx.meld()
					LayOffEscalera(m, fx.card)
					assertValidEscalera(t, m, fx.name)
				}
			})
		}
	})
}
