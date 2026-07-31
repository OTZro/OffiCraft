-- +goose Up
-- The runtime and effort an agent REPORTS it is running under are telemetry, not
-- owner configuration — the same split actual_model already makes. They lived
-- only in the in-memory telemetry store, so a server restart blanked them
-- fleet-wide and no read path could tell a configured value from a running one.
ALTER TABLE member ADD COLUMN actual_runtime TEXT NOT NULL DEFAULT '';
ALTER TABLE member ADD COLUMN actual_effort TEXT NOT NULL DEFAULT '';

-- Which owner operation opened the wind-down window refocus_since stamps. The
-- cause used to live only in a log line, so a client could say "last refocus"
-- (reads as history) but never "winding down right now so your change applies".
ALTER TABLE member ADD COLUMN refocus_op TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE member DROP COLUMN refocus_op;
ALTER TABLE member DROP COLUMN actual_effort;
ALTER TABLE member DROP COLUMN actual_runtime;
