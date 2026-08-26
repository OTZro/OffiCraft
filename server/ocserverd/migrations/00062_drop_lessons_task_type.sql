-- +goose Up
-- T-2 step B — 砍欄位. 00061 removed every non-general `lessons` ROW; this
-- removes the `task_type` COLUMN itself, so the classification name stops
-- existing on the wire, in the tool descriptions, and in storage.
--
-- 🔴 THE MECHANISM IS A REBUILD, NOT `ALTER TABLE ... DROP COLUMN`, AND THE
-- REASON IS MEASURED, NOT STYLISTIC. task_type is half of the composite
-- PRIMARY KEY (00001_schema.sql). SQLite REFUSES to drop such a column
-- outright — `ALTER TABLE lessons DROP COLUMN task_type` answers
--   Error: in prepare, cannot drop PRIMARY KEY column: "task_type"
-- and changes nothing. (Run against sqlite3 on the machine that wrote this,
-- 2026-08-27.) So the create/copy/drop/rename rebuild below is the only shape
-- that can do this at all, and it is also where the key fold actually happens.
-- Anyone carrying the belief that the drop "silently folds duplicate keys"
-- should read the next paragraph: neither door is silent.
--
-- 🔴 THE COPY IS A PLAIN `INSERT`, DELIBERATELY — NOT `INSERT OR REPLACE`,
-- NOT `OR IGNORE`. Collapsing (role_key, task_type) onto role_key alone is an
-- identity fold ONLY IF each role_key already has at most one row, which is
-- exactly what 00061 established. If that is ever untrue on some station —
-- 00061 skipped, a row written by an older binary in between, a hand-edited
-- database — the plain INSERT stops the migration with
--   UNIQUE constraint failed: lessons_rebuild.role_key
-- and NOTHING is dropped, because goose runs each migration in a transaction.
-- `OR REPLACE` / `OR IGNORE` would answer 0 and quietly keep whichever row
-- happened to be last / first, which is the lossy fold this whole two-step
-- ordering exists to avoid. Both directions were measured on a fixture before
-- this was written (see TestMigration00062*).
CREATE TABLE lessons_rebuild (
    role_key   TEXT PRIMARY KEY,
    text       TEXT NOT NULL DEFAULT '',
    tombstoned INTEGER NOT NULL DEFAULT 0
);
INSERT INTO lessons_rebuild (role_key, text, tombstoned)
  SELECT role_key, text, tombstoned FROM lessons;
DROP TABLE lessons;
ALTER TABLE lessons_rebuild RENAME TO lessons;

-- 🔴 THE SECOND TABLE — document_history. A lessons revision is addressed by
-- `<role_key>::<task_type>` (the key api_document_history.go's historyKeyParts
-- splits on the FIRST "::"). With the axis gone the key is the bare role_key,
-- so every retained lessons revision has to be re-addressed or it becomes
-- unreachable: list_document_history would ask for key "assistant" and the
-- rows would still be filed under "assistant::general". Leaving them would not
-- error anywhere — it would just quietly lose the owner's undo path, which is
-- the failure mode this repo cares about most.
--
-- 🔴 THE PREDICATE MIRRORS THE PARSE, exactly as 00061's did. The suffix after
-- the FIRST "::" must be precisely 'general': that is what historyKeyParts
-- would have handed to the restore, and after 00061 it is the only task_type
-- any restorable lessons revision can carry. Rewriting is therefore a rename,
-- not a merge — no two rewritten keys can collide on content that mattered,
-- and document_history has no UNIQUE constraint on (kind, key) in any case
-- (00043_document_history.sql: an append-only journal keyed by autoincrement
-- id), so the UPDATE cannot fail on a duplicate.
--
-- 🔴 WHAT IS DELIBERATELY LEFT ALONE, so it is not read as an oversight: the
-- three malformed shapes 00061 also spared — a key with no "::" at all, one
-- with an empty role side ('::general'), one with an empty task side
-- ('assistant::'). Live writers cannot produce any of them (both writers build
-- the key from two non-empty path segments), 00061 states the same, and this
-- is a rewrite rather than a delete precisely so that a row nobody can explain
-- is not silently renamed into something that looks explicable.
--
-- 🔴 AND THE PREMISE THAT MAKES SPARING THEM SAFE IS NOT INHERITED — IT IS
-- RE-ESTABLISHED HERE. 00061's argument for leaving them was "the list/restore
-- door refuses such a key with 400 before any restore runs". That door was
-- historyKeyParts' two-halves parse, and THIS ticket removes that parse. Simply
-- copying 00061's sentence would have been copying a premise this very
-- migration invalidates — and a first cut of T-2 did exactly that, reducing
-- historyKeyParts to "the key is non-empty" and turning `assistant::` into a
-- key that could be listed and RESTORED, with the restore materialising a
-- `lessons` row under a name no role carries. So the refusal is stated FORWARD
-- rather than inherited: since T-2 a lessons key is the bare role_key, one
-- carrying "::" names nothing, and historyKeyParts refuses it outright. Two of
-- the three shapes above are therefore still unreachable for that reason. The
-- third — a key with no "::" at all — is now a well-FORMED key that simply
-- names no role; it lists as empty and its restore lands under a role_key
-- nobody answers to, which is the roster gap peek_doc_sizes' summary describes
-- and which is not this migration's to close. Pinned in
-- api_document_history_lessons_key_t2_test.go, positive case first.
UPDATE document_history
   SET document_key = substr(document_key, 1, instr(document_key, '::') - 1)
 WHERE document_kind = 'lessons'
   AND instr(document_key, '::') > 1
   AND substr(document_key, instr(document_key, '::') + 2) = 'general';

