#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
jar="$root/bin/tla2tools-v1.8.0.jar"

if [ -n "${TLC_JAVA:-}" ]; then
    java_bin=$TLC_JAVA
else
    java_bin=$(command -v java || true)
fi

if [ -z "$java_bin" ] || [ ! -x "$java_bin" ]; then
    echo "tlc: Java 17 or newer is required; set TLC_JAVA to its executable" >&2
    exit 1
fi

version=$($java_bin -version 2>&1 | sed -n '1s/.*version "\([^"]*\)".*/\1/p')
case "$version" in
    1.*) major=$(printf '%s\n' "$version" | cut -d. -f2) ;;
    *) major=$(printf '%s\n' "$version" | cut -d. -f1) ;;
esac
case "$major" in
    ''|*[!0-9]*)
        echo "tlc: could not determine Java version from $version" >&2
        exit 1
        ;;
esac
if [ "$major" -lt 17 ]; then
    echo "tlc: Java 17 or newer is required; found $version" >&2
    exit 1
fi

metadir=$(mktemp -d /tmp/raptorgo-tlc.XXXXXX)
trap 'rm -rf "$metadir"' 0

"$java_bin" ${TLC_JAVA_OPTS:--XX:+UseParallelGC} -cp "$jar" tlc2.TLC -metadir "$metadir" "$@"
