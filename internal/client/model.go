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
	// Four-color deck (poker convention): hearts red, diamonds blue,
	// clubs green, spades white — each suit instantly recognizable.
	styleHearts   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleDiamonds = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	styleClubs    = lipgloss.NewStyle().Foreground(lipgloss.Color("41"))
	styleSpades   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
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

	// Opponent badge styles.
	styleBadgeNormal = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("63")).
				Padding(0, 1)
	styleBadgeActive = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder()).
				BorderForeground(lipgloss.Color("226")).
				Padding(0, 1).
				Bold(true)
	styleBadgeDisconn = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("240")).
				Padding(0, 1)
	// styleBadgeSelf is used for the self badge when it is NOT the active turn.
	// A teal/cyan border distinguishes self from other players without conflating
	// with the yellow thick border reserved for the active-turn player.
	styleBadgeSelf = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("51")).
			Padding(0, 1)
	styleBadgeName       = lipgloss.NewStyle().Bold(true)
	styleBadgeNameActive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("226"))
	styleBadgeNameSelf   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51"))
	styleBadgeDisconnStr = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleBadgeChipSep    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
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
	screenMenu      screen = iota // start menu (no-args launch)
	screenName                    // name entry prompt
	screenLobby                   // waiting room
	screenSeats                   // seat picker (reconnect to a started game)
	screenGame                    // main game table
	screenRound                   // round-end summary
	screenOver                    // game-over / winner
	screenScoreTable              // full-screen score table (toggled by P)
	screenEventLog                // full-screen event log   (toggled by L)
	screenFatalError              // fatal error before/at startup; Enter to quit
)

// maxClientEventHistory is the cap for the client-side event history.
// Newest entries are kept when the cap is exceeded.
const maxClientEventHistory = 200

// ─── Messages ─────────────────────────────────────────────────────────────────

type connectedMsg struct{ conn net.Conn }
type connErrMsg struct{ err error }
type serverMsg struct{ env protocol.Envelope }
type tickMsg struct{}

// HostBootstrapFunc is called when the user selects "Crear sala" from the menu.
// It must start the server in-process, open the tunnel if public is true, and
// return the local address to connect to (e.g. "localhost:7777"). It may also
// return a non-nil *tea.Program after the TUI starts via the progCh channel
// (for tunnel share-line printing); that is orchestrated outside the model.
type HostBootstrapFunc func(port string, public bool, progCh chan<- *tea.Program) (localAddr string, err error)

// JoinBootstrapFunc validates and normalises the user-supplied address.
type JoinBootstrapFunc func(addr string) (normalised string, err error)

// bootstrapHostMsg is sent back to the model when the host bootstrap succeeded.
type bootstrapHostMsg struct {
	addr   string
	progCh chan<- *tea.Program // non-nil when public tunnel was requested
}

// bootstrapJoinMsg is sent back to the model when the join address is ready.
type bootstrapJoinMsg struct{ addr string }

// bootstrapErrMsg carries a fatal bootstrap error to display in-TUI.
type bootstrapErrMsg struct{ err error }

// ─── Model ────────────────────────────────────────────────────────────────────

