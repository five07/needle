package player

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// quitCommands returns the commands the fake mpv received from the
// core-idle query on (the start of Close), without position polling.
func quitCommands(t *testing.T, log string) [][]any {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var out [][]any
	seen := false
	for _, line := range strings.Split(string(b), "\n") {
		js, ok := strings.CutPrefix(line, "cmd ")
		if !ok {
			continue
		}
		var req struct{ Command []any }
		if json.Unmarshal([]byte(js), &req) != nil {
			t.Fatalf("bad log line %q", line)
		}
		c := req.Command
		if len(c) > 1 && c[0] == "get_property" && c[1] == "core-idle" {
			seen = true
		}
		if !seen || (c[0] == "get_property" && (c[1] == "time-pos" || c[1] == "duration")) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func fastFade(c *MPVConfig) { c.FadeOut, c.FadeDrain = 20*time.Millisecond, 40*time.Millisecond }

func TestMPVCloseFadesOut(t *testing.T) {
	tests := []struct {
		name   string
		mode   string
		play   bool
		pause  bool
		ramp   float64 // starting volume of the ramp; 0 = no ramp
		mute   bool
		silent bool // quit right away, no fade commands
	}{
		{name: "playing", play: true, ramp: 100},
		{name: "playing at lower volume", mode: "quiet", play: true, ramp: 40},
		{name: "volume unknown mutes", mode: "no-volume", play: true, mute: true},
		{name: "paused", play: true, pause: true, silent: true},
		{name: "idle", silent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, env, err := startFake(t, tt.mode, fastFade)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if tt.play {
				if err := m.Play(ctx, item(1)); err != nil {
					t.Fatal(err)
				}
				collect(t, m, func(e Event) bool { return e.Kind == EventStarted })
			}
			if tt.pause {
				if err := m.SetPaused(ctx, true); err != nil {
					t.Fatal(err)
				}
			}
			start := time.Now()
			m.Close()
			took := time.Since(start)
			cmds := quitCommands(t, env.log)
			if len(cmds) == 0 || cmds[len(cmds)-1][0] != "quit" {
				t.Fatalf("commands = %v, want quit last", cmds)
			}
			body := cmds[1 : len(cmds)-1] // between the core-idle query and quit
			if tt.silent {
				if len(body) != 0 {
					t.Errorf("idle quit sent %v", body)
				}
				if took > 150*time.Millisecond {
					t.Errorf("idle quit waited %s", took)
				}
				return
			}
			want := m.cfg.FadeOut + m.cfg.FadeDrain
			if tt.mute {
				want = m.cfg.FadeDrain // no ramp, only the drain
			}
			if took < want {
				t.Errorf("Close took %s, want at least %s", took, want)
			}
			if last := body[len(body)-1]; last[0] != "set_property" || last[1] != "pause" || last[2] != true {
				t.Errorf("last fade command = %v, want pause", last)
			}
			sets := body[:len(body)-1]
			if tt.mute {
				if len(sets) != 2 || sets[1][1] != "mute" || sets[1][2] != true {
					t.Errorf("mute path = %v", sets)
				}
				return
			}
			sets = sets[1:] // after the volume query
			if len(sets) != fadeSteps {
				t.Fatalf("%d volume steps, want %d: %v", len(sets), fadeSteps, sets)
			}
			prev := tt.ramp
			for _, c := range sets {
				v := c[2].(float64)
				if c[1] != "volume" || v > prev || v < 0 {
					t.Fatalf("ramp not falling from %v: %v", tt.ramp, sets)
				}
				prev = v
			}
			if prev != 0 || sets[0][2].(float64) >= tt.ramp {
				t.Errorf("ramp = %v..%v, want below %v down to 0", sets[0][2], prev, tt.ramp)
			}
		})
	}
}

func TestMPVCloseFadeStallStaysWithinQuitTimeout(t *testing.T) {
	m, _, err := startFake(t, "stall-fade", fastFade)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Play(context.Background(), item(1)); err != nil {
		t.Fatal(err)
	}
	collect(t, m, func(e Event) bool { return e.Kind == EventStarted })
	start := time.Now()
	m.Close()
	if took := time.Since(start); took > m.cfg.QuitTimeout+100*time.Millisecond {
		t.Errorf("Close took %s, quit timeout %s", took, m.cfg.QuitTimeout)
	}
	if err := m.cmd.Process.Signal(os.Signal(nil)); err == nil {
		t.Error("stalled mpv still running")
	}
}

func TestFadeDefaults(t *testing.T) {
	m, _, err := startFake(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if m.cfg.FadeOut != 100*time.Millisecond || m.cfg.FadeDrain != 200*time.Millisecond {
		t.Errorf("defaults = %s %s", m.cfg.FadeOut, m.cfg.FadeDrain)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleep(ctx, time.Hour) {
		t.Error("sleep ignored a done context")
	}
}

// A stalled fade has its own budget, so quit still has time to end mpv
// cleanly instead of it being killed.
func TestMPVCloseFadeStallStillQuitsCleanly(t *testing.T) {
	m, _, err := startFake(t, "stall-volume", fastFade, func(c *MPVConfig) { c.QuitTimeout = time.Second })
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Play(context.Background(), item(1)); err != nil {
		t.Fatal(err)
	}
	collect(t, m, func(e Event) bool { return e.Kind == EventStarted })
	start := time.Now()
	m.Close()
	if took, budget := time.Since(start), m.cfg.FadeOut+m.cfg.FadeDrain+fadeSlack; took > budget+200*time.Millisecond {
		t.Errorf("Close took %s, fade budget %s", took, budget)
	}
	if code := m.cmd.ProcessState.ExitCode(); code != 0 {
		t.Errorf("mpv exit code %d, want a clean quit", code)
	}
}
