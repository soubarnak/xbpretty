package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/soubarnak/xbpretty/internal/hints"
)

// result is the outcome of running one xbps command.
type result struct {
	exitCode int
	stdout   string
	stderr   string
}

// output is both streams of the run, as the hint table reads them: xbps writes
// some failures to stdout and some to stderr (ADR-0006).
func (r result) output() hints.Output {
	return hints.Output{Stdout: r.stdout, Stderr: r.stderr}
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

func (r *runner) stdinReader() io.Reader {
	if r.stdin != nil {
		return r.stdin
	}
	return os.Stdin
}

func (r *runner) stdoutWriter() io.Writer {
	if r.stdout != nil {
		return r.stdout
	}
	return os.Stdout
}

func (r *runner) stderrWriter() io.Writer {
	if r.stderr != nil {
		return r.stderr
	}
	return os.Stderr
}

func (r *runner) isInteractive() bool {
	if r.verbose {
		return false
	}
	return isTTY(r.stdoutWriter())
}

// stream runs a command with its output streamed live, and forwards
// SIGINT and SIGTERM to it. It returns the child's exit code.
//
// In interactive mode (isTTY), it uses a hybrid engine spawning stdout on a PTY
// to force libc line-buffering during unpacking and configuring, and stderr on a pipe
// to capture live download telemetry ticks and errors without block buffering.
// In non-interactive mode or when -v/--verbose is passed, it falls back to direct pipes.
func (r *runner) stream(name string, args ...string) (int, error) {
	if r.isInteractive() {
		return r.streamHybrid(name, args...)
	}
	return r.streamDirect(name, args...)
}

func (r *runner) streamHybrid(name string, args ...string) (int, error) {
	r.echo(name, args)
	if r.parser == nil {
		r.parser = NewStreamParser(0, 0)
	}

	master, slave, err := openPty()
	if err != nil {
		return r.streamDirect(name, args...)
	}
	defer master.Close()

	cmd := exec.Command(name, args...)
	cmd.Env = r.env()
	cmd.Stdin = r.stdinReader()
	cmd.Stdout = slave

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = slave.Close()
		return 1, fmt.Errorf("creating stderr pipe for %s: %w", name, err)
	}

	if err := cmd.Start(); err != nil {
		_ = slave.Close()
		return 1, fmt.Errorf("starting %s: %w", name, err)
	}
	_ = slave.Close()

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer func() {
		signal.Stop(winch)
		close(winch)
	}()
	if f, ok := r.stdinReader().(*os.File); ok {
		_ = inheritPtySize(f, master)
	}
	go func() {
		for range winch {
			if f, ok := r.stdinReader().(*os.File); ok {
				_ = inheritPtySize(f, master)
			}
			if r.widget != nil {
				if f, ok := r.stderrWriter().(*os.File); ok {
					if tw := terminalWidth(f); tw > 0 {
						r.widget.SetWidth(tw)
					}
				}
			}
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer func() {
		signal.Stop(sigs)
		close(sigs)
	}()
	go func() {
		for s := range sigs {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(s)
			}
		}
	}()

	var rawStdout bytes.Buffer
	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		scanner := bufio.NewScanner(master)
		for scanner.Scan() {
			line := scanner.Text()
			rawStdout.WriteString(line)
			rawStdout.WriteByte('\n')
			if r.parser != nil {
				r.parser.FeedStdout(line)
			}
			if r.widget == nil {
				if out := r.stdoutWriter(); out != nil {
					fmt.Fprintln(out, line)
				}
			}
		}
	}()

	var rawStderr bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		pr, pw := io.Pipe()
		var mw io.Writer
		if r.widget == nil {
			mw = io.MultiWriter(&rawStderr, r.stderrWriter(), pw)
		} else {
			mw = io.MultiWriter(&rawStderr, pw)
		}
		parserDone := make(chan struct{})
		go func() {
			defer close(parserDone)
			s := bufio.NewScanner(pr)
			s.Split(scanLinesOrCR)
			for s.Scan() {
				if r.parser != nil {
					r.parser.FeedStderr(s.Text())
				}
			}
		}()

		_, _ = io.Copy(mw, stderrPipe)
		_ = pw.Close()
		<-parserDone
	}()

	<-stdoutDone
	<-stderrDone

	err = cmd.Wait()
	r.lastStdout = rawStdout.String()
	r.lastStderr = rawStderr.String()
	return waitCode(cmd, err, name)
}

