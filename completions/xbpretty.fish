# fish completion for xbpretty
function __xbpretty_no_subcommand
    set -l cmd (commandline -opc)
    for w in $cmd[2..-1]
        switch $w
            case install remove upgrade autoremove update completion link log
                return 1
        end
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

function __xbpretty_log_names
    string match -q -- '-*' (commandline -ct); and return 0
    __xbpretty_installed
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
    complete -c $__xbpretty_cmd -n '__xbpretty_no_subcommand' -a log -d 'print the transaction log'

    complete -c $__xbpretty_cmd -s n -l dry-run -d 'show the plan without changing anything'
    complete -c $__xbpretty_cmd -s y -l yes -d 'assume yes at the prompt'
    complete -c $__xbpretty_cmd -s v -l verbose -d 'print each xbps command'
    complete -c $__xbpretty_cmd -l no-sync -d 'skip the repository index sync'
    complete -c $__xbpretty_cmd -l no-log -d 'skip the transaction log'
    complete -c $__xbpretty_cmd -l color -x -a 'auto always never' -d 'colour output'
    complete -c $__xbpretty_cmd -l version -d 'print version'

    complete -c $__xbpretty_cmd -n '__fish_seen_subcommand_from install' -a '(__xbpretty_available)'
    complete -c $__xbpretty_cmd -n '__fish_seen_subcommand_from remove' -a '(__xbpretty_installed)'
    complete -c $__xbpretty_cmd -n '__fish_seen_subcommand_from log' -l limit -x -d 'keep only the last N matching lines'
    complete -c $__xbpretty_cmd -n '__fish_seen_subcommand_from log' -a '(__xbpretty_log_names)'
    complete -c $__xbpretty_cmd -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'
end
