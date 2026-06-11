package client

// menu_test.go — model-level tests for the start-menu screen.

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// noopHostFn is a HostBootstrapFunc that always succeeds.
func noopHostFn(port string, public bool, progCh chan<- *tea.Program) (string, error) {
	return "localhost:7777", nil
}

// noopJoinFn is a JoinBootstrapFunc that echoes the address back.
func noopJoinFn(addr string) (string, error) {
	return addr, nil
}

// newMenuModel returns a Model on the menu screen with no-op bootstrap funcs.
func newMenuModel() Model {
	return NewMenu(noopHostFn, noopJoinFn, "dev")
}

// sendMenuKey sends a single key to handleMenuKey and returns the updated model.
func sendMenuKey(m Model, key string) Model {
	newM, _ := m.handleMenuKey(key)
	return newM.(Model)
}

// sendMenuKeys sends a sequence of keys through handleMenuKey.
func sendMenuKeys(m Model, keys ...string) Model {
	for _, k := range keys {
		m = sendMenuKey(m, k)
	}
	return m
}

// ─── Navigation tests ─────────────────────────────────────────────────────────

// TestMenuInitialState verifies the menu starts on the main sub-screen with
// the cursor at position 0 (Crear sala).
func TestMenuInitialState(t *testing.T) {
	m := newMenuModel()
	if m.screen != screenMenu {
		t.Errorf("initial screen = %v, want screenMenu", m.screen)
	}
	if m.menuCursor != 0 {
		t.Errorf("initial cursor = %d, want 0", m.menuCursor)
	}
	if m.menuSubScreen != menuSubMain {
		t.Errorf("initial subscreen = %d, want menuSubMain (%d)", m.menuSubScreen, menuSubMain)
	}
}

// TestMenuCursorNavigation verifies arrow keys and j/k move the cursor
// within bounds [0, menuItemCount-1].
func TestMenuCursorNavigation(t *testing.T) {
	m := newMenuModel()

	// Move down to last item.
	for i := 0; i < menuItemCount-1; i++ {
		m = sendMenuKey(m, "down")
	}
	if m.menuCursor != menuItemCount-1 {
		t.Errorf("cursor after %d downs = %d, want %d", menuItemCount-1, m.menuCursor, menuItemCount-1)
	}

	// Extra down should not exceed bounds.
	m = sendMenuKey(m, "down")
	if m.menuCursor != menuItemCount-1 {
		t.Errorf("cursor after extra down = %d, want %d (clamped)", m.menuCursor, menuItemCount-1)
	}

	// Move back up past the top.
	for i := 0; i < menuItemCount+2; i++ {
		m = sendMenuKey(m, "up")
	}
	if m.menuCursor != 0 {
		t.Errorf("cursor after extra ups = %d, want 0 (clamped)", m.menuCursor)
	}

	// Test j/k aliases.
	m = sendMenuKey(m, "j")
	if m.menuCursor != 1 {
		t.Errorf("cursor after j = %d, want 1", m.menuCursor)
	}
	m = sendMenuKey(m, "k")
	if m.menuCursor != 0 {
		t.Errorf("cursor after k = %d, want 0", m.menuCursor)
	}
}

// TestMenuSelectCrearSala verifies that pressing Enter on "Crear sala" (index 0)
// opens the host sub-screen.
func TestMenuSelectCrearSala(t *testing.T) {
	m := newMenuModel()
	// cursor is already at 0 (Crear sala).
	m = sendMenuKey(m, "enter")
	if m.menuSubScreen != menuSubHost {
		t.Errorf("after Enter on Crear sala: subscreen = %d, want menuSubHost (%d)", m.menuSubScreen, menuSubHost)
	}
}

// TestMenuSelectUnirseASala verifies that pressing Enter on "Unirse a sala" (index 1)
// opens the join sub-screen.
func TestMenuSelectUnirseASala(t *testing.T) {
	m := newMenuModel()
	m = sendMenuKey(m, "down") // move to index 1
	m = sendMenuKey(m, "enter")
	if m.menuSubScreen != menuSubJoin {
		t.Errorf("after Enter on Unirse a sala: subscreen = %d, want menuSubJoin (%d)", m.menuSubScreen, menuSubJoin)
	}
	// Address input should be reset.
	if m.menuAddrInput != "" {
		t.Errorf("menuAddrInput should be empty on fresh join screen, got %q", m.menuAddrInput)
	}
}

