-- +goose Up
-- T-49e7 自訂頻率: the `custom` cadence — day/hour/minute multi-select sets whose
-- INTERSECTION names every wall-clock reading the schedule fires at. It is the
-- first cadence that can fire more than once a day, which is the whole point:
-- owner's "every 20 minutes" (card rc-4acc4013a0ae) is expressible as
-- custom_minutes {0,20,40} with every hour and every day listed, and the
-- alternative — seventy-two separate schedules a day — was not a real option.
--
-- Two schema moves, one rebuild:
--   1. DROP the DB-level cadence CHECK. The closed set now lives ONLY in code
--      (domain.ValidScheduledMessageCadence), so a future cadence costs zero
--      schema churn. SQLite cannot DROP a CHECK in place, hence the
--      create/copy/swap rebuild. This is the 00011/00016 template verbatim
--      (task.status and task_step.status both moved their whitelists to Go the
--      same way, owner-approved design point 4 in 00011); scheduled_message
--      carries no FKs either, so the rebuild is a plain create/copy/drop/rename.
--      `status` keeps its CHECK — only the CADENCE whitelist moved to code.
--   2. ADD the three set columns. They are TEXT holding a canonical
--      comma-joined list of decimal integers, sorted ascending and deduplicated
--      ('0,20,40'), '' when the cadence is not `custom`.
--
-- 🔴 Why sorted-and-deduplicated is a STORAGE invariant and not a formatting
-- preference: the PATCH re-aim test compares the supplied value against the
-- stored one field by field, and a difference moves the delivery cursor. A
-- cockpit that posts the whole form back on every save would therefore re-aim —
-- and SWALLOW the crossed delivery — merely because the user's checkbox order
-- produced '20,0,40' this time. Canonicalising on write is what keeps "same
-- choice" and "same bytes" the same question. The writer canonicalises; nothing
-- else in the system is allowed to write these columns raw.
--
-- 🔴 Why an EMPTY set is refused at write time (422) rather than read as "all"
-- or as "never": those two readings sit one keystroke apart and are
-- indistinguishable on screen. "Every day" is expressed by LISTING every day —
-- the same explicit-set rule the cockpit's 全選 button produces. An empty set
-- reaching this table means a writer bypassed the domain validator, so the
-- column default '' is only ever the not-custom marker, never a live custom row.
--
-- Existing daily/weekly/monthly rows are copied column for column and read
-- exactly as before: they never look at these three columns, and the three
-- columns are '' for them. This migration moves no data.
CREATE TABLE scheduled_message_rebuild (
    id              TEXT PRIMARY KEY,
    member_id       TEXT NOT NULL,
    label           TEXT NOT NULL DEFAULT '',
    body            TEXT NOT NULL,
    -- the closed set is enforced in code now (domain.ValidScheduledMessageCadence);
    -- no CHECK. Today: daily / weekly / monthly / custom.
    cadence         TEXT NOT NULL,
    -- weekly reads day_of_week (0=Sunday..6=Saturday); monthly reads
    -- day_of_month (1-31, a month lacking the day is SKIPPED entirely per
    -- iCalendar RFC 5545, never clamped); daily reads neither; custom reads
    -- NONE of these four and reads the three custom_* sets instead.
    day_of_week     INTEGER NOT NULL DEFAULT 0,
    day_of_month    INTEGER NOT NULL DEFAULT 1,
    hour            INTEGER NOT NULL DEFAULT 0,
    minute          INTEGER NOT NULL DEFAULT 0,
    -- canonical comma-joined sets for `custom`, sorted ascending and
    -- deduplicated; '' for every other cadence. See the header for why the
    -- canonical form is an invariant and why '' can never be a live custom row.
    custom_days     TEXT NOT NULL DEFAULT '',
    custom_hours    TEXT NOT NULL DEFAULT '',
    custom_minutes  TEXT NOT NULL DEFAULT '',
    -- IANA name; the wall clock is ALWAYS read in this zone, never the host's.
    timezone        TEXT NOT NULL DEFAULT 'Asia/Taipei',
    -- the revocation toggle, NOT a lifecycle: DELETE is the permanent removal.
    status          TEXT NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled', 'disabled')),
    last_fired_slot TEXT NOT NULL DEFAULT '',   -- the slot IDENTIFIER (see 00050)
    last_fired_ts   REAL NOT NULL DEFAULT 0.0,  -- human-facing only
    created_ts      REAL NOT NULL DEFAULT 0.0
);
INSERT INTO scheduled_message_rebuild (id, member_id, label, body, cadence,
    day_of_week, day_of_month, hour, minute, timezone, status,
    last_fired_slot, last_fired_ts, created_ts)
  SELECT id, member_id, label, body, cadence,
    day_of_week, day_of_month, hour, minute, timezone, status,
    last_fired_slot, last_fired_ts, created_ts FROM scheduled_message;
