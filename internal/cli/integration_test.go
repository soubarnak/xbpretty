package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// integration tests drive the real binary against the stub xbps binaries in
// testdata/stub, so the whole flow runs without touching a package system.

func buildBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "xbpretty")
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/xbpretty")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building test binary: %v\n%s", err, out)
	}
	return bin
}

// integrationConfig writes a rev 6 default layer pointing at the stubs, with
// the given [Color] mode and any extra lines appended. It returns the
// XDG_CONFIG_HOME directory and the file path.
func integrationConfig(t *testing.T, colorMode string, extra ...string) (cfgDir, cfgPath string) {
	t.Helper()
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	cfgDir = filepath.Join(t.TempDir(), "config")
	if err := os.MkdirAll(filepath.Join(cfgDir, "xbpretty"), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"[Xbps]",
		"install_path = \"" + filepath.Join(stubDir, "xbps-install") + "\"",
		"remove_path = \"" + filepath.Join(stubDir, "xbps-remove") + "\"",
		"query_path = \"" + filepath.Join(stubDir, "xbps-query") + "\"",
		"",
		"[Color]",
		"mode = \"" + colorMode + "\"",
	}
	lines = append(lines, extra...)
	cfgPath = filepath.Join(cfgDir, "xbpretty", "config.toml")
	if err := os.WriteFile(cfgPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgDir, cfgPath
}

// integrationEnv sets up a config pointing the built binary at the stubs.
func integrationEnv(t *testing.T) (bin, logPath, cfgDir string) {
	t.Helper()
	bin = buildBinary(t)
	cfgDir, _ = integrationConfig(t, "Never")
	return bin, filepath.Join(filepath.Dir(cfgDir), "commands.log"), cfgDir
}

// integrationCmd returns a command with the environment every integration
// test shares.
func integrationCmd(bin string, cfgDir, logPath string, args ...string) *exec.Cmd {
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+cfgDir,
		"XDG_STATE_HOME="+filepath.Join(filepath.Dir(cfgDir), "state"),
		"XB_STUB_LOG="+logPath,
		"XB_STUB_STDOUT="+planFixture,
		"NO_COLOR=",
	)
	return cmd
}

func TestIntegrationPipedStdinNeverBlocks(t *testing.T) {
	bin, logPath, cfgDir := integrationEnv(t)
	cmd := exec.Command(bin, "install", "figlet")
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+cfgDir,
		"XDG_STATE_HOME="+filepath.Dir(cfgDir)+"/state",
		"XB_STUB_LOG="+logPath,
		"XB_STUB_STDOUT="+planFixture,
		"NO_COLOR=1",
	)
	cmd.Stdin = nil // a closed pipe, not a terminal
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			t.Errorf("exit = %v, want 1", err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("xbpretty blocked on a pipe instead of exiting")
	}
	errs := stderr.String()
	if strings.Contains(errs, "[Y/n]") {
		t.Errorf("prompted on a pipe:\n%s", errs)
	}
	// FR-073 is unconditional for non-root, so a non-root piped run gets the
	// elevation hint, not the non-TTY message. AC-012's non-TTY line is the
	// root path.
	if os.Geteuid() == 0 {
		if !strings.Contains(errs, "-y") {
			t.Errorf("missing the -y hint:\n%s", errs)
		}
		if strings.Contains(errs, "sudo") || strings.Contains(errs, "doas") {
			t.Errorf("root run hit the privilege guard:\n%s", errs)
		}
	} else {
		if !strings.Contains(errs, "sudo") && !strings.Contains(errs, "doas") {
			t.Errorf("non-root run is missing the elevation hint:\n%s", errs)
		}
		if strings.Contains(errs, "stdin is not a terminal") {
			t.Errorf("non-root run took the root-only non-TTY path:\n%s", errs)
		}
	}
}

