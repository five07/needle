package queue

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"testing"
)

// render shows a queue as "history [current] upcoming | flags".
func render(q *Queue[string]) string {
	s := q.Snapshot()
	names := func(es []Entry[string]) string {
		var out []string
		for _, e := range es {
			out = append(out, e.Item)
		}
		return strings.Join(out, ",")
	}
	cur := "-"
	if s.Current != nil {
		cur = s.Current.Item
	}
	return fmt.Sprintf("%s [%s] %s", names(s.History), cur, names(s.Upcoming))
}

func newQueue(items ...string) *Queue[string] {
	q := New[string](WithRand(rand.New(rand.NewPCG(1, 2))))
	if len(items) > 0 {
		q.Set(items, 0)
	}
	return q
}

func TestEmptyQueue(t *testing.T) {
	q := newQueue()
	if _, ok := q.Current(); ok {
		t.Error("Current on empty queue ok")
	}
	for name, f := range map[string]func() (string, bool){"Next": q.Next, "Advance": q.Advance, "Previous": q.Previous} {
		if _, ok := f(); ok {
			t.Errorf("%s on empty queue ok", name)
		}
	}
	q.Clear()
	q.SetShuffle(true)
	if q.Remove(1) {
		t.Error("Remove on empty queue ok")
	}
	if _, ok := q.Jump(1); ok {
		t.Error("Jump on empty queue ok")
	}
	if got := render(q); got != " [-] " {
		t.Errorf("render = %q", got)
	}
}

