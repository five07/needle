package visualizer

import (
	"context"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

// fakeMPV writes a script that records its arguments and prints out.
func fakeMPV(t *testing.T, out string) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	bin = filepath.Join(dir, "mpv")
	script := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\" >> " + argsFile + "; done\n" + out + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, argsFile
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDecoderArgs(t *testing.T) {
	args := decoderArgs("/run/x.mpd", 90500*time.Millisecond)
	for _, want := range []string{"--no-config", "--load-scripts=no", "--ao=pcm", "--ao-pcm-file=/dev/stdout", "--ao-pcm-waveheader=no",
		"--audio-format=s16", "--audio-samplerate=22050", "--audio-channels=mono", "--af=", "--start=90.500"} {
		if !slices.Contains(args, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}
	// The file comes after "--", so a name can never be read as an option.
	if args[len(args)-2] != "--" || args[len(args)-1] != "/run/x.mpd" {
		t.Errorf("file: %v", args[len(args)-2:])
	}
	if a := decoderArgs("f", -time.Second); !slices.Contains(a, "--start=0.000") {
		t.Errorf("negative start: %v", a)
	}
}

func TestDecoderReadsPCM(t *testing.T) {
	// Samples 1, -2, then half of a third: the odd byte is kept for the
	// next read, not dropped or misaligned.
	bin, argsFile := fakeMPV(t, `printf '\001\000\376\377\003'; sleep 0.1; printf '\000'; sleep 5`)
	dir := t.TempDir()
	d, err := startDecoder(context.Background(), bin, dir, 1, []byte("<MPD/>"), time.Second, discard())
	if err != nil {
		t.Fatal(err)
	}
	var got []int16
	waitFor(t, func() bool { got = append(got, d.Take(10)...); return len(got) >= 3 })
	if !slices.Equal(got, []int16{1, -2, 3}) {
		t.Errorf("samples %v", got)
	}
	// The manifest copy is private while mpv runs.
	files, _ := filepath.Glob(filepath.Join(dir, "vis-*.mpd"))
	if len(files) != 1 {
		t.Fatalf("manifest files %v", files)
	}
	if info, _ := os.Stat(files[0]); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode().Perm())
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(args), "--start=1.000") || !strings.HasSuffix(strings.TrimSpace(string(args)), files[0]) {
		t.Errorf("args %s", args)
	}
	if err := d.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "vis-*.mpd")); len(files) != 0 {
		t.Errorf("manifest copy left: %v", files)
	}
}

func TestDecoderStopsAtTheBufferCap(t *testing.T) {
	bin, _ := fakeMPV(t, `head -c 400000 /dev/zero; sleep 5`) // 9 s of silence
	d, err := startDecoder(context.Background(), bin, t.TempDir(), 1, nil, 0, discard())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	waitFor(t, func() bool { d.mu.Lock(); defer d.mu.Unlock(); return len(d.buf) >= maxBuffered })
	time.Sleep(50 * time.Millisecond)
	d.mu.Lock()
	n := len(d.buf)
	d.mu.Unlock()
	if n > maxBuffered+2048 {
		t.Errorf("buffered %d samples, cap %d", n, maxBuffered)
	}
	// Taking makes room, and the rest flows in.
	if got := d.Take(SampleRate); len(got) != SampleRate {
		t.Errorf("took %d", len(got))
	}
	waitFor(t, func() bool { d.mu.Lock(); defer d.mu.Unlock(); return len(d.buf) >= maxBuffered })
	if got := d.Take(-1); len(got) != 0 {
		t.Errorf("negative take: %d", len(got))
	}
}

func TestDecoderEndsWithTheTrack(t *testing.T) {
	bin, _ := fakeMPV(t, `printf '\001\000'`)
	d, err := startDecoder(context.Background(), bin, t.TempDir(), 1, nil, 0, discard())
	if err != nil {
		t.Fatal(err)
	}
	<-d.done // mpv exited: the reader stopped by itself
	if got := d.Take(5); !slices.Equal(got, []int16{1}) {
		t.Errorf("samples %v", got)
	}
	if err := d.Close(); err != nil {
		t.Errorf("close after exit: %v", err)
	}
}

func TestDecoderStartFailures(t *testing.T) {
	dir := t.TempDir()
	if _, err := startDecoder(context.Background(), filepath.Join(dir, "no-such-mpv"), dir, 1, nil, 0, discard()); err == nil ||
		!strings.Contains(err.Error(), "visualizer: start") {
		t.Errorf("missing binary: %v", err)
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "vis-*.mpd")); len(files) != 0 {
		t.Errorf("manifest copy left: %v", files)
	}
	if _, err := startDecoder(context.Background(), "mpv", filepath.Join(dir, "missing"), 1, nil, 0, discard()); err == nil {
		t.Error("wrote into a missing directory")
	}
}

// With the real mpv, a 1 kHz tone decodes to samples that light the 1 kHz
// band: the arguments really produce mono 16-bit PCM at SampleRate.
func TestDecoderWithRealMPV(t *testing.T) {
	mpv, err := exec.LookPath("mpv")
	if err != nil {
		t.Skip("mpv not installed")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	tone := filepath.Join(dir, "tone.flac")
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=1000:duration=2:sample_rate=44100",
		"-c:a", "flac", tone).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg: %v %s", err, out)
	}
	data, _ := os.ReadFile(tone)
	d, err := startDecoder(context.Background(), mpv, dir, 1, data, 500*time.Millisecond, discard())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	<-d.done
	samples := d.Take(10 * SampleRate)
	if n := len(samples); n < SampleRate*14/10 || n > SampleRate*16/10 {
		t.Errorf("%d samples for 1.5 s", n)
	}
	a := NewAnalyzer()
	a.Push(samples)
	// ffmpeg's sine is at 1/8 of full scale, -18 dBFS: 0.70 on the bars'
	// -60..0 dB scale. Matching it shows the level survives the decode.
	if lv := a.Frame(0); math.Abs(lv[bandOf(t, a, 1000)]-0.70) > 0.05 {
		t.Errorf("1 kHz band at %.2f, want 0.70 (-18 dBFS)", lv[bandOf(t, a, 1000)])
	}
}
