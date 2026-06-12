// Package server implements the Loba TCP game server.
// It is server-authoritative: all game logic runs here; clients are
// dumb renderers that send commands and receive state snapshots.
package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"github.com/zwenger/TUI-LOBA/internal/game"
	"github.com/zwenger/TUI-LOBA/internal/protocol"
	"log"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"
)

// Server manages one Loba game session.
type Server struct {
	mu       sync.Mutex
	game     *game.Game
	clients  map[string]*client  // keyed by player ID
	pending  map[string]*pending // keyed by pendingID; clients picking a seat
	hostID   string
	lobby    []lobbyEntry // ordered player join list
	started  bool
	port     string
	hostName string
	version      string   // server binary version, set via New
	publicAddr   string   // bore.pub public address, empty when --public is not used
	pendingEvents []string // events to flush into the game once it starts
}

// pending represents a connection that has joined a started game and is in the
// process of picking a disconnected seat. It is not yet attached to any player.
type pending struct {
	id   string   // unique pending connection ID (not a player ID)
	conn net.Conn
	send chan []byte
	done chan struct{} // closed by writePump when it exits
}

type lobbyEntry struct {
	id        string
	name      string
	connected bool // false when the lobby member disconnected before game start
}

type client struct {
	id        string
	name      string
	conn      net.Conn
	send      chan []byte // buffered outbound channel
	connected bool       // false while the underlying connection is gone
}

// New creates a new server that will listen on the given port.
func New(port, hostName string) *Server {
	return &Server{
		clients:  make(map[string]*client),
		pending:  make(map[string]*pending),
		port:     port,
		hostName: hostName,
	}
}

// SetVersion records the server's own binary version so it can compare with
// joining clients. Call this once from main before starting to serve.
func (s *Server) SetVersion(v string) {
	s.mu.Lock()
	s.version = v
	s.mu.Unlock()
}

// SetPublicAddr stores the bore.pub public address so it is included in lobby
// state broadcasts. Call this after the tunnel is up, before or after the
// first client connects — the next broadcastLobby will pick it up.
func (s *Server) SetPublicAddr(addr string) {
	s.mu.Lock()
	s.publicAddr = addr
	s.mu.Unlock()
	s.broadcastLobby()
}

// ListenAndServe starts the TCP listener and blocks until the process exits.
func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf(":%s", s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("server listen: %w", err)
	}
	log.Printf("[server] Listening on %s", addr)
	return s.Serve(ln)
}

// enableKeepalive activates TCP keepalives on conn if it is a *net.TCPConn.
// This ensures that silently dead connections (process kill, network cut) are
// detected by the OS within ~15 s rather than waiting for the default system
// timeout (which can be hours). Non-TCP connections (e.g. bore tunnel pipe
// connections) are skipped safely.
func enableKeepalive(conn net.Conn) {
	tc, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}
	_ = tc.SetKeepAlive(true)
	_ = tc.SetKeepAlivePeriod(15 * time.Second)
}

// Serve accepts connections from an already-open net.Listener and blocks until
// the listener is closed or the process exits. This allows callers (e.g. a
// tunnel accept loop) to supply their own listener — including one returned by
// the bore.pub SDK — without changing any other server logic.
func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[server] Accept error: %v", err)
			// A permanent error (listener closed) stops the loop.
			return err
		}
		go s.handleConn(conn)
	}
}

// ─── Connection lifecycle ─────────────────────────────────────────────────────

// HandleConn processes a single inbound connection from any source (local TCP
// listener or bore.pub tunnel listener). It is exported so the tunnel accept loop
// in main.go can forward tunnel connections without coupling to server internals.
func (s *Server) HandleConn(conn net.Conn) {
	s.handleConn(conn)
}

