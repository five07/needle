package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/five07/needle/internal/player"
	"github.com/five07/needle/internal/queue"
	"github.com/five07/needle/internal/tidal"
	"github.com/zalando/go-keyring"

	tea "charm.land/bubbletea/v2"
)

func newTestQueue() *queue.Queue[player.Track] {
	return queue.New[player.Track](queue.WithRand(rand.New(rand.NewPCG(1, 2))))
}

func track(id string) player.Track {
	return player.Track{ID: id, Title: "Song " + id, Artists: "A, B", Album: "Al", Duration: 3*time.Minute + 5*time.Millisecond}
}

func bufLogger() (*slog.Logger, *bytes.Buffer) {
	var b bytes.Buffer
	return slog.New(slog.NewTextHandler(&b, nil)), &b
}

func TestSavedQueueRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), QueueFile)
	q := newTestQueue()
	auto := track("4")
	auto.Autoplay = true
	q.Set([]player.Track{track("1"), track("2"), track("3"), auto}, 0)
	q.Next()
	q.SetShuffle(true)
	q.SetRepeat(queue.RepeatOne)

	s, restored := loadSavedQueue(path, q, discardLogger()) // no file yet
	if restored {
		t.Fatal("restored from a missing file")
	}
	s.save()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", info.Mode().Perm())
	}

	r := newTestQueue()
	logger, logs := bufLogger()
	if _, ok := loadSavedQueue(path, r, logger); !ok {
		t.Fatalf("not restored: %s", logs)
	}
	got, _ := r.Export()
	want, _ := q.Export()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("restored %+v\nwant %+v", got, want)
	}
	if !strings.Contains(logs.String(), `msg="queue restored" tracks=4`) {
		t.Errorf("log: %s", logs)
	}
}

func TestSavedQueueWritesOnlyChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), QueueFile)
	q := newTestQueue()
	q.Set([]player.Track{track("1"), track("2")}, 0)
	s, _ := loadSavedQueue(path, q, discardLogger())
	s.save()
	os.Remove(path)
	s.save() // nothing changed: no write
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("wrote an unchanged queue")
	}
	q.Next()
	s.save()
	if _, err := os.Stat(path); err != nil {
		t.Error("a change was not written")
	}

	// A freshly restored queue is not written back until it changes.
	r := newTestQueue()
	s2, _ := loadSavedQueue(path, r, discardLogger())
	os.Remove(path)
	s2.save()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("wrote the queue it just restored")
	}
}

func TestSavedQueueEmptyKeepsModes(t *testing.T) {
	path := filepath.Join(t.TempDir(), QueueFile)
	q := newTestQueue()
	q.SetShuffle(true)
	q.SetRepeat(queue.RepeatAll)
	s, _ := loadSavedQueue(path, q, discardLogger())
	s.save()
	r := newTestQueue()
	if _, ok := loadSavedQueue(path, r, discardLogger()); ok {
		t.Error("an empty queue reported a current track")
	}
	if !r.Shuffle() || r.Repeat() != queue.RepeatAll {
		t.Errorf("shuffle %v repeat %v", r.Shuffle(), r.Repeat())
	}
}

func TestSavedQueueRejectsDamagedFiles(t *testing.T) {
	good := `{"version":1,"tracks":[{"id":"1","title":"T"}],"play":[0],"current":0,"repeat":"off"}`
	long := strings.Repeat("x", maxFieldLen+1)
	order := make([]string, maxSavedTracks+1)
	for i := range order {
		order[i] = fmt.Sprint(i)
	}
	// Valid apart from its length, so only the limit rejects it.
	many := `{"version":1,"tracks":[` + strings.TrimSuffix(strings.Repeat(`{"id":"1"},`, maxSavedTracks+1), ",") +
		`],"play":[` + strings.Join(order, ",") + `],"current":0,"repeat":"off"}`
	tests := map[string]string{
		"not JSON":          `{"version":`,
		"unknown version":   strings.Replace(good, `"version":1`, `"version":2`, 1),
		"non-numeric ID":    strings.Replace(good, `"id":"1"`, `"id":"../x"`, 1),
		"empty ID":          strings.Replace(good, `"id":"1"`, `"id":""`, 1),
		"ID too long":       strings.Replace(good, `"id":"1"`, `"id":"123456789012345678901"`, 1),
		"title too long":    strings.Replace(good, `"title":"T"`, `"title":"`+long+`"`, 1),
		"artists too long":  strings.Replace(good, `"title":"T"`, `"artists":"`+long+`"`, 1),
		"album too long":    strings.Replace(good, `"title":"T"`, `"album":"`+long+`"`, 1),
		"negative duration": strings.Replace(good, `"title":"T"`, `"duration_ms":-1`, 1),
		"huge duration":     strings.Replace(good, `"title":"T"`, `"duration_ms":86400001`, 1),
		"unknown repeat":    strings.Replace(good, `"repeat":"off"`, `"repeat":"twice"`, 1),
		"bad play order":    strings.Replace(good, `"play":[0]`, `"play":[1]`, 1),
		"current outside":   strings.Replace(good, `"current":0`, `"current":5`, 1),
		"too many tracks":   many,
		"too large":         good + strings.Repeat(" ", maxSavedBytes),
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), QueueFile)
			os.WriteFile(path, []byte(content), 0o600)
			q := newTestQueue()
			q.Set([]player.Track{track("9")}, 0)
			logger, logs := bufLogger()
			s, ok := loadSavedQueue(path, q, logger)
			if ok || !strings.Contains(logs.String(), "saved queue not restored") {
				t.Errorf("restored %v, log %s", ok, logs)
			}
			if c, _ := q.Current(); c.ID != "9" {
				t.Errorf("queue changed: current %q", c.ID)
			}
			// The damaged file is replaced on the next change.
			q.Next()
			s.save()
			if _, err := readSavedQueue(path); err != nil {
				t.Errorf("not replaced: %v", err)
			}
		})
	}
	// The good file itself loads.
	path := filepath.Join(t.TempDir(), QueueFile)
	os.WriteFile(path, []byte(good), 0o600)
	if _, ok := loadSavedQueue(path, newTestQueue(), discardLogger()); !ok {
		t.Error("good file not restored")
	}
}

func TestSavedQueueUnreadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, QueueFile)
	os.Mkdir(path, 0o700) // a directory: opens, but cannot be read
	logger, logs := bufLogger()
	if _, ok := loadSavedQueue(path, newTestQueue(), logger); ok || !strings.Contains(logs.String(), "saved queue not restored") {
		t.Errorf("restored %v, log %s", ok, logs)
	}
}

func TestSavedQueueWriteFailuresLogOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, QueueFile)
	q := newTestQueue()
	logger, logs := bufLogger()
	s, _ := loadSavedQueue(path, q, logger) // dir is missing: writes fail
	q.Add(track("1"))
	s.save()
	q.Add(track("2"))
	s.save()
	if n := strings.Count(logs.String(), "queue not saved"); n != 1 {
		t.Errorf("logged %d times: %s", n, logs)
	}
	// Once writing works again, the next failure is logged again.
	os.Mkdir(dir, 0o700)
	s.save()
	if _, err := os.Stat(path); err != nil {
		t.Fatal("not written once the directory exists")
	}
	os.Remove(path)
	os.Mkdir(path, 0o700) // rename onto a directory fails
	os.WriteFile(filepath.Join(path, "x"), nil, 0o600)
	q.Add(track("3"))
	s.save()
	if n := strings.Count(logs.String(), "queue not saved"); n != 2 {
		t.Errorf("logged %d times: %s", n, logs)
	}
}

func TestSavedQueueTooLongIsNotSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), QueueFile)
	q := newTestQueue()
	q.Add(track("1"))
	logger, logs := bufLogger()
	s, _ := loadSavedQueue(path, q, logger)
	s.save()
	items := make([]player.Track, maxSavedTracks+1)
	for i := range items {
		items[i] = track(fmt.Sprint(i + 1))
	}
	q.Set(items, 0)
	s.save()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the old, smaller queue was kept")
	}
	if !strings.Contains(logs.String(), "over the limit") {
		t.Errorf("log: %s", logs)
	}
	// Removing the old file can fail too; that is what gets logged.
	os.Mkdir(path, 0o700)
	os.WriteFile(filepath.Join(path, "x"), nil, 0o600)
	s.failed = false
	q.Next()
	s.save()
	if !strings.Contains(logs.String(), "forget queue.json") {
		t.Errorf("log: %s", logs)
	}
}

func TestForgetFile(t *testing.T) {
	dir := t.TempDir()
	if err := forgetFile(dir, QueueFile); err != nil {
		t.Errorf("missing file: %v", err)
	}
	os.WriteFile(filepath.Join(dir, QueueFile), nil, 0o600)
	if err := forgetFile(dir, QueueFile); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, QueueFile)); !os.IsNotExist(err) {
		t.Error("file kept")
	}
}

func TestWriteAtomicFailures(t *testing.T) {
	dir := t.TempDir()
	if err := writeAtomic(filepath.Join(dir, "missing", "f"), nil); err == nil {
		t.Error("wrote into a missing directory")
	}
	target := filepath.Join(dir, "d")
	os.MkdirAll(filepath.Join(target, "child"), 0o700)
	if err := writeAtomic(target, []byte("x")); err == nil {
		t.Error("renamed onto a non-empty directory")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temporary file left behind: %v", entries)
	}
}

