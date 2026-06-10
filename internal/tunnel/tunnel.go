// Package tunnel provides a small abstraction over TCP tunnel providers.
// Production code uses the bore.pub implementation; tests use the no-op stub.
package tunnel

import (
	"context"
	"net"
)

// Opener opens a TCP tunnel that forwards remote connections to a local address.
// The returned net.Listener accepts connections arriving through the tunnel;
// callers handle them exactly like connections from net.Listen.
type Opener interface {
	// Open starts the tunnel and returns a net.Listener whose Accept delivers
	// inbound connections from the public internet. The public address (e.g.
	// "bore.pub:12345") is available via Addr().String() on the listener.
	Open(ctx context.Context) (net.Listener, error)
}

// NoopOpener is a no-op implementation used in tests and when --public is not set.
type NoopOpener struct{}

// Open is a no-op: it returns nil, nil so callers can guard on a nil listener.
func (NoopOpener) Open(_ context.Context) (net.Listener, error) {
	return nil, nil
}
