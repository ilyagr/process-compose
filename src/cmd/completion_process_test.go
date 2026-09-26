package cmd

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/f1bonacc1/process-compose/src/config"
	"github.com/f1bonacc1/process-compose/src/loader"
	"github.com/f1bonacc1/process-compose/src/types"
	"github.com/spf13/cobra"
)

const completionValidConfig = `version: "0.5"
processes:
  postgres:
    command: "echo postgres"
    description: "the database"
  redis:
    command: "echo redis"
  minio:
    command: "echo minio"
`

const completionUnclosedBraceConfig = `version: "0.5"
processes:
  postgres:
    command: "echo ${UNCLOSED_BRACE"
`

const completionBaseConfig = `version: "0.5"
processes:
  base_proc:
    command: "echo base"
`

const completionExtendsConfig = `version: "0.5"
extends: base.yaml
processes:
  top_proc:
    command: "echo top"
`

func writeCompletionConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "process-compose.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestCompleteProcessNamesFromConfig(t *testing.T) {
	// The helper reads the package-global opts; restore it after the test.
	orig := opts
	t.Cleanup(func() { opts = orig })

	setConfig := func(t *testing.T, content string) {
		opts = &loader.LoaderOptions{FileNames: []string{writeCompletionConfig(t, content)}}
	}

	t.Run("returns names and descriptions in lexicographic order", func(t *testing.T) {
		setConfig(t, completionValidConfig)
		got, directive := completeProcessNamesFromConfig(true)(nil, nil, "")
		if want := []string{"minio", "postgres\tthe database", "redis"}; !slices.Equal(got, want) {
			t.Errorf("names = %v, want %v", got, want)
		}
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("directive = %d, want NoFileComp (%d)", directive, cobra.ShellCompDirectiveNoFileComp)
		}
	})

	t.Run("single falls back to file completion once PROCESS is present", func(t *testing.T) {
		// For `run PROCESS -- ARGS`, whose ARGS are passed to PROCESS.
		setConfig(t, completionValidConfig)
		got, directive := completeProcessNamesFromConfig(true)(nil, []string{"postgres"}, "")
		if len(got) != 0 {
			t.Errorf("names = %v, want none after first positional arg", got)
		}
		if directive != cobra.ShellCompDirectiveDefault {
			t.Errorf("directive = %d, want Default", directive)
		}
	})

	t.Run("non-single completes the remaining names at every position", func(t *testing.T) {
		setConfig(t, completionValidConfig)
		got, _ := completeProcessNamesFromConfig(false)(nil, []string{"postgres"}, "")
		if want := []string{"minio", "redis"}; !slices.Equal(got, want) {
			t.Errorf("names = %v, want %v", got, want)
		}
	})

	t.Run("loads a config file that is listed twice", func(t *testing.T) {
		// cobra parses flags twice during `__complete`, so `-f top.yaml`
		// arrives as [top.yaml top.yaml]. Loading top.yaml twice would fail
		// with "base.yaml extends itself" and yield no candidates.
		dir := t.TempDir()
		top := filepath.Join(dir, "top.yaml")
		if err := os.WriteFile(filepath.Join(dir, "base.yaml"), []byte(completionBaseConfig), 0o644); err != nil {
			t.Fatalf("write base config: %v", err)
		}
		if err := os.WriteFile(top, []byte(completionExtendsConfig), 0o644); err != nil {
			t.Fatalf("write extending config: %v", err)
		}
		opts = &loader.LoaderOptions{FileNames: []string{top, top}}
		got, _ := completeProcessNamesFromConfig(false)(nil, nil, "")
		if want := []string{"base_proc", "top_proc"}; !slices.Equal(got, want) {
			t.Errorf("names = %v, want %v", got, want)
		}
	})

	t.Run("broken config yields no candidates instead of exiting", func(t *testing.T) {
		// completionUnclosedBraceConfig triggers an envsubst "missing closing
		// brace" error, which is fatal (and would abort the whole test binary)
		// *unless* completeProcessNamesFromConfig properly sets the
		// IsInternalLoader option.
		setConfig(t, completionUnclosedBraceConfig)
		got, directive := completeProcessNamesFromConfig(true)(nil, nil, "")
		if len(got) != 0 {
			t.Errorf("names = %v, want none on a broken config", got)
		}
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("directive = %d, want NoFileComp", directive)
		}
	})
}

func TestCompleteProcessNamesFromServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/processes", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(types.ProcessesState{States: []types.ProcessState{
			{Name: "web"}, {Name: "db"}, {Name: "cache"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	host, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("parse test server address: %v", err)
	}

	// getClient() reads process-wide flags; restore them after the test.
	origUDS, origAddr, origPort := *pcFlags.IsUnixSocket, *pcFlags.Address, *pcFlags.PortNum
	t.Cleanup(func() {
		*pcFlags.IsUnixSocket, *pcFlags.Address, *pcFlags.PortNum = origUDS, origAddr, origPort
	})
	*pcFlags.IsUnixSocket, *pcFlags.Address = false, host
	if *pcFlags.PortNum, err = strconv.Atoi(port); err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	// With PC_SOCKET_PATH set, the helper would switch to a unix socket.
	t.Setenv(config.EnvVarUnixSocketPath, "")

	got, directive := completeProcessNamesFromServer(false)(&cobra.Command{}, nil, "")
	if want := []string{"cache", "db", "web"}; !slices.Equal(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %d, want NoFileComp", directive)
	}

	got, _ = completeProcessNamesFromServer(false)(&cobra.Command{}, []string{"web"}, "")
	if want := []string{"cache", "db"}; !slices.Equal(got, want) {
		t.Errorf("names after `web` = %v, want %v", got, want)
	}
}

func TestCompletionEntry(t *testing.T) {
	tests := []struct {
		name, description, want string
	}{
		{"web", "", "web"},
		{"web", "a server", "web\ta server"},
		{"web", "first line\nsecond line", "web\tfirst line"},
		{"web", "has\ttab", "web\thas tab"},
	}
	for _, tt := range tests {
		if got := completionEntry(tt.name, tt.description); got != tt.want {
			t.Errorf("completionEntry(%q, %q) = %q, want %q", tt.name, tt.description, got, tt.want)
		}
	}
}

// Tests that `--unix-socket` implies `-U` even in the context of completions.
//
// This very particular usecase requires a special workaround in
// `completeProcessNamesFromServer` to work correctly. Without it, completion
// would still work correctly for `process-compose -U --unix-socket blah process
// start <TAB>` or `PC_SOCKET_PATH=blah process-compose ...`, but it would
// surprisingly not work for just `process-compose --unix-socket blah ...`.
//
// While few people are likely to use process name completion with
// `--unix-socket`, they would be really confused if we didn't implement such a
// workaround, since they'd be used to `--unix-socket` implying `-U` in all
// other situations.
func TestProcessStopCompletionOverUnixSocketFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("--unix-socket flag is not registered on Windows")
	}

	// Fake server on a unix socket.
	//
	// We must keep the path short. For example, on macOS, the cap on `sun_path`
	// is 104 bytes, so process-compose would fail to bind to a socket with a
	// longer path. By an unfortunate coincidence, macOS also has particularly
	// long temporary dir names; t.TempDir() results in long paths along the
	// lines of `/var/folders/<2 chars>/<~30-char hash>/T/test_name.suffix/001`
	dir, err := os.MkdirTemp("", "pc") //nolint:usetesting // t.TempDir() path too long on e.g. macOS (see above)
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/processes", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(types.ProcessesState{States: []types.ProcessState{
			{Name: "web"}, {Name: "db"},
		}})
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	// UDS must come from the flag, not the PC_SOCKET_PATH env var, for the
	// failure-prone version of the scenario
	t.Setenv(config.EnvVarUnixSocketPath, "")

	// Execute mutates process-wide state; restore it.
	origUDS, origPath, origLog := *pcFlags.IsUnixSocket, *pcFlags.UnixSocketPath, *pcFlags.LogFile
	origCheck := config.CheckForUpdates
	t.Cleanup(func() {
		*pcFlags.IsUnixSocket, *pcFlags.UnixSocketPath, *pcFlags.LogFile = origUDS, origPath, origLog
		config.CheckForUpdates = origCheck
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	*pcFlags.IsUnixSocket = false
	config.CheckForUpdates = "false"                // PreRun would otherwise hit the network
	*pcFlags.LogFile = filepath.Join(dir, "pc.log") // keep PreRun's setupLogger out of the real log
	// PreRun may use the client log instead (e.g. for completion requests),
	// which honors PC_LOG_FILE.
	t.Setenv(config.LogPathEnvVarName, filepath.Join(dir, "pc.log"))

	var out, errb bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errb)
	rootCmd.SetArgs([]string{"__complete", "process", "stop", "--unix-socket", sock, ""})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute __complete: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "db") || !strings.Contains(got, "web") {
		t.Errorf("completion output = %q; want it to list db and web (UDS via --unix-socket honored)", got)
	}
}