// TestMenuEscFromHostReturnsToMain verifies Esc from host sub-screen goes back.
func TestMenuEscFromHostReturnsToMain(t *testing.T) {
	m := newMenuModel()
	m = sendMenuKey(m, "enter")  // open host sub-screen
	m = sendMenuKey(m, "esc")    // go back
	if m.menuSubScreen != menuSubMain {
		t.Errorf("after Esc from host: subscreen = %d, want menuSubMain", m.menuSubScreen)
	}
}

// TestMenuEscFromJoinReturnsToMain verifies Esc from join sub-screen goes back.
func TestMenuEscFromJoinReturnsToMain(t *testing.T) {
	m := sendMenuKeys(newMenuModel(), "down", "enter") // open join sub-screen
	m = sendMenuKey(m, "esc")
	if m.menuSubScreen != menuSubMain {
		t.Errorf("after Esc from join: subscreen = %d, want menuSubMain", m.menuSubScreen)
	}
}

// ─── Host sub-screen tests ────────────────────────────────────────────────────

// TestHostTogglePublic verifies the T key toggles the public-tunnel option.
func TestHostTogglePublic(t *testing.T) {
	m := sendMenuKey(newMenuModel(), "enter") // open host sub-screen
	if m.menuPublic {
		t.Error("public should default to false")
	}
	m = sendMenuKey(m, "t")
	if !m.menuPublic {
		t.Error("public should be true after t")
	}
	m = sendMenuKey(m, "t")
	if m.menuPublic {
		t.Error("public should toggle back to false after second t")
	}
	// Uppercase T also toggles.
	m = sendMenuKey(m, "T")
	if !m.menuPublic {
		t.Error("public should be true after uppercase T")
	}
}

// TestHostEnterProducesBootstrapCmd verifies that Enter on the host sub-screen
// returns a non-nil tea.Cmd.
func TestHostEnterProducesBootstrapCmd(t *testing.T) {
	m := sendMenuKey(newMenuModel(), "enter") // open host sub-screen
	_, cmd := m.handleMenuKey("enter")
	if cmd == nil {
		t.Error("pressing Enter on host sub-screen should return a non-nil Cmd")
	}
}

// ─── Join sub-screen tests ────────────────────────────────────────────────────

// TestJoinAddressInput verifies that typing characters updates menuAddrInput.
func TestJoinAddressInput(t *testing.T) {
	m := sendMenuKeys(newMenuModel(), "down", "enter") // open join sub-screen

	for _, ch := range "bore.pub:12345" {
		m = sendMenuKey(m, string(ch))
	}
	if m.menuAddrInput != "bore.pub:12345" {
		t.Errorf("menuAddrInput = %q, want %q", m.menuAddrInput, "bore.pub:12345")
	}
}

// TestJoinBackspace verifies backspace removes the last character.
func TestJoinBackspace(t *testing.T) {
	m := sendMenuKeys(newMenuModel(), "down", "enter")
	m = sendMenuKeys(m, "a", "b", "c")
	m = sendMenuKey(m, "backspace")
	if m.menuAddrInput != "ab" {
		t.Errorf("after backspace: %q, want %q", m.menuAddrInput, "ab")
	}
}

// TestJoinEmptyAddressShowsError verifies that Enter with an empty address
// sets menuAddrErr instead of producing a Cmd.
func TestJoinEmptyAddressShowsError(t *testing.T) {
	m := sendMenuKeys(newMenuModel(), "down", "enter")
	// No typing — address is empty.
	updated, cmd := m.handleMenuKey("enter")
	if cmd != nil {
		t.Error("Enter with empty address should return nil Cmd (show inline error)")
	}
	if updated.(Model).menuAddrErr == "" {
		t.Error("Enter with empty address should set menuAddrErr")
	}
}

// TestJoinAddressProducesBootstrapCmd verifies that Enter with a non-empty
// address returns a non-nil Cmd.
func TestJoinAddressProducesBootstrapCmd(t *testing.T) {
	m := sendMenuKeys(newMenuModel(), "down", "enter")
	for _, ch := range "192.168.1.10:7777" {
		m = sendMenuKey(m, string(ch))
	}
	_, cmd := m.handleMenuKey("enter")
	if cmd == nil {
		t.Error("Enter with a non-empty address should return a non-nil Cmd")
	}
}

