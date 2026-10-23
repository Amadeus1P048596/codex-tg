//go:build !windows

package codexruntime

import "os/exec"

func configureHiddenProcess(_ *exec.Cmd) {}