func TestSet(t *testing.T) {
	tests := []struct {
		name   string
		items  []string
		start  int
		wantOK bool
		want   string
	}{
		{"start at first", []string{"a", "b", "c"}, 0, true, " [a] b,c"},
		{"start in the middle keeps earlier items as history", []string{"a", "b", "c"}, 1, true, "a [b] c"},
		{"start out of range", []string{"a"}, 1, false, " [-] "},
		{"negative start", []string{"a"}, -1, false, " [-] "},
		{"no items", nil, 0, false, " [-] "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newQueue("x", "y")
			if ok := q.Set(tt.items, tt.start); ok != tt.wantOK {
				t.Errorf("Set ok = %v, want %v", ok, tt.wantOK)
			}
			if got := render(q); got != tt.want {
				t.Errorf("render = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPlayNextAndAdd(t *testing.T) {
	tests := []struct {
		name string
		ops  func(q *Queue[string])
		want string
	}{
		{"add appends to the end", func(q *Queue[string]) { q.Add("d") }, " [a] b,c,d"},
		{"play next inserts after current", func(q *Queue[string]) { q.PlayNext("x") }, " [a] x,b,c"},
		{"repeated play next stacks most recent first", func(q *Queue[string]) { q.PlayNext("x"); q.PlayNext("y") }, " [a] y,x,b,c"},
		{"play next after moving on", func(q *Queue[string]) { q.Next(); q.PlayNext("x") }, "a [b] x,c"},
		{"duplicates are separate entries", func(q *Queue[string]) { q.Add("a") }, " [a] b,c,a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newQueue("a", "b", "c")
			tt.ops(q)
			if got := render(q); got != tt.want {
				t.Errorf("render = %q, want %q", got, tt.want)
			}
		})
	}

	for _, name := range []string{"Add", "PlayNext"} {
		t.Run(name+" on empty queue becomes current", func(t *testing.T) {
			q := newQueue()
			if name == "Add" {
				q.Add("a")
			} else {
				q.PlayNext("a")
			}
			if got := render(q); got != " [a] " {
				t.Errorf("render = %q", got)
			}
		})
	}
}

func TestNavigation(t *testing.T) {
	tests := []struct {
		name    string
		repeat  Repeat
		steps   []string // "next", "advance", "prev"
		wantLog string   // item returned by each step, "!" for ok=false
		want    string
	}{
		{"next walks forward", RepeatOff, []string{"next", "next"}, "b,c", "a,b [c] "},
		{"repeat off stops at the end", RepeatOff, []string{"next", "next", "advance", "next"}, "b,c,!,!", "a,b [c] "},
		{"repeat all wraps", RepeatAll, []string{"next", "next", "advance"}, "b,c,a", " [a] b,c"},
		{"repeat all wraps on next too", RepeatAll, []string{"next", "next", "next"}, "b,c,a", " [a] b,c"},
		{"repeat one replays on advance", RepeatOne, []string{"advance", "advance"}, "a,a", " [a] b,c"},
		{"repeat one still skips on next and wraps", RepeatOne, []string{"next", "next", "next"}, "b,c,a", " [a] b,c"},
		{"previous goes back", RepeatOff, []string{"next", "next", "prev"}, "b,c,b", "a [b] c"},
		{"previous at the first track restarts it", RepeatOff, []string{"prev", "prev"}, "a,a", " [a] b,c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newQueue("a", "b", "c")
			q.SetRepeat(tt.repeat)
			var log []string
			for _, s := range tt.steps {
				var item string
				var ok bool
				switch s {
				case "next":
					item, ok = q.Next()
				case "advance":
					item, ok = q.Advance()
				case "prev":
					item, ok = q.Previous()
				}
				if !ok {
					item = "!"
				}
				log = append(log, item)
			}
			if got := strings.Join(log, ","); got != tt.wantLog {
				t.Errorf("returned %q, want %q", got, tt.wantLog)
			}
			if got := render(q); got != tt.want {
				t.Errorf("render = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRemoveAndClear(t *testing.T) {
	q := newQueue("a", "b", "c", "d")
	q.Next() // a [b] c,d
	s := q.Snapshot()

	if q.Remove(s.History[0].Handle) {
		t.Error("removed a history entry")
	}
	if q.Remove(s.Current.Handle) {
		t.Error("removed the current entry")
	}
	if !q.Remove(s.Upcoming[0].Handle) {
		t.Error("could not remove upcoming entry c")
	}
	if q.Remove(s.Upcoming[0].Handle) {
		t.Error("removed the same entry twice")
	}
	if got := render(q); got != "a [b] d" {
		t.Errorf("after remove = %q", got)
	}

	q.Add("e")
	q.Clear()
	if got := render(q); got != "a [b] " {
		t.Errorf("after clear = %q", got)
	}
	if cur, ok := q.Current(); !ok || cur != "b" {
		t.Errorf("clear stopped the current track: %q, %v", cur, ok)
	}
	if q.Len() != 2 {
		t.Errorf("Len = %d, want 2", q.Len())
	}
	// Removed entries must not come back when shuffle is toggled.
	q.SetShuffle(true)
	q.SetShuffle(false)
	if got := render(q); got != "a [b] " {
		t.Errorf("after shuffle toggle = %q", got)
	}
}

func TestShuffle(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	q := newQueue(items...)
	q.Next() // a [b] c..h

	q.SetShuffle(true)
	if !q.Shuffle() {
		t.Fatal("Shuffle() = false after SetShuffle(true)")
	}
	s := q.Snapshot()
	if s.History[0].Item != "a" || s.Current.Item != "b" {
		t.Fatalf("shuffle moved history or current: %s", render(q))
	}
	var up []string
	for _, e := range s.Upcoming {
		up = append(up, e.Item)
	}
	if strings.Join(up, "") == "cdefgh" {
		t.Errorf("upcoming not shuffled: %v", up)
	}
	sorted := slices.Sorted(slices.Values(up))
	if strings.Join(sorted, "") != "cdefgh" {
		t.Errorf("shuffle changed the set of upcoming items: %v", up)
	}

	before := render(q)
	q.SetShuffle(true) // already on: must not reshuffle
	if after := render(q); after != before {
		t.Errorf("repeated SetShuffle(true) reshuffled: %q -> %q", before, after)
	}

	// Play two shuffled tracks, then turn shuffle off: the rest come back in
	// original order, skipping what was already played.
	first, _ := q.Next()
	second, _ := q.Next()
	q.SetShuffle(false)
	want := []string{}
	for _, it := range items[2:] {
		if it != first && it != second {
			want = append(want, it)
		}
	}
	s = q.Snapshot()
	var got []string
	for _, e := range s.Upcoming {
		got = append(got, e.Item)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("unshuffled upcoming = %v, want %v", got, want)
	}
	if s.Current.Item != second {
		t.Errorf("current changed on unshuffle: %q, want %q", s.Current.Item, second)
	}
}

func TestShuffleWithInsertedItems(t *testing.T) {
	q := newQueue("a", "b", "c")
	q.SetShuffle(true)
	q.PlayNext("x") // goes right after the current entry, even when shuffled
	if s := q.Snapshot(); s.Upcoming[0].Item != "x" {
		t.Errorf("play next while shuffled = %s", render(q))
	}
	q.Add("z")
	q.SetShuffle(false)
	if got := render(q); got != " [a] x,b,c,z" {
		t.Errorf("unshuffled = %q, want inserted items in their original positions", got)
	}
}

func TestSetWhileShuffled(t *testing.T) {
	q := newQueue()
	q.SetShuffle(true)
	q.Set([]string{"a", "b", "c", "d", "e"}, 2)
	s := q.Snapshot()
	if s.Current.Item != "c" || len(s.History) != 0 || len(s.Upcoming) != 4 {
		t.Errorf("shuffled Set = %s, want c current with the other four upcoming", render(q))
	}
}

func TestRepeatAllWithShuffleReshuffles(t *testing.T) {
	q := newQueue("a", "b", "c", "d", "e", "f")
	q.SetShuffle(true)
	q.SetRepeat(RepeatAll)
	seen := map[string]bool{}
	for range 6 {
		cur, _ := q.Current()
		seen[cur] = true
		q.Advance()
	}
	if len(seen) != 6 {
		t.Errorf("a full pass played %d distinct tracks, want 6", len(seen))
	}
	if s := q.Snapshot(); len(s.History) != 0 || len(s.Upcoming) != 5 {
		t.Errorf("after wrap = %s", render(q))
	}
}

func TestJump(t *testing.T) {
	q := newQueue("a", "b", "c")
	h := q.Snapshot().Upcoming[1].Handle
	if item, ok := q.Jump(h); !ok || item != "c" {
		t.Errorf("Jump = %q, %v", item, ok)
	}
	if got := render(q); got != "a,b [c] " {
		t.Errorf("render = %q", got)
	}
	if _, ok := q.Jump(999); ok {
		t.Error("Jump to unknown handle ok")
	}
}

func TestRepeatModes(t *testing.T) {
	q := newQueue()
	want := []Repeat{RepeatAll, RepeatOne, RepeatOff, RepeatAll}
	for i, w := range want {
		if got := q.CycleRepeat(); got != w {
			t.Errorf("cycle %d = %v, want %v", i, got, w)
		}
	}
	if q.Repeat() != RepeatAll {
		t.Errorf("Repeat() = %v", q.Repeat())
	}
	names := map[Repeat]string{RepeatOff: "off", RepeatAll: "all", RepeatOne: "one", Repeat(9): "off"}
	for r, n := range names {
		if r.String() != n {
			t.Errorf("%d.String() = %q, want %q", r, r.String(), n)
		}
	}
}

func TestNewUsesDefaultRand(t *testing.T) {
	q := New[int]()
	q.Set([]int{1, 2, 3}, 0)
	q.SetShuffle(true)
	if q.Len() != 3 {
		t.Errorf("Len = %d", q.Len())
	}
}

func TestConcurrentUse(t *testing.T) {
	q := newQueue("a", "b", "c")
	var wg sync.WaitGroup
	for w := range 8 {
		wg.Go(func() {
			for i := range 200 {
				switch (w + i) % 9 {
				case 0:
					q.Add("x")
				case 1:
					q.PlayNext("y")
				case 2:
					q.Next()
				case 3:
					q.Previous()
				case 4:
					q.SetShuffle(i%2 == 0)
				case 5:
					q.CycleRepeat()
				case 6:
					if s := q.Snapshot(); len(s.Upcoming) > 0 {
						q.Remove(s.Upcoming[0].Handle)
					}
				case 7:
					q.Advance()
				case 8:
					if i%50 == 0 {
						q.Clear()
					}
					q.Current()
				}
			}
		})
	}
	wg.Wait()
	s := q.Snapshot()
	if s.Current == nil {
		t.Fatal("queue lost its current entry")
	}
	if got := len(s.History) + 1 + len(s.Upcoming); got != q.Len() {
		t.Errorf("snapshot has %d entries, Len = %d", got, q.Len())
	}
}
