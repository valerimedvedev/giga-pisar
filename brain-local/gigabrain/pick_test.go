package main

import "testing"

func TestPickAsset(t *testing.T) {
	old := []asset{{Name: "llama-b6000-bin-win-cpu-x64.zip"}, {Name: "llama-b6000-bin-win-vulkan-x64.zip"},
		{Name: "llama-b6000-bin-win-cuda-12.4-x64.zip"}, {Name: "cudart-llama-bin-win-cuda-12.4-x64.zip"},
		{Name: "llama-b6000-bin-win-cuda-11.7-x64.zip"}, {Name: "llama-b6000-bin-win-cpu-arm64.zip"},
		{Name: "llama-b6000-bin-macos-arm64.zip"}, {Name: "llama-b6000-bin-ubuntu-x64.zip"}, {Name: "llama-b6000-bin-win-hip-x64.zip"}}
	newer := []asset{{Name: "llama-v0.5.0-windows-x64-cpu.zip"}, {Name: "llama-v0.5.0-windows-x64-cuda12.zip"},
		{Name: "llama-v0.5.0-windows-x64-cuda13.zip"}, {Name: "cudart-windows-x64-cuda12.zip"}, {Name: "llama-v0.5.0-windows-x64-vulkan.zip"},
		{Name: "llama-v0.5.0-windows-arm64.zip"}, {Name: "llama-v0.5.0-linux-x64.tar.gz"}, {Name: "llama-v0.5.0-macos-arm64.tar.gz"}, {Name: "llama-v0.5.0-src.tar.gz"}}
	bare := []asset{{Name: "llama-v0.5.0-win-x64.zip"}, {Name: "llama-v0.5.0-win-x64-avx512.zip"}}
	for _, tc := range []struct {
		set              []asset
		be, want, cudart string
	}{
		{old, "cpu", "llama-b6000-bin-win-cpu-x64.zip", ""},
		{old, "vulkan", "llama-b6000-bin-win-vulkan-x64.zip", ""},
		{old, "cuda", "llama-b6000-bin-win-cuda-12.4-x64.zip", "cudart-llama-bin-win-cuda-12.4-x64.zip"},
		{newer, "cpu", "llama-v0.5.0-windows-x64-cpu.zip", ""},
		{newer, "cuda", "llama-v0.5.0-windows-x64-cuda12.zip", "cudart-windows-x64-cuda12.zip"},
		{newer, "vulkan", "llama-v0.5.0-windows-x64-vulkan.zip", ""},
		{bare, "cpu", "llama-v0.5.0-win-x64.zip", ""},
	} {
		b, c := pickAssetFor(tc.set, tc.be, "windows", "amd64")
		if b.Name != tc.want || (tc.be == "cuda" && c.Name != tc.cudart) {
			t.Errorf("%s: got %q / cudart %q, want %q / %q", tc.be, b.Name, c.Name, tc.want, tc.cudart)
		}
	}
	if b, _ := pickAssetFor(old, "cpu", "darwin", "arm64"); b.Name != "llama-b6000-bin-macos-arm64.zip" {
		t.Errorf("macos: %q", b.Name)
	}
	if b, _ := pickAssetFor(newer, "cpu", "linux", "amd64"); b.Name != "llama-v0.5.0-linux-x64.tar.gz" {
		t.Errorf("linux: %q", b.Name)
	}
	if b, _ := pickAssetFor(bare, "cuda", "windows", "amd64"); b.Name != "" {
		t.Errorf("cuda в наборе без cuda: %q", b.Name)
	}
}
