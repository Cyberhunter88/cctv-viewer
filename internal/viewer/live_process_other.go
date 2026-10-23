//go:build !linux

package viewer

import "os/exec"

func configureLiveProcess(_ *exec.Cmd) func() { return func() {} }
