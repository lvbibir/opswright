//go:build !windows

package session

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/lvbibir/opswright/internal/sshtest"
	"golang.org/x/crypto/ssh"
)

// ptyShell is sshtest's shell hook running a real /bin/bash on a pty, so the
// runner meets a shell with the echo, prompt and Ctrl-C a bastion gives.
func ptyShell(ch ssh.Channel, _ sshtest.PTY, resize <-chan sshtest.PTY) int {
	ptmx, err := pty.Start(exec.Command("/bin/bash"))
	if err != nil {
		return 127
	}
	defer ptmx.Close()
	go func() {
		for p := range resize {
			pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(p.Cols), Rows: uint16(p.Rows)})
		}
	}()
	go io.Copy(ptmx, ch)
	io.Copy(ch, ptmx)
	return 0
}

func shellRunnerFor(t *testing.T) *ShellRunner {
	t.Helper()
	srv := server(t)
	srv.Shell = ptyShell
	client, err := Dial(context.Background(), Config{Addr: srv.Addr, User: "demo", Credentials: password("secret"), HostKey: knownAll(t, srv).Callback()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	r, err := NewShellRunner(client, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestShellRunnerRun(t *testing.T) {
	r := shellRunnerFor(t)
	ctx := context.Background()

	var out bytes.Buffer
	if code, err := r.Run(ctx, "echo hi", &out); err != nil || code != 0 || out.String() != "hi\n" {
		t.Fatalf("echo: %q code %d err %v", out.String(), code, err)
	}
	out.Reset()
	if code, err := r.Run(ctx, "false", &out); err != nil || code != 1 || out.Len() != 0 {
		t.Fatalf("false: %q code %d err %v", out.String(), code, err)
	}
	out.Reset()
	if code, err := r.Run(ctx, "printf 'a\\nb\\n'", &out); err != nil || code != 0 || out.String() != "a\nb\n" {
		t.Fatalf("multi: %q code %d err %v", out.String(), code, err)
	}
}

func TestShellRunnerCancelLeavesItClean(t *testing.T) {
	r := shellRunnerFor(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := r.Run(ctx, "sleep 30", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel: %v", err)
	}
	var out bytes.Buffer
	if code, err := r.Run(context.Background(), "echo after", &out); err != nil || code != 0 || out.String() != "after\n" {
		t.Fatalf("after cancel: %q code %d err %v", out.String(), code, err)
	}
}
