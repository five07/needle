package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// AccountInfo is what the Account view shows besides the best quality,
// which comes with each NowPlaying snapshot.
type AccountInfo struct {
	Username      string
	Country       string
	Email         string
	EmailVerified bool
}

// AccountView is the Account section: who is logged in and the best
// quality TIDAL has delivered (TASK-014). It is read-only; the settings
// are in the Settings section (decision 70).
type AccountView struct {
	svc  *Services
	info AccountInfo
}

// NewAccountView returns the Account section view.
func NewAccountView(svc *Services, info AccountInfo) *AccountView {
	return &AccountView{svc: svc, info: info}
}

// Title implements View.
func (v *AccountView) Title() string { return "Account" }

// accountLabelW is the width of the label column.
const accountLabelW = 17

// Render implements View.
func (v *AccountView) Render(width, height int, _ bool) string {
	row := func(label, value string) string {
		return fit("  "+fit(label, accountLabelW)+Sanitize(value), width)
	}
	email := v.info.Email
	if email != "" && !v.info.EmailVerified {
		email += " (not verified)"
	}
	best := "nothing played yet"
	if np := v.svc.playing; np.Best != "" {
		best = np.Best
		if !np.BestSeen.IsZero() {
			best += " (first seen " + np.BestSeen.Format("2006-01-02") + ")"
		}
	}
	lines := []string{
		styleHeading.Render(fit("Account", width)),
		row("Username", orDash(v.info.Username)),
		row("Country", orDash(v.info.Country)),
		row("Email", orDash(email)),
		row("Highest quality", best),
		styleDim.Render(fit("  TIDAL does not share your plan; this is the best received.", width)),
		"",
		styleDim.Render(fit("  Playback and visualizer settings are in Settings (6).", width)),
	}
	return strings.Join(lines[:min(len(lines), max(height, 0))], "\n")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Update implements View. The view has no keys of its own.
func (v *AccountView) Update(tea.Msg) tea.Cmd { return nil }

// Bindings implements View.
func (v *AccountView) Bindings() []key.Binding { return nil }

// Capturing implements View.
func (v *AccountView) Capturing() bool { return false }
