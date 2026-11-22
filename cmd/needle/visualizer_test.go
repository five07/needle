package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/five07/needle/internal/player"
	"github.com/five07/needle/internal/queue"
	"github.com/five07/needle/internal/visualizer"
)

type fakeStatusManifest struct {
	st    player.Status
	asked queue.Handle
}

func (f *fakeStatusManifest) Status() player.Status { return f.st }
func (f *fakeStatusManifest) Manifest(_ context.Context, h queue.Handle) ([]byte, error) {
	f.asked = h
	if h == 0 {
		return nil, player.ErrNoManifest
	}
	return []byte("<MPD/>"), nil
}

func TestVisSource(t *testing.T) {
	f := &fakeStatusManifest{st: player.Status{State: player.Playing, Handle: 9, Position: 3 * time.Second}}
	v := visSource{f}
	if got := v.Clock(); got != (visualizer.Clock{Key: 9, Position: 3 * time.Second, Playing: true}) {
		t.Errorf("clock %+v", got)
	}
	f.st.Track = &player.Track{ID: "77"}
	if got := v.Clock(); got.TrackID != "77" {
		t.Errorf("clock %+v", got)
	}
	for _, s := range []player.PlayState{player.Paused, player.Loading, player.Stopped} {
		f.st.State = s
		if v.Clock().Playing {
			t.Errorf("%v reported as playing", s)
		}
	}
	if mpd, err := v.Manifest(context.Background(), 9); err != nil || string(mpd) != "<MPD/>" || f.asked != 9 {
		t.Errorf("manifest %q %v asked %d", mpd, err, f.asked)
	}
	if _, err := v.Manifest(context.Background(), 0); !errors.Is(err, player.ErrNoManifest) {
		t.Errorf("err %v", err)
	}
}

func TestStartVisualizer(t *testing.T) {
	run := t.TempDir()
	env := func(k string) string {
		if k == "XDG_RUNTIME_DIR" {
			return run
		}
		return ""
	}
	ctx, cancel := context.WithCancel(context.Background())
	vis, stop := startVisualizer(ctx, &fakeStatusManifest{}, "mpv", true, env, slog.New(slog.DiscardHandler))
	if !vis.On() {
		t.Error("enabled in config but off")
	}
	cancel()
	stop()

	// Without XDG_RUNTIME_DIR it makes its own temporary directory and
	// removes it when it stops.
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	ctx, cancel = context.WithCancel(context.Background())
	_, stop = startVisualizer(ctx, &fakeStatusManifest{}, "mpv", true, func(string) string { return "" }, slog.New(slog.DiscardHandler))
	if made, _ := filepath.Glob(filepath.Join(tmp, "needle-*")); len(made) != 1 {
		t.Fatalf("temp dirs %v", made)
	}
	cancel()
	stop()
	if left, _ := filepath.Glob(filepath.Join(tmp, "needle-*")); len(left) != 0 {
		t.Errorf("temp dir left: %v", left)
	}

	// No private directory (the runtime dir is a file): it still runs, the
	// bars stay empty, and the log says why.
	file := filepath.Join(run, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	bad := func(k string) string {
		if k == "XDG_RUNTIME_DIR" {
			return file
		}
		return ""
	}
	ctx, cancel = context.WithCancel(context.Background())
	vis, stop = startVisualizer(ctx, &fakeStatusManifest{}, "mpv", false, bad, slog.New(slog.NewTextHandler(&logs, nil)))
	cancel()
	stop()
	if vis.On() || !strings.Contains(logs.String(), "no private directory") {
		t.Errorf("on %v, logs %s", vis.On(), logs.String())
	}
}

type wakeCounter struct{ n atomic.Int32 }

func (w *wakeCounter) Wake() { w.n.Add(1) }

func TestWatchStatusWakesVisualizer(t *testing.T) {
	src := &stubStatus{updates: make(chan struct{})}
	w := &wakeCounter{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { watchStatus(ctx, src, watch{notices: make(chan string, 8), vis: w}); close(done) }()
	src.set("a")
	src.set("b")
	cancel()
	<-done
	if w.n.Load() < 2 {
		t.Errorf("woken %d times", w.n.Load())
	}
}
