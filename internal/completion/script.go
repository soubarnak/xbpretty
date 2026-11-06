package completion

import (
	"fmt"
	"strings"

	"github.com/soubarnak/xbpretty/internal/spec"
)

// Script returns the completion script for a shell.
func Script(shell string) (string, error) {
	switch strings.ToLower(shell) {
	case "bash":
		return bashScript(), nil
	case "zsh":
		return zshScript(), nil
	case "fish":
		return fishScript(), nil
	}
	return "", fmt.Errorf("unsupported shell %q (want bash, zsh or fish)", shell)
}

func visibleNames() []string {
	var names []string
	for _, c := range spec.VisibleCommands() {
		names = append(names, c.Name)
	}
	return names
}

func aliasNames() []string { return spec.LinkNames() }

// completionNames are the names a shell completion must be registered for:
// the primary binary plus the six link names.
func completionNames() []string {
	return append([]string{"xbpretty"}, aliasNames()...)
}

// aliasPairs lists every command alias and its canonical command, documented
// first. Detection uses the whole list; the command-word list uses only the
// documented aliases.
func aliasPairs() [][2]string {
	var pairs [][2]string
	for _, c := range spec.Commands {
		for _, alias := range c.Aliases {
			pairs = append(pairs, [2]string{alias, c.Name})
		}
	}
	for _, c := range spec.Commands {
		for _, alias := range c.UndocumentedAliases {
			pairs = append(pairs, [2]string{alias, c.Name})
		}
	}
	return pairs
}

// commandWords are the commands and documented aliases offered at the root.
func commandWords() []string {
	words := visibleNames()
	for _, c := range spec.VisibleCommands() {
		words = append(words, c.Aliases...)
	}
	return words
}

// wordsFor returns the advertised spellings for one command, optionally
// dropping a spelling that collides with a command-specific meaning. The
// fetch command does not accept the short -n for --dry-run, and list/search
// reserve -V for --virtual.
func wordsFor(cmd string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, f := range spec.FlagsFor(cmd) {
		if f.Hidden || f.Refusal != "" || f.NoOp {
			continue
		}
		spellings := spec.FlagSpellings(f)
		if cmd == "fetch" && f.Long == "dry-run" {
			spellings = []string{"--dry-run"}
		}
		// FR-005: after list/search -V is --virtual, so the global
		// --version keeps its long spelling only. The parser still accepts
		// the typed -V as --virtual in that position.
		if (cmd == "list" || cmd == "search") && f.Long == "version" {
			spellings = []string{"--version"}
		}
		if cmd == "search" && f.Long == "names" {
			spellings = []string{"-n", "--names", "--names-only"}
		}
		for _, s := range spellings {
			add(s)
		}
	}
	return out
}

// rootWords are the flags accepted before a command: global flags only.
func rootWords() []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, f := range spec.Flags {
		if !f.Global || f.Hidden || f.Refusal != "" {
			continue
		}
		for _, s := range spec.FlagSpellings(f) {
			add(s)
		}
	}
	return out
}

// commandsWithCompletion lists the visible commands whose positional argument
// completes from the given source. The table in internal/spec is the only
// source, so a new command joins the right group automatically.
func commandsWithCompletion(kind spec.CompletionKind) []string {
	var out []string
	for _, c := range spec.VisibleCommands() {
		if c.Completion == kind {
			out = append(out, c.Name)
		}
	}
	return out
}

// flagValues returns an enum flag's allowed values from the shared table.
func flagValues(long string) []string {
	for _, f := range spec.Flags {
		if f.Long == long {
			return f.Values
		}
	}
	return nil
}

func quotedWords(words []string) string {
	return strings.Join(words, " ")
}

func packageLists() string {
	return `_xbpretty_installed() {
  LC_ALL=C xbps-query -l 2>/dev/null | awk '{print $2}' | sed 's/-[^-]*$//'
}
_xbpretty_available() {
  # A search of the whole repository is slow and never wanted: only search for a
  # word the user has started, and never for a flag.
  [[ -z "$1" || "$1" == -* ]] && return 0
  LC_ALL=C xbps-query -Rs "$1" 2>/dev/null | awk '{print $2}' | sed 's/-[^-]*$//'
}
_xbpretty_history_ids() {
  local dir="${XBPRETTY_HISTORY_DIR:-/var/lib/xbpretty/history}"
  local f
  if [ -d "$dir" ]; then
    for f in "$dir"/*.json; do
      [ -e "$f" ] || continue
      f="${f##*/}"
      printf '%s\n' "${f%.json}"
    done
  fi
  printf '%s\n' last
}
`
}

