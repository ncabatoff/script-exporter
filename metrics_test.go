package main

import (
	// "github.com/kylelemons/godebug/pretty"
	"context"
	"os"
	"os/exec"
	"time"

	. "gopkg.in/check.v1"
)

func (s MySuite) TestRunCommand(c *C) {
	// Test zero exit code
	out, err := runCommand(context.Background(), "true")
	c.Assert(err, IsNil)
	c.Check(out, Equals, "")

	// Test nonzero exit code
	out, err = runCommand(context.Background(), "false")
	c.Assert(err, Not(IsNil))
	c.Check(out, Equals, "")

	// Test stdout capture
	out, err = runCommand(context.Background(), "echo", "test")
	c.Assert(err, IsNil)
	c.Check(out, Equals, "test\n")

	// Test stderr causes failure
	out, err = runCommand(context.Background(), "sh", "-c", "echo err 1>&2")
	c.Assert(err, Not(IsNil))
}

func (s MySuite) TestRunCommandCancel(c *C) {
	os.Remove("1")
	os.Remove("2")
	_, err := runCommand(context.Background(), "bash", "-c", "touch 1; sleep 5; touch 2")
	c.Assert(err, IsNil)
	c.Assert(os.Remove("1"), IsNil)
	c.Assert(os.Remove("2"), IsNil)

	// Test we can timeout a shell script containing a sleep.  Racy...
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = runCommand(ctx, "bash", "-c", "touch 1; sleep 5; touch 2")
	elapsed := time.Since(start)
	c.Check(err, Equals, context.DeadlineExceeded)
	c.Check(os.Remove("1"), IsNil)
	c.Check(os.Remove("2"), Not(IsNil))
	c.Check(elapsed > time.Second, Equals, false)
}

// This method serves to document why runCommand is as big and ugly as it is.
func (s MySuite) TestRunCommandUnsafeCancel(c *C) {
	// Test we can timeout a shell script containing a sleep.  With runCommandUnsafe
	// we can do so, but killing the bash parent won't kill the inner sleep, so the
	// command doesn't return until it elapses.  Note that this doesn't apply to all
	// bourney shells, e.g. dash works just fine.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := runCommandUnsafe(ctx, "bash", "-c", "touch 1; sleep 5; touch 2")
	elapsed := time.Since(start)
	c.Assert(err, Not(IsNil))
	c.Assert(os.Remove("1"), IsNil)
	c.Assert(os.Remove("2"), Not(IsNil))
	c.Check(elapsed >= 5*time.Second, Equals, true)
}

func runCommandUnsafe(ctx context.Context, script string, args ...string) (string, error) {
	// Create a new context for the command so that we don't fight over
	// the Done() message.
	cmdctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(cmdctx, script, args...)

	out, err := cmd.Output()
	return string(out), err
}
