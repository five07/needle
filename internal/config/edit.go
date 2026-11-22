package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Change sets one setting: Value is a string for string keys and a bool
// for bool keys.
type Change struct {
	Key   string
	Value any
}

// editable lists the keys Edit can write (decision 75), and the kind of
// value each takes.
var editable = map[string]bool{ // true: bool, false: string
	"quality":            false,
	"autoplay":           true,
	"audio.mode":         false,
	"audio.device":       false,
	"audio.volume":       false,
	"visualizer.enabled": true,
	"visualizer.theme":   false,
}

// UnsupportedError means the file writes a key in a shape Edit does not
// rewrite (a dotted key, an inline table, or a multi-line value), so the
// change is not saved.
type UnsupportedError struct{ Key string }

// Error implements error.
func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("config: %s is written in a form needle cannot update (a dotted key, inline table, or multi-line value); change it in config.toml by hand", e.Key)
}

// Edit saves changes to the configuration file at path, keeping every other
// line as it is, comments included (decision 75). It rewrites the line of
// each changed key, keeping a comment after the value, or adds the key under
// its table (adding the table at the end if the file has none). The result
// must load and validate, and hold the new values, before it replaces the
// file atomically with the same permissions (0600 for a new file). On any
// error the file is unchanged. A symlinked config file is written through
// the link.
func Edit(path string, changes ...Change) error {
	for _, c := range changes {
		isBool, ok := editable[c.Key]
		if !ok {
			return fmt.Errorf("config: %s cannot be edited", c.Key)
		}
		if _, b := c.Value.(bool); b != isBool {
			return fmt.Errorf("config: %s: wrong value type %T", c.Key, c.Value)
		}
		if s, isStr := c.Value.(string); isStr && strings.ContainsFunc(s, isControl) {
			return &ValidationError{Key: c.Key, Reason: "contains control characters"}
		}
	}
	target, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		target, err = path, nil
	}
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	mode := fs.FileMode(0o600)
	data, err := os.ReadFile(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fmt.Errorf("config: %w", err)
		}
	case err != nil:
		return fmt.Errorf("config: %w", err)
	default:
		if info, err := os.Stat(target); err == nil {
			mode = info.Mode().Perm()
		}
	}
	text := string(data)
	for _, c := range changes {
		if text, err = setKey(text, c.Key, formatValue(c.Value)); err != nil {
			return err
		}
	}
	return replaceChecked(target, text, mode, changes)
}

// replaceChecked writes text next to target, checks that it loads with the
// changed values, and renames it over target.
func replaceChecked(target, text string, mode fs.FileMode, changes []Change) error {
	tmp, err := os.CreateTemp(filepath.Dir(target), ".config.toml.*")
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name) // after the rename this is a no-op
	_, werr := tmp.WriteString(text)
	if werr == nil {
		werr = tmp.Chmod(mode)
	}
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("config: write: %w", werr)
	}
	cfg, err := Load(name, func(string) string { return "" })
	if err != nil {
		return fmt.Errorf("config: the change would not load: %w", err)
	}
	for _, c := range changes {
		if got := valueOf(cfg, c.Key); got != c.Value {
			return fmt.Errorf("config: %s did not take the new value (got %v)", c.Key, got)
		}
	}
	if err := os.Rename(name, target); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}

func valueOf(c Config, key string) any {
	switch key {
	case "quality":
		return string(c.Quality)
	case "autoplay":
		return c.Autoplay
	case "audio.mode":
		return string(c.Audio.Mode)
	case "audio.device":
		return c.Audio.Device
	case "audio.volume":
		return string(c.Audio.Volume)
	case "visualizer.enabled":
		return c.Visualizer.Enabled
	default: // "visualizer.theme"; Edit checks the key
		return c.Visualizer.Theme
	}
}

// formatValue writes a bool or a TOML basic string. Control characters are
// rejected before this.
func formatValue(v any) string {
	if b, ok := v.(bool); ok {
		return fmt.Sprint(b)
	}
	s := v.(string)
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// lexState carries what a line leaves open into the next one.
type lexState struct {
	ml    byte // inside a multi-line string: '"' or '\''; 0 if not
	depth int  // open [ and { in a value
}

func (s lexState) clean() bool { return s.ml == 0 && s.depth <= 0 }

// scan follows line from st and returns where a comment starts (-1 if
// none), updating st for the next line.
func scan(line string, st *lexState) int {
	for i := 0; i < len(line); i++ {
		c := line[i]
		if st.ml != 0 {
			if c == '\\' && st.ml == '"' {
				i++
				continue
			}
			if c == st.ml && strings.HasPrefix(line[i:], strings.Repeat(string(st.ml), 3)) {
				st.ml = 0
				i += 2
			}
			continue
		}
		switch c {
		case '#':
			return i
		case '"', '\'':
			if strings.HasPrefix(line[i:], strings.Repeat(string(c), 3)) {
				st.ml = c
				i += 2
				continue
			}
			for i++; i < len(line) && line[i] != c; i++ {
				if c == '"' && line[i] == '\\' {
					i++
				}
			}
		case '[', '{':
			st.depth++
		case ']', '}':
			st.depth--
		}
	}
	return -1
}

// splitKey splits a dotted key outside quotes and unquotes each part.
func splitKey(k string) []string {
	var parts []string
	var cur strings.Builder
	var quote byte
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '.':
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
		case c != ' ' && c != '\t':
			cur.WriteByte(c)
		}
	}
	return append(parts, strings.TrimSpace(cur.String()))
}

