-- +goose Up
-- T-52917b — 遞增票號. The counter new task ids are minted from.
--
-- ONE ROW, FOREVER. The CHECK on the primary key is what makes that a schema
-- fact rather than a convention: there is no second row for a writer to read the
-- wrong one of, and `INSERT OR IGNORE` on a re-run cannot quietly add one.
--
-- `next` is the number the NEXT task will be called — T-<next> — so a fresh
-- database mints T-1. It is claimed with a compare-and-set:
--
--     SELECT next FROM task_id_seq WHERE id = 1;          -- v
--     UPDATE task_id_seq SET next = next + 1 WHERE id = 1 AND next = v;
--
-- 1 row affected ⇒ v is MINE. 0 rows ⇒ somebody else took it; re-read and try
-- again. The claim runs inside the SAME transaction as the task INSERT, so a
-- crash between the two rolls BOTH back and the number is not burned.
--
-- 🔴 THE PROPERTY IS UNIQUENESS, NOT CONTIGUITY. A rolled-back create (the
-- outsource spawn gate denying, a failed insert) returns its number, and an
-- externally-advanced counter skips some — both are fine. A GAP hurts nobody;
-- two tasks called T-7 corrupts the system, because task.id is a TEXT PRIMARY
-- KEY written through an upsert, so the second T-7 does not error, it OVERWRITES
-- the first one and the API still answers 200.
--
-- Existing tasks keep their "t-" + 12-random-hex ids untouched. Nothing migrates
-- them: task.id has no CHECK, no COLLATE and no length limit, GetTask is a
-- byte-exact `WHERE id = ?`, and no table has a foreign key into task (0 hits
-- for `REFERENCES task` across every migration), so the two formats coexist with
-- no conversion and no dual-read path. "T-1" and "t-…" cannot collide either —
-- the comparison is binary, and a hex id is never a bare decimal.
CREATE TABLE task_id_seq (
    id   INTEGER PRIMARY KEY CHECK (id = 1),
    next INTEGER NOT NULL
);

-- Seeded ABOVE any T-<n> id that somehow already exists (it should not — this
-- format is new — but seeding from the data costs one scan once and removes the
-- question). COALESCE handles the empty table and the all-legacy-ids table
-- alike. The CAST is what makes '10' sort after '9'.
INSERT INTO task_id_seq (id, next)
SELECT 1, COALESCE(MAX(CAST(SUBSTR(id, 3) AS INTEGER)), 0) + 1
FROM task
WHERE id GLOB 'T-[0-9]*';

-- +goose Down
DROP TABLE task_id_seq;
