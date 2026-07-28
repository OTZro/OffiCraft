-- +goose Up
ALTER TABLE task ADD COLUMN handover_note    TEXT NOT NULL DEFAULT '';
ALTER TABLE task ADD COLUMN handover_note_ts REAL NOT NULL DEFAULT 0;
ALTER TABLE task ADD COLUMN handover_note_by TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE task DROP COLUMN handover_note_by;
ALTER TABLE task DROP COLUMN handover_note_ts;
ALTER TABLE task DROP COLUMN handover_note;
