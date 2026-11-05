#!/bin/sh
# Release gates for xbpretty (FR-076, AC-058-AC-060).
#
#   tools/release-gates.sh binary   PATH      static link + 8,000,000-byte budget
#   tools/release-gates.sh package  PATH      x86_64 package budget (3,600,000 bytes)
#   tools/release-gates.sh contents ROOT      installed surface, docs, no /etc/xbpretty
#   tools/release-gates.sh assets   DIR VERSION  nine packages + signatures + repodata
#   tools/release-gates.sh snapshot DATE [DAYS]  embedded mirror snapshot age
#
# The workflow calls each mode; the Go tests call the modes directly with
# fixtures, so a violated budget or a missing installed file fails the release.
set -eu

MAX_BINARY_BYTES=${MAX_BINARY_BYTES:-8000000}
MAX_PACKAGE_BYTES=${MAX_PACKAGE_BYTES:-3600000}
ARCHES=${ARCHES:-"x86_64 x86_64-musl i686 aarch64 aarch64-musl armv7l armv7l-musl armv6l armv6l-musl"}
ALIASES="xbp xbp-install xbp-remove xbp-upgrade xbp-autoremove xbp-update"

fail() {
	printf 'release-gates: %s\n' "$*" >&2
	exit 1
}

size_of() {
	if [ ! -f "$1" ]; then
		fail "$1 does not exist"
	fi
	wc -c <"$1" | tr -d ' '
}

check_binary() {
	bin=$1
	size=$(size_of "$bin")
	if [ "$size" -gt "$MAX_BINARY_BYTES" ]; then
		fail "binary $bin is $size bytes; the budget is $MAX_BINARY_BYTES bytes"
	fi
	kind=$(file -b "$bin" 2>/dev/null || true)
	case $kind in
	*ELF*) ;;
	*) fail "binary $bin is not an ELF executable: $kind" ;;
	esac
	case $kind in
	*dynamically\ linked*)
		fail "binary $bin is dynamically linked: $kind"
		;;
	*statically\ linked*)
		;;
	*)
		# `file` does not always name the link mode. An ELF with an
		# INTERP program header is dynamic; a static Go binary has none.
		if command -v readelf >/dev/null 2>&1 && readelf -l "$bin" 2>/dev/null | grep -q 'INTERP'; then
			fail "binary $bin has a dynamic interpreter"
		fi
		;;
	esac
}

check_package() {
	pkg=$1
	size=$(size_of "$pkg")
	if [ "$size" -gt "$MAX_PACKAGE_BYTES" ]; then
		fail "package $pkg is $size bytes; the budget is $MAX_PACKAGE_BYTES bytes"
	fi
}

require_file() {
	[ -f "$1" ] || fail "missing installed file $1"
}

require_link() {
	[ -L "$1" ] || fail "missing installed symlink $1"
	[ "$(readlink "$1")" = "$2" ] || fail "symlink $1 points to $(readlink "$1"), want $2"
}

check_contents() {
	root=$1
	[ -d "$root" ] || fail "$root is not an installed tree"

	require_file "$root/usr/bin/xbpretty"
	for alias in $ALIASES; do
		require_link "$root/usr/bin/$alias" xbpretty
	done
	# The alias set is closed: a stray xbp-* name fails the release.
	for path in "$root"/usr/bin/xbp-*; do
		[ -e "$path" ] || continue
		name=${path##*/}
		case " $ALIASES " in
		*" $name "*) ;;
		*) fail "unexpected argv[0] alias $name" ;;
		esac
	done

	require_file "$root/usr/share/bash-completion/completions/xbpretty"
	require_file "$root/usr/share/zsh/site-functions/_xbpretty"
	require_file "$root/usr/share/fish/vendor_completions.d/xbpretty.fish"
	require_file "$root/usr/share/man/man8/xbpretty.8"
	require_file "$root/usr/share/man/man5/xbpretty.conf.5"
	for alias in $ALIASES; do
		require_link "$root/usr/share/man/man8/$alias.8" xbpretty.8
	done

	require_file "$root/usr/share/doc/xbpretty/config.toml.example"
	require_file "$root/usr/share/doc/xbpretty/migration-rev6.md"
	require_file "$root/usr/share/licenses/xbpretty/LICENSE"

	if [ -e "$root/etc/xbpretty" ] || [ -L "$root/etc/xbpretty" ]; then
		fail "the package installed /etc/xbpretty"
	fi
}

check_assets() {
	dir=$1
	version=$2
	[ -d "$dir" ] || fail "$dir is not a directory"
	for arch in $ARCHES; do
		pkg="$dir/xbpretty-${version}_1.${arch}.xbps"
		require_file "$pkg"
		require_file "$pkg.sig2"
		require_file "$dir/$arch-repodata"
	done
}

check_snapshot() {
	date=$1
	max_days=${2:-90}
	case $date in
	[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]) ;;
	*) fail "invalid snapshot date $date" ;;
	esac
	epoch=$(date -u -d "$date" +%s 2>/dev/null) || fail "cannot parse snapshot date $date"
	now=$(date -u +%s)
	age_days=$(((now - epoch) / 86400))
	if [ "$age_days" -gt "$max_days" ]; then
		fail "mirror snapshot is $age_days days old (limit $max_days); run make mirrors"
	fi
}

[ $# -gt 0 ] || fail "usage: release-gates.sh binary|package|contents|assets|snapshot ..."
mode=$1
shift
case $mode in
binary) [ $# -eq 1 ] || fail "binary needs one path"; check_binary "$1" ;;
package) [ $# -eq 1 ] || fail "package needs one path"; check_package "$1" ;;
contents) [ $# -eq 1 ] || fail "contents needs one root"; check_contents "$1" ;;
assets) [ $# -eq 2 ] || fail "assets needs a directory and a version"; check_assets "$1" "$2" ;;
snapshot) [ $# -ge 1 ] && [ $# -le 2 ] || fail "snapshot needs a date and optional days"; check_snapshot "$@" ;;
*) fail "unknown mode $mode" ;;
esac
