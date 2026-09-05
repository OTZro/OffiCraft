// The panel-owned 更改 button's testid, verbatim from each detail panel. Shared
// by the identity-actions-row story and its spec so the two cannot drift.
//
// 🔴 It lives in its own module because Playwright CT's transform treats every
// import in a spec file as a component import: pulling a plain constant out of
// the STORY module alongside the story itself collides
// ("Identifier 'IdentityActionsRowStory' has already been declared").
export const CHANGE_TESTID = {
  member: "mp-change",
  worker: "worker-detail-change",
} as const;
