#!/bin/sh

set -eu

version=2.0.1
package_sha256=4d4215fb79ef19442a0c71616aabb0715a386e6a16ed9031775ee3e3f20e7502
archive_url=https://static.crates.io/crates/raptorq/raptorq-2.0.1.crate

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
patch_file=$root/dev/interop/patches/raptorq-2.0.1-oracle.patch
manifest_file=$root/dev/interop/patches/raptorq-2.0.1-checksums.json
cache_root=${RAPTORGO_ORACLE_CACHE:-${XDG_CACHE_HOME:-${HOME:?HOME is required}/.cache}/raptorgo/oracles}
destination=$cache_root/raptorq-$version-$package_sha256

verify() {
	go -C "$root/dev" run ./internal/tooling/verifyoracle "$1"
}

mkdir -p -- "$cache_root"
lock_file=$destination.lock
exec 9>"$lock_file"
flock 9

if [ -d "$destination" ]; then
	if verify "$destination" >/dev/null 2>&1; then
		printf '%s\n' "$destination"
		exit 0
	fi
	# This is a generated, content-addressed cache entry. Rebuild corrupt or
	# partially written entries instead of allowing Cargo to consume them.
	rm -rf -- "$destination"
fi

temporary=$(mktemp -d "$cache_root/.raptorq-$version.XXXXXX")
trap 'rm -rf -- "$temporary"' EXIT HUP INT TERM
archive=$temporary/raptorq-$version.crate

curl -A 'raptorgo-oracle-fetch/1.0' -fsSL "$archive_url" -o "$archive"
echo "$package_sha256  $archive" | sha256sum -c - >/dev/null
tar -xzf "$archive" -C "$temporary"
source_dir=$temporary/raptorq-$version
# cargo vendor omits the crate archive's VCS-only ignore file. Remove it so
# the authenticated tree exactly matches the pinned Cargo checksum manifest.
rm -- "$source_dir/.gitignore"
cp -- "$manifest_file" "$source_dir/.cargo-checksum.json"
(cd "$source_dir" && git apply "$patch_file")
verify "$source_dir" >/dev/null

if mv -T -- "$source_dir" "$destination" 2>/dev/null; then
	:
elif [ -d "$destination" ] && verify "$destination" >/dev/null; then
	# A concurrent preparer installed the same authenticated cache entry.
	:
else
	echo "could not install authenticated Rust oracle at $destination" >&2
	exit 1
fi

printf '%s\n' "$destination"
