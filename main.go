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
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)


const defaultPort = "7777"

// repoURL is the canonical clone URL shown in the share block.
const repoURL = "https://github.com/zwenger/TUI-LOBA"

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

	m := client.New(localAddr, *name)
	p := tea.NewProgram(m, tea.WithAltScreen())

	// Open the tunnel (no-op when --public is not set).
	// progCh delivers the *Program to the tunnel goroutine once it exists,
	// so p.Println can be called safely after the TUI is running.
	if *public {
		progCh := make(chan *tea.Program, 1)
		progCh <- p
		go runTunnel(srv, opener, progCh)
	}

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}

// runTunnel opens a bore.pub tunnel and starts a second accept loop that feeds
// connections from the public internet into the same server. The host's own
// client always connects via localhost — it never goes through the tunnel.
//
// progCh must contain the *tea.Program before this function returns so that
// p.Println can be called safely — that is the Bubbletea-supported way to
// print above a running viewport into the terminal's scrollback.
func runTunnel(srv *server.Server, opener tunnel.Opener, progCh <-chan *tea.Program) {
	fmt.Fprintln(os.Stderr, "[tunnel] Abriendo túnel público via bore.pub…")

	ctx := context.Background()
	ln, err := opener.Open(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[tunnel] No se pudo abrir el túnel: %v\n", err)
		fmt.Fprintln(os.Stderr, "[tunnel] Continuando en modo LAN — las conexiones locales siguen funcionando.")
		return
	}
	if ln == nil {
		return // NoopOpener
	}
	defer ln.Close()

	publicAddr := ln.Addr().String()

	// Propagate the address into the lobby so the TUI renders it.
	srv.SetPublicAddr(publicAddr)

	// Receive the running program and emit the share block into scrollback.
	// p.Println is the supported Bubbletea mechanism: it queues a printLineMessage
	// that the renderer flushes above the TUI viewport without corrupting the UI.
	prog := <-progCh
	for _, line := range buildShareLines(publicAddr) {
		prog.Println(line)
	}

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

// buildShareLines returns the ready-to-copy invite block as a slice of strings,
// one per line. Callers emit each line via p.Println so they land in the
// terminal scrollback above the TUI viewport.
func buildShareLines(addr string) []string {
	sep := strings.Repeat("─", 60)
	join := func(launcher, extra string) string {
		return fmt.Sprintf("  git clone %s && cd TUI-LOBA && %s join %s --name TU_NOMBRE%s",
			repoURL, launcher, addr, extra)
	}
	rejoin := func(launcher, extra string) string {
		return fmt.Sprintf("  (ya clonado: cd TUI-LOBA && %s join %s --name TU_NOMBRE%s)",
			launcher, addr, extra)
	}
	joinPS := func() string {
		return fmt.Sprintf("  git clone %s; cd TUI-LOBA; .\\play.ps1 join %s --name TU_NOMBRE",
			repoURL, addr)
	}
	rejoinPS := func() string {
		return fmt.Sprintf("  (ya clonado: cd TUI-LOBA; .\\play.ps1 join %s --name TU_NOMBRE)", addr)
	}
	return []string{
		"",
		sep,
		"  Pasale esto a tus amigos:",
		"",
		"  Linux / macOS / Windows (Git Bash):",
		join("./play.sh", ""),
		rejoin("./play.sh", ""),
		"",
		"  Windows (PowerShell):",
		joinPS(),
		rejoinPS(),
		sep,
		"",
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

	m := client.New(addr, *name)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}
