//go:build !linux

package tsp

import "os/exec"

func setProcAttr(cmd *exec.Cmd) {}
