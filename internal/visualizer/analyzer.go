// Package visualizer turns audio into spectrum bars for the now-playing
// bar (decisions 67 to 72). It analyzes a separate, silent decode of the
// playing track, so the audio that reaches the output is never touched.
package visualizer

import (
	"math"
	"math/cmplx"
	"time"
)

// Analysis format: mono, 16-bit, SampleRate samples per second.
const (
	SampleRate = 22050
	// Bands is the number of spectrum bars.
	Bands = 32

	fftSize = 2048 // about 93 ms at SampleRate
	minHz   = 40.0
	maxHz   = 10000.0
	// floorDB is the level drawn as an empty bar; 0 dBFS fills it.
	floorDB = -60.0
	// fallPerSecond is how fast a bar drops when the sound does: a full
	// bar empties in under a second (decision 72).
	fallPerSecond = 1.5
)

// Analyzer keeps the latest fftSize samples and turns them into band
// levels from 0 (silence) to 1 (full scale). It is not safe for
// concurrent use.
type Analyzer struct {
	ring   []float64 // the last fftSize samples, oldest at pos
	pos    int
	window []float64
	edges  [Bands + 1]int // FFT bins: band b covers edges[b] to edges[b+1]-1
	levels [Bands]float64
	spec   []complex128 // scratch
}

// NewAnalyzer returns an analyzer with silence in its buffer.
func NewAnalyzer() *Analyzer {
	a := &Analyzer{ring: make([]float64, fftSize), window: make([]float64, fftSize), spec: make([]complex128, fftSize)}
	for i := range a.window {
		a.window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(fftSize-1)) // Hann
	}
	a.edges = bandEdges()
	return a
}

// bandEdges spaces the bands logarithmically from minHz to maxHz, giving
// each at least one FFT bin.
func bandEdges() [Bands + 1]int {
	var e [Bands + 1]int
	binHz := float64(SampleRate) / fftSize
	for b := range e {
		hz := minHz * math.Pow(maxHz/minHz, float64(b)/Bands)
		e[b] = int(math.Round(hz / binHz))
		if b > 0 && e[b] <= e[b-1] {
			e[b] = e[b-1] + 1
		}
	}
	return e
}

// Push adds samples, newest last.
func (a *Analyzer) Push(samples []int16) {
	for _, s := range samples {
		a.ring[a.pos] = float64(s) / 32768
		a.pos = (a.pos + 1) % fftSize
	}
}

// Reset fills the buffer with silence, for a new track or position. The
// bars keep falling from where they were.
func (a *Analyzer) Reset() {
	clear(a.ring)
	a.pos = 0
}

// Frame analyzes the buffer and returns the bar levels after dt: a bar
// jumps up to a louder level at once and falls at fallPerSecond.
func (a *Analyzer) Frame(dt time.Duration) [Bands]float64 {
	for i := range fftSize {
		a.spec[i] = complex(a.ring[(a.pos+i)%fftSize]*a.window[i], 0)
	}
	fft(a.spec)
	// A full-scale sine peaks at fftSize/4 with a Hann window.
	ref := float64(fftSize) / 4
	for b := range Bands {
		peak := 0.0
		for k := a.edges[b]; k < a.edges[b+1]; k++ {
			peak = max(peak, cmplx.Abs(a.spec[k]))
		}
		a.levels[b] = max(level(peak/ref), a.fallen(b, dt))
	}
	return a.levels
}

// Decay lets the bars fall with no new sound (paused or stopped) and
// reports whether they are all empty.
func (a *Analyzer) Decay(dt time.Duration) ([Bands]float64, bool) {
	empty := true
	for b := range Bands {
		a.levels[b] = a.fallen(b, dt)
		empty = empty && a.levels[b] == 0
	}
	return a.levels, empty
}

func (a *Analyzer) fallen(b int, dt time.Duration) float64 {
	return max(a.levels[b]-fallPerSecond*dt.Seconds(), 0)
}

// level maps an amplitude (1 = full scale) to 0..1 on a dB scale.
func level(amp float64) float64 {
	if amp <= 0 {
		return 0
	}
	db := 20 * math.Log10(amp)
	return min(max((db-floorDB)/-floorDB, 0), 1)
}

// fft is an in-place iterative radix-2 FFT; len(x) must be a power of 2.
func fft(x []complex128) {
	n := len(x)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j |= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		step := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			w := complex(1, 0)
			for k := range size / 2 {
				u, v := x[start+k], x[start+k+size/2]*w
				x[start+k], x[start+k+size/2] = u+v, u-v
				w *= step
			}
		}
	}
}
