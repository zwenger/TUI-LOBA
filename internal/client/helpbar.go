package client

// helpbar.go — shared helper for rendering styled key/description help bars.
//
// Every screen's help line is built from helpEntry pairs so that key tokens
// are visually distinct from their descriptions. No hand-concatenated flat
// help strings should remain in view code; use helpBar() instead.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// helpEntry is a single key/description pair for a help bar.
type helpEntry struct {
	key  string // e.g. "Espacio", "M", "Shift+L"
	desc string // e.g. "seleccionar", "pierna"
}

var (
	// styleHelpKey renders key tokens in bold amber/orange.
	styleHelpKey  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	// styleHelpDesc renders descriptions in dim gray (same hue as styleHelp, non-italic).
	styleHelpDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	// styleHelpSep renders the " · " separator between entries in dark gray.
	styleHelpSep  = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
)

// helpBar renders a horizontal help bar from the given key/description pairs.
// Each key is styled bold amber, each description dim gray, entries separated
// by " · " in dark gray.
//
// Example:
//
//	helpBar(
//	    helpEntry{"Espacio", "seleccionar"},
//	    helpEntry{"M", "pierna"},
//	)
func helpBar(entries ...helpEntry) string {
	sep := styleHelpSep.Render(" · ")
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = styleHelpKey.Render(e.key) + " " + styleHelpDesc.Render(e.desc)
	}
	return strings.Join(parts, sep)
}
