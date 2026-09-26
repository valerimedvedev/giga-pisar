// Тонкая связка с onnxruntime без cgo: библиотека грузится динамически
// (onnxruntime.dll на Windows, libonnxruntime.so для проверки на Linux),
// а функции берутся из таблицы OrtApi по номерам. Номера — позиции в
// struct OrtApi из onnxruntime_c_api.h; таблица только растёт, поэтому одни
// и те же номера годятся и для старых сборок (1.12 для Windows 7), и для новых.
package ort

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Позиции функций в таблице OrtApi (посчитаны по заголовку версии 29).
const (
	fnGetErrorMessage                  = 2
	fnCreateEnv                        = 3
	fnCreateSessionOptions             = 10
	fnCreateSessionFromArray           = 8
	fnRun                              = 9
	fnSetSessionGraphOptimizationLevel = 23
	fnSetIntraOpNumThreads             = 24
	fnSessionGetInputCount             = 30
	fnSessionGetOutputCount            = 31
	fnSessionGetInputName              = 36
	fnSessionGetOutputName             = 37
	fnCreateTensorWithDataAsOrtValue   = 49
	fnGetTensorMutableData             = 51
	fnGetTensorElementType             = 60
	fnGetDimensionsCount               = 61
	fnGetDimensions                    = 62
	fnGetTensorTypeAndShape            = 65
	fnCreateCpuMemoryInfo              = 69
	fnAllocatorFree                    = 76
	fnGetAllocatorWithDefaultOptions   = 78
	fnReleaseStatus                    = 93
	fnReleaseSession                   = 95
	fnReleaseValue                     = 96
	fnReleaseTensorTypeAndShapeInfo    = 99
	fnReleaseSessionOptions            = 100

	apiVersion = 12 // минимальная версия таблицы, где есть всё нужное (onnxruntime 1.12)

	typeFloat = 1
	typeInt32 = 6
	typeInt64 = 7
)

var (
	api       []uintptr // таблица функций
	env       uintptr
	allocator uintptr
	memInfo   uintptr
)

func call(fn int, args ...uintptr) uintptr {
	r, _, _ := purego.SyscallN(api[fn], args...)
	return r
}

// Вызов функции, возвращающей OrtStatus*: nil — успех.
func check(fn int, args ...uintptr) error {
	st := call(fn, args...)
	if st == 0 {
		return nil
	}
	msg := cstr(call(fnGetErrorMessage, st))
	call(fnReleaseStatus, st)
	return errors.New(msg)
}

func cstr(p uintptr) string {
	if p == 0 {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Pointer(p + uintptr(n))) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(p)), n))
}

func czstr(s string) *byte {
	b := append([]byte(s), 0)
	return &b[0]
}

// Load открывает библиотеку и готовит окружение. Вызывается один раз.
func Load(path string) error {
	if api != nil {
		return nil
	}
	getApiBase, err := loadLibrary(path)
	if err != nil {
		return err
	}
	base, _, _ := purego.SyscallN(getApiBase)
	if base == 0 {
		return errors.New("OrtGetApiBase вернул NULL")
	}
	getApi := *(*uintptr)(unsafe.Pointer(base)) // OrtApiBase.GetApi
	tbl, _, _ := purego.SyscallN(getApi, uintptr(apiVersion))
	if tbl == 0 {
		return fmt.Errorf("onnxruntime не даёт таблицу API версии %d (слишком старая библиотека)", apiVersion)
	}
	api = unsafe.Slice((*uintptr)(unsafe.Pointer(tbl)), fnReleaseSessionOptions+1)
	logid := czstr("giga-pisar")
	if err := check(fnCreateEnv, 3 /* ORT_LOGGING_LEVEL_ERROR */, uintptr(unsafe.Pointer(logid)), uintptr(unsafe.Pointer(&env))); err != nil {
		return err
	}
	runtime.KeepAlive(logid)
	if err := check(fnGetAllocatorWithDefaultOptions, uintptr(unsafe.Pointer(&allocator))); err != nil {
		return err
	}
	// OrtArenaAllocator=1, OrtMemTypeDefault=0
	return check(fnCreateCpuMemoryInfo, 1, 0, uintptr(unsafe.Pointer(&memInfo)))
}

// Session — загруженная модель с известными именами входов и выходов.
type Session struct {
	h        uintptr
	Inputs   []string
	Outputs  []string
	inNames  []*byte
	outNames []*byte
	model    []byte // модель держим, пока жива сессия
}

