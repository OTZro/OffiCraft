// AgentDetailPanel · the slot map is EXHAUSTIVE (T-0b4f).
//
// owner 2026-08-09 (`rc-93536ece6a80`), verbatim:「兩邊共用應該是預設值,真正只有
// 一邊有的東西反而是特例,我們可以讓插槽的形式改變一下嗎? 就是都要給插槽的 key,
// 裡面某個欄位會說這個插槽有沒有生效,避免在沒注意到的情況下漏掉。這樣外包跟正職
// 要給的全部鍵值應該就是容器提供的全部鍵值」
//
// 🔴 The acceptance test of this ticket is a COMPILE error, not a runtime one:
// add a slot and implement it on one side only ⇒ the other side must not
// compile. That half cannot be asserted by running code, so it is written as
// `@ts-expect-error` below — those directives are themselves checked: if the
// type ever stops rejecting a missing key, tsc fails with "Unused
// '@ts-expect-error' directive". The guard reddens when the guarantee is
// removed, which is the only thing that makes it a guard.
//
// The runtime half below pins the OTHER direction (the one a compile check
// cannot see): an `on: false` slot must render NOTHING — in particular its
// reason must never reach the screen.

import { describe, it, expect, vi } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import {
  AGENT_DETAIL_SLOTS,
  notHere,
  slot,
  type AgentDetailSlots,
} from "./AgentDetailPanel";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([]),
    relocateMember: vi.fn(),
    activateMember: vi.fn(),
    patchMember: vi.fn(),
    getBootstrap: () =>
      Promise.resolve({ role: "assistant", name: "", taskType: "", context: "" }),
    listWebhooks: () => Promise.resolve([]),
    listScheduledMessages: () => Promise.resolve([]),
    getMemberResumeSummary: () => Promise.resolve(null),
    subscribeEvents: () => () => {},
  },
}));

// ─────────────────────────────────────────────────────────────────────────────
// COMPILE-TIME half. Every one of these is a type error TODAY; the assertions
// are the `@ts-expect-error` directives themselves.
// ─────────────────────────────────────────────────────────────────────────────

const every: AgentDetailSlots = {
  overlays: slot(null),
  afterIdentityCards: notHere("正職沒有這個概念"),
  afterInfoCards: notHere("正職沒有這個概念"),
  extraExpandCards: slot(null),
  afterPromptCards: slot(null),
};

// A wrapper that forgets ONE key must not compile. This is the ticket's core
// acceptance criterion: a slot added to AGENT_DETAIL_SLOTS breaks BOTH sides
// until each has said what it does with it, instead of one side silently
// rendering nothing.
// @ts-expect-error — a missing slot key is a type error, never a silent nothing
const missingOne: AgentDetailSlots = {
  overlays: slot(null),
  afterIdentityCards: slot(null),
  afterInfoCards: slot(null),
  extraExpandCards: slot(null),
};

// The other direction: a key the panel does not offer (a typo, or a slot that
// was removed — `beforeTerminalCards` was removed by this very ticket) must not
// compile either, or a wrapper could keep feeding a slot nobody renders.
const unknownKey: AgentDetailSlots = {
  ...every,
  // @ts-expect-error — the panel offers no such slot
  beforeTerminalCards: slot(null),
};

// 「這一邊不要」 must carry a reason. An empty one is indistinguishable from
// having forgotten, which is the whole failure this type removes.
// @ts-expect-error — an empty reason is rejected outright
const emptyReason = notHere("");

// And the off variant cannot be hand-written to dodge `notHere`.
const bareLiteral: AgentDetailSlots = {
  ...every,
  // @ts-expect-error — `why` is branded; only notHere() mints one
  afterPromptCards: { on: false, why: "" },
};

describe("AgentDetailPanel slot map", () => {
  it("offers exactly the five slots both wrappers must answer", () => {
    // Pins the LIST itself: growing it is exactly the event that must break
    // both wrappers, so it should be a deliberate edit here as well.
    expect([...AGENT_DETAIL_SLOTS]).toEqual([
      "overlays",
      "afterIdentityCards",
      "afterInfoCards",
      "extraExpandCards",
      "afterPromptCards",
    ]);
    // Keeps the compile-time fixtures above from being dead code under
    // noUnusedLocals — they exist for tsc, not for the runtime.
    expect(
      [every, missingOne, unknownKey, emptyReason, bareLiteral].every(Boolean),
    ).toBe(true);
  });

  it("renders nothing — not even the reason — for a slot this side declined", async () => {
    const member: Member = {
      id: "mira",
      memberId: "MB-AST001",
      name: "Mira",
      role: "assistant",
      status: "offline",
      lifecycle: "offline",
      runtime: "claude",
      actualRuntime: "",
      model: "opus",
      actualModel: "",
      effort: "medium",
      actualEffort: "",
      kind: "assistant",
      desiredMachineId: "",
      machine: "",
      actualMachine: "",
      account: null,
      contextPct: null,
      estimatedCost: null,
      bankedCost: null,
      tmuxSession: "member-mira",
      refocusSince: null,
      lastOp: "",
      lastOpOk: null,
      lastOpLog: "",
      lastOpReason: "",
      lastOpAt: null,
      unreadCount: 0,
    } as unknown as Member;

    const { container } = render(
      <I18nProvider>
        <MemberDetailPanel member={member} onBack={() => {}} />
      </I18nProvider>,
    );

    // The member panel declines two slots (委託任務 / 委託人 are outsource-only,
    // worker-panel-parity.md C4 / C3). Their reasons are developer notes — they
    // must never be rendered, or "this side has nothing here" would turn into a
    // line of internal prose on the owner's screen.
    expect(container.textContent ?? "").not.toContain("外包獨有");
    expect(container.textContent ?? "").not.toContain("worker-panel-parity");
  });
});
