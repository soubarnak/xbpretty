#!/bin/sh
# Measures whether xbps 0.60.7 writes its closing "downloaded, installed" line
# when stdout is a pipe or a file (ADR-0006, "Piped stdout"). Runs only inside a
# throwaway rootless podman container, never on the host:
#
#   scripts/measure-piped-stdout.sh
#
# Each run installs figlet from an empty package cache. `script` gives xbps a
# terminal on stderr; stdout is the terminal, a pipe drained to EOF, or a file.
# strace records every write to fd 1.
set -eu

if [ "${XB_INSIDE:-}" != "1" ]; then
	command -v podman >/dev/null || { echo "podman is required" >&2; exit 2; }
	exec podman run --rm -e XB_INSIDE=1 \
		-v "$(cd "$(dirname "$0")" && pwd)/measure-piped-stdout.sh:/measure.sh:ro,z" \
		ghcr.io/void-linux/void-glibc:latest sh /measure.sh
fi

export LC_ALL=C
xbps-install -Syu xbps >/dev/null 2>&1
xbps-install --version
xbps-install -y strace util-linux >/dev/null 2>&1
mkdir -p /m

closing='1 downloaded, 1 installed'
run() { # name, command run under a pty
	xbps-remove -y figlet >/dev/null 2>&1 || true
	rm -rf /var/cache/xbps/*
	script -qec "$2" /dev/null >/m/"$1".tty 2>&1
	printf '%-8s closing line: stdout/tty %s, strace write(1) %s\n' "$1" \
		"$(grep -c "$closing" /m/"$1".out /m/"$1".tty 2>/dev/null | tr '\n' ' ')" \
		"$(grep -c "$closing" /m/"$1".strace 2>/dev/null || echo -)"
}
run tty 'xbps-install -y figlet'
run pipe 'xbps-install -y figlet | cat >/m/pipe.out'
run file 'xbps-install -y figlet >/m/file.out'
run tpipe 'strace -f -s 8192 -e trace=write,writev -o /m/tpipe.strace xbps-install -y figlet 2>/dev/null | cat >/m/tpipe.out'
run tfile 'strace -f -s 8192 -e trace=write,writev -o /m/tfile.strace xbps-install -y figlet 2>/dev/null >/m/tfile.out'
grep -n "$closing" /m/tpipe.strace | cut -c1-40