func TestIntegrationLinksDispatchLikeTheCommand(t *testing.T) {
	bin, logPath, cfgDir := integrationEnv(t)
	dir := t.TempDir()
	out, err := exec.Command(bin, "link", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("link: %v\n%s", err, out)
	}
	link := filepath.Join(dir, "xbp-remove")
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("xbp-remove was not created: %v", err)
	}
	// Invoking through the link must behave like `xbpretty remove`.
	cmd := exec.Command(link, "figlet", "--dry-run")
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+cfgDir,
		"XDG_STATE_HOME="+filepath.Dir(cfgDir)+"/state",
		"XB_STUB_LOG="+logPath,
		"XB_STUB_STDOUT="+planFixture,
		"NO_COLOR=1",
	)
	cmd.Stdin = nil
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("running through the link: %v\n%s", err, stderr.String())
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "figlet") {
		t.Errorf("the linked run did not reach xbps-remove:\n%s", raw)
	}
	// A remove plan, not an install plan: xbps-remove was called.
	if !strings.Contains(string(raw), "\nfiglet") && !strings.Contains(string(raw), "figlet\n") {
		t.Errorf("unexpected command log:\n%s", raw)
	}
}

// TestIntegrationConfigAssumeYesEndToEnd pins the first behaviour AC of this
// ticket: [Nala] assume_yes, not a flag, takes a piped stdin past the "pass
// -y" refusal at the real binary seam.
func TestIntegrationConfigAssumeYesEndToEnd(t *testing.T) {
	bin := buildBinary(t)
	cfgDir, _ := integrationConfig(t, "Never", "", "[Nala]", "assume_yes = true")
	logPath := filepath.Join(filepath.Dir(cfgDir), "commands.log")
	cmd := integrationCmd(bin, cfgDir, logPath, "install", "figlet")
	cmd.Stdin = nil
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 2 {
		t.Fatalf("config rejected: %v\n%s", err, stderr.String())
	}
	errs := stderr.String()
	if strings.Contains(errs, "stdin is not a terminal") {
		t.Errorf("assume_yes did not take the run past the non-TTY refusal:\n%s", errs)
	}
	if !strings.Contains(stdout.String(), "Install") {
		t.Errorf("summary missing from stdout:\n%s", stdout.String())
	}
	if err != nil && !strings.Contains(errs, "root") && !strings.Contains(errs, "doas") && !strings.Contains(errs, "sudo") {
		t.Errorf("unexpected failed run: %v\n%s", err, errs)
	}
}

// TestIntegrationConfigColorModeEndToEnd drives [Color] mode through the real
// binary on a pseudo-terminal, where Auto would also colour but Always and
// Never decide it: the config, not a flag, is the switch.
func TestIntegrationConfigColorModeEndToEnd(t *testing.T) {
	bin := buildBinary(t)
	for _, tt := range []struct {
		mode     string
		wantANSI bool
	}{
		{"Always", true},
		{"Never", false},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			cfgDir, _ := integrationConfig(t, tt.mode)
			logPath := filepath.Join(filepath.Dir(cfgDir), "commands.log")
			master, slave, err := openPty()
			if err != nil {
				t.Skipf("no pseudo-terminal: %v", err)
			}
			defer master.Close()
			cmd := integrationCmd(bin, cfgDir, logPath, "install", "--dry-run", "figlet")
			cmd.Stdin = nil
			cmd.Stdout = slave
			var stderr strings.Builder
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				slave.Close()
				t.Fatal(err)
			}
			slave.Close()
			outCh := make(chan string, 1)
			go func() {
				var b strings.Builder
				buf := make([]byte, 4096)
				for {
					n, err := master.Read(buf)
					b.Write(buf[:n])
					if err != nil {
						break
					}
				}
				outCh <- b.String()
			}()
			if err := cmd.Wait(); err != nil {
				t.Fatalf("run: %v\n%s", err, stderr.String())
			}
			out := <-outCh
			if got := strings.Contains(out, "\x1b["); got != tt.wantANSI {
				t.Errorf("[Color] mode %s: ANSI = %v, want %v\n%s", tt.mode, got, tt.wantANSI, out)
			}
		})
	}
}