func (s *Server) handleConn(conn net.Conn) {
	enableKeepalive(conn)
	defer conn.Close()
	r := bufio.NewReader(conn)

	// First message must be a join command.
	var cmd protocol.Command
	if err := protocol.ReadJSON(r, &cmd); err != nil || cmd.Type != protocol.CmdJoin {
		_ = protocol.SendError(conn, "el primer mensaje debe ser un comando de unión")
		return
	}

	// Check if this join targets a started game — if so, route to the seat-picker flow.
	s.mu.Lock()
	isStarted := s.started
	s.mu.Unlock()

	if isStarted {
		s.handleConnStartedGame(conn, r, cmd.Name)
		return
	}

	// Lobby join flow.
	// If the client sent an empty name, prompt for one before registering.
	name := cmd.Name
	for name == "" {
		_ = protocol.SendEnvelope(conn, protocol.EvtNameRequired, map[string]string{"message": "Ingresá tu nombre para unirte."})
		var nameCmd protocol.Command
		if err := protocol.ReadJSON(r, &nameCmd); err != nil {
			return // client disconnected
		}
		if nameCmd.Type != protocol.CmdJoin {
			continue // ignore non-join messages while waiting for a name
		}
		name = strings.TrimSpace(nameCmd.Name)
	}

	playerID, _, err := s.registerPlayer(conn, name)
	if err != nil {
		_ = protocol.SendError(conn, err.Error())
		return
	}

	log.Printf("[server] %s joined as %s", conn.RemoteAddr(), name)

	// Version mismatch warning: if both are non-"dev" and differ, queue a warning event.
	// If a game is already running, add it immediately. Otherwise, enqueue it to be
	// flushed into the game's event log when the game starts.
	s.mu.Lock()
	srvVer := s.version
	s.mu.Unlock()
	clientVer := strings.TrimPrefix(cmd.Version, "v")
	if srvVer != "" && srvVer != "dev" && clientVer != "" && clientVer != "dev" && srvVer != clientVer {
		warnMsg := fmt.Sprintf("atención: %s usa v%s y el anfitrión v%s — conviene que ambos actualicen a la última versión",
			name, clientVer, srvVer)
		s.mu.Lock()
		if s.game != nil {
			s.game.AddEvent(warnMsg)
		} else {
			s.pendingEvents = append(s.pendingEvents, warnMsg)
		}
		s.mu.Unlock()
		log.Printf("[server] version mismatch: client=%s server=%s", clientVer, srvVer)
	}

	// Start the outbound writer goroutine.
	s.mu.Lock()
	cl := s.clients[playerID]
	s.mu.Unlock()
	go cl.writePump()

	// Broadcast updated lobby state for a fresh join.
	s.broadcastLobby()

	// Read loop.
	for {
		var incoming protocol.Command
		if err := protocol.ReadJSON(r, &incoming); err != nil {
			if err != io.EOF {
				log.Printf("[server] Read error from %s: %v", cmd.Name, err)
			}
			s.markDisconnected(playerID)
			return
		}
		s.handleCommand(playerID, incoming)
	}
}

// handleConnStartedGame handles a new connection that arrives after the game
// has already started. It sends the available disconnected seats and waits for
// the client to claim one.
func (s *Server) handleConnStartedGame(conn net.Conn, r *bufio.Reader, _ string) {
	// Build the list of available (disconnected) seats under the lock.
	s.mu.Lock()
	seats := s.availableSeatsLocked()
	s.mu.Unlock()

	if len(seats) == 0 {
		_ = protocol.SendError(conn, "la partida ya comenzó y no hay lugares libres")
		return
	}

	// Register as a pending connection (not yet a player).
	pendingID := fmt.Sprintf("pending-%d", time.Now().UnixNano())
	pnd := &pending{
		id:   pendingID,
		conn: conn,
		send: make(chan []byte, 64),
		done: make(chan struct{}),
	}
	s.mu.Lock()
	s.pending[pendingID] = pnd
	s.mu.Unlock()

	go pnd.writePump()

	// Send the seat list.
	offer := protocol.SeatsOffer{Seats: seats}
	if err := s.enqueueEnvelopePending(pnd, protocol.EvtSeats, offer); err != nil {
		s.removePending(pendingID)
		return
	}

	log.Printf("[server] %s is picking a seat", conn.RemoteAddr())

	// Wait for a claim_seat command (ignore everything else).
	for {
		var incoming protocol.Command
		if err := protocol.ReadJSON(r, &incoming); err != nil {
			log.Printf("[server] Pending client disconnected before claiming a seat")
			s.removePending(pendingID)
			return
		}
		if incoming.Type != protocol.CmdClaimSeat {
			continue
		}

		// Attempt to claim the seat.
		playerID, err := s.claimSeat(conn, pendingID, incoming.SeatID)
		if err != nil {
			// Could be a race (seat taken) or invalid ID — tell the client and
			// send a refreshed seat list so they can pick again.
			_ = s.enqueueEnvelopePending(pnd, protocol.EvtError, map[string]string{"message": err.Error()})

			s.mu.Lock()
			refreshed := s.availableSeatsLocked()
			s.mu.Unlock()

			if len(refreshed) == 0 {
				_ = s.enqueueEnvelopePending(pnd, protocol.EvtError, map[string]string{"message": "la partida ya comenzó y no hay lugares libres"})
				s.removePending(pendingID)
				return
			}
			_ = s.enqueueEnvelopePending(pnd, protocol.EvtSeats, protocol.SeatsOffer{Seats: refreshed})
			continue
		}

		// Claimed successfully: the pending entry is now gone and a real client entry exists.
		log.Printf("[server] %s claimed seat %s", conn.RemoteAddr(), playerID)

		s.mu.Lock()
		if s.game != nil {
			p := s.game.PlayerByID(playerID)
			if p != nil {
				msg := fmt.Sprintf("%s se reconectó.", p.Name)
				s.game.AddEvent(msg)
			}
		}
		s.broadcastStateLocked()
		s.mu.Unlock()

		// Switch the send channel to the real client; hand off to the normal read loop.
		for {
			var incoming protocol.Command
			if err := protocol.ReadJSON(r, &incoming); err != nil {
				if err != io.EOF {
					log.Printf("[server] Read error from %s: %v", playerID, err)
				}
				s.markDisconnected(playerID)
				return
			}
			s.handleCommand(playerID, incoming)
		}
	}
}

