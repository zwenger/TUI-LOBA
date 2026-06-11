package client

// menu.go — start-menu screen shown when loba is launched without subcommands.
// Provides "Crear sala" / "Unirse a sala" / "Salir" with arrow-key navigation.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// minArtWidth is the terminal width below which we skip the big art entirely.
const minArtWidth = 60

// minArtHeight is the terminal height below which we skip the big art entirely.
const minArtHeight = 24

// sideBySideWidth is the terminal width at or above which title and wolf are
// placed side-by-side (JoinHorizontal); below it they stack vertically.
const sideBySideWidth = 90

// Menu item indices.
const (
	menuItemHost = 0
	menuItemJoin = 1
	menuItemQuit = 2
	menuItemCount = 3
)

// menuSubScreen values.
const (
	menuSubMain  = 0 // main item list
	menuSubJoin  = 1 // address-input sub-screen
	menuSubHost  = 2 // host-confirm sub-screen (shows public toggle)
)

// menuItemLabels are the display labels for the three main menu items.
var menuItemLabels = [menuItemCount]string{
	"Crear sala",
	"Unirse a sala",
	"Salir",
}

// ─── Menu key handler ─────────────────────────────────────────────────────────

func (m Model) handleMenuKey(key string) (tea.Model, tea.Cmd) {
	switch m.menuSubScreen {
	case menuSubMain:
		return m.handleMenuMainKey(key)
	case menuSubHost:
		return m.handleMenuHostKey(key)
	case menuSubJoin:
		return m.handleMenuJoinKey(key)
	}
	return m, nil
}

func (m Model) handleMenuMainKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.menuCursor > 0 {
			m.menuCursor--
		}
	case "down", "j":
		if m.menuCursor < menuItemCount-1 {
			m.menuCursor++
		}
	case "enter":
		switch m.menuCursor {
		case menuItemHost:
			m.menuSubScreen = menuSubHost
		case menuItemJoin:
			m.menuSubScreen = menuSubJoin
			m.menuAddrInput = ""
			m.menuAddrErr = ""
		case menuItemQuit:
			return m, tea.Quit
		}
	case "ctrl+c", "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleMenuHostKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.menuSubScreen = menuSubMain
	case "ctrl+c":
		return m, tea.Quit
	case "t", "T":
		// Toggle public tunnel.
		m.menuPublic = !m.menuPublic
	case "enter":
		if m.hostBootstrap == nil {
			// Should not happen; guard defensively.
			m.screen = screenFatalError
			m.fatalError = "función de inicio no disponible"
			return m, nil
		}
		return m, runHostBootstrap(m.hostBootstrap, m.menuPublic)
	}
	return m, nil
}

func (m Model) handleMenuJoinKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.menuSubScreen = menuSubMain
		m.menuAddrErr = ""
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		addr := strings.TrimSpace(m.menuAddrInput)
		if addr == "" {
			m.menuAddrErr = "La dirección no puede estar vacía."
			return m, nil
		}
		if m.joinBootstrap == nil {
			m.screen = screenFatalError
			m.fatalError = "función de unión no disponible"
			return m, nil
		}
		return m, runJoinBootstrap(m.joinBootstrap, addr)
	case "backspace":
		if len(m.menuAddrInput) > 0 {
			runes := []rune(m.menuAddrInput)
			m.menuAddrInput = string(runes[:len(runes)-1])
		}
	default:
		// Accept printable ASCII characters (space included for pasting).
		if len(key) == 1 && len([]rune(m.menuAddrInput)) < 60 {
			m.menuAddrInput += key
		}
	}
	return m, nil
}

// ─── Bootstrap commands ───────────────────────────────────────────────────────

// runHostBootstrap runs the host bootstrap in a goroutine and sends the result
// back as a bootstrapHostMsg or bootstrapErrMsg.
func runHostBootstrap(fn HostBootstrapFunc, public bool) tea.Cmd {
	return func() tea.Msg {
		progCh := make(chan *tea.Program, 1)
		addr, err := fn("7777", public, progCh)
		if err != nil {
			return bootstrapErrMsg{err: err}
		}
		var ch chan<- *tea.Program
		if public {
			ch = progCh
		}
		return bootstrapHostMsg{addr: addr, progCh: ch}
	}
}

// runJoinBootstrap normalises the address and sends a bootstrapJoinMsg.
func runJoinBootstrap(fn JoinBootstrapFunc, addr string) tea.Cmd {
	return func() tea.Msg {
		normalised, err := fn(addr)
		if err != nil {
			return bootstrapErrMsg{err: err}
		}
		return bootstrapJoinMsg{addr: normalised}
	}
}

// ─── Menu styles ──────────────────────────────────────────────────────────────

var (
	styleMenuSel   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("226"))
	styleMenuItem  = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styleMenuPanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("213")).
			Padding(1, 2)
	styleToggleOn  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("46"))
	styleToggleOff = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	stylePanelTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
)

// ─── Menu view ────────────────────────────────────────────────────────────────

