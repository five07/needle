package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testEnv returns a getenv backed by temp XDG dirs, plus those dirs.
func testEnv(t *testing.T, extra map[string]string) (getenv func(string) string, configDir, stateDir string) {
	t.Helper()
	root := t.TempDir()
	env := map[string]string{
		"HOME":            filepath.Join(root, "home"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_STATE_HOME":  filepath.Join(root, "state"),
	}
	for k, v := range extra {
		if v == "" {
			delete(env, k)
			continue
		}
		env[k] = v
	}
	return func(k string) string { return env[k] },
		filepath.Join(root, "config", "tidal-tui"),
		filepath.Join(root, "state", "tidal-tui")
}

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		version    string
		env        map[string]string // "" removes a variable
		config     string            // written to config.toml when non-empty
		wantCode   int
		wantStdout string
		wantStderr string
		wantLog    []string // substrings expected in the log file
		wantNoLog  bool
	}{
		{
			name:       "version flag prints version",
			args:       []string{"--version"},
			version:    "v1.2.3",
			wantStdout: "tidal-tui v1.2.3\n",
			wantNoLog:  true,
		},
		{
			name:       "version works without HOME",
			args:       []string{"-version"},
			version:    "dev",
			env:        map[string]string{"HOME": "", "XDG_CONFIG_HOME": "", "XDG_STATE_HOME": ""},
			wantStdout: "tidal-tui dev\n",
			wantNoLog:  true,
		},
		{
			name:       "no args loads defaults, logs startup, and needs a client ID",
			wantCode:   1,
			wantStderr: "no TIDAL client ID: set auth.client_id in",
			wantLog:    []string{"msg=starting", "quality=hires", "audio_mode=shared", "audio_device=auto", "client_id_set=false"},
		},
		{
			name:       "config file and env are applied",
			env:        map[string]string{"TIDAL_CLIENT_ID": "envid"},
			config:     "quality = \"lossless\"\n[audio]\nmode = \"exclusive\"\n",
			wantStdout: "Logged in as listener (US).\n",
			wantStderr: "not implemented yet",
			wantLog:    []string{"quality=lossless", "audio_mode=exclusive", "client_id_set=true"},
		},
		{
			name:       "invalid config exits one naming the key",
			config:     "[audio]\nmode = \"loud\"\n",
			wantCode:   1,
			wantStderr: "audio.mode",
			wantNoLog:  true,
		},
		{
			name:       "missing HOME and XDG exits one",
			env:        map[string]string{"HOME": "", "XDG_CONFIG_HOME": "", "XDG_STATE_HOME": ""},
			wantCode:   1,
			wantStderr: "HOME is not set",
			wantNoLog:  true,
		},
		{
			name:       "help exits zero with usage",
			args:       []string{"-h"},
			wantStderr: "Usage: tidal-tui",
			wantNoLog:  true,
		},
		{
			name:       "unknown flag exits two",
			args:       []string{"--bogus"},
			wantCode:   2,
			wantStderr: "flag provided but not defined",
			wantNoLog:  true,
		},
		{
			name:       "positional argument exits two",
			args:       []string{"extra"},
			wantCode:   2,
			wantStderr: `unexpected argument "extra"`,
			wantNoLog:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.version != "" {
				old := version
				version = tt.version
				t.Cleanup(func() { version = old })
			}
			getenv, configDir, stateDir := testEnv(t, tt.env)
			if tt.config != "" {
				if err := os.MkdirAll(configDir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(tt.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr bytes.Buffer

			code := run(context.Background(), newFakeTIDAL(t).deps(), tt.args, getenv, &stdout, &stderr)

			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr: %q)", code, tt.wantCode, stderr.String())
			}
			if stdout.String() != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantStdout)
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}

			logBytes, err := os.ReadFile(filepath.Join(stateDir, "tidal-tui.log"))
			if tt.wantNoLog {
				if err == nil {
					t.Errorf("log file written, want none:\n%s", logBytes)
				}
				return
			}
			if err != nil {
				t.Fatalf("read log: %v", err)
			}
			for _, want := range tt.wantLog {
				if !strings.Contains(string(logBytes), want) {
					t.Errorf("log missing %q:\n%s", want, logBytes)
				}
			}
		})
	}
}

func TestRunLogOpenFailure(t *testing.T) {
	getenv, _, stateDir := testEnv(t, nil)
	// Put a file where the state directory should be.
	if err := os.MkdirAll(filepath.Dir(stateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), newFakeTIDAL(t).deps(), nil, getenv, &stdout, &stderr); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "logging:") {
		t.Errorf("stderr = %q, want a logging error", stderr.String())
	}
}

func TestMainExitCode(t *testing.T) {
	oldArgs, oldExit, oldStdout := os.Args, exit, os.Stdout
	t.Cleanup(func() { os.Args, exit, os.Stdout = oldArgs, oldExit, oldStdout })
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	os.Stdout = devNull // keep the version line out of test output

	for _, tt := range []struct {
		args []string
		want int
	}{{[]string{"-version"}, 0}, {[]string{"--bogus"}, 2}} {
		got := -1
		exit = func(code int) { got = code }
		os.Args = append([]string{"tidal-tui"}, tt.args...)
		main()
		if got != tt.want {
			t.Errorf("main %v exit = %d, want %d", tt.args, got, tt.want)
		}
	}
}
