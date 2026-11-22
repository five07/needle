package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sync"
	"time"
)

// ErrIPCClosed means the connection to mpv is gone.
var ErrIPCClosed = errors.New("player: mpv connection closed")

// ipcEvent is an asynchronous message from mpv.
type ipcEvent struct {
	Event           string          `json:"event"`
	Name            string          `json:"name"`
	ID              int64           `json:"id"`
	Data            json.RawMessage `json:"data"`
	Reason          string          `json:"reason"`
	FileError       string          `json:"file_error"`
	PlaylistEntryID int64           `json:"playlist_entry_id"`
	Prefix          string          `json:"prefix"`
	Level           string          `json:"level"`
	Text            string          `json:"text"`
}

type ipcReply struct {
	data json.RawMessage
	err  error
}

// ipcConn is a JSON IPC connection to mpv. Commands may be sent from any
// goroutine; events are delivered in order on events.
type ipcConn struct {
	conn net.Conn

	wmu sync.Mutex // serializes writes

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan ipcReply
	closed  bool

	events chan ipcEvent
	done   chan struct{}
}

// dialIPC connects to the socket at path, retrying until it appears, ctx
// ends, or exited is closed (the process died first).
func dialIPC(ctx context.Context, path string, exited <-chan struct{}) (*ipcConn, error) {
	var d net.Dialer
	for {
		conn, err := d.DialContext(ctx, "unix", path)
		if err == nil {
			c := &ipcConn{
				conn:    conn,
				pending: map[int64]chan ipcReply{},
				events:  make(chan ipcEvent, 64),
				done:    make(chan struct{}),
			}
			go c.readLoop()
			return c, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("player: connect to mpv: %w", ctx.Err())
		case <-exited:
			return nil, errors.New("player: mpv exited during startup")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// command sends one command and waits for its reply.
func (c *ipcConn) command(ctx context.Context, args ...any) (json.RawMessage, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrIPCClosed
	}
	c.nextID++
	id := c.nextID
	ch := make(chan ipcReply, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	b, err := json.Marshal(struct {
		Command   []any `json:"command"`
		RequestID int64 `json:"request_id"`
	}{args, id})
	if err != nil {
		c.forget(id)
		return nil, fmt.Errorf("player: encode mpv command: %w", err)
	}
	c.wmu.Lock()
	_, err = c.conn.Write(append(b, '\n'))
	c.wmu.Unlock()
	if err != nil {
		c.forget(id)
		return nil, fmt.Errorf("%w: %w", ErrIPCClosed, err)
	}
	select {
	case r := <-ch:
		return r.data, r.err
	case <-ctx.Done():
		c.forget(id)
		return nil, ctx.Err()
	case <-c.done:
		return nil, ErrIPCClosed
	}
}

func (c *ipcConn) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *ipcConn) readLoop() {
	defer c.shutdown()
	sc := bufio.NewScanner(c.conn)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		var msg struct {
			ipcEvent
			RequestID *int64 `json:"request_id"`
			Error     string `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) != nil {
			continue
		}
		if msg.Event != "" {
			select {
			case c.events <- msg.ipcEvent:
			case <-c.done:
				return
			}
			continue
		}
		if msg.RequestID == nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[*msg.RequestID]
		delete(c.pending, *msg.RequestID)
		c.mu.Unlock()
		if ch == nil {
			continue
		}
		r := ipcReply{data: msg.Data}
		if msg.Error != "success" {
			r.err = fmt.Errorf("player: mpv: %s", redact(msg.Error))
		}
		ch <- r
	}
	// A scan error and EOF end the connection the same way: shutdown wakes
	// every waiter with ErrIPCClosed.
	_ = sc.Err()
}

func (c *ipcConn) shutdown() {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		close(c.done)
	}
	c.mu.Unlock()
	_ = c.conn.Close()
}

// close closes the connection; readLoop then shuts down.
func (c *ipcConn) close() { _ = c.conn.Close() }

var urlPattern = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s'"]+`)

// redact removes URLs, which may carry signatures, from mpv text.
func redact(s string) string {
	s = urlPattern.ReplaceAllString(s, "<url>")
	if r := []rune(s); len(r) > 300 {
		s = string(r[:300]) + "..."
	}
	return s
}
