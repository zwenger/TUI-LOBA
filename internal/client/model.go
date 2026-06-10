// Package client implements the Bubbletea TUI client for Loba.
package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"loba/internal/protocol"
	"net"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─── Styles ───────────────────────────────────────────────────────────────────

var (
	styleTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213")).Padding(0, 1)
	styleBox      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	styleRed      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleBlack    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styleJoker    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("201"))
	styleActive   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("226"))
	styleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleHelp     = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	styleErr      = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	styleSel      = lipgloss.NewStyle().Background(lipgloss.Color("237")).Bold(true)
	styleHeader   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	styleMeld     = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1)
	styleInput    = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("213")).Padding(0, 1)
	styleWinner   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("226")).Padding(1, 2)
	stylePickedUp = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")) // orange highlight for picked-up discard
)

// ─── Sort mode ────────────────────────────────────────────────────────────────

type sortMode int

const (
	sortDealt sortMode = iota // as dealt / server order
	sortByRank                // rank asc, then suit asc
	sortBySuit                // suit asc, then rank asc
)

func (s sortMode) String() string {
	switch s {
	case sortByRank:
		return "sort:rank"
	case sortBySuit:
		return "sort:suit"
	default:
		return "sort:dealt"
	}
}

// ─── Screen states ────────────────────────────────────────────────────────────

type screen int

const (
	screenName  screen = iota // name entry prompt
	screenLobby               // waiting room
	screenGame                // main game table
	screenRound               // round-end summary
	screenOver                // game-over / winner
)

// ─── Messages ─────────────────────────────────────────────────────────────────

type connectedMsg struct{ conn net.Conn }
type connErrMsg struct{ err error }
type serverMsg struct{ env protocol.Envelope }
type tickMsg struct{}

// ─── Model ────────────────────────────────────────────────────────────────────

// Model is the Bubbletea model for the Loba client.
type Model struct {
	screen screen
	addr   string
	name   string // player display name

	// name entry
	nameInput string
	nameErr   string

	// networking
	conn   net.Conn
	reader *bufio.Reader

	// lobby
	lobbyPlayers []string
	hostID       string
	selfID       string
	publicAddr   string // tunnel public address, non-empty when host used --public

	// game state
	state     *protocol.StateSnapshot
	lastError string
	events    []string

	// hand cursor / selection
	cursor   int
	selected map[int]bool // hand indexes selected (display indexes)

	// sortMode controls how the hand is displayed. Sorting is client-side only.
	// The displayToServer slice maps display-order index → server hand index so
	// that all card commands use server indices, avoiding desyncs.
	sortMode      sortMode
	displayToServer []int // len == len(state.Hand); nil means identity mapping

	// width / height for layout
	width  int
	height int
}

// New returns the initial model.
func New(addr, name string) Model {
	m := Model{
		addr:     addr,
		name:     name,
		selected: make(map[int]bool),
	}
	if name == "" {
		m.screen = screenName
	} else {
		m.screen = screenLobby // will connect first
	}
	return m
}

