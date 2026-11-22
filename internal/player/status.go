package player

import "sync"

// statusBox holds a Status snapshot shared between the Run goroutine and
// readers. Pointer fields are copied so readers never share memory with the
// loop.
type statusBox struct {
	mu sync.Mutex
	s  Status
}

func (b *statusBox) set(s Status) {
	s = cloneStatus(s)
	b.mu.Lock()
	b.s = s
	b.mu.Unlock()
}

func (b *statusBox) get() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	return cloneStatus(b.s)
}

func cloneStatus(s Status) Status {
	if s.Track != nil {
		t := *s.Track
		s.Track = &t
	}
	if s.Source != nil {
		src := *s.Source
		s.Source = &src
	}
	if s.Output != nil {
		o := *s.Output
		s.Output = &o
	}
	return s
}
