#!/bin/sh

set -eu

tool=${GO_MUTESTING:-./bin/go-mutesting}
workers=${MUTATION_WORKERS:-6}
baseline=dev/mutation/baseline.json

# go-mutesting's --git-diff-lines parser matches the standard "+++ b/<path>"
# diff header. User-level git config such as diff.mnemonicPrefix or
# diff.noprefix changes that header and silently empties the changed-line
# filter, turning mutation:changed into a no-op. Force the standard prefixes
# for every git invocation the tool makes.
GIT_CONFIG_COUNT=2
GIT_CONFIG_KEY_0=diff.mnemonicprefix
GIT_CONFIG_VALUE_0=false
GIT_CONFIG_KEY_1=diff.noprefix
GIT_CONFIG_VALUE_1=false
export GIT_CONFIG_COUNT GIT_CONFIG_KEY_0 GIT_CONFIG_VALUE_0 GIT_CONFIG_KEY_1 GIT_CONFIG_VALUE_1

# go-mutesting always writes its full default report at the repository root,
# including when no JSON logger is requested. It is intentionally not a
# committed artifact; remove it on success, failure, or interruption so local
# and CI mutation runs cannot leave a multi-megabyte file to be swept up.
cleanup_default_report() {
	rm -f report.json
}
trap cleanup_default_report EXIT HUP INT TERM

runtime_targets() {
	find . -maxdepth 1 -type f -name '*.go' ! -name '*_test.go'
	find internal/codec internal/gf256 internal/matrix internal/rfc6330 \
		-maxdepth 1 -type f -name '*.go' ! -name '*_test.go' ! -name '*_gen.go'
}

case "${1:-}" in
gate)
	"$tool" \
		--noop \
		--workers="$workers" \
		--timeout-coefficient=5 \
		--baseline="$baseline" \
		--fail-on-escaped \
		--min-msi=98 \
		--quiet \
		--no-diffs \
		limits.go \
		payload_id.go \
		internal/gf256/gf256.go \
		internal/rfc6330/partition.go
	;;
audit)
	# Coverage and per-test selection make the broad campaign tractable. The
	# curated gate intentionally omits both so constants and error-only paths
	# remain mutation candidates.
	# shellcheck disable=SC2046
	"$tool" \
		--noop \
		--coverage \
		--per-test \
		--workers="$workers" \
		--timeout-coefficient=5 \
		--baseline="$baseline" \
		--quiet \
		--no-diffs \
		--logger-agentic-json \
		--logger-summary-json \
		$(runtime_targets | sort)
	;;
changed)
	if ! git rev-parse --verify HEAD >/dev/null 2>&1; then
		echo 'mutation:changed requires a committed Git base; use mutation:gate for an uncommitted tree' >&2
		exit 2
	fi
	set -- \
		--noop \
		--coverage \
		--per-test \
		--workers="$workers" \
		--timeout-coefficient=5 \
		--baseline="$baseline" \
		--git-diff-lines \
		--fail-on-escaped \
		--ignore-msi-with-no-mutations \
		--logger-agentic-json \
		--logger-summary-json \
		--git-diff-base="${MUTATION_DIFF_BASE:-HEAD}"
	# shellcheck disable=SC2046
	"$tool" "$@" $(runtime_targets | sort)
	;;
list)
	# shellcheck disable=SC2046
	"$tool" --dry-run $(runtime_targets | sort)
	;;
*)
	echo 'usage: mutation.sh gate|audit|changed|list' >&2
	exit 2
	;;
esac
