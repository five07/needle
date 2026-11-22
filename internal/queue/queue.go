// Package queue implements the play queue: play next, add to queue, shuffle,
// repeat, and track navigation. It is independent of the API, player, and UI,
// and is safe for concurrent use.
package queue

import (
	"math/rand/v2"
	"slices"
	"sync"
)

// Repeat is the repeat mode.
type Repeat int

// Repeat modes, in the order Cycle steps through them.
const (
	RepeatOff Repeat = iota
	RepeatAll
	RepeatOne
)

// String returns "off", "all", or "one".
func (r Repeat) String() string {
	switch r {
	case RepeatAll:
		return "all"
	case RepeatOne:
		return "one"
	default:
		return "off"
	}
}

// Handle identifies one queue entry. The same track can be queued more than
// once; each entry has its own handle.
type Handle uint64

// Entry is a queued item with its handle.
type Entry[T any] struct {
	Handle Handle
	Item   T
}

// Snapshot is a consistent view of the queue for display.
type Snapshot[T any] struct {
	// History holds entries already played, oldest first.
	History []Entry[T]
	// Current is the entry playing or paused, if any.
	Current *Entry[T]
	// Upcoming holds entries still to play, in play order.
	Upcoming []Entry[T]
	Shuffle  bool
	Repeat   Repeat
}

// Queue is a play queue of items of type T.
type Queue[T any] struct {
	mu      sync.Mutex
	orig    []Handle // original (unshuffled) order
	play    []Handle // play order
	cur     int      // index into play; -1 when the queue is empty
	items   map[Handle]T
	next    Handle
	shuffle bool
	repeat  Repeat
	rng     *rand.Rand
}

// Option configures a Queue.
type Option func(*options)

type options struct{ rng *rand.Rand }

// WithRand sets the random source used for shuffling (for tests).
func WithRand(r *rand.Rand) Option {
	return func(o *options) { o.rng = r }
}

// New returns an empty queue.
func New[T any](opts ...Option) *Queue[T] {
	o := options{rng: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))}
	for _, opt := range opts {
		opt(&o)
	}
	return &Queue[T]{cur: -1, items: map[Handle]T{}, rng: o.rng}
}

func (q *Queue[T]) newEntry(item T) Handle {
	q.next++
	q.items[q.next] = item
	return q.next
}

// Set replaces the queue with items and makes items[start] current. History
// is cleared. With shuffle on, the entries after start are shuffled.
// It returns false if start is out of range (the queue is then empty).
func (q *Queue[T]) Set(items []T, start int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.orig, q.play, q.items, q.cur = nil, nil, map[Handle]T{}, -1
	if start < 0 || start >= len(items) {
		return false
	}
	for _, it := range items {
		h := q.newEntry(it)
		q.orig = append(q.orig, h)
	}
	q.play = slices.Clone(q.orig)
	q.cur = start
	if q.shuffle {
		// Keep the chosen track first and shuffle everything else after it.
		chosen := q.play[start]
		rest := append(slices.Clone(q.play[:start]), q.play[start+1:]...)
		q.shuffleSlice(rest)
		q.play = append([]Handle{chosen}, rest...)
		q.cur = 0
	}
	return true
}

// PlayNext inserts item right after the current entry. On an empty queue it
// becomes the current entry.
func (q *Queue[T]) PlayNext(item T) Handle {
	q.mu.Lock()
	defer q.mu.Unlock()
	h := q.newEntry(item)
	if q.cur < 0 {
		q.orig, q.play, q.cur = []Handle{h}, []Handle{h}, 0
		return h
	}
	cur := q.play[q.cur]
	q.play = slices.Insert(q.play, q.cur+1, h)
	q.orig = slices.Insert(q.orig, slices.Index(q.orig, cur)+1, h)
	return h
}

// Add appends item to the end of the queue. On an empty queue it becomes the
// current entry.
func (q *Queue[T]) Add(item T) Handle {
	q.mu.Lock()
	defer q.mu.Unlock()
	h := q.newEntry(item)
	q.orig = append(q.orig, h)
	q.play = append(q.play, h)
	if q.cur < 0 {
		q.cur = 0
	}
	return h
}

// Remove deletes an upcoming entry. It returns false if h is not upcoming
// (the current entry and history cannot be removed).
func (q *Queue[T]) Remove(h Handle) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	i := slices.Index(q.play, h)
	if i <= q.cur {
		return false
	}
	q.play = slices.Delete(q.play, i, i+1)
	if j := slices.Index(q.orig, h); j >= 0 {
		q.orig = slices.Delete(q.orig, j, j+1)
	}
	delete(q.items, h)
	return true
}

