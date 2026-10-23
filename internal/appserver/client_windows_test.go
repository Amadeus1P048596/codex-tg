//go:build windows

package appserver

import (
	"testing"
	"time"
)

func TestBuildCommandUsesWindowlessChildProcessOnWindows(t *testing.T) {
	client := NewClient(`C:\Codex\codex.exe`, "stdio://", t.TempDir(), time.Second)
	cmd, err := client.buildCommand()
	if err != nil {
		t.Fatalf("buildCommand failed: %v", err)
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatal("App Server child is not configured with HideWindow")
	}
	if cmd.SysProcAttr.CreationFlags&0x08000000 == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NO_WINDOW", cmd.SysProcAttr.CreationFlags)
	}
}
