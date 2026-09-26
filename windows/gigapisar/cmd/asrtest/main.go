// Сверка с питоновским ядром: asrtest <библиотека onnxruntime> <папка модели> <папка wav с ref_transcripts.json>
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gigapisar/asr"
	"gigapisar/ort"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("asrtest <onnxruntime lib> <model dir> <wav dir>")
		os.Exit(2)
	}
	if err := ort.Load(os.Args[1]); err != nil {
		panic(err)
	}
	r, err := asr.Load(os.Args[2], 4)
	if err != nil {
		panic(err)
	}
	var ref map[string]string
	b, _ := os.ReadFile(filepath.Join(os.Args[3], "ref_transcripts.json"))
	json.Unmarshal(b, &ref)
	names := make([]string, 0, len(ref))
	for k := range ref {
		names = append(names, k)
	}
	sort.Strings(names)
	ok := 0
	for _, name := range names {
		wb, err := os.ReadFile(filepath.Join(os.Args[3], name))
		if err != nil {
			continue
		}
		s, rate, err := asr.ReadWav(wb)
		if err != nil {
			panic(err)
		}
		s = asr.Resample(s, rate, r.SampleRate())
		t0 := time.Now()
		got, err := r.Transcribe(s)
		if err != nil {
			panic(err)
		}
		mark := "✗"
		if got == ref[name] {
			mark = "✓"
			ok++
		}
		fmt.Printf("%s %s (%d с → %d мс): %s\n", mark, name, len(s)/16000, time.Since(t0).Milliseconds(), got)
		if got != ref[name] {
			fmt.Printf("   питон: %s\n", ref[name])
		}
	}
	fmt.Printf("совпало %d из %d\n", ok, len(names))
}