// TestJoinBootstrapCmdProducesJoinMsg verifies the bootstrap Cmd returns a
// bootstrapJoinMsg when the JoinBootstrapFunc succeeds.
func TestJoinBootstrapCmdProducesJoinMsg(t *testing.T) {
	m := sendMenuKeys(newMenuModel(), "down", "enter")
	for _, ch := range "bore.pub:9999" {
		m = sendMenuKey(m, string(ch))
	}
	_, cmd := m.handleMenuKey("enter")
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	msg := cmd()
	switch v := msg.(type) {
	case bootstrapJoinMsg:
		if v.addr != "bore.pub:9999" {
			t.Errorf("bootstrapJoinMsg.addr = %q, want %q", v.addr, "bore.pub:9999")
		}
	default:
		t.Errorf("expected bootstrapJoinMsg, got %T", msg)
	}
}

// TestJoinBootstrapCmdProducesErrMsg verifies the bootstrap Cmd returns a
// bootstrapErrMsg when the JoinBootstrapFunc returns an error.
func TestJoinBootstrapCmdProducesErrMsg(t *testing.T) {
	errFn := func(addr string) (string, error) {
		return "", errors.New("dirección inválida")
	}
	m := NewMenu(noopHostFn, errFn, "dev")
	m = sendMenuKeys(m, "down", "enter")
	for _, ch := range "bad" {
		m = sendMenuKey(m, string(ch))
	}
	_, cmd := m.handleMenuKey("enter")
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	msg := cmd()
	switch v := msg.(type) {
	case bootstrapErrMsg:
		if !strings.Contains(v.err.Error(), "dirección inválida") {
			t.Errorf("bootstrapErrMsg.err = %q, want to contain 'dirección inválida'", v.err.Error())
		}
	default:
		t.Errorf("expected bootstrapErrMsg, got %T", msg)
	}
}

// ─── Bootstrap result message tests ──────────────────────────────────────────

// TestBootstrapJoinMsgTransitionsToLobby verifies that receiving a bootstrapJoinMsg
// sets the addr and transitions to screenLobby.
func TestBootstrapJoinMsgTransitionsToLobby(t *testing.T) {
	m := newMenuModel()
	newM, _ := m.Update(bootstrapJoinMsg{addr: "bore.pub:9999"})
	updated := newM.(Model)
	if updated.screen != screenLobby {
		t.Errorf("screen after bootstrapJoinMsg = %v, want screenLobby", updated.screen)
	}
	if updated.addr != "bore.pub:9999" {
		t.Errorf("addr after bootstrapJoinMsg = %q, want %q", updated.addr, "bore.pub:9999")
	}
}

// TestBootstrapHostMsgTransitionsToLobby verifies that receiving a bootstrapHostMsg
// sets the addr and transitions to screenLobby.
func TestBootstrapHostMsgTransitionsToLobby(t *testing.T) {
	m := newMenuModel()
	newM, _ := m.Update(bootstrapHostMsg{addr: "localhost:7777"})
	updated := newM.(Model)
	if updated.screen != screenLobby {
		t.Errorf("screen after bootstrapHostMsg = %v, want screenLobby", updated.screen)
	}
	if updated.addr != "localhost:7777" {
		t.Errorf("addr after bootstrapHostMsg = %q, want %q", updated.addr, "localhost:7777")
	}
}

// TestBootstrapErrMsgTransitionsToFatalError verifies that a bootstrap error
// transitions to screenFatalError and stores the message.
func TestBootstrapErrMsgTransitionsToFatalError(t *testing.T) {
	m := newMenuModel()
	newM, _ := m.Update(bootstrapErrMsg{err: errors.New("puerto ocupado")})
	updated := newM.(Model)
	if updated.screen != screenFatalError {
		t.Errorf("screen after bootstrapErrMsg = %v, want screenFatalError", updated.screen)
	}
	if !strings.Contains(updated.fatalError, "puerto ocupado") {
		t.Errorf("fatalError = %q, want it to contain 'puerto ocupado'", updated.fatalError)
	}
}

// ─── Fatal error screen tests ─────────────────────────────────────────────────