// registerPlayer registers a new lobby player. Only called during the lobby phase.
// Returns (playerID, false, error). The bool is kept for API consistency.
func (s *Server) registerPlayer(conn net.Conn, name string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Lobby phase: reject duplicate connected names.
	for _, e := range s.lobby {
		if e.connected && strings.EqualFold(e.name, name) {
			return "", false, fmt.Errorf("ese nombre ya está en uso")
		}
		// Reclaim a disconnected lobby seat with the same name.
		if !e.connected && strings.EqualFold(e.name, name) {
			cl := &client{
				id:        e.id,
				name:      e.name,
				conn:      conn,
				send:      make(chan []byte, 64),
				connected: true,
			}
			s.clients[e.id] = cl
			// Mark the lobby entry as connected again.
			for i := range s.lobby {
				if s.lobby[i].id == e.id {
					s.lobby[i].connected = true
					break
				}
			}
			return e.id, false, nil
		}
	}

	if len(s.lobby) >= 6 {
		return "", false, fmt.Errorf("la sala está llena (máximo 6 jugadores)")
	}
	// name is guaranteed non-empty by the caller (handleConn loops until a name is provided).

	id := fmt.Sprintf("p%d", len(s.lobby)+1)

	cl := &client{
		id:        id,
		name:      name,
		conn:      conn,
		send:      make(chan []byte, 64),
		connected: true,
	}
	s.clients[id] = cl
	s.lobby = append(s.lobby, lobbyEntry{id: id, name: name, connected: true})

	if s.hostID == "" {
		s.hostID = id
		log.Printf("[server] %s is the host", name)
	}

	return id, false, nil
}

// availableSeatsLocked returns the list of disconnected player seats.
// Caller must hold mu.
func (s *Server) availableSeatsLocked() []protocol.SeatEntry {
	if s.game == nil {
		return nil
	}
	var seats []protocol.SeatEntry
	for _, p := range s.game.Players {
		if !p.Connected {
			seats = append(seats, protocol.SeatEntry{
				ID:        p.ID,
				Name:      p.Name,
				CardCount: len(p.Hand),
				Score:     p.TotalScore,
			})
		}
	}
	return seats
}

