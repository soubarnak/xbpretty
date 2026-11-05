#!/bin/sh
# Sign one xbpretty package and its repodata per architecture, then refuse to
# publish anything when the signing key is absent (FR-076, AC-060).
#
#   XBPS_PRIVKEY=... tools/release-sign.sh ASSETS_DIR VERSION [ARCH...]
#
# Without XBPS_PRIVKEY the script exits non-zero before touching a package:
# a release must never publish an unsigned repository.
set -eu

if [ -z "${XBPS_PRIVKEY:-}" ]; then
	echo "secret XBPS_PRIVKEY is not set; refusing to publish an unsigned repository" >&2
	exit 1
fi

[ $# -ge 2 ] || {
	echo "usage: XBPS_PRIVKEY=... tools/release-sign.sh ASSETS_DIR VERSION [ARCH...]" >&2
	exit 2
}
assets=$1
version=$2
shift 2
archs=${*:-"x86_64 x86_64-musl i686 aarch64 aarch64-musl armv7l armv7l-musl armv6l armv6l-musl"}

[ -d "$assets" ] || {
	echo "release-sign: $assets is not a directory" >&2
	exit 2
}
assets=$(cd "$assets" && pwd)

key=$(mktemp)
trap 'rm -f "$key"' EXIT HUP INT TERM
umask 077
printf '%s\n' "$XBPS_PRIVKEY" >"$key"
umask 022

cd "$assets"
for arch in $archs; do
	pkg="xbpretty-${version}_1.${arch}.xbps"
	[ -f "$pkg" ] || {
		echo "release-sign: missing $pkg" >&2
		exit 1
	}
	XBPS_ARCH=$arch xbps-rindex -a "$pkg"
	XBPS_ARCH=$arch xbps-rindex --privkey "$key" \
		--signedby "Bongbetic <soubarna@live.in>" -s .
	XBPS_ARCH=$arch xbps-rindex --privkey "$key" -S "$pkg"
done