// ─── Init ─────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	if m.name != "" {
		return connectCmd(m.addr, m.name)
	}
	return nil
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case connectedMsg:
		m.conn = msg.conn
		m.reader = bufio.NewReader(msg.conn)
		// Send join command.
		cmd := protocol.Command{Type: protocol.CmdJoin, Name: m.name}
		_ = protocol.WriteJSON(m.conn, cmd)
		return m, readServerMsg(m.reader)

	case connErrMsg:
		// If we were mid-game when the connection dropped, show a rejoin hint.
		if m.screen == screenGame || m.screen == screenRound || m.screen == screenOver {
			m.lastError = "conexión perdida — volvé a unirte con el mismo nombre para retomar tu lugar"
		} else {
			m.lastError = "No se pudo conectar: " + msg.err.Error()
		}
		// Stop trying to read: set conn/reader to nil and don't return a new readServerMsg.
		m.conn = nil
		m.reader = nil
		return m, nil

	case serverMsg:
		m, cmd := m.handleEnvelope(msg.env)
		if m.reader == nil {
			return m, cmd
		}
		return m, tea.Batch(cmd, readServerMsg(m.reader))

	case tickMsg:
		if m.reader == nil {
			return m, nil
		}
		return m, readServerMsg(m.reader)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleEnvelope(env protocol.Envelope) (Model, tea.Cmd) {
	switch env.Type {
	case "lobby":
		var ls protocol.LobbyState
		if err := json.Unmarshal(env.Payload, &ls); err == nil {
			m.lobbyPlayers = ls.Players
			m.hostID = ls.HostID
			m.publicAddr = ls.PublicAddr
			m.screen = screenLobby
		}

	case protocol.EvtState:
		var snap protocol.StateSnapshot
		if err := json.Unmarshal(env.Payload, &snap); err == nil {
			// Discover selfID.
			for _, p := range snap.Players {
				if p.IsSelf {
					m.selfID = p.ID
					break
				}
			}
			m.state = &snap
			m.events = snap.Events
			m.lastError = ""
			switch snap.Phase {
			case "game_over":
				m.screen = screenOver
			case "round_end":
				m.screen = screenRound
			default:
				m.screen = screenGame
			}
			// Rebuild display→server index mapping for the current sort mode.
			m.displayToServer = buildSortMapping(snap.Hand, m.sortMode)
			// Clear selections: the hand may have changed size or order, so any
			// display indices held in selected would now map to different cards.
			// Always reset on state update to avoid stale-selection bugs.
			m.selected = make(map[int]bool)
			// Reset cursor if out of range.
			if m.cursor >= len(snap.Hand) {
				m.cursor = max(0, len(snap.Hand)-1)
			}
		}

	case protocol.EvtError:
		var e map[string]string
		if err := json.Unmarshal(env.Payload, &e); err == nil {
			m.lastError = e["message"]
		}

	case protocol.EvtMessage:
		var e map[string]string
		if err := json.Unmarshal(env.Payload, &e); err == nil {
			m.events = append(m.events, e["text"])
		}
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global quit.
	if key == "ctrl+c" || key == "q" && m.screen != screenGame {
		return m, tea.Quit
	}

	switch m.screen {
	case screenName:
		return m.handleNameKey(key)
	case screenLobby:
		return m.handleLobbyKey(key)
	case screenGame:
		return m.handleGameKey(key)
	case screenRound, screenOver:
		return m.handleRoundKey(key)
	}
	return m, nil
}

// ─── Name entry keys ──────────────────────────────────────────────────────────

func (m Model) handleNameKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "enter":
		name := strings.TrimSpace(m.nameInput)
		if name == "" {
			m.nameErr = "El nombre no puede estar vacío."
			return m, nil
		}
		m.name = name
		m.screen = screenLobby
		return m, connectCmd(m.addr, m.name)
	case "backspace":
		if len(m.nameInput) > 0 {
			m.nameInput = m.nameInput[:len(m.nameInput)-1]
		}
	default:
		if len(key) == 1 && len(m.nameInput) < 20 {
			m.nameInput += key
		}
	}
	return m, nil
}

// ─── Lobby keys ───────────────────────────────────────────────────────────────

func (m Model) handleLobbyKey(key string) (tea.Model, tea.Cmd) {
	if key == "s" || key == "enter" {
		if m.conn != nil {
			_ = protocol.WriteJSON(m.conn, protocol.Command{Type: protocol.CmdStart})
		}
	}
	return m, nil
}

// ─── Game keys ────────────────────────────────────────────────────────────────