func (r *runner) streamDirect(name string, args ...string) (int, error) {
	r.echo(name, args)
	cmd := exec.Command(name, args...)
	cmd.Env = r.env()
	cmd.Stdin = r.stdinReader()
	cmd.Stdout = r.stdoutWriter()

	tee := &teeWriter{w: r.stderrWriter()}
	cmd.Stderr = tee
	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("starting %s: %w", name, err)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer func() {
		signal.Stop(sigs)
		close(sigs)
	}()
	go func() {
		for s := range sigs {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(s)
			}
		}
	}()

	err := cmd.Wait()
	r.lastStderr = tee.String()
	return waitCode(cmd, err, name)
}

// streamStderr runs a diagnostic xbps step with both streams displayed on
// stderr, forwards SIGINT/SIGTERM to the child, and returns its exit code plus
// the two captured streams separately for the hint table. The index sync uses
// it so its output is never durable stdout and an interrupt still reaches the
// child (FR-013, FR-074). ADR-0006 needs the streams kept apart even though
// both are displayed on stderr.
func (r *runner) streamStderr(name string, args ...string) (int, hints.Output, error) {
	r.echo(name, args)
	cmd := exec.Command(name, args...)
	cmd.Env = r.env()
	cmd.Stdin = r.stdinReader()
	// stdout and stderr are copied by two goroutines; display them through
	// one serialized writer so a non-*os.File sink (a test buffer, for
	// example) never sees concurrent writes.
	display := &serializedWriter{w: r.stderrWriter()}
	outTee := &teeWriter{w: display}
	errTee := &teeWriter{w: display}
	cmd.Stdout = outTee
	cmd.Stderr = errTee
	if err := cmd.Start(); err != nil {
		return 1, hints.Output{}, fmt.Errorf("starting %s: %w", name, err)
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer func() {
		signal.Stop(sigs)
		close(sigs)
	}()
	go func() {
		for s := range sigs {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(s)
			}
		}
	}()
	err := cmd.Wait()
	output := hints.Output{Stdout: outTee.String(), Stderr: errTee.String()}
	code, waitErr := waitCode(cmd, err, name)
	if waitErr != nil {
		return 1, output, waitErr
	}
	return code, output, nil
}

// waitCode turns exec.Cmd.Wait's error into the code a shell would see. A
// copy goroutine that fails with EPIPE (because stdout or stderr is a closed
// pipe) must not hide the child's real status: FR-074 keeps the run's code.
func waitCode(cmd *exec.Cmd, err error, name string) (int, error) {
	if err == nil {
		return 0, nil
	}
	if code, ok := exitCode(err); ok {
		return code, nil
	}
	if isBrokenPipe(err) || errors.Is(err, io.ErrClosedPipe) {
		if code, ok := processExitCode(cmd.ProcessState); ok {
			return code, nil
		}
	}
	return 1, fmt.Errorf("waiting for %s: %w", name, err)
}

// processExitCode reads the child's own status when Wait returned a copy
// error rather than an *exec.ExitError.
func processExitCode(ps *os.ProcessState) (int, bool) {
	if ps == nil {
		return 0, false
	}
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal()), true
	}
	code := ps.ExitCode()
	if code < 0 {
		return 0, false
	}
	return code, true
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

// serializedWriter makes a single writer safe for two copy goroutines.
type serializedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *serializedWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
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
