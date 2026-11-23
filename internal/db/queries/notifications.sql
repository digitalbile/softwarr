-- name: InsertNotification :one
INSERT INTO notifications (user_id, type, message, request_id, item_id, created_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListNotifications :many
SELECT * FROM notifications
WHERE user_id = ? AND (? = 0 OR read_at IS NULL)
ORDER BY id DESC
LIMIT 100;

-- name: MarkNotificationsRead :exec
UPDATE notifications SET read_at = ?
WHERE user_id = ? AND read_at IS NULL AND id IN (sqlc.slice('ids'));

-- name: MarkAllNotificationsRead :exec
UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at IS NULL;

-- name: ListActiveAdminIDs :many
SELECT id FROM users WHERE is_admin = 1 AND status = 'active' ORDER BY id;

-- name: ListWebhooks :many
SELECT * FROM webhooks ORDER BY id;

-- name: GetWebhook :one
SELECT * FROM webhooks WHERE id = ?;

-- name: CreateWebhook :one
INSERT INTO webhooks (name, url, secret, events, enabled) VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateWebhook :one
UPDATE webhooks SET name = ?, url = ?, secret = ?, events = ?, enabled = ? WHERE id = ?
RETURNING *;

-- name: DeleteWebhook :exec
DELETE FROM webhooks WHERE id = ?;
