//go:build !windows

package appserver

import "os/exec"

func configureHiddenProcess(_ *exec.Cmd) {}