// viewMenu renders the full start-menu screen.
// Wide/tall terminals: compact header (wordmark + hot dog side-by-side) + panel.
// Narrow/short terminals: single-line text header + panel.
func (m Model) viewMenu() string {
	useArt := m.width >= minArtWidth && m.height >= minArtHeight

	var content strings.Builder
	if useArt {
		content.WriteString(m.viewMenuArtHeader())
	} else {
		content.WriteString(header())
		content.WriteString("\n")
	}

	// Screen panel — same border style for all sub-screens.
	var panel string
	switch m.menuSubScreen {
	case menuSubMain:
		panel = m.viewMenuMainPanel(useArt)
	case menuSubHost:
		panel = m.viewMenuHostPanel()
	case menuSubJoin:
		panel = m.viewMenuJoinPanel()
	}
	content.WriteString(panel)

	// Update notice — dim, placed below the panel.
	if m.updateNotice != "" {
		content.WriteString("\n")
		content.WriteString(styleDim.Render(
			"nueva versión disponible: v" + m.updateNotice + " — loba update",
		))
	}

	raw := content.String()

	// Horizontal centering when we know the terminal width.
	if m.width > 0 {
		return lipgloss.Place(m.width, 0, lipgloss.Center, lipgloss.Top, raw)
	}
	return raw
}

// viewMenuArtHeader builds the compact retro banner:
//
//	[LOBA wordmark + subtitle]   [hot dog]
//
// The wordmark block (5 rows + 1 subtitle row = 6 rows) is vertically centered
// inside the hot dog height (12 rows) using JoinHorizontal(Center).
// Total header height: 12 rows + 1 blank separator = 13 rows.
func (m Model) viewMenuArtHeader() string {
	// Left column: wordmark (5 rows) + subtitle (1 row).
	wordmark := renderLobaWordmark()
	subtitle := styleDim.Render("rummy argentino")
	leftCol := lipgloss.JoinVertical(lipgloss.Left, wordmark, subtitle)

	// Right column: hot dog (12 rows).
	hotdog := renderHotDog()

	// Join side-by-side, center-aligned vertically, 3-space gap.
	banner := lipgloss.JoinHorizontal(lipgloss.Center, leftCol, "   ", hotdog)

	return banner + "\n"
}

// viewMenuMain delegates to viewMenuMainPanel for callers (including tests)
// that need the main-screen content directly.
func (m Model) viewMenuMain() string {
	return m.viewMenuMainPanel(m.width >= minArtWidth && m.height >= minArtHeight)
}

// viewMenuHost delegates to viewMenuHostPanel for callers that need the
// host sub-screen content directly.
func (m Model) viewMenuHost() string { return m.viewMenuHostPanel() }

// viewMenuJoin delegates to viewMenuJoinPanel for callers that need the
// join sub-screen content directly.
func (m Model) viewMenuJoin() string { return m.viewMenuJoinPanel() }

// viewMenuMainPanel renders the three-item list inside a rounded panel.
func (m Model) viewMenuMainPanel(showFan bool) string {
	var inner strings.Builder

	for i, label := range menuItemLabels {
		if i > 0 {
			inner.WriteString("\n")
		}
		if i == m.menuCursor {
			inner.WriteString(styleMenuSel.Render("▶  " + label))
		} else {
			inner.WriteString(styleMenuItem.Render("   " + label))
		}
	}

	if showFan {
		inner.WriteString("\n\n")
		inner.WriteString(cardFanFooter())
	}

	inner.WriteString("\n\n")
	inner.WriteString(helpBar(
		helpEntry{"↑↓ / k j", "mover"},
		helpEntry{"Enter", "elegir"},
		helpEntry{"Q", "salir"},
	))

	return styleMenuPanel.Render(inner.String()) + "\n"
}

// viewMenuHostPanel renders the "Crear sala" sub-screen inside a rounded panel.
func (m Model) viewMenuHostPanel() string {
	var inner strings.Builder

	inner.WriteString(stylePanelTitle.Render("Crear sala") + "\n\n")

	toggleLabel := "sala pública (túnel bore.pub)"
	if m.menuPublic {
		inner.WriteString(styleToggleOn.Render("[ ✓ ] " + toggleLabel) + "\n")
	} else {
		inner.WriteString(styleToggleOff.Render("[ · ] " + toggleLabel) + "\n")
	}
	inner.WriteString(styleDim.Render("Puerto: 7777 (por defecto)") + "\n")

	inner.WriteString("\n")
	inner.WriteString(helpBar(
		helpEntry{"T", "alternar túnel"},
		helpEntry{"Enter", "iniciar sala"},
		helpEntry{"Esc", "volver"},
	))

	return styleMenuPanel.Render(inner.String()) + "\n"
}

// viewMenuJoinPanel renders the address-input sub-screen inside a rounded panel.
func (m Model) viewMenuJoinPanel() string {
	var inner strings.Builder

	inner.WriteString(stylePanelTitle.Render("Unirse a sala") + "\n\n")
	inner.WriteString(styleDim.Render("Dirección del servidor (ej: bore.pub:12345 o 192.168.1.10:7777)") + "\n\n")
	inner.WriteString(styleInput.Render(m.menuAddrInput+"█") + "\n")
	if m.menuAddrErr != "" {
		inner.WriteString("\n" + styleErr.Render(m.menuAddrErr) + "\n")
	}
	inner.WriteString("\n")
	inner.WriteString(helpBar(
		helpEntry{"Enter", "conectar"},
		helpEntry{"Esc", "volver"},
	))

	return styleMenuPanel.Render(inner.String()) + "\n"
}

// ─── Fatal error view ─────────────────────────────────────────────────────────

func (m Model) viewFatalError() string {
	var b strings.Builder
	b.WriteString(header())
	b.WriteString("\n")
	b.WriteString(styleBox.Render(
		styleErr.Render("Error:")+"\n\n"+
			m.fatalError+"\n\n"+
			helpBar(helpEntry{"Enter", "salir"}),
	))
	return b.String()
}

func (m Model) handleFatalErrorKey(key string) (tea.Model, tea.Cmd) {
	if key == "enter" || key == "ctrl+c" || key == "q" {
		return m, tea.Quit
	}
	return m, nil
}

