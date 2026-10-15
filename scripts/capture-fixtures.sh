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
#   <root>/lookups/<key>/<case>.txt   the stdout of an xbps-query the CLI reads
#   <root>/<kind>/<key>/<case>.meta   the command, its exit code, and the state
#                                     the capture had to induce first
#
# The remaining stream of each case is kept beside its fixture: `.err` next to
# a plan case, `.out` next to a hint case. <key> is the running xbps version
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
#     upgrades the system to race the package-database lock. Run it as
#     `sudo XB_CAPTURE_MODE=host scripts/capture-fixtures.sh`.
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

# The host's own xbps. Every case here changes the system: the plans need
# their targets installed, the stale-index family truncates the live index,
# and the lock race runs two real transactions.
capture_host() {
	set_dirs
	# The packages the remove and real-run cases need to already exist.
	xbps-install -Sy figlet wget curl >/dev/null 2>&1 || true

	plan_case install xbps-install -Sn figlet
	plan_case reinstall xbps-install -Sfn curl
	plan_case remove xbps-remove -n wget
	plan_case upgrade xbps-install -Sun
	plan_case autoremove xbps-remove -on
	plan_case nothing xbps-install -Sun

	# The stale-index family: the same error with two different exit codes
	# (ADR-0002 #1). The index is truncated in place, so an interrupt must
	# restore it or every later case sees a corrupt repository.
	REPO_DIR="$(ls -d /var/db/xbps/https___repo-default* 2>/dev/null | head -1 || true)"
	restore_index() {
		if [ -f /tmp/repodata.bak ]; then
			mv -f /tmp/repodata.bak "$REPO_DIR/x86_64-repodata"
		fi
	}
	if [ -n "$REPO_DIR" ] && [ -f "$REPO_DIR/x86_64-repodata" ]; then
		trap 'restore_index' EXIT INT TERM
		cp "$REPO_DIR/x86_64-repodata" /tmp/repodata.bak
		: > "$REPO_DIR/x86_64-repodata"
		hint_case stale-index xbps-install -Sn figlet
		hint_case stale-index-upgrade xbps-install -Sun
		restore_index
		trap - EXIT INT TERM
		# Re-sync: truncating may have left the cached archive unusable.
		xbps-install -S >/dev/null 2>&1 || true
	fi

	# FR-017's other failures. `ignorepkg` is absent on purpose: it prints
	# nothing at all, so there is no stderr to match (ADR-0002 #3).
	hint_case not-found xbps-install -Sn this-package-does-not-exist-xyz
	# The "not installed" message goes to stdout, so no hint can match it:
	# captured as both streams, with no fixture for a test to read.
	cap hints not-installed both xbps-remove -yn this-package-does-not-exist-xyz

	lookup_case xbps xbps-query -p pkgver,installed_size xbps
	lookup_case base-files xbps-query -p pkgver,installed_size base-files
	lookup_case missing xbps-query -p pkgver,installed_size this-package-does-not-exist-xyz

	# Reverse dependencies for FR-018.
	lookup_case revdeps xbps-query -X wget

	# Real execution: streamed output and xbps's own prompt, captured through
	# a pty so the prompt bytes are exact (FR-009, FR-014). These are raw
	# captures no test reads.
	cap_pty() {
		name="$1"
		shift
		mkdir -p "$PTY"
		printf 'CMD: %s\nPTY: yes\n' "$*" > "$PTY/$name.meta"
		ec=0
		timeout 90 script -q -c "$*" "$PTY/$name.typescript" >/dev/null 2>&1 || ec=$?
		printf 'EXIT: %s\n' "$ec" >> "$PTY/$name.meta"
		echo "--- $KEY/$name (pty) exit=$ec"
	}
	cap_pty real-install xbps-install -y figlet
	cap_pty real-prompt xbps-install figlet

	# A lock held by a second live xbps process: the warning is captured from
	# a racing transaction (ADR-0002 #4), since a plain flock does not block
	# it. The index must be fresh here or the race captures a sync failure.
	xbps-install -S >/dev/null 2>&1 || true
	(xbps-install -Syu >/dev/null 2>&1) &
	sleep 0.8
	mkdir -p "$HINTS"
	printf 'CMD: xbps-install -Syu\n' > "$HINTS/lock-wait.meta"
	ec=0
	timeout 60 xbps-install -Syu > "$HINTS/lock-wait.out" 2> "$HINTS/lock-wait.txt" || ec=$?
	printf 'EXIT: %s\nNOTE: raced against a live transaction\n' "$ec" >> "$HINTS/lock-wait.meta"
	wait || true
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
