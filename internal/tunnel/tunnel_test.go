package tunnel_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/zwenger/TUI-LOBA/internal/tunnel"

	"github.com/google/uuid"
)

// ─── NoopOpener ───────────────────────────────────────────────────────────────

// TestNoopOpenerReturnsNil verifies that NoopOpener.Open returns (nil, nil)
// so callers can safely guard on a nil listener.
func TestNoopOpenerReturnsNil(t *testing.T) {
	var o tunnel.NoopOpener
	ln, err := o.Open(context.Background())
	if err != nil {
		t.Fatalf("NoopOpener.Open() returned unexpected error: %v", err)
	}
	if ln != nil {
		_ = ln.Close()
		t.Fatal("NoopOpener.Open() returned non-nil listener, want nil")
	}
}

// ─── Bore framing ─────────────────────────────────────────────────────────────

// TestBoreFramingRoundTrip verifies null-delimited JSON framing encode/decode.
func TestBoreFramingRoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		message map[string]interface{}
	}{
		{"Hello", map[string]interface{}{"Hello": uint16(0)}},
		{"Accept", map[string]interface{}{"Accept": uuid.New().String()}},
		{"Authenticate", map[string]interface{}{"Authenticate": "tok"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(tc.message)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			// Simulate the wire: JSON followed by null byte.
			wire := append(payload, 0)

			// Parse back: read until null byte.
			r := bufio.NewReader(bytes.NewReader(wire))
			frame, err := r.ReadBytes(0)
			if err != nil && err != io.EOF {
				t.Fatalf("ReadBytes: %v", err)
			}
			frame = bytes.TrimSuffix(frame, []byte{0})
			if !bytes.Equal(frame, payload) {
				t.Fatalf("round-trip mismatch: got %s want %s", frame, payload)
			}
		})
	}
}

// ─── Fake bore server ─────────────────────────────────────────────────────────

// fakeBoreServer spins up an in-process TCP listener that speaks the bore
// protocol:
//  1. Waits for client Hello(0)
//  2. Responds with Hello(<assignedPort>)
//  3. Sends a Connection(<id>) message
//  4. On a second connection sending Accept(<id>), pipes bytes back and forth
//
// The server signals readiness via the ready channel and the assigned port via
// assignedPort.
type fakeBoreServer struct {
	ln           net.Listener
	assignedPort uint16
	connID       uuid.UUID
	ready        chan struct{}
	t            *testing.T
}

func newFakeBoreServer(t *testing.T) *fakeBoreServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake bore: listen: %v", err)
	}
	s := &fakeBoreServer{
		ln:           ln,
		assignedPort: 19999,
		connID:       uuid.New(),
		ready:        make(chan struct{}),
		t:            t,
	}
	return s
}

func (s *fakeBoreServer) Addr() string { return s.ln.Addr().String() }

