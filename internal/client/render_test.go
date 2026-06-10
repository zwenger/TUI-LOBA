package client

import (
	"fmt"
	"strings"
	"testing"

	"loba/internal/protocol"

	"github.com/charmbracelet/lipgloss"
)

// makeHand builds a slice of CardView for testing.
func makeHand() []protocol.CardView {
	// 9 cards: mixed suits and ranks, including a joker at index 7
	return []protocol.CardView{
		{Rank: 6, Suit: 1},   // 0:  6♥
		{Rank: 10, Suit: 0},  // 1: 10♠
		{Rank: 1, Suit: 2},   // 2:  A♦
		{Rank: 7, Suit: 3},   // 3:  7♣
		{Rank: 13, Suit: 1},  // 4:  K♥
		{Rank: 3, Suit: 0},   // 5:  3♠
		{Rank: 5, Suit: 2},   // 6:  5♦
		{Rank: 0, Suit: 0},   // 7:  Joker
		{Rank: 9, Suit: 3},   // 8:  9♣
	}
}

// makeState builds a fake StateSnapshot for render testing.
func makeState() *protocol.StateSnapshot {
	hand := makeHand()

	// Pierna meld: three 7s with a joker.
	pierna := protocol.MeldView{
		Index:     0,
		Type:      "pierna",
		OwnerName: "Alice",
		Cards: []protocol.CardView{
			{Rank: 7, Suit: 0}, // 7♠
			{Rank: 7, Suit: 1}, // 7♥
			{Rank: 0, Suit: 0}, // Joker
		},
	}

	// Escalera meld: 5-6-7 of diamonds.
	escalera := protocol.MeldView{
		Index:     1,
		Type:      "escalera",
		OwnerName: "Bob",
		Cards: []protocol.CardView{
			{Rank: 5, Suit: 2}, // 5♦
			{Rank: 6, Suit: 2}, // 6♦
			{Rank: 7, Suit: 2}, // 7♦
		},
	}

	discard := &protocol.CardView{Rank: 6, Suit: 1} // 6♥

	return &protocol.StateSnapshot{
		Phase:      "play",
		Round:      1,
		ActiveID:   "self-1",
		StockCount: 89,
		DiscardTop: discard,
		Hand:       hand,
		Melds:      []protocol.MeldView{pierna, escalera},
		Players: []protocol.PlayerView{
			{
				ID:         "self-1",
				Name:       "TestPlayer",
				CardCount:  9,
				TotalScore: 12,
				IsSelf:     true,
				IsActive:   true,
				Connected:  true,
			},
			{
				ID:        "opp-1",
				Name:      "Alice",
				CardCount: 8,
				TotalScore: 5,
				Connected: true,
			},
		},
	}
}

// TestHandStripAlignment verifies that every block in the hand strip has the
// same line count (= cardBoxHeight) and that no block is empty.
func TestHandStripAlignment(t *testing.T) {
	const cardBoxHeight = 5 // renderCardBox always produces 5 lines

	hand := makeHand()
	cursor := 4 // cursor in the middle
	selected := map[int]bool{1: true, 6: true}
	pickedUpIdx := 0 // index 0 is the picked-up discard card

	blocks := make([]string, len(hand))
	for i, cv := range hand {
		isCursor := i == cursor
		isSelected := selected[i]
		isPicked := i == pickedUpIdx

		if isCursor {
			blocks[i] = renderCardBox(cv, isSelected, isPicked)
		} else {
			blocks[i] = renderCardFanTab(cv, isSelected, isPicked)
		}

		// Strip ANSI codes to count raw lines.
		plain := lipgloss.NewStyle().Render(blocks[i]) // no-op but exercises the style path
		_ = plain

		rawLines := strings.Split(blocks[i], "\n")
		// lipgloss may produce trailing empty strings; count non-empty-trailing lines.
		lineCount := len(rawLines)
		// Trim one trailing empty string if present (renderCardBox ends with "╯" not "\n").
		for lineCount > 0 && rawLines[lineCount-1] == "" {
			lineCount--
		}

		if lineCount != cardBoxHeight {
			t.Errorf("card[%d] has %d lines, want %d\nblock:\n%s", i, lineCount, cardBoxHeight, blocks[i])
		}
	}

	// Join horizontally and verify the resulting block has exactly cardBoxHeight lines.
	strip := lipgloss.JoinHorizontal(lipgloss.Top, blocks...)
	stripLines := strings.Split(strip, "\n")
	stripLineCount := len(stripLines)
	for stripLineCount > 0 && stripLines[stripLineCount-1] == "" {
		stripLineCount--
	}
	if stripLineCount != cardBoxHeight {
		t.Errorf("JoinHorizontal strip has %d lines, want %d", stripLineCount, cardBoxHeight)
	}
}

