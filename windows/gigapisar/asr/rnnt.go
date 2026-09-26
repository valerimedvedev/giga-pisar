package asr

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"gigapisar/ort"
)

const (
	ModelName          = "v3_e2e_rnnt"
	MaxChunk           = 24.0 // предел одного прохода модели — 25 секунд
	maxSymbolsPerFrame = 3
)

// Files — пять файлов модели GigaAM v3.
var Files = []string{ModelName + ".yaml", ModelName + "_encoder.onnx", ModelName + "_decoder.onnx", ModelName + "_joint.onnx", ModelName + "_tokenizer.model"}

func HasModel(dir string) bool {
	for _, f := range Files {
		if st, err := os.Stat(filepath.Join(dir, f)); err != nil || st.Size() == 0 {
			return false
		}
	}
	return true
}

type ModelConfig struct {
	Features               FeatureConfig
	PredHidden, PredLayers int
}

func ParseConfig(text string) ModelConfig {
	value := func(key string) (string, bool) {
		for _, line := range strings.Split(text, "\n") {
			s := strings.TrimSpace(line)
			if strings.HasPrefix(s, key+":") {
				return strings.TrimSpace(s[len(key)+1:]), true
			}
		}
		return "", false
	}
	num := func(key string, def int) int {
		if v, ok := value(key); ok {
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
		return def
	}
	d := DefaultFeatures
	c, hasC := value("center")
	return ModelConfig{
		Features: FeatureConfig{SampleRate: num("sample_rate", d.SampleRate), NMels: num("features", d.NMels), NFFT: num("n_fft", d.NFFT),
			WinLength: num("win_length", d.WinLength), HopLength: num("hop_length", d.HopLength), Center: hasC && c == "true"},
		PredHidden: num("pred_hidden", 320), PredLayers: num("pred_rnn_layers", 1),
	}
}

// Recognizer — энкодер, декодер, джойнт и токенизатор; жадное декодирование RNN-T.
type Recognizer struct {
	Config    ModelConfig
	enc, dec  *ort.Session
	joint     *ort.Session
	tok       *Tokenizer
	feat      *Features
	mu        sync.Mutex
}

// Load грузит модель из папки (onnxruntime должен быть загружен: ort.Load).
func Load(dir string, threads int) (*Recognizer, error) {
	read := func(name string) ([]byte, error) { return os.ReadFile(filepath.Join(dir, name)) }
	yaml, err := read(Files[0])
	if err != nil {
		return nil, err
	}
	cfg := ParseConfig(string(yaml))
	r := &Recognizer{Config: cfg, feat: NewFeatures(cfg.Features)}
	for i, dst := range []**ort.Session{&r.enc, &r.dec, &r.joint} {
		b, err := read(Files[1+i])
		if err != nil {
			return nil, err
		}
		th := 1 // декодер и джойнт крошечные: один поток быстрее раздачи работы
		if i == 0 {
			th = threads
		}
		s, err := ort.NewSession(b, th)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", Files[1+i], err)
		}
		*dst = s
	}
	tb, err := read(Files[4])
	if err != nil {
		return nil, err
	}
	if r.tok, err = NewTokenizer(tb); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Recognizer) Close() {
	for _, s := range []*ort.Session{r.enc, r.dec, r.joint} {
		if s != nil {
			s.Close()
		}
	}
}

func (r *Recognizer) SampleRate() int { return r.Config.Features.SampleRate }

// Transcribe — запись любой длины: длинная режется по паузам и склеивается.
func (r *Recognizer) Transcribe(samples []float32) (string, error) {
	rate := r.SampleRate()
	total := float64(len(samples)) / float64(rate)
	if total <= MaxChunk+1 {
		return r.TranscribeWave(samples)
	}
	var parts []string
	for _, b := range ChunkBounds(total, Silences(samples, rate, -35, 0.3), MaxChunk) {
		from, to := int(b[0]*float64(rate)), int(b[1]*float64(rate))
		if to > len(samples) {
			to = len(samples)
		}
		if to <= from {
			continue
		}
		t, err := r.TranscribeWave(samples[from:to])
		if err != nil {
			return "", err
		}
		if t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " "), nil
}

// TranscribeWave — одна волна не длиннее предела модели.
func (r *Recognizer) TranscribeWave(wave []float32) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	values, frames := r.feat.Compute(wave)
	if frames <= 0 {
		return "", nil
	}
	nMels := int64(r.Config.Features.NMels)
	out, err := r.enc.Run([]ort.Tensor{
		{F32: values, Shape: []int64{1, nMels, int64(frames)}},
		{I64: []int64{int64(r.feat.OutLen(len(wave)))}, Shape: []int64{1}},
	})
	if err != nil {
		return "", err
	}
	if len(out) < 2 || len(out[0].Shape) < 3 {
		return "", errors.New("энкодер вернул не то")
	}
	encD, encT := int(out[0].Shape[1]), int(out[0].Shape[2])
	encLen := encT
	if len(out[1].I64) > 0 {
		encLen = int(out[1].I64[0])
	} else if len(out[1].I32) > 0 {
		encLen = int(out[1].I32[0])
	}
	if encLen > encT {
		encLen = encT
	}
	ids, err := r.greedy(out[0].F32, encD, encT, encLen)
	if err != nil {
		return "", err
	}
	return r.tok.Decode(ids), nil
}

// Жадное декодирование: декодер пересчитывается только когда выдана буква.
func (r *Recognizer) greedy(encoded []float32, encD, encT, encLen int) ([]int, error) {
	blank := r.tok.BlankID()
	layers, hidden := r.Config.PredLayers, r.Config.PredHidden
	stateShape := []int64{int64(layers), 1, int64(hidden)}
	zeros := make([]float32, layers*hidden)
	var hyp []int
	label := blank
	h, c := zeros, zeros
	started := false
	var g, hNext, cNext []float32
	frame := make([]float32, encD)
	for t := 0; t < encLen; t++ {
		for d := 0; d < encD; d++ {
			frame[d] = encoded[d*encT+t]
		}
		for s := 0; s < maxSymbolsPerFrame; s++ {
			if g == nil {
				lab := blank
				hh, cc := zeros, zeros
				if started {
					lab, hh, cc = label, h, c
				}
				o, err := r.dec.Run([]ort.Tensor{
					{I64: []int64{int64(lab)}, Shape: []int64{1, 1}},
					{F32: hh, Shape: stateShape}, {F32: cc, Shape: stateShape},
				})
				if err != nil {
					return nil, err
				}
				g, hNext, cNext = o[0].F32, o[1].F32, o[2].F32
			}
			o, err := r.joint.Run([]ort.Tensor{
				{F32: frame, Shape: []int64{1, int64(encD), 1}},
				{F32: g, Shape: []int64{1, int64(hidden), 1}},
			})
			if err != nil {
				return nil, err
			}
			best, bestV := 0, float32(math.Inf(-1))
			for i, v := range o[0].F32 {
				if v > bestV {
					bestV, best = v, i
				}
			}
			if best == blank {
				break
			}
			hyp = append(hyp, best)
			label, h, c, started = best, hNext, cNext, true
			g = nil
		}
	}
	return hyp, nil
}
