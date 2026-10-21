#!/bin/sh
# The [[build]] step of `herdr plugin install`. `herdr plugin link` does not run
# it, so a local checkout uses `make build`.
set -eu

cd "$(dirname "$0")/.."
name=herdr-cmd-status
base=${HERDR_CMD_STATUS_BASE_URL:-https://github.com/janosmiko/herdr-cmd-status/releases/download}
version=$(sed -n 's/^version = "\(.*\)"/\1/p' herdr-plugin.toml | head -n 1)

say() { printf '%s: %s\n' "$name" "$*" >&2; }

fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		say "no curl or wget to download the release"
		return 1
	fi
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d ' ' -f 1
	else
		shasum -a 256 "$1" | cut -d ' ' -f 1
	fi
}

download() {
	case $(uname -s) in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) return 1 ;;
	esac
	case $(uname -m) in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) return 1 ;;
	esac
	asset=$name-$os-$arch
	mkdir -p bin || return 1
	# Inside bin/, so the final mv is a rename that cannot leave half a binary behind.
	tmp=$(mktemp -d bin/.download.XXXXXX) || return 1
	trap 'rm -rf "$tmp"' EXIT
	if ! fetch "$base/v$version/$asset" "$tmp/$asset" || ! fetch "$base/v$version/SHA256SUMS" "$tmp/SHA256SUMS"; then
		say "cannot download $asset of v$version"
		return 1
	fi
	want=$(awk -v asset="$asset" '$2 == asset { print $1 }' "$tmp/SHA256SUMS")
	if [ -z "$want" ] || [ "$(sha256 "$tmp/$asset")" != "$want" ]; then
		say "checksum of $asset does not match SHA256SUMS"
		return 1
	fi
	# download runs on the left of ||, where set -e does not stop on a failed step.
	chmod +x "$tmp/$asset" && mv "$tmp/$asset" "bin/$name"
}

build() {
	if ! command -v go >/dev/null 2>&1; then
		say "no release binary for this machine, and no Go to build from source (https://go.dev/dl)"
		exit 1
	fi
	say "building from source"
	CGO_ENABLED=0 go build -trimpath -o "bin/$name" .
}

download || build
