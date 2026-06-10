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
