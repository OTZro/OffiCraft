// 🔴 THE LATCHES OF ONE CONVERSATION, AS LEASES RATHER THAN FIELDS (T-48).
//
// The same defect shipped four times in this feature — second review F2, third
// review R3-1, fourth review R4-1 and R4-2. Every time, a boolean or counter
// that gates the newest-page load was left SET by a conversation the owner had
// already left, or by a call that had already ended, and the room never loaded
// again: permanently blank, never marked read, and nothing on screen to say so.
//
// Three shapes of the same mistake, and what each of them needs:
//   1. "the latch belongs to nobody"     ⇒ stamp it with the conversation.
//   2. "the reset list is hand-written"  ⇒ build the whole record at once.
//   3. "the release re-looks-up the CURRENT record instead of holding the one
//      it acquired"                      ⇒ the release must be a handle bound
//      to the record it came from, and there must be no way to say
//      "release whatever is current".
//
// (3) is R4-1, which passed 1672 tests while being a live regression. The
// previous fix put the acquire/release pair in one function, which left the
// mutable fields public — so the broken form was still writable, and the rule
// in latch-inventory.md §3 literally told the next person to write it.
//
// So the fields are gone from the type. State lives in this module's closure;
// there is no property to read, no property to assign, and `x as any` finds
// nothing to reach. The only way to set a latch is `acquire`, the only way to
// drop one is the handle `acquire` handed back, and that handle closes over
// THIS record — releasing it after the conversation moved on writes into an
// orphan nobody reads, which is exactly right: the debt died with its
// conversation.
//
// What is deliberately NOT here: `loadSeq` / `committedSeq`. Those are a
// MONOTONIC GLOBAL CLOCK, not latches — a ticket taken later must outrank one
// taken earlier even across a peer switch.

/** Every latch a conversation can hold. */
export type LatchName =
  | "entryAnchor"
  | "anchorFetch"
  | "loadStale"
  | "loadingOlder"
  | "loadingNewer";

/** The latches a caller may TAKE.
 *
 * `entryAnchor` is absent on purpose: it is armed when the conversation is
 * opened AT an anchor (nobody takes it) and it is dropped when an anchor fetch
 * ends (nobody drops it by name). Making it acquirable would hand back a
 * handle that could unlatch an anchor still in the air — the second half of
 * R4-1 — so the type simply does not offer it. */
export type Lease = Exclude<LatchName, "entryAnchor">;

/** Drops the latch it was taken on. Idempotent, and bound to ONE record: a
 * second call, or a call after that conversation is gone, does nothing. */
export type LatchRelease = () => void;

export type ConversationLatches = {
  /** The conversation this record belongs to. Readable so a holder can say
   * which one it is; it can never be reassigned. */
  readonly peer: string;
  /** Take a latch. `null` means "not yours, or already held" — the caller must
   * stand down, and there is nothing to release. The returned handle is the
   * ONLY way back out. */
  acquire(peer: string, name: Lease): LatchRelease | null;
  /** Is this latch held right now? `false` for a conversation that is not
   * this record's. */
  isHeld(peer: string, name: LatchName): boolean;
};

function once(drop: () => void): LatchRelease {
  let done = false;
  return () => {
    if (done) return;
    done = true;
    drop();
  };
}

/** Open the latch record for one conversation.
 *
 * `anchored` = this conversation was entered AT a message id, so the thread is
 * deliberately empty until the anchor window lands and no ordinary load may
 * put the live tail on screen in the meantime. That is `entryAnchor`. */
export function openLatches(
  peer: string,
  anchored: boolean,
): ConversationLatches {
  let entryAnchor = anchored;
  let anchorFetch = 0;
  let loadStale = false;
  let loadingOlder = false;
  let loadingNewer = false;

  const held = (name: LatchName): boolean => {
    switch (name) {
      case "entryAnchor":
        return entryAnchor;
      case "anchorFetch":
        return anchorFetch > 0;
      case "loadStale":
        return loadStale;
      case "loadingOlder":
        return loadingOlder;
      case "loadingNewer":
        return loadingNewer;
    }
  };

  return {
    peer,
    isHeld(who, name) {
      return who === peer && held(name);
    },
    acquire(who, name) {
      if (who !== peer) return null;
      switch (name) {
        // Same-direction mutexes: a second holder is refused, which is how the
        // caller learns to stand down (a scroll handler firing repeatedly near
        // the top must not stack duplicate cursor requests).
        case "loadingOlder":
          if (loadingOlder) return null;
          loadingOlder = true;
          return once(() => {
            loadingOlder = false;
          });
        case "loadingNewer":
          if (loadingNewer) return null;
          loadingNewer = true;
          return once(() => {
            loadingNewer = false;
          });
        // The "last load never landed" debt (T-929f). Not a mutex — a second
        // failure re-states the same debt rather than being refused — and the
        // holder is the failing load, while the payer is the next load that
        // lands. The payer settles it through the handle the failure kept.
        case "loadStale":
          loadStale = true;
          return once(() => {
            loadStale = false;
          });
        // 🔴 COUNTED, AND IT ALSO ENDS THE ENTRY-ANCHOR WINDOW. An anchor pair
        // is two parallel GETs; a load starting after it takes a HIGHER
        // generation ticket and can commit first, which makes the anchor's own
        // commit "superseded" — the jump silently does not happen and the
        // reader is told the message was probably cleared (F3). So the
        // ordinary refresh is held for the round trips.
        //
        // Dropping the lease also clears `entryAnchor` because the end of an
        // anchor fetch IS the end of the entry window — EVERY ending, including
        // 404, 422, superseded and a rejected fetch (R3-3: the superseded
        // branch used to keep the latch set "because the caller re-schedules",
        // and the caller's only clearing path fired on an EMPTY thread, which
        // superseded by definition is not). Coupling them here is what makes
        // "release on every ending" unbreakable from the call site.
        case "anchorFetch":
          anchorFetch += 1;
          return once(() => {
            anchorFetch -= 1;
            entryAnchor = false;
          });
      }
    },
  };
}
