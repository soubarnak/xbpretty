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

  local commands="install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade update download clean list search show policy history undo redo fetch completion link log uninstall dist-upgrade info"
  local global_flags="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui"
  local flags_install="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui --dry-run -y --yes --assume-yes -n --assume-no -d --download-only --simple --update --no-update --no-sync --purge --reinstall --auto-remove --autoremove --no-auto-remove --no-autoremove --full --no-full --safe --no-log --no-fix-broken --no-install-recommends --no-install-suggests"
  local flags_remove="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui --dry-run -y --yes --assume-yes -n --assume-no -d --download-only --simple --update --no-update --no-sync --purge --auto-remove --autoremove --no-auto-remove --no-autoremove --full --no-full --safe --no-log --no-fix-broken --no-install-recommends --no-install-suggests"
  local flags_purge="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui --dry-run -y --yes --assume-yes -n --assume-no -d --download-only --simple --update --no-update --no-sync --purge --auto-remove --autoremove --no-auto-remove --no-autoremove --full --no-full --safe --no-log --no-fix-broken --no-install-recommends --no-install-suggests"
  local flags_autoremove="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui --dry-run -y --yes --assume-yes -n --assume-no -d --download-only --simple --update --no-update --no-sync --purge --auto-remove --autoremove --no-auto-remove --no-autoremove --full --no-full --safe --no-log --no-fix-broken --no-install-recommends --no-install-suggests"
  local flags_autopurge="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui --dry-run -y --yes --assume-yes -n --assume-no -d --download-only --simple --update --no-update --no-sync --purge --auto-remove --autoremove --no-auto-remove --no-autoremove --full --no-full --safe --no-log --no-fix-broken --no-install-recommends --no-install-suggests"
  local flags_upgrade="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui --dry-run -y --yes --assume-yes -n --assume-no -d --download-only --simple --update --no-update --no-sync --purge --auto-remove --autoremove --no-auto-remove --no-autoremove --full --no-full --safe --print-uris --no-log --no-fix-broken --no-install-recommends --no-install-suggests"
  local flags_full_upgrade="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui --dry-run -y --yes --assume-yes -n --assume-no -d --download-only --simple --update --no-update --no-sync --purge --auto-remove --autoremove --no-auto-remove --no-autoremove --full --no-full --safe --print-uris --no-log --no-fix-broken --no-install-recommends --no-install-suggests"
  local flags_safe_upgrade="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui --dry-run -y --yes --assume-yes -n --assume-no -d --download-only --simple --update --no-update --no-sync --purge --auto-remove --autoremove --no-auto-remove --no-autoremove --full --no-full --safe --print-uris --no-log --no-fix-broken --no-install-recommends --no-install-suggests"
  local flags_update="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui --no-log"
  local flags_download="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui"
  local flags_clean="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui --lists --fetch"
  local flags_list="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui --virtual -a --all-versions -A --all-arches -i --installed -u --upgradable --description --summary -m --machine"
  local flags_search="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui --virtual -a --all-versions -A --all-arches -i --installed -u --upgradable --description --summary -m --machine -n --names --names-only"
  local flags_show="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui -a --all-versions -A --all-arches -m --machine"
  local flags_policy="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui -m --machine"
  local flags_history="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui --all"
  local flags_undo="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui -y --yes --assume-yes -n --assume-no --no-log"
  local flags_redo="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui -y --yes --assume-yes -n --assume-no --no-log"
  local flags_fetch="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui -c --country --dry-run --auto --https-only --non-free"
  local flags_completion="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui"
  local flags_link="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui"
  local flags_log="-h --help -V --version -v --verbose --debug --config --color --no-color --tui --no-tui --limit"

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
        uninstall)
          sub=remove; break ;;
        dist-upgrade)
          sub=full-upgrade; break ;;
        info)
          sub=show; break ;;
        up)
          sub=upgrade; break ;;
        dist-up)
          sub=upgrade; break ;;
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
    install|download|list|search|show)
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
complete -F _xbpretty xbpretty xbp xbp-install xbp-remove xbp-upgrade xbp-autoremove xbp-update
