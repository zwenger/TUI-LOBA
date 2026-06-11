// Package update provides self-update functionality for loba.
// It queries the GitHub Releases API, downloads the correct asset for the
// current OS/arch, verifies the SHA-256 checksum, and atomically replaces
// the running executable.
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.github.com/repos/zwenger/TUI-LOBA"

// Release holds the parsed fields we need from the GitHub API response.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset is a single release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// LatestVersion queries the GitHub releases API and returns the latest version
// string (e.g. "1.2.3", leading "v" stripped). Returns an error on any network
// or parse failure — callers must treat errors as "unknown, don't update".
func LatestVersion(ctx context.Context) (string, error) {
	return latestVersionFromBase(ctx, defaultBaseURL)
}

func latestVersionFromBase(ctx context.Context, baseURL string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "loba-updater/1")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	return stripV(rel.TagName), nil
}

// LatestRelease returns the full Release object for the latest GitHub release.
func LatestRelease(ctx context.Context) (*Release, error) {
	return latestReleaseFromBase(ctx, defaultBaseURL)
}

func latestReleaseFromBase(ctx context.Context, baseURL string) (*Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "loba-updater/1")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	rel.TagName = stripV(rel.TagName)
	return &rel, nil
}

// IsNewer returns true if latest is a higher semver than current.
// "dev" current is NEVER considered outdated (local/play.sh builds).
func IsNewer(current, latest string) bool {
	current = stripV(current)
	latest = stripV(latest)
	if current == "dev" || current == "" {
		return false
	}
	if latest == "" || latest == "dev" {
		return false
	}
	return semverGT(latest, current)
}

// stripV removes a leading "v" from a version string.
func stripV(v string) string {
	return strings.TrimPrefix(v, "v")
}

// semverGT returns true if a > b using numeric segment comparison.
// Segments beyond what either version provides are treated as 0.
func semverGT(a, b string) bool {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	n := len(aParts)
	if len(bParts) > n {
		n = len(bParts)
	}
	for i := 0; i < n; i++ {
		av := segmentInt(aParts, i)
		bv := segmentInt(bParts, i)
		if av != bv {
			return av > bv
		}
	}
	return false
}

func segmentInt(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	// Strip any pre-release suffix (e.g. "1-beta" → 1).
	s := parts[i]
	if idx := strings.IndexAny(s, "-+"); idx >= 0 {
		s = s[:idx]
	}
	n, _ := strconv.Atoi(s)
	return n
}

// AssetName returns the expected archive asset name for the given OS, arch, and
// version (without leading v). This matches the goreleaser name_template.
func AssetName(version, goos, goarch string) string {
	base := fmt.Sprintf("loba_%s_%s_%s", version, goos, goarch)
	if goos == "windows" {
		return base + ".zip"
	}
	return base + ".tar.gz"
}

// SelfUpdate downloads the correct release asset, verifies its checksum, and
// replaces the running executable atomically.
// exePath must be the resolved (EvalSymlinks) path to the current executable.
// baseURL is the GitHub API base (injectable for tests).
// logf is called for progress messages.
func SelfUpdate(ctx context.Context, rel *Release, currentVersion, exePath, baseURL string, logf func(string, ...any)) error {
	version := rel.TagName // already stripped of leading v
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	assetName := AssetName(version, goos, goarch)
	checksumsName := "checksums.txt"

	// Find download URLs from the release assets.
	assetURL := ""
	checksumsURL := ""
	for _, a := range rel.Assets {
		switch a.Name {
		case assetName:
			assetURL = a.BrowserDownloadURL
		case checksumsName:
			checksumsURL = a.BrowserDownloadURL
		}
	}
	if assetURL == "" {
		return fmt.Errorf("no se encontró el asset %q en la release %s", assetName, version)
	}
	if checksumsURL == "" {
		return fmt.Errorf("no se encontró checksums.txt en la release %s", version)
	}

	logf("Descargando %s...", assetName)
	assetData, err := downloadBytes(ctx, assetURL)
	if err != nil {
		return fmt.Errorf("error descargando asset: %w", err)
	}

	logf("Descargando checksums.txt...")
	checksumsData, err := downloadBytes(ctx, checksumsURL)
	if err != nil {
		return fmt.Errorf("error descargando checksums: %w", err)
	}

	// Verify SHA-256.
	logf("Verificando integridad...")
	if err := verifyChecksum(assetData, assetName, checksumsData); err != nil {
		return fmt.Errorf("verificación fallida: %w", err)
	}

	// Extract the binary from the archive.
	logf("Extrayendo binario...")
	newBinary, err := extractBinary(assetData, assetName)
	if err != nil {
		return fmt.Errorf("error extrayendo binario: %w", err)
	}

	// Atomic replace.
	logf("Instalando nueva versión...")
	if err := atomicReplace(exePath, newBinary); err != nil {
		return fmt.Errorf("error reemplazando ejecutable: %w", err)
	}

	return nil
}