DROP TABLE scheduled_message;
ALTER TABLE scheduled_message_rebuild RENAME TO scheduled_message;
CREATE INDEX idx_scheduled_message_member ON scheduled_message (member_id);

-- +goose Down
-- Reverse: drop the three set columns and restore the cadence CHECK (another
-- rebuild). `custom` will not exist after rollback, so any such row has to
-- become something the CHECK accepts.
--
-- 🔴 It is squashed to `daily` AND FORCED TO `disabled`, and the second half is
-- the load-bearing one. A custom row carries no meaningful hour/minute — those
-- are the fields it does not read, so they hold their 0/0 defaults — and
-- squashing to `daily` alone would hand the older binary a live schedule that
-- fires at midnight in the recipient's zone. That is a WRONG delivery at a time
-- nobody chose, and it is silent: the row looks like an ordinary daily schedule
-- and nothing anywhere records that it used to mean something else. Rolling
-- back therefore stops those schedules rather than re-aiming them: `disabled`
-- is visible in the cockpit and reversible by hand, whereas a midnight
-- delivery is neither. The set columns are dropped with the table, so the
-- choice itself IS lost on rollback — this Down is lossy by construction and
-- says so rather than pretending otherwise.
CREATE TABLE scheduled_message_rebuild (
    id              TEXT PRIMARY KEY,
    member_id       TEXT NOT NULL,
    label           TEXT NOT NULL DEFAULT '',
    body            TEXT NOT NULL,
    cadence         TEXT NOT NULL CHECK (cadence IN ('daily', 'weekly', 'monthly')),
    day_of_week     INTEGER NOT NULL DEFAULT 0,
    day_of_month    INTEGER NOT NULL DEFAULT 1,
    hour            INTEGER NOT NULL DEFAULT 0,
    minute          INTEGER NOT NULL DEFAULT 0,
    timezone        TEXT NOT NULL DEFAULT 'Asia/Taipei',
    status          TEXT NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled', 'disabled')),
    last_fired_slot TEXT NOT NULL DEFAULT '',
    last_fired_ts   REAL NOT NULL DEFAULT 0.0,
    created_ts      REAL NOT NULL DEFAULT 0.0
);
INSERT INTO scheduled_message_rebuild (id, member_id, label, body, cadence,
    day_of_week, day_of_month, hour, minute, timezone, status,
    last_fired_slot, last_fired_ts, created_ts)
  SELECT id, member_id, label, body,
    CASE WHEN cadence = 'custom' THEN 'daily' ELSE cadence END,
    day_of_week, day_of_month, hour, minute, timezone,
    CASE WHEN cadence = 'custom' THEN 'disabled' ELSE status END,
    last_fired_slot, last_fired_ts, created_ts FROM scheduled_message;
DROP TABLE scheduled_message;
ALTER TABLE scheduled_message_rebuild RENAME TO scheduled_message;
CREATE INDEX idx_scheduled_message_member ON scheduled_message (member_id);