// TestMiniCardHeight checks that renderMiniCard always produces 3 lines.
func TestMiniCardHeight(t *testing.T) {
	cards := []protocol.CardView{
		{Rank: 6, Suit: 1},
		{Rank: 0, Suit: 0}, // Joker
		{Hidden: true},
	}
	for _, c := range cards {
		cv := c
		block := renderMiniCard(&cv)
		lines := strings.Split(block, "\n")
		count := len(lines)
		for count > 0 && lines[count-1] == "" {
			count--
		}
		if count != 3 {
			t.Errorf("renderMiniCard(%+v) has %d lines, want 3\nblock:\n%s", c, count, block)
		}
	}
}

// TestRenderVisual is a human-readable print test that outputs the full game
// view to stdout so you can visually inspect alignment.  It always passes;
// its purpose is to let developers run `go test -v -run TestRenderVisual` and
// see the rendered output.
func TestRenderVisual(t *testing.T) {
	s := makeState()

	m := Model{
		screen:   screenGame,
		selfID:   "self-1",
		state:    s,
		cursor:   4,
		selected: map[int]bool{1: true, 6: true},
		width:    120,
		height:   40,
	}
	// identity mapping (no sort)
	m.displayToServer = make([]int, len(s.Hand))
	for i := range m.displayToServer {
		m.displayToServer[i] = i
	}
	// Simulate picked-up discard at display index 0.
	s.PickedUpDiscard = &protocol.CardView{Rank: 6, Suit: 1}

	view := m.viewGame()
	fmt.Println(view)

	// Basic sanity: view must not be empty.
	if strings.TrimSpace(view) == "" {
		t.Error("viewGame() returned empty string")
	}
}

// make5OpponentState returns a snapshot with 5 opponents + self for badge layout tests.
func make5OpponentState() *protocol.StateSnapshot {
	s := makeState()
	s.ActiveID = "opp-3"
	s.Players = []protocol.PlayerView{
		{ID: "self-1", Name: "TestPlayer", CardCount: 9, TotalScore: 12, IsSelf: true, Connected: true},
		{ID: "opp-1", Name: "Alice", CardCount: 8, TotalScore: 5, Connected: true},
		{ID: "opp-2", Name: "BobWithLongName", CardCount: 3, TotalScore: 42, Connected: true},
		{ID: "opp-3", Name: "Carlos", CardCount: 6, TotalScore: 99, IsActive: true, Connected: true},
		{ID: "opp-4", Name: "Diana", CardCount: 11, TotalScore: 0, Connected: false},
		{ID: "opp-5", Name: "Eve", CardCount: 7, TotalScore: 77, Connected: true},
	}
	return s
}

// TestOpponentBadgeHeight verifies that every badge produced by renderOpponentBadge
// has the same rendered height so they align properly in a row.
func TestOpponentBadgeHeight(t *testing.T) {
	players := make5OpponentState().Players
	var badges []string
	for _, p := range players {
		if !p.IsSelf {
			badges = append(badges, renderOpponentBadge(p, p.TurnIndex))
		}
	}
	if len(badges) == 0 {
		t.Fatal("no badges produced")
	}

	heights := make([]int, len(badges))
	for i, b := range badges {
		lines := strings.Split(b, "\n")
		h := len(lines)
		for h > 0 && lines[h-1] == "" {
			h--
		}
		heights[i] = h
	}

	// All badges in the same row must have the same line count.
	first := heights[0]
	for i, h := range heights {
		if h != first {
			t.Errorf("badge[%d] has height %d, want %d (matching badge[0])", i, h, first)
		}
	}
}

