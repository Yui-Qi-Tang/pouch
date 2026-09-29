//go:build !unix

package sat

import (
	"os/exec"
	"time"
)

func controlProcess(cmd *exec.Cmd) { cmd.WaitDelay = 2 * time.Second }
