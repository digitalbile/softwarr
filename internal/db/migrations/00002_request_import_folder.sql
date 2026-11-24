-- +goose Up
-- The library folder an import claimed, saved before files are placed so a
-- retried import reuses it instead of making a numbered copy.
ALTER TABLE requests ADD COLUMN import_folder TEXT;

-- +goose Down
ALTER TABLE requests DROP COLUMN import_folder;
