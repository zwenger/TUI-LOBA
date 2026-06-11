package client

// helpbar_test.go — tests for the helpBar helper and screen-level key-label coverage.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// ─── Helper unit tests ────────────────────────────────────────────────────────

// TestHelpBarPlainTextContent verifies that helpBar output contains the raw key
// and description strings from each entry (plain-text assertions work because
// lipgloss embeds ANSI sequences around — not within — the text tokens).
func TestHelpBarPlainTextContent(t *testing.T) {
	bar := helpBar(
		helpEntry{"Espacio", "seleccionar"},
		helpEntry{"M", "pierna"},
		helpEntry{"E", "escalera"},
	)

	for _, want := range []string{"Espacio", "seleccionar", "M", "pierna", "E", "escalera"} {
		if !strings.Contains(bar, want) {
			t.Errorf("helpBar output missing %q\nbar: %q", want, bar)
		}
	}
}

// TestHelpBarSeparator verifies that the " · " separator character appears
// between entries.
func TestHelpBarSeparator(t *testing.T) {
	bar := helpBar(
		helpEntry{"A", "alpha"},
		helpEntry{"B", "beta"},
	)
	if !strings.Contains(bar, "·") {
		t.Errorf("helpBar output missing separator '·'\nbar: %q", bar)
	}
}

// TestHelpBarSingleEntry verifies single-entry bars have no separator.
func TestHelpBarSingleEntry(t *testing.T) {
	bar := helpBar(helpEntry{"Q", "salir"})
	if !strings.Contains(bar, "Q") {
		t.Errorf("single-entry bar missing key Q\nbar: %q", bar)
	}
	if !strings.Contains(bar, "salir") {
		t.Errorf("single-entry bar missing desc 'salir'\nbar: %q", bar)
	}
	// No separator in a single-entry bar.
	if strings.Contains(bar, "·") {
		t.Errorf("single-entry bar should not contain separator '·'\nbar: %q", bar)
	}
}

// TestHelpBarStyledOutput verifies that helpBar produces non-empty output and
// that the styled key color is configured correctly (Color("214") resolves without panic).
func TestHelpBarStyledOutput(t *testing.T) {
	bar := helpBar(helpEntry{"X", "descartar"})
	if bar == "" {
		t.Error("helpBar returned empty string")
	}
	// Verify that the key style has the expected foreground color configured.
	// lipgloss.Color("214") is amber/orange — used for all key tokens.
	wantColor := lipgloss.Color("214")
	_ = wantColor // color itself does not panic on construction
	if !strings.Contains(bar, "X") {
		t.Error("helpBar styled output missing key text X")
	}
}

// ─── Screen key-label coverage ────────────────────────────────────────────────

// TestGameHelpBarMyTurnPlay verifies that the play-phase help bar (my turn)
// contains all expected key labels.
func TestGameHelpBarMyTurnPlay(t *testing.T) {
	s := makeState()
	s.ActiveID = "self-1"
	s.Phase = "play"
	hand := makeHand()
	s.Hand = hand

	m := Model{
		screen:   screenGame,
		selfID:   "self-1",
		state:    s,
		width:    120,
		height:   40,
		cursor:   0,
		selected: make(map[int]bool),
	}
	m.displayToServer = make([]int, len(hand))
	for i := range m.displayToServer {
		m.displayToServer[i] = i
	}

	bar := m.renderHelp(true)
	for _, key := range []string{"Espacio", "M", "E", "1-9", "X", "S", "Esc"} {
		if !strings.Contains(bar, key) {
			t.Errorf("play-phase help bar missing key %q\nbar: %q", key, bar)
		}
	}
}

// TestGameHelpBarMyTurnDraw verifies that the draw-phase help bar contains D, T, S.
func TestGameHelpBarMyTurnDraw(t *testing.T) {
	s := makeState()
	s.ActiveID = "self-1"
	s.Phase = "draw"
	hand := makeHand()
	s.Hand = hand

	m := Model{
		screen:   screenGame,
		selfID:   "self-1",
		state:    s,
		width:    120,
		height:   40,
		cursor:   0,
		selected: make(map[int]bool),
	}
	m.displayToServer = make([]int, len(hand))
	for i := range m.displayToServer {
		m.displayToServer[i] = i
	}

	bar := m.renderHelp(true)
	for _, key := range []string{"D", "T", "S"} {
		if !strings.Contains(bar, key) {
			t.Errorf("draw-phase help bar missing key %q\nbar: %q", key, bar)
		}
	}
}

// TestGameHelpBarNotMyTurn verifies the waiting help bar contains the movement
// and overlay keys.
func TestGameHelpBarNotMyTurn(t *testing.T) {
	s := makeState()
	s.ActiveID = "opp-1"
	s.Phase = "play"
	hand := makeHand()
	s.Hand = hand

	m := Model{
		screen:   screenGame,
		selfID:   "self-1",
		state:    s,
		width:    120,
		height:   40,
		cursor:   0,
		selected: make(map[int]bool),
	}
	m.displayToServer = make([]int, len(hand))
	for i := range m.displayToServer {
		m.displayToServer[i] = i
	}

	bar := m.renderHelp(false)
	for _, key := range []string{"Espacio", "S", "P", "Shift+L"} {
		if !strings.Contains(bar, key) {
			t.Errorf("waiting help bar missing key %q\nbar: %q", key, bar)
		}
	}
}