// -logout also forgets the saved queue (decision 61).
func TestLogoutForgetsSavedQueue(t *testing.T) {
	keyring.MockInit()
	getenv, _, stateDir := testEnv(t, nil)
	os.MkdirAll(stateDir, 0o700)
	file := filepath.Join(stateDir, QueueFile)
	os.WriteFile(file, []byte(`{}`), 0o600)
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), newFakeTIDAL(t).deps(), []string{"-logout"}, getenv, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("saved queue kept after logout")
	}
	os.MkdirAll(filepath.Join(file, "child"), 0o700)
	stderr.Reset()
	if code := run(context.Background(), newFakeTIDAL(t).deps(), []string{"-logout"}, getenv, &stdout, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), "forget queue.json") {
		t.Errorf("exit %d: %s", code, stderr.String())
	}
}

// The watcher saves after a change and once more when it stops, and shows
// every new player notice, even a repeated text.
func TestWatchStatusSavesAndShowsNotices(t *testing.T) {
	path := filepath.Join(t.TempDir(), QueueFile)
	q := newTestQueue()
	saved, _ := loadSavedQueue(path, q, discardLogger())
	st := &player.Status{}
	src := &stubStatus{updates: make(chan struct{}), status: st}
	notices := make(chan string, 4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { watchStatus(ctx, src, watch{notices: notices, q: q, saved: saved}); close(done) }()

	q.Add(track("1"))
	src.mu.Lock()
	st.Notice, st.NoticeID = "Skipped x", 1
	src.mu.Unlock()
	src.set("")
	if got := <-notices; got != "Skipped x" {
		t.Errorf("notice %q", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("change not saved")
	}
	src.mu.Lock()
	st.NoticeID = 2 // the same text again
	src.mu.Unlock()
	src.set("")
	if got := <-notices; got != "Skipped x" {
		t.Errorf("repeated notice %q", got)
	}
	src.set("") // no new notice
	select {
	case got := <-notices:
		t.Errorf("unexpected notice %q", got)
	default:
	}
	// A notice marks the end of an update (saving comes before notices),
	// so the change below comes after every save but the final one.
	src.mu.Lock()
	st.Notice, st.NoticeID = "sync", 3
	src.mu.Unlock()
	src.set("")
	<-notices

	q.Add(track("2")) // changed after the last update
	cancel()
	<-done
	if r, _ := readSavedQueue(path); len(r.Items) != 2 {
		t.Errorf("final save has %d tracks", len(r.Items))
	}
}

// newUI restores the saved queue, shows it on the Queue view, and leaves
// it stopped (decision 59).
func TestNewUIRestoresQueue(t *testing.T) {
	d := newFakeTIDAL(t).deps()
	c, err := tidal.New(stubTokenSource{}, append([]tidal.Option{tidal.WithLogger(discardLogger())}, d.apiOptions...)...)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	os.WriteFile(filepath.Join(stateDir, QueueFile), []byte(`{"version":1,"tracks":[
		{"id":"1","title":"First Song","artists":"Someone","duration_ms":200000},
		{"id":"2","title":"Second Song","artists":"Someone","duration_ms":185000},
		{"id":"3","title":"Third Song","artists":"Someone","duration_ms":100000,"autoplay":true}],
		"play":[0,1,2],"current":1,"repeat":"all"}`), 0o600)
	cfg := testConfig()
	cfg.Autoplay = true
	model, stop, err := newUI(context.Background(), d, cfg, c, tidal.User{Country: "US"}, "", stateDir, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, msg := range exec(model.Init(), time.Second) {
		model.Update(msg)
	}
	s := screen(model)
	for _, want := range []string{"▸ Queue (1)", "▶ Second Song", "∞ Third Song", "Stopped", "↻ all", "∞ on"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "First Song") {
		t.Errorf("history shown in the Queue view:\n%s", s)
	}
}

// stop waits for the watcher's last save, so a change made just before
// quitting is on disk once stop returns.
func TestNewUIStopSavesQueue(t *testing.T) {
	d := newFakeTIDAL(t).deps()
	c, err := tidal.New(stubTokenSource{}, append([]tidal.Option{tidal.WithLogger(discardLogger())}, d.apiOptions...)...)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	model, stop, err := newUI(context.Background(), d, testConfig(), c, tidal.User{Country: "US"}, "", stateDir, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if s := screen(model); !strings.Contains(s, "▸ Search") {
		t.Errorf("without a saved queue the app does not open on Search:\n%s", s)
	}
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, msg := range exec(model.Init(), 10*time.Millisecond) {
		model.Update(msg)
	}
	// Shuffle on, from the keyboard: a queue change with no track.
	_, cmd := model.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	exec(cmd, time.Second)
	stop()
	st, err := readSavedQueue(filepath.Join(stateDir, QueueFile))
	if err != nil || !st.Shuffle {
		t.Errorf("saved %+v, err %v", st, err)
	}
}