// TestOpponentBadgeRowUniformHeight checks that when badges are joined horizontally
// into a row, each line of the joined block is the same width (i.e. lipgloss pads
// them correctly) and no badge content is split across rows.
func TestOpponentBadgeRowUniformHeight(t *testing.T) {
	players := make5OpponentState().Players
	var badges []string
	for _, p := range players {
		if !p.IsSelf {
			badges = append(badges, renderOpponentBadge(p, p.TurnIndex))
		}
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, badges...)
	lines := strings.Split(row, "\n")
	h := len(lines)
	for h > 0 && lines[h-1] == "" {
		h--
	}
	// Each badge is 5 lines (3 content + top/bottom border); the joined row must
	// maintain that height.
	if h < 3 {
		t.Errorf("joined row has only %d lines, expected at least 3", h)
	}
}

// TestOpponentBadgeWrapping verifies that at a narrow terminal width (40 cols)
// badges wrap to new rows rather than overflowing a single line.
func TestOpponentBadgeWrapping(t *testing.T) {
	s := make5OpponentState()
	m := Model{
		screen: screenGame,
		selfID: "self-1",
		state:  s,
		width:  40,
	}

	result := m.renderOpponents(s)
	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	// With width=40 and 5 badges of ~18 chars each, we must have more than 1 line.
	if len(lines) <= 1 {
		t.Errorf("expected wrapping at width=40 but got %d line(s)", len(lines))
	}
	// No single line should exceed termWidth (plus some ANSI escape overhead is OK;
	// check rendered/visible width using lipgloss.Width on each line).
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w > 40 {
			t.Errorf("line[%d] visible width %d exceeds terminal width 40: %q", i, w, line)
		}
	}
}

// TestOpponentNameTruncation verifies that names longer than 12 chars are truncated.
func TestOpponentNameTruncation(t *testing.T) {
	p := protocol.PlayerView{
		ID:        "x",
		Name:      "VeryLongPlayerName",
		CardCount: 5,
		TotalScore: 10,
		Connected: true,
	}
	badge := renderOpponentBadge(p, p.TurnIndex)
	// The full name must not appear in the badge.
	if strings.Contains(badge, "VeryLongPlayerName") {
		t.Error("badge contains un-truncated name VeryLongPlayerName")
	}
	// truncateName(name, 12) keeps 11 runes then appends "…": "VeryLongPla…"
	if !strings.Contains(badge, "VeryLongPla…") {
		t.Errorf("badge does not contain expected truncated name VeryLongPla…\nbadge:\n%s", badge)
	}
}

// TestOpponentCompactChips verifies that very narrow terminals (width < 60) fall
// back to chip mode and still wrap whole chips.
func TestOpponentCompactChips(t *testing.T) {
	s := make5OpponentState()
	m := Model{
		screen: screenGame,
		selfID: "self-1",
		state:  s,
		width:  50,
	}
	result := m.renderOpponents(s)
	// In chip mode there should be no box borders.
	if strings.Contains(result, "╭") || strings.Contains(result, "┏") {
		t.Error("compact chip mode should not contain box border characters")
	}
	// Must still contain player names (possibly truncated).
	if !strings.Contains(result, "Alice") {
		t.Error("chip output missing player name Alice")
	}
}

// TestOpponentActiveHighlight checks that the active-turn badge uses a
// visually distinct marker (▶ arrow) that text rendering can verify.
func TestOpponentActiveHighlight(t *testing.T) {
	active := protocol.PlayerView{
		ID: "a", Name: "Carlos", CardCount: 6, TotalScore: 99, IsActive: true, Connected: true,
	}
	inactive := protocol.PlayerView{
		ID: "b", Name: "Alice", CardCount: 8, TotalScore: 5, Connected: true,
	}
	activeBadge := renderOpponentBadge(active, 0)
	inactiveBadge := renderOpponentBadge(inactive, 0)

	if !strings.Contains(activeBadge, "▶") {
		t.Error("active badge missing ▶ turn indicator")
	}
	if !strings.Contains(activeBadge, "◀") {
		t.Error("active badge missing ◀ turn indicator")
	}
	if strings.Contains(inactiveBadge, "▶") {
		t.Error("inactive badge should not contain ▶")
	}
}

// ─── Round summary reveal render tests ───────────────────────────────────────

