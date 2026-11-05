#!/bin/sh
# Build xbpretty .xbps packages without a void-packages checkout and index them in a local
# repository. xbpretty is a static Go binary, so there is one package per architecture.
#   tools/build-xbps.sh [REPO_DIR [ARCH...]]    default: build/repo, every architecture below
# Install with: sudo xbps-install --repository="$PWD/build/repo" xbpretty
# Needs go, make, xbps-create and xbps-rindex. The version comes from srcpkgs/xbpretty/template.
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
version=$(sed -n 's/^version=//p' "$here/srcpkgs/xbpretty/template")
revision=${REVISION:-1}
repo=${1:-$here/build/repo}
[ $# -gt 0 ] && shift
archs=${*:-x86_64 x86_64-musl i686 aarch64 aarch64-musl armv7l armv7l-musl armv6l armv6l-musl}
mkdir -p "$repo"
repo=$(cd "$repo" && pwd)
import_path=github.com/soubarnak/xbpretty

# Map a Void architecture to the Go target. The binary has no cgo, so glibc and musl share it.
goenv() {
	case $1 in
	x86_64 | x86_64-musl) echo "GOARCH=amd64 GOARM=" ;;
	i686) echo "GOARCH=386 GOARM=" ;;
	aarch64 | aarch64-musl) echo "GOARCH=arm64 GOARM=" ;;
	armv7l | armv7l-musl) echo "GOARCH=arm GOARM=7" ;;
	armv6l | armv6l-musl) echo "GOARCH=arm GOARM=6" ;;
	*) echo "unknown architecture $1" >&2; return 1 ;;
	esac
}

make -C "$here" test
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
for arch in $archs; do
	root="$stage/$arch"
	install -d "$root/usr/bin"
	# shellcheck disable=SC2046 # goenv prints KEY=VALUE words on purpose
	env $(goenv "$arch") GOOS=linux CGO_ENABLED=0 \
		go build -C "$here" -trimpath -buildvcs=false -ldflags "-s -w -X $import_path/internal/cli.Version=$version" \
		-o "$root/usr/bin/xbpretty" ./cmd/xbpretty
	# Release gates that must fail on every architecture: static link and the
	# 8,000,000-byte binary budget (FR-076, AC-060).
	"$here/tools/release-gates.sh" binary "$root/usr/bin/xbpretty"
	# The argv[0] aliases: one binary, six names (FR-001).
	for alias in xbp xbp-install xbp-remove xbp-upgrade xbp-autoremove xbp-update; do
		ln -s xbpretty "$root/usr/bin/$alias"
	done
	install -Dm644 "$here/completions/xbpretty.bash" "$root/usr/share/bash-completion/completions/xbpretty"
	install -Dm644 "$here/completions/_xbpretty" "$root/usr/share/zsh/site-functions/_xbpretty"
	install -Dm644 "$here/completions/xbpretty.fish" "$root/usr/share/fish/vendor_completions.d/xbpretty.fish"
	install -Dm644 "$here/man/xbpretty.8" "$root/usr/share/man/man8/xbpretty.8"
	install -Dm644 "$here/man/xbpretty.conf.5" "$root/usr/share/man/man5/xbpretty.conf.5"
	# One man8 alias link per installed name; the page itself is the shared
	# generated xbpretty.8 (FR-076, AC-059).
	ln -s xbpretty.8 "$root/usr/share/man/man8/xbp.8"
	ln -s xbpretty.8 "$root/usr/share/man/man8/xbp-install.8"
	ln -s xbpretty.8 "$root/usr/share/man/man8/xbp-remove.8"
	ln -s xbpretty.8 "$root/usr/share/man/man8/xbp-upgrade.8"
	ln -s xbpretty.8 "$root/usr/share/man/man8/xbp-autoremove.8"
	ln -s xbpretty.8 "$root/usr/share/man/man8/xbp-update.8"
	install -Dm644 "$here/docs/config.toml.example" "$root/usr/share/doc/xbpretty/config.toml.example"
	install -Dm644 "$here/docs/migration-rev6.md" "$root/usr/share/doc/xbpretty/migration-rev6.md"
	install -Dm644 "$here/LICENSE" "$root/usr/share/licenses/xbpretty/LICENSE"

	# The installed surface is exactly the template's: binary, six aliases,
	# three completions, two manpages plus six man8 links, two docs and the
	# licence, with no system configuration path (FR-060, AC-060).
	"$here/tools/release-gates.sh" contents "$root"

	pkg="xbpretty-${version}_${revision}"
	rm -f "$repo/$pkg.$arch.xbps"
	(cd "$repo" && xbps-create -A "$arch" -n "$pkg" \
	    -s "Friendlier front end for the xbps package manager" \
	    -D "xbps>=0.60.0" -l MIT -H https://github.com/soubarnak/xbpretty \
	    -m "soubarnak <soubarna@live.in>" "$root")
	XBPS_ARCH=$arch xbps-rindex -f -a "$repo/$pkg.$arch.xbps"
	# The x86_64 package is the release's size canary (3,600,000 bytes).
	if [ "$arch" = x86_64 ]; then
		"$here/tools/release-gates.sh" package "$repo/$pkg.$arch.xbps"
	fi
	echo "built $repo/$pkg.$arch.xbps"
done
