-- +goose Up
-- The agent's boot report is runtime telemetry, not owner configuration. Keep
-- it separately so an offline member honestly has no reported model.
ALTER TABLE member ADD COLUMN actual_model TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE member DROP COLUMN actual_model;
