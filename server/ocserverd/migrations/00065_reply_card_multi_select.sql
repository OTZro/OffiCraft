-- +goose Up
-- T-40: the reply card stops being single-select and stops encoding "which one
-- did the AI suggest" as a POSITION. Three moves, one rebuild (SQLite cannot
-- change a column's type or nullability in place — the 00013 precedent):
--
--   1. options: a JSON array of STRINGS becomes a JSON array of OBJECTS
--      [{"text": ..., "ai_pick": true|false}]. Until now "options[0] is the AI
--      recommendation" was a convention written in prose that NOTHING in the
--      code executed; ai_pick is that fact made into a real field, and position
--      stops meaning anything.
--   2. select_mode: new column, 'single' | 'multi' (default 'single'). It is a
--      SEPARATE axis from kind — kind says what the owner must DO, select_mode
--      says how many options the answer may carry — so kind keeps its CHECK and
--      is not touched.
--   3. answer_option_idx INTEGER (one index or NULL) becomes
--      answer_option_idxs TEXT (a JSON array of indices, or NULL when the card
--      was answered with text/attachments only, or is not answered yet).
--
-- Data carry-over. Existing rows are rewritten under the OLD convention, which
-- is the one and only time it is ever executed: options[0] becomes
-- ai_pick=true and every other option ai_pick=false. ["甲","乙"] therefore
-- becomes [{"text":"甲","ai_pick":true},{"text":"乙","ai_pick":false}]; a card
-- with no options stays []. answer_option_idx 3 becomes [3]; NULL stays NULL.
-- select_mode backfills to 'single' for every existing row — which is exactly
-- what every existing card was.
--
-- reply_card carries no FKs and one index, so the rebuild is a plain
-- create/copy/drop/rename.
CREATE TABLE reply_card_rebuild (
    id                 TEXT PRIMARY KEY,
    from_member        TEXT NOT NULL DEFAULT '',
    kind               TEXT NOT NULL CHECK (kind IN ('decision', 'action')),
    summary            TEXT NOT NULL DEFAULT '',
    body               TEXT NOT NULL DEFAULT '',
    -- JSON array of 1..4 {"text": ..., "ai_pick": bool} objects. ai_pick is
    -- the ONLY carrier of "this is the AI's recommendation"; position is not.
    options            TEXT NOT NULL DEFAULT '[]',
    -- 'single' (at most one circled option, at most one ai_pick) | 'multi'
    -- (any number of both). Closed set in code, no CHECK — same posture as
    -- status since 00013.
    select_mode        TEXT NOT NULL DEFAULT 'single',
    status             TEXT NOT NULL DEFAULT 'waiting',
    created_ts         REAL NOT NULL DEFAULT 0.0,
    answered_ts        REAL NOT NULL DEFAULT 0.0,
    expired_ts         REAL NOT NULL DEFAULT 0.0,
    chat_message_id    TEXT NOT NULL DEFAULT '',
    -- JSON array of indices into options, deduped + ascending; NULL when the
    -- answer carried no option at all.
    answer_option_idxs TEXT,
    answer_text        TEXT NOT NULL DEFAULT '',
    answer_attachments TEXT NOT NULL DEFAULT '[]',
    attachments        TEXT NOT NULL DEFAULT '[]',
    task_id            TEXT NOT NULL DEFAULT '',
    task_step_id       TEXT NOT NULL DEFAULT ''
);
INSERT INTO reply_card_rebuild (id, from_member, kind, summary, body, options,
    select_mode, status, created_ts, answered_ts, expired_ts, chat_message_id,
    answer_option_idxs, answer_text, answer_attachments, attachments,
    task_id, task_step_id)
  SELECT id, from_member, kind, summary, body,
    (SELECT json_group_array(json_object(
        'text', j.value,
        'ai_pick', json(CASE WHEN j.key = 0 THEN 'true' ELSE 'false' END)))
       FROM json_each(reply_card.options) AS j),
    'single', status, created_ts, answered_ts, expired_ts, chat_message_id,
    CASE WHEN answer_option_idx IS NULL THEN NULL
         ELSE json_array(answer_option_idx) END,
    answer_text, answer_attachments, attachments, task_id, task_step_id
  FROM reply_card;
DROP TABLE reply_card;
ALTER TABLE reply_card_rebuild RENAME TO reply_card;
CREATE INDEX idx_reply_card_status ON reply_card (status);

-- +goose Down
-- ⚠️ THIS ROLLBACK IS LOSSY, AND SILENTLY SO. The old schema holds ONE answer
-- index per card, so a MULTI-SELECT answer cannot survive it: only the FIRST
-- (lowest) circled index is kept and every other circled option is DROPPED on
-- the floor with no record anywhere that it was ever chosen. A card whose
-- owner circled [0,2] rolls back looking exactly like a card whose owner
-- circled [0] alone. Rolling forward again does NOT bring the lost indices
-- back. select_mode is dropped outright, so 'multi' cards come back
-- indistinguishable from 'single' ones.
--
-- The options column loses ai_pick the same way: the objects flatten back to
-- their text strings, and the "which one did the AI suggest" fact survives ONLY
-- where it happens to coincide with position 0. An ai_pick on any other option
-- is gone; a card that marked no option at all comes back looking like one
-- whose first option was the AI pick, because under the old schema every card
-- claimed that.
CREATE TABLE reply_card_rebuild (
    id                 TEXT PRIMARY KEY,
    from_member        TEXT NOT NULL DEFAULT '',
    kind               TEXT NOT NULL CHECK (kind IN ('decision', 'action')),
    summary            TEXT NOT NULL DEFAULT '',
    body               TEXT NOT NULL DEFAULT '',
    options            TEXT NOT NULL DEFAULT '[]',
    status             TEXT NOT NULL DEFAULT 'waiting',
    created_ts         REAL NOT NULL DEFAULT 0.0,
    answered_ts        REAL NOT NULL DEFAULT 0.0,
    expired_ts         REAL NOT NULL DEFAULT 0.0,
    chat_message_id    TEXT NOT NULL DEFAULT '',
    answer_option_idx  INTEGER,
    answer_text        TEXT NOT NULL DEFAULT '',
    answer_attachments TEXT NOT NULL DEFAULT '[]',
    attachments        TEXT NOT NULL DEFAULT '[]',
    task_id            TEXT NOT NULL DEFAULT '',
    task_step_id       TEXT NOT NULL DEFAULT ''
);
INSERT INTO reply_card_rebuild (id, from_member, kind, summary, body, options,
    status, created_ts, answered_ts, expired_ts, chat_message_id,
    answer_option_idx, answer_text, answer_attachments, attachments,
    task_id, task_step_id)
  SELECT id, from_member, kind, summary, body,
    (SELECT json_group_array(json_extract(j.value, '$.text'))
       FROM json_each(reply_card.options) AS j),
    status, created_ts, answered_ts, expired_ts, chat_message_id,
    -- lossy: only the first circled index survives (see the note above).
    CASE WHEN answer_option_idxs IS NULL THEN NULL
         ELSE json_extract(answer_option_idxs, '$[0]') END,
    answer_text, answer_attachments, attachments, task_id, task_step_id
  FROM reply_card;
DROP TABLE reply_card;
ALTER TABLE reply_card_rebuild RENAME TO reply_card;
CREATE INDEX idx_reply_card_status ON reply_card (status);
