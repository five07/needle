package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/five07/needle/internal/player"
	"github.com/five07/needle/internal/queue"
	"github.com/five07/needle/internal/visualizer"
)

// statusManifest is the part of *player.Player the visualizer reads.
type statusManifest interface {
	Status() player.Status
	Manifest(ctx context.Context, h queue.Handle) ([]byte, error)
}

// visSource lets the visualizer follow the player (read-only).
type visSource struct{ p statusManifest }

// Clock implements visualizer.Source.
func (v visSource) Clock() visualizer.Clock {
	st := v.p.Status()
	c := visualizer.Clock{Key: uint64(st.Handle), Position: st.Position, Playing: st.State == player.Playing}
	if st.Track != nil {
		c.TrackID = st.Track.ID
	}
	return c
}

// Manifest implements visualizer.Source.
func (v visSource) Manifest(ctx context.Context, key uint64) ([]byte, error) {
	return v.p.Manifest(ctx, queue.Handle(key))
}

// startVisualizer runs the visualizer until ctx ends; stop waits for it
// and removes a temporary directory it created. A missing private
// directory only disables the analysis decode (it is logged).
func startVisualizer(ctx context.Context, p statusManifest, binary string, on bool, getenv func(string) string,
	logger *slog.Logger) (vis *visualizer.Controller, stop func()) {
	dir, owned, err := player.PrivateDir(getenv)
	if err != nil {
		logger.Warn("visualizer: no private directory", "err", err)
		dir = ""
	}
	vis = visualizer.New(visualizer.Config{Source: visSource{p}, Binary: binary, Dir: dir, Logger: logger})
	vis.SetOn(on)
	done := make(chan struct{})
	go func() { defer close(done); vis.Run(ctx) }()
	return vis, func() {
		<-done
		if owned {
			os.RemoveAll(dir)
		}
	}
}
