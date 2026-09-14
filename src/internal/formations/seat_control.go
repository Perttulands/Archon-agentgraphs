package formations

import (
	"bufio"
	"context"
	"errors"
	"github.com/Perttulands/Archon-agentgraphs/internal/core"
	"io"
	"os/exec"
)

type seatControl struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events chan struct{}
}

type existingServerClient struct{ realTmuxHarnessClient }

func (existingServerClient) StartKeeper(context.Context, string, string) error {
	return errors.New("standalone seats require an already running tmux server")
}

func openSeatControl(ctx context.Context, socket, session string) (*seatControl, error) {
	cmd := exec.CommandContext(ctx, core.TmuxBin(), "-S", socket, "-C", "attach-session", "-t", session)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, err
	}
	c := &seatControl{cmd: cmd, stdin: stdin, events: make(chan struct{}, 1)}
	go func() {
		defer close(c.events)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 65536), 4<<20)
		for scanner.Scan() {
			select {
			case c.events <- struct{}{}:
			default:
			}
		}
	}()
	// This is the only sizing operation, once per newly created session.
	if _, err := io.WriteString(stdin, "refresh-client -C 160,48\n"); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func (c *seatControl) Close() {
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil {
		c.cmd.Wait()
	}
}
