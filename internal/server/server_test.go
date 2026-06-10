package server

import (
	"bufio"
	"encoding/json"
	"loba/internal/game"
	"loba/internal/protocol"
	"net"
	"strings"
	"testing"
	"time"
)

// ─── Test helpers ─────────────────────────────────────────────────────────────

// testConn wraps both sides of an in-process net.Pipe connection.
type testConn struct {
	client net.Conn // client side (the fake player)
	server net.Conn // server side (fed to handleConn)
	reader *bufio.Reader
}

func newTestConn(t *testing.T) *testConn {
	t.Helper()
	c, s, err := netPipe()
	if err != nil {
		t.Fatalf("net.Pipe: %v", err)
	}
	return &testConn{
		client: c,
		server: s,
		reader: bufio.NewReader(c),
	}
}

func netPipe() (net.Conn, net.Conn, error) {
	c, s := net.Pipe()
	return c, s, nil
}

// send writes a Command to the client side.
func (tc *testConn) send(t *testing.T, cmd protocol.Command) {
	t.Helper()
	if err := protocol.WriteJSON(tc.client, cmd); err != nil {
		t.Fatalf("send %s: %v", cmd.Type, err)
	}
}

// recv reads one Envelope from the client side (with a short deadline).
func (tc *testConn) recv(t *testing.T) protocol.Envelope {
	t.Helper()
	tc.client.SetReadDeadline(time.Now().Add(2 * time.Second))
	defer tc.client.SetReadDeadline(time.Time{})
	var env protocol.Envelope
	if err := protocol.ReadJSON(tc.reader, &env); err != nil {
		t.Fatalf("recv: %v", err)
	}
	return env
}

// recvState reads envelopes until it gets a "state" event.
func (tc *testConn) recvState(t *testing.T) protocol.StateSnapshot {
	t.Helper()
	for {
		env := tc.recv(t)
		if env.Type == protocol.EvtState {
			var snap protocol.StateSnapshot
			if err := json.Unmarshal(env.Payload, &snap); err != nil {
				t.Fatalf("unmarshal state: %v", err)
			}
			return snap
		}
		// Ignore lobby / message / error envelopes.
	}
}

// recvError reads envelopes until it gets an "error" event.
func (tc *testConn) recvError(t *testing.T) string {
	t.Helper()
	for {
		env := tc.recv(t)
		if env.Type == protocol.EvtError {
			var e map[string]string
			if err := json.Unmarshal(env.Payload, &e); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			return e["message"]
		}
	}
}

// startServer starts a Server with a real TCP listener on :0.
func startServer(t *testing.T) (*Server, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := New("0", "host")
	go srv.Serve(ln) //nolint
	t.Cleanup(func() { ln.Close() })
	return srv, ln.Addr().String()
}

