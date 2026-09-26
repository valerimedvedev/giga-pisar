package asr

import "math"

// Silences — середины пауз (сек): тише порога дольше minSeconds.
func Silences(x []float32, rate int, noiseDb, minSeconds float64) []float64 {
	th := float32(math.Pow(10, noiseDb/20))
	minRun := int(minSeconds * float64(rate))
	var pts []float64
	start := -1
	for i, v := range x {
		if v < 0 {
			v = -v
		}
		if v < th {
			if start < 0 {
				start = i
			}
		} else if start >= 0 {
			if i-start >= minRun {
				pts = append(pts, float64(start+i)/2/float64(rate))
			}
			start = -1
		}
	}
	return pts
}

// ChunkBounds — границы кусков не длиннее maxChunk, разрез по последней паузе.
func ChunkBounds(total float64, sil []float64, maxChunk float64) [][2]float64 {
	var out [][2]float64
	pos := 0.0
	for total-pos > maxChunk {
		cut := pos + maxChunk
		for _, s := range sil {
			if s > pos+3 && s <= pos+maxChunk {
				cut = s
			}
		}
		out = append(out, [2]float64{pos, cut})
		pos = cut
	}
	return append(out, [2]float64{pos, total})
}

// LiveChunker — нарезка во время записи: фраза уходит в распознавание на паузе,
// пока человек говорит следующую. Как в приложении для Android.
type LiveChunker struct {
	rate                          int
	pause, minSpeech, maxSeconds  float64
	th                            float32
	buf                           []float32
	speech, quiet, lastQuietMid   int
}

func NewLiveChunker(rate int, pause, minSpeech, maxSeconds float64) *LiveChunker {
	return &LiveChunker{rate: rate, pause: pause, minSpeech: minSpeech, maxSeconds: maxSeconds, th: float32(math.Pow(10, -35.0/20)), lastQuietMid: -1}
}

// Push добавляет звук; вернёт готовый кусок или nil.
func (c *LiveChunker) Push(s []float32) []float32 {
	for _, v := range s {
		c.buf = append(c.buf, v)
		if v < 0 {
			v = -v
		}
		if v < c.th {
			c.quiet++
			if c.quiet == int(0.3*float64(c.rate)) {
				c.lastQuietMid = len(c.buf) - c.quiet/2
			}
		} else {
			c.speech++
			c.quiet = 0
		}
	}
	minSp := int(c.minSpeech * float64(c.rate))
	if c.speech >= minSp && c.quiet >= int(c.pause*float64(c.rate)) {
		return c.cut(len(c.buf) - c.quiet/2)
	}
	if float64(len(c.buf)) >= c.maxSeconds*float64(c.rate) {
		if c.speech < minSp {
			c.reset()
			return nil
		}
		at := len(c.buf)
		if c.lastQuietMid > len(c.buf)/4 {
			at = c.lastQuietMid
		}
		return c.cut(at)
	}
	return nil
}

// Flush — остаток при остановке (nil, если речи не было).
func (c *LiveChunker) Flush() []float32 {
	if c.speech < int(0.15*float64(c.rate)) {
		c.reset()
		return nil
	}
	return c.cut(len(c.buf))
}

// TrailingQuiet — сколько тишины в хвосте куска (отсчётов): длинная пауза = новый абзац.
func TrailingQuiet(chunk []float32) int {
	th := float32(math.Pow(10, -35.0/20))
	n := 0
	for i := len(chunk) - 1; i >= 0; i-- {
		v := chunk[i]
		if v < 0 {
			v = -v
		}
		if v >= th {
			break
		}
		n++
	}
	return n
}

func (c *LiveChunker) cut(at int) []float32 {
	out := append([]float32(nil), c.buf[:at]...)
	rest := append([]float32(nil), c.buf[at:]...)
	c.buf = rest
	c.speech, c.quiet, c.lastQuietMid = 0, 0, -1
	for _, v := range rest {
		if v < 0 {
			v = -v
		}
		if v >= c.th {
			c.speech++
		} else {
			c.quiet++
		}
	}
	return out
}

func (c *LiveChunker) reset() { c.buf = c.buf[:0]; c.speech, c.quiet, c.lastQuietMid = 0, 0, -1 }
