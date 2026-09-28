//go:build windows

package components

import (
	"os"
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestConfigureProcessUsesCtrlBreakForCancellation(t *testing.T) {
	const processID = 4242

	var gotEvent uint32
	var gotProcessGroupID uint32
	sendConsoleEvent := func(event uint32, processGroupID uint32) error {
		gotEvent = event
		gotProcessGroupID = processGroupID

		return nil
	}

	command := exec.CommandContext(t.Context(), "component.exe")
	command.Process = &os.Process{Pid: processID}
	configureWindowsProcess(command, sendConsoleEvent)

	if command.SysProcAttr == nil ||
		command.SysProcAttr.CreationFlags&windows.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatal("managed process does not start in a new Windows process group")
	}
	if err := command.Cancel(); err != nil {
		t.Fatalf("cancel managed process: %v", err)
	}
	if gotEvent != windows.CTRL_BREAK_EVENT {
		t.Errorf("console event = %d, want CTRL_BREAK_EVENT", gotEvent)
	}
	if gotProcessGroupID != processID {
		t.Errorf("process group ID = %d, want %d", gotProcessGroupID, processID)
	}
}