// NewSession грузит модель из памяти. threads — потоков на один прогон.
func NewSession(model []byte, threads int) (*Session, error) {
	if api == nil {
		return nil, errors.New("onnxruntime не загружен")
	}
	var opts uintptr
	if err := check(fnCreateSessionOptions, uintptr(unsafe.Pointer(&opts))); err != nil {
		return nil, err
	}
	defer call(fnReleaseSessionOptions, opts)
	if threads > 0 {
		if err := check(fnSetIntraOpNumThreads, opts, uintptr(threads)); err != nil {
			return nil, err
		}
	}
	if err := check(fnSetSessionGraphOptimizationLevel, opts, 99 /* ORT_ENABLE_ALL */); err != nil {
		return nil, err
	}
	s := &Session{model: model}
	if err := check(fnCreateSessionFromArray, env, uintptr(unsafe.Pointer(&model[0])), uintptr(len(model)), opts, uintptr(unsafe.Pointer(&s.h))); err != nil {
		return nil, err
	}
	var n uintptr
	check(fnSessionGetInputCount, s.h, uintptr(unsafe.Pointer(&n)))
	for i := uintptr(0); i < n; i++ {
		var p uintptr
		check(fnSessionGetInputName, s.h, i, allocator, uintptr(unsafe.Pointer(&p)))
		s.Inputs = append(s.Inputs, cstr(p))
		call(fnAllocatorFree, allocator, p)
	}
	check(fnSessionGetOutputCount, s.h, uintptr(unsafe.Pointer(&n)))
	for i := uintptr(0); i < n; i++ {
		var p uintptr
		check(fnSessionGetOutputName, s.h, i, allocator, uintptr(unsafe.Pointer(&p)))
		s.Outputs = append(s.Outputs, cstr(p))
		call(fnAllocatorFree, allocator, p)
	}
	for _, x := range s.Inputs {
		s.inNames = append(s.inNames, czstr(x))
	}
	for _, x := range s.Outputs {
		s.outNames = append(s.outNames, czstr(x))
	}
	return s, nil
}

func (s *Session) Close() {
	if s.h != 0 {
		call(fnReleaseSession, s.h)
		s.h = 0
	}
}

// Tensor — вход модели: float32 или int64 с формой.
type Tensor struct {
	F32   []float32
	I64   []int64
	Shape []int64
}

// Output — выход модели, скопированный в память Go.
type Output struct {
	F32   []float32
	I64   []int64
	I32   []int32
	Shape []int64
}

func (o *Output) Len() int {
	n := int64(1)
	for _, d := range o.Shape {
		n *= d
	}
	return int(n)
}

// Run прогоняет модель: входы в порядке s.Inputs, выходы в порядке s.Outputs.
func (s *Session) Run(inputs []Tensor) ([]Output, error) {
	if len(inputs) != len(s.Inputs) {
		return nil, fmt.Errorf("нужно %d входов, дано %d", len(s.Inputs), len(inputs))
	}
	vals := make([]uintptr, len(inputs))
	defer func() {
		for _, v := range vals {
			if v != 0 {
				call(fnReleaseValue, v)
			}
		}
	}()
	for i, t := range inputs {
		var data unsafe.Pointer
		var bytes uintptr
		var typ uintptr
		if t.I64 != nil {
			data, bytes, typ = unsafe.Pointer(&t.I64[0]), uintptr(len(t.I64)*8), typeInt64
		} else {
			if len(t.F32) == 0 {
				return nil, errors.New("пустой тензор")
			}
			data, bytes, typ = unsafe.Pointer(&t.F32[0]), uintptr(len(t.F32)*4), typeFloat
		}
		if err := check(fnCreateTensorWithDataAsOrtValue, memInfo, uintptr(data), bytes, uintptr(unsafe.Pointer(&t.Shape[0])), uintptr(len(t.Shape)), typ, uintptr(unsafe.Pointer(&vals[i]))); err != nil {
			return nil, err
		}
	}
	outs := make([]uintptr, len(s.Outputs))
	inPtrs := make([]*byte, len(s.inNames))
	copy(inPtrs, s.inNames)
	if err := check(fnRun, s.h, 0, uintptr(unsafe.Pointer(&s.inNames[0])), uintptr(unsafe.Pointer(&vals[0])), uintptr(len(vals)),
		uintptr(unsafe.Pointer(&s.outNames[0])), uintptr(len(outs)), uintptr(unsafe.Pointer(&outs[0]))); err != nil {
		return nil, err
	}
	runtime.KeepAlive(inputs)
	result := make([]Output, len(outs))
	for i, v := range outs {
		result[i] = readOutput(v)
		call(fnReleaseValue, v)
	}
	return result, nil
}

func readOutput(v uintptr) Output {
	var info uintptr
	check(fnGetTensorTypeAndShape, v, uintptr(unsafe.Pointer(&info)))
	var nd uintptr
	check(fnGetDimensionsCount, info, uintptr(unsafe.Pointer(&nd)))
	shape := make([]int64, nd)
	if nd > 0 {
		check(fnGetDimensions, info, uintptr(unsafe.Pointer(&shape[0])), nd)
	}
	var typ uintptr
	check(fnGetTensorElementType, info, uintptr(unsafe.Pointer(&typ)))
	call(fnReleaseTensorTypeAndShapeInfo, info)
	o := Output{Shape: shape}
	n := o.Len()
	var data uintptr
	check(fnGetTensorMutableData, v, uintptr(unsafe.Pointer(&data)))
	if data == 0 || n == 0 {
		return o
	}
	switch typ {
	case typeFloat:
		o.F32 = append([]float32(nil), unsafe.Slice((*float32)(unsafe.Pointer(data)), n)...)
	case typeInt64:
		o.I64 = append([]int64(nil), unsafe.Slice((*int64)(unsafe.Pointer(data)), n)...)
	case typeInt32:
		o.I32 = append([]int32(nil), unsafe.Slice((*int32)(unsafe.Pointer(data)), n)...)
	}
	return o
}