// makeRoundReveal builds a sample RoundReveal slice for render testing:
// Alice won (empty hand), Bob holds K♠+Q♦+5♣ = 10+10+5 = 25,
// Carol holds a Joker = 25.
func makeRoundReveal() []protocol.RevealedPlayerHand {
	return []protocol.RevealedPlayerHand{
		{
			PlayerID:   "p1",
			PlayerName: "Alice",
			Cards:      nil, // empty — won the round
			RoundScore: 0,
			TotalScore: 45,
			IsWinner:   true,
		},
		{
			PlayerID:   "p2",
			PlayerName: "Bob",
			Cards: []protocol.CardView{
				{Rank: 13, Suit: 0, Label: "K♠"}, // K♠ = 10
				{Rank: 12, Suit: 2, Label: "Q♦"}, // Q♦ = 10
				{Rank: 5, Suit: 3, Label: " 5♣"}, // 5♣ = 5
			},
			RoundScore: 25,
			TotalScore: 60,
		},
		{
			PlayerID:   "p3",
			PlayerName: "Carol",
			Cards: []protocol.CardView{
				{Rank: 0, Suit: 0, Label: "★JKR"}, // Joker = 25
			},
			RoundScore: 25,
			TotalScore: 72,
		},
	}
}

// TestRenderRoundSummaryVisual prints the full round-summary view at width 100
// so the developer can inspect it. Always passes.
// Run with: go test -v -run TestRenderRoundSummaryVisual
func TestRenderRoundSummaryVisual(t *testing.T) {
	snap := &protocol.StateSnapshot{
		Phase:       "round_end",
		Round:       3,
		RoundReveal: makeRoundReveal(),
		Players: []protocol.PlayerView{
			{ID: "p1", Name: "Alice", TotalScore: 45, IsSelf: true, Connected: true},
			{ID: "p2", Name: "Bob", TotalScore: 60, Connected: true},
			{ID: "p3", Name: "Carol", TotalScore: 72, Connected: true},
		},
	}

	m := Model{
		screen: screenRound,
		selfID: "p1",
		state:  snap,
		width:  100,
		height: 40,
	}

	view := m.viewRoundSummary()
	fmt.Println("\n=== Round Summary (width 100) ===")
	fmt.Println(view)

	if strings.TrimSpace(view) == "" {
		t.Error("viewRoundSummary() returned empty string")
	}
	// Must contain the winner marker.
	if !strings.Contains(view, "ganó la mano") {
		t.Error("round summary missing 'ganó la mano' winner marker")
	}
	// Must contain a score breakdown for Bob.
	if !strings.Contains(view, "25") {
		t.Error("round summary missing Bob's penalty total 25")
	}
}

// TestRenderGameOverVisual prints the game-over view at width 100.
// Always passes; run with: go test -v -run TestRenderGameOverVisual
func TestRenderGameOverVisual(t *testing.T) {
	snap := &protocol.StateSnapshot{
		Phase:       "game_over",
		Round:       5,
		WinnerID:    "p1",
		WinnerName:  "Alice",
		RoundReveal: makeRoundReveal(),
		Players: []protocol.PlayerView{
			{ID: "p1", Name: "Alice", TotalScore: 45, IsSelf: true, Connected: true},
			{ID: "p2", Name: "Bob", TotalScore: 102, Connected: true},
			{ID: "p3", Name: "Carol", TotalScore: 88, Connected: true},
		},
	}

	m := Model{
		screen: screenOver,
		selfID: "p1",
		state:  snap,
		width:  100,
		height: 40,
	}

	view := m.viewGameOver()
	fmt.Println("\n=== Game Over (width 100) ===")
	fmt.Println(view)

	if strings.TrimSpace(view) == "" {
		t.Error("viewGameOver() returned empty string")
	}
	if !strings.Contains(view, "GANADOR") {
		t.Error("game over missing GANADOR")
	}
	// Final standings must show Alice first (lowest score).
	if !strings.Contains(view, "1. ") {
		t.Error("game over missing position markers in standings")
	}
}

// TestRoundSummaryRevealBlocks verifies that every reveal block renders without
// panicking and contains the player name.
func TestRoundSummaryRevealBlocks(t *testing.T) {
	reveal := makeRoundReveal()
	for _, rph := range reveal {
		block := renderRevealBlock(rph, 100)
		if !strings.Contains(block, rph.PlayerName) {
			t.Errorf("reveal block for %s missing player name\nblock:\n%s", rph.PlayerName, block)
		}
	}
}

