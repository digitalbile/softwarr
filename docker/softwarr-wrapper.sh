#!/bin/sh
# Runs softwarr as the PUID:PGID user. Installed as /usr/local/bin/softwarr so
# that `docker exec <container> softwarr reset-password` (which runs as root)
# doesn't leave database files the app can't write.
set -eu

umask "${UMASK:-022}"

if [ "$(id -u)" = "0" ]; then
	exec su-exec "${PUID:-1000}:${PGID:-1000}" /app/softwarr "$@"
fi
exec /app/softwarr "$@"
