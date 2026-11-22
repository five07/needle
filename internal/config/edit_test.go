package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The README sample: comments, blank lines, and every table.
const sample = `# Stream quality: "aac", "lossless", or "hires".
quality = "hires"
# Continue with track radio when the queue runs out (toggle with c).
autoplay = false

[audio]
# "shared" plays through PipeWire.
mode = "shared"   # the default
device = "auto"

[visualizer]
enabled = false
theme = "terminal"
custom = [
  "#202020", "#505050",   # mode = "not a key"
  "#a0a0a0", "#ffffff",
]

[auth]
# client_id = ""
`

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEditKeepsEverythingElse(t *testing.T) {
	tests := []struct {
		name    string
		changes []Change
		want    func(string) string
	}{
		{"top-level string", []Change{{"quality", "lossless"}},
			func(s string) string { return strings.Replace(s, `quality = "hires"`, `quality = "lossless"`, 1) }},
		{"bool", []Change{{"autoplay", true}},
			func(s string) string { return strings.Replace(s, "autoplay = false", "autoplay = true", 1) }},
		{"keeps the comment and spacing after a value", []Change{{"audio.mode", "exclusive"}, {"audio.device", "alsa/hw:CARD=DAC,DEV=0"}},
			func(s string) string {
				s = strings.Replace(s, `mode = "shared"   # the default`, `mode = "exclusive"   # the default`, 1)
				return strings.Replace(s, `device = "auto"`, `device = "alsa/hw:CARD=DAC,DEV=0"`, 1)
			}},
		{"next to a multi-line array", []Change{{"visualizer.theme", "deep red"}, {"visualizer.enabled", true}},
			func(s string) string {
				s = strings.Replace(s, `theme = "terminal"`, `theme = "deep red"`, 1)
				return strings.Replace(s, "enabled = false", "enabled = true", 1)
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := writeConfig(t, sample)
			if err := Edit(p, tt.changes...); err != nil {
				t.Fatal(err)
			}
			if got, want := readFile(t, p), tt.want(sample); got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestEditAddsMissingKeys(t *testing.T) {
	tests := []struct {
		name, body string
		changes    []Change
		want       string
	}{
		{"no file", "", []Change{{"quality", "aac"}, {"visualizer.theme", "ice blue"}},
			"quality = \"aac\"\n\n[visualizer]\ntheme = \"ice blue\"\n"},
		{"after the table's last key", "[audio]\nmode = \"shared\"\n\n# trailing comment\n[auth]\n",
			[]Change{{"audio.device", "pipewire/sink"}},
			"[audio]\nmode = \"shared\"\ndevice = \"pipewire/sink\"\n\n# trailing comment\n[auth]\n"},
		{"volume control", "[audio]\nmode = \"exclusive\" # DAC\ndevice = \"alsa/hw:4\"\n",
			[]Change{{"audio.volume", "software"}},
			"[audio]\nmode = \"exclusive\" # DAC\ndevice = \"alsa/hw:4\"\nvolume = \"software\"\n"},
		{"empty table", "[visualizer]\n# nothing yet\n", []Change{{"visualizer.enabled", true}},
			"[visualizer]\nenabled = true\n# nothing yet\n"},
		{"top-level after the last top-level key", "# hi\nquality = \"hires\"\n\n[audio]\nmode = \"shared\"\n",
			[]Change{{"autoplay", true}}, "# hi\nquality = \"hires\"\nautoplay = true\n\n[audio]\nmode = \"shared\"\n"},
		{"top-level before the first table", "# hi\n\n[audio]\nmode = \"shared\"\n", []Change{{"autoplay", true}},
			"# hi\n\nautoplay = true\n\n[audio]\nmode = \"shared\"\n"},
		{"top-level without tables", "# only a comment", []Change{{"autoplay", true}}, "# only a comment\nautoplay = true\n"},
		{"new table after content without a final newline", "quality = \"aac\"", []Change{{"audio.mode", "shared"}},
			"quality = \"aac\"\n\n[audio]\nmode = \"shared\"\n"},
		{"quoted keys and headers", "[ \"audio\" ]\n\"mode\" = 'shared'\n", []Change{{"audio.mode", "shared"}},
			"[ \"audio\" ]\n\"mode\" = \"shared\"\n"},
		{"after a triple-quoted value on one line", "[auth]\nclient_id = \"\"\"abc\"\"\" # x\n[audio]\nmode = \"shared\"\n",
			[]Change{{"audio.mode", "shared"}}, "[auth]\nclient_id = \"\"\"abc\"\"\" # x\n[audio]\nmode = \"shared\"\n"},
		{"after a multi-line literal", "[auth]\nclient_id = '''\nabc'''\n[audio]\nmode = \"x\"\n",
			[]Change{{"audio.mode", "shared"}}, "[auth]\nclient_id = '''\nabc'''\n[audio]\nmode = \"shared\"\n"},
		{"an escaped quote before a comment", "[audio]\ndevice = \"a\\\"#b\" # keep\n", []Change{{"audio.device", "auto"}},
			"[audio]\ndevice = \"auto\" # keep\n"},
		{"CRLF", "quality = \"hires\"\r\n# x\r\n", []Change{{"quality", "aac"}}, "quality = \"aac\"\r\n# x\r\n"},
		{"strings with quotes and hashes", "[audio]\ndevice = \"a#b\" # c\n", []Change{{"audio.device", `x"y\z`}},
			"[audio]\ndevice = \"x\\\"y\\\\z\" # c\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "sub", FileName)
			if tt.body != "" {
				os.MkdirAll(filepath.Dir(p), 0o700)
				os.WriteFile(p, []byte(tt.body), 0o640)
			}
			if tt.changes == nil {
				return
			}
			if err := Edit(p, tt.changes...); err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, p); got != tt.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestEditRefuses(t *testing.T) {
	tests := []struct {
		name, body string
		change     Change
		unsup      bool
		sub        string
	}{
		{"dotted key", "audio.mode = \"shared\"\n", Change{"audio.mode", "exclusive"}, true, ""},
		{"table of dotted keys", "audio.mode = \"shared\"\n", Change{"audio.device", "auto"}, true, ""},
		{"inline table", "audio = { mode = \"shared\" }\n", Change{"audio.device", "auto"}, true, ""},
		{"multi-line value", "quality = \"\"\"\nhires\"\"\"\n", Change{"quality", "aac"}, true, ""},
		{"invalid result", "[audio]\ndevice = \"auto\"\n", Change{"audio.mode", "exclusive"}, false, "would not load"},
		{"invalid value", "", Change{"quality", "best"}, false, "would not load"},
		{"broken file", "quality = \n", Change{"autoplay", true}, false, "would not load"},
		{"unknown key", "", Change{"auth.client_id", "x"}, false, "cannot be edited"},
		{"wrong type", "", Change{"autoplay", "yes"}, false, "wrong value type"},
		{"control character", "", Change{"audio.device", "a\x1bb"}, false, "control characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := writeConfig(t, tt.body)
			err := Edit(p, tt.change)
			var ue *UnsupportedError
			if tt.unsup != errors.As(err, &ue) || err == nil || !strings.Contains(err.Error(), tt.sub) {
				t.Fatalf("err %v", err)
			}
			if tt.unsup && (ue.Key != tt.change.Key || !strings.Contains(err.Error(), "by hand")) {
				t.Errorf("err %v", err)
			}
			if got := readFile(t, p); got != tt.body {
				t.Errorf("file changed:\n%s", got)
			}
			if left, _ := filepath.Glob(filepath.Join(filepath.Dir(p), ".config.toml.*")); len(left) != 0 {
				t.Errorf("temp files left: %v", left)
			}
		})
	}
}

func TestEditModeAndSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", FileName)
	os.MkdirAll(filepath.Dir(real), 0o700)
	os.WriteFile(real, []byte("quality = \"hires\"\n"), 0o640)
	link := filepath.Join(dir, FileName)
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := Edit(link, Change{"quality", "aac"}); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced")
	}
	if got := readFile(t, real); got != "quality = \"aac\"\n" {
		t.Errorf("target %q", got)
	}
	if fi, _ := os.Stat(real); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	// A new file is private.
	fresh := filepath.Join(dir, "new", FileName)
	if err := Edit(fresh, Change{"autoplay", true}); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(fresh); fi.Mode().Perm() != 0o600 {
		t.Errorf("new file mode %v", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(filepath.Dir(fresh)); fi.Mode().Perm() != 0o700 {
		t.Errorf("new dir mode %v", fi.Mode().Perm())
	}
}

func TestEditIOErrors(t *testing.T) {
	dir := t.TempDir()
	// A loop of symlinks cannot be resolved.
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	os.Symlink(a, b)
	os.Symlink(b, a)
	if err := Edit(a, Change{"autoplay", true}); err == nil {
		t.Error("symlink loop")
	}
	// The path is a directory.
	if err := Edit(dir, Change{"autoplay", true}); err == nil {
		t.Error("directory")
	}
	// The parent is a file.
	f := filepath.Join(dir, "file")
	os.WriteFile(f, nil, 0o600)
	if err := Edit(filepath.Join(f, FileName), Change{"autoplay", true}); err == nil {
		t.Error("parent is a file")
	}
	// A read-only directory: the temp file cannot be made.
	ro := filepath.Join(dir, "ro")
	os.MkdirAll(ro, 0o700)
	p := filepath.Join(ro, FileName)
	os.WriteFile(p, []byte("autoplay = false\n"), 0o600)
	os.Chmod(ro, 0o500)
	defer os.Chmod(ro, 0o700)
	if os.Getuid() != 0 {
		if err := Edit(p, Change{"autoplay", true}); err == nil {
			t.Error("read-only dir")
		}
	}
}
