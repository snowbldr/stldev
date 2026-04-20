//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachedProcAttr puts the child in its own process group so a ctrl-c on the
// parent terminal doesn't get delivered to it directly — we kill it ourselves
// on shutdown.
func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killBuildGroup signals the full process group of cmd. The build command runs
// as `sh -c "..."`, which often spawns go, which spawns the built binary —
// signalling sh alone leaves those grandchildren running. Requires cmd to have
// been started with detachedProcAttr so it is its own group leader.
func killBuildGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}
