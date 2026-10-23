package viewer

import (
	"os/exec"
	"runtime"
	"syscall"
)

// Also stop the supervised daemon if the viewer itself is killed unexpectedly.
// go2rtc applies the same parent-death rule to its FFmpeg children on Linux.
func configureLiveProcess(command *exec.Cmd) func() {
	// Linux associates Pdeathsig with the creating OS thread. Keep that thread
	// alive until the child has exited, even when Go reschedules other work.
	runtime.LockOSThread()
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	return runtime.UnlockOSThread
}
