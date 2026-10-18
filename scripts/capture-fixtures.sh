#!/bin/sh
# Regenerate the xbpretty fixture corpus from a real xbps.
#
#   scripts/capture-fixtures.sh [output-root]        # default: testdata
#
# Every case is written in the shape the tests read it back in, so a
# regeneration feeds the tests instead of landing beside them:
#
#   <root>/parser/<key>/<case>.txt    the dry-run stdout internal/xbps parses
#   <root>/hints/<key>/<case>.txt     the stderr internal/hints matches a hint on
#   <root>/hints/<key>/<case>.out     its stdout, matched too (ADR-0006)
#   <root>/lookups/<key>/<case>.txt   the stdout of an xbps-query the CLI reads
#   <root>/<kind>/<key>/<case>.meta   the command, its exit code, and the state
#                                     the capture had to induce first
#
# The remaining stream of each case is kept beside its fixture: `.err` next to
# a plan case. A hint case keeps both streams under the same names every time,
# `.txt` for stderr and `.out` for stdout, because xbps writes some failures to
# one and some to the other. <key> is the running xbps version
# with its dots dropped (0.60.7_1 -> 0607), so a capture lands in the
# directory of the version that produced it.
#
# Modes:
#
#   XB_CAPTURE_MODE=container (default) runs two phases in a throwaway
#     voidlinux container. "pre" captures what the image's own xbps (0.59.1)
#     can produce, then xbps is upgraded and "post" captures the 0.60.7 cases
#     whose package database state the host must not be given. Set
#     XB_CAPTURE_PHASE=pre|post to run one phase only.
#
#   XB_CAPTURE_MODE=host runs against the host's own xbps as root and performs
#     real transactions: it installs the packages the plan cases target and
#     truncates the live repository index (restored afterwards). Its lock-race
#     step holds the package-database lock with an upgrade that is never
#     confirmed, so it cannot upgrade the system. Run it as
#     `sudo XB_CAPTURE_MODE=host scripts/capture-fixtures.sh`.
#     It refuses to run without root, prints what it is about to change before
#     it changes anything, and exits non-zero unless the host's xbps is
#     0.60.7, the version whose directory it writes. Set
#     XB_CAPTURE_ALLOW_XBPS_VERSION=1 to run against another xbps version; the
#     output then lands in that version's own directory (for a container
#     check, give an output root outside the repository's testdata).
#
# Both modes are needed for a complete corpus: the host supplies the 0.60.7
# plan and hint cases, the container the 0.59.1 ones plus every case that needs
# an induced hold or unpacked state.
set -eu

ROOT="${1:-testdata}"
MODE="${XB_CAPTURE_MODE:-container}"
PHASE="${XB_CAPTURE_PHASE:-all}"
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PKGDB=/var/db/xbps/pkgdb-0.38.plist
# The xbps whose corpus directory (testdata/*/0607) a host capture rewrites.
HOST_XBPS_VERSION=0.60.7

# Outside the container the capture is re-entered with the output root as its
# only argument; the mounts make that directory the whole of /out.
if [ "$MODE" = "container" ] && [ "$(id -u)" != "0" ]; then
	case "$PHASE" in all | pre | post) ;; *)
		echo "XB_CAPTURE_PHASE must be all, pre or post" >&2
		exit 2
		;;
	esac
	echo "capturing in voidlinux/voidlinux:latest -> $ROOT"
	exec podman run --rm --privileged \
		-e "XB_CAPTURE_PHASE=$PHASE" \
		-v "$ROOT_DIR/$ROOT:/out:rw,z" \
		-v "$ROOT_DIR/scripts/capture-fixtures.sh:/capture.sh:ro,z" \
		docker.io/voidlinux/voidlinux:latest \
		sh -c 'mkdir -p /etc/xbps.d
# The image ships a repository of its own, definition and cached index both:
# drop them, so every capture comes from the one repository the fixtures are
# read against rather than whichever mirror is reachable.
rm -f /usr/share/xbps.d/00-repository-main.conf
rm -rf /var/db/xbps/https___alpha*
printf "repository=https://repo-default.voidlinux.org/current\n" > /etc/xbps.d/00-repository-main.conf
export LC_ALL=C
xbps-install -S >/dev/null 2>&1 || true
sh /capture.sh /out'
fi

