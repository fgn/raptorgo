#!/bin/sh

set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT HUP INT TERM

export RAPTORGO_ORACLE_CACHE=$temporary/cache

# Two cold preparers must converge on one authenticated cache entry.
"$script_dir/prepare-rust-oracle.sh" >"$temporary/first.path" &
first_pid=$!
"$script_dir/prepare-rust-oracle.sh" >"$temporary/second.path" &
second_pid=$!
wait "$first_pid"
wait "$second_pid"
cmp "$temporary/first.path" "$temporary/second.path"
source_dir=$(cat "$temporary/first.path")
test "$(find "$RAPTORGO_ORACLE_CACHE" -mindepth 1 -maxdepth 1 -type d | wc -l)" -eq 1

# Resolution must work from the authenticated cache with neither a registry
# index nor an archive in a fresh Cargo home. Observe the reader lock during a
# clean build so removing the Cargo-side flock fails this regression.
CARGO_HOME=$temporary/cargo-home \
CARGO_TARGET_DIR=$temporary/target \
"$script_dir/cargo-oracle.sh" build --release --locked --offline \
	--manifest-path "$root/dev/interop/rust-oracle/Cargo.toml" \
	>"$temporary/cargo.out" 2>&1 &
cargo_pid=$!
exec 8>"$source_dir.lock"
reader_lock_observed=false
attempt=0
while kill -0 "$cargo_pid" 2>/dev/null && [ "$attempt" -lt 200 ]; do
	if flock -n 8; then
		flock -u 8
	else
		reader_lock_observed=true
		break
	fi
	attempt=$((attempt + 1))
	sleep 0.02
done
if ! wait "$cargo_pid"; then
	cat "$temporary/cargo.out" >&2
	exit 1
fi
if [ "$reader_lock_observed" != true ]; then
	echo "Cargo reader lock was not observed" >&2
	exit 1
fi

# A corrupt published entry must not be removed while a reader holds the lock.
flock 8
printf '\ncorrupt-cache-control\n' >>"$source_dir/src/lib.rs"
"$script_dir/prepare-rust-oracle.sh" >"$temporary/repaired.path" &
repair_pid=$!
sleep 0.1
if ! kill -0 "$repair_pid" 2>/dev/null; then
	echo "corrupt-cache repair did not wait for the reader lock" >&2
	wait "$repair_pid" || true
	exit 1
fi
flock -u 8
wait "$repair_pid"
cmp "$temporary/first.path" "$temporary/repaired.path"
go -C "$root/dev" run ./internal/tooling/verifyoracle "$source_dir" >/dev/null
