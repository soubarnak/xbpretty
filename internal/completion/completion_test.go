package completion

import (
	"os"
	"os/exec"
	"path/filepath"
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

func TestStripVersion(t *testing.T) {
	tests := []struct{ in, want string }{
		{"htop-3.5.3_1", "htop"},
		{"ca-certificates-20250419+3.127_1", "ca-certificates"},
		{"base-files-0.146_1", "base-files"},
		{"noversion", ""},
		{"-1.0_1", ""},
	}
	for _, tt := range tests {
		if got := stripVersion(tt.in); got != tt.want {
			t.Errorf("stripVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPackageField(t *testing.T) {
	tests := []struct{ in, want string }{
		{"ii htop-3.5.3_1 Interactive process viewer", "htop-3.5.3_1"},
		{"[-] htop-3.5.3_1 Interactive process viewer", "htop-3.5.3_1"},
		{"[*] vim-9.2.1031_1 Vim editor", "vim-9.2.1031_1"},
		{"htop-3.5.3_1", "htop-3.5.3_1"},
	}
	for _, tt := range tests {
		if got := packageField(tt.in); got != tt.want {
			t.Errorf("packageField(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
