package stream

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/five07/needle/internal/tidal"
)

func apiErr(status int, code string) error {
	return fmt.Errorf("resolve: %w", &tidal.APIError{Method: "GET", Path: "/trackManifests/1", Status: status,
		Errors: []tidal.ErrorObject{{Status: fmt.Sprint(status), Code: code}}})
}

func TestMessage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"canceled", fmt.Errorf("x: %w", context.Canceled), ""},
		{"concurrent playback", apiErr(403, "CONCURRENT_PLAYBACK"), "playing on another app or device"},
		{"geo restricted", apiErr(403, "GEO_RESTRICTED"), "not available in your country"},
		{"purchase required", apiErr(403, "PURCHASE_REQUIRED"), "must be purchased"},
		{"client not entitled", apiErr(403, "CLIENT_NOT_ENTITLED"), "does not allow this app"},
		{"prerequisite missing", apiErr(403, "PREREQUISITE_MISSING"), "needs something set up"},
		{"DRM", &tidal.DRMError{TrackID: "1", System: "WIDEVINE"}, "DRM protected"},
		{"session ended", fmt.Errorf("%w: refresh: x", tidal.ErrUnauthorized), "Log in again"},
		{"not found", apiErr(404, ""), "not available on TIDAL"},
		{"invalid ID", apiErr(400, "INVALID_RESOURCE_ID"), "not available on TIDAL"},
		{"rate limited", apiErr(429, ""), "busy"},
		{"timeout", context.DeadlineExceeded, "did not respond in time"},
		{"anything else", errors.New("tidal: GET /trackManifests/1: 500 Internal Server Error"), "See the log"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Message(tt.err)
			if (tt.want == "" && got != "") || !strings.Contains(got, tt.want) {
				t.Errorf("Message = %q, want it to contain %q", got, tt.want)
			}
			if strings.Contains(got, "trackManifests") || strings.Contains(got, "tidal:") {
				t.Errorf("message exposes internals: %q", got)
			}
		})
	}
}

func TestPreviewMessage(t *testing.T) {
	tests := map[tidal.PreviewReason]string{
		tidal.PreviewRequiresSubscription:     "subscription",
		tidal.PreviewRequiresPurchase:         "purchased",
		tidal.PreviewRequiresHigherAccessTier: "developer access",
		"":                                    "did not provide the full track",
	}
	for reason, want := range tests {
		got := PreviewMessage(reason)
		if !strings.HasPrefix(got, "Preview only: ") || !strings.Contains(got, want) {
			t.Errorf("PreviewMessage(%q) = %q, want it to contain %q", reason, got, want)
		}
	}
}