func (m Model) handleGameKey(key string) (tea.Model, tea.Cmd) {
	if m.state == nil {
		return m, nil
	}

	handLen := len(m.state.Hand)
	isMyTurn := m.state.ActiveID == m.selfID

	switch key {
	case "left", "h":
		if m.cursor > 0 {
			m.cursor--
		}
	case "right", "l":
		if m.cursor < handLen-1 {
			m.cursor++
		}
	case " ":
		// Toggle selection.
		if m.selected[m.cursor] {
			delete(m.selected, m.cursor)
		} else {
			m.selected[m.cursor] = true
		}
	case "d":
		// Draw from stock.
		if isMyTurn && m.conn != nil {
			_ = protocol.WriteJSON(m.conn, protocol.Command{Type: protocol.CmdDrawStock})
		}
	case "t":
		// Take discard.
		if isMyTurn && m.conn != nil {
			_ = protocol.WriteJSON(m.conn, protocol.Command{Type: protocol.CmdDrawDiscard})
		}
	case "m":
		// Meld selected as pierna.
		if isMyTurn && m.conn != nil {
			_ = protocol.WriteJSON(m.conn, protocol.Command{
				Type:        protocol.CmdMeld,
				CardIndexes: m.serverIndexes(selectedSlice(m.selected)),
				MeldType:    "pierna",
			})
			m.selected = make(map[int]bool)
		}
	case "e":
		// Meld selected as escalera.
		if isMyTurn && m.conn != nil {
			_ = protocol.WriteJSON(m.conn, protocol.Command{
				Type:        protocol.CmdMeld,
				CardIndexes: m.serverIndexes(selectedSlice(m.selected)),
				MeldType:    "escalera",
			})
			m.selected = make(map[int]bool)
		}
	case "x":
		// Discard cursor card.
		if isMyTurn && m.conn != nil {
			_ = protocol.WriteJSON(m.conn, protocol.Command{
				Type:      protocol.CmdDiscard,
				CardIndex: m.serverIndex(m.cursor),
			})
			m.selected = make(map[int]bool)
		}
	case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// Lay off selected card(s) onto meld displayed as #N (1-based).
		// Digit "0" is not a valid meld label.
		if isMyTurn && len(m.selected) > 0 {
			displayNum := int(key[0] - '0')
			if displayNum == 0 {
				m.lastError = "Ingresá el número de combinación (1, 2, …)"
				break
			}
			// Translate 1-based display number to 0-based server index.
			serverMeldIdx := displayNum - 1
			if m.conn != nil {
				_ = protocol.WriteJSON(m.conn, protocol.Command{
					Type:        protocol.CmdLayOff,
					CardIndexes: m.serverIndexes(selectedSlice(m.selected)),
					MeldIndex:   serverMeldIdx,
				})
				m.selected = make(map[int]bool)
			}
		}
	case "s":
		// Cycle sort mode: dealt → rank → suit → dealt.
		m.sortMode = (m.sortMode + 1) % 3
		if m.state != nil {
			// Remap cursor to follow the same logical card.
			oldServerIdx := m.serverIndex(m.cursor)
			m.displayToServer = buildSortMapping(m.state.Hand, m.sortMode)
			// Find where the same server card ended up in the new display order.
			for dispIdx, srvIdx := range m.displayToServer {
				if srvIdx == oldServerIdx {
					m.cursor = dispIdx
					break
				}
			}
		}
	case "esc":
		m.selected = make(map[int]bool)
	case "ctrl+c", "q":
		return m, tea.Quit
	}
	return m, nil
}

// ─── Round/Over keys ──────────────────────────────────────────────────────────

func (m Model) handleRoundKey(key string) (tea.Model, tea.Cmd) {
	if (key == "enter" || key == "n") && m.conn != nil {
		_ = protocol.WriteJSON(m.conn, protocol.Command{Type: protocol.CmdNextRound})
	}
	if key == "q" || key == "ctrl+c" {
		return m, tea.Quit
	}
	return m, nil
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	switch m.screen {
	case screenName:
		return m.viewNameEntry()
	case screenLobby:
		return m.viewLobby()
	case screenGame:
		return m.viewGame()
	case screenRound:
		return m.viewRoundSummary()
	case screenOver:
		return m.viewGameOver()
	}
	return ""
}

