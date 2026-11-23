-- name: CreateOIDCState :exec
INSERT INTO oidc_states (state, code_verifier, nonce, link_user_id, expires_at) VALUES (?, ?, ?, ?, ?);

-- name: ConsumeOIDCState :one
DELETE FROM oidc_states WHERE state = ? RETURNING *;

-- name: DeleteExpiredOIDCStates :exec
DELETE FROM oidc_states WHERE expires_at <= ?;

-- name: GetIdentity :one
SELECT * FROM user_identities WHERE issuer = ? AND subject = ?;

-- name: CreateIdentity :exec
INSERT INTO user_identities (user_id, issuer, subject, created_at) VALUES (?, ?, ?, ?);

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ?;

-- name: SetUserAdminFromClaim :exec
UPDATE users SET is_admin = ? WHERE id = ? AND is_owner = 0;
