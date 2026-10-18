package completion

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestScriptShells(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			s, err := Script(shell)
			if err != nil {
				t.Fatalf("Script(%s): %v", shell, err)
			}
			if s == "" {
				t.Fatal("empty script")
			}
			for _, cmd := range Subcommands {
				if !strings.Contains(s, cmd) {
					t.Errorf("script is missing subcommand %q", cmd)
				}
			}
			for _, flag := range Flags {
				if fishFlag(s, shell, flag) {
					continue
				}
				t.Errorf("script is missing flag %q", flag)
			}
			for _, bin := range []string{"xbps-query", "-l", "-Rs"} {
				if !strings.Contains(s, bin) {
					t.Errorf("script does not call %s", bin)
				}
			}
		})
	}
}

// fishFlag reports whether a script declares the given flag. Fish spells long
// flags as `-l name` (and shorts as `-s y`), never as `--name`.
func fishFlag(script, shell, flag string) bool {
	if shell != "fish" || !strings.HasPrefix(flag, "-") {
		return strings.Contains(script, flag)
	}
	name := strings.TrimLeft(flag, "-")
	switch {
	case strings.HasPrefix(flag, "--"):
		return strings.Contains(script, "-l "+name)
	default:
		return strings.Contains(script, "-s "+name)
	}
}

// TestAC025BashCompletionOffersInstalledNames runs the generated bash script
// and asks it what it would offer after `xbp remove <TAB>`: the completion
// has to read the installed list back, not merely name xbps-query.
func TestAC025BashCompletionOffersInstalledNames(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
	script, err := Script("bash")
	if err != nil {
		t.Fatalf("Script(bash): %v", err)
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  *-l*) printf '%s\\n' 'ii figlet-2.2.5_6 A program' 'ii htop-3.5.3_1 A viewer' ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(binDir, "xbps-query"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(dir, "xbpretty.bash")
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	probe := ". " + scriptPath + "\n" +
		"COMP_WORDS=(xbp remove '')\n" +
		"COMP_CWORD=2\n" +
		"_xbpretty\n" +
		"printf '%s\\n' \"${COMPREPLY[@]}\"\n"
	cmd := exec.Command("bash", "-c", probe)
	// Replace PATH rather than appending to it: a duplicate entry resolves to
	// whichever copy the shell reads first, which is not worth relying on.
	env := []string{"PATH=" + binDir + ":" + os.Getenv("PATH")}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PATH=") {
			env = append(env, kv)
		}
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running the completion: %v\n%s", err, out)
	}
	for _, name := range []string{"figlet", "htop"} {
		if !strings.Contains(string(out), name) {
			t.Errorf("the completion offered no %s:\n%s", name, out)
		}
	}
}

func TestScriptDialects(t *testing.T) {
	bash, _ := Script("bash")
	zsh, _ := Script("zsh")
	fish, _ := Script("fish")

	if !strings.Contains(bash, "complete -F") {
		t.Error("bash script lacks bash completion syntax")
	}
	if strings.Contains(bash, "complete -c ") {
		t.Error("bash script contains fish syntax")
	}
	if !strings.Contains(zsh, "#compdef") {
		t.Error("zsh script lacks a #compdef header")
	}
	if strings.Contains(zsh, "complete -F") {
		t.Error("zsh script contains bash syntax")
	}
	if !strings.Contains(fish, "complete -c") {
		t.Error("fish script lacks fish completion syntax")
	}
	if strings.Contains(fish, "complete -F") || strings.Contains(fish, "#compdef") {
		t.Error("fish script contains bash or zsh syntax")
	}
}

