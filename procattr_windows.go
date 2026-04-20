//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: 0x00000200}
}

// killBuildGroup best-effort kills the build process on Windows. Doesn't walk
// the process tree — Windows doesn't have Unix-style process groups — but this
// at least stops the top-level shell.
func killBuildGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
