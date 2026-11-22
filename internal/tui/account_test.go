package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var testAccount = AccountInfo{Username: "listener", Country: "US", Email: "listener@example.com", EmailVerified: true,
	Quality: "hires", AudioMode: "exclusive", AudioDevice: "alsa/hw:CARD=DAC,DEV=0", Autoplay: true}

func TestAccountGoldens(t *testing.T) {
	tests := []struct {
		name string
		info AccountInfo
		np   NowPlaying
	}{
		{"account_80x24", testAccount, NowPlaying{Best: "FLAC 24/192", BestSeen: time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)}},
		{"account_nothing_played_60x18", AccountInfo{Username: "new", Country: "GB", Email: "new@example.com", Quality: "lossless", AudioMode: "shared", AudioDevice: "auto"}, NowPlaying{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			w, h := 80, 24
			if strings.Contains(tt.name, "60x18") {
				w, h = 60, 18
			}
			m := New(Config{Services: e.svc, Account: NewAccountView(e.svc, tt.info)})
			m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			drive(m, NowPlayingMsg{tt.np}, press("5"), press("l"))
			golden(t, tt.name, m.render())
		})
	}
}

func TestAccountView(t *testing.T) {
	svc := &Services{}
	v := NewAccountView(svc, testAccount)
	out := ansi.Strip(v.Render(100, 20, true))
	for _, want := range []string{"listener", "US", "listener@example.com", "nothing played yet", "hires", "exclusive", "alsa/hw:CARD=DAC,DEV=0", "config.toml"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "not verified") {
		t.Error("verified email marked")
	}
	// The best quality, with and without a date.
	svc.playing = NowPlaying{Best: "AAC 44.1 kHz"}
	if out := ansi.Strip(v.Render(100, 20, true)); !strings.Contains(out, "AAC 44.1 kHz\n") && !strings.Contains(out, "AAC 44.1 kHz ") {
		t.Errorf("best without date:\n%s", out)
	}
	if strings.Contains(ansi.Strip(v.Render(100, 20, true)), "first seen") {
		t.Error("date without a time")
	}
	// Missing values show a dash; an unverified email is marked; text from
	// TIDAL is sanitized.
	empty := NewAccountView(svc, AccountInfo{Email: "a\x1b[31m@b", Username: ""})
	raw := empty.Render(100, 20, true)
	out = ansi.Strip(raw)
	if !strings.Contains(out, "a@b (not verified)") || !strings.Contains(out, "Username         -") || strings.Contains(raw, "\x1b[31m") {
		t.Errorf("empty account:\n%q", raw)
	}
	if out := ansi.Strip(NewAccountView(svc, AccountInfo{}).Render(100, 20, true)); !strings.Contains(out, "Email            -\n") && !strings.Contains(out, "Email            - ") {
		t.Errorf("no email:\n%s", out)
	}
	// A short pane shows the first lines; no keys.
	if got := strings.Count(v.Render(80, 3, true), "\n"); got != 2 {
		t.Errorf("3 rows rendered %d lines", got+1)
	}
	if v.Render(80, 0, true) != "" || v.Update(nil) != nil || v.Bindings() != nil || v.Capturing() || v.Title() != "Account" {
		t.Error("view metadata")
	}
}
