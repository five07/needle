// Package stream turns track IDs into playable manifests: it applies the
// quality setting, caches and prefetches manifests, re-fetches them before
// their signed URLs expire, and explains failures in user-facing terms.
package stream

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/five07/tidal-tui/internal/config"
	"github.com/five07/tidal-tui/internal/tidal"
)

// FormatsFor maps the quality setting to the formats requested from TIDAL,
// best first, so the server falls back automatically.
func FormatsFor(q config.Quality) ([]tidal.Format, error) {
	switch q {
	case config.QualityAAC:
		return []tidal.Format{tidal.FormatAACLC}, nil
	case config.QualityLossless:
		return []tidal.Format{tidal.FormatFLAC, tidal.FormatAACLC}, nil
	case config.QualityHiRes:
		return []tidal.Format{tidal.FormatFLACHires, tidal.FormatFLAC, tidal.FormatAACLC}, nil
	}
	return nil, fmt.Errorf("stream: unknown quality %q", q)
}

// Fetcher fetches a manifest. *tidal.Client implements it.
type Fetcher interface {
	TrackManifest(ctx context.Context, trackID string, formats []tidal.Format) (tidal.Manifest, error)
}

const (
	// Margin is extra validity required beyond the track duration, so the
	// last segments are fetched before the signed URLs expire.
	Margin = 2 * time.Minute
	// maxCached bounds the cache: the current track, the prefetched next
	// one, and a little history for "previous".
	maxCached = 8
)

// Resolver resolves track IDs to manifests. It is safe for concurrent use.
type Resolver struct {
	fetcher Fetcher
	formats []tidal.Format
	logger  *slog.Logger
	now     func() time.Time

	mu       sync.Mutex
	cache    map[string]tidal.Manifest
	inflight map[string]*call
}

type call struct {
	done chan struct{}
	m    tidal.Manifest
	err  error
}

// NewResolver returns a Resolver requesting formats for quality q.
func NewResolver(f Fetcher, q config.Quality, logger *slog.Logger) (*Resolver, error) {
	formats, err := FormatsFor(q)
	if err != nil {
		return nil, err
	}
	return &Resolver{
		fetcher:  f,
		formats:  formats,
		logger:   logger,
		now:      time.Now,
		cache:    map[string]tidal.Manifest{},
		inflight: map[string]*call{},
	}, nil
}

// Formats returns the formats requested for the configured quality.
func (r *Resolver) Formats() []tidal.Format { return slices.Clone(r.formats) }

// Resolve returns a manifest for trackID that stays valid for the whole
// track. A cached manifest is reused only if its signed URLs outlive the
// track duration plus Margin; otherwise a fresh one is fetched. Concurrent
// calls for one track share a single request.
func (r *Resolver) Resolve(ctx context.Context, trackID string) (tidal.Manifest, error) {
	c, m, ok := r.start(ctx, trackID)
	if ok {
		return m, nil
	}
	select {
	case <-c.done:
		return c.m, c.err
	case <-ctx.Done():
		return tidal.Manifest{}, ctx.Err()
	}
}

// Prefetch fetches the manifest for trackID in the background, if it is not
// already cached and fresh, so the next track can start without a gap.
// Failures are logged; Resolve retries later.
func (r *Resolver) Prefetch(ctx context.Context, trackID string) {
	r.start(ctx, trackID)
}

// Invalidate drops the cached manifest for trackID, for example after the
// player saw a segment URL rejected. The next Resolve fetches a new one.
func (r *Resolver) Invalidate(trackID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cache, trackID)
}

// start returns a fresh cached manifest (ok=true) or the in-flight call for
// trackID, starting one if needed.
func (r *Resolver) start(ctx context.Context, trackID string) (*call, tidal.Manifest, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.cache[trackID]; ok {
		if m.ValidFor(r.now(), m.Duration+Margin) {
			return nil, m, true
		}
		delete(r.cache, trackID)
	}
	if c, ok := r.inflight[trackID]; ok {
		return c, tidal.Manifest{}, false
	}
	c := &call{done: make(chan struct{})}
	r.inflight[trackID] = c
	// The shared request must not die with the first caller's context;
	// each caller stops waiting on its own context instead. The HTTP client
	// timeout bounds the request.
	go r.fetch(context.WithoutCancel(ctx), trackID, c)
	return c, tidal.Manifest{}, false
}

func (r *Resolver) fetch(ctx context.Context, trackID string, c *call) {
	m, err := r.fetcher.TrackManifest(ctx, trackID, r.formats)
	r.mu.Lock()
	delete(r.inflight, trackID)
	if err == nil {
		r.storeLocked(m)
	}
	r.mu.Unlock()
	c.m, c.err = m, err
	close(c.done)

	if err != nil {
		r.logger.Warn("manifest fetch failed", "track_id", trackID, "err", err)
		return
	}
	r.logger.Info("manifest resolved", "track_id", trackID, "formats", m.Formats,
		"presentation", m.Presentation, "preview_reason", m.PreviewReason,
		"sample_rate", m.SampleRate, "expires_in", m.ExpiresAt.Sub(r.now()).Round(time.Second))
}

// storeLocked caches m, dropping expired entries and then the entries that
// expire soonest while over capacity.
func (r *Resolver) storeLocked(m tidal.Manifest) {
	now := r.now()
	for id, old := range r.cache {
		if !old.ValidFor(now, 0) {
			delete(r.cache, id)
		}
	}
	r.cache[m.TrackID] = m
	for len(r.cache) > maxCached {
		var oldest string
		for id, old := range r.cache {
			if oldest == "" || old.ExpiresAt.Before(r.cache[oldest].ExpiresAt) {
				oldest = id
			}
		}
		delete(r.cache, oldest)
	}
}
