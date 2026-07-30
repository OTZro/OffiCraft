# T-7d33 independent review

Reviewed `origin/main...feature/t-7d33-document-history` at `5eafc2f` without modifying product code or running MCP calls.

## Verdict: request changes

### Must fix

1. **[P1] The snapshot is not the exact pre-write state, despite the transaction claim.**
   Every handler folds/reads the current document before calling `SaveWithDocumentHistory`; that helper begins the transaction only after receiving a precomputed JSON string. Concurrent writers can both read `A`; the first commits `A -> B`, then the second stores another `A` snapshot and commits `A -> C`. `B` is no longer recoverable. This breaks the promised retained revision chain and the stated requirement that snapshot + live write share one transaction. Move the read/fold/snapshot decision into the same transaction as the journal insert and live write (or use a transactional compare-and-swap/version guard), then add a race/ordering test.

2. **[P1] Reset/default revisions cannot be faithfully restored.**
   Global context, roles, and lessons are overlay documents with `Tombstoned`/default semantics, but history serializes only their *folded text*. A reset stores the folded seed/current text, while restore always writes a non-tombstoned overlay. For example, edit a seed role, reset it, then restore the revision representing that reset: the visible text may match today, but `is_default` changes from true to false and future seed changes no longer apply. Retain the persisted overlay state (including tombstone) or explicitly define/version a restore representation that preserves it; test edit → reset → restore for all three overlay families.

3. **[P1] Destructive document writes bypass recoverability.**
   `HandleDeleteTaskManual...` and custom-role deletion do not create a final snapshot, and restore rejects a missing current manual/role. Therefore an accidental delete has no restorable previous revision, even though these are editable documents and the feature is a rollback journal. Either include delete as a journaled/restorable state, or explicitly narrow the product contract and task scope; the present API/spec says every editable document without such an exception.

4. **[P1] The promised cockpit history UI is absent.**
   The diff adds generated OpenAPI types only. There is no frontend adapter method, wire mapper, mock implementation, hook, Settings/task-manual component, strings, or UI test using `list_document_history`/`restore_document_history` (a repo-wide frontend search finds only the generated schema). The APIs may be callable through MCP, but the owner-requested “座艙歷史清單” and an in-product way to read/restore versions have not been delivered.

### Test and contract gaps

- The only end-to-end handler test covers global context. Add round trips for role definitions, lessons (including task-type key parsing), and task manuals; the manual test must prove the required four-field snapshot is restored as one coherent revision.
- Add negative tests for malformed history JSON, history belonging to another kind/key, restore at/over the document cap, and each caller class on list/restore. The current authorization addition only documents the special write branch; it does not exercise the new endpoints’ complete matrix.
- The OpenAPI route responses document only `200`, omitting the intentional `400`, `403`, and `404` outcomes. This makes the public contract materially less truthful for clients.

### What is sound

- The migration and per-document `(kind,key)` retention trim implement a bounded three-entry journal.
- Restore itself journals the outgoing current version, so the happy-path global-context test correctly demonstrates a reversible switch once a valid snapshot exists.
- Manual snapshots intentionally contain the owner-confirmed four content fields and do not mix fields from separate versions.