// claimSeat atomically claims a disconnected seat for the pending connection.
// It removes the pending entry and creates a proper client entry.
// Returns the playerID on success, or an error if the seat is gone.
func (s *Server) claimSeat(conn net.Conn, pendingID, seatID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pnd, ok := s.pending[pendingID]
	if !ok {
		return "", fmt.Errorf("conexión pendiente no encontrada")
	}

	if s.game == nil {
		return "", fmt.Errorf("la partida no está en curso")
	}

	// Re-check under the lock: seat must still be disconnected.
	var target *game.Player
	for _, p := range s.game.Players {
		if p.ID == seatID {
			target = p
			break
		}
	}
	if target == nil {
		return "", fmt.Errorf("lugar no encontrado")
	}
	if target.Connected {
		return "", fmt.Errorf("ese lugar ya fue tomado — elegí otro")
	}

	// Reuse the pending send channel so the writePump already running keeps working.
	newCl := &client{
		id:        target.ID,
		name:      target.Name,
		conn:      conn,
		send:      pnd.send, // hand off the channel — writePump is already running it
		connected: true,
	}
	s.clients[target.ID] = newCl
	target.Connected = true

	delete(s.pending, pendingID)

	return target.ID, nil
}

// removePending closes and removes a pending connection entry.
// It waits for the writePump to drain before returning so that any queued
// error messages are fully written before the caller's deferred conn.Close fires.
func (s *Server) removePending(pendingID string) {
	s.mu.Lock()
	pnd, ok := s.pending[pendingID]
	if ok {
		delete(s.pending, pendingID)
	}
	s.mu.Unlock()

	if ok {
		close(pnd.send)
		<-pnd.done // wait for writePump to flush and exit
	}
}

// enqueueEnvelopePending sends an envelope to a pending (not-yet-claimed) connection.
func (s *Server) enqueueEnvelopePending(pnd *pending, evtType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	env := protocol.Envelope{Type: evtType, Payload: raw}
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	select {
	case pnd.send <- data:
	default:
	}
	return nil
}

func (s *Server) markDisconnected(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := playerID
	if s.game != nil {
		// In-game disconnect: mark the game player disconnected so auto-play kicks
		// in and the seat can be reclaimed via the seat picker.
		p := s.game.PlayerByID(playerID)
		if p != nil {
			p.Connected = false
			name = p.Name
			log.Printf("[server] %s disconnected (in-game)", p.Name)
		}
	} else {
		// Lobby disconnect: remove the player from the lobby immediately so the
		// updated player list is broadcast to everyone still waiting. We also
		// remove the client map entry — there is nothing to reconnect to in the
		// lobby (a rejoin just creates a fresh entry via registerPlayer).
		for i := range s.lobby {
			if s.lobby[i].id == playerID {
				name = s.lobby[i].name
				log.Printf("[server] %s disconnected (lobby)", name)
				// Remove this entry by swapping with the last element.
				s.lobby[i] = s.lobby[len(s.lobby)-1]
				s.lobby = s.lobby[:len(s.lobby)-1]
				break
			}
		}

		// Promote a new host if the disconnecting player was the host.
		if playerID == s.hostID {
			s.promoteNextHostLocked()
		}

		// Clean up the client map entry entirely — no reconnection needed.
		if cl, ok := s.clients[playerID]; ok {
			cl.connected = false
			close(cl.send)
			delete(s.clients, playerID)
		}

		// Broadcast the updated lobby to remaining players.
		s.broadcastLobbyLocked()
		return
	}

	if cl, ok := s.clients[playerID]; ok {
		// Drain and close the send channel. Keep the map entry so a rejoin can
		// replace it, but mark it disconnected.
		cl.connected = false
		close(cl.send)
		// Replace with a stub so enqueueEnvelope silently drops messages while
		// disconnected. We keep the map entry; handleRejoin will replace it.
		s.clients[playerID] = &client{
			id:        playerID,
			name:      name,
			conn:      nil,
			send:      make(chan []byte, 1), // tiny channel, never drained
			connected: false,
		}
	}

	s.broadcastStateLocked()
	// Schedule auto-play if the disconnected player is now the active one.
	s.scheduleAutoPlayLocked()
}

// promoteNextHostLocked picks the first remaining connected lobby member as the
// new host. If the lobby is now empty the server simply waits for new joins —
// the next player to join will become host via registerPlayer.
// Caller must hold mu.
func (s *Server) promoteNextHostLocked() {
	s.hostID = ""
	for _, e := range s.lobby {
		if e.connected {
			s.hostID = e.id
			log.Printf("[server] Host disconnected; %s promoted to host", e.name)
			return
		}
	}
	// Lobby is now empty (or all disconnected). The next join will set hostID.
	log.Printf("[server] Host disconnected; lobby is empty, waiting for new joins")
}

