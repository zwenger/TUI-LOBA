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
	type key struct{ rank Rank; suit Suit }
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

	t.Run("accept when card can be laid off on existing meld", func(t *testing.T) {
		g := newG()
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
		g.Players[0].HasMelded = true
		if err := g.DrawDiscard("p1"); err != nil {
			t.Errorf("expected acceptance (lay-off possible): %v", err)
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

	t.Run("picked flag cleared after lay-off", func(t *testing.T) {
		g := newG()
		// Put a pierna on the table that Alice can lay the picked card onto.
		g.Melds = []Meld{{
			Type:    MeldPierna,
			Cards:   []Card{c(Seven, Spades), c(Seven, Hearts), c(Seven, Diamonds)},
			OwnerID: "p1",
		}}
		g.Players[0].HasMelded = true
		g.Players[0].Hand = Hand{
			c(Two, Clubs), c(Three, Clubs), c(Four, Clubs),
			c(King, Spades), c(Queen, Hearts), c(Jack, Diamonds),
			c(Ten, Clubs), c(Nine, Clubs), c(Eight, Spades),
		}
		g.DiscardPile = []Card{c(Seven, Clubs)}
		_ = g.DrawDiscard("p1")
		lastIdx := len(g.Players[0].Hand) - 1
		if err := g.LayOff("p1", []int{lastIdx}, 0); err != nil {
			t.Fatalf("lay-off: %v", err)
		}
		if g.Players[0].PickedUpDiscard != nil {
			t.Error("PickedUpDiscard should be nil after laying off the picked card")
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
		{c(Ace, Hearts), c(Two, Hearts), c(Three, Hearts)},                         // ace-low
		{c(Queen, Diamonds), c(King, Diamonds), c(Ace, Diamonds)},                  // ace-high
		{c(Three, Clubs), c(Four, Clubs), c(Five, Clubs), c(Six, Clubs)},           // 4-card
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
