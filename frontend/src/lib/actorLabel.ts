// lib/actorLabel.ts — resolve a document-history actor id to a human name.
//
// The wire carries only the stable id (`m-…`, `ow-…`, `owner`), and an id is
// exactly what the owner said he could not read. The roster is the only place a
// name lives, and it lists LIVE members only: a dismissed member and a released
// outsource worker are both absent from it. Owner ruling 2026-07-31 ("釋出我們
// 可以先簡單做，就顯示代號就好"): resolve what the roster knows, show the bare
// id for everyone else — no server-side resolution of removed identities.
//
// Returning "" for the unresolvable case (rather than the id) keeps the caller
// honest: the name and the id are rendered by DIFFERENT code paths, so an empty
// name can never silently produce "m-abc（m-abc）".

/** The owner's fixed wire id — the `sub` of the owner token, which is what the
 * server stamps on a revision the cockpit itself wrote. Kept local, the same as
 * ChatArea's / ChatGalleryPanel's OWNER_ID: the value is the wire's, not a new
 * name to invent. The owner is never on the roster, so without this he was the
 * one actor shown as a bare id on his own edits (owner ruling 2026-07-31). */
export const OWNER_ACTOR_ID = "owner";

export function actorDisplayName(
  actorId: string,
  members: readonly { id: string; name: string }[]
): string {
  const name = members.find((m) => m.id === actorId)?.name?.trim() ?? "";
  // A member whose display name IS its id (the assistant's `mira` shape) adds
  // nothing beside the id — one token, not the same token twice.
  return name === actorId ? "" : name;
}