// scheduleAutoPlayLocked checks whether the current active player is disconnected
// and, if so, launches a goroutine that will auto-play their turn after a short
// delay. It must be called with mu held so it can safely read game state; the
// goroutine itself re-acquires mu before acting.
func (s *Server) scheduleAutoPlayLocked() {
	if s.game == nil {
		return
	}
	g := s.game
	if g.Phase == game.PhaseRoundEnd || g.Phase == game.PhaseGameOver {
		return
	}
	active := g.Players[g.ActiveIndex]
	if active.Connected {
		return
	}
	// Snapshot the player ID and the current active index so the goroutine can
	// verify nothing changed while it was sleeping.
	playerID := active.ID
	activeIdx := g.ActiveIndex
	go s.runAutoPlay(playerID, activeIdx)
}

func (s *Server) runAutoPlay(playerID string, activeIdx int) {
	time.Sleep(1 * time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.game == nil {
		return
	}
	g := s.game
	if g.Phase == game.PhaseRoundEnd || g.Phase == game.PhaseGameOver {
		return
	}
	// Guard: the active player must still be the same disconnected player.
	if g.ActiveIndex != activeIdx {
		return
	}
	active := g.Players[g.ActiveIndex]
	if active.ID != playerID || active.Connected {
		return
	}
	if err := g.AutoPlayDisconnected(); err != nil {
		log.Printf("[server] auto-play error for %s: %v", playerID, err)
		return
	}
	s.broadcastStateLocked()
	// Chain: if the next player is also disconnected, schedule again.
	s.scheduleAutoPlayLocked()
}

// ─── Command dispatch ─────────────────────────────────────────────────────────

func (s *Server) handleCommand(playerID string, cmd protocol.Command) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch cmd.Type {
	case protocol.CmdStart:
		s.handleStart(playerID)
	case protocol.CmdDrawStock:
		s.handleAction(playerID, func() error { return s.game.DrawStock(playerID) })
	case protocol.CmdDrawDiscard:
		s.handleAction(playerID, func() error { return s.game.DrawDiscard(playerID) })
	case protocol.CmdMeld:
		s.handleAction(playerID, func() error {
			mt := game.MeldPierna
			if cmd.MeldType == "escalera" {
				mt = game.MeldEscalera
			}
			return s.game.Meld(playerID, cmd.CardIndexes, mt)
		})
	case protocol.CmdLayOff:
		s.handleAction(playerID, func() error {
			return s.game.LayOff(playerID, cmd.CardIndexes, cmd.MeldIndex)
		})
	case protocol.CmdDiscard:
		s.handleAction(playerID, func() error { return s.game.Discard(playerID, cmd.CardIndex) })
	case protocol.CmdNextRound:
		s.handleNextRound(playerID)
	case protocol.CmdChat:
		s.broadcastChatLocked(playerID, cmd.Text)
	default:
		s.sendErrorTo(playerID, "comando desconocido: "+cmd.Type)
	}
}

func (s *Server) handleStart(playerID string) {
	if playerID != s.hostID {
		s.sendErrorTo(playerID, "solo el anfitrión puede iniciar la partida")
		return
	}
	if s.started {
		s.sendErrorTo(playerID, "la partida ya comenzó")
		return
	}
	if len(s.lobby) < 2 {
		s.sendErrorTo(playerID, "se necesitan al menos 2 jugadores para comenzar")
		return
	}

	players := make([]struct{ ID, Name string }, len(s.lobby))
	for i, e := range s.lobby {
		players[i] = struct{ ID, Name string }{ID: e.id, Name: e.name}
	}

	var err error
	s.game, err = game.NewGame(players, rand.Int63())
	if err != nil {
		s.sendErrorTo(playerID, err.Error())
		return
	}
	s.started = true
	log.Printf("[server] Game started with %d players", len(players))

	// Flush any pending events (e.g. version mismatch warnings from the lobby phase).
	for _, ev := range s.pendingEvents {
		s.game.AddEvent(ev)
	}
	s.pendingEvents = nil

	s.broadcastStateLocked()
	s.scheduleAutoPlayLocked()
}

func (s *Server) handleAction(playerID string, action func() error) {
	if !s.started || s.game == nil {
		s.sendErrorTo(playerID, "la partida no ha comenzado")
		return
	}
	if err := action(); err != nil {
		s.sendErrorTo(playerID, err.Error())
		return
	}
	s.broadcastStateLocked()
	// After any action that may advance the turn, check whether the next active
	// player is disconnected and schedule auto-play if so.
	s.scheduleAutoPlayLocked()
}

