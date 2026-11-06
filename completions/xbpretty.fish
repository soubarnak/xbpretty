# fish completion for xbpretty
function __xbpretty_verb
    set -l cmd (commandline -opc)
    switch $cmd[1]
        case xbp-install
            echo install
            return
        case xbp-remove
            echo remove
            return
        case xbp-upgrade
            echo upgrade
            return
        case xbp-autoremove
            echo autoremove
            return
        case xbp-update
            echo update
            return
    end
    for w in $cmd[2..-1]
        switch $w
            case install
                echo install
                return
            case remove
                echo remove
                return
            case purge
                echo purge
                return
            case autoremove
                echo autoremove
                return
            case autopurge
                echo autopurge
                return
            case upgrade
                echo upgrade
                return
            case full-upgrade
                echo full-upgrade
                return
            case safe-upgrade
                echo safe-upgrade
                return
            case update
                echo update
                return
            case download
                echo download
                return
            case clean
                echo clean
                return
            case list
                echo list
                return
            case search
                echo search
                return
            case show
                echo show
                return
            case policy
                echo policy
                return
            case history
                echo history
                return
            case undo
                echo undo
                return
            case redo
                echo redo
                return
            case fetch
                echo fetch
                return
            case completion
                echo completion
                return
            case link
                echo link
                return
            case log
                echo log
                return
            case uninstall
                echo remove
                return
            case dist-upgrade
                echo full-upgrade
                return
            case info
                echo show
                return
            case up
                echo upgrade
                return
            case dist-up
                echo upgrade
                return
        end
    end
end

function __xbpretty_no_subcommand
    test -z (__xbpretty_verb)
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

for __xbpretty_cmd in xbpretty xbp xbp-install xbp-remove xbp-upgrade xbp-autoremove xbp-update
    complete -c $__xbpretty_cmd -f
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a install -d 'install packages from repositories'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a remove -d 'remove installed packages'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a purge -d 'remove packages and their configuration files'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a autoremove -d 'remove orphaned packages'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a autopurge -d 'remove orphaned packages and their configuration files'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a upgrade -d 'upgrade all installed packages'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a full-upgrade -d 'upgrade all installed packages (same as upgrade)'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a safe-upgrade -d 'upgrade all installed packages (same as upgrade)'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a update -d 'refresh the repository index'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a download -d 'download package archives into the current directory'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a clean -d 'clean the package cache'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a list -d 'list packages matching the given globs'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a search -d 'search package names and descriptions'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a show -d 'show package details'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a policy -d 'show repository precedence and pinned packages'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a history -d 'list, inspect, undo, redo or clear transaction history'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a undo -d 'revert a recorded transaction'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a redo -d 'reapply a recorded transaction'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a fetch -d 'rank Void mirrors and configure repositories'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a completion -d 'print a shell completion script'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a link -d 'create the six xbp symlinks in a directory'
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a log -d 'print the transaction log'

    complete -c $__xbpretty_cmd -l version -d 'print version'
    complete -c $__xbpretty_cmd -s V -n 'not contains (__xbpretty_verb) list search' -d 'print version'
    complete -c $__xbpretty_cmd -s h -l help -d 'print this help'
    complete -c $__xbpretty_cmd -s v -l verbose -d 'print every xbps command before running it'
    complete -c $__xbpretty_cmd -l debug -d 'dump the resolved configuration'
    complete -c $__xbpretty_cmd -l config -x -d 'read FILE instead of the system and user config files'
    complete -c $__xbpretty_cmd -l color -x -a 'auto always never' -d 'colour output: auto, always or never'
    complete -c $__xbpretty_cmd -l no-color -d 'never emit ANSI colour'
    complete -c $__xbpretty_cmd -l tui -d 'force the full-screen interface'
    complete -c $__xbpretty_cmd -l no-tui -d 'disable the full-screen interface'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) list search' -s V -l virtual -d 'list virtual packages'
    complete -c $__xbpretty_cmd -n 'test (__xbpretty_verb) = fetch' -s c -l country -x -d 'restrict mirrors to a country or region (repeatable)'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade fetch' -l dry-run -d 'show the plan without changing anything'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade undo redo' -s y -l yes -d 'answer yes and skip the prompt'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade undo redo' -l assume-yes
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade undo redo' -s n -l assume-no -d 'refuse at the prompt'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade' -s d -l download-only -d 'download the plans archives and stop'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade' -l simple -d 'print the simple summary'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade' -l update -d 'refresh the repository index first'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade' -l no-update -d 'do not refresh the repository index'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade' -l no-sync -d 'do not refresh the repository index'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade' -l purge -d 'also remove configuration files'
    complete -c $__xbpretty_cmd -n 'test (__xbpretty_verb) = install' -l reinstall -d 'reinstall packages that are already installed'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade' -l auto-remove -d 'force the implicit orphan removal'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade' -l autoremove
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade' -l no-auto-remove -d 'skip the implicit orphan removal'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade' -l no-autoremove
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade' -l full -d 'accepted no-op; xbps -u never removes packages'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade' -l no-full -d 'accepted no-op; wins over --full'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade' -l safe -d 'accepted no-op for nala compatibility'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) upgrade full-upgrade safe-upgrade' -l print-uris -d 'print the plans download URLs and stop'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install remove purge autoremove autopurge upgrade full-upgrade safe-upgrade update undo redo' -l no-log -d 'do not append to the transaction log'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) list search show' -s a -l all-versions -d 'show every version of a package'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) list search' -s i -l installed -d 'show installed packages only'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) list search' -s u -l upgradable -d 'show upgradable packages only'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) list search' -l description -d 'print the long description under each row'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) list search' -l summary -d 'print the short summary under each row'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) list search show policy' -s m -l machine -d 'emit NDJSON schema 1'
    complete -c $__xbpretty_cmd -n 'test (__xbpretty_verb) = search' -s n -l names -d 'search names only'
    complete -c $__xbpretty_cmd -n 'test (__xbpretty_verb) = search' -l names-only
    complete -c $__xbpretty_cmd -n 'test (__xbpretty_verb) = log' -l limit -x -d 'keep only the last N matching log entries'
    complete -c $__xbpretty_cmd -n 'test (__xbpretty_verb) = clean' -l lists -d 'delete cached repodata files'
    complete -c $__xbpretty_cmd -n 'test (__xbpretty_verb) = clean' -l fetch -d 'restore the repository configuration written by fetch'
    complete -c $__xbpretty_cmd -n 'test (__xbpretty_verb) = history' -l all -d 'with history clear: delete every entry'
    complete -c $__xbpretty_cmd -n 'test (__xbpretty_verb) = fetch' -l auto -x -d 'validate the N fastest mirrors (default 3)'
    complete -c $__xbpretty_cmd -n 'test (__xbpretty_verb) = fetch' -l https-only -d 'probe and write https mirrors only'
    complete -c $__xbpretty_cmd -n 'test (__xbpretty_verb) = fetch' -l non-free -d 'include the non-free repository paths'

    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) install download list search show' -a '(__xbpretty_available)'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) remove purge policy log' -a '(__xbpretty_installed)'
    complete -c $__xbpretty_cmd -n 'contains (__xbpretty_verb) history undo redo' -a '(__xbpretty_history_ids)'
    complete -c $__xbpretty_cmd -n 'test (__xbpretty_verb) = completion' -a 'bash zsh fish'
end