// Model is the Bubbletea model for the Loba client.
type Model struct {
	screen screen
	addr   string
	name   string // player display name

	// menu (start screen shown when launched with no subcommand)
	menuCursor    int    // 0=Crear sala 1=Unirse a sala 2=Salir
	menuPublic    bool   // "sala pública (túnel)" toggle for host
	menuAddrInput string // address typed in the join sub-screen
	menuAddrErr   string // inline validation error for join address
	menuSubScreen int    // 0=main menu  1=join address input  2=host confirm

	// bootstrap callbacks injected by main when launching in menu mode
	hostBootstrap HostBootstrapFunc
	joinBootstrap JoinBootstrapFunc

	// fatalError is shown on screenFatalError; user presses Enter to quit.
	fatalError string

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

	// seat picker (reconnect to a started game)
	seats       []protocol.SeatEntry
	seatCursor  int

	// game state
	state     *protocol.StateSnapshot
	lastError string
	events    []string // current-round events (last N from server)

	// eventHistory is the client-side lifetime log, capped at maxClientEventHistory.
	// On each state update we merge the server's EventLogTail (for reconnects) and
	// the current Events slice; on EvtMessage we append directly.
	eventHistory []string

	// overlayFrom stores the screen we were on before opening an overlay (score
	// table or event log) so that Esc / the toggle key can return to it.
	overlayFrom screen

	// eventLogOffset is the scroll position in the event log view.
	// 0 = show the newest entries (bottom of the list).
	// Positive values scroll toward older entries.
	eventLogOffset int

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

// New returns the initial model for a direct host/join launch (CLI subcommand path).
func New(addr, name string) Model {
	m := Model{
		addr:     addr,
		name:     name,
		selected: make(map[int]bool),
		// Always start connecting immediately; if name is empty we show the name
		// screen only after the server asks for one (EvtNameRequired). For a started
		// game the name is never needed — the seat defines identity.
		screen: screenLobby,
	}
	return m
}

// NewMenu returns the initial model for an interactive (no-args) launch.
// The caller must inject hostBootstrap and joinBootstrap so the menu can
// trigger the same bootstrap logic as the CLI subcommands.
func NewMenu(hostFn HostBootstrapFunc, joinFn JoinBootstrapFunc) Model {
	return Model{
		screen:        screenMenu,
		selected:      make(map[int]bool),
		hostBootstrap: hostFn,
		joinBootstrap: joinFn,
	}
}

// NewFatalError returns a model that immediately shows a fatal error with an
// "Enter to quit" prompt. Used when a pre-TUI error must be shown inside the TUI.
func NewFatalError(msg string) Model {
	return Model{
		screen:     screenFatalError,
		fatalError: msg,
		selected:   make(map[int]bool),
	}
}

// ─── Init ─────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	// Menu and fatal-error screens don't connect on start.
	if m.screen == screenMenu || m.screen == screenFatalError {
		return nil
	}
	// Connect immediately regardless of whether we have a name.
	// The server will tell us if it needs a name (lobby + empty name path).
	return connectCmd(m.addr)
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case bootstrapHostMsg:
		// Host bootstrap succeeded; now connect to localhost as a player.
		m.addr = msg.addr
		m.screen = screenLobby
		return m, connectCmd(m.addr)

	case bootstrapJoinMsg:
		// Address is ready; connect to the remote server.
		m.addr = msg.addr
		m.screen = screenLobby
		return m, connectCmd(m.addr)

	case bootstrapErrMsg:
		// Bootstrap failed (port busy, tunnel error, etc.) — show in-TUI error.
		m.screen = screenFatalError
		m.fatalError = msg.err.Error()
		return m, nil

	case connectedMsg:
		m.conn = msg.conn
		m.reader = bufio.NewReader(msg.conn)
		// Send join command with whatever name we have (may be empty for lobby path
		// without --name; the server will ask for one via EvtNameRequired).
		cmd := protocol.Command{Type: protocol.CmdJoin, Name: m.name}
		_ = protocol.WriteJSON(m.conn, cmd)
		return m, readServerMsg(m.reader)

	case connErrMsg:
		// If we were mid-game when the connection dropped, show a rejoin hint.
		if m.screen == screenGame || m.screen == screenRound || m.screen == screenOver {
			m.lastError = "conexión perdida — volvé a unirte y elegí tu lugar para retomar"
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

	case protocol.EvtSeats:
		var offer protocol.SeatsOffer
		if err := json.Unmarshal(env.Payload, &offer); err == nil {
			m.seats = offer.Seats
			m.seatCursor = 0
			m.screen = screenSeats
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

			// Merge the server's EventLogTail into our local history.
			// On a fresh connect or reconnect the tail provides recent context
			// that may predate this session. We deduplicate by checking whether
			// the tail's last entry already appears at the end of our history.
			m.mergeEventLogTail(snap.EventLogTail)
			// Also merge this round's Events in case they're not already there.
			m.appendEventHistory(snap.Events...)

			m.lastError = ""
			// Don't override overlay screens when we're in them.
			if m.screen != screenScoreTable && m.screen != screenEventLog {
				switch snap.Phase {
				case "game_over":
					m.screen = screenOver
				case "round_end":
					m.screen = screenRound
				default:
					m.screen = screenGame
				}
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

	case protocol.EvtNameRequired:
		// Server is asking us for a name (lobby join with empty name).
		// Switch to the name entry screen; the connection stays alive.
		m.screen = screenName
		m.nameInput = ""
		m.nameErr = ""

	case protocol.EvtError:
		var e map[string]string
		if err := json.Unmarshal(env.Payload, &e); err == nil {
			m.lastError = e["message"]
		}

	case protocol.EvtMessage:
		var e map[string]string
		if err := json.Unmarshal(env.Payload, &e); err == nil {
			m.events = append(m.events, e["text"])
			m.appendEventHistory(e["text"])
		}
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global quit (q works on all screens except in-game where it conflicts with lay-off numbering).
	if key == "ctrl+c" || (key == "q" && m.screen != screenGame && m.screen != screenMenu) {
		return m, tea.Quit
	}

	switch m.screen {
	case screenMenu:
		return m.handleMenuKey(key)
	case screenFatalError:
		return m.handleFatalErrorKey(key)
	case screenName:
		return m.handleNameKey(key)
	case screenLobby:
		return m.handleLobbyKey(key)
	case screenSeats:
		return m.handleSeatsKey(key)
	case screenGame:
		return m.handleGameKey(key)
	case screenRound, screenOver:
		return m.handleRoundKey(key)
	case screenScoreTable:
		return m.handleScoreTableKey(key)
	case screenEventLog:
		return m.handleEventLogKey(key)
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
		m.nameInput = ""
		m.nameErr = ""
		m.screen = screenLobby
		// We are already connected (server prompted for our name). Send the join
		// command with the chosen name; the server will register and send lobby state.
		if m.conn != nil {
			_ = protocol.WriteJSON(m.conn, protocol.Command{Type: protocol.CmdJoin, Name: m.name})
		}
		return m, nil
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

// ─── Seat picker keys ─────────────────────────────────────────────────────────

func (m Model) handleSeatsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.seatCursor > 0 {
			m.seatCursor--
		}
	case "down", "j":
		if m.seatCursor < len(m.seats)-1 {
			m.seatCursor++
		}
	case "enter":
		if m.conn != nil && len(m.seats) > 0 {
			seat := m.seats[m.seatCursor]
			_ = protocol.WriteJSON(m.conn, protocol.Command{
				Type:   protocol.CmdClaimSeat,
				SeatID: seat.ID,
			})
		}
	case "esc", "q", "ctrl+c":
		return m, tea.Quit
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
		// We must remap both cursor and selected display indices so they continue
		// to refer to the same logical cards after the sort changes.
		if m.state != nil {
			// Step 1: snapshot the server index of the cursor and each selected
			// card BEFORE the mapping changes. Do this against the current
			// (pre-change) displayToServer slice.
			oldCursorSrv := m.serverIndex(m.cursor)

			// Collect server indices for every currently-selected display index.
			selectedSrvIdxs := make([]int, 0, len(m.selected))
			for oldDisp := range m.selected {
				selectedSrvIdxs = append(selectedSrvIdxs, m.serverIndex(oldDisp))
			}

			// Step 2: advance mode and rebuild the display→server mapping.
			m.sortMode = (m.sortMode + 1) % 3
			m.displayToServer = buildSortMapping(m.state.Hand, m.sortMode)

			// Step 3: build a reverse map server-index → new display index so
			// we can relocate each tracked card in O(n).
			newDispForSrv := make([]int, len(m.state.Hand))
			for newDisp, srv := range m.displayToServer {
				newDispForSrv[srv] = newDisp
			}

			// Step 4: remap cursor.
			if oldCursorSrv >= 0 && oldCursorSrv < len(newDispForSrv) {
				m.cursor = newDispForSrv[oldCursorSrv]
			}

			// Step 5: remap selection — translate each server index back to its
			// new display position. This is the fix for the sort-change stale-
			// selection bug: without this, old display indices point to different
			// cards in the new sort order.
			newSelected := make(map[int]bool, len(selectedSrvIdxs))
			for _, srvIdx := range selectedSrvIdxs {
				if srvIdx >= 0 && srvIdx < len(newDispForSrv) {
					newSelected[newDispForSrv[srvIdx]] = true
				}
			}
			m.selected = newSelected
		}
	case "p", "P":
		// Toggle score table overlay.
		m.overlayFrom = screenGame
		m.screen = screenScoreTable
		return m, nil
	case "L":
		// Toggle event log overlay.
		// NOTE: lowercase "l" is reserved for vim-right cursor movement, so only
		// the shift variant opens the log from the game screen.
		m.overlayFrom = screenGame
		m.eventLogOffset = 0
		m.screen = screenEventLog
		return m, nil
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
	switch key {
	case "p", "P":
		m.overlayFrom = m.screen
		m.screen = screenScoreTable
	case "L":
		m.overlayFrom = m.screen
		m.eventLogOffset = 0
		m.screen = screenEventLog
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

// ─── Score table keys ─────────────────────────────────────────────────────────

func (m Model) handleScoreTableKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "p", "P":
		m.screen = m.overlayFrom
	case "L":
		// Switch directly to event log.
		m.eventLogOffset = 0
		m.screen = screenEventLog
	case "ctrl+c", "q":
		return m, tea.Quit
	}
	return m, nil
}

// ─── Event log keys ───────────────────────────────────────────────────────────

func (m Model) handleEventLogKey(key string) (tea.Model, tea.Cmd) {
	visibleLines := m.logVisibleLines()
	total := len(m.eventHistory)
	maxOffset := total - visibleLines
	if maxOffset < 0 {
		maxOffset = 0
	}
	switch key {
	case "esc", "L":
		m.screen = m.overlayFrom
		m.eventLogOffset = 0
	case "p", "P":
		// Switch directly to score table.
		m.screen = screenScoreTable
	case "up", "k":
		if m.eventLogOffset < maxOffset {
			m.eventLogOffset++
		}
	case "down", "j":
		if m.eventLogOffset > 0 {
			m.eventLogOffset--
		}
	case "pgup":
		m.eventLogOffset += visibleLines
		if m.eventLogOffset > maxOffset {
			m.eventLogOffset = maxOffset
		}
	case "pgdown":
		m.eventLogOffset -= visibleLines
		if m.eventLogOffset < 0 {
			m.eventLogOffset = 0
		}
	case "ctrl+c", "q":
		return m, tea.Quit
	}
	return m, nil
}

// logVisibleLines returns the number of log lines that fit in the current terminal.
func (m Model) logVisibleLines() int {
	h := m.height
	if h <= 0 {
		h = 40
	}
	// Reserve 3 lines for title + help bar + 1 breathing room.
	n := h - 3
	if n < 1 {
		n = 1
	}
	return n
}

// ─── Event history helpers ────────────────────────────────────────────────────

// appendEventHistory appends entries to the client-side event history, deduping
// against the current tail and capping at maxClientEventHistory.
func (m *Model) appendEventHistory(entries ...string) {
	for _, e := range entries {
		if e == "" {
			continue
		}
		// Skip if this entry is already the last one (avoids duplicates from
		// repeated state snapshots that include the same events).
		if len(m.eventHistory) > 0 && m.eventHistory[len(m.eventHistory)-1] == e {
			continue
		}
		m.eventHistory = append(m.eventHistory, e)
	}
	// Cap.
	if len(m.eventHistory) > maxClientEventHistory {
		m.eventHistory = m.eventHistory[len(m.eventHistory)-maxClientEventHistory:]
	}
}

// mergeEventLogTail merges the server's tail into our local history without
// duplicating entries we already have. Used on (re)connect to seed history with
// context from before this session started.
//
// Strategy: find the longest suffix of `tail` that is also a suffix of our
// history. Everything in `tail` before that overlap is genuinely new and must
// be prepended.
func (m *Model) mergeEventLogTail(tail []string) {
	if len(tail) == 0 {
		return
	}
	if len(m.eventHistory) == 0 {
		// History empty — seed with the whole tail.
		m.eventHistory = append([]string{}, tail...)
		return
	}
	// Find the largest k such that tail[len(tail)-k:] == m.eventHistory suffix of length k.
	// We look for the first i where tail[i:] is a suffix of m.eventHistory.
	for i := 0; i <= len(tail); i++ {
		candidate := tail[i:]
		if len(candidate) == 0 {
			// The whole tail is new (no overlap) — prepend it.
			combined := make([]string, 0, len(tail)+len(m.eventHistory))
			combined = append(combined, tail...)
			combined = append(combined, m.eventHistory...)
			m.eventHistory = combined
			if len(m.eventHistory) > maxClientEventHistory {
				m.eventHistory = m.eventHistory[len(m.eventHistory)-maxClientEventHistory:]
			}
			return
		}
		if m.hasSuffix(candidate) {
			// tail[:i] are the new entries to prepend.
			if i == 0 {
				// Nothing new.
				return
			}
			newEntries := tail[:i]
			combined := make([]string, 0, len(newEntries)+len(m.eventHistory))
			combined = append(combined, newEntries...)
			combined = append(combined, m.eventHistory...)
			m.eventHistory = combined
			if len(m.eventHistory) > maxClientEventHistory {
				m.eventHistory = m.eventHistory[len(m.eventHistory)-maxClientEventHistory:]
			}
			return
		}
	}
}

// hasSuffix returns true if candidate is a suffix of m.eventHistory.
func (m *Model) hasSuffix(candidate []string) bool {
	h := m.eventHistory
	if len(candidate) > len(h) {
		return false
	}
	offset := len(h) - len(candidate)
	for i, e := range candidate {
		if h[offset+i] != e {
			return false
		}
	}
	return true
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	switch m.screen {
	case screenMenu:
		return m.viewMenu()
	case screenFatalError:
		return m.viewFatalError()
	case screenName:
		return m.viewNameEntry()
	case screenLobby:
		return m.viewLobby()
	case screenSeats:
		return m.viewSeats()
	case screenGame:
		return m.viewGame()
	case screenRound:
		return m.viewRoundSummary()
	case screenOver:
		return m.viewGameOver()
	case screenScoreTable:
		return m.viewScoreTable()
	case screenEventLog:
		return m.viewEventLog()
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

var (
	stylePublicAddr = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("46")). // bright green
		Padding(0, 1)

	// styleShareBox is a left-aligned box with no centering, optimized for
	// select-and-copy. Monospace rendering is ensured by no padding tricks.
	styleShareBox = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("46")).
			Padding(0, 1)
)

// repoURL mirrors the constant in main.go — kept here to avoid a circular
// import; the client package has no access to main.
const repoURL = "https://github.com/zwenger/TUI-LOBA"

func (m Model) viewLobby() string {
	var b strings.Builder
	b.WriteString(header())
	b.WriteString("\n")

	// Public address banner + copy-ready share block.
	if m.publicAddr != "" {
		banner := stylePublicAddr.Render(
			"DIRECCIÓN DE LA SALA: " + m.publicAddr,
		)
		b.WriteString(banner + "\n\n")

		// Share block rendered inside a left-aligned bordered box.
		// One command per line — no centering — optimized for select+copy.
		addr := m.publicAddr
		lines := []string{
			"Pasale esto a tus amigos:",
			"",
			"Linux / macOS / Windows (Git Bash):",
			fmt.Sprintf("  git clone %s && cd TUI-LOBA && ./play.sh join %s --name TU_NOMBRE", repoURL, addr),
			fmt.Sprintf("  (ya clonado: cd TUI-LOBA && ./play.sh join %s --name TU_NOMBRE)", addr),
			"",
			"Windows (PowerShell):",
			fmt.Sprintf("  git clone %s; cd TUI-LOBA; .\\play.ps1 join %s --name TU_NOMBRE", repoURL, addr),
			fmt.Sprintf("  (ya clonado: cd TUI-LOBA; .\\play.ps1 join %s --name TU_NOMBRE)", addr),
		}
		b.WriteString(styleShareBox.Render(strings.Join(lines, "\n")) + "\n\n")
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

// ─── Seat picker view ─────────────────────────────────────────────────────────

func (m Model) viewSeats() string {
	var b strings.Builder
	b.WriteString(header())
	b.WriteString("\n")

	b.WriteString(styleTitle.Render("Elegí tu lugar para volver a la partida") + "\n\n")

	if len(m.seats) == 0 {
		b.WriteString(styleErr.Render("No hay lugares disponibles.") + "\n")
	} else {
		for i, seat := range m.seats {
			line := fmt.Sprintf("  %-20s  cartas: %d   puntaje: %d",
				seat.Name, seat.CardCount, seat.Score)
			if i == m.seatCursor {
				line = styleSel.Render("▶ " + strings.TrimLeft(line, " "))
			}
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\n" + styleHelp.Render("↑ ↓ / k j: mover   Enter: elegir   Esc/Q: salir"))

	if m.lastError != "" {
		b.WriteString("\n" + styleErr.Render(m.lastError))
	}
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

	// ── Event log (recent lines) ──
	// Show up to 3 most recent lines when the terminal is tall enough; otherwise 1.
	// We need at least ~4 extra lines above the help bar to show 3 (title=1, hand≈6,
	// melds≈3, piles=4, opponents≈5 → ~19 minimum; 3 extra log lines require h≥22).
	maxLogLines := 1
	if m.height >= 26 {
		maxLogLines = 3
	} else if m.height >= 24 {
		maxLogLines = 2
	}
	evts := m.events
	if len(evts) > maxLogLines {
		evts = evts[len(evts)-maxLogLines:]
	}
	for _, ev := range evts {
		b.WriteString(styleDim.Render("► "+ev) + "\n")
	}
	if m.lastError != "" {
		b.WriteString(styleErr.Render("✗ "+m.lastError) + "\n")
	}

	// ── Help bar ──
	isMyTurn := s.ActiveID == m.selfID
	b.WriteString(m.renderHelp(isMyTurn))

	return b.String()
}

// truncateName shortens a name to maxLen visible characters, appending "…".
func truncateName(name string, maxLen int) string {
	runes := []rune(name)
	if len(runes) <= maxLen {
		return name
	}
	return string(runes[:maxLen-1]) + "…"
}

// renderPlayerBadge renders a multi-line bordered badge for any player, including self.
// Height is always 3 content lines (name/turn, cards, score+conn) so that
// lipgloss.JoinHorizontal aligns all badges in a row at the same baseline.
// pos is the 1-based turn index shown as a position badge prefix.
// Self is visually distinct: double teal border + "(vos)" suffix. Active-turn
// styling (thick yellow border + ▶ ◀) applies to whoever is active, self included.
func renderPlayerBadge(p protocol.PlayerView, pos int) string {
	name := truncateName(p.Name, 12)

	var posLabel string
	if pos > 0 {
		posLabel = fmt.Sprintf("%d· ", pos)
	}

	// Build name line. Self gets "(vos)" appended; active gets arrow markers.
	selfSuffix := ""
	if p.IsSelf {
		selfSuffix = " (vos)"
	}

	var nameLine string
	if p.IsActive {
		nameLine = styleBadgeNameActive.Render("▶ "+posLabel+name+selfSuffix) + styleActive.Render(" ◀")
	} else if p.IsSelf {
		nameLine = styleBadgeNameSelf.Render("  " + posLabel + name + selfSuffix)
	} else {
		nameLine = styleBadgeName.Render("  " + posLabel + name)
	}

	cardsLine := fmt.Sprintf("  ♦ %d cartas", p.CardCount)

	var connStr string
	if !p.Connected {
		connStr = styleBadgeDisconnStr.Render(" desconectado")
	}
	scoreLine := fmt.Sprintf("  %d pts%s", p.TotalScore, connStr)

	content := strings.Join([]string{nameLine, cardsLine, scoreLine}, "\n")

	// Border precedence: active (thick yellow) > disconnected (dim) > self (double teal) > normal.
	if p.IsActive {
		return styleBadgeActive.Render(content)
	}
	if !p.Connected {
		return styleBadgeDisconn.Render(styleDim.Render(content))
	}
	if p.IsSelf {
		return styleBadgeSelf.Render(content)
	}
	return styleBadgeNormal.Render(content)
}

// renderOpponentBadge is a compatibility shim — delegates to renderPlayerBadge.
// Kept so that existing callers (score reveal, tests) continue to compile.
func renderOpponentBadge(p protocol.PlayerView, pos int) string {
	return renderPlayerBadge(p, pos)
}

// renderPlayerChip renders a compact single-line chip for any player (self included)
// at very narrow terminals. pos is the 1-based turn index shown as a prefix.
// Self gets a "*" marker appended so it stands out without ANSI borders.
func renderPlayerChip(p protocol.PlayerView, pos int) string {
	name := truncateName(p.Name, 10)
	posLabel := ""
	if pos > 0 {
		posLabel = fmt.Sprintf("%d·", pos)
	}
	turnMark := ""
	if p.IsActive {
		turnMark = styleActive.Render("▶")
	}
	selfMark := ""
	if p.IsSelf {
		selfMark = styleBadgeNameSelf.Render("*")
	}
	connMark := ""
	if !p.Connected {
		connMark = styleBadgeDisconnStr.Render("✗")
	}
	chip := fmt.Sprintf("%s%s%s%s ♦%d ▸%dpts%s", turnMark, posLabel, name, selfMark, p.CardCount, p.TotalScore, connMark)
	return chip
}

// renderOpponentChip is a compatibility shim — delegates to renderPlayerChip.
func renderOpponentChip(p protocol.PlayerView, pos int) string {
	return renderPlayerChip(p, pos)
}

// wrapBadges lays out a slice of pre-rendered badge strings into rows using
// lipgloss.JoinHorizontal, wrapping whole badges when the next one would exceed
// termWidth. The gap between badges in a row is badgeGap spaces.
// This mirrors the same wrapping pattern used by renderMelds.
func wrapBadges(badges []string, termWidth, badgeGap int) string {
	if len(badges) == 0 {
		return ""
	}
	gap := strings.Repeat(" ", badgeGap)
	var rows []string
	for _, badge := range badges {
		if len(rows) == 0 {
			rows = append(rows, badge)
			continue
		}
		lastRow := rows[len(rows)-1]
		candidate := lipgloss.JoinHorizontal(lipgloss.Top, lastRow, gap, badge)
		if lipgloss.Width(candidate) <= termWidth {
			rows[len(rows)-1] = candidate
		} else {
			rows = append(rows, badge)
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// compactChipWidth is the minimum terminal width below which we switch to chips.
const compactChipWidth = 60

// rotationOrderedOpponents returns the opponents slice sorted in turn-rotation
// order starting from the player after self. Each player's TurnIndex is used;
// if TurnIndex is zero (old server / snapshot without the field) the slice
// order from the snapshot is used unchanged.
// Kept for any callers that still need opponents-only ordering.
func rotationOrderedOpponents(players []protocol.PlayerView) []protocol.PlayerView {
	var self protocol.PlayerView
	selfFound := false
	for _, p := range players {
		if p.IsSelf {
			self = p
			selfFound = true
			break
		}
	}

	var opponents []protocol.PlayerView
	for _, p := range players {
		if !p.IsSelf {
			opponents = append(opponents, p)
		}
	}

	// Only reorder when TurnIndex is populated (non-zero) and self is known.
	if !selfFound || self.TurnIndex == 0 {
		return opponents
	}

	n := len(players) // total number of players
	// Sort opponents by their position in the rotation starting just after self.
	// Distance from self = (p.TurnIndex - self.TurnIndex + n) % n
	sort.SliceStable(opponents, func(a, b int) bool {
		da := (opponents[a].TurnIndex - self.TurnIndex + n) % n
		db := (opponents[b].TurnIndex - self.TurnIndex + n) % n
		return da < db
	})
	return opponents
}

// absoluteOrderedPlayers returns all players (self included) sorted by
// TurnIndex ascending (1, 2, 3, … N). This order is identical for every
// client, making the turn sequence immediately obvious.
// If TurnIndex is zero for any player (old server snapshot), the original
// snapshot order is used unchanged.
func absoluteOrderedPlayers(players []protocol.PlayerView) []protocol.PlayerView {
	if len(players) == 0 {
		return nil
	}
	// Check whether TurnIndex is populated.
	hasIndex := false
	for _, p := range players {
		if p.TurnIndex > 0 {
			hasIndex = true
			break
		}
	}
	result := make([]protocol.PlayerView, len(players))
	copy(result, players)
	if !hasIndex {
		return result
	}
	sort.SliceStable(result, func(a, b int) bool {
		return result[a].TurnIndex < result[b].TurnIndex
	})
	return result
}

func (m Model) renderOpponents(s *protocol.StateSnapshot) string {
	// Show ALL players (self included) in absolute turn order.
	players := absoluteOrderedPlayers(s.Players)
	if len(players) == 0 {
		return styleDim.Render("Sin jugadores") + "\n"
	}

	termWidth := m.width
	if termWidth <= 0 {
		termWidth = 120
	}

	if termWidth < compactChipWidth {
		// Compact chip mode: single-line chips separated by " · ", wrapped as whole units.
		var chips []string
		for _, p := range players {
			chips = append(chips, renderPlayerChip(p, p.TurnIndex))
		}
		sep := styleBadgeChipSep.Render(" · ")
		// Wrap chips into rows keeping them as whole units.
		var chipRows []string
		currentRow := ""
		for i, chip := range chips {
			if i == 0 {
				currentRow = chip
				continue
			}
			candidate := currentRow + sep + chip
			if lipgloss.Width(candidate) <= termWidth {
				currentRow = candidate
			} else {
				chipRows = append(chipRows, currentRow)
				currentRow = chip
			}
		}
		chipRows = append(chipRows, currentRow)
		return strings.Join(chipRows, "\n") + "\n"
	}

	// Full badge mode.
	badges := make([]string, len(players))
	for i, p := range players {
		badges[i] = renderPlayerBadge(p, p.TurnIndex)
	}
	return wrapBadges(badges, termWidth, 1) + "\n"
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
	selfTurnIdx := 0
	for _, p := range s.Players {
		if p.IsSelf {
			selfName = p.Name
			selfScore = fmt.Sprintf("%d", p.TotalScore)
			selfTurnIdx = p.TurnIndex
			break
		}
	}
	isMyTurn := s.ActiveID == m.selfID
	turnMark := ""
	if isMyTurn {
		turnMark = styleActive.Render(" ◄ TU TURNO")
	}
	posLabel := ""
	if selfTurnIdx > 0 {
		posLabel = fmt.Sprintf(" (%d·)", selfTurnIdx)
	}
	handHeader := fmt.Sprintf("Tu mano — %s%s%s  (puntaje: %s)", selfName, posLabel, turnMark, selfScore)

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
	overlayHints := styleDim.Render("  P:puntajes  Shift+L:log")
	pickedNote := ""
	if m.state != nil && m.state.PickedUpDiscard != nil {
		pickedNote = stylePickedUp.Render("  [★ debés jugar " + m.state.PickedUpDiscard.Label + " antes de descartar]")
	}

	// "siguiente" hint: show who plays after the current active player.
	nextHint := ""
	if m.state != nil && m.state.NextID != "" {
		nextName := ""
		for _, p := range m.state.Players {
			if p.ID == m.state.NextID {
				if p.TurnIndex > 0 {
					nextName = fmt.Sprintf("%d·%s", p.TurnIndex, truncateName(p.Name, 10))
				} else {
					nextName = truncateName(p.Name, 10)
				}
				break
			}
		}
		if nextName != "" {
			nextHint = styleDim.Render("  siguiente: " + nextName)
		}
	}

	if !isMyTurn {
		return styleHelp.Render("← →/h l: mover cursor  Espacio: seleccionar  S: ordenar  (esperando)") + " " + sortLabel + overlayHints + nextHint + pickedNote + "\n"
	}
	phase := ""
	if m.state != nil {
		phase = m.state.Phase
	}
	switch phase {
	case "draw":
		return styleHelp.Render("D: robar del mazo  T: tomar del pozo  S: ordenar") + " " + sortLabel + overlayHints + nextHint + "\n"
	default:
		return styleHelp.Render("Espacio: seleccionar  M: pierna  E: escalera  1-9: agregar en comb.#N  X: descartar  S: ordenar  Esc: limpiar") + " " + sortLabel + overlayHints + nextHint + pickedNote + "\n"
	}
}

// ─── Round summary view ───────────────────────────────────────────────────────

// renderRevealBlock renders one player's round-end reveal as a bordered block:
// name row, mini-card strip, score breakdown line.
func renderRevealBlock(rph protocol.RevealedPlayerHand, termWidth int) string {
	// Name / winner marker.
	nameStr := rph.PlayerName
	if rph.IsWinner {
		winnerLabel := "ganó la mano"
		if rph.WentOutInOnePlay {
			winnerLabel = "ganó la mano — ¡de mano! −10 pts"
		}
		nameStr = styleActive.Render("★ " + nameStr + " — " + winnerLabel)
	} else {
		nameStr = styleBadgeName.Render(nameStr)
	}

	// Card strip with per-card value annotation.
	var cardBlocks []string
	var valueLabels []string
	for _, cv := range rph.Cards {
		c := cv
		cardBlocks = append(cardBlocks, renderMiniCard(&c))
		val := cardPenaltyValue(cv)
		valueLabels = append(valueLabels, styleDim.Render(fmt.Sprintf("%2d", val)))
	}

	var cardStrip string
	if len(cardBlocks) == 0 {
		cardStrip = styleDim.Render("  (mano vacía)")
	} else {
		cardStrip = lipgloss.JoinHorizontal(lipgloss.Top, cardBlocks...)
	}

	// Score breakdown: "K♠+Q♦+5♣ = 10+10+5 = 25" style.
	scoreBreakdown := buildScoreBreakdown(rph.Cards, rph.RoundScore)

	content := lipgloss.JoinVertical(lipgloss.Left,
		nameStr,
		cardStrip,
		styleDim.Render(scoreBreakdown),
	)

	// Use winner-highlighted or normal border.
	if rph.IsWinner {
		return styleBadgeActive.Render(content)
	}
	return styleBadgeNormal.Render(content)
}

// cardPenaltyValue returns the penalty value for a CardView (mirrors game.Rank.Score).
func cardPenaltyValue(cv protocol.CardView) int {
	switch {
	case cv.Rank == 0: // joker
		return 25
	case cv.Rank == 1: // ace
		return 15
	case cv.Rank >= 11: // J, Q, K
		return 10
	default:
		return cv.Rank
	}
}

// buildScoreBreakdown builds a human-readable score formula, e.g.
// "K♠+Q♦+5♣ = 10+10+5 = 25" or "+0 pts esta mano" for the round winner,
// or "−10 pts esta mano (de mano)" for the "cerrar de mano" bonus.
func buildScoreBreakdown(cards []protocol.CardView, total int) string {
	if len(cards) == 0 {
		if total < 0 {
			return fmt.Sprintf("%d pts esta mano (de mano)", total)
		}
		return "+0 pts esta mano"
	}
	var labels []string
	var values []string
	for _, cv := range cards {
		labels = append(labels, cardLabelFromView(cv))
		values = append(values, fmt.Sprintf("%d", cardPenaltyValue(cv)))
	}
	cardPart := strings.Join(labels, "+")
	valPart := strings.Join(values, "+")
	if len(cards) == 1 {
		return fmt.Sprintf("%s = %d pts esta mano", cardPart, total)
	}
	return fmt.Sprintf("%s = %s = %d pts esta mano", cardPart, valPart, total)
}

func (m Model) viewRoundSummary() string {
	var b strings.Builder
	b.WriteString(header())

	round := 0
	if m.state != nil {
		round = m.state.Round
	}
	b.WriteString(styleTitle.Render(fmt.Sprintf("Ronda %d — Fin", round)) + "\n\n")

	if m.state != nil && len(m.state.RoundReveal) > 0 {
		b.WriteString(m.renderRevealSection(m.state.RoundReveal))
	} else if m.state != nil {
		// Fallback: no reveal data (shouldn't happen in normal flow).
		for _, p := range m.state.Players {
			line := fmt.Sprintf("  %-20s  esta ronda: +%d   total: %d",
				p.Name, p.RoundScore, p.TotalScore)
			if p.TotalScore > 101 {
				line += styleErr.Render("  ELIMINADO")
			}
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\n" + styleHelp.Render("Enter / N: siguiente ronda  ·  P: puntajes  ·  L: log  ·  Q: salir"))
	return b.String()
}

// renderRevealSection renders all player reveal blocks wrapped to terminal width.
func (m Model) renderRevealSection(reveal []protocol.RevealedPlayerHand) string {
	termWidth := m.width
	if termWidth <= 0 {
		termWidth = 100
	}

	blocks := make([]string, len(reveal))
	for i, rph := range reveal {
		blocks[i] = renderRevealBlock(rph, termWidth)
	}

	// Wrap blocks like badges / melds: fill rows left-to-right.
	return wrapBadges(blocks, termWidth, 1) + "\n"
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

	// Show the last-round reveal (same component as round summary).
	if m.state != nil && len(m.state.RoundReveal) > 0 {
		b.WriteString(styleTitle.Render("Última mano") + "\n")
		b.WriteString(m.renderRevealSection(m.state.RoundReveal))
		b.WriteString("\n")
	}

	// Final standings sorted by total score (lowest first, winner highlighted).
	if m.state != nil && len(m.state.Players) > 0 {
		b.WriteString(styleTitle.Render("Clasificación final") + "\n")

		// Sort a copy by TotalScore ascending.
		sorted := make([]protocol.PlayerView, len(m.state.Players))
		copy(sorted, m.state.Players)
		for i := 0; i < len(sorted); i++ {
			for j := i + 1; j < len(sorted); j++ {
				if sorted[j].TotalScore < sorted[i].TotalScore {
					sorted[i], sorted[j] = sorted[j], sorted[i]
				}
			}
		}
		for pos, p := range sorted {
			line := fmt.Sprintf("  %d. %-20s  total: %d", pos+1, p.Name, p.TotalScore)
			if p.ID == m.state.WinnerID {
				line = styleActive.Render(line + "  ← GANADOR")
			}
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\n" + styleHelp.Render("Q: salir"))
	return b.String()
}

// ─── Score table view ─────────────────────────────────────────────────────────

// viewScoreTable renders the full-screen per-round score table.
// Rows = rounds ("Ronda 1", "Ronda 2", …), columns = players.
// A bold "Total" row at the bottom matches the accumulated totals.
// The player with the lowest total so far is highlighted (winner-so-far).
func (m Model) viewScoreTable() string {
	var b strings.Builder
	b.WriteString(header())
	b.WriteString(styleTitle.Render("Puntajes por ronda") + "\n\n")

	if m.state == nil || len(m.state.ScoreHistory) == 0 {
		b.WriteString(styleDim.Render("  (todavía no se completó ninguna ronda)") + "\n")
		b.WriteString("\n" + styleHelp.Render("P / Esc: volver"))
		return b.String()
	}

	history := m.state.ScoreHistory
	players := m.state.Players

	// Build ordered player ID list from the Players slice (preserves join order).
	playerIDs := make([]string, 0, len(players))
	playerNames := make([]string, 0, len(players))
	for _, p := range players {
		playerIDs = append(playerIDs, p.ID)
		playerNames = append(playerNames, p.Name)
	}

	termWidth := m.width
	if termWidth <= 0 {
		termWidth = 100
	}

	// Compute per-column widths. Round label column is 8 chars wide.
	// Player name columns: truncate to fit. Max 6 players; we try 10 chars first,
	// then 8, then 6 if columns don't fit.
	roundColW := 8
	nameMaxLen := 10
	// Each player col = max(nameMaxLen, 5) + 2 padding on each side + separator
	playerColW := nameMaxLen + 2 // "  " + name + "  "
	totalCols := roundColW + len(playerIDs)*(playerColW+1) // +1 for │ separator
	if totalCols > termWidth {
		nameMaxLen = 8
		playerColW = nameMaxLen + 2
		totalCols = roundColW + len(playerIDs)*(playerColW+1)
	}
	if totalCols > termWidth {
		nameMaxLen = 6
		playerColW = nameMaxLen + 2
	}

	// Separator line.
	sep := strings.Repeat("─", roundColW)
	for range playerIDs {
		sep += "┼" + strings.Repeat("─", playerColW)
	}

	// Header row.
	row := fmt.Sprintf("%-*s", roundColW, "Ronda")
	for i, name := range playerNames {
		_ = i
		col := truncateName(name, nameMaxLen)
		row += "│" + centerPad(col, playerColW)
	}
	b.WriteString(styleBadgeName.Render(row) + "\n")
	b.WriteString(styleDim.Render(sep) + "\n")

	// Accumulate totals as we render rows (for consistency with displayed values).
	totals := make(map[string]int, len(playerIDs))

	for _, rs := range history {
		row = fmt.Sprintf("%-*s", roundColW, fmt.Sprintf("Ronda %d", rs.Round))
		for _, id := range playerIDs {
			pts := rs.Scores[id]
			totals[id] += pts
			cell := fmt.Sprintf("%d", pts)
			row += "│" + centerPad(cell, playerColW)
		}
		b.WriteString(row + "\n")
	}

	// Separator before Total row.
	totalSep := strings.Repeat("═", roundColW)
	for range playerIDs {
		totalSep += "╪" + strings.Repeat("═", playerColW)
	}
	b.WriteString(styleDim.Render(totalSep) + "\n")

	// Find winner-so-far (lowest total).
	winnerID := ""
	winnerTotal := -1
	for _, id := range playerIDs {
		t := totals[id]
		if winnerID == "" || t < winnerTotal {
			winnerID = id
			winnerTotal = t
		}
	}

	// Total row.
	totalRow := fmt.Sprintf("%-*s", roundColW, "Total")
	for _, id := range playerIDs {
		cell := fmt.Sprintf("%d", totals[id])
		colStr := centerPad(cell, playerColW)
		if id == winnerID {
			totalRow += "│" + styleActive.Render(colStr)
		} else {
			totalRow += "│" + colStr
		}
	}
	b.WriteString(styleBadgeName.Render(totalRow) + "\n")

	b.WriteString("\n" + styleHelp.Render("P / Esc: volver  ·  L: historial"))
	return b.String()
}

// centerPad pads s to width w, centering it with spaces.
func centerPad(s string, w int) string {
	runes := []rune(s)
	n := len(runes)
	if n >= w {
		return string(runes[:w])
	}
	left := (w - n) / 2
	right := w - n - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

// ─── Event log view ───────────────────────────────────────────────────────────

// viewEventLog renders the full-screen scrollable event log.
// Newest entries are at the bottom. ↑/k scrolls toward older; ↓/j toward newer.
func (m Model) viewEventLog() string {
	var b strings.Builder
	b.WriteString(header())
	b.WriteString(styleTitle.Render("Historial de la partida") + "\n\n")

	visibleLines := m.logVisibleLines()
	total := len(m.eventHistory)

	if total == 0 {
		b.WriteString(styleDim.Render("  (sin eventos todavía)") + "\n")
	} else {
		// offset=0 → show newest (bottom), offset>0 → scroll up toward older.
		// The slice to show: indices [start, end) in m.eventHistory.
		end := total - m.eventLogOffset
		start := end - visibleLines
		if start < 0 {
			start = 0
		}
		if end > total {
			end = total
		}
		for _, ev := range m.eventHistory[start:end] {
			b.WriteString(styleDim.Render("  " + ev) + "\n")
		}
		// Scroll indicator.
		if total > visibleLines {
			shown := end - start
			indicator := fmt.Sprintf("  (%d–%d de %d)", start+1, start+shown, total)
			b.WriteString(styleDim.Render(indicator) + "\n")
		}
	}

	b.WriteString("\n" + styleHelp.Render("↑/k: arriba  ↓/j: abajo  PgUp/PgDn: página  L / Esc: volver  P: puntajes"))
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
	switch suit {
	case 1: // Hearts
		return styleHearts.Render(s)
	case 2: // Diamonds
		return styleDiamonds.Render(s)
	case 3: // Clubs
		return styleClubs.Render(s)
	default: // Spades and face-down/neutral
		return styleSpades.Render(s)
	}
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

func connectCmd(addr string) tea.Cmd {
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