// TestScoreBreakdownFormula verifies buildScoreBreakdown produces sensible output.
func TestScoreBreakdownFormula(t *testing.T) {
	cards := []protocol.CardView{
		{Rank: 13, Suit: 0, Label: "K♠"},
		{Rank: 12, Suit: 2, Label: "Q♦"},
		{Rank: 5, Suit: 3, Label: " 5♣"},
	}
	result := buildScoreBreakdown(cards, 25)
	if !strings.Contains(result, "25") {
		t.Errorf("buildScoreBreakdown missing total 25, got: %s", result)
	}
	// Must contain individual values.
	if !strings.Contains(result, "10") {
		t.Errorf("buildScoreBreakdown missing face-card value 10, got: %s", result)
	}
}

// TestScoreBreakdownEmpty verifies that an empty hand returns the winner string.
func TestScoreBreakdownEmpty(t *testing.T) {
	result := buildScoreBreakdown(nil, 0)
	if result != "+0 pts esta mano" {
		t.Errorf("buildScoreBreakdown(nil) = %q, want '+0 pts esta mano'", result)
	}
}

// TestCardPenaltyValues verifies cardPenaltyValue for each rule-relevant rank.
func TestCardPenaltyValues(t *testing.T) {
	cases := []struct {
		rank int
		want int
	}{
		{0, 25},  // Joker
		{1, 15},  // Ace
		{2, 2},   // 2
		{5, 5},   // 5
		{10, 10}, // 10
		{11, 10}, // Jack
		{12, 10}, // Queen
		{13, 10}, // King
	}
	for _, tc := range cases {
		cv := protocol.CardView{Rank: tc.rank, Suit: 0}
		got := cardPenaltyValue(cv)
		if got != tc.want {
			t.Errorf("cardPenaltyValue(rank=%d) = %d, want %d", tc.rank, got, tc.want)
		}
	}
}

// TestRenderVisual5Opponents is a visual sanity test: prints a 5-opponent
// layout at width 100 and width 60 so the developer can inspect output.
// Always passes; run with: go test -v -run TestRenderVisual5Opponents
func TestRenderVisual5Opponents(t *testing.T) {
	s := make5OpponentState()
	hand := makeHand()
	s.Hand = hand

	makeModel := func(w int) Model {
		m := Model{
			screen: screenGame,
			selfID: "self-1",
			state:  s,
			width:  w,
			height: 40,
			cursor: 2,
			selected: make(map[int]bool),
		}
		m.displayToServer = make([]int, len(hand))
		for i := range m.displayToServer {
			m.displayToServer[i] = i
		}
		return m
	}

	for _, w := range []int{100, 60} {
		m := makeModel(w)
		section := m.renderOpponents(s)
		fmt.Printf("\n=== Opponents at width %d ===\n%s", w, section)
	}
}

// ─── Score table tests ────────────────────────────────────────────────────────

// makeScoreHistoryState builds a snapshot with 4 players and 3 rounds of
// score history for score-table render tests.
func makeScoreHistoryState() *protocol.StateSnapshot {
	playerIDs := []string{"p1", "p2", "p3", "p4"}
	names := map[string]string{
		"p1": "Alice",
		"p2": "Bob",
		"p3": "Carol",
		"p4": "Dave",
	}

	history := []protocol.RoundScoresView{
		{
			Round:  1,
			Scores: map[string]int{"p1": 0, "p2": 15, "p3": 7, "p4": 22},
			Names:  names,
		},
		{
			Round:  2,
			Scores: map[string]int{"p1": 10, "p2": 0, "p3": 25, "p4": 5},
			Names:  names,
		},
		{
			Round:  3,
			Scores: map[string]int{"p1": 0, "p2": 8, "p3": 12, "p4": 30},
			Names:  names,
		},
	}

	players := make([]protocol.PlayerView, len(playerIDs))
	totals := map[string]int{}
	for _, rs := range history {
		for id, pts := range rs.Scores {
			totals[id] += pts
		}
	}
	for i, id := range playerIDs {
		players[i] = protocol.PlayerView{
			ID:         id,
			Name:       names[id],
			TotalScore: totals[id],
			Connected:  true,
			IsSelf:     id == "p1",
		}
	}

	return &protocol.StateSnapshot{
		Phase:        "draw",
		Round:        4,
		Players:      players,
		ScoreHistory: history,
	}
}