// TestFatalErrorViewNotEmpty verifies that viewFatalError returns a non-empty string.
func TestFatalErrorViewNotEmpty(t *testing.T) {
	m := NewFatalError("algo salió mal")
	view := m.viewFatalError()
	if strings.TrimSpace(view) == "" {
		t.Error("viewFatalError() returned empty string")
	}
	if !strings.Contains(view, "algo salió mal") {
		t.Error("viewFatalError() missing error message")
	}
	if !strings.Contains(view, "Enter") {
		t.Error("viewFatalError() missing Enter-to-quit hint")
	}
}

// TestFatalErrorEnterQuits verifies that pressing Enter on the fatal error screen
// returns a quit Cmd.
func TestFatalErrorEnterQuits(t *testing.T) {
	m := NewFatalError("test")
	_, cmd := m.handleFatalErrorKey("enter")
	if cmd == nil {
		t.Error("Enter on fatal error screen should return a quit Cmd")
	}
}

// ─── View rendering smoke tests ───────────────────────────────────────────────

// TestMenuViewMainNotEmpty verifies the main menu renders without panicking.
func TestMenuViewMainNotEmpty(t *testing.T) {
	m := newMenuModel()
	view := m.viewMenu()
	if strings.TrimSpace(view) == "" {
		t.Error("viewMenu() returned empty string")
	}
	for _, label := range menuItemLabels {
		if !strings.Contains(view, label) {
			t.Errorf("viewMenu() missing label %q", label)
		}
	}
}

// TestMenuViewHostNotEmpty verifies the host sub-screen renders without panicking.
func TestMenuViewHostNotEmpty(t *testing.T) {
	m := newMenuModel()
	m.menuSubScreen = menuSubHost
	view := m.viewMenu()
	if strings.TrimSpace(view) == "" {
		t.Error("viewMenu() host sub-screen returned empty string")
	}
	if !strings.Contains(view, "Crear sala") {
		t.Error("host sub-screen missing 'Crear sala' title")
	}
}

// TestMenuViewJoinNotEmpty verifies the join sub-screen renders without panicking.
func TestMenuViewJoinNotEmpty(t *testing.T) {
	m := newMenuModel()
	m.menuSubScreen = menuSubJoin
	view := m.viewMenu()
	if strings.TrimSpace(view) == "" {
		t.Error("viewMenu() join sub-screen returned empty string")
	}
	if !strings.Contains(view, "Unirse a sala") {
		t.Error("join sub-screen missing 'Unirse a sala' title")
	}
}

// TestMenuViewShowsCursorMarker verifies that the menu renders the ▶ marker
// on the currently selected item.
func TestMenuViewShowsCursorMarker(t *testing.T) {
	m := newMenuModel()
	view := m.viewMenu()
	if !strings.Contains(view, "▶") {
		t.Error("menu view missing ▶ cursor marker on selected item")
	}
}

// TestMenuViewPublicToggleRendered verifies that the host sub-screen reflects
// the toggle state in its rendered output.
func TestMenuViewPublicToggleRendered(t *testing.T) {
	m := newMenuModel()
	m.menuSubScreen = menuSubHost
	m.menuPublic = false
	view := m.viewMenu()
	// With toggle off, must not contain the checkmark.
	if strings.Contains(view, "✓") {
		t.Error("host sub-screen with public=false should not show checkmark ✓")
	}
	m.menuPublic = true
	viewOn := m.viewMenu()
	if !strings.Contains(viewOn, "✓") {
		t.Error("host sub-screen with public=true should show checkmark ✓")
	}
}

// ─── Visual composition tests ─────────────────────────────────────────────────

// TestMenuScreensVisual100x30 prints menu, host, and join screens at 100×30.
// Always passes; run with: go test -v -run TestMenuScreensVisual100x30
func TestMenuScreensVisual100x30(t *testing.T) {
	dims := []struct{ w, h int }{{100, 30}, {80, 24}}
	screens := []struct {
		name string
		sub  int
	}{
		{"menu-main", menuSubMain},
		{"host", menuSubHost},
		{"join", menuSubJoin},
	}
	for _, d := range dims {
		for _, s := range screens {
			m := makeMenuModel(d.w, d.h)
			m.menuSubScreen = s.sub
			if s.sub == menuSubJoin {
				m.menuAddrInput = "bore.pub:12345"
			}
			view := m.viewMenu()
			t.Logf("\n=== %s @ %dx%d ===\n%s", s.name, d.w, d.h, view)
			if strings.TrimSpace(view) == "" {
				t.Errorf("%s @ %dx%d: viewMenu() empty", s.name, d.w, d.h)
			}
		}
	}
}
