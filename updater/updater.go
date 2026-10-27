// Package updater implements self-update for the VietElite MCP server binary
// by pulling release artifacts published by .github/workflows/release.yml.
//
// Typical usage from main:
//
//	client := updater.New(updater.Options{Repo: "chipdien/vietelite_mcp"})
//	res, err := client.Check(ctx)      // is there a newer release?
//	client.Update(ctx, res)            // download, verify, replace this binary
//
// The package never writes to stdout: the stdio transport uses stdout for the
// MCP protocol, so callers must pass stderr (or a log file) as the output
// writer. Notices are emitted only through the optional Logf callback.
package updater

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultRepo is the GitHub repository that publishes release artifacts.
const DefaultRepo = "chipdien/vietelite_mcp"

// DefaultAPIBase is the GitHub REST API root. Overridable for tests.
const DefaultAPIBase = "https://api.github.com"

// BinaryPrefix is the asset filename prefix produced by the release workflow.
const BinaryPrefix = "vietelite-mcp"

// ErrUpToDate is returned by Update when the running binary is already current.
var ErrUpToDate = errors.New("already running the latest version")

// Release describes the subset of a GitHub release this package needs.
type Release struct {
	TagName    string  `json:"tag_name"`
	Name       string  `json:"name"`
	Body       string  `json:"body"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
	Assets     []Asset `json:"assets"`
}

// Asset is a single downloadable file attached to a release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Options configures a Client.
type Options struct {
	// Repo is "owner/name". Defaults to DefaultRepo.
	Repo string
	// CurrentVersion is the running version, e.g. "v1.2.3" or "dev".
	CurrentVersion string
	// Token is an optional GitHub token, used only to raise the anonymous
	// API rate limit. Never required for public repositories.
	Token string
	// APIBase overrides the GitHub API root (tests). Defaults to DefaultAPIBase.
	APIBase string
	// HTTPClient overrides the HTTP client. Defaults to a 30s-timeout client.
	HTTPClient *http.Client
	// IncludePrerelease allows updating to a prerelease when the current
	// version is also a prerelease, or when the caller opts in explicitly.
	IncludePrerelease bool
	// Logf receives human-readable progress notices. Defaults to a no-op so
	// the package is silent unless the caller wants output.
	Logf func(format string, args ...any)
}

// CheckResult is the outcome of a version check.
type CheckResult struct {
	Current    string   `json:"current"`
	Latest     string   `json:"latest"`
	Updateable bool     `json:"update_available"`
	Release    *Release `json:"release,omitempty"`
	// Asset is the release asset matching this OS/arch, nil when none exists.
	Asset *Asset `json:"asset,omitempty"`
}

// Client talks to the GitHub releases API for one repository.
type Client struct {
	repo    string
	current string
	token   string
	apiBase string
	http    *http.Client
	preOK   bool
	logf    func(string, ...any)
	goos    string
	goarch  string
}

// New builds a Client from opts, filling in defaults.
func New(opts Options) *Client {
	c := &Client{
		repo:    opts.Repo,
		current: opts.CurrentVersion,
		token:   opts.Token,
		apiBase: opts.APIBase,
		http:    opts.HTTPClient,
		preOK:   opts.IncludePrerelease,
		logf:    opts.Logf,
		goos:    runtime.GOOS,
		goarch:  runtime.GOARCH,
	}
	if c.repo == "" {
		c.repo = DefaultRepo
	}
	if c.current == "" {
		c.current = "dev"
	}
	if c.apiBase == "" {
		c.apiBase = DefaultAPIBase
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 30 * time.Second}
	}
	if c.logf == nil {
		c.logf = func(string, ...any) {}
	}
	return c
}

// AssetName returns the release asset filename for a version on the current
// platform, e.g. "vietelite-mcp_v1.2.3_linux_amd64".
func (c *Client) AssetName(tag string) string {
	return assetName(tag, c.goos, c.goarch)
}

func assetName(tag, goos, goarch string) string {
	name := fmt.Sprintf("%s_%s_%s_%s", BinaryPrefix, tag, goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// Check fetches the newest release and compares it with the running version.
// A nil Release with Updateable=false means "no newer release published".
func (c *Client) Check(ctx context.Context) (*CheckResult, error) {
	release, err := c.latestRelease(ctx)
	if err != nil {
		return nil, err
	}

	res := &CheckResult{
		Current: c.current,
		Latest:  release.TagName,
		Release: release,
	}
	if newer, err := isNewer(c.current, release.TagName); err == nil && newer {
		res.Updateable = true
	}

	want := c.AssetName(release.TagName)
	for i := range release.Assets {
		if release.Assets[i].Name == want {
			res.Asset = &release.Assets[i]
			break
		}
	}
	if res.Updateable && res.Asset == nil {
		return res, fmt.Errorf("release %s has no asset for %s/%s (expected %q)",
			release.TagName, c.goos, c.goarch, want)
	}
	return res, nil
}

// latestRelease returns the newest non-draft release. Prereleases are included
// when the current version is itself a prerelease or the caller opted in.
func (c *Client) latestRelease(ctx context.Context) (*Release, error) {
	if !c.preOK && !isPrerelease(c.current) {
		return c.fetchOne(ctx, fmt.Sprintf("%s/repos/%s/releases/latest", c.apiBase, c.repo))
	}
	return c.fetchList(ctx, fmt.Sprintf("%s/repos/%s/releases", c.apiBase, c.repo))
}

// fetchOne decodes a single release payload.
func (c *Client) fetchOne(ctx context.Context, url string) (*Release, error) {
	body, err := c.getJSON(ctx, url)
	if err != nil {
		return nil, err
	}
	var rel Release
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	if rel.Draft {
		return nil, fmt.Errorf("newest release %s is a draft", rel.TagName)
	}
	return &rel, nil
}

// fetchList decodes a release list and picks the highest non-draft tag.
func (c *Client) fetchList(ctx context.Context, url string) (*Release, error) {
	body, err := c.getJSON(ctx, url)
	if err != nil {
		return nil, err
	}
	var list []Release
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("decode releases: %w", err)
	}
	rel := pickLatest(list)
	if rel == nil {
		return nil, fmt.Errorf("no published release found for %s", c.repo)
	}
	return rel, nil
}

// getJSON performs an authenticated GET and returns the response body.
func (c *Client) getJSON(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	setGitHubHeaders(req, c.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contact github: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, fmt.Errorf("no published release found for %s", c.repo)
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, errors.New("github api rate limit exceeded (set MCP_GITHUB_TOKEN to raise it)")
	default:
		return nil, fmt.Errorf("github api returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return body, nil
}

// pickLatest chooses the highest non-draft tag from a release list.
func pickLatest(list []Release) *Release {
	var best *Release
	for i := range list {
		rel := &list[i]
		if rel.Draft {
			continue
		}
		if best == nil {
			best = rel
			continue
		}
		if newer, err := isNewer(best.TagName, rel.TagName); err == nil && newer {
			best = rel
		}
	}
	return best
}

// Update downloads the matching asset, verifies its checksum against
// checksums.txt, and atomically replaces the running executable.
//
// On Unix the replacement is a rename over the live binary, which is safe
// because the running process keeps its open inode; the new build takes effect
// on the next start. On Windows a running .exe cannot be replaced, so the new
// binary is staged next to it as "<exe>.new" and an error explains the swap.
func (c *Client) Update(ctx context.Context, res *CheckResult) error {
	if res == nil {
		return errors.New("nil check result")
	}
	if !res.Updateable {
		return ErrUpToDate
	}
	if res.Asset == nil {
		return fmt.Errorf("release %s has no asset for %s/%s", res.Latest, c.goos, c.goarch)
	}

	c.logf("Downloading %s (%s)...", res.Asset.Name, humanSize(res.Asset.Size))
	data, err := c.download(ctx, res.Asset.BrowserDownloadURL)
	if err != nil {
		return err
	}

	sumsURL := assetURL(res.Release, "checksums.txt")
	if sumsURL == "" {
		return errors.New("release has no checksums.txt; refusing to install an unverified binary")
	}
	sumsBody, err := c.download(ctx, sumsURL)
	if err != nil {
		return fmt.Errorf("download checksums.txt: %w", err)
	}
	if err := verifyChecksum(data, res.Asset.Name, sumsBody); err != nil {
		return err
	}
	c.logf("Checksum verified for %s", res.Asset.Name)

	return c.replaceExecutable(data)
}

// download fetches a URL into memory, capped to avoid runaway allocations.
func (c *Client) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	setGitHubHeaders(req, c.token)
	// Release assets redirect to a CDN; the default client follows that.
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}

	const maxBytes = 256 << 20 // 256 MiB is far above a ~15 MiB server binary.
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, io.LimitReader(resp.Body, maxBytes+1)); err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	if buf.Len() > maxBytes {
		return nil, fmt.Errorf("asset %s exceeds %d bytes; refusing to install", url, maxBytes)
	}
	return buf.Bytes(), nil
}

// replaceExecutable writes data to the running binary path atomically.
func (c *Client) replaceExecutable(data []byte) error {
	exe, err := c.resolveExecutable()
	if err != nil {
		return err
	}

	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(exe)+".update-*")
	if err != nil {
		return fmt.Errorf("create temp file next to %s (need write permission): %w", exe, err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Best-effort cleanup if we never renamed it into place.
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp binary: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp binary: %w", err)
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return fmt.Errorf("chmod temp binary: %w", err)
	}

	if runtime.GOOS == "windows" {
		staged := exe + ".new"
		if err := os.Rename(tmpName, staged); err != nil {
			return fmt.Errorf("stage new binary: %w", err)
		}
		return fmt.Errorf("new binary staged at %s; stop the server and rename it over %s to finish the update", staged, exe)
	}

	if err := os.Rename(tmpName, exe); err != nil {
		return fmt.Errorf("replace %s: %w", exe, err)
	}
	c.logf("Replaced %s", exe)
	return nil
}

// resolveExecutable returns the path to replace, honouring an explicit override
// in MCP_UPDATE_TARGET (useful when the binary is launched via a wrapper).
func (c *Client) resolveExecutable() (string, error) {
	if override := os.Getenv("MCP_UPDATE_TARGET"); override != "" {
		return override, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve running executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

// setGitHubHeaders applies the headers GitHub expects on API and asset calls.
func setGitHubHeaders(req *http.Request, token string) {
	req.Header.Set("User-Agent", "vietelite-mcp-updater")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// assetURL finds an asset by exact name and returns its download URL.
func assetURL(rel *Release, name string) string {
	if rel == nil {
		return ""
	}
	for _, a := range rel.Assets {
		if a.Name == name {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// verifyChecksum checks data against the "<sha256>  <filename>" line for name
// in a checksums.txt body (the format produced by shasum -a 256).
func verifyChecksum(data []byte, name string, sums []byte) error {
	want, err := lookupChecksum(sums, name)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", name, want, got)
	}
	return nil
}

// lookupChecksum extracts the expected digest for name from a checksums file.
func lookupChecksum(sums []byte, name string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		// shasum writes "<hash>  <file>"; accept the single-space binary form too.
		if strings.TrimPrefix(fields[1], "*") == name {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum entry for %s in checksums.txt", name)
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
