// chatDraftStore.test.ts — the store that OUTLIVES a room (T-48, R13-4).
//
// 🔴 WHY THIS FILE EXISTS. `ChatArea` is mounted under `key={peerId}`, so every
// piece of per-conversation state dies with the room. This module is the ONE
// deliberate exception: a draft and its staged files have to survive leaving the
// room, because that is what a draft IS.
//
// So the rule this file keeps is narrow and mechanical: NOTHING IN THIS MODULE
// MAY BE MUTABLE STATE THAT IS NOT KEYED BY PEER. A `let lastError` here is one
// room's sentence shown in another room, and no component test would go red.
//
// 🔴 WHAT THIS FILE CANNOT KEEP, AND WHO KEEPS IT (T-48, R14-3.1). The store's
// header claims everything surviving a room switch now lives here. This test
// cannot check that claim: it reads THIS file, the one file already obeying it.
// The instance that actually happened was `liveComposers` — a second
// module-level, peer-keyed table grown in `ChatArea.tsx` — and a one-file
// census is blind to it by construction. `lint:async-landing`'s MODULE_STATE
// register is the census over the whole chat import graph; this one is the
// narrow rule for the file that is the exception.

import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import {
  getChatDraft,
  getChatDraftAttachments,
  getChatAttachError,
  resetChatDrafts,
  saveChatDraftText,
  setChatAttachError,
  subscribeChatDraft,
  updateChatDraftAttachments,
} from "./chatDraftStore";

const SOURCE = readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), "chatDraftStore.ts"),
  "utf8",
);

const A = "m-aaa";
const B = "m-bbb";

beforeEach(() => resetChatDrafts());

describe("chatDraftStore", () => {
  it("keeps every room's draft, attachments and error to itself", () => {
    saveChatDraftText(A, "for A", "c-1");
    saveChatDraftText(B, "for B");
    updateChatDraftAttachments(A, () => [
      { id: "att-a", name: "a.png" } as never,
    ]);
    setChatAttachError(B, "太大了");

    expect(getChatDraft(A)).toMatchObject({ text: "for A", replyTo: "c-1" });
    expect(getChatDraft(B)).toMatchObject({ text: "for B" });
    expect(getChatDraft(B)?.replyTo).toBeUndefined();
    expect(getChatDraftAttachments(A)).toHaveLength(1);
    expect(getChatDraftAttachments(B)).toHaveLength(0);
    expect(getChatAttachError(A)).toBeNull();
    expect(getChatAttachError(B)).toBe("太大了");
  });

  it("notifies only the room that changed", () => {
    let seenA = 0;
    let seenB = 0;
    subscribeChatDraft(A, () => seenA++);
    subscribeChatDraft(B, () => seenB++);

    saveChatDraftText(A, "typing");
    setChatAttachError(A, "太大了");

    expect(seenA).toBe(2);
    expect(seenB).toBe(0);
  });

  it("hands back one frozen empty list, so a subscriber does not re-render forever", () => {
    // useSyncExternalStore re-renders whenever the snapshot's identity changes;
    // a fresh [] per read is an infinite loop rather than a wrong pixel.
    expect(getChatDraftAttachments(A)).toBe(getChatDraftAttachments(B));
  });

  it("declares no module-level mutable state that is not keyed by peer", () => {
    // The census, reduced to the one rule that matters here. A top-level `let`
    // or `var` in this module is state shared by every room — the exact shape
    // `key={peerId}` was introduced to delete, reappearing where no component
    // test can see it. Keyed containers (Map/Set) and frozen constants are the
    // permitted forms.
    const topLevelMutable = SOURCE.split("\n").filter((line) =>
      /^(let|var)\s/.test(line),
    );
    expect(topLevelMutable).toEqual([]);

    const topLevelConsts = [...SOURCE.matchAll(/^const\s+(\w+)\s*(?::[^=]+)?=\s*(.*)$/gm)];
    expect(topLevelConsts.length).toBeGreaterThan(0);
    for (const [, name, init] of topLevelConsts) {
      expect(
        /^new (Map|Set)[(<]/.test(init) || /^Object\.freeze\(/.test(init),
        `${name} is module-level state that is neither keyed by peer (Map/Set) nor frozen: ${init}`,
      ).toBe(true);
    }
  });
});
