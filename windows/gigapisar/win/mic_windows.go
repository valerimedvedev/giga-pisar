//go:build windows

// Микрофон через waveIn (winmm): работает на всех Windows от 7, без cgo.
package win

import (
	"errors"
	"sync"
	"syscall"
	"unsafe"
)

var (
	winmm              = syscall.NewLazyDLL("winmm.dll")
	waveInOpen         = winmm.NewProc("waveInOpen")
	waveInPrepareHdr   = winmm.NewProc("waveInPrepareHeader")
	waveInUnprepareHdr = winmm.NewProc("waveInUnprepareHeader")
	waveInAddBuffer    = winmm.NewProc("waveInAddBuffer")
	waveInStart        = winmm.NewProc("waveInStart")
	waveInStop         = winmm.NewProc("waveInStop")
	waveInReset        = winmm.NewProc("waveInReset")
	waveInClose        = winmm.NewProc("waveInClose")
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	createEvent        = kernel32.NewProc("CreateEventW")
	waitForSingle      = kernel32.NewProc("WaitForSingleObject")
	closeHandle        = kernel32.NewProc("CloseHandle")
)

type waveFormatEx struct {
	FormatTag, Channels               uint16
	SamplesPerSec, AvgBytesPerSec     uint32
	BlockAlign, BitsPerSample, CbSize uint16
}

type waveHdr struct {
	Data          uintptr
	BufferLength  uint32
	BytesRecorded uint32
	User          uintptr
	Flags         uint32
	Loops         uint32
	Next          uintptr
	Reserved      uintptr
}

const (
	callbackEvent = 0x00050000
	whdrDone      = 0x00000001
	Rate          = 16000
)

// Mic отдаёт звук кусками по 20 мс через onSamples (в своём потоке).
type Mic struct {
	onSamples func([]float32)
	h         uintptr
	event     uintptr
	hdrs      []*waveHdr
	bufs      [][]byte
	stop      chan struct{}
	done      sync.WaitGroup
	running   bool
}

func NewMic(onSamples func([]float32)) *Mic { return &Mic{onSamples: onSamples} }

func (m *Mic) Start() error {
	if m.running {
		return nil
	}
	ev, _, _ := createEvent.Call(0, 0, 0, 0)
	if ev == 0 {
		return errors.New("событие не создалось")
	}
	m.event = ev
	fmtx := waveFormatEx{FormatTag: 1, Channels: 1, SamplesPerSec: Rate, AvgBytesPerSec: Rate * 2, BlockAlign: 2, BitsPerSample: 16}
	r, _, _ := waveInOpen.Call(uintptr(unsafe.Pointer(&m.h)), 0xFFFFFFFF /* WAVE_MAPPER */, uintptr(unsafe.Pointer(&fmtx)), ev, 0, callbackEvent)
	if r != 0 {
		closeHandle.Call(ev)
		return mmError(r)
	}
	// четыре буфера по 20 мс: успеваем забирать без пропусков
	m.hdrs, m.bufs = nil, nil
	for i := 0; i < 4; i++ {
		buf := make([]byte, Rate/50*2)
		h := &waveHdr{Data: uintptr(unsafe.Pointer(&buf[0])), BufferLength: uint32(len(buf))}
		waveInPrepareHdr.Call(m.h, uintptr(unsafe.Pointer(h)), unsafe.Sizeof(*h))
		waveInAddBuffer.Call(m.h, uintptr(unsafe.Pointer(h)), unsafe.Sizeof(*h))
		m.hdrs = append(m.hdrs, h)
		m.bufs = append(m.bufs, buf)
	}
	if r, _, _ := waveInStart.Call(m.h); r != 0 {
		m.cleanup()
		return mmError(r)
	}
	m.running = true
	m.stop = make(chan struct{})
	m.done.Add(1)
	go m.loop()
	return nil
}

func (m *Mic) loop() {
	defer m.done.Done()
	out := make([]float32, Rate/50)
	for {
		select {
		case <-m.stop:
			return
		default:
		}
		waitForSingle.Call(m.event, 100)
		for i, h := range m.hdrs {
			if h.Flags&whdrDone == 0 {
				continue
			}
			n := int(h.BytesRecorded) / 2
			buf := m.bufs[i]
			for k := 0; k < n && k < len(out); k++ {
				out[k] = float32(int16(uint16(buf[k*2])|uint16(buf[k*2+1])<<8)) / 32768
			}
			if n > 0 && m.onSamples != nil {
				m.onSamples(out[:n])
			}
			h.Flags &^= whdrDone
			h.BytesRecorded = 0
			waveInAddBuffer.Call(m.h, uintptr(unsafe.Pointer(h)), unsafe.Sizeof(*h))
		}
	}
}

func (m *Mic) Stop() {
	if !m.running {
		return
	}
	m.running = false
	close(m.stop)
	m.done.Wait()
	m.cleanup()
}

func (m *Mic) cleanup() {
	if m.h != 0 {
		waveInStop.Call(m.h)
		waveInReset.Call(m.h)
		for _, h := range m.hdrs {
			waveInUnprepareHdr.Call(m.h, uintptr(unsafe.Pointer(h)), unsafe.Sizeof(*h))
		}
		waveInClose.Call(m.h)
		m.h = 0
	}
	if m.event != 0 {
		closeHandle.Call(m.event)
		m.event = 0
	}
}

func mmError(code uintptr) error {
	switch code {
	case 4:
		return errors.New("микрофон занят другой программой")
	case 32:
		return errors.New("микрофон не даёт 16 кГц")
	case 2:
		return errors.New("микрофон не найден")
	}
	return errors.New("микрофон не открылся (winmm " + itoa(int(code)) + ")")
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}