export LC_ALL=C

# Every capture lands in the directory of the xbps that produced it, so the
# version is resolved again for each phase: the container's own xbps becomes
# 0.60.7 halfway through a run.
set_dirs() {
	KEY="$(xbps-query -p pkgver xbps | awk -F- '{ print $NF }' | awk -F_ '{ print $1 }' | tr -d .)"
	PLANS="$ROOT/parser/$KEY"
	HINTS="$ROOT/hints/$KEY"
	LOOKUPS="$ROOT/lookups/$KEY"
	PTY="$ROOT/pty/$KEY"
	mkdir -p "$PLANS" "$HINTS" "$LOOKUPS"
	{
		echo "xbps: $(xbps-query -p pkgver xbps)"
		echo "date: $(date -Is)"
		echo "mode: $MODE"
	} > "$ROOT/version-$KEY.txt"
}

# fixture names the stream the case's test reads: a plan is read from stdout,
# a hint from stderr, a lookup from stdout. Both streams are kept either way.
# POSIX sh has no locals outside a function's own scope, so every case
# variable is prefixed rather than shadowing the caller's.
cap() {
	cap_kind="$1"; cap_name="$2"; cap_fixture="$3"; shift 3
	case "$cap_kind" in
	parser) cap_dir="$PLANS" ;;
	hints) cap_dir="$HINTS" ;;
	lookups) cap_dir="$LOOKUPS" ;;
	*)
		echo "unknown fixture kind $cap_kind" >&2
		exit 2
		;;
	esac
	mkdir -p "$cap_dir"
	cap_ec=0
	case "$cap_fixture" in
	stdout) "$@" > "$cap_dir/$cap_name.txt" 2> "$cap_dir/$cap_name.err" || cap_ec=$? ;;
	stderr) "$@" > "$cap_dir/$cap_name.out" 2> "$cap_dir/$cap_name.txt" || cap_ec=$? ;;
	both) "$@" > "$cap_dir/$cap_name.out" 2> "$cap_dir/$cap_name.err" || cap_ec=$? ;;
	esac
	{
		printf 'CMD: %s\n' "$*"
		printf 'EXIT: %s\n' "$cap_ec"
	} > "$cap_dir/$cap_name.meta"
	echo "--- $KEY/$cap_name exit=$cap_ec"
}

# The command and exit code a plan case ran with, for a case whose output is
# only interesting next to how the package database was set up: the .meta
# carries the command, a following line carries the state.
plan_case() { cap_name="$1"; shift; cap parser "$cap_name" stdout "$@"; }
lookup_case() { cap_name="$1"; shift; cap lookups "$cap_name" stdout "$@"; }
hint_case() { cap_name="$1"; shift; cap hints "$cap_name" stderr "$@"; }

# The first package with an upgrade pending: holding it is what makes a plan
# print a `hold` row.
pending_update() {
	xbps-install -Sun 2>/dev/null | awk '$2 == "update" { print $1; exit }'
}

# xbps-pkgdb takes a package name where a plan prints a pkgver.
pkg_name() { printf '%s\n' "$1" | sed 's/-[^-]*$//'; }

die() {
	echo "capture-fixtures: $*" >&2
	exit 1
}

# The plan carrying a `configure` row. A package is left configurable by
# killing an install between unpacking it and configuring it: the cache is
# warmed first so the kill lands in the unpack phase, which the loop watches
# for in the package database rather than guessing at a delay. Nothing repairs
# the state afterwards, so this is the last case of a run.
cap_configure_plan() {
	xbps-install -Sy -D figlet wget curl htop >/dev/null 2>&1 || true
	xbps-install -Sy figlet wget curl htop >/dev/null 2>&1 &
	hung="$!"
	interrupted=0
	polls=0
	while [ "$polls" -lt 200 ]; do
		if grep -q '<string>unpacked</string>' "$PKGDB"; then
			interrupted=1
			break
		fi
		kill -0 "$hung" 2>/dev/null || break
		sleep 0.05
		polls=$((polls + 1))
	done
	kill -9 "$hung" 2>/dev/null || true
	wait 2>/dev/null || true
	[ "$interrupted" = 1 ] || die "the install finished before it could be interrupted"
	plan_case configure xbps-install -Sun
	printf 'INDUCED: the install of figlet wget curl htop was killed after unpacking\n' >> "$PLANS/configure.meta"
	awk '$2 == "configure"' "$PLANS/configure.txt" | grep -q . ||
		die "the configure case captured no configure row"
}