// keyEnd returns the index of the "=" that ends the key in line, or -1.
func keyEnd(line string) int {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '=':
			return i
		}
	}
	return -1
}

// setKey sets key to the formatted value in the TOML text.
func setKey(text, key, value string) (string, error) {
	table, name := "", key
	if i := strings.LastIndexByte(key, '.'); i >= 0 {
		table, name = key[:i], key[i+1:]
	}
	lines := strings.Split(text, "\n")
	var (
		st        lexState
		cur       = "" // the table of the current line
		headers   = map[string]int{}
		lastStmt  = map[string]int{} // the last line of a statement, per table
		firstHead = -1
		found     = -1
		foundAt   lexState
	)
	for i, raw := range lines {
		line := strings.TrimSuffix(raw, "\r")
		start := st
		if !start.clean() { // continuing a multi-line value
			scan(line, &st)
			if st.clean() {
				lastStmt[cur] = i
			}
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed[0] == '#' {
			continue
		}
		if trimmed[0] == '[' {
			inner := strings.TrimPrefix(trimmed, "[")
			array := strings.HasPrefix(inner, "[")
			inner = strings.TrimPrefix(inner, "[")
			if j := strings.IndexByte(inner, ']'); j >= 0 {
				inner = inner[:j]
			}
			cur = strings.Join(splitKey(inner), ".")
			if array {
				cur = "[[" + cur + "]]" // never one of ours
			}
			if _, dup := headers[cur]; !dup {
				headers[cur] = i
			}
			if firstHead < 0 {
				firstHead = i
			}
			continue
		}
		eq := keyEnd(line)
		parts := []string{}
		if eq >= 0 {
			parts = splitKey(line[:eq])
		}
		full := strings.Join(append(splitKey(cur), parts...), ".")
		if cur == "" {
			full = strings.Join(parts, ".")
		}
		after := st
		scan(line[max(eq+1, 0):], &after)
		st = after
		if st.clean() {
			lastStmt[cur] = i
		}
		switch {
		case full == key && cur == table && len(parts) == 1:
			found, foundAt = i, after
		case full == key || (table != "" && (full == table || strings.HasPrefix(table, full+"."))):
			return "", &UnsupportedError{Key: key} // dotted, or an inline table
		case table != "" && len(parts) > 1 && strings.HasPrefix(full, table+"."):
			return "", &UnsupportedError{Key: key} // the table is made of dotted keys
		}
	}
	kv := name + " = " + value
	switch {
	case found >= 0:
		if !foundAt.clean() {
			return "", &UnsupportedError{Key: key}
		}
		raw := lines[found]
		cr := strings.HasSuffix(raw, "\r")
		line := strings.TrimSuffix(raw, "\r")
		eq := keyEnd(line)
		rest := line[eq+1:]
		pad := rest[:len(rest)-len(strings.TrimLeft(rest, " \t"))]
		tail := ""
		var probe lexState
		if c := scan(rest, &probe); c >= 0 {
			body := rest[:c]
			tail = body[len(strings.TrimRight(body, " \t")):] + rest[c:]
		}
		line = line[:eq+1] + pad + value + tail
		if cr {
			line += "\r"
		}
		lines[found] = line
	case table == "":
		if l, ok := lastStmt[""]; ok {
			lines = slices.Insert(lines, l+1, kv) // after the last top-level key
		} else if firstHead >= 0 {
			lines = slices.Insert(lines, firstHead, kv, "") // before the first table
		} else {
			lines = appendLines(lines, false, kv)
		}
	default:
		h, ok := headers[table]
		if !ok {
			lines = appendLines(lines, true, "["+table+"]", kv)
			break
		}
		at := h
		if l, ok := lastStmt[table]; ok && l > h {
			at = l
		}
		lines = slices.Insert(lines, at+1, kv)
	}
	return strings.Join(lines, "\n"), nil
}

// appendLines adds add at the end of lines, after a blank line when sep is
// set and the file has content, keeping a final newline.
func appendLines(lines []string, sep bool, add ...string) []string {
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1] // the final newline
	}
	if sep && len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}
	return append(append(lines, add...), "")
}
