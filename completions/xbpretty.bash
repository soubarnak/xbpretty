# bash completion for xbpretty
_xbpretty() {
  local cur prev words cword
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"

_xbpretty_installed() {
  LC_ALL=C xbps-query -l 2>/dev/null | awk '{print $2}' | sed 's/-[^-]*$//'
}
_xbpretty_available() {
  # A search of the whole repository is slow and never wanted: only search for a
  # word the user has started, and never for a flag.
  [[ -z "$1" || "$1" == -* ]] && return 0
  LC_ALL=C xbps-query -Rs "$1" 2>/dev/null | awk '{print $2}' | sed 's/-[^-]*$//'
}

  if [[ "$prev" == "--color" ]]; then
    COMPREPLY=( $(compgen -W "auto always never" -- "$cur") )
    return 0
  fi

  local cmds="install remove upgrade autoremove update completion link log"
  local flags="-n --dry-run -y --yes -v --verbose --no-sync --no-log --color --version"

  # which subcommand are we in?
  local sub="" i
  for (( i=1; i < COMP_CWORD; i++ )); do
    case "${COMP_WORDS[i]}" in
      install|remove|upgrade|autoremove|update|completion|link|log)
        sub="${COMP_WORDS[i]}"; break ;;
      --)
        break ;;
    esac
  done

  if [[ -z "$sub" ]]; then
    COMPREPLY=( $(compgen -W "$cmds $flags" -- "$cur") )
    return 0
  fi

  if [[ "$prev" == "--limit" ]]; then
    return 0
  fi

  if [[ "$cur" == -* ]]; then
    [[ "$sub" == "log" ]] && flags="$flags --limit"
    COMPREPLY=( $(compgen -W "$flags" -- "$cur") )
    return 0
  fi

  case "$sub" in
    install)
      COMPREPLY=( $(compgen -W "$(_xbpretty_available "$cur")" -- "$cur") ) ;;
    remove|log)
      COMPREPLY=( $(compgen -W "$(_xbpretty_installed)" -- "$cur") ) ;;
    completion)
      COMPREPLY=( $(compgen -W "bash zsh fish" -- "$cur") ) ;;
  esac
  return 0
}
complete -F _xbpretty xbpretty xbp xbp-install xbp-remove xbp-upgrade xbp-autoremove xbp-update
