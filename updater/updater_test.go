package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, candidate string
		want               bool
	}{
		{"dev", "v1.0.0", true},
		{"", "v1.0.0", true},
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.1", "v1.0.0", false},
		{"v1.0.0", "v1.0.0", false},
		{"v1.1.0", "v2.0.0", true},
		{"v2.0.0", "v1.9.9", false},
		{"v1.0.0", "v1.0.0-rc.1", false}, // a release outranks its prerelease
		{"v1.0.0-rc.1", "v1.0.0", true},
		{"v1.0.0-rc.1", "v1.0.0-rc.2", true},
		{"v1.0.0-alpha.1", "v1.0.0-beta.1", true},
		{"v1.0.0", "v1.0.0+build.1", false}, // build metadata is ignored
	}
	for _, tc := range cases {
		got, err := isNewer(tc.current, tc.candidate)
		if err != nil {
			t.Errorf("isNewer(%q, %q): unexpected error: %v", tc.current, tc.candidate, err)
			continue
		}
		if got != tc.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tc.current, tc.candidate, got, tc.want)
		}
	}
}

func TestIsPrerelease(t *testing.T) {
	if isPrerelease("v1.0.0") {
		t.Error("v1.0.0 should not be a prerelease")
	}
	if !isPrerelease("v1.0.0-rc.1") {
		t.Error("v1.0.0-rc.1 should be a prerelease")
	}
	if isPrerelease("dev") {
		t.Error("dev should not parse as a prerelease")
	}
}

func TestAssetName(t *testing.T) {
	if got := assetName("v1.2.3", "linux", "amd64"); got != "vietelite-mcp_v1.2.3_linux_amd64" {
		t.Errorf("unexpected asset name: %s", got)
	}
	if got := assetName("v1.2.3", "windows", "amd64"); got != "vietelite-mcp_v1.2.3_windows_amd64.exe" {
		t.Errorf("unexpected windows asset name: %s", got)
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("hello world")
	sum := sha256.Sum256(data)
	const name = "testfile"
	sums := hex.EncodeToString(sum[:]) + "  " + name + "\n"

	if err := verifyChecksum(data, name, []byte(sums)); err != nil {
		t.Fatalf("valid checksum rejected: %v", err)
	}
	if err := verifyChecksum([]byte("different"), name, []byte(sums)); err == nil {
		t.Error("expected checksum mismatch error")
	}
	if err := verifyChecksum(data, "other", []byte(sums)); err == nil {
		t.Error("expected missing-entry error")
	}
}

func TestLookupChecksumFormats(t *testing.T) {
	sums := "aaa111  file-a\nbbb222 *file-b\n"
	if got, err := lookupChecksum([]byte(sums), "file-a"); err != nil || got != "aaa111" {
		t.Errorf("file-a: got %q, %v", got, err)
	}
	if got, err := lookupChecksum([]byte(sums), "file-b"); err != nil || got != "bbb222" {
		t.Errorf("file-b (binary form): got %q, %v", got, err)
	}
}

func TestPickLatest(t *testing.T) {
	list := []Release{
		{TagName: "v1.0.0"},
		{TagName: "v1.2.0"},
		{TagName: "v1.1.9"},
		{TagName: "v9.9.9", Draft: true}, // drafts are never selected
	}
	got := pickLatest(list)
	if got == nil || got.TagName != "v1.2.0" {
		t.Errorf("pickLatest = %+v, want v1.2.0", got)
	}
	if pickLatest(nil) != nil {
		t.Error("pickLatest(nil) should be nil")
	}
}

// newReleaseServer serves a fake GitHub API plus release assets.
func newReleaseServer(t *testing.T, tag string, payload []byte) (*httptest.Server, *assetHandler) {
	t.Helper()
	items := &assetHandler{
		payload: payload,
	}
	asset := assetName(tag, runtime.GOOS, runtime.GOARCH)
	// Freeze the expected digest against the original payload so a later
	// tamper() is detectable as a checksum mismatch.
	sum := sha256.Sum256(payload)
	items.sumsText = fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), asset)

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		rel := Release{
			TagName: tag,
			Assets: []Asset{
				{Name: asset, BrowserDownloadURL: base + "/download/" + asset, Size: int64(len(payload))},
				{Name: "checksums.txt", BrowserDownloadURL: base + "/download/checksums.txt"},
			},
		}
		_ = json.NewEncoder(w).Encode(rel)
	})
	mux.HandleFunc("/download/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(items.checksums()))
	})
	mux.HandleFunc("/download/"+asset, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(items.data())
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, items
}

// assetHandler holds mutable asset bytes so a test can tamper mid-flight.
type assetHandler struct {
	payload  []byte
	sumsText string
}

func (a *assetHandler) data() []byte { return a.payload }

func (a *assetHandler) tamper(b []byte) { a.payload = b }

// checksums returns the digest recorded when the fixture was created.
func (a *assetHandler) checksums() string { return a.sumsText }

func TestCheckAndUpdate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("replacement staging differs on windows")
	}

	tag := "v9.9.9"
	payload := []byte("#!/bin/sh\necho fake binary\n")
	srv, _ := newReleaseServer(t, tag, payload)

	// Point the updater at a throwaway binary so the real test binary is safe.
	dir := t.TempDir()
	target := filepath.Join(dir, "vietelite-mcp")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	t.Setenv("MCP_UPDATE_TARGET", target)

	c := New(Options{
		Repo:           "test/repo",
		CurrentVersion: "v1.0.0",
		APIBase:        srv.URL,
	})

	res, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !res.Updateable {
		t.Fatalf("expected update to be available, got %+v", res)
	}
	if res.Latest != tag || res.Asset == nil {
		t.Fatalf("unexpected check result: %+v", res)
	}

	if err := c.Update(context.Background(), res); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read replaced binary: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("replaced binary content = %q, want %q", got, payload)
	}
}

func TestUpdateUpToDate(t *testing.T) {
	c := New(Options{CurrentVersion: "v1.0.0"})
	if err := c.Update(context.Background(), &CheckResult{Updateable: false}); err != ErrUpToDate {
		t.Errorf("Update on current version = %v, want ErrUpToDate", err)
	}
}

func TestCheckRejectsChecksumMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("replacement staging differs on windows")
	}

	tag := "v9.9.9"
	srv, items := newReleaseServer(t, tag, []byte("payload"))

	dir := t.TempDir()
	target := filepath.Join(dir, "vietelite-mcp")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	t.Setenv("MCP_UPDATE_TARGET", target)

	c := New(Options{
		Repo:           "test/repo",
		CurrentVersion: "v1.0.0",
		APIBase:        srv.URL,
	})
	res, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	// Serve different bytes than the ones the checksums.txt was computed from,
	// so verification must fail.
	items.tamper([]byte("tampered"))

	if err := c.Update(context.Background(), res); err == nil ||
		!strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}

	// The original binary must survive a failed verification.
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Errorf("target was modified despite failed verification: %q", got)
	}
}