# The plan carrying a `hold` row, and the held list beside it.
cap_hold_plan() {
	pkg="$(pending_update)"
	[ -n "$pkg" ] || die "no package with an upgrade pending to put on hold"
	pkgname="$(pkg_name "$pkg")"
	xbps-pkgdb -m hold "$pkgname"
	plan_case hold xbps-install -Sun
	printf 'INDUCED: xbps-pkgdb -m hold %s\n' "$pkgname" >> "$PLANS/hold.meta"
	lookup_case hold xbps-query -H
	printf 'INDUCED: xbps-pkgdb -m hold %s\n' "$pkgname" >> "$LOOKUPS/hold.meta"
	xbps-pkgdb -m unhold "$pkgname"
	awk '$2 == "hold"' "$PLANS/hold.txt" | grep -q . ||
		die "the hold case captured no hold row"
}

# The held list on its own, for the version whose plans drop held packages
# rather than printing them as `hold`.
cap_hold_list() {
	pkg="$(pending_update)"
	[ -n "$pkg" ] || die "no package with an upgrade pending to put on hold"
	pkgname="$(pkg_name "$pkg")"
	xbps-pkgdb -m hold "$pkgname"
	lookup_case hold xbps-query -H
	printf 'INDUCED: xbps-pkgdb -m hold %s\n' "$pkgname" >> "$LOOKUPS/hold.meta"
	xbps-pkgdb -m unhold "$pkgname"
}

# What the container's own xbps can produce before xbps itself is upgraded.
# No `configure` plan here: a package only reaches the unpacked state through a
# transaction, and this xbps refuses every transaction until xbps itself is
# updated — the case its own hint fixture captures.
capture_pre() {
	set_dirs
	hint_case xbps-unsigned-update xbps-install -Sn htop curl wget
	# An install's real run, as `xbp install` issues it after a synced plan:
	# the refusal stays on stderr, unlike the upgrade's below.
	hint_case xbps-unsigned-update-install xbps-install -y figlet
	# The same refusal on the other stream: an upgrade gets it on stdout.
	hint_case xbps-unsigned-update-upgrade xbps-install -Syu
	hint_case not-installed xbps-remove -yn this-package-does-not-exist-xyz

	plan_case upgrade xbps-install -Sun

	lookup_case xbps xbps-query -p pkgver,installed_size xbps
	lookup_case base-files xbps-query -p pkgver,installed_size base-files
	lookup_case missing xbps-query -p pkgver,installed_size this-package-does-not-exist-xyz

	cap_hold_list

	# A truncated index reads as a package that was never there at this
	# version: no "failed to open repodata" line is printed at all.
	REPO_DIR="$(ls -d /var/db/xbps/https___repo-default* 2>/dev/null | head -1 || true)"
	if [ -n "$REPO_DIR" ] && [ -f "$REPO_DIR/x86_64-repodata" ]; then
		cp "$REPO_DIR/x86_64-repodata" /tmp/repodata.bak
		: > "$REPO_DIR/x86_64-repodata"
		hint_case not-found xbps-install -Sn figlet
		printf 'INDUCED: the repository index was truncated first\n' >> "$HINTS/not-found.meta"
		mv -f /tmp/repodata.bak "$REPO_DIR/x86_64-repodata"
		xbps-install -S >/dev/null 2>&1 || true
	fi
}

# What needs 0.60.7 plus package database state: the two plan actions the
# parser had no capture of, and the held list that goes with one of them.
capture_post() {
	set_dirs
	cap_hold_plan
	cap_configure_plan
}