func (s *Server) handleNextRound(playerID string) {
	if s.game == nil || s.game.Phase == game.PhaseGameOver {
		return
	}
	if s.game.Phase != game.PhaseRoundEnd {
		s.sendErrorTo(playerID, "la ronda no ha terminado")
		return
	}
	// Only host (or any connected player) triggers next round.
	if err := s.game.NextRound(); err != nil {
		s.sendErrorTo(playerID, err.Error())
		return
	}
	s.broadcastStateLocked()
	// New round might start with a disconnected player's turn.
	s.scheduleAutoPlayLocked()
}

// ─── Broadcast helpers ────────────────────────────────────────────────────────

// broadcastLobby sends a lobby state snapshot to all connected clients.
// Must NOT hold mu when called (it acquires it).
func (s *Server) broadcastLobby() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broadcastLobbyLocked()
}

func (s *Server) broadcastLobbyLocked() {
	var names []string
	for _, e := range s.lobby {
		if e.connected {
			names = append(names, e.name)
		}
	}
	ls := protocol.LobbyState{
		Players:    names,
		HostID:     s.hostID,
		PublicAddr: s.publicAddr,
	}
	for _, cl := range s.clients {
		if cl.connected {
			_ = s.enqueueEnvelope(cl, "lobby", ls)
		}
	}
}

// broadcastStateLocked sends personalized state snapshots to all connected clients.
// Caller must hold mu.
func (s *Server) broadcastStateLocked() {
	if s.game == nil {
		s.broadcastLobbyLocked()
		return
	}
	for id, cl := range s.clients {
		if !cl.connected {
			continue
		}
		snap := buildSnapshot(s.game, id)
		_ = s.enqueueEnvelope(cl, protocol.EvtState, snap)
	}
}

// maxChatTextLen caps a single chat message (in bytes) to keep envelopes sane.
const maxChatTextLen = 4000

func (s *Server) broadcastChatLocked(senderID, text string) {
	text = strings.TrimRight(text, " \n")
	if strings.TrimSpace(text) == "" {
		return
	}
	if len(text) > maxChatTextLen {
		text = text[:maxChatTextLen]
	}
	name := senderID
	if s.game != nil {
		if p := s.game.PlayerByID(senderID); p != nil {
			name = p.Name
		}
	} else if cl, ok := s.clients[senderID]; ok && cl.name != "" {
		name = cl.name
	}
	msg := fmt.Sprintf("[%s] %s", name, text)
	for _, cl := range s.clients {
		_ = s.enqueueEnvelope(cl, protocol.EvtMessage, map[string]string{"text": msg})
	}
}

func (s *Server) sendErrorTo(playerID, msg string) {
	cl, ok := s.clients[playerID]
	if !ok {
		return
	}
	_ = s.enqueueEnvelope(cl, protocol.EvtError, map[string]string{"message": msg})
}

func (s *Server) enqueueEnvelope(cl *client, evtType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	env := protocol.Envelope{Type: evtType, Payload: raw}
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	select {
	case cl.send <- data:
	default:
		// Drop if buffer full to avoid blocking server mutex.
	}
	return nil
}

// ─── Client write pump ────────────────────────────────────────────────────────

func (cl *client) writePump() {
	for data := range cl.send {
		if _, err := cl.conn.Write(data); err != nil {
			break
		}
	}
}

func (pnd *pending) writePump() {
	defer close(pnd.done)
	for data := range pnd.send {
		if _, err := pnd.conn.Write(data); err != nil {
			break
		}
	}
}

// ─── State snapshot builder ───────────────────────────────────────────────────

// eventLogTailSize is the number of recent lifetime-log entries included in
// every snapshot. Reconnecting clients use this to quickly recover context.
// 50 entries is plenty without bloating the wire payload significantly.
const eventLogTailSize = 50

