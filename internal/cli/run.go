package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

// result is the outcome of running one xbps command.
type result struct {
	exitCode int
	stdout   string
	stderr   string
}

// run captures a command's output and returns it. Used for dry runs and for
// xbps-query lookups, where xbpretty reads what xbps says.
func (r *runner) run(name string, args ...string) (result, error) {
	r.echo(name, args)
	cmd := exec.Command(name, args...)
	cmd.Env = r.env()
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	res := result{stdout: out.String(), stderr: errb.String()}
	if err != nil {
		code, ok := exitCode(err)
		if !ok {
			return res, fmt.Errorf("running %s: %w", name, err)
		}
		res.exitCode = code
	}
	return res, nil
}

// stream runs a command with its output going straight to xbpretty's own
// stdout and stderr, so xbps's progress output stays visible, and forwards
// SIGINT and SIGTERM to it. It returns the child's exit code.
func (r *runner) stream(name string, args ...string) (int, error) {
	r.echo(name, args)
	cmd := exec.Command(name, args...)
	cmd.Env = r.env()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	// Tee stderr: xbps's own text goes out live, and xbpretty keeps a copy
	// for the failure hints in FR-017.
	tee := &teeWriter{w: os.Stderr}
	cmd.Stderr = tee
	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("starting %s: %w", name, err)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigs)
	go func() {
		for s := range sigs {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(s)
			}
		}
	}()

	err := cmd.Wait()
	r.lastStderr = tee.String()
	if err == nil {
		return 0, nil
	}
	code, ok := exitCode(err)
	if !ok {
		return 1, fmt.Errorf("waiting for %s: %w", name, err)
	}
	return code, nil
}

// runner executes xbps commands, honouring the verbose flag and the
// configured binary paths. It is declared in main.go with the rest of the
// invocation state.
func (r *runner) env() []string {
	// Every xbps call runs in the C locale so the captured patterns in
	// hints.go keep matching whatever the user's locale would say.
	return append(os.Environ(), "LC_ALL=C")
}

func (r *runner) echo(name string, args []string) {
	if !r.verbose || r.quiet {
		return
	}
	line := name
	if len(args) > 0 {
		line += " " + strings.Join(args, " ")
	}
	fmt.Fprintln(r.out, "verbose: "+line)
}

func exitCode(err error) (int, bool) {
	ee, ok := err.(*exec.ExitError)
	if !ok {
		return 0, false
	}
	// FR-014 / AC-014: a signalled child never had an exit code of its own,
	// so report the one a shell would: 128 plus the signal.
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal()), true
	}
	return ee.ExitCode(), true
}

// teeWriter writes to the underlying writer and remembers everything it saw.
type teeWriter struct {
	w   io.Writer
	buf bytes.Buffer
}

func (t *teeWriter) Write(p []byte) (int, error) {
	t.buf.Write(p)
	return t.w.Write(p)
}

func (t *teeWriter) String() string {
	return t.buf.String()
}