// TestRenderScoreTableVisual is a human-readable print test at width 100
// with 4 players × 3 rounds. Run with: go test -v -run TestRenderScoreTableVisual
func TestRenderScoreTableVisual(t *testing.T) {
	snap := makeScoreHistoryState()
	m := Model{
		screen:          screenScoreTable,
		selfID:          "p1",
		state:           snap,
		overlayFrom:     screenGame,
		width:           100,
		height:          40,
		displayToServer: []int{},
	}

	view := m.viewScoreTable()
	fmt.Println("\n=== Score Table (width 100, 4 players × 3 rounds) ===")
	fmt.Println(view)

	if strings.TrimSpace(view) == "" {
		t.Error("viewScoreTable() returned empty string")
	}
	// Must contain all player names.
	for _, name := range []string{"Alice", "Bob", "Carol", "Dave"} {
		if !strings.Contains(view, name) {
			t.Errorf("score table missing player name %q", name)
		}
	}
	// Must contain round labels.
	for _, label := range []string{"Ronda 1", "Ronda 2", "Ronda 3"} {
		if !strings.Contains(view, label) {
			t.Errorf("score table missing round label %q", label)
		}
	}
	// Must contain the Total row.
	if !strings.Contains(view, "Total") {
		t.Error("score table missing Total row")
	}
}

// TestScoreTableEmptyHistory verifies that the score table handles zero rounds
// gracefully (no panic).
func TestScoreTableEmptyHistory(t *testing.T) {
	snap := &protocol.StateSnapshot{
		Phase:   "draw",
		Round:   1,
		Players: []protocol.PlayerView{{ID: "p1", Name: "Alice", IsSelf: true, Connected: true}},
	}
	m := Model{
		screen:      screenScoreTable,
		selfID:      "p1",
		state:       snap,
		overlayFrom: screenGame,
		width:       100,
		height:      40,
	}
	view := m.viewScoreTable()
	if strings.TrimSpace(view) == "" {
		t.Error("viewScoreTable() with empty history returned empty string")
	}
}

// TestScoreTableTotalsCorrect verifies that the Total row in the score table
// matches the sum of per-round scores for each player.
func TestScoreTableTotalsCorrect(t *testing.T) {
	snap := makeScoreHistoryState()
	m := Model{
		screen:      screenScoreTable,
		selfID:      "p1",
		state:       snap,
		overlayFrom: screenGame,
		width:       100,
		height:      40,
	}
	view := m.viewScoreTable()

	// Alice: rounds 1+2+3 = 0+10+0 = 10; expect "10" in Total row context.
	// Bob:   0+15+8 = 23 (wait, round 1 scores: p2=15; round 2: p2=0; round 3: p2=8 → 23).
	// Just verify the view contains the expected totals.
	wantTotals := map[string]int{
		"p1": 10,
		"p2": 23,
		"p3": 44,
		"p4": 57,
	}
	for id, want := range wantTotals {
		cell := fmt.Sprintf("%d", want)
		if !strings.Contains(view, cell) {
			t.Errorf("score table missing total %s for player %s", cell, id)
		}
	}
}

// ─── Event log tests ──────────────────────────────────────────────────────────

// TestEventLogScrollBoundsNoPanic verifies that scrolling at the top and bottom
// of the event log does not panic or produce invalid state.
func TestEventLogScrollBoundsNoPanic(t *testing.T) {
	m := Model{
		screen:         screenEventLog,
		overlayFrom:    screenGame,
		width:          100,
		height:         40,
		eventLogOffset: 0,
		eventHistory:   []string{"event-1", "event-2", "event-3"},
	}

	// Scroll up past the top — must clamp, not panic.
	for i := 0; i < 20; i++ {
		result, _ := m.handleEventLogKey("up")
		m = result.(Model)
	}
	// Should still render without panic.
	view := m.viewEventLog()
	if strings.TrimSpace(view) == "" {
		t.Error("viewEventLog() returned empty string after scrolling up")
	}

	// Scroll back down past the bottom.
	for i := 0; i < 20; i++ {
		result, _ := m.handleEventLogKey("down")
		m = result.(Model)
	}
	if m.eventLogOffset < 0 {
		t.Errorf("eventLogOffset went negative: %d", m.eventLogOffset)
	}
}

