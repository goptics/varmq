//go:build unix

package examples

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const exampleTimeout = 60 * time.Second

// TestExamplesRun runs every example under this directory with `go run` and
// fails on a compile error, a panic, a non-zero exit, a hang (for example a
// missing Wait) or an example that prints nothing.
func TestExamplesRun(t *testing.T) {
	mains, err := filepath.Glob("*/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(mains) == 0 {
		t.Fatal("no examples found")
	}

	for _, m := range mains {
		dir := filepath.Dir(m)
		t.Run(dir, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), exampleTimeout)
			defer cancel()

			var stdout, stderr bytes.Buffer
			cmd := exec.CommandContext(ctx, "go", "run", "./"+dir)
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			// `go run` execs the example as a child process, so killing only
			// `go run` on timeout would leave a hung example running forever.
			// Run it in its own process group and kill the whole group.
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
			cmd.WaitDelay = 5 * time.Second

			err := cmd.Run()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				t.Fatalf("%s: did not finish in %ds (missing Wait?)", dir, int(exampleTimeout/time.Second))
			}
			if err != nil {
				t.Fatalf("%s: %v\n%s%s", dir, err, stdout.String(), stderr.String())
			}
			if strings.TrimSpace(stdout.String()) == "" {
				t.Fatalf("%s: printed nothing", dir)
			}
		})
	}
}
