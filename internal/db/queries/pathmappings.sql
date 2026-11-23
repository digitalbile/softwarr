-- name: ListPathMappings :many
SELECT * FROM path_mappings ORDER BY id;

-- name: CreatePathMapping :one
INSERT INTO path_mappings (host, remote_path, local_path)
VALUES (?, ?, ?)
RETURNING *;

-- name: UpdatePathMapping :one
UPDATE path_mappings
SET host = ?, remote_path = ?, local_path = ?
WHERE id = ?
RETURNING *;

-- name: DeletePathMapping :execrows
DELETE FROM path_mappings WHERE id = ?;
