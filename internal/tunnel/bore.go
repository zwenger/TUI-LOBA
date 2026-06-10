// Package tunnel provides a small abstraction over TCP tunnel providers.
package tunnel

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	borego "github.com/FrontMage/bore-go"
	"github.com/google/uuid"
)

const (
	boreServer      = "bore.pub"
	boreConnTimeout = 10 * time.Second
)

// BoreOpener opens a TCP tunnel via bore.pub.
// No account, no token, and no configuration is required.
type BoreOpener struct{}

// NewTestBoreOpener returns a BoreOpener that dials host:port instead of
// bore.pub:7835. Intended for use in unit tests with an in-process fake server.
func NewTestBoreOpener(host string, port uint16) *testBoreOpener {
	return &testBoreOpener{host: host, port: port}
}

type testBoreOpener struct {
	host string
	port uint16
}

func (o *testBoreOpener) Open(ctx context.Context) (net.Listener, error) {
	type result struct {
		ln  *boreListener
		err error
	}
	ch := make(chan result, 1)
	go func() {
		ln, err := dialBoreAt(ctx, o.host, o.port)
		ch <- result{ln, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(boreConnTimeout):
		return nil, fmt.Errorf("timed out connecting to fake bore server")
	case r := <-ch:
		return r.ln, r.err
	}
}

// Open connects to bore.pub, negotiates a random remote port, and returns a
// net.Listener whose Accept delivers connections arriving through the tunnel.
// The listener's Addr().String() returns "bore.pub:<port>".
//
// If bore.pub cannot be reached within 10 s the error is returned and callers
// can fall back to LAN-only mode.
func (BoreOpener) Open(ctx context.Context) (net.Listener, error) {
	type result struct {
		ln  *boreListener
		err error
	}
	ch := make(chan result, 1)

	go func() {
		ln, err := dialBoreAt(ctx, boreServer, uint16(borego.ControlPort))
		ch <- result{ln, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(boreConnTimeout):
		return nil, fmt.Errorf("timed out connecting to %s after %s (bore.pub may be down)", boreServer, boreConnTimeout)
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("bore: %w", r.err)
		}
		return r.ln, nil
	}
}

// dialBoreAt opens a control connection to host:port, performs the Hello
// handshake, and returns a ready boreListener.
func dialBoreAt(ctx context.Context, host string, port uint16) (*boreListener, error) {
	rawConn, err := net.DialTimeout("tcp",
		fmt.Sprintf("%s:%d", host, port),
		boreConnTimeout)
	if err != nil {
		return nil, fmt.Errorf("dial control port: %w", err)
	}

	framed := borego.NewDelimited(rawConn)

	// Client sends Hello(0) to request a random port.
	if err := framed.SendJSON(map[string]interface{}{"Hello": uint16(0)}); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("send Hello: %w", err)
	}

	// Server responds with Hello(<assigned-port>) or Error.
	helloMsg, ok, err := framed.RecvServer(true)
	if err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("recv Hello: %w", err)
	}
	if !ok {
		rawConn.Close()
		return nil, fmt.Errorf("server closed connection during handshake")
	}
	switch helloMsg.Kind {
	case borego.ServerHello:
		// good
	case borego.ServerError:
		rawConn.Close()
		return nil, fmt.Errorf("server error: %s", helloMsg.ErrorText)
	default:
		rawConn.Close()
		return nil, fmt.Errorf("unexpected initial message: %s", helloMsg.Kind)
	}

	lnCtx, cancel := context.WithCancel(ctx)
	l := &boreListener{
		framed:     framed,
		serverHost: host,
		serverPort: port,
		// Addr always shows bore.pub as the user-visible host regardless of
		// which server host was dialled (e.g. a test override).
		addr:   boreAddr{s: fmt.Sprintf("%s:%d", boreServer, helloMsg.Port)},
		connCh: make(chan net.Conn, 32),
		errCh:  make(chan error, 1),
		cancel: cancel,
		closed: make(chan struct{}),
	}
	go l.controlLoop(lnCtx)
	return l, nil
}

