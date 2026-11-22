package player

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// The test binary doubles as a fake mpv when FAKE_MPV=1. FAKE_MPV_MODE picks
// a scenario; FAKE_MPV_LOG receives one line per command and the argv.
//
// Modes:
//
//	normal        tracks play for FAKE_MPV_TRACK_MS (default 150) and end with eof
//	nosocket      exit before creating the socket
//	hang          never create the socket
//	crash-on-load exit 1 on the first loadfile
//	busy          loading fails with an ALSA "Device or resource busy" log line
//	busy-then-fail the first load is busy, later loads fail without the busy line
//	stream-error  playback fails 50 ms after starting
//	ignore-quit   reply to quit but keep running
//	old-mpv       loadfile returns no playlist entry id
//	bad-mpd       reject manifest files that are not mode 0600
func fakeMPVMain() {
	mode := os.Getenv("FAKE_MPV_MODE")
	var socket string
	for _, a := range os.Args[1:] {
		if v, ok := strings.CutPrefix(a, "--input-ipc-server="); ok {
			socket = v
		}
	}
	logf := func(format string, args ...any) {
		if p := os.Getenv("FAKE_MPV_LOG"); p != "" {
			f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err == nil {
				fmt.Fprintf(f, format+"\n", args...)
				f.Close()
			}
		}
	}
	logf("argv %s", strings.Join(os.Args[1:], " "))
	switch mode {
	case "nosocket":
		os.Exit(3)
	case "hang":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		os.Exit(4)
	}
	conn, err := ln.Accept()
	if err != nil {
		os.Exit(5)
	}
	f := &fakeMPV{conn: conn, mode: mode, logf: logf, trackLen: 150 * time.Millisecond}
	if ms := os.Getenv("FAKE_MPV_TRACK_MS"); ms != "" {
		var n int
		fmt.Sscan(ms, &n)
		f.trackLen = time.Duration(n) * time.Millisecond
	}
	f.serve()
}

type fakeEntry struct {
	id   int64
	file string
}

type fakeMPV struct {
	conn     net.Conn
	mode     string
	logf     func(string, ...any)
	trackLen time.Duration

	mu       sync.Mutex
	nextID   int64
	playlist []fakeEntry
	current  *fakeEntry
	started  time.Time
	gen      int // invalidates timers of replaced tracks
	loads    int
	starts   int
}

func (f *fakeMPV) send(v any) {
	b, _ := json.Marshal(v)
	f.conn.Write(append(b, '\n'))
}

func (f *fakeMPV) reply(id any, data any, errText string) {
	if errText == "" {
		errText = "success"
	}
	f.send(map[string]any{"request_id": id, "error": errText, "data": data})
}