func TestParseShell(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
	}{
		{"bash", false},
		{"zsh", false},
		{"fish", false},
		{"Bash", false},
		{"ZSH", false},
		{"sh", true},
		{"powershell", true},
		{"", true},
	}
	for _, tt := range tests {
		if _, err := Script(tt.in); (err != nil) != tt.wantErr {
			t.Errorf("Script(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
	}
	if _, err := Script("sh"); err == nil || !strings.Contains(err.Error(), "bash") {
		t.Errorf("error %v should name the supported shells", err)
	}
}

// TestShippedScriptsAreCurrent keeps completions/, which the xbps-src template
// and tools/build-xbps.sh install, in step with the generator. A cross build
// cannot run the target binary, so the scripts are checked in; regenerate them
// with `make completions`.
func TestShippedScriptsAreCurrent(t *testing.T) {
	files := map[string]string{
		"bash": "xbpretty.bash",
		"zsh":  "_xbpretty",
		"fish": "xbpretty.fish",
	}
	for shell, name := range files {
		want, err := Script(shell)
		if err != nil {
			t.Fatalf("Script(%s): %v", shell, err)
		}
		got, err := os.ReadFile(filepath.Join("..", "..", "completions", name))
		if err != nil {
			t.Fatalf("read shipped %s script: %v (run `make completions`)", shell, err)
		}
		if string(got) != want {
			t.Errorf("completions/%s is stale (run `make completions`)", name)
		}
	}
}

// completionProbe runs a generated script for one shell against a stub
// xbps-query that logs each call, and reports what the completion offered and
// which xbps-query calls it made.
type completionProbe struct {
	offered []string
	calls   []string
}

func probeCompletion(t *testing.T, shell string, words ...string) completionProbe {
	t.Helper()
	script, err := Script(shell)
	if err != nil {
		t.Fatalf("Script(%s): %v", shell, err)
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	stub := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"$XBPS_QUERY_LOG\"\n" +
		"case \"$*\" in\n" +
		"  *-Rs*) printf '%s\\n' '[-] figlet-2.2.5_6 A program' '[-] fig2dev-3.2.9_1 A converter' ;;\n" +
		"  *-l*) printf '%s\\n' 'ii figlet-2.2.5_6 A program' 'ii htop-3.5.3_1 A viewer' ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(binDir, "xbps-query"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(dir, "script")
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = "'" + w + "'"
	}
	var cmd *exec.Cmd
	switch shell {
	case "bash":
		probe := ". " + scriptPath + "\n" +
			"COMP_WORDS=(" + strings.Join(quoted, " ") + ")\n" +
			"COMP_CWORD=" + strconv.Itoa(len(words)-1) + "\n" +
			"_xbpretty\n" +
			"printf '%s\\n' \"${COMPREPLY[@]}\"\n"
		cmd = exec.Command("bash", "-c", probe)
	case "zsh":
		// Stand in for the completion builtins: _describe prints the
		// candidates it is given, the others print nothing.
		probe := "_describe() { local arr=${@[-1]}; print -l -- \"${(@P)arr}\" }\n" +
			"_arguments() { : }\n_values() { : }\n" +
			"words=(" + strings.Join(quoted, " ") + ")\n" +
			"CURRENT=" + strconv.Itoa(len(words)) + "\n" +
			". " + scriptPath + "\n"
		cmd = exec.Command("zsh", "-f", "-c", probe)
	case "fish":
		probe := "source " + scriptPath + "\n" +
			"complete -C '" + strings.Join(words, " ") + "'\n"
		cmd = exec.Command("fish", "--no-config", "-c", probe)
	}
	env := []string{"PATH=" + binDir + ":" + os.Getenv("PATH"), "XBPS_QUERY_LOG=" + logPath}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PATH=") {
			env = append(env, kv)
		}
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running the %s completion: %v\n%s", shell, err, out)
	}
	var p completionProbe
	for _, line := range strings.Split(string(out), "\n") {
		// fish prints "name<TAB>description"; keep the name only.
		name, _, _ := strings.Cut(line, "\t")
		if shell == "zsh" {
			// zsh _describe candidates read "name:description".
			name, _, _ = strings.Cut(name, ":")
		}
		if name != "" {
			p.offered = append(p.offered, name)
		}
	}
	if data, err := os.ReadFile(logPath); err == nil {
		p.calls = strings.Split(strings.TrimSpace(string(data)), "\n")
	}
	return p
}

