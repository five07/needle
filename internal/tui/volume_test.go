package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeVolume records volume changes and returns a level or an error.
type fakeVolume struct {
	deltas []float64
	level  float64
	err    error
}

func (f *fakeVolume) AdjustVolume(_ context.Context, delta float64) (float64, error) {
	f.deltas = append(f.deltas, delta)
	if f.err != nil {
		return 0, f.err
	}
	f.level = min(max(f.level+delta, 0), 100)
	return f.level, nil
}

// TASK-018 AC #2 (decision 82): - and = change the volume from any view,
// with a notice of the new level.
func TestVolumeKeys(t *testing.T) {
	e := newEnv(t)
	vol := &fakeVolume{level: 100}
	e.svc.Volume = vol
	m := e.model(80, 24)
	n := drive(m, press("-"), press("-"), press("4"), press("l"), press("="))
	if want := []float64{-VolumeStep, -VolumeStep, VolumeStep}; !reflect.DeepEqual(vol.deltas, want) {
		t.Errorf("deltas %v", vol.deltas)
	}
	if want := []string{"Volume 95%", "Volume 90%", "Volume 95%"}; !reflect.DeepEqual(n, want) {
		t.Errorf("notices %v", n)
	}
	// Volume on the output device.
	vol.err = ErrVolumeOnDevice
	if n := drive(m, press("-")); len(n) != 1 || n[0] != VolumeOnDeviceNotice {
		t.Errorf("device notices %v", n)
	}
	// Other failures are logged and shown briefly.
	vol.err = errors.New("ipc closed")
	if n := drive(m, press("=")); len(n) != 1 || !strings.HasPrefix(n[0], "Volume: ") || strings.Contains(n[0], "ipc") {
		t.Errorf("failure notices %v", n)
	}
	// No volume service: the keys do nothing.
	e.svc.Volume = nil
	if n := drive(m, press("-")); len(n) != 0 {
		t.Errorf("without a service: %v", n)
	}
}

// The volume keys type into the search input instead.
func TestVolumeKeysWhileTyping(t *testing.T) {
	e := newEnv(t)
	vol := &fakeVolume{level: 100}
	e.svc.Volume = vol
	m := e.model(80, 24)
	typeQuery(m, "a-ha =")
	if len(vol.deltas) != 0 {
		t.Errorf("deltas %v while typing", vol.deltas)
	}
	if !strings.Contains(screen(m), "a-ha =") {
		t.Errorf("query not typed:\n%s", screen(m))
	}
}
