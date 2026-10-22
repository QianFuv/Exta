package main

import (
	"os/exec"
	"syscall"
)

func hideProcess(process *exec.Cmd) { process.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
