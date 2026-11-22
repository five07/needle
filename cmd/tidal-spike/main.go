// Command tidal-spike is a throwaway probe for TASK-001. It checks what a
// THIRD_PARTY TIDAL developer app can actually do with the official API:
// PKCE login, which scopes are granted, whether user-scoped endpoints work
// without r_usr, and whether track manifests are full, DRM-free, and playable
// by mpv.
//
// It uses only the standard library. Token values and signed stream URLs are
// never printed.
//
// Usage:
//
//	TIDAL_SPIKE_TOKENS=/path/tokens.json go run ./cmd/tidal-spike <command> [args]
//
// Commands: login, refresh, whoami, get <path>, manifest <track-id>, play <track-id>.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
)

const (
	authorizeURL = "https://login.tidal.com/authorize"
	tokenURL     = "https://auth.tidal.com/v1/oauth2/token"
	apiBase      = "https://openapi.tidal.com/v2"
	redirectURI  = "http://127.0.0.1:8765/callback"
	listenAddr   = "127.0.0.1:8765"
)

// requestedScopes are every scope the developer dashboard allows for the app.
var requestedScopes = []string{
	"collection.read", "collection.write", "entitlements.read", "playback",
	"playlists.read", "playlists.write", "recommendations.read",
	"search.read", "search.write", "user.read",
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: tidal-spike <login|refresh|whoami|get|manifest|play> [args]")
	}
	env, err := loadDotEnv(".env")
	if err != nil {
		return err
	}
	clientID := env["TIDAL_CLIENT_ID"]
	if clientID == "" {
		return errors.New("TIDAL_CLIENT_ID missing from .env")
	}
	store := os.Getenv("TIDAL_SPIKE_TOKENS")
	if store == "" {
		return errors.New("set TIDAL_SPIKE_TOKENS to the token file path")
	}
	c := &client{clientID: clientID, tokenPath: store}

	switch args[0] {
	case "login":
		return c.login(ctx)
	case "refresh":
		return c.refreshCmd(ctx)
	case "whoami":
		return c.getCmd(ctx, "/users/me")
	case "get":
		if len(args) < 2 {
			return errors.New("usage: get <path-with-query>")
		}
		return c.getCmd(ctx, args[1])
	case "manifest":
		return c.manifestCmd(ctx, args[1:])
	case "play":
		return c.playCmd(ctx, args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
