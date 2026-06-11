package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// ─── IsNewer tests ─────────────────────────────────────────────────────────────

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    bool
	}{
		// dev is never outdated
		{"dev", "1.0.0", false},
		{"dev", "2.3.4", false},
		// equal versions
		{"1.0.0", "1.0.0", false},
		{"1.2.3", "1.2.3", false},
		// newer versions
		{"1.0.0", "1.0.1", true},
		{"1.0.0", "1.1.0", true},
		{"1.0.0", "2.0.0", true},
		{"0.9.9", "1.0.0", true},
		// multi-digit segments
		{"1.9.0", "1.10.0", true},
		{"1.9.99", "1.10.0", true},
		// older versions
		{"2.0.0", "1.9.9", false},
		{"1.1.0", "1.0.9", false},
		// v prefix stripped
		{"v1.0.0", "v1.0.1", true},
		{"1.0.0", "v1.0.1", true},
		{"v1.0.0", "1.0.1", true},
		// empty strings
		{"1.0.0", "", false},
		{"", "1.0.0", false},
		// latest is dev
		{"1.0.0", "dev", false},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s→%s", tc.current, tc.latest), func(t *testing.T) {
			got := IsNewer(tc.current, tc.latest)
			if got != tc.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
			}
		})
	}
}

// ─── LatestVersion + SelfUpdate httptest fixtures ─────────────────────────────

// buildFakeTarGz builds a tar.gz containing a fake binary at the given path.
func buildFakeTarGz(t *testing.T, binaryName, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	data := []byte(content)
	hdr := &tar.Header{
		Name: binaryName,
		Mode: 0755,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar WriteHeader: %v", err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatalf("tar Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gz Close: %v", err)
	}
	return buf.Bytes()
}

// buildChecksums generates a checksums.txt for the given name→data pairs.
func buildChecksums(entries map[string][]byte) []byte {
	var buf bytes.Buffer
	for name, data := range entries {
		sum := sha256.Sum256(data)
		fmt.Fprintf(&buf, "%x  %s\n", sum, name)
	}
	return buf.Bytes()
}

// fakeServer starts an httptest server serving a fake release with one
// tar.gz asset containing a known binary and a matching checksums.txt.
// Returns the server, the release JSON URL base, and the expected binary content.
func fakeServer(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()

	// For this test use linux/amd64 asset naming regardless of host OS
	// so the test runs uniformly. The real SelfUpdate path uses runtime.GOOS/GOARCH.
	version := "1.2.3"
	binaryContent := "fake-loba-binary-v" + version

	goos := "linux"
	goarch := "amd64"
	if runtime.GOOS == "windows" {
		goos = "windows"
	} else if runtime.GOOS == "darwin" {
		goos = "darwin"
	}
	if runtime.GOARCH == "arm64" {
		goarch = "arm64"
	}

	archiveName := AssetName(version, goos, goarch)
	binaryInArchive := "loba"

	archive := buildFakeTarGz(t, binaryInArchive, binaryContent)
	checksums := buildChecksums(map[string][]byte{
		archiveName: archive,
	})

	mux := http.NewServeMux()

	// /releases/latest
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		rel := Release{
			TagName: "v" + version,
			Assets: []Asset{
				{
					Name:               archiveName,
					BrowserDownloadURL: "http://" + r.Host + "/download/" + archiveName,
				},
				{
					Name:               "checksums.txt",
					BrowserDownloadURL: "http://" + r.Host + "/download/checksums.txt",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	})

	// /download/<file>
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		switch name {
		case archiveName:
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archive)
		case "checksums.txt":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write(checksums)
		default:
			http.NotFound(w, r)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, srv.URL, binaryContent
}

func TestLatestVersion_httptest(t *testing.T) {
	srv, baseURL, _ := fakeServer(t)
	_ = srv

	got, err := latestVersionFromBase(context.Background(), baseURL)
	if err != nil {
		t.Fatalf("latestVersionFromBase: %v", err)
	}
	if got != "1.2.3" {
		t.Errorf("LatestVersion = %q, want %q", got, "1.2.3")
	}
}

func TestSelfUpdate_httptest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows atomic replace tested via windowsRename unit test")
	}

	_, baseURL, expectedContent := fakeServer(t)

	// Create a temp dir to stand in as the directory containing the "exe".
	dir := t.TempDir()
	fakeExe := filepath.Join(dir, "loba")
	if err := os.WriteFile(fakeExe, []byte("old-binary"), 0755); err != nil {
		t.Fatalf("write fake exe: %v", err)
	}

	rel, err := latestReleaseFromBase(context.Background(), baseURL)
	if err != nil {
		t.Fatalf("latestReleaseFromBase: %v", err)
	}

	err = SelfUpdate(context.Background(), rel, "1.0.0", fakeExe, baseURL, func(format string, args ...any) {
		t.Logf("progress: "+format, args...)
	})
	if err != nil {
		t.Fatalf("SelfUpdate: %v", err)
	}

	// Verify the fake exe was replaced.
	got, err := os.ReadFile(fakeExe)
	if err != nil {
		t.Fatalf("read replaced exe: %v", err)
	}
	if string(got) != expectedContent {
		t.Errorf("replaced binary content = %q, want %q", string(got), expectedContent)
	}
}

// ─── Windows rename sequence unit test ────────────────────────────────────────

func TestWindowsRenameSequence(t *testing.T) {
	// Test the windows rename logic on all platforms — the function is exported
	// precisely for this purpose.
	dir := t.TempDir()

	exePath := filepath.Join(dir, "loba.exe")
	tmpPath := filepath.Join(dir, ".loba-update-tmp")

	if err := os.WriteFile(exePath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpPath, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := windowsRename(tmpPath, exePath); err != nil {
		t.Fatalf("windowsRename: %v", err)
	}

	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("read exe after rename: %v", err)
	}
	if string(got) != "new" {
		t.Errorf("exe content = %q, want %q", string(got), "new")
	}
	// Ensure .old was cleaned up.
	if _, err := os.Stat(exePath + ".old"); !os.IsNotExist(err) {
		t.Error("expected .old file to be removed after successful rename")
	}
}

// ─── Checksum tests ───────────────────────────────────────────────────────────

func TestVerifyChecksum_pass(t *testing.T) {
	data := []byte("hello world")
	sum := sha256.Sum256(data)
	checksums := []byte(fmt.Sprintf("%x  loba_1.0.0_linux_amd64.tar.gz\n", sum))
	if err := verifyChecksum(data, "loba_1.0.0_linux_amd64.tar.gz", checksums); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestVerifyChecksum_fail(t *testing.T) {
	data := []byte("hello world")
	checksums := []byte("deadbeef  loba_1.0.0_linux_amd64.tar.gz\n")
	if err := verifyChecksum(data, "loba_1.0.0_linux_amd64.tar.gz", checksums); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
}

// ─── AssetName tests ──────────────────────────────────────────────────────────

func TestAssetName(t *testing.T) {
	tests := []struct {
		ver, goos, goarch, want string
	}{
		{"1.0.0", "linux", "amd64", "loba_1.0.0_linux_amd64.tar.gz"},
		{"1.0.0", "darwin", "arm64", "loba_1.0.0_darwin_arm64.tar.gz"},
		{"1.0.0", "windows", "amd64", "loba_1.0.0_windows_amd64.zip"},
	}
	for _, tc := range tests {
		got := AssetName(tc.ver, tc.goos, tc.goarch)
		if got != tc.want {
			t.Errorf("AssetName(%q,%q,%q) = %q, want %q", tc.ver, tc.goos, tc.goarch, got, tc.want)
		}
	}
}