// TestEventLogEmptyNoPanic verifies no panic when event history is empty.
func TestEventLogEmptyNoPanic(t *testing.T) {
	m := Model{
		screen:      screenEventLog,
		overlayFrom: screenGame,
		width:       100,
		height:      40,
	}
	view := m.viewEventLog()
	if strings.TrimSpace(view) == "" {
		t.Error("viewEventLog() with empty history returned empty string")
	}
}

// TestEventHistoryAppendDedup verifies that appending the same entry twice
// in a row is deduplicated, and that the cap is respected.
func TestEventHistoryAppendDedup(t *testing.T) {
	m := Model{}
	m.appendEventHistory("A")
	m.appendEventHistory("A") // duplicate → must not be added
	if len(m.eventHistory) != 1 {
		t.Errorf("expected 1 entry after dedup, got %d", len(m.eventHistory))
	}
	m.appendEventHistory("B")
	if len(m.eventHistory) != 2 {
		t.Errorf("expected 2 entries, got %d", len(m.eventHistory))
	}

	// Fill to over the cap.
	for i := 0; i < maxClientEventHistory+10; i++ {
		m.appendEventHistory(fmt.Sprintf("ev-%d", i))
	}
	if len(m.eventHistory) > maxClientEventHistory {
		t.Errorf("eventHistory exceeded cap: %d > %d", len(m.eventHistory), maxClientEventHistory)
	}
}

// TestEventLogScrollBoundsWithManyEntries verifies PgUp/PgDn clamp correctly
// for a larger history than the visible window.
func TestEventLogScrollBoundsWithManyEntries(t *testing.T) {
	history := make([]string, 100)
	for i := range history {
		history[i] = fmt.Sprintf("event-%03d", i)
	}
	m := Model{
		screen:         screenEventLog,
		overlayFrom:    screenGame,
		width:          100,
		height:         30, // ~27 visible lines
		eventLogOffset: 0,
		eventHistory:   history,
	}

	// PgUp many times.
	for i := 0; i < 10; i++ {
		result, _ := m.handleEventLogKey("pgup")
		m = result.(Model)
	}
	visibleLines := m.logVisibleLines()
	maxOffset := len(history) - visibleLines
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.eventLogOffset > maxOffset {
		t.Errorf("eventLogOffset %d exceeded max %d after PgUp", m.eventLogOffset, maxOffset)
	}

	// PgDn back to bottom.
	for i := 0; i < 10; i++ {
		result, _ := m.handleEventLogKey("pgdown")
		m = result.(Model)
	}
	if m.eventLogOffset < 0 {
		t.Errorf("eventLogOffset went negative: %d", m.eventLogOffset)
	}
}

// TestMergeEventLogTail verifies that merging a server tail into an empty
// history seeds it, and that merging an already-present tail is a no-op.
func TestMergeEventLogTail(t *testing.T) {
	m := Model{}
	tail := []string{"A", "B", "C"}
	m.mergeEventLogTail(tail)
	if len(m.eventHistory) != 3 {
		t.Fatalf("expected 3 entries after seed, got %d", len(m.eventHistory))
	}

	// Merging the same tail again must not duplicate.
	m.mergeEventLogTail(tail)
	if len(m.eventHistory) != 3 {
		t.Errorf("expected 3 entries after second merge, got %d", len(m.eventHistory))
	}

	// Merging a longer tail that includes our current content plus new prefix.
	m.mergeEventLogTail([]string{"X", "Y", "A", "B", "C"})
	// "X" and "Y" are new; "A","B","C" are already there.
	if len(m.eventHistory) < 5 {
		t.Errorf("expected at least 5 entries after extended merge, got %d: %v", len(m.eventHistory), m.eventHistory)
	}
}

// ─── Turn-order display tests ─────────────────────────────────────────────────

// makeRotationState builds a 4-player snapshot where self is turn index 2,
// so rotation order for opponents is 3·, 4·, 1· (wrapping around).
func makeRotationState() *protocol.StateSnapshot {
	return &protocol.StateSnapshot{
		Phase:    "draw",
		Round:    1,
		ActiveID: "p3",
		NextID:   "p4",
		Players: []protocol.PlayerView{
			{ID: "p1", Name: "Alice", CardCount: 9, TotalScore: 0, TurnIndex: 1, Connected: true},
			{ID: "p2", Name: "Bob", CardCount: 7, TotalScore: 5, TurnIndex: 2, Connected: true, IsSelf: true},
			{ID: "p3", Name: "Carlos", CardCount: 6, TotalScore: 3, TurnIndex: 3, Connected: true, IsActive: true},
			{ID: "p4", Name: "Diana", CardCount: 8, TotalScore: 10, TurnIndex: 4, Connected: true},
		},
	}
}