// bashScanCase maps one word to a canonical command, or returns "".
func bashScanCase() string {
	var b strings.Builder
	b.WriteString("        " + strings.Join(visibleNames(), "|") + ")\n")
	b.WriteString("          sub=\"${COMP_WORDS[i]}\"; break ;;\n")
	for _, pair := range aliasPairs() {
		fmt.Fprintf(&b, "        %s)\n          sub=%s; break ;;\n", pair[0], pair[1])
	}
	b.WriteString("        --)\n          break ;;\n")
	return b.String()
}

func bashScript() string {
	var b strings.Builder
	b.WriteString(`# bash completion for xbpretty
_xbpretty() {
  local cur prev words cword
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"

`)
	b.WriteString(packageLists())
	b.WriteString(`
  local commands="` + quotedWords(commandWords()) + `"
  local global_flags="` + quotedWords(rootWords()) + `"
`)
	for _, c := range spec.VisibleCommands() {
		fmt.Fprintf(&b, "  local flags_%s=\"%s\"\n", strings.ReplaceAll(c.Name, "-", "_"), quotedWords(wordsFor(c.Name)))
	}
	b.WriteString(`
  local prog="${COMP_WORDS[0]##*/}"
  local sub="" i
  case "$prog" in
    xbp-install) sub=install ;;
    xbp-remove) sub=remove ;;
    xbp-upgrade) sub=upgrade ;;
    xbp-autoremove) sub=autoremove ;;
    xbp-update) sub=update ;;
  esac
  if [[ -z "$sub" ]]; then
    for (( i=1; i < COMP_CWORD; i++ )); do
      case "${COMP_WORDS[i]}" in
` + bashScanCase() + `      esac
    done
  fi

  case "$prev" in
    --color)
      COMPREPLY=( $(compgen -W "` + strings.Join(flagValues("color"), " ") + `" -- "$cur") ); return 0 ;;
    --config|--history-dir|--country|-c|--auto|--limit)
      return 0 ;;
  esac

  if [[ -z "$sub" ]]; then
    if [[ "$cur" == -* ]]; then
      COMPREPLY=( $(compgen -W "$global_flags" -- "$cur") )
    else
      COMPREPLY=( $(compgen -W "$commands" -- "$cur") )
    fi
    return 0
  fi

  if [[ "$cur" == -* ]]; then
    local flagvar="flags_${sub//-/_}"
    COMPREPLY=( $(compgen -W "${!flagvar}" -- "$cur") )
    return 0
  fi

  case "$sub" in
    ` + strings.Join(commandsWithCompletion(spec.CompletionAvailablePackages), "|") + `)
      COMPREPLY=( $(compgen -W "$(_xbpretty_available "$cur")" -- "$cur") ) ;;
    ` + strings.Join(commandsWithCompletion(spec.CompletionInstalledPackages), "|") + `)
      COMPREPLY=( $(compgen -W "$(_xbpretty_installed)" -- "$cur") ) ;;
    ` + strings.Join(commandsWithCompletion(spec.CompletionHistoryIDs), "|") + `)
      COMPREPLY=( $(compgen -W "$(_xbpretty_history_ids)" -- "$cur") ) ;;
    ` + strings.Join(commandsWithCompletion(spec.CompletionShells), "|") + `)
      COMPREPLY=( $(compgen -W "bash zsh fish" -- "$cur") ) ;;
  esac
  return 0
}
complete -F _xbpretty ` + quotedWords(completionNames()) + `
`)
	return b.String()
}

func zshScanCase() string {
	var b strings.Builder
	b.WriteString("        " + strings.Join(visibleNames(), "|") + ")\n")
	b.WriteString("          sub=\"${words[i]}\"; break ;;\n")
	for _, pair := range aliasPairs() {
		fmt.Fprintf(&b, "        %s)\n          sub=%s; break ;;\n", pair[0], pair[1])
	}
	b.WriteString("        --)\n          break ;;\n")
	return b.String()
}

