package server

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/zwenger/TUI-LOBA/internal/protocol"
)

// recvUntil reads envelopes (with a deadline) until pred matches one.
func recvUntil(t *testing.T, conn net.Conn, r *bufio.Reader, what string, pred func(protocol.Envelope) bool) protocol.Envelope {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	for {
		var env protocol.Envelope
		if err := protocol.ReadJSON(r, &env); err != nil {
			t.Fatalf("waiting for %s: %v", what, err)
		}
		if pred(env) {
			return env
		}
	}
}

// waitLobbyPlayers blocks until the client receives a lobby state listing n players.
// Joins are processed asynchronously, so tests must sync on this before chatting.
func waitLobbyPlayers(t *testing.T, conn net.Conn, r *bufio.Reader, n int) {
	t.Helper()
	recvUntil(t, conn, r, "lobby state", func(env protocol.Envelope) bool {
		if env.Type != "lobby" {
			return false
		}
		var ls protocol.LobbyState
		return json.Unmarshal(env.Payload, &ls) == nil && len(ls.Players) == n
	})
}

// recvChatText blocks until the client receives a chat/message event and returns its text.
func recvChatText(t *testing.T, conn net.Conn, r *bufio.Reader) string {
	t.Helper()
	env := recvUntil(t, conn, r, "chat message", func(env protocol.Envelope) bool {
		return env.Type == protocol.EvtMessage
	})
	var e map[string]string
	if err := json.Unmarshal(env.Payload, &e); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	return e["text"]
}

// TestLobbyChatBroadcast verifies that a chat command sent before the game
// starts is broadcast to every connected client with the sender's name, and
// that multiline text survives the round trip.
func TestLobbyChatBroadcast(t *testing.T) {
	_, addr := startServer(t)

	aliceConn, aliceR := dialAndJoin(t, addr, "Alice")
	bobConn, bobR := dialAndJoin(t, addr, "Bob")

	// Sync: both clients must see the 2-player lobby before Alice chats,
	// otherwise the chat may be broadcast before Bob is registered.
	waitLobbyPlayers(t, aliceConn, aliceR, 2)
	waitLobbyPlayers(t, bobConn, bobR, 2)

	art := "hola\n (\\_/)\n (o.o)"
	if err := protocol.WriteJSON(aliceConn, protocol.Command{
		Type: protocol.CmdChat,
		Text: art,
	}); err != nil {
		t.Fatalf("chat write: %v", err)
	}

	for _, tc := range []struct {
		who string
		got string
	}{
		{"alice", recvChatText(t, aliceConn, aliceR)},
		{"bob", recvChatText(t, bobConn, bobR)},
	} {
		if !strings.HasPrefix(tc.got, "[Alice] ") {
			t.Errorf("%s: message %q does not start with %q", tc.who, tc.got, "[Alice] ")
		}
		if !strings.Contains(tc.got, "(o.o)") {
			t.Errorf("%s: multiline content lost: %q", tc.who, tc.got)
		}
	}
}

// TestLobbyChatIgnoresEmpty verifies that whitespace-only chat messages are
// dropped and not broadcast.
func TestLobbyChatIgnoresEmpty(t *testing.T) {
	_, addr := startServer(t)

	aliceConn, aliceR := dialAndJoin(t, addr, "Alice")
	waitLobbyPlayers(t, aliceConn, aliceR, 1)

	if err := protocol.WriteJSON(aliceConn, protocol.Command{
		Type: protocol.CmdChat,
		Text: "   \n\n  ",
	}); err != nil {
		t.Fatalf("chat write: %v", err)
	}
	// Follow with a real message; the first EvtMessage received must be this
	// one, proving the empty message was never broadcast.
	if err := protocol.WriteJSON(aliceConn, protocol.Command{
		Type: protocol.CmdChat,
		Text: "real",
	}); err != nil {
		t.Fatalf("chat write: %v", err)
	}

	if got := recvChatText(t, aliceConn, aliceR); got != "[Alice] real" {
		t.Errorf("first broadcast message = %q, want %q", got, "[Alice] real")
	}
}
