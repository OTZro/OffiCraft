-- +goose Up
-- Per-agent AI CLI runtime. Constant defaults preserve every existing row and
-- every older client that omits the new field.
ALTER TABLE member ADD COLUMN runtime TEXT NOT NULL DEFAULT 'claude';
ALTER TABLE task ADD COLUMN outsource_runtime TEXT NOT NULL DEFAULT 'claude';

-- +goose Down
ALTER TABLE task DROP COLUMN outsource_runtime;
ALTER TABLE member DROP COLUMN runtime;
