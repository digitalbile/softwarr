-- +goose Up
-- The full error behind a failed request, for admins. error holds the
-- message the requester sees.
ALTER TABLE requests ADD COLUMN error_detail TEXT;

-- +goose Down
ALTER TABLE requests DROP COLUMN error_detail;
