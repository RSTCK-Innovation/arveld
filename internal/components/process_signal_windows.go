//go:build windows

package components

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureProcess(command *exec.Cmd) {
	configureWindowsProcess(command, windows.GenerateConsoleCtrlEvent)
}

func configureWindowsProcess(command *exec.Cmd, sendConsoleEvent func(uint32, uint32) error) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
	}
	command.Cancel = func() error {
		return sendConsoleEvent(
			windows.CTRL_BREAK_EVENT,
			uint32(command.Process.Pid), //nolint:gosec // Windows process IDs are DWORD values
		)
	}
}
