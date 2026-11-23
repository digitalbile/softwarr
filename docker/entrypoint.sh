#!/bin/sh
set -eu

PUID="${PUID:-1000}"
PGID="${PGID:-1000}"
UMASK="${UMASK:-022}"
CONFIG_DIR="${SOFTWARR_DATA_DIR:-/config}"

if [ "$(id -u)" = "0" ]; then
	# Reuse a group or user that already has the requested ID.
	group="$(getent group "$PGID" | cut -d: -f1 || true)"
	if [ -z "$group" ]; then
		addgroup -g "$PGID" softwarr
		group=softwarr
	fi
	if ! getent passwd "$PUID" >/dev/null; then
		adduser -D -H -u "$PUID" -G "$group" softwarr
	fi

	mkdir -p "$CONFIG_DIR"
	if [ "$(stat -c '%u:%g' "$CONFIG_DIR")" != "$PUID:$PGID" ]; then
		chown -R "$PUID:$PGID" "$CONFIG_DIR"
	fi
	run_uid="$PUID"
	run_gid="$PGID"
else
	run_uid="$(id -u)"
	run_gid="$(id -g)"
fi

umask "$UMASK"
echo "softwarr running as uid=$run_uid gid=$run_gid umask=$UMASK"
export PUID PGID UMASK
exec /usr/local/bin/softwarr "$@"
