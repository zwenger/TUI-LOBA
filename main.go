// Loba — Argentine rummy, multiplayer over TCP.
// Usage:
//
//	loba host [--port 7777] [--name Alvaro] [--public]
//	loba join <host:port> [--name Pablo]
package main

import (
	"context"
	"flag"
	"fmt"
	"loba/internal/client"
	"loba/internal/server"
	"loba/internal/tunnel"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)


const defaultPort = "7777"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "host":
		runHost(args)
	case "join":
		runJoin(args)
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Loba — Argentine rummy, multiplayer over TCP.

Usage:
  loba host [--port 7777] [--name YourName] [--public]   Start a game server and join as host
  loba join <host:port> [--name YourName]                Join an existing game

Flags (host):
  --public   Open a public TCP tunnel via bore.pub so friends can join from
             anywhere. No account or token required.`)
}

// ─── Host ─────────────────────────────────────────────────────────────────────

func runHost(args []string) {
	fs := flag.NewFlagSet("host", flag.ExitOnError)
	port   := fs.String("port", defaultPort, "TCP port to listen on")
	name   := fs.String("name", "", "Your display name")
	public := fs.Bool("public", false, "Open a public bore.pub tunnel (no account or token required)")
	_ = fs.Parse(args)

	var opener tunnel.Opener = tunnel.NoopOpener{}
	if *public {
		opener = tunnel.BoreOpener{}
	}

	localAddr := "localhost:" + *port
	srv := server.New(*port, *name)

	// Start the local TCP server.
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	}()

	// Small delay to let the server start listening.
	time.Sleep(150 * time.Millisecond)

	// Open the tunnel (no-op when --public is not set).
	if *public {
		go runTunnel(srv, opener)
	}

	runClient(localAddr, *name)
}

// runTunnel opens a bore.pub tunnel and starts a second accept loop that feeds
// connections from the public internet into the same server. The host's own
// client always connects via localhost — it never goes through the tunnel.
func runTunnel(srv *server.Server, opener tunnel.Opener) {
	fmt.Fprintln(os.Stderr, "[tunnel] Opening public TCP tunnel via bore.pub…")

	ctx := context.Background()
	ln, err := opener.Open(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[tunnel] Failed to open tunnel: %v\n", err)
		fmt.Fprintln(os.Stderr, "[tunnel] Continuing in LAN-only mode — local connections still work.")
		return
	}
	if ln == nil {
		return // NoopOpener
	}
	defer ln.Close()

	publicAddr := ln.Addr().String()
	fmt.Printf("\n[tunnel] Public address: %s\n", publicAddr)
	fmt.Println("[tunnel] Share this address with friends. They run: loba join " + publicAddr)
	fmt.Println()

	// Propagate the address into the lobby so the TUI shows it.
	srv.SetPublicAddr(publicAddr)

	// Accept loop: hand tunnel connections to the server exactly like LAN ones.
	for {
		conn, err := ln.Accept()
		if err != nil {
			// Listener was closed (e.g. game over / process exit).
			return
		}
		go srv.HandleConn(conn)
	}
}


// ─── Join ─────────────────────────────────────────────────────────────────────

func runJoin(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: join requires <host:port>")
		usage()
		os.Exit(1)
	}

	addr := args[0]
	remaining := args[1:]

	fs := flag.NewFlagSet("join", flag.ExitOnError)
	name := fs.String("name", "", "Your display name")
	_ = fs.Parse(remaining)

	runClient(addr, *name)
}

// ─── Shared TUI runner ────────────────────────────────────────────────────────

func runClient(addr, name string) {
	m := client.New(addr, name)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}
