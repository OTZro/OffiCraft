-- +goose Up
-- T-ae38 每段文件各有自己的字數上限. Renames the ONE shared cap setting to the
-- suffixed key that keeps its meaning; the other three keys are absent rows and
-- therefore read their code-side defaults (settings.go: absent = default).
--
-- WHY A RENAME AND NOT "leave the old key for the manual" (owner's window Mira,
-- 2026-08-03, adopted): an agent reading `get_settings` sees key NAMES with no
-- descriptions attached. With `doc.cap_chars` sitting beside `doc.cap_chars.duty`
-- / `.insight` / `.learning`, the unsuffixed one reads as "the global default" —
-- someone raising the task-manual cap would edit it believing they had moved all
-- four, and nothing anywhere would contradict them. The argument for keeping the
-- old name was zero migration cost, and it does not survive contact with this
-- file: the VALUE has to move to a per-segment meaning either way, so the
-- migration was always going to be written.
--
-- WHY THE MANUAL INHERITS THE VALUE AND THE OTHER THREE DO NOT: the stored value
-- is whatever the owner last raised the SHARED cap to (15000 on this install),
-- and the manual's two long documents are the ones that were actually sized
-- against it. Insight and Learning start at the owner's stated 10000 (T-ae38,
-- verbatim: 「我預期 duty 1000 / insight 10000 / learning 10000」). Docs already
-- over the new number are NOT truncated — the standing rule (DocCapBlocked) lets
-- an over-cap doc keep converging downward and only refuses a write that is not
-- shorter, and that rule now applies identically to all three segments.
--
-- The task manual's sop_md / learnings answer to `.manual` rather than to any of
-- the three role-journal segments because they are keyed by `type_key`: they are
-- assets of a task TYPE, not entries in a role's journal.
UPDATE setting SET key = 'doc.cap_chars.manual' WHERE key = 'doc.cap_chars';

-- +goose Down
-- Put the shared key back. The three per-segment keys are dropped rather than
-- folded into it: an older binary reads only `doc.cap_chars`, so any Duty /
-- Insight / Learning number written while this migration was applied has
-- nowhere older to live. Deleting them beats leaving unread rows that a
-- re-migration would silently resurrect at a value nobody remembers setting.
UPDATE setting SET key = 'doc.cap_chars' WHERE key = 'doc.cap_chars.manual';
DELETE FROM setting WHERE key IN ('doc.cap_chars.duty', 'doc.cap_chars.insight', 'doc.cap_chars.learning');
