package client

// menu.go — start-menu screen shown when loba is launched without subcommands.
// Provides "Crear sala" / "Unirse a sala" / "Salir" with arrow-key navigation.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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

// ─── Menu view ────────────────────────────────────────────────────────────────

var (
	styleMenuSel  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("226"))
	styleMenuItem = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styleMenuBox  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("213")).Padding(1, 3)
	styleToggleOn  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("46"))
	styleToggleOff = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

func (m Model) viewMenu() string {
	var b strings.Builder
	b.WriteString(header())
	b.WriteString("\n")

	switch m.menuSubScreen {
	case menuSubMain:
		b.WriteString(m.viewMenuMain())
	case menuSubHost:
		b.WriteString(m.viewMenuHost())
	case menuSubJoin:
		b.WriteString(m.viewMenuJoin())
	}
	return b.String()
}

func (m Model) viewMenuMain() string {
	var b strings.Builder

	var items strings.Builder
	for i, label := range menuItemLabels {
		if i == m.menuCursor {
			items.WriteString(styleMenuSel.Render("▶  " + label))
		} else {
			items.WriteString(styleMenuItem.Render("   " + label))
		}
		if i < menuItemCount-1 {
			items.WriteString("\n")
		}
	}
	b.WriteString(styleMenuBox.Render(items.String()))
	b.WriteString("\n\n")
	b.WriteString(helpBar(
		helpEntry{"↑↓ / k j", "mover"},
		helpEntry{"Enter", "elegir"},
		helpEntry{"Q", "salir"},
	))
	if m.updateNotice != "" {
		b.WriteString("\n")
		b.WriteString(styleDim.Render(
			"Hay una versión nueva (v" + m.updateNotice + ") — actualizá con: loba update",
		))
	}
	return b.String()
}

func (m Model) viewMenuHost() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Crear sala") + "\n\n")

	toggleLabel := "sala pública (túnel bore.pub)"
	if m.menuPublic {
		b.WriteString("  " + styleToggleOn.Render("[ ✓ ] "+toggleLabel) + "\n")
	} else {
		b.WriteString("  " + styleToggleOff.Render("[ · ] "+toggleLabel) + "\n")
	}
	b.WriteString(styleDim.Render("  Puerto: 7777 (por defecto)") + "\n")
	b.WriteString("\n")
	b.WriteString(helpBar(
		helpEntry{"T", "alternar túnel"},
		helpEntry{"Enter", "iniciar sala"},
		helpEntry{"Esc", "volver"},
	))
	return b.String()
}

func (m Model) viewMenuJoin() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Unirse a sala") + "\n\n")
	b.WriteString(styleDim.Render("  Dirección del servidor (ej: bore.pub:12345 o 192.168.1.10:7777)") + "\n\n")
	b.WriteString("  " + styleInput.Render(m.menuAddrInput+"█") + "\n")
	if m.menuAddrErr != "" {
		b.WriteString("\n  " + styleErr.Render(m.menuAddrErr) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(helpBar(
		helpEntry{"Enter", "conectar"},
		helpEntry{"Esc", "volver"},
	))
	return b.String()
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