func zshScript() string {
	var b strings.Builder
	b.WriteString(`#compdef ` + quotedWords(completionNames()) + `
# zsh completion for xbpretty

_xbpretty_installed() {
  local -a pkgs
  pkgs=(${(f)"$(LC_ALL=C xbps-query -l 2>/dev/null | awk '{print $2}' | sed 's/-[^-]*$//')"})
  _describe 'installed package' pkgs
}

_xbpretty_available() {
  [[ -z "$1" || "$1" == -* ]] && return 0
  local -a pkgs
  pkgs=(${(f)"$(LC_ALL=C xbps-query -Rs "$1" 2>/dev/null | awk '{print $2}' | sed 's/-[^-]*$//')"})
  _describe 'available package' pkgs
}

_xbpretty_history_ids() {
  local dir="${XBPRETTY_HISTORY_DIR:-/var/lib/xbpretty/history}"
  local -a ids
  if [[ -d "$dir" ]]; then
    ids=(${(f)"$(cd "$dir" 2>/dev/null && printf '%s\n' *.json 2>/dev/null | sed 's/\.json$//')"})
  fi
  ids+=(last)
  _describe 'history entry' ids
}

_xbpretty() {
  local -a cmds
  cmds=(
`)
	for _, c := range spec.VisibleCommands() {
		fmt.Fprintf(&b, "    '%s:%s'\n", c.Name, c.Summary)
	}
	b.WriteString(`  )

  local -a globalflags
  globalflags=(` + quotedWords(rootWords()) + `)
`)
	for _, c := range spec.VisibleCommands() {
		varName := "f_" + strings.ReplaceAll(c.Name, "-", "_")
		fmt.Fprintf(&b, "  local -a %s\n  %s=(%s)\n", varName, varName, quotedWords(wordsFor(c.Name)))
	}
	b.WriteString(`
  local prog="${words[1]##*/}"
  local sub="" i
  case "$prog" in
    xbp-install) sub=install ;;
    xbp-remove) sub=remove ;;
    xbp-upgrade) sub=upgrade ;;
    xbp-autoremove) sub=autoremove ;;
    xbp-update) sub=update ;;
  esac
  if [[ -z "$sub" ]]; then
    for (( i=2; i < CURRENT; i++ )); do
      case "${words[i]}" in
` + zshScanCase() + `      esac
    done
  fi

  if [[ -z "$sub" ]]; then
    _describe -t commands 'xbpretty command' cmds
    _describe 'option' globalflags
    return
  fi

  local key="$sub"
  if [[ "$key" == "full-upgrade" ]]; then key="full_upgrade"; fi
  if [[ "$key" == "safe-upgrade" ]]; then key="safe_upgrade"; fi

  # --limit takes a count: offer nothing and query nothing for it.
  [[ "${words[CURRENT-1]}" == --limit ]] && return
  case "${words[CURRENT-1]}" in
    --color)
      _values 'colour mode' ` + strings.Join(flagValues("color"), " ") + `; return ;;
    --config|--history-dir|--country|-c|--auto)
      return ;;
  esac

  if [[ "${words[CURRENT]}" == -* ]]; then
    local -a subflags
    eval "subflags=(\${f_${key}})"
    _describe 'option' subflags
    return
  fi

  case "$sub" in
    ` + strings.Join(commandsWithCompletion(spec.CompletionAvailablePackages), "|") + `) _xbpretty_available "${words[CURRENT]}" ;;
    ` + strings.Join(commandsWithCompletion(spec.CompletionInstalledPackages), "|") + `) _xbpretty_installed ;;
    ` + strings.Join(commandsWithCompletion(spec.CompletionHistoryIDs), "|") + `) _xbpretty_history_ids ;;
    ` + strings.Join(commandsWithCompletion(spec.CompletionShells), "|") + `) _values 'shell' bash zsh fish ;;
  esac
}
_xbpretty "$@"
`)
	return b.String()
}

// fishVerbFunction derives the effective verb from either a fixed-verb link
// name or the first command/alias token on the line.
func fishVerbFunction() string {
	var b strings.Builder
	b.WriteString(`function __xbpretty_verb
    set -l cmd (commandline -opc)
    switch $cmd[1]
`)
	for _, pair := range [][2]string{
		{"xbp-install", "install"}, {"xbp-remove", "remove"}, {"xbp-upgrade", "upgrade"},
		{"xbp-autoremove", "autoremove"}, {"xbp-update", "update"},
	} {
		fmt.Fprintf(&b, "        case %s\n            echo %s\n            return\n", pair[0], pair[1])
	}
	b.WriteString(`    end
    for w in $cmd[2..-1]
        switch $w
`)
	for _, name := range visibleNames() {
		fmt.Fprintf(&b, "            case %s\n                echo %s\n                return\n", name, name)
	}
	for _, pair := range aliasPairs() {
		fmt.Fprintf(&b, "            case %s\n                echo %s\n                return\n", pair[0], pair[1])
	}
	b.WriteString(`        end
    end
end

function __xbpretty_no_subcommand
    test -z (__xbpretty_verb)
end
`)
	return b.String()
}

