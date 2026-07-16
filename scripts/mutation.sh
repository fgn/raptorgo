#!/bin/sh

set -eu

tool=${GO_MUTESTING:-./bin/go-mutesting}
workers=${MUTATION_WORKERS:-6}
baseline=mutation/baseline.json

runtime_targets() {
	find . -maxdepth 1 -type f -name '*.go' ! -name '*_test.go'
	find internal/codec internal/gf256 internal/matrix internal/rfc6330 \
		-maxdepth 1 -type f -name '*.go' ! -name '*_test.go' ! -name '*_gen.go'
}

case "${1:-}" in
gate)
	exec "$tool" \
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
	exec "$tool" \
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
		--logger-summary-json
	if [ -n "${MUTATION_DIFF_BASE:-}" ]; then
		set -- "$@" --git-diff-base="$MUTATION_DIFF_BASE"
	fi
	# shellcheck disable=SC2046
	exec "$tool" "$@" $(runtime_targets | sort)
	;;
list)
	# shellcheck disable=SC2046
	exec "$tool" --dry-run $(runtime_targets | sort)
	;;
*)
	echo 'usage: mutation.sh gate|audit|changed|list' >&2
	exit 2
	;;
esac
