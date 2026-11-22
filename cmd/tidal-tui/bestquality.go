package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/five07/tidal-tui/internal/player"
)

// BestQualityFile is the file under $XDG_STATE_HOME/tidal-tui/ that
// remembers the highest quality TIDAL has delivered (decision 52).
const BestQualityFile = "best_quality.json"

// bestQuality tracks the highest source quality received. It is used from
// the watchStatus goroutine only.
type bestQuality struct {
	path   string // empty: not remembered
	logger *slog.Logger
	src    player.Source
	seen   time.Time
}

type bestQualityJSON struct {
	Codec      string    `json:"codec"`
	Lossless   bool      `json:"lossless"`
	SampleRate int       `json:"sample_rate"`
	BitDepth   int       `json:"bit_depth"`
	Seen       time.Time `json:"seen"`
}

// loadBestQuality reads path. A missing or unreadable file starts empty;
// a damaged one is logged and replaced on the next better track.
func loadBestQuality(path string, logger *slog.Logger) *bestQuality {
	b := &bestQuality{path: path, logger: logger}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logger.Warn("best quality not loaded", "err", err)
		}
		return b
	}
	var j bestQualityJSON
	if err := json.Unmarshal(data, &j); err != nil || j.SampleRate <= 0 || j.Codec == "" || len(j.Codec) > 16 || j.BitDepth < 0 {
		logger.Warn("best quality file is damaged; ignoring it", "path", path)
		return b
	}
	b.src = player.Source{Codec: j.Codec, Lossless: j.Lossless, SampleRate: j.SampleRate, BitDepth: j.BitDepth}
	b.seen = j.Seen
	return b
}

// better reports whether a is a higher quality than b: sample rate first,
// then bit depth, then lossless over lossy.
func better(a, b player.Source) bool {
	if a.SampleRate != b.SampleRate {
		return a.SampleRate > b.SampleRate
	}
	if a.BitDepth != b.BitDepth {
		return a.BitDepth > b.BitDepth
	}
	return a.Lossless && !b.Lossless
}

// offer records st's source when it beats the best so far. Previews are
// ignored: they say nothing about the account's quality.
func (b *bestQuality) offer(st player.Status, now time.Time) {
	if st.Source == nil || st.Preview || st.Source.SampleRate <= 0 || !better(*st.Source, b.src) {
		return
	}
	b.src, b.seen = *st.Source, now
	if b.path == "" {
		return
	}
	if err := b.save(); err != nil {
		b.logger.Warn("best quality not saved", "err", err)
	}
}

// save writes the file atomically (a temporary file, then a rename).
func (b *bestQuality) save() error {
	data, err := json.Marshal(bestQualityJSON{Codec: b.src.Codec, Lossless: b.src.Lossless, SampleRate: b.src.SampleRate, BitDepth: b.src.BitDepth, Seen: b.seen})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(b.path), ".best_quality-*")
	if err != nil {
		return fmt.Errorf("best quality: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("best quality: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("best quality: %w", err)
	}
	if err := os.Rename(tmp.Name(), b.path); err != nil {
		return fmt.Errorf("best quality: %w", err)
	}
	return nil
}

// label returns the best quality for the UI, "" when nothing was received.
func (b *bestQuality) label() string {
	if b.src.SampleRate <= 0 {
		return ""
	}
	return b.src.Codec + " " + shortFormat(b.src.BitDepth, b.src.SampleRate)
}

// forgetBestQuality deletes the file (for -logout). A missing file is fine.
func forgetBestQuality(stateDir string) error {
	err := os.Remove(filepath.Join(stateDir, BestQualityFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("forget best quality: %w", err)
	}
	return nil
}
