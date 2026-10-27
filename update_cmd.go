package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"vietelite-mcp/go-server/logger"
	"vietelite-mcp/go-server/updater"
	"vietelite-mcp/go-server/version"
)

// envBool parses a boolean env var, treating "1"/"true"/"yes"/"on" as true.
func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// repoFromEnv returns the GitHub repo to check, overridable for forks.
func repoFromEnv() string {
	if repo := strings.TrimSpace(os.Getenv("MCP_UPDATE_REPO")); repo != "" {
		return repo
	}
	return updater.DefaultRepo
}

// newUpdater builds an update client. Notices go to stderr, never stdout,
// because stdout carries the MCP protocol on the stdio transport.
func newUpdater(out io.Writer) *updater.Client {
	return updater.New(updater.Options{
		Repo:              repoFromEnv(),
		CurrentVersion:    version.Version,
		Token:             strings.TrimSpace(os.Getenv("MCP_GITHUB_TOKEN")),
		IncludePrerelease: envBool("MCP_UPDATE_PRERELEASE"),
		Logf: func(format string, args ...any) {
			fmt.Fprintf(out, "[update] "+format+"\n", args...)
		},
	})
}

// runUpdateCheck prints whether a newer release exists. Returns the process
// exit code: 0 for "current", 2 when an update is available, 1 on error.
func runUpdateCheck(out io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Fprintf(out, "Current version: %s\n", version.String())

	res, err := newUpdater(out).Check(ctx)
	if err != nil {
		fmt.Fprintf(out, "Update check failed: %v\n", err)
		logger.Error("UPDATE_CHECK", "failed: %v", err)
		return 1
	}

	if !res.Updateable {
		fmt.Fprintf(out, "Up to date (latest release: %s).\n", res.Latest)
		logger.Info("UPDATE_CHECK", "up to date: current=%s latest=%s", version.Version, res.Latest)
		return 0
	}

	fmt.Fprintf(out, "Update available: %s -> %s\n", res.Current, res.Latest)
	if res.Asset != nil {
		fmt.Fprintf(out, "Asset: %s\n", res.Asset.Name)
	}
	fmt.Fprintln(out, "Run with --update to install it.")
	logger.Info("UPDATE_CHECK", "update available: current=%s latest=%s", version.Version, res.Latest)
	return 2
}

// runUpdate performs a manual update. Returns the process exit code.
func runUpdate(out io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client := newUpdater(out)
	fmt.Fprintf(out, "Current version: %s\n", version.String())

	res, err := client.Check(ctx)
	if err != nil {
		fmt.Fprintf(out, "Update check failed: %v\n", err)
		logger.Error("UPDATE", "check failed: %v", err)
		return 1
	}
	if !res.Updateable {
		fmt.Fprintf(out, "Already up to date (latest release: %s).\n", res.Latest)
		logger.Info("UPDATE", "already up to date: %s", res.Latest)
		return 0
	}

	fmt.Fprintf(out, "Updating %s -> %s...\n", res.Current, res.Latest)
	if err := client.Update(ctx, res); err != nil {
		if err == updater.ErrUpToDate {
			fmt.Fprintln(out, "Already up to date.")
			return 0
		}
		fmt.Fprintf(out, "Update failed: %v\n", err)
		logger.Error("UPDATE", "failed: %v", err)
		return 1
	}

	fmt.Fprintf(out, "Updated to %s. Restart the server to use the new build.\n", res.Latest)
	logger.Info("UPDATE", "updated to %s", res.Latest)
	return 0
}

// autoUpdate runs a best-effort update check on startup. It never blocks the
// server for long and never fails startup: any problem is logged and ignored.
//
// Auto-update is opt-in because replacing a running binary under a service
// manager is surprising. Set MCP_AUTO_UPDATE=1 to enable it; MCP_UPDATE_PRERELEASE=1
// additionally opts into prerelease builds.
func autoUpdate() {
	if !envBool("MCP_AUTO_UPDATE") {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	client := newUpdater(os.Stderr)
	res, err := client.Check(ctx)
	if err != nil {
		logger.Warn("AUTO_UPDATE", "check failed: %v", err)
		return
	}
	if !res.Updateable {
		logger.Info("AUTO_UPDATE", "up to date: %s", res.Latest)
		return
	}

	logger.Info("AUTO_UPDATE", "update available: %s -> %s", res.Current, res.Latest)
	if err := client.Update(ctx, res); err != nil {
		logger.Warn("AUTO_UPDATE", "install failed (run --update manually): %v", err)
		return
	}
	logger.Info("AUTO_UPDATE", "installed %s; restart required", res.Latest)
	fmt.Fprintf(os.Stderr, "[update] installed %s — restart the server to use it\n", res.Latest)
}
