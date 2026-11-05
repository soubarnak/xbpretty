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
		if !f.Global || f.Hidden || f.Refusal != "" || f.NoOp {
			continue
		}
		for _, s := range spec.FlagSpellings(f) {
			add(s)
		}
	}
	return out
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
  local commands="` + quotedWords(visibleNames()) + `"
  local global_flags="` + quotedWords(rootWords()) + `"
  local flags_install="` + quotedWords(wordsFor("install")) + `"
  local flags_remove="` + quotedWords(wordsFor("remove")) + `"
  local flags_purge="` + quotedWords(wordsFor("purge")) + `"
  local flags_autoremove="` + quotedWords(wordsFor("autoremove")) + `"
  local flags_autopurge="` + quotedWords(wordsFor("autopurge")) + `"
  local flags_upgrade="` + quotedWords(wordsFor("upgrade")) + `"
  local flags_full_upgrade="` + quotedWords(wordsFor("full-upgrade")) + `"
  local flags_safe_upgrade="` + quotedWords(wordsFor("safe-upgrade")) + `"
  local flags_update="` + quotedWords(wordsFor("update")) + `"
  local flags_download="` + quotedWords(wordsFor("download")) + `"
  local flags_clean="` + quotedWords(wordsFor("clean")) + `"
  local flags_list="` + quotedWords(wordsFor("list")) + `"
  local flags_search="` + quotedWords(wordsFor("search")) + `"
  local flags_show="` + quotedWords(wordsFor("show")) + `"
  local flags_policy="` + quotedWords(wordsFor("policy")) + `"
  local flags_history="` + quotedWords(wordsFor("history")) + `"
  local flags_undo="` + quotedWords(wordsFor("undo")) + `"
  local flags_redo="` + quotedWords(wordsFor("redo")) + `"
  local flags_fetch="` + quotedWords(wordsFor("fetch")) + `"
  local flags_completion="` + quotedWords(wordsFor("completion")) + `"
  local flags_link="` + quotedWords(wordsFor("link")) + `"
  local flags_log="` + quotedWords(wordsFor("log")) + `"

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
        install|remove|purge|autoremove|autopurge|upgrade|full-upgrade|safe-upgrade|update|download|clean|list|search|show|policy|history|undo|redo|fetch|completion|link|log)
          sub="${COMP_WORDS[i]}"; break ;;
        --)
          break ;;
      esac
    done
  fi

  case "$prev" in
    --color)
      COMPREPLY=( $(compgen -W "auto always never" -- "$cur") ); return 0 ;;
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
    install|download|show|list|search)
      COMPREPLY=( $(compgen -W "$(_xbpretty_available "$cur")" -- "$cur") ) ;;
    remove|purge|policy|log)
      COMPREPLY=( $(compgen -W "$(_xbpretty_installed)" -- "$cur") ) ;;
    history|undo|redo)
      COMPREPLY=( $(compgen -W "$(_xbpretty_history_ids)" -- "$cur") ) ;;
    completion)
      COMPREPLY=( $(compgen -W "bash zsh fish" -- "$cur") ) ;;
  esac
  return 0
}
complete -F _xbpretty ` + quotedWords(aliasNames()) + `
`)
	return b.String()
}

func zshScript() string {
	var b strings.Builder
	b.WriteString(`#compdef ` + quotedWords(aliasNames()) + `
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
  local sub=""
  case "${words[1]}" in
    install|remove|purge|autoremove|autopurge|upgrade|full-upgrade|safe-upgrade|update|download|clean|list|search|show|policy|history|undo|redo|fetch|completion|link|log)
      sub="${words[1]}" ;;
  esac
  if [[ -z "$sub" ]]; then
    case "${words[0]##*/}" in
      xbp-install) sub=install ;;
      xbp-remove) sub=remove ;;
      xbp-upgrade) sub=upgrade ;;
      xbp-autoremove) sub=autoremove ;;
      xbp-update) sub=update ;;
    esac
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
      _values 'colour mode' auto always never; return ;;
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
    install|download|show|list|search) _xbpretty_available "${words[CURRENT]}" ;;
    remove|purge|policy|log) _xbpretty_installed ;;
    history|undo|redo) _xbpretty_history_ids ;;
    completion) _values 'shell' bash zsh fish ;;
  esac
}
_xbpretty "$@"
`)
	return b.String()
}

func fishScript() string {
	var b strings.Builder
	b.WriteString(`# fish completion for xbpretty
function __xbpretty_no_subcommand
    set -l cmd (commandline -opc)
    for w in $cmd[2..-1]
        switch $w
`)
	for _, name := range visibleNames() {
		fmt.Fprintf(&b, "            case %s\n                return 1\n", name)
	}
	b.WriteString(`        end
    end
    return 0
end

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

function __xbpretty_log_names
    string match -q -- '-*' (commandline -ct); and return 0
    __xbpretty_installed
end

for __xbpretty_cmd in ` + quotedWords(aliasNames()) + `
    complete -c $__xbpretty_cmd -f
`)
	for _, c := range spec.VisibleCommands() {
		fmt.Fprintf(&b, "    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a %s -d '%s'\n", c.Name, c.Summary)
	}
	b.WriteString(`
    complete -c $__xbpretty_cmd -l version -d 'print version'
    complete -c $__xbpretty_cmd -s V -n 'not __fish_seen_subcommand_from list search' -d 'print version'
    complete -c $__xbpretty_cmd -s V -n '__fish_seen_subcommand_from list search' -l virtual -d 'list virtual packages'
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
		cond := "__fish_seen_subcommand_from " + strings.Join(f.Commands, " ")
		writeFishFlag(&b, "$__xbpretty_cmd", f, cond)
	}
	b.WriteString(`
    complete -c $__xbpretty_cmd -n '__fish_seen_subcommand_from install download show list search' -a '(__xbpretty_available)'
    complete -c $__xbpretty_cmd -n '__fish_seen_subcommand_from remove purge policy log' -a '(__xbpretty_installed)'
    complete -c $__xbpretty_cmd -n '__fish_seen_subcommand_from history undo redo' -a '(__xbpretty_history_ids)'
    complete -c $__xbpretty_cmd -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'
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
