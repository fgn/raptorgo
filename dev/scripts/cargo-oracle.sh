#!/bin/sh

set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
source_dir=$("$script_dir/prepare-rust-oracle.sh")

newline='
'
carriage_return=$(printf '\r')
case "$source_dir" in
*'"'* | *\\* | *"$newline"* | *"$carriage_return"*)
	echo "Rust oracle cache path cannot be represented safely in Cargo configuration" >&2
	exit 1
	;;
esac

exec 9>"$source_dir.lock"
flock 9
go -C "$root/dev" run ./internal/tooling/verifyoracle "$source_dir" >/dev/null
exec cargo --config "patch.crates-io.raptorq.path=\"$source_dir\"" "$@"
