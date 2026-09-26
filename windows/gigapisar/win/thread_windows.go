//go:build windows

package win

import "runtime"

func lockThread() { runtime.LockOSThread() }
