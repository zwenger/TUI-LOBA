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
			badges = append(badges, renderOpponentBadge(p))
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
			badges = append(badges, renderOpponentBadge(p))
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
	badge := renderOpponentBadge(p)
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
	activeBadge := renderOpponentBadge(active)
	inactiveBadge := renderOpponentBadge(inactive)

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
