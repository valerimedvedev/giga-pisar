// Звуковая волна → лог-мел-спектрограмма, шаг в шаг как server/giga_core.py,
// web/giga/features.js и android engine/Features.kt: та же точность float32
// в тех же местах, иначе на выходе не «чуть хуже», а бессмыслица.
package asr

import "math"

type FeatureConfig struct {
	SampleRate, NMels, NFFT, WinLength, HopLength int
	Center                                        bool
}

var DefaultFeatures = FeatureConfig{SampleRate: 16000, NMels: 64, NFFT: 320, WinLength: 320, HopLength: 160}

type Features struct {
	cfg    FeatureConfig
	nFreqs int
	window []float32
	cosT   []float64
	sinT   []float64
	fb     []float32
}

func NewFeatures(cfg FeatureConfig) *Features {
	f := &Features{cfg: cfg, nFreqs: cfg.NFFT/2 + 1}
	f.window = make([]float32, cfg.WinLength)
	for n := range f.window {
		f.window[n] = float32(0.5 - 0.5*math.Cos(2.0*math.Pi*float64(n)/float64(cfg.WinLength)))
	}
	N := cfg.NFFT
	f.cosT = make([]float64, f.nFreqs*N)
	f.sinT = make([]float64, f.nFreqs*N)
	for k := 0; k < f.nFreqs; k++ {
		for j := 0; j < N; j++ {
			a := 2.0 * math.Pi * float64((k*j)%N) / float64(N)
			f.cosT[k*N+j] = math.Cos(a)
			f.sinT[k*N+j] = -math.Sin(a)
		}
	}
	f.fb = melFilterbank(f.nFreqs, 0, float64(cfg.SampleRate)/2, cfg.NMels, cfg.SampleRate)
	return f
}

func hzToMel(f float64) float64 { return 2595.0 * math.Log10(1.0+f/700.0) }
func melToHz(m float64) float64 { return 700.0 * (math.Pow(10.0, m/2595.0) - 1.0) }

func melFilterbank(nFreqs int, fMin, fMax float64, nMels, sampleRate int) []float32 {
	top := float64(sampleRate / 2)
	all := make([]float64, nFreqs)
	for i := range all {
		all[i] = top * float64(i) / float64(nFreqs-1)
	}
	mMin, mMax := hzToMel(fMin), hzToMel(fMax)
	fPts := make([]float64, nMels+2)
	for i := range fPts {
		fPts[i] = melToHz(mMin + (mMax-mMin)*float64(i)/float64(nMels+1))
	}
	fDiff := make([]float64, nMels+1)
	for i := range fDiff {
		fDiff[i] = fPts[i+1] - fPts[i]
	}
	out := make([]float32, nFreqs*nMels)
	for i := 0; i < nFreqs; i++ {
		for m := 0; m < nMels; m++ {
			down := -(fPts[m] - all[i]) / fDiff[m]
			up := (fPts[m+2] - all[i]) / fDiff[m+1]
			v := math.Min(down, up)
			if v < 0 {
				v = 0
			}
			out[i*nMels+m] = float32(v)
		}
	}
	return out
}

// OutLen — сколько кадров энкодер ждёт для такого числа отсчётов.
func (f *Features) OutLen(samples int) int {
	if f.cfg.Center {
		return samples/f.cfg.HopLength + 1
	}
	return (samples-f.cfg.WinLength)/f.cfg.HopLength + 1
}

// Compute: волна → [nMels × кадры] подряд по строкам, число кадров.
func (f *Features) Compute(wave []float32) ([]float32, int) {
	nFFT, hop, nMels := f.cfg.NFFT, f.cfg.HopLength, f.cfg.NMels
	x := wave
	if f.cfg.Center {
		pad := nFFT / 2
		padded := make([]float32, len(x)+2*pad)
		for i := 0; i < pad; i++ {
			j := pad - i
			if j > len(x)-1 {
				j = len(x) - 1
			}
			padded[i] = x[j]
		}
		copy(padded[pad:], x)
		for i := 0; i < pad; i++ {
			j := len(x) - 2 - i
			if j < 0 {
				j = 0
			}
			padded[pad+len(x)+i] = x[j]
		}
		x = padded
	}
	n := (len(x)-nFFT)/hop + 1
	if len(x) < nFFT || n <= 0 {
		return nil, 0
	}
	frame := make([]float32, nFFT)
	power := make([]float32, f.nFreqs)
	out := make([]float32, nMels*n)
	for fr := 0; fr < n; fr++ {
		off := fr * hop
		for j := 0; j < nFFT; j++ {
			frame[j] = x[off+j] * f.window[j]
		}
		for k := 0; k < f.nFreqs; k++ {
			row := k * nFFT
			var re, im float64
			for j := 0; j < nFFT; j++ {
				v := float64(frame[j])
				re += v * f.cosT[row+j]
				im += v * f.sinT[row+j]
			}
			power[k] = float32(re*re + im*im)
		}
		for m := 0; m < nMels; m++ {
			var s float64
			for k := 0; k < f.nFreqs; k++ {
				s += float64(power[k]) * float64(f.fb[k*nMels+m])
			}
			mel := float32(s)
			if mel < 1e-9 {
				mel = 1e-9
			}
			if mel > 1e9 {
				mel = 1e9
			}
			out[m*n+fr] = float32(math.Log(float64(mel)))
		}
	}
	return out, n
}