func header() string {
	return styleHeader.Render("▄▀ L O B A ▀▄  ◈  Rummy Argentino") + "\n"
}

// ─── Name entry view ──────────────────────────────────────────────────────────

func (m Model) viewNameEntry() string {
	var b strings.Builder
	b.WriteString(header())
	b.WriteString("\n")
	b.WriteString(styleBox.Render(
		"Ingresá tu nombre:\n\n" +
			styleInput.Render(m.nameInput+"█") + "\n\n" +
			styleHelp.Render("Presioná Enter para confirmar"),
	))
	if m.nameErr != "" {
		b.WriteString("\n" + styleErr.Render(m.nameErr))
	}
	return b.String()
}

// ─── Lobby view ───────────────────────────────────────────────────────────────

var stylePublicAddr = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("46")). // bright green
	Padding(0, 1)

func (m Model) viewLobby() string {
	var b strings.Builder
	b.WriteString(header())
	b.WriteString("\n")

	// Public address banner (shown to all clients when the host used --public).
	if m.publicAddr != "" {
		banner := stylePublicAddr.Render(
			"DIRECCIÓN DE LA SALA: " + m.publicAddr + " — compartila con tus amigos",
		)
		b.WriteString(banner + "\n\n")
	}

	players := strings.Join(m.lobbyPlayers, "\n  ")
	content := fmt.Sprintf("  Jugadores conectados (%d/6):\n  %s\n\n  %s",
		len(m.lobbyPlayers),
		players,
		styleHelp.Render("Anfitrión: presioná S o Enter para comenzar"),
	)
	b.WriteString(styleBox.Render(content))

	if m.lastError != "" {
		b.WriteString("\n" + styleErr.Render(m.lastError))
	}
	b.WriteString("\n\n" + styleHelp.Render("Esperando que el anfitrión inicie la partida..."))
	return b.String()
}

// ─── Game view ────────────────────────────────────────────────────────────────

func (m Model) viewGame() string {
	if m.state == nil {
		return header() + "\nConectando..."
	}
	s := m.state

	var b strings.Builder
	b.WriteString(header())

	// ── Opponents row ──
	b.WriteString(m.renderOpponents(s))
	b.WriteString("\n")

	// ── Melds ──
	b.WriteString(m.renderMelds(s))
	b.WriteString("\n")

	// ── Stock / Discard ──
	b.WriteString(renderPiles(s) + "\n")

	// ── Your hand ──
	b.WriteString(m.renderHand(s))
	b.WriteString("\n")

	// ── Event log ──
	if len(m.events) > 0 {
		last := m.events[len(m.events)-1]
		b.WriteString(styleDim.Render("► "+last) + "\n")
	}
	if m.lastError != "" {
		b.WriteString(styleErr.Render("✗ "+m.lastError) + "\n")
	}

	// ── Help bar ──
	isMyTurn := s.ActiveID == m.selfID
	b.WriteString(m.renderHelp(isMyTurn))

	return b.String()
}

func (m Model) renderOpponents(s *protocol.StateSnapshot) string {
	var parts []string
	for _, p := range s.Players {
		if p.IsSelf {
			continue
		}
		turn := ""
		if p.IsActive {
			turn = styleActive.Render(" ◄")
		}
		line := fmt.Sprintf("%s%s  (%d cartas)  puntaje:%d",
			p.Name, turn, p.CardCount, p.TotalScore)
		if !p.Connected {
			line += styleDim.Render(" [desc]")
		}
		parts = append(parts, line)
	}
	if len(parts) == 0 {
		return styleDim.Render("Sin oponentes") + "\n"
	}
	return strings.Join(parts, "   ") + "\n"
}