-- +goose Down
-- REVERSIBLE — a real Down is written here instead of the `SELECT 1` no-op
-- 00061 had to use, because 00061 was irreversible for a reason this migration
-- does not share: it DELETED rows and kept no copy. This migration deletes
-- nothing. But "reversible" is exact on one table and approximate on the other,
-- and the two are separated here rather than averaged into one claim.
--
-- 🔴 ON `lessons` IT IS EXACT. After 00061 every surviving row carried
-- task_type = 'general', so putting the constant back reconstructs the pre-Up
-- state rather than synthesising a plausible-looking one. Rows written by the
-- NEW code after this Up also belong in the 'general' bucket under the old
-- code — that is the only bucket the old code's identity fold ever produced —
-- so the rollback is correct for them too.
--
-- 🔴 ON `document_history` IT IS NOT, AND THAT IS A PROPERTY OF THE UP, NOT
-- A BUG IN THE DOWN. An earlier draft of this comment claimed "byte for byte"
-- across both tables; measured, that is false, and the claim is corrected here
-- rather than deleted because it is the kind of thing a reader would otherwise
-- re-derive from scratch. The Up rewrote '<role>::general' → '<role>'. The Down
-- can only ask "is this key bare NOW" (`instr(document_key,'::') = 0`), and a
-- key that was ALREADY bare before the Up — one of the three malformed shapes
-- both migrations spare — answers that question identically to one the Up just
-- made bare. So the Down hands it a '::general' suffix it never carried:
--   pre-Up   id=4 'assistant'          (spared, malformed)
--   post-Up  id=4 'assistant'          (untouched)
--   post-Down id=4 'assistant::general' (a name it never had)
-- Telling the two apart would require the Up to RECORD what it changed, which
-- is a larger change than a rollback path that only ever retreats the code
-- deserves — and the row in question is one no reader can address anyway. The
-- asymmetry is pinned, in both directions, by
-- TestMigration00062DownRewritesEveryBareLessonsHistoryKey, so it stays a
-- documented shape rather than a surprise.
CREATE TABLE lessons_rebuild (
    role_key   TEXT NOT NULL,
    task_type  TEXT NOT NULL,
    text       TEXT NOT NULL DEFAULT '',
    tombstoned INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (role_key, task_type)
);
INSERT INTO lessons_rebuild (role_key, task_type, text, tombstoned)
  SELECT role_key, 'general', text, tombstoned FROM lessons;
DROP TABLE lessons;
ALTER TABLE lessons_rebuild RENAME TO lessons;

UPDATE document_history
   SET document_key = document_key || '::general'
 WHERE document_kind = 'lessons'
   AND instr(document_key, '::') = 0
   AND document_key <> '';
