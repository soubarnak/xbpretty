package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
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
	defer signal.Stop(winch)
	if f, ok := r.stdinReader().(*os.File); ok {
		_ = inheritPtySize(f, master)
	}
	go func() {
		for range winch {
			if f, ok := r.stdinReader().(*os.File); ok {
				_ = inheritPtySize(f, master)
			}
		}
	}()

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
	if err == nil {
		return 0, nil
	}
	code, ok := exitCode(err)
	if !ok {
		return 1, fmt.Errorf("waiting for %s: %w", name, err)
	}
	return code, nil
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