// TestIntegrationConfigUnitEndToEnd drives [Ui] unit into the size output:
// the same plan prints KiB under Binary and KB under Decimal.
func TestIntegrationConfigUnitEndToEnd(t *testing.T) {
	bin := buildBinary(t)
	for _, tt := range []struct {
		unit      string
		want      string
		notWanted string
	}{
		{"Binary", "KiB", "KB"},
		{"Decimal", "KB", "KiB"},
	} {
		t.Run(tt.unit, func(t *testing.T) {
			cfgDir, _ := integrationConfig(t, "Never", "", "[Ui]", `unit = "`+tt.unit+`"`)
			logPath := filepath.Join(filepath.Dir(cfgDir), "commands.log")
			cmd := integrationCmd(bin, cfgDir, logPath, "install", "--dry-run", "figlet")
			cmd.Stdin = nil
			var stdout, stderr strings.Builder
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("run: %v\n%s", err, stderr.String())
			}
			out := stdout.String()
			if !strings.Contains(out, tt.want) {
				t.Errorf("[Ui] unit %s: output has no %s:\n%s", tt.unit, tt.want, out)
			}
			if strings.Contains(out, tt.notWanted) {
				t.Errorf("[Ui] unit %s: output also has %s:\n%s", tt.unit, tt.notWanted, out)
			}
		})
	}
}

