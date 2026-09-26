package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/f1bonacc1/process-compose/src/config"
	"github.com/rs/zerolog/log"
)

// Shells run `process-compose __complete ...` on every <TAB>, often while a
// server is running, so completion must not truncate the server's log. It also
// shouldn't start the background update check, which can delay every <TAB>.
func TestCompletionKeepsServerLogAndSkipsUpdateCheck(t *testing.T) {
	dir := t.TempDir()
	serverLog := filepath.Join(dir, "server.log")
	if err := os.WriteFile(serverLog, []byte("server output\n"), 0o600); err != nil {
		t.Fatalf("write server log: %v", err)
	}
	// Client-mode invocations log to PC_LOG_FILE when it is set.
	t.Setenv(config.LogPathEnvVarName, filepath.Join(dir, "client.log"))

	// Execute mutates process-wide state; restore it.
	origUDS, origLog, origCheck := *pcFlags.IsUnixSocket, *pcFlags.LogFile, config.CheckForUpdates
	origLogFile, origLogger := logFile, log.Logger
	t.Cleanup(func() {
		if logFile != nil && logFile != origLogFile {
			_ = logFile.Close() // so that t.TempDir() can be removed on Windows
		}
		*pcFlags.IsUnixSocket, *pcFlags.LogFile, config.CheckForUpdates = origUDS, origLog, origCheck
		logFile, log.Logger = origLogFile, origLogger
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	*pcFlags.LogFile = serverLog // stands in for the default (server) log path
	config.CheckForUpdates = "true"
	updateMsg = nil // set by checkForUpdatesInBackground

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"__complete", "x"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute __complete: %v", err)
	}

	if got, err := os.ReadFile(serverLog); err != nil || string(got) != "server output\n" {
		t.Errorf("server log = %q (err: %v), want it untouched", got, err)
	}
	if updateMsg != nil {
		t.Error("completion started the background update check")
	}
}