# Packages the install case may target, in order of preference: small, with
# few dependencies, and absent from a default Void install.
INSTALL_CANDIDATES="figlet cowsay sl tree"

# The first candidate the host does not have installed: an install plan for a
# package already present holds no `install` row.
uninstalled_candidate() {
	for candidate in $INSTALL_CANDIDATES; do
		if ! xbps-query "$candidate" >/dev/null 2>&1; then
			printf '%s\n' "$candidate"
			return 0
		fi
	done
	return 1
}

# Host mode's guard rails, run before anything changes the host. A capture
# that runs on the wrong xbps would overwrite the corpus of another version.
host_preflight() {
	[ "$(id -u)" = "0" ] ||
		die "host mode needs root: run it as sudo XB_CAPTURE_MODE=host $0"
	host_version="$(xbps-install --version | awk '{ print $2 }')"
	if [ "$host_version" != "$HOST_XBPS_VERSION" ]; then
		if [ "${XB_CAPTURE_ALLOW_XBPS_VERSION:-}" = "1" ]; then
			echo "capture-fixtures: host xbps is $host_version, not $HOST_XBPS_VERSION; continuing because XB_CAPTURE_ALLOW_XBPS_VERSION=1" >&2
		else
			die "host xbps is $host_version but host mode rewrites the $HOST_XBPS_VERSION corpus; set XB_CAPTURE_ALLOW_XBPS_VERSION=1 to run anyway"
		fi
	fi
	INSTALL_PKG="$(uninstalled_candidate)" ||
		die "none of $INSTALL_CANDIDATES is free of the host: the install case needs an uninstalled package"
	cat >&2 <<NOTICE
capture-fixtures: host mode will change this host (xbps $host_version):
  - install $INSTALL_PKG, wget and curl if they are missing, and leave them installed
  - truncate the live repository index while two plans are captured, then restore it from a private backup
  - hold the package-database lock with an upgrade that is never confirmed (the host is not upgraded)
  - write the fixtures under $ROOT
NOTICE
}