// TestIntegrationInvalidConfigIsExactHardError pins the error surface:
// xbpretty: config <path>:<line>: <message>, exit 2.
func TestIntegrationInvalidConfigIsExactHardError(t *testing.T) {
	bin := buildBinary(t)
	cfgDir, cfgPath := integrationConfig(t, "Never")
	if err := os.WriteFile(cfgPath, []byte("[Ui]\nmode = \"auto\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(filepath.Dir(cfgDir), "commands.log")
	cmd := integrationCmd(bin, cfgDir, logPath, "install", "--dry-run", "figlet")
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 2 {
		t.Fatalf("exit = %v, want 2\n%s", err, stderr.String())
	}
	prefix := "xbpretty: config " + cfgPath + ":2: "
	if !strings.Contains(stderr.String(), prefix) {
		t.Errorf("stderr missing %q:\n%s", prefix, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("config error wrote to stdout:\n%s", stdout.String())
	}
}

// TestIntegrationBypassCommandsDoNotLoadConfig pins FR-053's bypass list with
// an invalid default file present: every bypass still succeeds.
func TestIntegrationBypassCommandsDoNotLoadConfig(t *testing.T) {
	bin := buildBinary(t)
	cfgDir, cfgPath := integrationConfig(t, "Never")
	if err := os.WriteFile(cfgPath, []byte("this is not TOML\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(filepath.Dir(cfgDir), "commands.log")
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	linkDir := t.TempDir()
	missing := filepath.Join(t.TempDir(), "absent.toml")
	tests := [][]string{
		{"--help"},
		{"--version"},
		{"completion", "bash"},
		{"link", linkDir},
		{"--config", missing, "--help"},
	}
	for _, argv := range tests {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			cmd := integrationCmd(bin, cfgDir, logPath, argv...)
			cmd.Env = append(cmd.Env, "PATH="+stubDir+":"+os.Getenv("PATH"))
			var stdout, stderr strings.Builder
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("bypass command loaded config: %v\n%s", err, stderr.String())
			}
			if strings.Contains(stderr.String(), "config ") {
				t.Errorf("bypass command reported a config error:\n%s", stderr.String())
			}
		})
	}
}

// TestIntegrationMissingDefaultConfigIsSilent pins the complement: with no
// default file at all, a normal run proceeds.
func TestIntegrationMissingDefaultConfigIsSilent(t *testing.T) {
	bin := buildBinary(t)
	cfgDir := filepath.Join(t.TempDir(), "config") // no xbpretty/config.toml
	stubDir, err := filepath.Abs("../../testdata/stub")
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(filepath.Dir(cfgDir), "commands.log")
	cmd := integrationCmd(bin, cfgDir, logPath, "install", "--dry-run", "figlet")
	cmd.Env = append(cmd.Env, "PATH="+stubDir+":"+os.Getenv("PATH"))
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("missing default file was not silent: %v\n%s", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "config ") {
		t.Errorf("missing default file produced a config message:\n%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Install") {
		t.Errorf("summary missing:\n%s", stdout.String())
	}
}

// writeIntegrationConfigFile writes one config file at path.
func writeIntegrationConfigFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestIntegrationConfigFlagSelectsFile pins --config: it replaces the default
// user layer, is accepted before and after the subcommand, is last-wins and
// changes behaviour through config.LoadFile.
func TestIntegrationConfigFlagSelectsFile(t *testing.T) {
	bin := buildBinary(t)
	cfgDir, cfgPath := integrationConfig(t, "Never")
	defaultBody, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	// The default layer is invalid: a successful run proves --config replaced
	// it rather than merely supplementing it.
	writeIntegrationConfigFile(t, cfgPath, "[Ui]\nmode = \"not-a-mode\"\n")
	logPath := filepath.Join(filepath.Dir(cfgDir), "commands.log")

	decimal := filepath.Join(t.TempDir(), "decimal.toml")
	writeIntegrationConfigFile(t, decimal, string(defaultBody)+"\n[Ui]\nunit = \"Decimal\"\n")
	binaryCfg := filepath.Join(t.TempDir(), "binary.toml")
	writeIntegrationConfigFile(t, binaryCfg, string(defaultBody))

	for _, tt := range []struct {
		name string
		argv []string
		want string
	}{
		{"before the subcommand", []string{"--config", decimal, "install", "--dry-run", "figlet"}, "KB"},
		{"after the subcommand", []string{"install", "--config", decimal, "--dry-run", "figlet"}, "KB"},
		{"last one wins", []string{"--config", decimal, "install", "--dry-run", "figlet", "--config", binaryCfg}, "KiB"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := integrationCmd(bin, cfgDir, logPath, tt.argv...)
			var stdout, stderr strings.Builder
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("run: %v\n%s", err, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.want) {
				t.Errorf("output does not use %s:\n%s", tt.want, stdout.String())
			}
			if strings.Contains(stderr.String(), "config ") {
				t.Errorf("unexpected config error:\n%s", stderr.String())
			}
		})
	}
}

// TestIntegrationConfigFlagErrors pins --config's hard errors: a missing or
// invalid file and the two stream paths FR-053 rejects.
func TestIntegrationConfigFlagErrors(t *testing.T) {
	bin := buildBinary(t)
	cfgDir, _ := integrationConfig(t, "Never")
	logPath := filepath.Join(filepath.Dir(cfgDir), "commands.log")

	t.Run("missing file", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "absent.toml")
		cmd := integrationCmd(bin, cfgDir, logPath, "--config", missing, "install", "--dry-run", "figlet")
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		ee, ok := err.(*exec.ExitError)
		if !ok || ee.ExitCode() != 2 {
			t.Fatalf("exit = %v, want 2\n%s", err, stderr.String())
		}
		if !strings.Contains(stderr.String(), "xbpretty: config "+missing+":") {
			t.Errorf("missing config envelope for %q:\n%s", missing, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Errorf("config error wrote to stdout:\n%s", stdout.String())
		}
	})

	t.Run("invalid file", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "bad.toml")
		writeIntegrationConfigFile(t, bad, "[Ui]\nmode = \"Auto\"\nunit = \"binary\"\n")
		cmd := integrationCmd(bin, cfgDir, logPath, "install", "--config", bad, "--dry-run", "figlet")
		var stderr strings.Builder
		cmd.Stderr = &stderr
		err := cmd.Run()
		ee, ok := err.(*exec.ExitError)
		if !ok || ee.ExitCode() != 2 {
			t.Fatalf("exit = %v, want 2\n%s", err, stderr.String())
		}
		if !strings.Contains(stderr.String(), "xbpretty: config "+bad+":3:") {
			t.Errorf("invalid config envelope missing line 3:\n%s", stderr.String())
		}
	})

	for _, stream := range []string{"-", "/dev/stdin"} {
		t.Run("rejects "+stream, func(t *testing.T) {
			cmd := integrationCmd(bin, cfgDir, logPath, "--config", stream, "install", "--dry-run", "figlet")
			var stderr strings.Builder
			cmd.Stderr = &stderr
			err := cmd.Run()
			ee, ok := err.(*exec.ExitError)
			if !ok || ee.ExitCode() != 2 {
				t.Fatalf("exit = %v, want 2\n%s", err, stderr.String())
			}
			if !strings.Contains(stderr.String(), "not supported") {
				t.Errorf("stderr does not reject %s:\n%s", stream, stderr.String())
			}
		})
	}
}

