//go:build !windows

package components

import (
	"os"
	"os/exec"
)

func configureProcess(command *exec.Cmd) {
	command.Cancel = func() error {
		return command.Process.Signal(os.Interrupt)
	}
}
