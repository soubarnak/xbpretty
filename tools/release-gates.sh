#!/bin/sh
# Release gates for xbpretty (FR-076, AC-058-AC-060).
#
#   tools/release-gates.sh binary   PATH      static link + 8,000,000-byte budget
#   tools/release-gates.sh package  PATH      x86_64 package budget (3,600,000 bytes)
#   tools/release-gates.sh contents ROOT      package's own staged tree; rejects any /etc entry
#   tools/release-gates.sh installed ROOT     merged clean-root tree; rejects /etc/xbpretty
#   tools/release-gates.sh assets   DIR VERSION [REVISION]
#                                             nine packages + signatures + repodata
#
# The workflow calls each mode; the Go tests call the modes directly with
# fixtures, so a violated budget or a missing installed file fails the release.
# Needs `file` and `readelf`; the static-link check fails closed without them.
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
	command -v file >/dev/null 2>&1 || fail "cannot verify static link: file is not installed"
	command -v readelf >/dev/null 2>&1 || fail "cannot verify static link: readelf is not installed"
	kind=$(file -b "$bin" 2>/dev/null) || fail "cannot inspect binary $bin with file"
	case $kind in
	*ELF*) ;;
	*) fail "binary $bin is not an ELF executable: $kind" ;;
	esac
	case $kind in
	*dynamically\ linked*) fail "binary $bin is dynamically linked: $kind" ;;
	esac
	if readelf -l "$bin" 2>/dev/null | grep -q 'INTERP'; then
		fail "binary $bin has a dynamic interpreter"
	fi
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

check_surface() {
	root=$1
	[ -d "$root" ] || fail "$root is not a tree"

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
}

check_contents() {
	root=$1
	check_surface "$root"
	# The package's own staged tree must contain no /etc entry at all. This is
	# deliberately stricter than the merged clean-root check below, which sees
	# the base system's /etc from installed dependencies.
	if [ -e "$root/etc" ] || [ -L "$root/etc" ]; then
		fail "the package staged an /etc entry"
	fi
}

check_installed() {
	root=$1
	check_surface "$root"
	if [ -e "$root/etc/xbpretty" ] || [ -L "$root/etc/xbpretty" ]; then
		fail "the package installed /etc/xbpretty"
	fi
}

check_assets() {
	dir=$1
	version=$2
	revision=${3:-1}
	[ -d "$dir" ] || fail "$dir is not a directory"
	for arch in $ARCHES; do
		pkg="$dir/xbpretty-${version}_${revision}.${arch}.xbps"
		require_file "$pkg"
		require_file "$pkg.sig2"
		require_file "$dir/$arch-repodata"
	done
}

[ $# -gt 0 ] || fail "usage: release-gates.sh binary|package|contents|installed|assets ..."
mode=$1
shift
case $mode in
binary) [ $# -eq 1 ] || fail "binary needs one path"; check_binary "$1" ;;
package) [ $# -eq 1 ] || fail "package needs one path"; check_package "$1" ;;
contents) [ $# -eq 1 ] || fail "contents needs one root"; check_contents "$1" ;;
installed) [ $# -eq 1 ] || fail "installed needs one root"; check_installed "$1" ;;
assets) [ $# -ge 2 ] && [ $# -le 3 ] || fail "assets needs a directory, version and optional revision"; check_assets "$@" ;;
*) fail "unknown mode $mode" ;;
esac