// dialAndJoin connects to addr, sends a join command, and returns the connection
// plus a buffered reader for it.
func dialAndJoin(t *testing.T, addr, name string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	r := bufio.NewReader(conn)
	if err := protocol.WriteJSON(conn, protocol.Command{Type: protocol.CmdJoin, Name: name}); err != nil {
		t.Fatalf("join write: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, r
}

// readEnvelope reads one newline-delimited envelope with a 2 s deadline.
func readEnvelope(t *testing.T, r *bufio.Reader) protocol.Envelope {
	t.Helper()
	// Peek at the underlying conn via the reader; we use a separate deadline trick.
	var env protocol.Envelope
	done := make(chan error, 1)
	go func() {
		done <- protocol.ReadJSON(r, &env)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("readEnvelope: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("readEnvelope timeout")
	}
	return env
}

// drainUntilState reads envelopes until a "state" envelope arrives.
func drainUntilState(t *testing.T, r *bufio.Reader) protocol.StateSnapshot {
	t.Helper()
	for {
		env := readEnvelope(t, r)
		if env.Type == protocol.EvtState {
			var snap protocol.StateSnapshot
			if err := json.Unmarshal(env.Payload, &snap); err != nil {
				t.Fatalf("unmarshal state: %v", err)
			}
			return snap
		}
	}
}

// drainUntilError reads envelopes until an "error" envelope arrives.
func drainUntilError(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	for {
		env := readEnvelope(t, r)
		if env.Type == protocol.EvtError {
			var e map[string]string
			if err := json.Unmarshal(env.Payload, &e); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			return e["message"]
		}
	}
}

// drainLobby reads one envelope and expects it to be a lobby update.
// Call this to consume the lobby broadcast sent when another player joins.
func drainLobby(t *testing.T, r *bufio.Reader) {
	t.Helper()
	for {
		env := readEnvelope(t, r)
		if env.Type == "lobby" {
			return
		}
	}
}

// drainUntilSeats reads envelopes until a "seats" envelope arrives.
func drainUntilSeats(t *testing.T, r *bufio.Reader) protocol.SeatsOffer {
	t.Helper()
	for {
		env := readEnvelope(t, r)
		if env.Type == protocol.EvtSeats {
			var offer protocol.SeatsOffer
			if err := json.Unmarshal(env.Payload, &offer); err != nil {
				t.Fatalf("unmarshal seats offer: %v", err)
			}
			return offer
		}
	}
}

// joinStartedGame connects to a started game and returns the conn, reader, and
// the initial seat offer sent by the server.
func joinStartedGame(t *testing.T, addr string) (net.Conn, *bufio.Reader, protocol.SeatsOffer) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	r := bufio.NewReader(conn)
	if err := protocol.WriteJSON(conn, protocol.Command{Type: protocol.CmdJoin}); err != nil {
		t.Fatalf("join write: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	offer := drainUntilSeats(t, r)
	return conn, r, offer
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestReconnectSeatPickerHandAndScorePreserved: Alice joins, game starts, Alice
// disconnects, reconnects via the seat picker, claims her seat, and her hand +
// score are preserved.
func TestReconnectSeatPickerHandAndScorePreserved(t *testing.T) {
	_, addr := startServer(t)

	// Alice joins first and becomes host.
	aliceConn, aliceR := dialAndJoin(t, addr, "Alice")
	drainLobby(t, aliceR)

	// Bob joins.
	bobConn, bobR := dialAndJoin(t, addr, "Bob")
	_, _ = bobConn, bobR
	drainLobby(t, aliceR)

	// Alice starts the game.
	if err := protocol.WriteJSON(aliceConn, protocol.Command{Type: protocol.CmdStart}); err != nil {
		t.Fatalf("start: %v", err)
	}
	snap := drainUntilState(t, aliceR)
	if snap.Phase == "" {
		t.Fatal("expected non-empty phase")
	}
	initialHandSize := len(snap.Hand)
	if initialHandSize == 0 {
		t.Fatal("expected non-empty hand")
	}
	aliceID := ""
	for _, p := range snap.Players {
		if p.IsSelf {
			aliceID = p.ID
		}
	}

	// Drop Alice's connection.
	aliceConn.Close()
	time.Sleep(100 * time.Millisecond)

	// Alice reconnects: expects a seat offer, not a direct state snapshot.
	aliceConn2, aliceR2, offer := joinStartedGame(t, addr)

	if len(offer.Seats) == 0 {
		t.Fatal("expected at least one seat in the offer")
	}

	// Find Alice's seat by ID.
	foundSeat := false
	for _, seat := range offer.Seats {
		if seat.ID == aliceID {
			foundSeat = true
			// Claim Alice's seat.
			if err := protocol.WriteJSON(aliceConn2, protocol.Command{
				Type:   protocol.CmdClaimSeat,
				SeatID: seat.ID,
			}); err != nil {
				t.Fatalf("claim seat: %v", err)
			}
			break
		}
	}
	if !foundSeat {
		t.Fatalf("Alice's seat (ID %s) not found in offer %+v", aliceID, offer)
	}

	// After claiming, should receive a state snapshot.
	snap2 := drainUntilState(t, aliceR2)
	if len(snap2.Hand) != initialHandSize {
		t.Errorf("hand size after rejoin: got %d, want %d", len(snap2.Hand), initialHandSize)
	}
	for _, p := range snap2.Players {
		if p.IsSelf && !p.Connected {
			t.Error("expected Connected=true after rejoin")
		}
	}
}

// TestReconnectNoFreeSeats: a join during an active game when no player is
// disconnected must be rejected with a clear error.
func TestReconnectNoFreeSeats(t *testing.T) {
	_, addr := startServer(t)

	aliceConn, aliceR := dialAndJoin(t, addr, "Alice")
	drainLobby(t, aliceR)
	bobConn, bobR := dialAndJoin(t, addr, "Bob")
	_, _ = bobConn, bobR
	drainLobby(t, aliceR)

	if err := protocol.WriteJSON(aliceConn, protocol.Command{Type: protocol.CmdStart}); err != nil {
		t.Fatalf("start: %v", err)
	}
	drainUntilState(t, aliceR)
	// Alice and Bob are both still connected — no free seats.

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	if err := protocol.WriteJSON(conn, protocol.Command{Type: protocol.CmdJoin}); err != nil {
		t.Fatalf("join write: %v", err)
	}
	errMsg := drainUntilError(t, r)
	if !strings.Contains(errMsg, "no hay lugares libres") {
		t.Errorf("unexpected error %q; want 'no hay lugares libres'", errMsg)
	}
}

// TestReconnectClaimRacedSeat: two clients join a game where only one seat is
// disconnected. The first to claim wins; the second gets an error.
// We verify this property without assuming which connection wins the race.
func TestReconnectClaimRacedSeat(t *testing.T) {
	_, addr := startServer(t)

	aliceConn, aliceR := dialAndJoin(t, addr, "Alice")
	drainLobby(t, aliceR)
	bobConn, bobR := dialAndJoin(t, addr, "Bob")
	_, _ = bobConn, bobR
	drainLobby(t, aliceR)

	if err := protocol.WriteJSON(aliceConn, protocol.Command{Type: protocol.CmdStart}); err != nil {
		t.Fatalf("start: %v", err)
	}
	snap := drainUntilState(t, aliceR)
	aliceID := ""
	for _, p := range snap.Players {
		if p.IsSelf {
			aliceID = p.ID
		}
	}

	// Drop Alice so her seat becomes available.
	aliceConn.Close()
	time.Sleep(100 * time.Millisecond)

	// Two newcomers race to claim Alice's seat.
	conn1, r1, offer1 := joinStartedGame(t, addr)
	conn2, r2, offer2 := joinStartedGame(t, addr)

	if len(offer1.Seats) == 0 || len(offer2.Seats) == 0 {
		t.Fatal("expected seats in both offers")
	}

	// Both try to claim the same seat.
	claimAlice := protocol.Command{Type: protocol.CmdClaimSeat, SeatID: aliceID}
	if err := protocol.WriteJSON(conn1, claimAlice); err != nil {
		t.Fatalf("conn1 claim: %v", err)
	}
	if err := protocol.WriteJSON(conn2, claimAlice); err != nil {
		t.Fatalf("conn2 claim: %v", err)
	}

	// One connection wins (gets a state snapshot) and the other loses (gets an error).
	// We read from both without assuming order: use channels with a timeout.
	type result struct {
		gotState bool
		gotError bool
	}
	readResult := func(r *bufio.Reader) result {
		for {
			done := make(chan protocol.Envelope, 1)
			go func() {
				var env protocol.Envelope
				if err := protocol.ReadJSON(r, &env); err == nil {
					done <- env
				} else {
					close(done)
				}
			}()
			select {
			case env, ok := <-done:
				if !ok {
					return result{} // EOF / closed
				}
				if env.Type == protocol.EvtState {
					return result{gotState: true}
				}
				if env.Type == protocol.EvtError {
					return result{gotError: true}
				}
				// seats refresh or other — keep reading
			case <-time.After(3 * time.Second):
				return result{}
			}
		}
	}

	res1 := readResult(r1)
	res2 := readResult(r2)

	winners := 0
	losers := 0
	if res1.gotState {
		winners++
	}
	if res1.gotError {
		losers++
	}
	if res2.gotState {
		winners++
	}
	if res2.gotError {
		losers++
	}

	if winners != 1 {
		t.Errorf("expected exactly 1 winner, got %d", winners)
	}
	if losers != 1 {
		t.Errorf("expected exactly 1 loser, got %d", losers)
	}
}

// TestDuplicateConnectedNameRejected: if "Alice" is already connected, a second
// join with the same name must be rejected.
func TestDuplicateConnectedNameRejected(t *testing.T) {
	_, addr := startServer(t)

	_, _ = dialAndJoin(t, addr, "Alice")

	// Second "Alice" in the lobby — should be rejected.
	conn2, r2 := dialAndJoin(t, addr, "Alice")
	errMsg := drainUntilError(t, r2)
	conn2.Close()

	if errMsg == "" {
		t.Error("expected rejection for duplicate connected name")
	}
}

// ─── Lobby disconnection tests ───────────────────────────────────────────────

// drainUntilLobby reads envelopes until a "lobby" envelope arrives and returns
// the decoded LobbyState.
func drainUntilLobby(t *testing.T, r *bufio.Reader) protocol.LobbyState {
	t.Helper()
	for {
		env := readEnvelope(t, r)
		if env.Type == "lobby" {
			var ls protocol.LobbyState
			if err := json.Unmarshal(env.Payload, &ls); err != nil {
				t.Fatalf("unmarshal lobby: %v", err)
			}
			return ls
		}
	}
}

// TestLobbyDisconnectRemovesPlayer: Bob joins, then drops his connection.
// Alice (host) must receive a lobby broadcast that no longer includes Bob.
func TestLobbyDisconnectRemovesPlayer(t *testing.T) {
	_, addr := startServer(t)

	// Alice joins first — she is the host.
	aliceConn, aliceR := dialAndJoin(t, addr, "Alice")
	drainUntilLobby(t, aliceR) // consume Alice's own join broadcast

	// Bob joins.
	bobConn, _ := dialAndJoin(t, addr, "Bob")
	// Alice receives a lobby update showing both players.
	ls := drainUntilLobby(t, aliceR)
	if len(ls.Players) != 2 {
		t.Fatalf("expected 2 players after Bob joins, got %d: %v", len(ls.Players), ls.Players)
	}

	// Drop Bob's connection abruptly.
	bobConn.Close()

	// Alice must receive a lobby update that reflects Bob's removal.
	ls2 := drainUntilLobby(t, aliceR)
	for _, name := range ls2.Players {
		if strings.EqualFold(name, "Bob") {
			t.Errorf("Bob still listed after disconnect: %v", ls2.Players)
		}
	}
	if len(ls2.Players) != 1 {
		t.Errorf("expected 1 player after Bob disconnects, got %d: %v", len(ls2.Players), ls2.Players)
	}

	_ = aliceConn
}

// TestLobbyHostDisconnectPromotion: Alice (host) drops. Bob must receive a
// lobby broadcast where HostID is Bob's ID (he is promoted).
func TestLobbyHostDisconnectPromotion(t *testing.T) {
	_, addr := startServer(t)

	// Alice joins first — she is the host.
	aliceConn, aliceR := dialAndJoin(t, addr, "Alice")
	drainUntilLobby(t, aliceR)

	// Bob joins.
	bobConn, bobR := dialAndJoin(t, addr, "Bob")
	_ = bobConn
	// Consume Alice's join-notification and Bob's own join broadcast.
	drainUntilLobby(t, aliceR)
	ls := drainUntilLobby(t, bobR)

	// Capture Bob's player ID from the lobby we just received.
	// The HostID in ls should currently be Alice's ID ("p1").
	if ls.HostID == "" {
		t.Fatal("expected non-empty HostID before host disconnects")
	}
	aliceHostID := ls.HostID

	// Drop Alice.
	aliceConn.Close()

	// Bob should receive a lobby broadcast with an updated HostID pointing to him.
	ls2 := drainUntilLobby(t, bobR)
	if ls2.HostID == "" {
		t.Fatal("HostID empty after host disconnected")
	}
	if ls2.HostID == aliceHostID {
		t.Errorf("HostID still %s (Alice's) after she disconnected; expected Bob to be promoted", aliceHostID)
	}
}

// TestInGameDisconnectReconnectUnchanged: verify that the existing in-game
// reconnection flow (seat picker) still works after the lobby-disconnect changes.
func TestInGameDisconnectReconnectUnchanged(t *testing.T) {
	_, addr := startServer(t)

	aliceConn, aliceR := dialAndJoin(t, addr, "Alice")
	drainUntilLobby(t, aliceR)
	bobConn, bobR := dialAndJoin(t, addr, "Bob")
	_, _ = bobConn, bobR
	drainUntilLobby(t, aliceR)

	if err := protocol.WriteJSON(aliceConn, protocol.Command{Type: protocol.CmdStart}); err != nil {
		t.Fatalf("start: %v", err)
	}
	snap := drainUntilState(t, aliceR)
	aliceID := ""
	for _, p := range snap.Players {
		if p.IsSelf {
			aliceID = p.ID
		}
	}

	// Drop Alice mid-game.
	aliceConn.Close()
	time.Sleep(100 * time.Millisecond)

	// Reconnect via seat picker.
	aliceConn2, aliceR2, offer := joinStartedGame(t, addr)
	_ = aliceConn2
	if len(offer.Seats) == 0 {
		t.Fatal("expected at least one seat")
	}
	for _, seat := range offer.Seats {
		if seat.ID == aliceID {
			if err := protocol.WriteJSON(aliceConn2, protocol.Command{
				Type:   protocol.CmdClaimSeat,
				SeatID: seat.ID,
			}); err != nil {
				t.Fatalf("claim seat: %v", err)
			}
			break
		}
	}

	snap2 := drainUntilState(t, aliceR2)
	if snap2.Phase == "" {
		t.Fatal("expected non-empty phase after reconnect")
	}
}

// ─── Round reveal snapshot tests ─────────────────────────────────────────────

// TestSnapshotNoRevealDuringPlay verifies that RoundReveal is absent from
// snapshots during normal (non-round-end) play, so opponents' hands stay hidden.
func TestSnapshotNoRevealDuringPlay(t *testing.T) {
	_, addr := startServer(t)

	aliceConn, aliceR := dialAndJoin(t, addr, "Alice")
	drainLobby(t, aliceR)
	bobConn, bobR := dialAndJoin(t, addr, "Bob")
	_, _ = bobConn, bobR
	drainLobby(t, aliceR)

	if err := protocol.WriteJSON(aliceConn, protocol.Command{Type: protocol.CmdStart}); err != nil {
		t.Fatalf("start: %v", err)
	}
	snap := drainUntilState(t, aliceR)

	if snap.Phase == "round_end" || snap.Phase == "game_over" {
		t.Skip("game ended immediately, cannot test mid-round")
	}
	if len(snap.RoundReveal) != 0 {
		t.Errorf("RoundReveal must be empty during play, got %d entries", len(snap.RoundReveal))
	}
}

// TestBuildSnapshotRevealDirect tests buildSnapshot directly using a rigged game.
// server_test.go is in package server so it has access to unexported buildSnapshot
// and can import loba/internal/game.
func TestBuildSnapshotRevealDirect(t *testing.T) {
	// Build a minimal 2-player game and force it into PhaseRoundEnd.
	g, _ := buildTestGame(t)

	// During play: no reveal.
	snapPlay := buildSnapshot(g, "p1")
	if len(snapPlay.RoundReveal) != 0 {
		t.Errorf("RoundReveal must be empty during play, got %d", len(snapPlay.RoundReveal))
	}

	// Rig hands and force round end via engine.
	g.Players[0].Hand = gameHand(gameCard(5, 0))    // Alice: 5♠
	g.Players[1].Hand = gameHand(gameCard(13, 1), gameCard(5, 3)) // Bob: K♥ + 5♣ = 15
	g.Phase = gamePhaseMelding()

	if err := g.Discard("p1", 0); err != nil {
		t.Fatalf("discard: %v", err)
	}

	if g.Phase != gamePhaseRoundEnd() && g.Phase != gamePhaseGameOver() {
		t.Fatalf("expected round_end or game_over, got %s", g.Phase)
	}

	snapEnd := buildSnapshot(g, "p1")
	if len(snapEnd.RoundReveal) == 0 {
		t.Fatal("RoundReveal must be populated at round_end")
	}
	if len(snapEnd.RoundReveal) != 2 {
		t.Errorf("expected 2 RoundReveal entries, got %d", len(snapEnd.RoundReveal))
	}

	var winnerEntry, loserEntry *protocol.RevealedPlayerHand
	for i := range snapEnd.RoundReveal {
		if snapEnd.RoundReveal[i].IsWinner {
			e := snapEnd.RoundReveal[i]
			winnerEntry = &e
		} else {
			e := snapEnd.RoundReveal[i]
			loserEntry = &e
		}
	}
	if winnerEntry == nil {
		t.Fatal("no entry with IsWinner=true")
	}
	if len(winnerEntry.Cards) != 0 {
		t.Errorf("winner's card slice must be empty, got %v", winnerEntry.Cards)
	}
	if loserEntry == nil {
		t.Fatal("no entry with IsWinner=false")
	}
	if len(loserEntry.Cards) == 0 {
		t.Error("loser's card slice must be non-empty")
	}
	if loserEntry.RoundScore != 15 {
		t.Errorf("loser RoundScore = %d, want 15 (K♥=10 + 5♣=5)", loserEntry.RoundScore)
	}
}

// ─── Helpers for buildSnapshot unit tests ────────────────────────────────────

// buildTestGame creates a 2-player game in PhaseDrawing using game.NewGame.
func buildTestGame(t *testing.T) (*game.Game, string) {
	t.Helper()
	players := []struct{ ID, Name string }{
		{"p1", "Alice"},
		{"p2", "Bob"},
	}
	g, err := game.NewGame(players, 42)
	if err != nil {
		t.Fatalf("NewGame: %v", err)
	}
	return g, "p1"
}

// gameCard builds a game.Card with the given rank (int) and suit (game.Suit).
func gameCard(rank int, suit game.Suit) game.Card {
	return game.Card{Rank: game.Rank(rank), Suit: suit}
}

// gameHand builds a game.Hand from a variadic list of cards.
func gameHand(cards ...game.Card) game.Hand {
	return game.Hand(cards)
}

// gamePhaseMelding returns game.PhaseMelding.
func gamePhaseMelding() game.Phase { return game.PhaseMelding }

// gamePhaseRoundEnd returns game.PhaseRoundEnd.
func gamePhaseRoundEnd() game.Phase { return game.PhaseRoundEnd }

// gamePhaseGameOver returns game.PhaseGameOver.
func gamePhaseGameOver() game.Phase { return game.PhaseGameOver }

// ─── ScoreHistory + EventLogTail snapshot tests ───────────────────────────────

// TestBuildSnapshotScoreHistoryEmpty verifies that ScoreHistory is absent from
// the snapshot before any round completes.
func TestBuildSnapshotScoreHistoryEmpty(t *testing.T) {
	g, selfID := buildTestGame(t)
	snap := buildSnapshot(g, selfID)
	if len(snap.ScoreHistory) != 0 {
		t.Errorf("expected empty ScoreHistory before first round ends, got %d entries", len(snap.ScoreHistory))
	}
}

// TestBuildSnapshotScoreHistoryAfterRound verifies that ScoreHistory is
// populated after a round ends and contains the correct per-player scores.
func TestBuildSnapshotScoreHistoryAfterRound(t *testing.T) {
	g, selfID := buildTestGame(t)

	// Rig and end round 1.
	g.Players[0].Hand = gameHand(gameCard(2, 0))          // Alice: 2♠ (2 pts — but she wins)
	g.Players[1].Hand = gameHand(gameCard(13, 1), gameCard(5, 3)) // Bob: K♥+5♣ = 15
	g.Phase = gamePhaseMelding()
	if err := g.Discard("p1", 0); err != nil {
		t.Fatalf("discard: %v", err)
	}

	snap := buildSnapshot(g, selfID)
	if len(snap.ScoreHistory) != 1 {
		t.Fatalf("expected 1 ScoreHistory entry, got %d", len(snap.ScoreHistory))
	}
	rs := snap.ScoreHistory[0]
	if rs.Round != 1 {
		t.Errorf("ScoreHistory[0].Round = %d, want 1", rs.Round)
	}
	if rs.Scores["p1"] != 0 {
		t.Errorf("Alice round score = %d, want 0", rs.Scores["p1"])
	}
	if rs.Scores["p2"] != 15 {
		t.Errorf("Bob round score = %d, want 15", rs.Scores["p2"])
	}
	if rs.Names["p1"] != "Alice" {
		t.Errorf("Names[p1] = %q, want Alice", rs.Names["p1"])
	}
}

// TestBuildSnapshotEventLogTailPopulated verifies that EventLogTail is non-empty
// after game start (since startRound generates events).
func TestBuildSnapshotEventLogTailPopulated(t *testing.T) {
	g, selfID := buildTestGame(t)
	snap := buildSnapshot(g, selfID)
	if len(snap.EventLogTail) == 0 {
		t.Error("expected non-empty EventLogTail after game start")
	}
}

// TestBuildSnapshotEventLogTailCapped verifies that EventLogTail never exceeds
// eventLogTailSize entries.
func TestBuildSnapshotEventLogTailCapped(t *testing.T) {
	g, selfID := buildTestGame(t)
	// Add more events than the cap.
	for i := 0; i < eventLogTailSize+20; i++ {
		g.AddEvent("spam-event")
	}
	snap := buildSnapshot(g, selfID)
	if len(snap.EventLogTail) > eventLogTailSize {
		t.Errorf("EventLogTail len = %d, want ≤ %d", len(snap.EventLogTail), eventLogTailSize)
	}
}

// TestReconnectGetsEventLogContext is an integration-level test: Alice joins,
// game starts, Alice disconnects and reconnects. The first snapshot she receives
// must include a non-empty EventLogTail so she has recent context.
func TestReconnectGetsEventLogContext(t *testing.T) {
	_, addr := startServer(t)

	aliceConn, aliceR := dialAndJoin(t, addr, "Alice")
	drainLobby(t, aliceR)

	bobConn, bobR := dialAndJoin(t, addr, "Bob")
	_, _ = bobConn, bobR
	drainLobby(t, aliceR)

	if err := protocol.WriteJSON(aliceConn, protocol.Command{Type: protocol.CmdStart}); err != nil {
		t.Fatalf("start: %v", err)
	}
	snap := drainUntilState(t, aliceR)
	aliceID := ""
	for _, p := range snap.Players {
		if p.IsSelf {
			aliceID = p.ID
		}
	}

	// Drop Alice.
	aliceConn.Close()
	time.Sleep(100 * time.Millisecond)

	// Reconnect via seat picker.
	aliceConn2, aliceR2, offer := joinStartedGame(t, addr)
	if len(offer.Seats) == 0 {
		t.Fatal("expected at least one seat")
	}
	foundSeat := false
	for _, seat := range offer.Seats {
		if seat.ID == aliceID {
			foundSeat = true
			if err := protocol.WriteJSON(aliceConn2, protocol.Command{
				Type:   protocol.CmdClaimSeat,
				SeatID: seat.ID,
			}); err != nil {
				t.Fatalf("claim seat: %v", err)
			}
			break
		}
	}
	if !foundSeat {
		t.Fatalf("Alice's seat not in offer")
	}

	snap2 := drainUntilState(t, aliceR2)
	if len(snap2.EventLogTail) == 0 {
		t.Error("reconnecting client must receive non-empty EventLogTail")
	}
}

// ─── No-name reconnect protocol tests ────────────────────────────────────────

// drainUntilNameRequired reads envelopes until a "name_required" envelope arrives.
func drainUntilNameRequired(t *testing.T, r *bufio.Reader) {
	t.Helper()
	for {
		env := readEnvelope(t, r)
		if env.Type == protocol.EvtNameRequired {
			return
		}
	}
}

// TestEmptyNameLobbyGetsNamePrompt: joining the lobby with an empty name must
// trigger a name_required event. After sending a join with a real name the server
// registers the player and sends a lobby update.
func TestEmptyNameLobbyGetsNamePrompt(t *testing.T) {
	_, addr := startServer(t)

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)

	// Join with empty name.
	if err := protocol.WriteJSON(conn, protocol.Command{Type: protocol.CmdJoin, Name: ""}); err != nil {
		t.Fatalf("join write: %v", err)
	}

	// Must receive name_required prompt.
	drainUntilNameRequired(t, r)

	// Send a join with a proper name.
	if err := protocol.WriteJSON(conn, protocol.Command{Type: protocol.CmdJoin, Name: "Alvaro"}); err != nil {
		t.Fatalf("name write: %v", err)
	}

	// Server should now register and send a lobby state.
	ls := drainUntilLobby(t, r)
	found := false
	for _, name := range ls.Players {
		if name == "Alvaro" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("player Alvaro not in lobby after name prompt: %v", ls.Players)
	}
}

// TestEmptyNameStartedGameGetsSeatsDirectly: joining a started game with an empty
// name must receive seats immediately — no name exchange at all.
func TestEmptyNameStartedGameGetsSeatsDirectly(t *testing.T) {
	_, addr := startServer(t)

	// Set up a 2-player game and start it.
	aliceConn, aliceR := dialAndJoin(t, addr, "Alice")
	drainLobby(t, aliceR)
	bobConn, bobR := dialAndJoin(t, addr, "Bob")
	_, _ = bobConn, bobR
	drainLobby(t, aliceR)

	if err := protocol.WriteJSON(aliceConn, protocol.Command{Type: protocol.CmdStart}); err != nil {
		t.Fatalf("start: %v", err)
	}
	drainUntilState(t, aliceR)

	// Drop Alice so a seat is free.
	aliceConn.Close()
	time.Sleep(100 * time.Millisecond)

	// Connect with empty name — must get seats without any name_required event.
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)

	if err := protocol.WriteJSON(conn, protocol.Command{Type: protocol.CmdJoin, Name: ""}); err != nil {
		t.Fatalf("join write: %v", err)
	}

	// First envelope must be a seats offer, not name_required.
	env := readEnvelope(t, r)
	if env.Type == protocol.EvtNameRequired {
		t.Error("started game join with empty name must not trigger name_required")
	}
	if env.Type != protocol.EvtSeats {
		t.Errorf("expected seats offer, got %q", env.Type)
	}
}

// TestNamedLobbyJoinUnchanged: joining with --name provided works as before
// (no name_required prompt).
func TestNamedLobbyJoinUnchanged(t *testing.T) {
	_, addr := startServer(t)

	conn, aliceR := dialAndJoin(t, addr, "Alice")
	_ = conn

	// Alice should receive a lobby state immediately (no name_required).
	ls := drainUntilLobby(t, aliceR)
	found := false
	for _, name := range ls.Players {
		if name == "Alice" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Alice not in lobby after named join: %v", ls.Players)
	}
}

// TestTurnIndexPreservedAfterReconnect: disconnect and reclaim must not change the
// Players slice order or ActiveIndex progression.
func TestTurnIndexPreservedAfterReconnect(t *testing.T) {
	_, addr := startServer(t)

	aliceConn, aliceR := dialAndJoin(t, addr, "Alice")
	drainLobby(t, aliceR)
	bobConn, bobR := dialAndJoin(t, addr, "Bob")
	_, _ = bobConn, bobR
	drainLobby(t, aliceR)

	if err := protocol.WriteJSON(aliceConn, protocol.Command{Type: protocol.CmdStart}); err != nil {
		t.Fatalf("start: %v", err)
	}
	snap1 := drainUntilState(t, aliceR)

	// Record Alice's TurnIndex before disconnect.
	aliceID := ""
	aliceTurnIdx := 0
	for _, p := range snap1.Players {
		if p.IsSelf {
			aliceID = p.ID
			aliceTurnIdx = p.TurnIndex
		}
	}
	if aliceTurnIdx == 0 {
		t.Fatal("TurnIndex not populated in snapshot")
	}

	// Drop Alice.
	aliceConn.Close()
	time.Sleep(100 * time.Millisecond)

	// Reconnect via seat picker.
	aliceConn2, aliceR2, offer := joinStartedGame(t, addr)
	_ = aliceConn2
	if len(offer.Seats) == 0 {
		t.Fatal("expected seat offer")
	}
	for _, seat := range offer.Seats {
		if seat.ID == aliceID {
			if err := protocol.WriteJSON(aliceConn2, protocol.Command{
				Type:   protocol.CmdClaimSeat,
				SeatID: seat.ID,
			}); err != nil {
				t.Fatalf("claim seat: %v", err)
			}
			break
		}
	}

	snap2 := drainUntilState(t, aliceR2)

	// Alice's TurnIndex must be unchanged after reconnect.
	aliceTurnIdx2 := 0
	for _, p := range snap2.Players {
		if p.IsSelf {
			aliceTurnIdx2 = p.TurnIndex
		}
	}
	if aliceTurnIdx2 != aliceTurnIdx {
		t.Errorf("TurnIndex changed after reconnect: before=%d after=%d", aliceTurnIdx, aliceTurnIdx2)
	}

	// Players must appear in the same order (compare IDs in order).
	if len(snap1.Players) != len(snap2.Players) {
		t.Fatalf("player count changed: %d → %d", len(snap1.Players), len(snap2.Players))
	}
	for i := range snap1.Players {
		if snap1.Players[i].ID != snap2.Players[i].ID {
			t.Errorf("player order changed at index %d: %s → %s",
				i, snap1.Players[i].ID, snap2.Players[i].ID)
		}
	}
}
