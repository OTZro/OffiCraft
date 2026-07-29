-- +goose Up
-- The model a member actually reported while waking is observed runtime state,
-- separate from the owner's next-launch model selection. Empty means no wake
-- report has supplied a model yet.
ALTER TABLE member ADD COLUMN actual_model TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE member DROP COLUMN actual_model;
