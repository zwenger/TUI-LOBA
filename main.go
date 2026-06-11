// Loba — Argentine rummy, multiplayer over TCP.
// Usage:
//
//	loba host [--port 7777] [--name Alvaro] [--public]
//	loba join <host:port> [--name Pablo]
//	loba           (no arguments → interactive start menu)
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"github.com/zwenger/TUI-LOBA/internal/client"
	"github.com/zwenger/TUI-LOBA/internal/server"
	"github.com/zwenger/TUI-LOBA/internal/tunnel"
	"github.com/zwenger/TUI-LOBA/internal/update"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const defaultPort = "7777"

// repoURL is the canonical clone URL shown in the share block.
const repoURL = "https://github.com/zwenger/TUI-LOBA"

// version is injected at build time via -ldflags "-X main.version=<tag>".
// Falls back to "dev" for local / untagged builds.
var version = "dev"

// ─── Startup decision ─────────────────────────────────────────────────────────

func main() {
	if len(os.Args) < 2 {
		runMenu()
		return
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "--version", "-version", "version":
		fmt.Println("loba", version)
	case "host":
		printUpdateNoticeIfNewer()
		runHost(args)
	case "join":
		printUpdateNoticeIfNewer()
		runJoin(args)
	case "update":
		runUpdate()
	default:
		usage()
		windowsPause()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Loba — Argentine rummy, multiplayer over TCP.

Usage:
  loba host [--port 7777] [--name YourName] [--public]   Start a game server and join as host
  loba join <host:port> [--name YourName]                Join an existing game
  loba update                                            Update loba to the latest release
  loba                                                   Interactive start menu

Flags (host):
  --public   Open a public TCP tunnel via bore.pub so friends can join from
             anywhere. No account or token required.`)
}

// ─── Version update notice ────────────────────────────────────────────────────

// updateNoticeCh receives a newer version string when the background check finishes.
var updateNoticeCh = make(chan string, 1)

// checkUpdateBackground fires a goroutine that checks for a newer version and
// sends it to updateNoticeCh. Safe to call from the menu init path.
func checkUpdateBackground() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		latest, err := update.LatestVersion(ctx)
		if err != nil {
			return
		}
		if update.IsNewer(version, latest) {
			select {
			case updateNoticeCh <- latest:
			default:
			}
		}
	}()
}

// printUpdateNoticeIfNewer performs a synchronous (2 s timeout) version check
// and prints a notice to stderr when a newer version is available.
// Used by the host/join CLI paths before the TUI starts.
// Silent on any error or timeout.
func printUpdateNoticeIfNewer() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	latest, err := update.LatestVersion(ctx)
	if err != nil {
		return
	}
	if update.IsNewer(version, latest) {
		fmt.Fprintf(os.Stderr, "Hay una versión nueva (v%s) — actualizá con: loba update\n", latest)
	}
}

// ─── loba update subcommand ───────────────────────────────────────────────────

func runUpdate() {
	fmt.Printf("loba %s\n", version)

	if version == "dev" {
		fmt.Fprintln(os.Stderr, "instalación desde código — actualizá con play.sh o git pull")
		os.Exit(0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Println("Verificando última versión...")
	rel, err := update.LatestRelease(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error consultando GitHub: %v\n", err)
		os.Exit(1)
	}

	if !update.IsNewer(version, rel.TagName) {
		fmt.Printf("Ya estás en la última versión (v%s).\n", rel.TagName)
		os.Exit(0)
	}

	exePath, err := update.ResolveExePath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "No se pudo determinar la ruta del ejecutable: %v\n", err)
		os.Exit(1)
	}

	// Check write permission early — provide actionable error.
	dir := filepath.Dir(exePath)
	if err := checkWritable(dir); err != nil {
		fmt.Fprintf(os.Stderr, "Sin permiso de escritura en %s.\n", dir)
		if strings.HasPrefix(dir, "/usr/") || strings.HasPrefix(dir, "/opt/") {
			fmt.Fprintln(os.Stderr, "Intentá ejecutar con: sudo loba update")
		}
		os.Exit(1)
	}

	oldVersion := version
	err = update.SelfUpdate(ctx, rel, version, exePath, "", func(format string, args ...any) {
		fmt.Printf(format+"\n", args...)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error actualizando: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("loba actualizado: v%s → v%s\n", oldVersion, rel.TagName)
}

// checkWritable returns an error if dir is not writable by the current process.
func checkWritable(dir string) error {
	tmp, err := os.CreateTemp(dir, ".loba-write-check-*")
	if err != nil {
		return err
	}
	_ = tmp.Close()
	_ = os.Remove(tmp.Name())
	return nil
}

// windowsPause waits for Enter on Windows so a double-clicked console window
// stays open long enough for the user to read the message. No-op on other OSes.
func windowsPause() {
	if runtime.GOOS != "windows" {
		return
	}
	fmt.Fprintln(os.Stderr, "\nPresioná Enter para salir...")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
}

// ─── Interactive menu (no-args path) ─────────────────────────────────────────

// menuState holds the server and tunnel state created during the menu bootstrap
// so the tunnel goroutine can be started after the program reference is available.
type menuState struct {
	mu     sync.Mutex
	srv    *server.Server
	public bool
	progCh chan *tea.Program // non-nil when public=true
}

func runMenu() {
	// Start background version check; notice will be shown in the TUI footer.
	checkUpdateBackground()

	ms := &menuState{}

	hostFn := func(port string, public bool, progCh chan<- *tea.Program) (string, error) {
		srv, localAddr, err := startServer(port, "")
		if err != nil {
			return "", err
		}
		ms.mu.Lock()
		ms.srv = srv
		ms.public = public
		if public {
			// Allocate the channel; it will be filled with the *tea.Program
			// by the goroutine started below once p is known.
			ch := make(chan *tea.Program, 1)
			ms.progCh = ch
		}
		ms.mu.Unlock()
		return localAddr, nil
	}

	joinFn := func(addr string) (string, error) {
		return normaliseAddr(addr)
	}

	m := client.NewMenu(hostFn, joinFn, version)
	p := tea.NewProgram(m, tea.WithAltScreen())

	// Forward update notice to the running TUI when the background check finishes.
	go func() {
		select {
		case latest := <-updateNoticeCh:
			p.Send(client.UpdateNoticeMsg{Version: latest})
		case <-time.After(5 * time.Second):
			// Don't keep this goroutine alive indefinitely if the check is slow.
		}
	}()

	// This goroutine waits until the host bootstrap has set ms.srv, then
	// launches the tunnel goroutine with p injected into the channel.
	go func() {
		// Poll until srv is set (bootstrap happens inside a tea.Cmd goroutine).
		for {
			ms.mu.Lock()
			srv := ms.srv
			public := ms.public
			progCh := ms.progCh
			ms.mu.Unlock()
			if srv != nil && public && progCh != nil {
				progCh <- p
				runTunnel(srv, tunnel.BoreOpener{}, progCh)
				return
			}
			if srv != nil && !public {
				// Host without tunnel — nothing more to do.
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		windowsPause()
		os.Exit(1)
	}
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

	srv, localAddr, err := startServer(*port, *name)
	if err != nil {
		showFatalError(err)
		return
	}

	m := client.New(localAddr, *name, version)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if *public {
		progCh := make(chan *tea.Program, 1)
		progCh <- p
		go runTunnel(srv, opener, progCh)
	}

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		windowsPause()
		os.Exit(1)
	}
}

// ─── Join ─────────────────────────────────────────────────────────────────────

func runJoin(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: join requires <host:port>")
		usage()
		windowsPause()
		os.Exit(1)
	}

	addr := args[0]
	remaining := args[1:]

	fs := flag.NewFlagSet("join", flag.ExitOnError)
	name := fs.String("name", "", "Your display name")
	_ = fs.Parse(remaining)

	m := client.New(addr, *name, version)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		windowsPause()
		os.Exit(1)
	}
}

// ─── Shared bootstrap helpers ─────────────────────────────────────────────────

// startServer creates and starts a TCP server on the given port.
// Returns the server instance, local address, and any startup error.
// It waits up to 200 ms for the listener to bind and checks for early failures.
func startServer(port, name string) (*server.Server, string, error) {
	srv := server.New(port, name)
	srv.SetVersion(version)
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			errCh <- err
		}
	}()
	// Give the server time to bind; check for early failure.
	select {
	case err := <-errCh:
		msg := fmt.Sprintf("no se pudo iniciar el servidor en el puerto %s: %v", port, err)
		if hint := diagnosePortOwner(port); hint != "" {
			msg += "\n\n" + hint
		}
		return nil, "", fmt.Errorf("%s", msg)
	case <-time.After(200 * time.Millisecond):
	}
	return srv, "localhost:" + port, nil
}

// diagnosePortOwner inspects which process is listening on the given TCP port
// and returns a human-readable hint, or "" when nothing could be determined.
// Uses lsof, so it is a no-op on Windows.
func diagnosePortOwner(port string) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	out, err := exec.Command("lsof", "-ti", "tcp:"+port, "-sTCP:LISTEN").Output()
	if err != nil {
		return ""
	}
	pid := strings.TrimSpace(string(out))
	if pid == "" {
		return ""
	}
	if i := strings.IndexByte(pid, '\n'); i >= 0 {
		pid = pid[:i] // multiple PIDs: report the first listener
	}

	comm := ""
	if cout, cerr := exec.Command("ps", "-o", "comm=", "-p", pid).Output(); cerr == nil {
		// ps reports the full executable path on macOS; show just the name.
		comm = filepath.Base(strings.TrimSpace(string(cout)))
	}

	if strings.Contains(comm, "loba") {
		return fmt.Sprintf(
			"Hay una instancia previa de Loba corriendo (PID %s) que quedó con el puerto.\n"+
				"Cerrala con:  kill %s", pid, pid)
	}
	if comm != "" {
		return fmt.Sprintf("El puerto %s está ocupado por %q (PID %s).", port, comm, pid)
	}
	return fmt.Sprintf("El puerto %s está ocupado por el proceso PID %s.", port, pid)
}

// normaliseAddr returns addr unchanged if it contains a colon (host:port),
// otherwise appends the default port.
func normaliseAddr(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", fmt.Errorf("la dirección no puede estar vacía")
	}
	if !strings.Contains(addr, ":") {
		addr = addr + ":" + defaultPort
	}
	return addr, nil
}

// showFatalError launches a minimal TUI that shows err and waits for Enter.
func showFatalError(err error) {
	m := client.NewFatalError(err.Error())
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, tuiErr := p.Run(); tuiErr != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		windowsPause()
	}
}

// ─── Tunnel ───────────────────────────────────────────────────────────────────

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