func fishCondition(commands []string) string {
	if len(commands) == 1 {
		return "test (__xbpretty_verb) = " + commands[0]
	}
	return "contains (__xbpretty_verb) " + strings.Join(commands, " ")
}

func fishScript() string {
	var b strings.Builder
	b.WriteString(`# fish completion for xbpretty
`)
	b.WriteString(fishVerbFunction())
	b.WriteString(`
function __xbpretty_installed
    LC_ALL=C xbps-query -l 2>/dev/null | awk '{print $2}' | string replace -r -- '-[^-]*$' ''
end

function __xbpretty_available
    set -l word (commandline -ct)
    test -n "$word"; or return 0
    string match -q -- '-*' $word; and return 0
    LC_ALL=C xbps-query -Rs $word 2>/dev/null | awk '{print $2}' | string replace -r -- '-[^-]*$' ''
end

function __xbpretty_history_ids
    set -l dir $XBPRETTY_HISTORY_DIR
    test -n "$dir"; or set dir /var/lib/xbpretty/history
    if test -d $dir
        for f in $dir/*.json
            test -e $f; or continue
            string replace -r -- '\.json$' '' (string replace -r -- '.*/' '' $f)
        end
    end
    printf '%s\n' last
end

for __xbpretty_cmd in ` + quotedWords(completionNames()) + `
    complete -c $__xbpretty_cmd -f
`)
	for _, c := range spec.VisibleCommands() {
		fmt.Fprintf(&b, "    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a %s -d '%s'\n", c.Name, c.Summary)
	}
	b.WriteString(`
    complete -c $__xbpretty_cmd -l version -d 'print version'
    complete -c $__xbpretty_cmd -s V -n 'not contains (__xbpretty_verb) list search' -d 'print version'
`)
	// Global flags are completed after a subcommand too.
	for _, f := range spec.Flags {
		if !f.Global || f.Hidden || f.Refusal != "" || f.NoOp || f.Long == "version" {
			continue
		}
		writeFishFlag(&b, "$__xbpretty_cmd", f, "")
	}
	// Per-command flags, grouped by the commands that accept each one.
	for _, f := range spec.Flags {
		if f.Global || f.Hidden || f.Refusal != "" || f.NoOp {
			continue
		}
		writeFishFlag(&b, "$__xbpretty_cmd", f, fishCondition(f.Commands))
	}
	b.WriteString(`
    complete -c $__xbpretty_cmd -n '` + fishCondition(commandsWithCompletion(spec.CompletionAvailablePackages)) + `' -a '(__xbpretty_available)'
    complete -c $__xbpretty_cmd -n '` + fishCondition(commandsWithCompletion(spec.CompletionInstalledPackages)) + `' -a '(__xbpretty_installed)'
    complete -c $__xbpretty_cmd -n '` + fishCondition(commandsWithCompletion(spec.CompletionHistoryIDs)) + `' -a '(__xbpretty_history_ids)'
    complete -c $__xbpretty_cmd -n '` + fishCondition(commandsWithCompletion(spec.CompletionShells)) + `' -a 'bash zsh fish'
end
`)
	return b.String()
}

func writeFishFlag(b *strings.Builder, cmd string, f spec.Flag, cond string) {
	condition := ""
	if cond != "" {
		condition = " -n '" + cond + "'"
	}
	value := ""
	if f.Kind != spec.BoolFlag || len(f.Values) > 0 {
		value = " -x"
		if len(f.Values) > 0 {
			value += " -a '" + strings.Join(f.Values, " ") + "'"
		}
	}
	desc := ""
	if f.Summary != "" {
		desc = " -d '" + strings.ReplaceAll(f.Summary, "'", "") + "'"
	}
	fmt.Fprintf(b, "    complete -c %s%s", cmd, condition)
	if f.Short != 0 {
		fmt.Fprintf(b, " -s %c", f.Short)
	}
	fmt.Fprintf(b, " -l %s%s%s\n", f.Long, value, desc)
	for _, alias := range f.Aliases {
		fmt.Fprintf(b, "    complete -c %s%s -l %s%s\n", cmd, condition, alias, value)
	}
}
