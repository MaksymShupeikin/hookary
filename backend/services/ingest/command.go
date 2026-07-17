package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

var errCommandOutputLimit = errors.New("command output exceeded its limit")

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedBuffer) Write(payload []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		return 0, errCommandOutputLimit
	}
	if len(payload) > remaining {
		_, _ = b.buffer.Write(payload[:remaining])
		return remaining, errCommandOutputLimit
	}
	return b.buffer.Write(payload)
}

func (b *limitedBuffer) Bytes() []byte { return b.buffer.Bytes() }

// runCommand never invokes a shell. Every user-controlled URL is a separate
// argv element after "--", so it cannot become an option or executable code.
func runCommand(ctx context.Context, outputLimit int, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 5 * time.Second
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}

	stdout := &limitedBuffer{limit: outputLimit}
	stderr := &limitedBuffer{limit: 64 << 10}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		// Command output can contain the submitted URL, including its query
		// string. Do not propagate it into job errors or logs.
		return nil, fmt.Errorf("%s failed: %w", name, err)
	}
	return stdout.Bytes(), nil
}