// Clear removes all upcoming entries. The current entry and history stay.
func (q *Queue[T]) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.cur < 0 {
		return
	}
	for _, h := range q.play[q.cur+1:] {
		delete(q.items, h)
	}
	q.play = q.play[:q.cur+1]
	q.orig = slices.DeleteFunc(q.orig, func(h Handle) bool { _, ok := q.items[h]; return !ok })
}

// Current returns the current item.
func (q *Queue[T]) Current() (T, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.currentLocked()
}

func (q *Queue[T]) currentLocked() (T, bool) {
	if q.cur < 0 {
		var zero T
		return zero, false
	}
	return q.items[q.play[q.cur]], true
}

// Advance moves on after the current track finishes. Repeat one replays the
// current track, repeat all wraps to the start, and repeat off returns false
// at the end of the queue, leaving the last track current.
func (q *Queue[T]) Advance() (T, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.repeat == RepeatOne {
		return q.currentLocked()
	}
	return q.stepLocked()
}

// Next skips to the next track at the user's request. It behaves like
// Advance, except that repeat one moves on (and wraps like repeat all).
func (q *Queue[T]) Next() (T, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.stepLocked()
}

func (q *Queue[T]) stepLocked() (T, bool) {
	var zero T
	switch {
	case q.cur < 0:
		return zero, false
	case q.cur+1 < len(q.play):
		q.cur++
	case q.repeat == RepeatOff:
		return zero, false
	default:
		// Wrap. With shuffle on, play the whole queue in a fresh order.
		if q.shuffle {
			q.shuffleSlice(q.play)
		}
		q.cur = 0
	}
	return q.items[q.play[q.cur]], true
}

// Previous moves to the previous track. At the first entry it returns the
// current track so the caller restarts it. It returns false only when the
// queue is empty.
func (q *Queue[T]) Previous() (T, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.cur > 0 {
		q.cur--
	}
	return q.currentLocked()
}

// Jump makes the entry h current. It returns false if h is not queued.
func (q *Queue[T]) Jump(h Handle) (T, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	i := slices.Index(q.play, h)
	if i < 0 {
		var zero T
		return zero, false
	}
	q.cur = i
	return q.items[h], true
}

// SetShuffle turns shuffle on or off. Turning it on shuffles the upcoming
// entries; turning it off restores the not-yet-played entries to their
// original relative order. History and the current entry do not move.
func (q *Queue[T]) SetShuffle(on bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if on == q.shuffle {
		return
	}
	q.shuffle = on
	if q.cur < 0 {
		return
	}
	if on {
		q.shuffleSlice(q.play[q.cur+1:])
		return
	}
	played := make(map[Handle]bool, q.cur+1)
	for _, h := range q.play[:q.cur+1] {
		played[h] = true
	}
	upcoming := slices.DeleteFunc(slices.Clone(q.orig), func(h Handle) bool { return played[h] })
	q.play = append(q.play[:q.cur+1], upcoming...)
}

// Shuffle reports whether shuffle is on.
func (q *Queue[T]) Shuffle() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.shuffle
}

// SetRepeat sets the repeat mode.
func (q *Queue[T]) SetRepeat(r Repeat) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.repeat = r
}

// CycleRepeat steps off -> all -> one -> off and returns the new mode.
func (q *Queue[T]) CycleRepeat() Repeat {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.repeat = (q.repeat + 1) % 3
	return q.repeat
}

// Repeat returns the repeat mode.
func (q *Queue[T]) Repeat() Repeat {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.repeat
}

// Len returns the number of entries, including history and the current one.
func (q *Queue[T]) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.play)
}

// Snapshot returns a consistent copy of the queue state.
func (q *Queue[T]) Snapshot() Snapshot[T] {
	q.mu.Lock()
	defer q.mu.Unlock()
	s := Snapshot[T]{Shuffle: q.shuffle, Repeat: q.repeat}
	if q.cur < 0 {
		return s
	}
	entry := func(h Handle) Entry[T] { return Entry[T]{Handle: h, Item: q.items[h]} }
	for _, h := range q.play[:q.cur] {
		s.History = append(s.History, entry(h))
	}
	c := entry(q.play[q.cur])
	s.Current = &c
	for _, h := range q.play[q.cur+1:] {
		s.Upcoming = append(s.Upcoming, entry(h))
	}
	return s
}

func (q *Queue[T]) shuffleSlice(hs []Handle) {
	q.rng.Shuffle(len(hs), func(i, j int) { hs[i], hs[j] = hs[j], hs[i] })
}
