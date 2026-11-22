package visualizer

import (
	"math"
	"math/cmplx"
	"testing"
	"time"
)

func sine(hz, amp float64, n int) []int16 {
	out := make([]int16, n)
	for i := range out {
		out[i] = int16(amp * 32767 * math.Sin(2*math.Pi*hz*float64(i)/SampleRate))
	}
	return out
}

// bandOf returns the band whose bins cover hz.
func bandOf(t *testing.T, a *Analyzer, hz float64) int {
	t.Helper()
	k := int(math.Round(hz / (float64(SampleRate) / fftSize)))
	for b := range Bands {
		if k >= a.edges[b] && k < a.edges[b+1] {
			return b
		}
	}
	t.Fatalf("%v Hz is in no band", hz)
	return -1
}

func TestFFTMatchesDFT(t *testing.T) {
	x := make([]complex128, 16)
	for i := range x {
		x[i] = complex(math.Sin(float64(i)*0.7)+0.3*float64(i%3), 0)
	}
	want := make([]complex128, len(x))
	for k := range want {
		for n, v := range x {
			want[k] += v * cmplx.Exp(complex(0, -2*math.Pi*float64(k*n)/float64(len(x))))
		}
	}
	fft(x)
	for k := range x {
		if cmplx.Abs(x[k]-want[k]) > 1e-9 {
			t.Errorf("bin %d = %v, want %v", k, x[k], want[k])
		}
	}
}

func TestBandEdges(t *testing.T) {
	e := bandEdges()
	binHz := float64(SampleRate) / fftSize
	if lo, hi := float64(e[0])*binHz, float64(e[Bands])*binHz; math.Abs(lo-minHz) > binHz || math.Abs(hi-maxHz) > binHz {
		t.Errorf("range %v-%v Hz", lo, hi)
	}
	for b := range Bands {
		if e[b+1] <= e[b] {
			t.Errorf("band %d is empty: %d-%d", b, e[b], e[b+1])
		}
	}
	if e[Bands] > fftSize/2 {
		t.Errorf("top edge %d is past Nyquist", e[Bands])
	}
}

func TestToneLightsItsBand(t *testing.T) {
	for _, hz := range []float64{100, 1000, 5000} {
		a := NewAnalyzer()
		a.Push(sine(hz, 1, fftSize))
		lv := a.Frame(0)
		b := bandOf(t, a, hz)
		if lv[b] < 0.95 {
			t.Errorf("%v Hz: its band %d is at %.2f", hz, b, lv[b])
		}
		for o := range Bands {
			if (o < b-2 || o > b+2) && lv[o] > 0.5*lv[b] {
				t.Errorf("%v Hz: band %d at %.2f, next to %.2f", hz, o, lv[o], lv[b])
			}
		}
	}
}

func TestQuieterIsLower(t *testing.T) {
	loud, quiet := NewAnalyzer(), NewAnalyzer()
	loud.Push(sine(1000, 1, fftSize))
	quiet.Push(sine(1000, 0.01, fftSize)) // -40 dB
	b := bandOf(t, loud, 1000)
	l, q := loud.Frame(0)[b], quiet.Frame(0)[b]
	if math.Abs(q-(l-40.0/60)) > 0.05 {
		t.Errorf("loud %.2f quiet %.2f: not 40 dB apart on the scale", l, q)
	}
}

func TestSilenceAndFalling(t *testing.T) {
	a := NewAnalyzer()
	if lv := a.Frame(0); lv != [Bands]float64{} {
		t.Errorf("silence: %v", lv)
	}
	a.Push(sine(1000, 1, fftSize))
	b := bandOf(t, a, 1000)
	a.Frame(0)
	// Silence again: the bar falls at fallPerSecond, not at once.
	a.Reset()
	if lv := a.Frame(100 * time.Millisecond); math.Abs(lv[b]-(1-0.15)) > 0.06 {
		t.Errorf("after 100 ms: %.2f", lv[b])
	}
	// Paused: Decay empties every bar within a second.
	lv, empty := a.Decay(400 * time.Millisecond)
	if empty || lv[b] == 0 {
		t.Errorf("empty too soon: %.2f", lv[b])
	}
	if lv, empty = a.Decay(600 * time.Millisecond); !empty || lv[b] != 0 {
		t.Errorf("not empty after a second: %.2f", lv[b])
	}
}

func TestPushKeepsTheNewestSamples(t *testing.T) {
	a := NewAnalyzer()
	a.Push(sine(1000, 1, fftSize))
	a.Push(make([]int16, fftSize)) // a full buffer of silence pushes the tone out
	if lv := a.Frame(time.Second); lv != [Bands]float64{} {
		t.Errorf("old samples still counted: %v", lv)
	}
}

func TestLevel(t *testing.T) {
	for amp, want := range map[float64]float64{0: 0, -1: 0, 1: 1, 2: 1, 0.001: 0, 0.0001: 0, math.Pow(10, -30.0/20): 0.5} {
		if got := level(amp); math.Abs(got-want) > 1e-9 {
			t.Errorf("level(%v) = %v, want %v", amp, got, want)
		}
	}
}
