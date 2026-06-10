// Package server implements the Loba TCP game server.
// It is server-authoritative: all game logic runs here; clients are
// dumb renderers that send commands and receive state snapshots.
package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"loba/internal/game"
	"loba/internal/protocol"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"
)

// Server manages one Loba game session.
type Server struct {
	mu         sync.Mutex
	game       *game.Game
	clients    map[string]*client // keyed by player ID
	hostID     string
	lobby      []lobbyEntry // ordered player join list
	started    bool
	port       string
	hostName   string
	publicAddr string // bore.pub public address, empty when --public is not used
}

type lobbyEntry struct {
	id   string
	name string
}

type client struct {
	id   string
	name string
	conn net.Conn
	send chan []byte // buffered outbound channel
}

// New creates a new server that will listen on the given port.
func New(port, hostName string) *Server {
	return &Server{
		clients:  make(map[string]*client),
		port:     port,
		hostName: hostName,
	}
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
	defer conn.Close()
	r := bufio.NewReader(conn)

	// First message must be a join command.
	var cmd protocol.Command
	if err := protocol.ReadJSON(r, &cmd); err != nil || cmd.Type != protocol.CmdJoin {
		_ = protocol.SendError(conn, "first message must be a join command")
		return
	}

	playerID, err := s.registerPlayer(conn, cmd.Name)
	if err != nil {
		_ = protocol.SendError(conn, err.Error())
		return
	}

	log.Printf("[server] %s joined as %s", conn.RemoteAddr(), cmd.Name)

	// Start the outbound writer goroutine.
	s.mu.Lock()
	cl := s.clients[playerID]
	s.mu.Unlock()
	go cl.writePump()

	// Broadcast updated lobby state.
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

func (s *Server) registerPlayer(conn net.Conn, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return "", fmt.Errorf("game already started")
	}
	if len(s.lobby) >= 6 {
		return "", fmt.Errorf("game is full (max 6 players)")
	}
	if name == "" {
		name = fmt.Sprintf("Player%d", len(s.lobby)+1)
	}

	id := fmt.Sprintf("p%d", len(s.lobby)+1)

	cl := &client{
		id:   id,
		name: name,
		conn: conn,
		send: make(chan []byte, 64),
	}
	s.clients[id] = cl
	s.lobby = append(s.lobby, lobbyEntry{id: id, name: name})

	if s.hostID == "" {
		s.hostID = id
		log.Printf("[server] %s is the host", name)
	}

	return id, nil
}

func (s *Server) markDisconnected(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.game != nil {
		p := s.game.PlayerByID(playerID)
		if p != nil {
			p.Connected = false
			log.Printf("[server] %s disconnected", p.Name)
		}
	}

	if cl, ok := s.clients[playerID]; ok {
		close(cl.send)
		delete(s.clients, playerID)
	}

	// If game is running and it's the disconnected player's turn, auto-play.
	if s.game != nil && s.game.Phase == game.PhaseDrawing {
		active := s.game.Players[s.game.ActiveIndex]
		if active.ID == playerID {
			go s.runAutoPlay(playerID)
		}
	}
	s.broadcastStateLocked()
}

func (s *Server) runAutoPlay(playerID string) {
	time.Sleep(500 * time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.game == nil {
		return
	}
	active := s.game.Players[s.game.ActiveIndex]
	if active.ID == playerID && !active.Connected {
		_ = s.game.AutoPlayDisconnected()
		s.broadcastStateLocked()
	}
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
		s.sendErrorTo(playerID, "unknown command: "+cmd.Type)
	}
}

func (s *Server) handleStart(playerID string) {
	if playerID != s.hostID {
		s.sendErrorTo(playerID, "only the host can start the game")
		return
	}
	if s.started {
		s.sendErrorTo(playerID, "game already started")
		return
	}
	if len(s.lobby) < 2 {
		s.sendErrorTo(playerID, "need at least 2 players to start")
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
	s.broadcastStateLocked()
}

func (s *Server) handleAction(playerID string, action func() error) {
	if !s.started || s.game == nil {
		s.sendErrorTo(playerID, "game has not started")
		return
	}
	if err := action(); err != nil {
		s.sendErrorTo(playerID, err.Error())
		return
	}
	s.broadcastStateLocked()
}

func (s *Server) handleNextRound(playerID string) {
	if s.game == nil || s.game.Phase == game.PhaseGameOver {
		return
	}
	if s.game.Phase != game.PhaseRoundEnd {
		s.sendErrorTo(playerID, "round has not ended")
		return
	}
	// Only host (or any connected player) triggers next round.
	if err := s.game.NextRound(); err != nil {
		s.sendErrorTo(playerID, err.Error())
		return
	}
	s.broadcastStateLocked()
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
	names := make([]string, len(s.lobby))
	for i, e := range s.lobby {
		names[i] = e.name
	}
	ls := protocol.LobbyState{
		Players:    names,
		HostID:     s.hostID,
		PublicAddr: s.publicAddr,
	}
	for _, cl := range s.clients {
		_ = s.enqueueEnvelope(cl, "lobby", ls)
	}
}

// broadcastStateLocked sends personalized state snapshots to all clients.
// Caller must hold mu.
func (s *Server) broadcastStateLocked() {
	if s.game == nil {
		s.broadcastLobbyLocked()
		return
	}
	for id, cl := range s.clients {
		snap := buildSnapshot(s.game, id)
		_ = s.enqueueEnvelope(cl, protocol.EvtState, snap)
	}
}

func (s *Server) broadcastChatLocked(senderID, text string) {
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

// ─── State snapshot builder ───────────────────────────────────────────────────

func buildSnapshot(g *game.Game, selfID string) protocol.StateSnapshot {
	snap := protocol.StateSnapshot{
		Phase:      g.Phase.String(),
		Round:      g.Round,
		StockCount: len(g.Stock),
		Events:     g.Events,
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
	}

	// Discard pile top.
	if top, ok := g.DiscardTop(); ok {
		cv := cardView(top, false)
		snap.DiscardTop = &cv
	}

	// Players.
	for _, p := range g.Players {
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