// TestIntegrationLogValidatesConfig pins FR-053's bypass list: log is not on
// it, so an invalid default layer is a hard error for log too.
func TestIntegrationLogValidatesConfig(t *testing.T) {
	bin := buildBinary(t)
	cfgDir, cfgPath := integrationConfig(t, "Never")
	writeIntegrationConfigFile(t, cfgPath, "[Ui]\nmode = \"not-a-mode\"\n")
	logPath := filepath.Join(filepath.Dir(cfgDir), "commands.log")
	cmd := integrationCmd(bin, cfgDir, logPath, "log")
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 2 {
		t.Fatalf("exit = %v, want 2\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "xbpretty: config "+cfgPath+":2:") {
		t.Errorf("log did not report the config error:\n%s", stderr.String())
	}
}

// TestIntegrationRejectedTOMLConstructsExitTwo pins AC-067 at the built-binary
// seam: every rejected construct prints the exact path-and-line envelope and
// exits 2.
func TestIntegrationRejectedTOMLConstructsExitTwo(t *testing.T) {
	bin := buildBinary(t)
	tests := []struct {
		name string
		body string
		line int
	}{
		{"floats", "[Nala]\nauto_remove = 1.5\n", 2},
		{"dates", "[Nala]\nauto_remove = 2026-01-02\n", 2},
		{"times", "[Nala]\nauto_remove = 07:32:00\n", 2},
		{"multiline strings", "[Ui]\nmode = \"\"\"Auto\"\"\"\n", 2},
		{"dotted keys", "[Ui]\nmode = \"Auto\"\nunit.value = \"Binary\"\n", 3},
		{"arrays of tables", "[[Nala]]\n", 1},
		{"quoted keys", "[Nala]\n\"assume_yes\" = true\n", 2},
		{"alternate theme key", "[Color]\ntheme = { Primary = { fg = \"Reset\" } }\n", 2},
		{"alternate theme subtable", "[Color.theme.Primary]\n", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgDir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(cfgDir, "xbpretty"), 0o755); err != nil {
				t.Fatal(err)
			}
			cfgPath := filepath.Join(cfgDir, "xbpretty", "config.toml")
			writeIntegrationConfigFile(t, cfgPath, tt.body)
			cmd := exec.Command(bin, "install", "--dry-run", "figlet")
			cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+cfgDir)
			var stdout, stderr strings.Builder
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			ee, ok := err.(*exec.ExitError)
			if !ok || ee.ExitCode() != 2 {
				t.Fatalf("exit = %v, want 2\n%s", err, stderr.String())
			}
			prefix := "xbpretty: config " + cfgPath + ":" + strconv.Itoa(tt.line) + ": "
			if !strings.Contains(stderr.String(), prefix) {
				t.Errorf("stderr missing %q:\n%s", prefix, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("rejected config wrote to stdout:\n%s", stdout.String())
			}
		})
	}
}
