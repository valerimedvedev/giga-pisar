package asr

import (
	"encoding/binary"
	"errors"
	"os"
)

// ReadWav читает 16-битный wav (первый канал). Возвращает отсчёты и частоту.
func ReadWav(b []byte) ([]float32, int, error) {
	if len(b) <= 44 || string(b[0:4]) != "RIFF" {
		return nil, 0, errors.New("это не wav")
	}
	rate, channels, bits := 16000, 1, 16
	i := 12
	for i+8 <= len(b) {
		id := string(b[i : i+4])
		size := int(binary.LittleEndian.Uint32(b[i+4:]))
		body := i + 8
		switch id {
		case "fmt ":
			channels = int(binary.LittleEndian.Uint16(b[body+2:]))
			rate = int(binary.LittleEndian.Uint32(b[body+4:]))
			bits = int(binary.LittleEndian.Uint16(b[body+14:]))
		case "data":
			if bits != 16 {
				return nil, 0, errors.New("нужен 16-битный wav")
			}
			end := body + size
			if end > len(b) {
				end = len(b)
			}
			if channels < 1 {
				channels = 1
			}
			n := (end - body) / 2 / channels
			out := make([]float32, n)
			for k := 0; k < n; k++ {
				out[k] = float32(int16(binary.LittleEndian.Uint16(b[body+k*2*channels:]))) / 32768
			}
			return out, rate, nil
		}
		i = body + size + size%2
	}
	return nil, 0, errors.New("в wav нет данных")
}

// Resample — простая линейная передискретизация.
func Resample(x []float32, from, to int) []float32 {
	if from == to || len(x) == 0 {
		return x
	}
	n := int(int64(len(x)) * int64(to) / int64(from))
	out := make([]float32, n)
	step := float64(from) / float64(to)
	for i := 0; i < n; i++ {
		p := float64(i) * step
		j := int(p)
		f := float32(p - float64(j))
		a := x[minInt(j, len(x)-1)]
		b := x[minInt(j+1, len(x)-1)]
		out[i] = a + (b-a)*f
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// WavWriter пишет 16 кГц моно 16 бит по мере записи; заголовок правится при закрытии.
type WavWriter struct {
	f     *os.File
	bytes int64
	Rate  int
}

func NewWavWriter(path string, rate int) (*WavWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := &WavWriter{f: f, Rate: rate}
	f.Write(make([]byte, 44))
	return w, nil
}

func (w *WavWriter) Write(s []float32) error {
	b := make([]byte, len(s)*2)
	for i, v := range s {
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		binary.LittleEndian.PutUint16(b[i*2:], uint16(int16(v*32767)))
	}
	_, err := w.f.Write(b)
	w.bytes += int64(len(b))
	return err
}

func (w *WavWriter) Seconds() float64 { return float64(w.bytes) / 2 / float64(w.Rate) }

func (w *WavWriter) header() []byte {
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+w.bytes))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], uint32(w.Rate))
	binary.LittleEndian.PutUint32(h[28:], uint32(w.Rate*2))
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(w.bytes))
	return h
}

// FlushHeader делает файл валидным в любой момент.
func (w *WavWriter) FlushHeader() { w.f.WriteAt(w.header(), 0) }

func (w *WavWriter) Close() error { w.FlushHeader(); return w.f.Close() }
