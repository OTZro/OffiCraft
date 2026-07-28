-- +goose Up
ALTER TABLE task ADD COLUMN handover_notes TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE task DROP COLUMN handover_notes;
