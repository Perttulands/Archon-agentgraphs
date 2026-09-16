package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

const (
	defaultNotifyCommandTimeout = 30 * time.Second
	notifyCommandStderrBytes    = 4 << 10
)

// CommandNotifier delivers a notification by running one host command, with no
// shell and no arguments, and writing the notification JSON to its stdin. Exit
// status 0 means delivered. The host command owns the channel and recipient.
type CommandNotifier struct {
	Path    string
	Timeout time.Duration
}

func (n CommandNotifier) NotifyNeedsYou(ctx context.Context, notification formations.NeedsYouNotification) error {
	payload, err := json.Marshal(notification)
	if err != nil {
		return err
	}
	timeout := n.Timeout
	if timeout <= 0 {
		timeout = defaultNotifyCommandTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, n.Path)
	cmd.Stdin = bytes.NewReader(payload)
	stderr := &headBuffer{limit: notifyCommandStderrBytes}
	cmd.Stderr = stderr
	// A timed-out command and anything it started end together.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if text := strings.TrimSpace(stderr.String()); text != "" {
		log.Printf("needs-you command, run %s ask %d, stderr: %s", notification.RunID, notification.Seq, text)
	}
	switch {
	case err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("notify command timed out after %s", timeout)
	case err != nil:
		return fmt.Errorf("notify command failed: %w", err)
	}
	return nil
}

// headBuffer keeps the first limit bytes written and discards the rest.
type headBuffer struct {
	limit     int
	buf       bytes.Buffer
	truncated bool
}

func (b *headBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
			b.truncated = true
		} else {
			b.buf.Write(p)
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func (b *headBuffer) String() string {
	if b.truncated {
		return b.buf.String() + " [stderr truncated]"
	}
	return b.buf.String()
}