// ─── boreAddr ─────────────────────────────────────────────────────────────────

// boreAddr implements net.Addr.
type boreAddr struct{ s string }

func (a boreAddr) Network() string { return "tcp" }
func (a boreAddr) String() string  { return a.s }

// ─── boreListener ─────────────────────────────────────────────────────────────

// boreListener wraps a bore control connection as a net.Listener.
// Accept blocks until the bore server signals an inbound connection.
type boreListener struct {
	framed     *borego.Delimited
	serverHost string // bore server host (bore.pub or test override)
	serverPort uint16 // bore control port
	addr       net.Addr
	connCh     chan net.Conn
	errCh      chan error
	once       sync.Once
	cancel     context.CancelFunc
	closed     chan struct{}
}

// controlLoop reads ServerConnection / ServerHeartbeat messages from the bore
// control channel and hands proxied net.Conns to Accept via connCh.
func (l *boreListener) controlLoop(ctx context.Context) {
	defer close(l.closed)

	for {
		// Check for cancellation before each blocking read.
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, ok, err := l.framed.RecvServer(false)
		if err != nil {
			select {
			case l.errCh <- err:
			default:
			}
			return
		}
		if !ok {
			return // clean EOF
		}

		switch msg.Kind {
		case borego.ServerHeartbeat:
			// no-op: the server sends these to keep the control conn alive.
		case borego.ServerConnection:
			id := msg.ID
			go func() {
				conn, err := acceptProxy(l.serverHost, l.serverPort, id)
				if err != nil {
					// Non-fatal: one missed connection. Log nothing — TUI is running.
					return
				}
				select {
				case l.connCh <- conn:
				case <-ctx.Done():
					conn.Close()
				}
			}()
		case borego.ServerError:
			select {
			case l.errCh <- fmt.Errorf("bore server: %s", msg.ErrorText):
			default:
			}
			return
		}
	}
}

// Accept blocks until a new tunnelled connection is available or the listener
// is closed.
func (l *boreListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connCh:
		return conn, nil
	case err := <-l.errCh:
		return nil, err
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

// Close shuts down the bore control loop.
func (l *boreListener) Close() error {
	l.once.Do(func() {
		l.cancel()
		_ = l.framed.Close()
	})
	return nil
}

// Addr returns the public bore.pub address, e.g. "bore.pub:12345".
func (l *boreListener) Addr() net.Addr {
	return l.addr
}

// ─── acceptProxy ──────────────────────────────────────────────────────────────

// acceptProxy opens a new TCP connection to host:port, sends Accept(id),
// and returns the raw net.Conn ready for bidirectional proxying.
//
// After Accept is sent the framing layer is done; the TCP socket becomes a
// plain byte pipe (same pattern as the Rust reference client).
func acceptProxy(host string, port uint16, id uuid.UUID) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp",
		fmt.Sprintf("%s:%d", host, port),
		boreConnTimeout)
	if err != nil {
		return nil, fmt.Errorf("dial bore for proxy: %w", err)
	}
	framed := borego.NewDelimited(conn)
	if err := framed.SendJSON(map[string]interface{}{"Accept": id.String()}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send Accept: %w", err)
	}
	// Clear deadline — the socket is now a raw pipe.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, err
	}
	// Drain any bytes the framer may have buffered before handing off.
	buffered, err := framed.BufferedData()
	if err != nil {
		conn.Close()
		return nil, err
	}
	if len(buffered) > 0 {
		return &prefixConn{Conn: conn, buf: buffered}, nil
	}
	return conn, nil
}

// ─── prefixConn ───────────────────────────────────────────────────────────────

// prefixConn is a net.Conn that replays a slice of buffered bytes before
// delegating reads to the underlying connection.
type prefixConn struct {
	net.Conn
	buf []byte
}

func (c *prefixConn) Read(b []byte) (int, error) {
	if len(c.buf) > 0 {
		n := copy(b, c.buf)
		c.buf = c.buf[n:]
		return n, nil
	}
	return c.Conn.Read(b)
}
