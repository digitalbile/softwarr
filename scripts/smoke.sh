#!/usr/bin/env bash
# End-to-end check of the Docker image: setup, login, file ownership,
# privilege drop, password reset, and the non-root (user:) path.
set -euo pipefail

cd "$(dirname "$0")/.."

IMAGE="softwarr:smoke"
UID_GID=1234
SUFFIX="$$-$RANDOM"
CONTAINERS=()
VOLUMES=()
JAR=""

fail() { echo "FAIL: $*" >&2; exit 1; }

cleanup() {
	local c v
	for c in ${CONTAINERS[@]+"${CONTAINERS[@]}"}; do
		docker rm -f "$c" >/dev/null 2>&1 || true
	done
	for v in ${VOLUMES[@]+"${VOLUMES[@]}"}; do
		docker volume rm -f "$v" >/dev/null 2>&1 || true
	done
	[ -n "$JAR" ] && rm -f "$JAR"
	return 0
}
trap cleanup EXIT

# base_url <container> prints http://127.0.0.1:<mapped port>.
base_url() {
	local mapping
	mapping="$(docker port "$1" 7979/tcp | head -n1)"
	echo "http://127.0.0.1:${mapping##*:}"
}

wait_ping() {
	local url="$1" i
	for i in $(seq 1 60); do
		if curl -fsS "$url/api/v1/ping" >/dev/null 2>&1; then return 0; fi
		sleep 1
	done
	return 1
}

echo "Building image"
docker build -q -t "$IMAGE" . >/dev/null

# Main container: root start, drop to PUID/PGID.
# Named volumes keep ownership visible on Docker Desktop, where bind mounts
# can report every file as root, and need no host cleanup.
CONFIG="softwarr-smoke-config-$SUFFIX"
VOLUMES+=("$CONFIG")
docker volume create "$CONFIG" >/dev/null
C1="softwarr-smoke-$SUFFIX"
CONTAINERS+=("$C1")
docker run -d --name "$C1" -p 127.0.0.1::7979 \
	-e PUID=$UID_GID -e PGID=$UID_GID -e UMASK=022 \
	-v "$CONFIG:/config" "$IMAGE" >/dev/null
URL="$(base_url "$C1")"
wait_ping "$URL" || { docker logs "$C1" >&2; fail "softwarr did not answer /api/v1/ping"; }
echo "ping ok"

docker logs "$C1" 2>&1 | grep -q "softwarr running as uid=$UID_GID gid=$UID_GID umask=022" \
	|| fail "startup line missing"

JAR="$(mktemp)"
PASSWORD="smoke-test-password-1"
curl -fsS -b "$JAR" -c "$JAR" -H 'X-Softwarr-CSRF: 1' -H 'Content-Type: application/json' \
	-d "{\"username\":\"owner\",\"password\":\"$PASSWORD\"}" "$URL/api/v1/setup" >/dev/null \
	|| fail "setup failed"
curl -fsS -b "$JAR" -c "$JAR" -H 'X-Softwarr-CSRF: 1' -H 'Content-Type: application/json' \
	-d "{\"username\":\"owner\",\"password\":\"$PASSWORD\"}" "$URL/api/v1/auth/login" >/dev/null \
	|| fail "login failed"
HEALTH="$(curl -fsS -b "$JAR" "$URL/api/v1/system/health")" || fail "system health request failed"
case "$HEALTH" in
	"{"*|"["*) ;;
	*) fail "system health did not return JSON: $HEALTH" ;;
esac
echo "setup, login, health ok"

OWNER="$(docker exec "$C1" stat -c '%u:%g' /config/softwarr.db)"
[ "$OWNER" = "$UID_GID:$UID_GID" ] || fail "softwarr.db owner is $OWNER, want $UID_GID:$UID_GID"
echo "database ownership ok"

PID1_UID="$(docker exec "$C1" awk '/^Uid:/ {print $2}' /proc/1/status)"
[ "$PID1_UID" = "$UID_GID" ] || fail "main process runs as uid $PID1_UID, want $UID_GID"
[ "$PID1_UID" != "0" ] || fail "main process runs as root"
echo "privilege drop ok"

RESET="$(docker exec "$C1" softwarr reset-password)" || fail "reset-password failed"
echo "$RESET" | grep -q "Temporary password:" || fail "reset-password output has no temporary password: $RESET"
BAD="$(docker exec "$C1" sh -c 'for f in /config/softwarr.db*; do stat -c "%n %u:%g" "$f"; done' | grep -v " $UID_GID:$UID_GID\$" || true)"
[ -z "$BAD" ] || fail "database files with wrong owner after reset-password: $BAD"
echo "reset-password ok"

# Second container: started with --user, so the entrypoint skips the drop.
CONFIG2="softwarr-smoke-config2-$SUFFIX"
VOLUMES+=("$CONFIG2")
docker volume create "$CONFIG2" >/dev/null
docker run --rm -v "$CONFIG2:/x" alpine:3.24 chown $UID_GID:$UID_GID /x >/dev/null
C2="softwarr-smoke-user-$SUFFIX"
CONTAINERS+=("$C2")
docker run -d --name "$C2" -p 127.0.0.1::7979 --user $UID_GID:$UID_GID \
	-v "$CONFIG2:/config" "$IMAGE" >/dev/null
URL2="$(base_url "$C2")"
wait_ping "$URL2" || { docker logs "$C2" >&2; fail "non-root container did not answer /api/v1/ping"; }
echo "non-root ping ok"

echo "smoke test passed"
