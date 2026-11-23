-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = ?;

-- name: ListUsers :many
SELECT * FROM users ORDER BY id;

-- name: CreateUser :one
INSERT INTO users (username, email, password_hash, is_owner, is_admin, auto_approve, status, must_change_password, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateUserFields :exec
UPDATE users SET is_admin = ?, auto_approve = ?, status = ?, email = ? WHERE id = ?;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = ?;

-- name: SetUserPassword :exec
UPDATE users SET password_hash = ?, must_change_password = 0 WHERE id = ?;

-- name: UserHasIdentity :one
SELECT EXISTS (SELECT 1 FROM user_identities WHERE user_id = ?);

-- name: GetOwner :one
SELECT * FROM users WHERE is_owner = 1;