func buildSnapshot(g *game.Game, selfID string) protocol.StateSnapshot {
	snap := protocol.StateSnapshot{
		Phase:        g.Phase.String(),
		Round:        g.Round,
		StockCount:   len(g.Stock),
		Events:       g.Events,
		EventLogTail: g.EventLogTail(eventLogTailSize),
	}

	// ScoreHistory: convert engine type to protocol view.
	for _, rs := range g.ScoreHistory {
		rv := protocol.RoundScoresView{
			Round:  rs.Round,
			Scores: make(map[string]int, len(rs.Scores)),
			Names:  make(map[string]string, len(rs.Names)),
		}
		for id, pts := range rs.Scores {
			rv.Scores[id] = pts
		}
		for id, name := range rs.Names {
			rv.Names[id] = name
		}
		snap.ScoreHistory = append(snap.ScoreHistory, rv)
	}
	if g.Phase == game.PhaseGameOver {
		w := g.Winner()
		if w != nil {
			snap.WinnerID = w.ID
			snap.WinnerName = w.Name
		}
	}

	if len(g.Players) > 0 {
		snap.ActiveID = g.Players[g.ActiveIndex].ID
		nextIdx := (g.ActiveIndex + 1) % len(g.Players)
		snap.NextID = g.Players[nextIdx].ID
	}

	// Discard pile top.
	if top, ok := g.DiscardTop(); ok {
		cv := cardView(top, false)
		snap.DiscardTop = &cv
	}

	// Players.
	for i, p := range g.Players {
		pv := protocol.PlayerView{
			ID:         p.ID,
			Name:       p.Name,
			CardCount:  len(p.Hand),
			TotalScore: p.TotalScore,
			RoundScore: p.RoundScore,
			HasMelded:  p.HasMelded,
			Connected:  p.Connected,
			IsActive:   p.ID == snap.ActiveID,
			IsSelf:     p.ID == selfID,
			TurnIndex:  i + 1, // 1-based, fixed by Players slice order
		}
		snap.Players = append(snap.Players, pv)

		// Self hand.
		if p.ID == selfID {
			for _, c := range p.Hand {
				snap.Hand = append(snap.Hand, cardView(c, false))
			}
			// Include picked-up discard for the recipient only.
			if p.PickedUpDiscard != nil {
				cv := cardView(*p.PickedUpDiscard, false)
				snap.PickedUpDiscard = &cv
			}
		}
	}

	// Round reveal: only during round_end and game_over phases.
	if (g.Phase == game.PhaseRoundEnd || g.Phase == game.PhaseGameOver) && g.LastRoundResult != nil {
		rr := g.LastRoundResult
		// Determine the round winner: the player whose RoundScore == 0 and hand was empty.
		// In LastRoundResult the winner is the player with an empty hand.
		winnerID := ""
		for _, pr := range rr.Results {
			if len(pr.Hand) == 0 {
				winnerID = pr.PlayerID
				break
			}
		}
		for _, pr := range rr.Results {
			rph := protocol.RevealedPlayerHand{
				PlayerID:         pr.PlayerID,
				PlayerName:       pr.PlayerName,
				RoundScore:       pr.RoundScore,
				TotalScore:       pr.TotalScore,
				IsWinner:         pr.PlayerID == winnerID,
				WentOutInOnePlay: pr.WentOutInOnePlay,
			}
			for _, c := range pr.Hand {
				rph.Cards = append(rph.Cards, cardView(c, false))
			}
			snap.RoundReveal = append(snap.RoundReveal, rph)
		}
	}

	// Melds.
	for i, m := range g.Melds {
		mv := protocol.MeldView{
			Index:   i,
			OwnerID: m.OwnerID,
		}
		if m.Type == game.MeldPierna {
			mv.Type = "pierna"
		} else {
			mv.Type = "escalera"
		}
		// Find owner name.
		op := g.PlayerByID(m.OwnerID)
		if op != nil {
			mv.OwnerName = op.Name
		}
		for _, c := range m.Cards {
			mv.Cards = append(mv.Cards, cardView(c, false))
		}
		snap.Melds = append(snap.Melds, mv)
	}

	return snap
}

func cardView(c game.Card, hidden bool) protocol.CardView {
	cv := protocol.CardView{
		Rank:       int(c.Rank),
		Suit:       int(c.Suit),
		JokerIndex: c.JokerIndex,
		Hidden:     hidden,
		Label:      cardLabel(c),
	}
	return cv
}

func cardLabel(c game.Card) string {
	if c.IsJoker() {
		return "★JKR"
	}
	return fmt.Sprintf("%s%s", c.Rank.String(), c.Suit.String())
}
