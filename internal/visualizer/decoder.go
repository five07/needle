package visualizer

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// maxBuffered caps the decoded samples held ahead of the display (2 s).
// When it is full the reader stops, the pipe fills, and mpv waits, so the
// decode never runs far ahead.
const maxBuffered = 2 * SampleRate

// decoderArgs returns the command line of the analysis mpv: user config,
// scripts, and filters off; decoded audio is downmixed to mono 16-bit at
// SampleRate and written as raw PCM to stdout instead of a sound device.
func decoderArgs(file string, start time.Duration) []string {
	return []string{
		"--no-config", "--no-video", "--no-terminal", "--idle=no",
		"--load-scripts=no", "--ytdl=no", "--load-auto-profiles=no", "--audio-display=no",
		"--ao=pcm", "--ao-pcm-file=/dev/stdout", "--ao-pcm-waveheader=no",
		"--audio-format=s16", "--audio-samplerate=" + strconv.Itoa(SampleRate), "--audio-channels=mono",
		"--af=", "--replaygain=no", "--volume=100",
		"--start=" + strconv.FormatFloat(max(start.Seconds(), 0), 'f', 3, 64),
		// The MPD is a local file; its segments are absolute https URLs.
		"--demuxer-lavf-o=protocol_whitelist=[file,http,https,tls,tcp,crypto,data]",
		"--", file,
	}
}

// mpvDecoder is one running analysis decode.
type mpvDecoder struct {
	cmd  *exec.Cmd
	file string

	mu     sync.Mutex
	cond   *sync.Cond
	buf    []int16
	closed bool
	done   chan struct{} // closed when the reader stops
}

// startDecoder writes mpd to a 0600 file in dir and starts mpv decoding it
// from start.
func startDecoder(ctx context.Context, binary, dir string, seq uint64, mpd []byte, start time.Duration, logger *slog.Logger) (*mpvDecoder, error) {
	file := filepath.Join(dir, fmt.Sprintf("vis-%d-%d.mpd", os.Getpid(), seq))
	if err := os.WriteFile(file, mpd, 0o600); err != nil {
		return nil, fmt.Errorf("visualizer: %w", err)
	}
	cmd := exec.CommandContext(ctx, binary, decoderArgs(file, start)...)
	// The kernel stops it if needle dies, like the playing mpv.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	out, err := cmd.StdoutPipe()
	if err != nil {
		os.Remove(file)
		return nil, fmt.Errorf("visualizer: %w", err)
	}
	if err := cmd.Start(); err != nil {
		os.Remove(file)
		return nil, fmt.Errorf("visualizer: start %s: %w", binary, err)
	}
	d := &mpvDecoder{cmd: cmd, file: file, done: make(chan struct{})}
	d.cond = sync.NewCond(&d.mu)
	go d.read(out, logger)
	return d, nil
}

// read moves samples from mpv into buf, waiting while buf is full.
func (d *mpvDecoder) read(out io.Reader, logger *slog.Logger) {
	defer close(d.done)
	r := bufio.NewReaderSize(out, 8192)
	chunk := make([]byte, 4096)
	odd := []byte(nil) // a sample split across reads
	for {
		n, err := r.Read(chunk)
		data := append(odd, chunk[:n]...)
		samples := len(data) / 2
		odd = append([]byte(nil), data[samples*2:]...)
		d.mu.Lock()
		for !d.closed && len(d.buf) >= maxBuffered {
			d.cond.Wait()
		}
		if d.closed {
			d.mu.Unlock()
			return
		}
		for i := range samples {
			d.buf = append(d.buf, int16(binary.LittleEndian.Uint16(data[2*i:])))
		}
		d.mu.Unlock()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				logger.Debug("visualizer decoder stopped", "err", err)
			}
			return
		}
	}
}

// Take removes and returns up to n decoded samples.
func (d *mpvDecoder) Take(n int) []int16 {
	d.mu.Lock()
	defer d.mu.Unlock()
	n = min(max(n, 0), len(d.buf))
	out := append([]int16(nil), d.buf[:n]...)
	d.buf = d.buf[n:]
	d.cond.Broadcast()
	return out
}

// Close stops mpv and removes its manifest copy.
func (d *mpvDecoder) Close() error {
	d.mu.Lock()
	d.closed = true
	d.cond.Broadcast()
	d.mu.Unlock()
	if d.cmd.Process != nil {
		d.cmd.Process.Kill()
	}
	d.cmd.Wait() // also closes stdout, which ends read
	<-d.done
	return os.Remove(d.file)
}