// downloadBytes fetches a URL and returns the body as bytes.
func downloadBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "loba-updater/1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d para %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// verifyChecksum checks that the SHA-256 of data matches the entry for assetName
// in the goreleaser-style checksums.txt file.
func verifyChecksum(data []byte, assetName string, checksumsData []byte) error {
	want, err := parseChecksum(checksumsData, assetName)
	if err != nil {
		return err
	}
	got := sha256.Sum256(data)
	gotHex := fmt.Sprintf("%x", got)
	if gotHex != want {
		return fmt.Errorf("checksum mismatch para %s: got %s want %s", assetName, gotHex, want)
	}
	return nil
}

// parseChecksum finds the hex checksum for fileName in goreleaser checksums.txt.
// Format per line: "<hex>  <filename>" (two spaces).
func parseChecksum(data []byte, fileName string) (string, error) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Split on two spaces (goreleaser format) or one.
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		if parts[1] == fileName {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("no se encontró checksum para %q en checksums.txt", fileName)
}

// extractBinary extracts the "loba" (or "loba.exe") binary from the archive.
func extractBinary(data []byte, assetName string) ([]byte, error) {
	if strings.HasSuffix(assetName, ".zip") {
		return extractFromZip(data)
	}
	return extractFromTarGz(data)
}

func extractFromTarGz(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		base := filepath.Base(hdr.Name)
		if base == "loba" || base == "loba.exe" {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("no se encontró el binario 'loba' en el archivo tar.gz")
}

func extractFromZip(data []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("zip: %w", err)
	}
	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		if base == "loba" || base == "loba.exe" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("no se encontró el binario 'loba' en el archivo zip")
}

// atomicReplace writes newBinary to a temp file in the same directory as exePath,
// then performs an atomic rename. On Windows it uses the old-file dance because
// Windows doesn't allow renaming over a running executable.
func atomicReplace(exePath string, newBinary []byte) error {
	dir := filepath.Dir(exePath)

	// Remove stale .old file on Windows if it exists (from a previous failed update).
	if runtime.GOOS == "windows" {
		_ = os.Remove(exePath + ".old")
	}

	tmp, err := os.CreateTemp(dir, ".loba-update-*")
	if err != nil {
		return fmt.Errorf("no se pudo crear archivo temporal en %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	// Ensure we clean up on failure.
	defer func() {
		// If the rename succeeded, tmp is gone; ignore the error.
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(newBinary); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("escritura del binario: %w", err)
	}
	if err := tmp.Chmod(0755); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return renameExe(tmpName, exePath)
}

// renameExe is the platform-specific rename step. Extracted so it can be
// unit-tested independently.
func renameExe(tmpPath, exePath string) error {
	if runtime.GOOS == "windows" {
		return windowsRename(tmpPath, exePath)
	}
	return os.Rename(tmpPath, exePath)
}

// windowsRename implements the Windows rename dance:
// rename(exe → exe.old) then rename(tmp → exe); attempt to remove .old.
// Exported for testing on non-Windows.
func windowsRename(tmpPath, exePath string) error {
	oldPath := exePath + ".old"
	if err := os.Rename(exePath, oldPath); err != nil {
		return fmt.Errorf("no se pudo renombrar el ejecutable actual: %w", err)
	}
	if err := os.Rename(tmpPath, exePath); err != nil {
		// Try to restore the original; ignore secondary error.
		_ = os.Rename(oldPath, exePath)
		return fmt.Errorf("no se pudo instalar el nuevo ejecutable: %w", err)
	}
	// Best effort: remove the .old file. Ignore failure — it'll be cleaned up
	// at the next update.
	_ = os.Remove(oldPath)
	return nil
}

// ResolveExePath returns the real path of the current executable, following
// symlinks. Returns an error if os.Executable() or EvalSymlinks fails.
func ResolveExePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
