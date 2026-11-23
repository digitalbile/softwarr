-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?);

-- name: GetSession :one
SELECT * FROM sessions WHERE token_hash = ?;

-- name: ExtendSession :exec
UPDATE sessions SET expires_at = ? WHERE token_hash = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = ?;

-- name: DeleteSessionsForUser :exec
DELETE FROM sessions WHERE user_id = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= ?;