// run serves a single control connection then a single proxy connection.
// It must be called in a goroutine.
func (s *fakeBoreServer) run() {
	close(s.ready)

	// ── control connection ──────────────────────────────────────────────────
	ctrlConn, err := s.ln.Accept()
	if err != nil {
		s.t.Errorf("fake bore: accept control: %v", err)
		return
	}
	defer ctrlConn.Close()

	// Receive Hello from client.
	r := bufio.NewReader(ctrlConn)
	frame, err := r.ReadBytes(0)
	if err != nil {
		s.t.Errorf("fake bore: read Hello: %v", err)
		return
	}
	frame = bytes.TrimSuffix(frame, []byte{0})
	var hello map[string]interface{}
	if err := json.Unmarshal(frame, &hello); err != nil {
		s.t.Errorf("fake bore: unmarshal Hello: %v", err)
		return
	}
	if _, ok := hello["Hello"]; !ok {
		s.t.Errorf("fake bore: expected Hello message, got %s", frame)
		return
	}

	// Send Hello response with assigned port.
	resp, _ := json.Marshal(map[string]interface{}{"Hello": s.assignedPort})
	ctrlConn.Write(append(resp, 0)) //nolint:errcheck

	// Send Connection message.
	connMsg, _ := json.Marshal(map[string]interface{}{"Connection": s.connID.String()})
	ctrlConn.Write(append(connMsg, 0)) //nolint:errcheck

	// ── proxy connection ────────────────────────────────────────────────────
	proxyConn, err := s.ln.Accept()
	if err != nil {
		s.t.Errorf("fake bore: accept proxy: %v", err)
		return
	}

	// Receive Accept from client.
	pr := bufio.NewReader(proxyConn)
	proxyFrame, err := pr.ReadBytes(0)
	if err != nil {
		proxyConn.Close()
		s.t.Errorf("fake bore: read Accept: %v", err)
		return
	}
	proxyFrame = bytes.TrimSuffix(proxyFrame, []byte{0})
	var accept map[string]interface{}
	if err := json.Unmarshal(proxyFrame, &accept); err != nil {
		proxyConn.Close()
		s.t.Errorf("fake bore: unmarshal Accept: %v", err)
		return
	}
	if _, ok := accept["Accept"]; !ok {
		proxyConn.Close()
		s.t.Errorf("fake bore: expected Accept message, got %s", proxyFrame)
		return
	}

	// Echo bytes from client back to client. Use a pipe: read from buffered
	// reader (which may have leftover data from the framer) and write back.
	// Block until the proxy connection is closed so the deferred Close on
	// ctrlConn does not race.
	go func() {
		defer proxyConn.Close()
		// Replay any data the bufio.Reader pulled ahead before handing off.
		buf := make([]byte, 4096)
		for {
			n, err := pr.Read(buf)
			if n > 0 {
				proxyConn.Write(buf[:n]) //nolint:errcheck
			}
			if err != nil {
				return
			}
		}
	}()

	// Keep the control connection open long enough for the proxy exchange to
	// complete. The test closes the proxy conn which will unblock this select.
	<-time.After(5 * time.Second)
}

// ─── BoreOpener integration test ──────────────────────────────────────────────

// TestBoreOpenerFakeTunnel runs a full end-to-end round trip against an
// in-process fake bore server. It verifies:
//   - BoreOpener connects and completes the Hello handshake
//   - Addr() returns "bore.pub:<assignedPort>"
//   - Accept() returns a conn when the fake server emits a Connection message
//   - Bytes sent through the proxied conn are echoed back
func TestBoreOpenerFakeTunnel(t *testing.T) {
	srv := newFakeBoreServer(t)
	go srv.run()
	<-srv.ready

	// Override the bore server address and port for this test by patching via
	// a test-only opener that points at the fake server.
	fakeHost, fakePortStr, _ := net.SplitHostPort(srv.Addr())
	var fakePort int
	fmt.Sscanf(fakePortStr, "%d", &fakePort)

	opener := tunnel.NewTestBoreOpener(fakeHost, uint16(fakePort))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ln, err := opener.Open(ctx)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer ln.Close()

	// Addr should be "bore.pub:<assignedPort>".
	wantAddr := fmt.Sprintf("bore.pub:%d", srv.assignedPort)
	if ln.Addr().String() != wantAddr {
		t.Fatalf("Addr: got %s want %s", ln.Addr().String(), wantAddr)
	}

	// Accept must return the proxied connection.
	connCh := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		connCh <- conn
	}()

	select {
	case conn := <-connCh:
		defer conn.Close()
		// Write a probe and expect an echo.
		probe := []byte("hello bore\n")
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := conn.Write(probe); err != nil {
			t.Fatalf("Write: %v", err)
		}
		got := make([]byte, len(probe))
		if _, err := io.ReadFull(conn, got); err != nil {
			t.Fatalf("ReadFull: %v", err)
		}
		if !bytes.Equal(got, probe) {
			t.Fatalf("echo mismatch: got %q want %q", got, probe)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Accept timed out")
	}
}
