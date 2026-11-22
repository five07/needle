package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		version    string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "version flag prints version",
			args:       []string{"--version"},
			version:    "v1.2.3",
			wantCode:   0,
			wantStdout: "tidal-tui v1.2.3\n",
		},
		{
			name:       "single dash version flag",
			args:       []string{"-version"},
			version:    "dev",
			wantCode:   0,
			wantStdout: "tidal-tui dev\n",
		},
		{
			name:       "no args reports UI not implemented",
			args:       nil,
			wantCode:   0,
			wantStderr: "not implemented yet",
		},
		{
			name:       "help exits zero with usage",
			args:       []string{"-h"},
			wantCode:   0,
			wantStderr: "Usage: tidal-tui",
		},
		{
			name:       "unknown flag exits two",
			args:       []string{"--bogus"},
			wantCode:   2,
			wantStderr: "flag provided but not defined",
		},
		{
			name:       "positional argument exits two",
			args:       []string{"extra"},
			wantCode:   2,
			wantStderr: `unexpected argument "extra"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.version != "" {
				old := version
				version = tt.version
				t.Cleanup(func() { version = old })
			}
			var stdout, stderr bytes.Buffer

			code := run(tt.args, &stdout, &stderr)

			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr: %q)", code, tt.wantCode, stderr.String())
			}
			if tt.wantStdout != "" && stdout.String() != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantStdout)
			}
			if tt.wantStdout == "" && stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}
