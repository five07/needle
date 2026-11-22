package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/five07/needle/internal/config"
	"github.com/five07/needle/internal/player"
	"github.com/five07/needle/internal/queue"
	"github.com/five07/needle/internal/tui"
	"github.com/zalando/go-keyring"
)

var (
	hires   = player.Source{Codec: "FLAC", Lossless: true, SampleRate: 192000, BitDepth: 24}
	cd      = player.Source{Codec: "FLAC", Lossless: true, SampleRate: 44100, BitDepth: 16}
	aac     = player.Source{Codec: "AAC", SampleRate: 44100}
	aac16   = player.Source{Codec: "AAC", SampleRate: 44100, BitDepth: 16}
	hires96 = player.Source{Codec: "FLAC", Lossless: true, SampleRate: 96000, BitDepth: 24}
)

func TestBetter(t *testing.T) {
	tests := []struct {
		a, b player.Source
		want bool
	}{
		{hires, hires96, true},  // rate first
		{hires96, hires, false}, //
		{cd, aac16, true},       // same rate and depth: lossless wins
		{aac16, cd, false},      //
		{aac16, aac, true},      // depth before lossless
		{cd, cd, false},         // equal is not better
		{aac, player.Source{}, true},
	}
	for _, tt := range tests {
		if got := better(tt.a, tt.b); got != tt.want {
			t.Errorf("better(%v, %v) = %v", tt.a, tt.b, got)
		}
	}
}

func TestBestQualityRemembered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, BestQualityFile)
	logger := discardLogger()
	day1 := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)

	b := loadBestQuality(path, logger)
	if b.label() != "" {
		t.Fatalf("empty start = %q", b.label())
	}
	steps := []struct {
		st    player.Status
		want  string
		saved bool
	}{
		{player.Status{}, "", false},                                                      // nothing loaded
		{player.Status{Source: &hires, Preview: true}, "", false},                         // previews do not count
		{player.Status{Source: &player.Source{Codec: "FLAC", Lossless: true}}, "", false}, // no rate (lossless would rank above nothing)
		{player.Status{Source: &cd}, "FLAC 16/44.1", true},
		{player.Status{Source: &aac}, "FLAC 16/44.1", false}, // worse
		{player.Status{Source: &hires}, "FLAC 24/192", true},
	}
	for i, s := range steps {
		before, _ := os.ReadFile(path)
		b.offer(s.st, day1.Add(time.Duration(i)*time.Hour))
		after, _ := os.ReadFile(path)
		if b.label() != s.want || bytes.Equal(before, after) == s.saved {
			t.Errorf("step %d: label %q, file changed %v", i, b.label(), !bytes.Equal(before, after))
		}
	}
	// The next run reads it back, with the date it was first seen.
	again := loadBestQuality(path, logger)
	if again.label() != "FLAC 24/192" || !again.seen.Equal(day1.Add(5*time.Hour)) {
		t.Errorf("reloaded %q seen %v", again.label(), again.seen)
	}
	// An equal track later keeps the first date.
	again.offer(player.Status{Source: &hires}, day1.Add(48*time.Hour))
	if !again.seen.Equal(day1.Add(5 * time.Hour)) {
		t.Error("an equal track moved the date")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".best_quality-*")); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
}

func TestBestQualityBadFiles(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name, content string
	}{
		{"not JSON", "{"},
		{"no rate", `{"codec":"FLAC","sample_rate":0}`},
		{"no codec", `{"codec":"","sample_rate":44100}`},
		{"long codec", `{"codec":"` + strings.Repeat("x", 17) + `","sample_rate":44100}`},
		{"negative depth", `{"codec":"FLAC","sample_rate":44100,"bit_depth":-1}`},
	}
	for _, tt := range tests {
		path := filepath.Join(dir, tt.name)
		os.WriteFile(path, []byte(tt.content), 0o600)
		if b := loadBestQuality(path, discardLogger()); b.label() != "" {
			t.Errorf("%s: loaded %q", tt.name, b.label())
		}
	}
	// Unreadable (a directory): starts empty.
	if b := loadBestQuality(dir, discardLogger()); b.label() != "" {
		t.Error("loaded a directory")
	}
	// A save that fails is logged; the best is still shown this session.
	b := loadBestQuality(filepath.Join(dir, "missing", BestQualityFile), discardLogger())
	b.offer(player.Status{Source: &cd}, time.Now())
	if b.label() != "FLAC 16/44.1" {
		t.Error("best lost when saving failed")
	}
	// The target is a directory: the rename fails.
	target := filepath.Join(dir, "target")
	os.MkdirAll(filepath.Join(target, "child"), 0o700)
	b = &bestQuality{path: target, logger: discardLogger(), src: cd}
	if err := b.save(); err == nil {
		t.Error("save over a directory")
	}
	// Not remembered at all.
	mem := &bestQuality{logger: discardLogger()}
	mem.offer(player.Status{Source: &aac}, time.Now())
	if mem.label() != "AAC 44.1 kHz" {
		t.Errorf("in memory = %q", mem.label())
	}
}

func TestForgetBestQuality(t *testing.T) {
	dir := t.TempDir()
	if err := forgetBestQuality(dir); err != nil {
		t.Errorf("missing file: %v", err)
	}
	os.WriteFile(filepath.Join(dir, BestQualityFile), []byte("{}"), 0o600)
	if err := forgetBestQuality(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, BestQualityFile)); !os.IsNotExist(err) {
		t.Error("file kept")
	}
	os.MkdirAll(filepath.Join(dir, BestQualityFile, "child"), 0o700)
	if err := forgetBestQuality(dir); err == nil {
		t.Error("removing a non-empty directory succeeded")
	}
}

// -logout also forgets the best quality (decision 52).
func TestLogoutForgetsBestQuality(t *testing.T) {
	keyring.MockInit()
	getenv, _, stateDir := testEnv(t, nil)
	os.MkdirAll(stateDir, 0o700)
	file := filepath.Join(stateDir, BestQualityFile)
	os.WriteFile(file, []byte(`{"codec":"FLAC","sample_rate":44100}`), 0o600)
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), newFakeTIDAL(t).deps(), []string{"-logout"}, getenv, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("best quality kept after logout")
	}
	// A failure to delete it fails the logout.
	os.MkdirAll(filepath.Join(file, "child"), 0o700)
	stderr.Reset()
	if code := run(context.Background(), newFakeTIDAL(t).deps(), []string{"-logout"}, getenv, &stdout, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), "forget best quality") {
		t.Errorf("exit %d: %s", code, stderr.String())
	}
}

func TestWatchStatusBestQuality(t *testing.T) {
	src := &stubStatus{updates: make(chan struct{})}
	src.status = &player.Status{Source: &hires96}
	status := make(chan tui.NowPlaying, 1)
	at := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watchStatus(ctx, src, watch{notices: make(chan string, 1), status: status, q: queue.New[player.Track](),
			mode: config.AudioShared, best: &bestQuality{logger: discardLogger()}, now: func() time.Time { return at }})
		close(done)
	}()
	src.set("")
	cancel()
	<-done
	if np := <-status; np.Best != "FLAC 24/96" || !np.BestSeen.Equal(at) {
		t.Errorf("best %q seen %v", np.Best, np.BestSeen)
	}
}
