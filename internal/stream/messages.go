package stream

import (
	"context"
	"errors"

	"github.com/five07/tidal-tui/internal/tidal"
)

// Message returns a short, user-readable explanation of a playback error,
// or "" for nil and for cancellation (which the user caused). The details
// stay in the log.
func Message(err error) string {
	switch {
	case err == nil, errors.Is(err, context.Canceled):
		return ""
	case errors.Is(err, tidal.ErrConcurrentPlayback):
		return "Your TIDAL account is playing on another app or device. Stop it there, then try again."
	case errors.Is(err, tidal.ErrGeoRestricted):
		return "This track is not available in your country."
	case errors.Is(err, tidal.ErrPurchaseRequired):
		return "This track must be purchased on TIDAL before it can be played."
	case errors.Is(err, tidal.ErrClientNotEntitled):
		return "TIDAL does not allow this app to play this track."
	case errors.Is(err, tidal.ErrPrerequisiteMissing):
		return "TIDAL needs something set up on your account before this track can play. Check your account in the TIDAL app."
	case errors.Is(err, tidal.ErrDRMProtected):
		return "This track is DRM protected, and tidal-tui only plays unprotected streams."
	case errors.Is(err, tidal.ErrUnauthorized):
		return "Your TIDAL session has ended. Log in again."
	case errors.Is(err, tidal.ErrNotFound), errors.Is(err, tidal.ErrInvalidResourceID):
		return "This track is not available on TIDAL."
	case errors.Is(err, tidal.ErrRateLimited):
		return "TIDAL is busy right now. Try again in a moment."
	case errors.Is(err, context.DeadlineExceeded):
		return "TIDAL did not respond in time. Check your connection and try again."
	}
	return "Could not start playback. See the log for details."
}

// PreviewMessage explains why only a preview is playing.
func PreviewMessage(reason tidal.PreviewReason) string {
	switch reason {
	case tidal.PreviewRequiresSubscription:
		return "Preview only: full tracks need an active TIDAL subscription."
	case tidal.PreviewRequiresPurchase:
		return "Preview only: the full track must be purchased on TIDAL."
	case tidal.PreviewRequiresHigherAccessTier:
		return "Preview only: this app's TIDAL developer access allows previews, not full tracks."
	}
	return "Preview only: TIDAL did not provide the full track."
}
