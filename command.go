package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
)

// runCommand invokes script under sh.scriptPath, returning its stdout and
// any error that resulted.  Errors include the script exiting with nonzero
// status or via signal, the script writing to stderr, or the context
// reaching Done state.  In the latter case the error will be one of
// context.Canceled or context.DeadlineExceeded.
func runCommand(ctx context.Context, script string, args ...string) (string, error) {
	// Create a new context for the command so that we don't fight over
	// the Done() message.
	cmdctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(cmdctx, script, args...)

	// It'd be simpler to use cmd.Output(), which was what I tried first.
	// The problem is that due to https://github.com/golang/go/issues/18874
	// we then may fail to promptly timeout children that spawn their own
	// child processes.

	var pstdout, pstderr io.ReadCloser
	var err error
	pstdout, err = cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("unable to create stdout pipe: %v", err)
	}
	defer func(rc io.ReadCloser) {
		rc.Close()
	}(pstdout)

	pstderr, err = cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("unable to create stderr pipe: %v", err)
	}
	defer func(rc io.ReadCloser) {
		rc.Close()
	}(pstderr)

	err = cmd.Start()
	if err != nil {
		return "", fmt.Errorf("failed to start child: %v", err)
	}

	var stdout, stderr bytes.Buffer
	chdone := make(chan struct{}, 2)

	// These goroutines shouldn't leak because once Wait() returns, Copy()
	// inputs will be closed and thus the goroutines will return.
	go func() {
		io.Copy(&stdout, pstdout)
		chdone <- struct{}{}
	}()
	go func() {
		io.Copy(&stderr, pstderr)
		chdone <- struct{}{}
	}()

	closed, ctxdone := 0, false
	for !ctxdone && closed < 2 {
		select {
		case <-ctx.Done():
			// We may get partial stdout in this case, which is fine.
			ctxdone = true
		case <-chdone:
			closed++
		}
	}
	err = cmd.Wait()
	if ctxdone {
		err = ctx.Err()
	}
	if err == nil && stderr.Len() != 0 {
		err = fmt.Errorf("got stderr output: %v", stderr.String())
	}
	return stdout.String(), err
}