# The host's own xbps. Every case here can change the system: the plans need
# their targets installed, the stale-index family truncates the live index,
# and the lock race runs a transaction that waits on the lock.
capture_host() {
	host_preflight
	set_dirs
	# The install plan comes first: once the package is installed the plan
	# holds no `install` row.
	plan_case install xbps-install -Sn "$INSTALL_PKG"
	awk '$2 == "install"' "$PLANS/install.txt" | grep -q . ||
		die "the install case captured no install row"
	# The packages the remove and real-run cases need to already exist.
	xbps-install -Sy "$INSTALL_PKG" wget curl >/dev/null 2>&1 || true

	plan_case reinstall xbps-install -Sfn curl
	plan_case remove xbps-remove -n wget
	plan_case upgrade xbps-install -Sun
	plan_case autoremove xbps-remove -on
	plan_case nothing xbps-install -Sun

	# The stale-index family: the same error with two different exit codes
	# (ADR-0002 #1). The index is truncated in place, so an interrupt must
	# restore it or every later case sees a corrupt repository. The backup is
	# a private temporary file, restored by the trap.
	REPO_DIR="$(ls -d /var/db/xbps/https___repo-default* 2>/dev/null | head -1 || true)"
	INDEX_BACKUP=""
	restore_index() {
		if [ -n "$INDEX_BACKUP" ] && [ -f "$INDEX_BACKUP" ]; then
			mv -f "$INDEX_BACKUP" "$REPO_DIR/x86_64-repodata"
		fi
	}
	if [ -n "$REPO_DIR" ] && [ -f "$REPO_DIR/x86_64-repodata" ]; then
		INDEX_BACKUP="$(mktemp)"
		trap 'restore_index' EXIT INT TERM
		cp "$REPO_DIR/x86_64-repodata" "$INDEX_BACKUP"
		: > "$REPO_DIR/x86_64-repodata"
		hint_case stale-index xbps-install -Sn "$INSTALL_PKG"
		hint_case stale-index-upgrade xbps-install -Sun
		restore_index
		trap - EXIT INT TERM
		# Re-sync: truncating may have left the cached archive unusable.
		xbps-install -S >/dev/null 2>&1 || true
	fi

	# FR-017's other failures. `ignorepkg` is absent on purpose: it prints
	# nothing at all, so there is no stderr to match (ADR-0002 #3).
	hint_case not-found xbps-install -Sn this-package-does-not-exist-xyz
	# The "not installed" message is on stdout with nothing on stderr, so its
	# hint matches the stdout half of the capture (ADR-0006).
	hint_case not-installed xbps-remove -yn this-package-does-not-exist-xyz

	lookup_case xbps xbps-query -p pkgver,installed_size xbps
	lookup_case base-files xbps-query -p pkgver,installed_size base-files
	lookup_case missing xbps-query -p pkgver,installed_size this-package-does-not-exist-xyz

	# Reverse dependencies for FR-018.
	lookup_case revdeps xbps-query -X wget

	# Real execution: streamed output and xbps's own prompt, captured through
	# a pty so the prompt bytes are exact (FR-009, FR-014). These are raw
	# captures no test reads. The prompt is answered `n` after two seconds, so
	# no step waits out a timeout on a prompt and none can confirm anything.
	cap_pty() {
		name="$1"
		shift
		mkdir -p "$PTY"
		printf 'CMD: %s\nPTY: yes\n' "$*" > "$PTY/$name.meta"
		ec=0
		(sleep 2; echo n) | timeout 90 script -q -c "$*" "$PTY/$name.typescript" >/dev/null 2>&1 || ec=$?
		printf 'EXIT: %s\n' "$ec" >> "$PTY/$name.meta"
		echo "--- $KEY/$name (pty) exit=$ec"
	}
	cap_pty real-install xbps-install -y "$INSTALL_PKG"
	cap_pty real-prompt xbps-install "$INSTALL_PKG"

	# A lock held by a second live xbps process: the warning is captured from
	# a racing transaction (ADR-0002 #4), since a plain flock does not block
	# it. The index must be fresh here or the race captures a sync failure.
	# Neither run is confirmed: no -y, and stdin is never a terminal. The
	# holder's stdin is a pipe that closes after a few seconds, so it keeps the
	# lock while it waits at xbps's prompt and then aborts; the foreground run
	# reads /dev/null, waits for the lock, and aborts at the same prompt.
	# Neither can upgrade the system. The holder only keeps the lock while an
	# upgrade is pending for it to ask about, so the case stops with an error
	# when no lock warning was captured.
	xbps-install -S >/dev/null 2>&1 || true
	(sleep 6 | xbps-install -Su >/dev/null 2>&1) &
	sleep 1.5
	mkdir -p "$HINTS"
	printf 'CMD: xbps-install -Su\n' > "$HINTS/lock-wait.meta"
	ec=0
	timeout 60 xbps-install -Su < /dev/null > "$HINTS/lock-wait.out" 2> "$HINTS/lock-wait.txt" || ec=$?
	printf 'EXIT: %s\nNOTE: raced against a live transaction that was never confirmed\n' "$ec" >> "$HINTS/lock-wait.meta"
	wait || true
	grep -q 'package database locked' "$HINTS/lock-wait.txt" ||
		die "the lock-wait case captured no lock warning (is an upgrade pending?)"
}

case "$MODE" in
container)
	case "$PHASE" in
	all | pre | post) ;;
	*)
		echo "XB_CAPTURE_PHASE must be all, pre or post" >&2
		exit 2
		;;
	esac
	if [ "$PHASE" = "all" ] || [ "$PHASE" = "pre" ]; then
		capture_pre
	fi
	if [ "$PHASE" = "all" ] || [ "$PHASE" = "post" ]; then
		# xbps must be current before it will transact at all.
		xbps-install -yu xbps >/dev/null 2>&1 || true
		capture_post
	fi
	;;
host)
	capture_host
	;;
*)
	echo "XB_CAPTURE_MODE must be container or host" >&2
	exit 2
	;;
esac

echo "captured $KEY into $ROOT"
