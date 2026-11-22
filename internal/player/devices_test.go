package player

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExclusiveDevices(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "card3", "id"), "AppleJ314\n")
	writeFile(t, filepath.Join(root, "card3", "pcm0p", "info"), "card: 3\ndevice: 0\nid: Primary (*)\nname: \n")
	writeFile(t, filepath.Join(root, "card3", "pcm1p", "info"), "")
	writeFile(t, filepath.Join(root, "card3", "pcm0c", "info"), "id: capture\n")
	writeFile(t, filepath.Join(root, "card4", "id"), "Vestri\n")
	writeFile(t, filepath.Join(root, "card4", "usbid"), "30be:0101\n")
	writeFile(t, filepath.Join(root, "card4", "pcm0p", "info"), "id: USB Audio\n")
	writeFile(t, filepath.Join(root, "card5", "pcm0p", "info"), "") // no id: skipped
	writeFile(t, filepath.Join(root, "card6", "id"), "Odd\n")
	writeFile(t, filepath.Join(root, "card6", "pcmXp", "info"), "") // bad number: skipped

	got, err := ExclusiveDevices(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []Device{
		{Name: "alsa/hw:CARD=AppleJ314,DEV=0", Description: "AppleJ314: Primary (*)", BuiltIn: true},
		{Name: "alsa/hw:CARD=AppleJ314,DEV=1", Description: "AppleJ314", BuiltIn: true},
		{Name: "alsa/hw:CARD=Vestri,DEV=0", Description: "Vestri: USB Audio"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	if _, err := ExclusiveDevices("/no/such/dir"); err != nil {
		t.Errorf("missing root err = %v", err)
	}
	if _, err := ExclusiveDevices("[bad"); err == nil {
		t.Error("bad glob accepted")
	}
	if _, err := ExclusiveDevices(""); err != nil { // the real /proc/asound
		t.Errorf("default root err = %v", err)
	}
}

const mpvHelp = `List of detected audio devices:
  'auto' (Autoselect device)
  'pipewire' (Default (pipewire))
  'pipewire/alsa_output.usb-Schiit_Audio_Vestri-00.analog-stereo' (Vestri Analog Stereo)
  'pulse/alsa_output.usb-Schiit_Audio_Vestri-00.analog-stereo' (Vestri Analog Stereo)
  'alsa/front:CARD=Vestri,DEV=0' (Vestri, USB Audio/Front output / input)
garbage line
`

func TestSharedDevices(t *testing.T) {
	got := SharedDevices([]byte(mpvHelp))
	if len(got) != 2 || got[0].Name != "auto" || got[1].Description != "Vestri Analog Stereo" {
		t.Errorf("got %+v", got)
	}
}

func TestMPVDeviceHelp(t *testing.T) {
	if _, err := MPVDeviceHelp(context.Background(), "mpv-does-not-exist-xyz"); !errors.Is(err, ErrMPVNotFound) {
		t.Errorf("missing binary err = %v", err)
	}
	// /bin/false exits non-zero with no output.
	if _, err := MPVDeviceHelp(context.Background(), "false"); err == nil {
		t.Error("empty failing output accepted")
	}
	// mpv prints the list and then exits non-zero.
	script := filepath.Join(t.TempDir(), "mpv")
	writeFile(t, script, "#!/bin/sh\necho \"  'auto' (Autoselect device)\"\nexit 1\n")
	os.Chmod(script, 0o755)
	out, err := MPVDeviceHelp(context.Background(), script)
	if err != nil || !strings.Contains(string(out), "auto") {
		t.Errorf("= %q %v", out, err)
	}
	t.Setenv("PATH", filepath.Dir(script))
	if out, err := MPVDeviceHelp(context.Background(), ""); err != nil || len(out) == 0 {
		t.Errorf("default binary = %q %v", out, err)
	}
}

func TestWriteDeviceList(t *testing.T) {
	var b bytes.Buffer
	WriteDeviceList(&b, []Device{{Name: "alsa/hw:CARD=AppleJ314,DEV=0", Description: "x", BuiltIn: true},
		{Name: "alsa/hw:CARD=Vestri,DEV=0", Description: "Vestri"}}, []Device{{Name: "auto", Description: "Autoselect"}}, nil)
	out := b.String()
	for _, want := range []string{"Exclusive mode", "alsa/hw:CARD=Vestri,DEV=0", "built-in, not USB", "Shared mode", "auto"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "built-in") != 1 {
		t.Error("USB device flagged as built-in")
	}
	b.Reset()
	WriteDeviceList(&b, nil, nil, ErrMPVNotFound)
	if !strings.Contains(b.String(), "no ALSA playback devices") || !strings.Contains(b.String(), "unavailable") {
		t.Errorf("empty output:\n%s", b.String())
	}
	b.Reset()
	WriteDeviceList(&b, nil, nil, nil)
	if !strings.Contains(b.String(), "no PipeWire sinks") {
		t.Errorf("no sinks output:\n%s", b.String())
	}
}
