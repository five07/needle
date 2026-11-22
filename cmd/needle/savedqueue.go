package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/five07/needle/internal/player"
	"github.com/five07/needle/internal/queue"
)

// QueueFile is the file under $XDG_STATE_HOME/needle/ that keeps the play
// queue across restarts (decisions 59 to 61).
const QueueFile = "queue.json"

// Limits on what a saved queue may hold. A longer queue is not saved, and
// a file over them is ignored.
const (
	maxSavedTracks = 10000
	maxSavedBytes  = 8 << 20
	maxFieldLen    = 1024
	savedVersion   = 1
)

type savedQueueJSON struct {
	Version int          `json:"version"`
	Tracks  []savedTrack `json:"tracks"` // original order
	Play    []int        `json:"play"`   // indexes into tracks, in play order
	Current int          `json:"current"`
	Shuffle bool         `json:"shuffle"`
	Repeat  string       `json:"repeat"`
}

type savedTrack struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Artists    string `json:"artists,omitempty"`
	Album      string `json:"album,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

// savedQueue writes the queue to its file whenever it changes. It is used
// from the watchStatus goroutine only.
type savedQueue struct {
	path   string
	q      *queue.Queue[player.Track]
	logger *slog.Logger
	rev    uint64 // the revision last written (or loaded)
	failed bool   // the last write failed; logged once until one works
}

// loadSavedQueue restores q from path and reports whether a current track
// came back. A missing file starts empty; an unreadable, damaged, or
// oversized one is logged, ignored, and replaced on the next change.
func loadSavedQueue(path string, q *queue.Queue[player.Track], logger *slog.Logger) (*savedQueue, bool) {
	s := &savedQueue{path: path, q: q, logger: logger}
	st, err := readSavedQueue(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, false
	case err != nil:
		logger.Warn("saved queue not restored", "err", err)
		return s, false
	}
	if err := q.Restore(st); err != nil {
		logger.Warn("saved queue not restored", "err", err)
		return s, false
	}
	s.rev = q.Revision()
	logger.Info("queue restored", "tracks", len(st.Items))
	return s, st.Current >= 0
}

func readSavedQueue(path string) (queue.State[player.Track], error) {
	var st queue.State[player.Track]
	f, err := os.Open(path)
	if err != nil {
		return st, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSavedBytes+1))
	if err != nil {
		return st, fmt.Errorf("saved queue: %w", err)
	}
	if len(data) > maxSavedBytes {
		return st, errors.New("saved queue: file is too large")
	}
	var j savedQueueJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return st, errors.New("saved queue: file is damaged")
	}
	if j.Version != savedVersion {
		return st, fmt.Errorf("saved queue: unknown version %d", j.Version)
	}
	if len(j.Tracks) > maxSavedTracks {
		return st, errors.New("saved queue: too many tracks")
	}
	repeat, ok := parseRepeat(j.Repeat)
	if !ok {
		return st, errors.New("saved queue: file is damaged")
	}
	for _, t := range j.Tracks {
		if !validTrackID(t.ID) || len(t.Title) > maxFieldLen || len(t.Artists) > maxFieldLen ||
			len(t.Album) > maxFieldLen || t.DurationMS < 0 || t.DurationMS > int64(24*time.Hour/time.Millisecond) {
			return st, errors.New("saved queue: file is damaged")
		}
		st.Items = append(st.Items, player.Track{ID: t.ID, Title: t.Title, Artists: t.Artists, Album: t.Album,
			Duration: time.Duration(t.DurationMS) * time.Millisecond})
	}
	st.Play, st.Current, st.Shuffle, st.Repeat = j.Play, j.Current, j.Shuffle, repeat
	return st, nil // Restore checks Play and Current
}

func parseRepeat(s string) (queue.Repeat, bool) {
	for _, r := range []queue.Repeat{queue.RepeatOff, queue.RepeatAll, queue.RepeatOne} {
		if r.String() == s {
			return r, true
		}
	}
	return 0, false
}

// validTrackID accepts TIDAL track IDs: 1 to 20 digits.
func validTrackID(id string) bool {
	if id == "" || len(id) > 20 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// save writes the queue if it changed since the last write. Position
// updates do not change the queue, so they never write (decision 60).
func (s *savedQueue) save() {
	if s.q.Revision() == s.rev {
		return
	}
	st, rev := s.q.Export()
	err := s.write(st)
	switch {
	case err == nil:
		s.rev, s.failed = rev, false
	case !s.failed:
		s.failed = true
		s.logger.Warn("queue not saved", "err", err)
	}
}

func (s *savedQueue) write(st queue.State[player.Track]) error {
	if len(st.Items) > maxSavedTracks {
		// Too long to restore: remove the old file rather than keep a
		// queue that no longer matches.
		if err := forgetFile(filepath.Dir(s.path), filepath.Base(s.path)); err != nil {
			return err
		}
		return fmt.Errorf("saved queue: %d tracks is over the limit of %d", len(st.Items), maxSavedTracks)
	}
	j := savedQueueJSON{Version: savedVersion, Play: st.Play, Current: st.Current, Shuffle: st.Shuffle,
		Repeat: st.Repeat.String(), Tracks: make([]savedTrack, len(st.Items))}
	for i, t := range st.Items {
		j.Tracks[i] = savedTrack{ID: t.ID, Title: t.Title, Artists: t.Artists, Album: t.Album, DurationMS: t.Duration.Milliseconds()}
	}
	data, err := json.Marshal(j)
	if err != nil {
		return fmt.Errorf("saved queue: %w", err)
	}
	return writeAtomic(s.path, data)
}

// writeAtomic writes data to path through a temporary file (mode 0600) and
// a rename, so a crash leaves the old file or the new one, never half.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

// forgetFile deletes stateDir/name (for -logout). A missing file is fine.
func forgetFile(stateDir, name string) error {
	err := os.Remove(filepath.Join(stateDir, name))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("forget %s: %w", name, err)
	}
	return nil
}
