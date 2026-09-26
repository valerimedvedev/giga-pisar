//go:build !windows

package localllm

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
