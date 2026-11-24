-- +goose Up
-- Failed requests from before error_detail existed show their full error,
-- which can hold paths, to the requester. Keep it for admins and give the
-- requester the plain message.
UPDATE requests
SET error_detail = error,
    error = 'This request failed. An admin has the details.'
WHERE status = 'failed' AND error_detail IS NULL AND error <> '';

-- +goose Down
UPDATE requests
SET error = error_detail,
    error_detail = NULL
WHERE status = 'failed' AND error = 'This request failed. An admin has the details.';