func (f *fakeMPV) serve() {
	sc := bufio.NewScanner(f.conn)
	for sc.Scan() {
		var req struct {
			Command   []any `json:"command"`
			RequestID any   `json:"request_id"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil || len(req.Command) == 0 {
			continue
		}
		name, _ := req.Command[0].(string)
		f.logf("cmd %s", sc.Text())
		switch name {
		case "observe_property", "request_log_messages", "set_property":
			f.reply(req.RequestID, nil, "")
		case "get_property":
			f.getProperty(req.RequestID, req.Command[1].(string))
		case "loadfile":
			f.loadfile(req.RequestID, req.Command)
		case "playlist-clear":
			f.mu.Lock()
			f.playlist = nil
			f.mu.Unlock()
			f.reply(req.RequestID, nil, "")
		case "stop":
			f.mu.Lock()
			f.playlist = nil
			cur := f.current
			f.current = nil
			f.gen++
			f.mu.Unlock()
			f.reply(req.RequestID, nil, "")
			if cur != nil {
				f.send(map[string]any{"event": "end-file", "reason": "stop", "playlist_entry_id": cur.id})
			}
		case "quit":
			f.reply(req.RequestID, nil, "")
			if f.mode != "ignore-quit" {
				os.Exit(0)
			}
		default:
			f.reply(req.RequestID, nil, "invalid parameter")
		}
	}
	_ = sc.Err()
	os.Exit(0)
}

func (f *fakeMPV) getProperty(id any, prop string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case prop == "time-pos" && f.current != nil:
		f.reply(id, time.Since(f.started).Seconds()+12, "")
	case prop == "duration" && f.current != nil:
		f.reply(id, 29.994, "")
	default:
		f.reply(id, nil, "property unavailable")
	}
}

func (f *fakeMPV) loadfile(id any, cmd []any) {
	file, _ := cmd[1].(string)
	flag, _ := cmd[2].(string)
	f.mu.Lock()
	f.loads++
	loads := f.loads
	f.mu.Unlock()
	if f.mode == "crash-on-load" && loads == 1 {
		os.Exit(1)
	}
	info, err := os.Stat(file)
	if err != nil || (f.mode == "bad-mpd" && info.Mode().Perm() != 0o600) || info.Mode().Perm() != 0o600 {
		f.reply(id, nil, "loading failed")
		return
	}
	if f.mode == "old-mpv" {
		f.reply(id, nil, "")
		return
	}
	f.mu.Lock()
	f.nextID++
	e := fakeEntry{id: f.nextID, file: file}
	var stopped *fakeEntry
	start := false
	switch flag {
	case "replace":
		stopped = f.current
		f.current = nil
		f.playlist = []fakeEntry{e}
		start = true
	default: // append
		f.playlist = append(f.playlist, e)
	}
	f.mu.Unlock()
	f.reply(id, map[string]any{"playlist_entry_id": e.id}, "")
	if stopped != nil {
		f.send(map[string]any{"event": "end-file", "reason": "stop", "playlist_entry_id": stopped.id})
	}
	if start {
		f.startNext()
	}
}

// startNext plays the head of the playlist, if any.
func (f *fakeMPV) startNext() {
	f.mu.Lock()
	if len(f.playlist) == 0 {
		f.current = nil
		f.mu.Unlock()
		return
	}
	e := f.playlist[0]
	f.playlist = f.playlist[1:]
	f.current = &e
	f.started = time.Now()
	f.gen++
	gen := f.gen
	f.mu.Unlock()

	f.send(map[string]any{"event": "start-file", "playlist_entry_id": e.id})
	f.mu.Lock()
	f.starts++
	starts := f.starts
	f.mu.Unlock()
	if f.mode == "busy-then-fail" && starts > 1 {
		f.end(e.id, gen, "error", "audio output initialization failed")
		return
	}
	if f.mode == "busy" || f.mode == "busy-then-fail" {
		f.send(map[string]any{"event": "log-message", "prefix": "ao/alsa", "level": "error",
			"text": "Playback open error: Device or resource busy\n"})
		f.end(e.id, gen, "error", "audio output initialization failed")
		return
	}
	params := map[string]any{"samplerate": 192000, "channel-count": 2, "format": "s32"}
	f.send(map[string]any{"event": "property-change", "id": 1, "name": "audio-params", "data": params})
	f.send(map[string]any{"event": "property-change", "id": 2, "name": "audio-out-params", "data": params})
	f.send(map[string]any{"event": "property-change", "id": 3, "name": "volume", "data": 100.0})
	f.send(map[string]any{"event": "property-change", "id": 4, "name": "af", "data": []any{}})
	f.send(map[string]any{"event": "log-message", "prefix": "ffmpeg", "level": "warn",
		"text": "Opening https://sp-ad-cf.audio.tidal.com/x.mp4?Signature=SECRETSIG"})
	go func() {
		if f.mode == "stream-error" {
			time.Sleep(50 * time.Millisecond)
			f.end(e.id, gen, "error", "loading failed: https://sp-ad-cf.audio.tidal.com/y?Signature=SECRETSIG")
			return
		}
		time.Sleep(f.trackLen)
		f.end(e.id, gen, "eof", "")
	}()
}

func (f *fakeMPV) end(id int64, gen int, reason, fileError string) {
	f.mu.Lock()
	if gen != f.gen {
		f.mu.Unlock()
		return
	}
	f.current = nil
	f.mu.Unlock()
	ev := map[string]any{"event": "end-file", "reason": reason, "playlist_entry_id": id}
	if fileError != "" {
		ev["file_error"] = fileError
	}
	f.send(ev)
	f.startNext()
}
