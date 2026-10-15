package completion

import (
	"fmt"
	"strings"
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

func packageLists() string {
	return fmt.Sprintf(`_xbpretty_installed() {
  LC_ALL=C xbps-query -l 2>/dev/null | awk '{print $2}' | sed 's/-[^-]*$//'
}
_xbpretty_available() {
  LC_ALL=C xbps-query -Rs "$1" 2>/dev/null | awk '{print $2}' | sed 's/-[^-]*$//'
}
`)
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
  if [[ "$prev" == "--color" ]]; then
    COMPREPLY=( $(compgen -W "auto always never" -- "$cur") )
    return 0
  fi

  local cmds="install remove upgrade autoremove update completion link"
  local flags="-n --dry-run -y --yes -v --verbose --no-sync --no-log --color --version"

  # which subcommand are we in?
  local sub="" i
  for (( i=1; i < COMP_CWORD; i++ )); do
    case "${COMP_WORDS[i]}" in
      install|remove|upgrade|autoremove|update|completion|link)
        sub="${COMP_WORDS[i]}"; break ;;
      --)
        break ;;
    esac
  done

  if [[ -z "$sub" ]]; then
    COMPREPLY=( $(compgen -W "$cmds $flags" -- "$cur") )
    return 0
  fi

  if [[ "$cur" == -* ]]; then
    COMPREPLY=( $(compgen -W "$flags" -- "$cur") )
    return 0
  fi

  case "$sub" in
    install)
      COMPREPLY=( $(compgen -W "$(_xbpretty_available "$cur")" -- "$cur") ) ;;
    remove|autoremove)
      COMPREPLY=( $(compgen -W "$(_xbpretty_installed)" -- "$cur") ) ;;
    completion)
      COMPREPLY=( $(compgen -W "bash zsh fish" -- "$cur") ) ;;
  esac
  return 0
}
complete -F _xbpretty xbpretty xbp xbp-install xbp-remove xbp-upgrade xbp-autoremove xbp-update
`)
	return b.String()
}

func zshScript() string {
	var b strings.Builder
	b.WriteString(`#compdef xbpretty xbp xbp-install xbp-remove xbp-upgrade xbp-autoremove xbp-update
# zsh completion for xbpretty

_xbpretty_installed() {
  local -a pkgs
  pkgs=(${(f)"$(LC_ALL=C xbps-query -l 2>/dev/null | awk '{print $2}' | sed 's/-[^-]*$//')"})
  _describe 'installed package' pkgs
}

_xbpretty_available() {
  local -a pkgs
  pkgs=(${(f)"$(LC_ALL=C xbps-query -Rs "$1" 2>/dev/null | awk '{print $2}' | sed 's/-[^-]*$//')"})
  _describe 'available package' pkgs
}

_xbpretty() {
  local -a cmds
  cmds=(
    'install:install packages from repositories'
    'remove:remove installed packages'
    'upgrade:upgrade all packages'
    'autoremove:remove orphaned packages'
    'update:refresh the repository index'
    'completion:print a shell completion script'
    'link:create xbp symlinks'
  )

  if (( CURRENT == 2 )); then
    _describe -t commands 'xbpretty command' cmds
    _arguments '*::flag:(-n --dry-run -y --yes -v --verbose --no-sync --no-log --color --version)'
    return
  fi

  case "${words[2]}" in
    install) _xbpretty_available "${words[CURRENT]}" ;;
    remove|autoremove) _xbpretty_installed ;;
    completion) _values 'shell' bash zsh fish ;;
    *)
      _arguments \
        '(-n --dry-run)'{-n,--dry-run}'[dry run]' \
        '(-y --yes)'{-y,--yes}'[assume yes]' \
        '(-v --verbose)'{-v,--verbose}'[verbose]' \
        '--no-sync[skip index sync]' \
        '--no-log[skip transaction log]' \
        '--color=[colour output]:mode:(auto always never)' \
        '--version[print version]'
      ;;
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
            case install remove upgrade autoremove update completion link
                return 1
        end
    end
    return 0
end

function __xbpretty_installed
    LC_ALL=C xbps-query -l 2>/dev/null | awk '{print $2}' | string replace -r '-[^-]*$' ''
end

function __xbpretty_available
    LC_ALL=C xbps-query -Rs (commandline -ct) 2>/dev/null | awk '{print $2}' | string replace -r '-[^-]*$' ''
end

for __xbpretty_cmd in xbpretty xbp xbp-install xbp-remove xbp-upgrade xbp-autoremove xbp-update
    complete -c $__xbpretty_cmd -f
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a install -d 'install packages'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a remove -d 'remove packages'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a upgrade -d 'upgrade packages'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a autoremove -d 'remove orphans'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a update -d 'refresh index'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a completion -d 'print completion script'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a link -d 'create symlinks'

    complete -c $__xbpretty_cmd -s n -l dry-run -d 'show the plan without changing anything'
    complete -c $__xbpretty_cmd -s y -l yes -d 'assume yes at the prompt'
    complete -c $__xbpretty_cmd -s v -l verbose -d 'print each xbps command'
    complete -c $__xbpretty_cmd -l no-sync -d 'skip the repository index sync'
    complete -c $__xbpretty_cmd -l no-log -d 'skip the transaction log'
    complete -c $__xbpretty_cmd -l color -x -a 'auto always never' -d 'colour output'
    complete -c $__xbpretty_cmd -l version -d 'print version'

    complete -c $__xbpretty_cmd -n '__fish_seen_subcommand_from install' -a '(__xbpretty_available)'
    complete -c $__xbpretty_cmd -n '__fish_seen_subcommand_from remove' -a '(__xbpretty_installed)'
    complete -c $__xbpretty_cmd -n '__fish_seen_subcommand_from autoremove' -a '(__xbpretty_installed)'
    complete -c $__xbpretty_cmd -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'
end
`)
	return b.String()
}