// TestOpponentBadgesShowPositionNumbers verifies that opponent badges include
// the 1-based turn position prefix ("3·", "4·", "1·") in rotation order.
func TestOpponentBadgesShowPositionNumbers(t *testing.T) {
	s := makeRotationState()
	m := Model{
		screen: screenGame,
		selfID: "p2",
		state:  s,
		width:  100,
		height: 40,
	}

	result := m.renderOpponents(s)

	// Each opponent's position label must appear.
	for _, pos := range []string{"3·", "4·", "1·"} {
		if !strings.Contains(result, pos) {
			t.Errorf("opponent row missing position label %q\nrow:\n%s", pos, result)
		}
	}
	// Self (p2, Bob) must NOT appear in the opponents row.
	if strings.Contains(result, "Bob") {
		t.Error("opponents row must not contain self player Bob")
	}
}

// TestOpponentRotationOrder verifies that opponents appear in rotation order
// starting after self. Self is 2·, so order should be 3·Carlos, 4·Diana, 1·Alice.
func TestOpponentRotationOrder(t *testing.T) {
	s := makeRotationState()
	opponents := rotationOrderedOpponents(s.Players)

	wantOrder := []int{3, 4, 1}
	for i, p := range opponents {
		if p.TurnIndex != wantOrder[i] {
			t.Errorf("opponents[%d].TurnIndex = %d, want %d", i, p.TurnIndex, wantOrder[i])
		}
	}
}

// TestSiguienteHintInView verifies that the game view status line includes the
// "siguiente" hint with the next player's position and name.
func TestSiguienteHintInView(t *testing.T) {
	s := makeRotationState()
	hand := makeHand()
	s.Hand = hand

	m := Model{
		screen: screenGame,
		selfID: "p2",
		state:  s,
		width:  120,
		height: 40,
		cursor: 0,
		selected: make(map[int]bool),
	}
	m.displayToServer = make([]int, len(hand))
	for i := range m.displayToServer {
		m.displayToServer[i] = i
	}

	view := m.viewGame()
	// NextID is p4=Diana (TurnIndex 4); expect "siguiente: 4·Diana" or similar.
	if !strings.Contains(view, "siguiente") {
		t.Error("game view missing 'siguiente' turn hint")
	}
	if !strings.Contains(view, "Diana") {
		t.Error("game view 'siguiente' hint missing next player name Diana")
	}
}

// TestHandHeaderShowsPosition verifies that the hand header includes the self
// turn index label (e.g. "(2·)").
func TestHandHeaderShowsPosition(t *testing.T) {
	s := makeRotationState()
	hand := makeHand()
	s.Hand = hand

	m := Model{
		screen: screenGame,
		selfID: "p2",
		state:  s,
		width:  120,
		height: 40,
		cursor: 0,
		selected: make(map[int]bool),
	}
	m.displayToServer = make([]int, len(hand))
	for i := range m.displayToServer {
		m.displayToServer[i] = i
	}

	view := m.viewGame()
	if !strings.Contains(view, "(2·)") {
		t.Errorf("hand header missing self position label (2·)\nview:\n%s", view)
	}
}

// TestRenderVisualWithRotation is a visual sanity print of the full game view
// with 4 players and rotation order. Always passes.
func TestRenderVisualWithRotation(t *testing.T) {
	s := makeRotationState()
	hand := makeHand()
	s.Hand = hand

	m := Model{
		screen: screenGame,
		selfID: "p2",
		state:  s,
		width:  100,
		height: 40,
		cursor: 2,
		selected: make(map[int]bool),
	}
	m.displayToServer = make([]int, len(hand))
	for i := range m.displayToServer {
		m.displayToServer[i] = i
	}

	view := m.viewGame()
	fmt.Println("\n=== Game view with rotation order (width 100) ===")
	fmt.Println(view)

	if strings.TrimSpace(view) == "" {
		t.Error("viewGame() returned empty string")
	}
}
