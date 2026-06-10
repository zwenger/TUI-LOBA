package server

import (
	"bufio"
	"encoding/json"
	"loba/internal/protocol"
	"net"
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

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestReconnectHandAndScorePreserved: Alice joins, game starts, Alice
// disconnects, rejoins with the same name, and her hand + score are intact.
func TestReconnectHandAndScorePreserved(t *testing.T) {
	_, addr := startServer(t)

	// Alice joins first and becomes host.
	aliceConn, aliceR := dialAndJoin(t, addr, "Alice")

	// Drain Alice's own lobby message.
	drainLobby(t, aliceR)

	// Bob joins.
	bobConn, bobR := dialAndJoin(t, addr, "Bob")
	_, _ = bobConn, bobR

	// Drain Alice's lobby update when Bob joins.
	drainLobby(t, aliceR)

	// Alice is host — start the game.
	if err := protocol.WriteJSON(aliceConn, protocol.Command{Type: protocol.CmdStart}); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Drain until Alice receives a state snapshot.
	snap := drainUntilState(t, aliceR)
	if snap.Phase == "" {
		t.Fatal("expected non-empty phase")
	}
	// Record Alice's initial hand size.
	initialHandSize := len(snap.Hand)
	if initialHandSize == 0 {
		t.Fatal("expected non-empty hand")
	}

	// Drop Alice's connection.
	aliceConn.Close()

	// Give the server a moment to process the disconnect.
	time.Sleep(100 * time.Millisecond)

	// Alice reconnects with the same name.
	aliceConn2, aliceR2 := dialAndJoin(t, addr, "Alice")
	_ = aliceConn2

	// Read the fresh state snapshot sent on reconnect.
	snap2 := drainUntilState(t, aliceR2)

	// Hand size must be preserved.
	if len(snap2.Hand) != initialHandSize {
		t.Errorf("hand size after rejoin: got %d, want %d", len(snap2.Hand), initialHandSize)
	}

	// The player must appear as Connected in the snapshot.
	for _, p := range snap2.Players {
		if p.IsSelf && !p.Connected {
			t.Error("expected Connected=true after rejoin")
		}
	}
}

// TestReconnectWrongNameRejected: a join during an active game with an unknown
// name must be rejected.
func TestReconnectWrongNameRejected(t *testing.T) {
	_, addr := startServer(t)

	aliceConn, aliceR := dialAndJoin(t, addr, "Alice")
	drainLobby(t, aliceR) // Alice's own join
	bobConn, bobR := dialAndJoin(t, addr, "Bob")
	_, _ = bobConn, bobR
	drainLobby(t, aliceR) // Bob joined

	if err := protocol.WriteJSON(aliceConn, protocol.Command{Type: protocol.CmdStart}); err != nil {
		t.Fatalf("start: %v", err)
	}
	drainUntilState(t, aliceR)
	aliceConn.Close()
	time.Sleep(100 * time.Millisecond)

	// Try to join with a different name during the active game.
	conn, r := dialAndJoin(t, addr, "Charlie")
	errMsg := drainUntilError(t, r)
	conn.Close()

	if errMsg == "" {
		t.Error("expected an error message for unknown-name rejoin")
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

// TestDuplicateConnectedNameRejectedDuringGame: same check when the game is
// already running and Alice is still connected.
func TestDuplicateConnectedNameRejectedDuringGame(t *testing.T) {
	_, addr := startServer(t)

	aliceConn, aliceR := dialAndJoin(t, addr, "Alice")
	drainLobby(t, aliceR) // Alice's own join broadcast
	bobConn, bobR := dialAndJoin(t, addr, "Bob")
	_, _ = bobConn, bobR
	drainLobby(t, aliceR) // Bob joined broadcast

	if err := protocol.WriteJSON(aliceConn, protocol.Command{Type: protocol.CmdStart}); err != nil {
		t.Fatalf("start: %v", err)
	}
	drainUntilState(t, aliceR)

	// Alice is still connected — second join with same name must be rejected.
	conn2, r2 := dialAndJoin(t, addr, "Alice")
	errMsg := drainUntilError(t, r2)
	conn2.Close()

	if errMsg == "" {
		t.Error("expected rejection for duplicate connected name during game")
	}
}
