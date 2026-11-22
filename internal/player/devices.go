package player

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Device is an audio output that can go in audio.device.
type Device struct {
	// Name is the audio.device value.
	Name        string
	Description string
	// BuiltIn marks ALSA devices that are not USB. On some laptops these
	// drive the speakers without protection and are unsafe at full level.
	BuiltIn bool
}

// ExclusiveDevices lists ALSA playback devices usable in exclusive mode.
// root is the procfs ALSA directory ("" means /proc/asound).
func ExclusiveDevices(root string) ([]Device, error) {
	if root == "" {
		root = "/proc/asound"
	}
	cards, err := filepath.Glob(filepath.Join(root, "card[0-9]*"))
	if err != nil {
		return nil, fmt.Errorf("player: list ALSA cards: %w", err)
	}
	var out []Device
	for _, card := range cards {
		id := strings.TrimSpace(readSmall(filepath.Join(card, "id")))
		if id == "" {
			continue
		}
		_, usbErr := os.Stat(filepath.Join(card, "usbid"))
		pcms, _ := filepath.Glob(filepath.Join(card, "pcm[0-9]*p"))
		for _, pcm := range pcms {
			dev, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(pcm), "pcm"), "p"))
			if err != nil {
				continue
			}
			d := Device{
				Name:        fmt.Sprintf("alsa/hw:CARD=%s,DEV=%d", id, dev),
				Description: id,
				BuiltIn:     usbErr != nil,
			}
			if name := pcmName(filepath.Join(pcm, "info")); name != "" {
				d.Description += ": " + name
			}
			out = append(out, d)
		}
	}
	return out, nil
}

// pcmName reads the "id" line of a pcm info file.
func pcmName(path string) string {
	sc := bufio.NewScanner(strings.NewReader(readSmall(path)))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "id: "); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// readSmall reads up to 4 KiB of a procfs file, or "" on error.
func readSmall(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, 4096))
	return string(b)
}

var helpLine = regexp.MustCompile(`^\s*'([^']+)'\s+\((.*)\)\s*$`)

// SharedDevices parses `mpv --audio-device=help` output, keeping the system
// default and PipeWire sinks.
func SharedDevices(help []byte) []Device {
	var out []Device
	sc := bufio.NewScanner(bytes.NewReader(help))
	for sc.Scan() {
		m := helpLine.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		if m[1] == "auto" || strings.HasPrefix(m[1], "pipewire/") {
			out = append(out, Device{Name: m[1], Description: m[2]})
		}
	}
	return out
}

// MPVDeviceHelp runs `mpv --audio-device=help`. mpv exits non-zero after
// printing the list, so output is returned whenever there is some.
func MPVDeviceHelp(ctx context.Context, binary string) ([]byte, error) {
	if binary == "" {
		binary = "mpv"
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMPVNotFound, err)
	}
	out, err := exec.CommandContext(ctx, path, "--no-config", "--audio-device=help").CombinedOutput()
	if len(out) == 0 && err != nil {
		return nil, fmt.Errorf("player: list mpv audio devices: %w", err)
	}
	return out, nil
}

// WriteDeviceList prints both device lists for -list-devices.
func WriteDeviceList(w io.Writer, exclusive []Device, shared []Device, sharedErr error) {
	fmt.Fprintln(w, "Exclusive mode (audio.mode = \"exclusive\", bit-perfect):")
	if len(exclusive) == 0 {
		fmt.Fprintln(w, "  (no ALSA playback devices found)")
	}
	for _, d := range exclusive {
		note := ""
		if d.BuiltIn {
			note = "  [built-in, not USB: check before using]"
		}
		fmt.Fprintf(w, "  %-40s %s%s\n", d.Name, d.Description, note)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Shared mode (audio.mode = \"shared\", through PipeWire):")
	switch {
	case sharedErr != nil:
		fmt.Fprintf(w, "  (unavailable: %v)\n", sharedErr)
	case len(shared) == 0:
		fmt.Fprintln(w, "  (no PipeWire sinks found)")
	}
	for _, d := range shared {
		fmt.Fprintf(w, "  %-40s %s\n", d.Name, d.Description)
	}
}