func (m Model) renderMelds(s *protocol.StateSnapshot) string {
	if len(s.Melds) == 0 {
		return styleDim.Render("  (no hay combinaciones en la mesa todavía)") + "\n"
	}

	termWidth := m.width
	if termWidth <= 0 {
		termWidth = 120
	}

	var rows []string
	for _, mv := range s.Melds {
		// Build the card strip for this meld using 3-line mini cards joined horizontally.
		miniCards := make([]string, len(mv.Cards))
		for i, c := range mv.Cards {
			card := c // local copy
			miniCards[i] = renderMiniCard(&card)
		}
		cardStrip := lipgloss.JoinHorizontal(lipgloss.Top, miniCards...)

		typeLabel := strings.ToUpper(mv.Type[:1]) + mv.Type[1:]
		header := styleDim.Render(fmt.Sprintf("#%d %s [%s]", mv.Index+1, typeLabel, mv.OwnerName))
		meldBlock := lipgloss.JoinVertical(lipgloss.Left, header, cardStrip)
		meldBlock = styleMeld.Render(meldBlock)

		// Flow melds left-to-right, wrapping when they don't fit.
		if len(rows) == 0 {
			rows = append(rows, meldBlock)
			continue
		}
		// Append to last row if it fits, otherwise start a new row.
		lastRow := rows[len(rows)-1]
		candidate := lipgloss.JoinHorizontal(lipgloss.Top, lastRow, "  ", meldBlock)
		if lipgloss.Width(candidate) <= termWidth {
			rows[len(rows)-1] = candidate
		} else {
			rows = append(rows, meldBlock)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...) + "\n"
}

func (m Model) renderHand(s *protocol.StateSnapshot) string {
	if len(s.Hand) == 0 {
		return styleDim.Render("Tu mano está vacía.") + "\n"
	}

	// Self info.
	selfName := ""
	selfScore := "?"
	for _, p := range s.Players {
		if p.IsSelf {
			selfName = p.Name
			selfScore = fmt.Sprintf("%d", p.TotalScore)
			break
		}
	}
	isMyTurn := s.ActiveID == m.selfID
	turnMark := ""
	if isMyTurn {
		turnMark = styleActive.Render(" ◄ TU TURNO")
	}
	handHeader := fmt.Sprintf("Tu mano — %s%s  (puntaje: %s)", selfName, turnMark, selfScore)

	// Build sorted display order.
	displayHand := make([]protocol.CardView, len(s.Hand))
	for dispIdx, srvIdx := range m.displayToServer {
		displayHand[dispIdx] = s.Hand[srvIdx]
	}

	// Determine which display index corresponds to the picked-up discard.
	pickedDisplayIdx := -1
	if s.PickedUpDiscard != nil {
		for dispIdx, srvIdx := range m.displayToServer {
			hc := s.Hand[srvIdx]
			if hc.Rank == s.PickedUpDiscard.Rank && hc.Suit == s.PickedUpDiscard.Suit && hc.JokerIndex == s.PickedUpDiscard.JokerIndex {
				pickedDisplayIdx = dispIdx
				break
			}
		}
	}

	// Render fanned hand: all cards except the cursor use a narrow 5-line "fan tab"
	// block; the cursor card gets the full 5-line card box. All blocks share the
	// same height so lipgloss.JoinHorizontal aligns them correctly on one strip.
	blocks := make([]string, len(displayHand))
	for i, cv := range displayHand {
		isCursor := i == m.cursor
		isSelected := m.selected[i]
		isPicked := i == pickedDisplayIdx

		if isCursor {
			blocks[i] = renderCardBox(cv, isSelected, isPicked)
		} else {
			blocks[i] = renderCardFanTab(cv, isSelected, isPicked)
		}
	}

	strip := lipgloss.JoinHorizontal(lipgloss.Top, blocks...)
	return handHeader + "\n" + strip + "\n"
}

func (m Model) renderHelp(isMyTurn bool) string {
	sortLabel := styleDim.Render("[S: " + m.sortMode.String() + "]")
	pickedNote := ""
	if m.state != nil && m.state.PickedUpDiscard != nil {
		pickedNote = stylePickedUp.Render("  [★ debés jugar " + m.state.PickedUpDiscard.Label + " antes de descartar]")
	}
	if !isMyTurn {
		return styleHelp.Render("← →/h l: mover cursor  Espacio: seleccionar  S: ordenar  (esperando)") + " " + sortLabel + pickedNote + "\n"
	}
	phase := ""
	if m.state != nil {
		phase = m.state.Phase
	}
	switch phase {
	case "draw":
		return styleHelp.Render("D: robar del mazo  T: tomar del pozo  S: ordenar") + " " + sortLabel + "\n"
	default:
		return styleHelp.Render("Espacio: seleccionar  M: pierna  E: escalera  1-9: agregar en comb.#N  X: descartar  S: ordenar  Esc: limpiar") + " " + sortLabel + pickedNote + "\n"
	}
}

// ─── Round summary view ───────────────────────────────────────────────────────

func (m Model) viewRoundSummary() string {
	var b strings.Builder
	b.WriteString(header())
	b.WriteString(styleTitle.Render(fmt.Sprintf("Ronda %d — Fin", m.state.Round)) + "\n\n")

	if m.state != nil {
		for _, p := range m.state.Players {
			line := fmt.Sprintf("  %-20s  esta ronda: +%d   total: %d",
				p.Name, p.RoundScore, p.TotalScore)
			if p.TotalScore > 101 {
				line += styleErr.Render("  ELIMINADO")
			}
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\n" + styleHelp.Render("Enter / N: siguiente ronda  ·  Q: salir"))
	return b.String()
}

// ─── Game over view ───────────────────────────────────────────────────────────

func (m Model) viewGameOver() string {
	var b strings.Builder
	b.WriteString(header())

	winner := ""
	if m.state != nil {
		winner = m.state.WinnerName
	}
	b.WriteString(styleWinner.Render(fmt.Sprintf("🏆  GANADOR: %s", winner)) + "\n\n")

	if m.state != nil {
		for _, p := range m.state.Players {
			line := fmt.Sprintf("  %-20s  total: %d", p.Name, p.TotalScore)
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\n" + styleHelp.Render("Q: salir"))
	return b.String()
}

// ─── Card rendering ───────────────────────────────────────────────────────────

// renderCardBox renders a full ASCII box card (used for the cursor card).
// Layout (7 chars wide × 5 lines tall):
//
//	╭─────╮
//	│R    │
//	│  S  │
//	│    R│
//	╰─────╯
func renderCardBox(c protocol.CardView, selected, pickedUp bool) string {
	if c.Hidden {
		return hiddenCardBox()
	}

	rank := rankString(c.Rank)   // 2-char string, e.g. " A", " 7", "10"
	suit := suitString(c.Suit)   // single rune, e.g. "♥"
	isJoker := c.Rank == 0

	var top, l1, l2, l3, bot string
	if selected {
		top = "╔═════╗"
		bot = "╚═════╝"
		l1 = fmt.Sprintf("║%s   ║", rank)
		if isJoker {
			l2 = "║ JKR ║"
		} else {
			l2 = fmt.Sprintf("║  %s  ║", suit)
		}
		l3 = fmt.Sprintf("║   %s║", rank)
	} else {
		top = "╭─────╮"
		bot = "╰─────╯"
		l1 = fmt.Sprintf("│%s   │", rank)
		if isJoker {
			l2 = "│ JKR │"
		} else {
			l2 = fmt.Sprintf("│  %s  │", suit)
		}
		l3 = fmt.Sprintf("│   %s│", rank)
	}

	box := top + "\n" + l1 + "\n" + l2 + "\n" + l3 + "\n" + bot
	box = applyCardColor(box, c.Rank, c.Suit, pickedUp)
	return box
}

// renderCardFanTab renders a narrow 5-line "fan tab" block for a non-cursor card.
// All 5 rows are the same width (4 cols: left-border + rank + suit + space) so
// that lipgloss.JoinHorizontal can align tab blocks and the full cursor card box
// on the same baseline without any misalignment.
//
// Layout (4 cols × 5 lines):
//
//	╭──      (selected: ╔══)
//	│Rk      (selected: ║Rk)
//	│Su      (selected: ║Su)
//	│        (selected: ║  )   ← lifted: blank filler row for selected cards
//	│        (selected: ╚══)   ← bottom border only for selected
//
// Non-selected bottom two rows are plain border+space to complete the height.
func renderCardFanTab(c protocol.CardView, selected, pickedUp bool) string {
	if c.Hidden {
		// Face-down tab: show back pattern.
		raw := "╭──\n│░░\n│░░\n│░░\n╰──"
		return applyCardColor(raw, 0, -1, pickedUp)
	}

	isJoker := c.Rank == 0
	rank := rankString(c.Rank) // 2 chars, e.g. " A", "10"
	suit := suitString(c.Suit)
	if isJoker {
		rank = "  "
		suit = "★"
	}

	var top, r1, r2, r3, bot string
	if selected {
		// Double-line border = selected indicator; content shifted up one row.
		top = "╔══"
		r1 = fmt.Sprintf("║%s", rank)
		r2 = fmt.Sprintf("║%s ", suit)
		r3 = "║  "
		bot = "╚══"
	} else {
		top = "╭──"
		r1 = fmt.Sprintf("│%s", rank)
		r2 = fmt.Sprintf("│%s ", suit)
		r3 = "│  "
		bot = "╰──"
	}

	raw := top + "\n" + r1 + "\n" + r2 + "\n" + r3 + "\n" + bot
	return applyCardColor(raw, c.Rank, c.Suit, pickedUp)
}

func hiddenCardBox() string {
	return "╭─────╮\n│░░░░░│\n│░░░░░│\n│░░░░░│\n╰─────╯"
}

// renderMiniCard renders a compact 3-line card box used in melds and the discard pile.
// Layout (6 cols × 3 lines):
//
//	╭────╮
//	│ RS │   (rank + suit, centered)
//	╰────╯
func renderMiniCard(c *protocol.CardView) string {
	if c == nil {
		return "╭────╮\n│    │\n╰────╯"
	}
	if c.Hidden {
		return applyCardColor("╭────╮\n│░░░░│\n╰────╯", 0, -1, false)
	}
	rank := rankString(c.Rank)
	suit := suitString(c.Suit)
	// rank is 2 chars, suit is 1 char; interior of box is 4 chars wide.
	label := rank + suit + " " // 4 chars total
	if c.Rank == 0 {
		label = "JKR "
	}
	raw := fmt.Sprintf("╭────╮\n│%s│\n╰────╯", label)
	return applyCardColor(raw, c.Rank, c.Suit, false)
}

// renderStockPile renders a face-down 3-line box showing the stock count.
func renderStockPile(count int) string {
	label := fmt.Sprintf("%2d", count)
	raw := fmt.Sprintf("╭────╮\n│░%s░│\n╰────╯", label)
	return styleDim.Render(raw)
}

// renderPiles renders the discard top and stock pile side-by-side with labels.
func renderPiles(s *protocol.StateSnapshot) string {
	var discardBlock string
	if s.DiscardTop != nil {
		discardBlock = lipgloss.JoinVertical(lipgloss.Center,
			styleDim.Render("pozo"),
			renderMiniCard(s.DiscardTop),
		)
	} else {
		discardBlock = lipgloss.JoinVertical(lipgloss.Center,
			styleDim.Render("pozo"),
			styleDim.Render("╭────╮\n│    │\n╰────╯"),
		)
	}

	stockBlock := lipgloss.JoinVertical(lipgloss.Center,
		styleDim.Render("mazo"),
		renderStockPile(s.StockCount),
	)

	return lipgloss.JoinHorizontal(lipgloss.Top, discardBlock, "  ", stockBlock)
}

// applyCardColor applies the correct color style based on suit/rank.
func applyCardColor(s string, rank, suit int, pickedUp bool) string {
	if pickedUp {
		return stylePickedUp.Render(s)
	}
	if rank == 0 {
		return styleJoker.Render(s)
	}
	if suit == 1 || suit == 2 { // Hearts, Diamonds
		return styleRed.Render(s)
	}
	return styleBlack.Render(s)
}

// renderCardLabel renders a short inline card label (for melds, discard top, etc.)
func renderCardLabel(c *protocol.CardView) string {
	if c == nil {
		return "?"
	}
	label := c.Label
	if label == "" {
		label = cardLabelFromView(*c)
	}
	return applyCardColor(label, c.Rank, c.Suit, false)
}

func cardLabelFromView(c protocol.CardView) string {
	if c.Hidden {
		return "??"
	}
	if c.Rank == 0 {
		return "★JKR"
	}
	return rankString(c.Rank) + suitString(c.Suit)
}

func rankString(rank int) string {
	switch rank {
	case 1:
		return " A"
	case 11:
		return " J"
	case 12:
		return " Q"
	case 13:
		return " K"
	default:
		return fmt.Sprintf("%2d", rank)
	}
}

func suitString(suit int) string {
	switch suit {
	case 0:
		return "♠"
	case 1:
		return "♥"
	case 2:
		return "♦"
	case 3:
		return "♣"
	default:
		return "★"
	}
}

// ─── Sort mapping helpers ─────────────────────────────────────────────────────

// buildSortMapping returns a slice where result[displayIdx] = serverIdx.
func buildSortMapping(hand []protocol.CardView, mode sortMode) []int {
	n := len(hand)
	mapping := make([]int, n)
	for i := range mapping {
		mapping[i] = i
	}
	switch mode {
	case sortByRank:
		sort.SliceStable(mapping, func(a, b int) bool {
			ca, cb := hand[mapping[a]], hand[mapping[b]]
			if ca.Rank != cb.Rank {
				// Joker (rank 0) sorts last.
				if ca.Rank == 0 {
					return false
				}
				if cb.Rank == 0 {
					return true
				}
				return ca.Rank < cb.Rank
			}
			return ca.Suit < cb.Suit
		})
	case sortBySuit:
		sort.SliceStable(mapping, func(a, b int) bool {
			ca, cb := hand[mapping[a]], hand[mapping[b]]
			if ca.Suit != cb.Suit {
				return ca.Suit < cb.Suit
			}
			if ca.Rank != cb.Rank {
				if ca.Rank == 0 {
					return false
				}
				if cb.Rank == 0 {
					return true
				}
				return ca.Rank < cb.Rank
			}
			return false
		})
	}
	return mapping
}

// serverIndex converts a display-order index to a server hand index.
func (m Model) serverIndex(displayIdx int) int {
	if displayIdx < 0 || displayIdx >= len(m.displayToServer) {
		return displayIdx
	}
	return m.displayToServer[displayIdx]
}

// serverIndexes converts a slice of display indices to server hand indices.
func (m Model) serverIndexes(displayIdxs []int) []int {
	result := make([]int, len(displayIdxs))
	for i, di := range displayIdxs {
		result[i] = m.serverIndex(di)
	}
	return result
}

// ─── Tea commands ─────────────────────────────────────────────────────────────

func connectCmd(addr, name string) tea.Cmd {
	return func() tea.Msg {
		conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
		if err != nil {
			return connErrMsg{err: err}
		}
		return connectedMsg{conn: conn}
	}
}

func readServerMsg(r *bufio.Reader) tea.Cmd {
	return func() tea.Msg {
		var env protocol.Envelope
		if err := protocol.ReadJSON(r, &env); err != nil {
			return connErrMsg{err: err}
		}
		return serverMsg{env: env}
	}
}

// ─── Utilities ────────────────────────────────────────────────────────────────

func selectedSlice(m map[int]bool) []int {
	s := make([]int, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
