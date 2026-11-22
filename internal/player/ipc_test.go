package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net"
	"strings"
	"testing"
	"time"
)

// pipeIPC returns a client and the server side of an in-memory connection.
func pipeIPC(t *testing.T) (*ipcConn, net.Conn, *bufio.Reader) {
	t.Helper()
	client, server := net.Pipe()
	c := &ipcConn{conn: client, pending: map[int64]chan ipcReply{}, events: make(chan ipcEvent, 64), done: make(chan struct{})}
	go c.readLoop()
	t.Cleanup(func() { c.shutdown(); server.Close() })
	return c, server, bufio.NewReader(server)
}

func TestIPCReplies(t *testing.T) {
	c, server, r := pipeIPC(t)
	go func() {
		for i := 0; ; i++ {
			line, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			var req struct {
				RequestID int64 `json:"request_id"`
			}
			json.Unmarshal(line, &req)
			switch i {
			case 0:
				// Noise first: garbage, a reply nobody waits for, a bare message.
				server.Write([]byte("not json\n{\"request_id\":999,\"error\":\"success\"}\n{\"foo\":1}\n"))
				server.Write([]byte(`{"request_id":1,"error":"success","data":42}` + "\n"))
			case 1:
				server.Write([]byte(`{"request_id":2,"error":"property unavailable https://x.example/?Signature=S"}` + "\n"))
			}
		}
	}()
	data, err := c.command(context.Background(), "get_property", "volume")
	if err != nil || string(data) != "42" {
		t.Fatalf("command = %s %v", data, err)
	}
	_, err = c.command(context.Background(), "get_property", "nope")
	if err == nil || !strings.Contains(err.Error(), "property unavailable") || strings.Contains(err.Error(), "Signature") {
		t.Errorf("error reply = %v", err)
	}
}

func TestIPCCommandFailures(t *testing.T) {
	c, server, r := pipeIPC(t)
	if _, err := c.command(context.Background(), math.Inf(1)); err == nil || !strings.Contains(err.Error(), "encode") {
		t.Errorf("encode err = %v", err)
	}
	// One server goroutine: ignore the first request, then hang up after
	// reading the second.
	go func() {
		r.ReadBytes('\n')
		r.ReadBytes('\n')
		server.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.command(ctx, "stop"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("timeout err = %v", err)
	}
	c.mu.Lock()
	n := len(c.pending)
	c.mu.Unlock()
	if n != 0 {
		t.Errorf("pending = %d after timeout", n)
	}
	// The server goes away while a command waits.
	if _, err := c.command(context.Background(), "stop"); !errors.Is(err, ErrIPCClosed) {
		t.Errorf("closed err = %v", err)
	}
	if _, err := c.command(context.Background(), "stop"); !errors.Is(err, ErrIPCClosed) {
		t.Errorf("after close err = %v", err)
	}
}

func TestIPCWriteFailure(t *testing.T) {
	client, server := net.Pipe()
	server.Close()
	c := &ipcConn{conn: client, pending: map[int64]chan ipcReply{}, events: make(chan ipcEvent, 1), done: make(chan struct{})}
	if _, err := c.command(context.Background(), "stop"); !errors.Is(err, ErrIPCClosed) {
		t.Errorf("write err = %v", err)
	}
	if len(c.pending) != 0 {
		t.Error("pending entry left after a write failure")
	}
}

func TestIPCEventsStopOnShutdown(t *testing.T) {
	c, server, _ := pipeIPC(t)
	go func() {
		for range 100 { // more than the buffer, nobody reads
			if _, err := server.Write([]byte(`{"event":"idle"}` + "\n")); err != nil {
				return
			}
		}
	}()
	waitFor(t, func() bool { return len(c.events) == cap(c.events) })
	c.shutdown()
	select {
	case <-c.done:
	case <-time.After(time.Second):
		t.Fatal("read loop did not stop")
	}
}