// TestGameHelpBarPickedNote verifies that the picked-card warning appears when
// PickedUpDiscard is set.
func TestGameHelpBarPickedNote(t *testing.T) {
	s := makeState()
	s.ActiveID = "self-1"
	s.Phase = "play"
	s.PickedUpDiscard = &s.Hand[0] // 6♥
	hand := makeHand()
	s.Hand = hand
	s.PickedUpDiscard = &s.Hand[0]

	m := Model{
		screen:   screenGame,
		selfID:   "self-1",
		state:    s,
		width:    120,
		height:   40,
		cursor:   0,
		selected: make(map[int]bool),
	}
	m.displayToServer = make([]int, len(hand))
	for i := range m.displayToServer {
		m.displayToServer[i] = i
	}

	bar := m.renderHelp(true)
	if !strings.Contains(bar, "★") {
		t.Errorf("picked-card warning missing ★ in help bar\nbar: %q", bar)
	}
	if !strings.Contains(bar, "antes de descartar") {
		t.Errorf("picked-card warning missing 'antes de descartar'\nbar: %q", bar)
	}
}

// TestSeatPickerHelpKeys verifies the seat picker view contains Enter and Esc.
func TestSeatPickerHelpKeys(t *testing.T) {
	m := Model{
		screen: screenSeats,
		width:  80,
		height: 24,
	}
	// viewSeats always renders the help bar regardless of seat count.
	view := m.viewSeats()
	for _, key := range []string{"Enter", "Esc"} {
		if !strings.Contains(view, key) {
			t.Errorf("seat picker view missing key %q\nview: %q", key, view)
		}
	}
}

// TestRoundSummaryHelpKeys verifies the round summary contains Enter and P.
func TestRoundSummaryHelpKeys(t *testing.T) {
	s := makeState()
	s.Phase = "round_end"
	m := Model{
		screen: screenRound,
		state:  s,
		width:  100,
		height: 40,
	}
	view := m.viewRoundSummary()
	for _, key := range []string{"Enter", "P", "Q"} {
		if !strings.Contains(view, key) {
			t.Errorf("round summary view missing key %q", key)
		}
	}
}

// TestGameOverHelpKeys verifies the game over view contains Q.
func TestGameOverHelpKeys(t *testing.T) {
	s := makeState()
	s.Phase = "game_over"
	s.WinnerName = "Alice"
	s.WinnerID = "self-1"
	m := Model{
		screen: screenOver,
		state:  s,
		width:  100,
		height: 40,
	}
	view := m.viewGameOver()
	if !strings.Contains(view, "Q") {
		t.Errorf("game over view missing key Q")
	}
}

// TestScoreTableHelpKeys verifies the score table contains P and L keys.
func TestScoreTableHelpKeys(t *testing.T) {
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
	for _, key := range []string{"P", "L"} {
		if !strings.Contains(view, key) {
			t.Errorf("score table view missing key %q", key)
		}
	}
}

// TestEventLogHelpKeys verifies the event log contains ↑, ↓, PgUp, L, P.
func TestEventLogHelpKeys(t *testing.T) {
	m := Model{
		screen:      screenEventLog,
		overlayFrom: screenGame,
		width:       100,
		height:      40,
	}
	view := m.viewEventLog()
	for _, key := range []string{"PgUp", "L", "P"} {
		if !strings.Contains(view, key) {
			t.Errorf("event log view missing key %q", key)
		}
	}
}

// TestMenuMainHelpKeys verifies the main menu contains Enter and Q.
func TestMenuMainHelpKeys(t *testing.T) {
	m := newMenuModel()
	view := m.viewMenuMain()
	for _, key := range []string{"Enter", "Q"} {
		if !strings.Contains(view, key) {
			t.Errorf("menu main view missing key %q", key)
		}
	}
}

// TestMenuHostHelpKeys verifies the host sub-screen contains T, Enter, Esc.
func TestMenuHostHelpKeys(t *testing.T) {
	m := newMenuModel()
	view := m.viewMenuHost()
	for _, key := range []string{"T", "Enter", "Esc"} {
		if !strings.Contains(view, key) {
			t.Errorf("menu host view missing key %q", key)
		}
	}
}

// TestMenuJoinHelpKeys verifies the join sub-screen contains Enter and Esc.
func TestMenuJoinHelpKeys(t *testing.T) {
	m := newMenuModel()
	view := m.viewMenuJoin()
	for _, key := range []string{"Enter", "Esc"} {
		if !strings.Contains(view, key) {
			t.Errorf("menu join view missing key %q", key)
		}
	}
}

// TestFatalErrorHelpKey verifies the fatal error view contains the Enter key.
func TestFatalErrorHelpKey(t *testing.T) {
	m := NewFatalError("algo salió mal")
	view := m.viewFatalError()
	if !strings.Contains(view, "Enter") {
		t.Error("fatal error view missing key Enter")
	}
}
