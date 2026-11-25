#!/bin/sh
# Runs Opswright with `mygo dev`.
#
# `mygo dev` rebuilds and relaunches the app whenever a source file changes.
# Unless DEV_WATCH=1, it builds a snapshot of the working tree instead, so
# editing files leaves the running app alone; run the script again to pick
# the changes up.
#
# Arguments go to `mygo dev`.
set -eu

cd "$(dirname "$0")/.."

if [ "${DEV_WATCH:-}" != 1 ]; then
	snapshot="${TMPDIR:-/tmp}/opswright-dev"
	rm -rf "$snapshot"
	mkdir -p "$snapshot"
	git ls-files -z -co --exclude-standard | tar -cf - --null -T - | tar -xf - -C "$snapshot"
	cd "$snapshot"
fi

exec go tool mygo dev "$@"