func offers(p completionProbe, name string) bool {
	for _, o := range p.offered {
		if o == name {
			return true
		}
	}
	return false
}

// TestCompletionQueryRules pins the rules of ADR-0009 in every shell: install
// searches the repository only for a non-empty word that is not a flag, a flag
// word queries nothing, autoremove offers no package names, remove still
// offers the installed ones. Shells that are not on PATH are skipped.
func TestCompletionQueryRules(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			if _, err := exec.LookPath(shell); err != nil {
				t.Skipf("%s is not installed", shell)
			}

			t.Run("install with a word searches that word", func(t *testing.T) {
				p := probeCompletion(t, shell, "xbp", "install", "fig")
				if len(p.calls) != 1 || p.calls[0] != "-Rs fig" {
					t.Errorf("xbps-query calls = %q, want exactly [\"-Rs fig\"]", p.calls)
				}
				if !offers(p, "figlet") {
					t.Errorf("figlet not offered: %q", p.offered)
				}
			})
			t.Run("install on an empty word offers nothing and queries nothing", func(t *testing.T) {
				p := probeCompletion(t, shell, "xbp", "install", "")
				if len(p.calls) != 0 {
					t.Errorf("xbps-query was called: %q", p.calls)
				}
				if offers(p, "figlet") {
					t.Errorf("package names offered on an empty word: %q", p.offered)
				}
			})
			t.Run("install on a flag queries nothing", func(t *testing.T) {
				p := probeCompletion(t, shell, "xbp", "install", "-")
				if len(p.calls) != 0 {
					t.Errorf("xbps-query was called: %q", p.calls)
				}
			})
			t.Run("autoremove offers no package names", func(t *testing.T) {
				for _, word := range []string{"", "h", "-"} {
					p := probeCompletion(t, shell, "xbp", "autoremove", word)
					if len(p.calls) != 0 {
						t.Errorf("word %q: xbps-query was called: %q", word, p.calls)
					}
					if offers(p, "htop") || offers(p, "figlet") {
						t.Errorf("word %q: package names offered: %q", word, p.offered)
					}
				}
			})
			t.Run("the command list offers log", func(t *testing.T) {
				p := probeCompletion(t, shell, "xbp", "")
				if !offers(p, "log") {
					t.Errorf("log not offered after xbp: %q", p.offered)
				}
			})
			t.Run("log offers installed names", func(t *testing.T) {
				p := probeCompletion(t, shell, "xbp", "log", "")
				if len(p.calls) != 1 || p.calls[0] != "-l" {
					t.Errorf("xbps-query calls = %q, want exactly [\"-l\"]", p.calls)
				}
				for _, name := range []string{"figlet", "htop"} {
					if !offers(p, name) {
						t.Errorf("%s not offered: %q", name, p.offered)
					}
				}
			})
			t.Run("log on a flag queries nothing and offers --limit", func(t *testing.T) {
				p := probeCompletion(t, shell, "xbp", "log", "-")
				if len(p.calls) != 0 {
					t.Errorf("xbps-query was called: %q", p.calls)
				}
				if !offers(p, "--limit") {
					t.Errorf("--limit not offered: %q", p.offered)
				}
				if offers(p, "figlet") {
					t.Errorf("package names offered on a flag word: %q", p.offered)
				}
			})
			t.Run("after --limit offers nothing and queries nothing", func(t *testing.T) {
				for _, word := range []string{"", "5"} {
					p := probeCompletion(t, shell, "xbp", "log", "--limit", word)
					if len(p.calls) != 0 {
						t.Errorf("word %q: xbps-query was called: %q", word, p.calls)
					}
					if len(p.offered) != 0 {
						t.Errorf("word %q: completion offered %q after --limit", word, p.offered)
					}
				}
			})
			t.Run("remove offers installed names", func(t *testing.T) {
				p := probeCompletion(t, shell, "xbp", "remove", "")
				for _, name := range []string{"figlet", "htop"} {
					if !offers(p, name) {
						t.Errorf("%s not offered: %q", name, p.offered)
					}
				}
			})
		})
	}
}
