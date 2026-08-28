// useQuotedMessageOverlay — the ONE exit behind every 「看原訊息／跳到原訊息」
// button in the cockpit (T-0b78).
//
// 🔴 WHAT THIS FILE IS FOR. Before T-0b78 the same intent had two answers on
// one screen: the chat bubble fetched the one message by id and showed it whole
// (right), while the 請示 card and the inline task card wrote a route and left
// ChatArea to find the row in the DOM it had already painted — which lands on
// the NEWEST message, silently, whenever the target is not in that DOM. The
// three call sites now share this hook, and the last test here is what makes a
// fourth copy of the fetch go red instead of shipping.
//
// 🔴 AND HERE IS WHAT THAT LAST TEST DOES *NOT* CATCH, stated because the
// version before this one claimed the whole property and only enforced part of
// it. It reads SOURCE TEXT. An independent reviewer walked straight through it
// by assembling the method name at runtime ("getChat" + "Message") and shipped
// a full second copy — read, overlay and failure state — with every test green
// and typecheck clean. So it catches the copy someone writes by HAND, which is
// the one that actually happens, and it does not catch deliberate evasion:
// dynamic names, an alias, or going under the adapter to fetch directly.
// Closing that needs an AST-level rule over the whole api surface; it is not
// here, and pretending otherwise is how the previous claim got written.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { api } from "../api";
import type { ChatMessage } from "../api/adapter";
import { useQuotedMessageOverlay } from "./useQuotedMessageOverlay";

function mkMsg(over: Partial<ChatMessage> & { id: string }): ChatMessage {
  return {
    from: "m1",
    to: "owner",
    body: "",
    ts: 1,
    attachments: [],
    replyCardId: null,
    replyCardStatus: null,
    ...over,
  };
}

/** A surface that has a LOADED WINDOW of messages on screen and a control that
 * asks for one message by id. `window` is what the deleted design searched;
 * `targetId` is deliberately not in it. */
function Harness({
  windowMsgs,
  targetId,
}: {
  windowMsgs: ChatMessage[];
  targetId: string;
}) {
  const quoted = useQuotedMessageOverlay();
  return (
    <div>
      {windowMsgs.map((m) => (
        <div key={m.id} data-msg-id={m.id}>
          {m.body}
        </div>
      ))}
      <button type="button" onClick={() => void quoted.open(targetId)}>
        看原訊息
      </button>
      {quoted.failureNotice(targetId)}
      {quoted.overlay}
    </div>
  );
}

function renderHarness(windowMsgs: ChatMessage[], targetId: string) {
  return render(
    <I18nProvider>
      <Harness windowMsgs={windowMsgs} targetId={targetId} />
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("useQuotedMessageOverlay", () => {
  it("shows the message whole, by id, and never touches the route", async () => {
    const full = "原訊息開頭" + "，還有後面很長的一整段".repeat(10);
    const get = vi
      .spyOn(api, "getChatMessage")
      .mockResolvedValue(mkMsg({ id: "c-1", from: "m1", body: full }));
    window.location.hash = "";

    const { getByText } = renderHarness([], "c-1");
    await act(async () => {
      fireEvent.click(getByText("看原訊息"));
    });

    expect(get).toHaveBeenCalledWith("c-1");
    expect(document.querySelector(".md-preview")?.textContent).toContain(
      "後面很長的一整段",
    );
    expect(window.location.hash).toBe("");
  });

  // 🔴 DENSITY, NOT AGE. The variable that pushes a message out of a loaded
  // window is how much traffic came AFTER it, not how old it is: a message sent
  // seconds ago is already out of a 30-row window on a busy line. A fixture
  // built out of "a very old message" tests the wrong thing and would still
  // pass under a design that resolved the target from the window whenever the
  // window happened to be deep enough. Here the target is the SECOND-newest
  // message on the line by timestamp, and it is out of the window anyway,
  // because 40 messages landed on that line in the same minute.
  it("shows the right message when a busy line has pushed it out of the loaded window", async () => {
    const targetTs = 10_000;
    const loaded = Array.from({ length: 40 }, (_, i) =>
      mkMsg({
        id: `c-noise-${i}`,
        body: `後續洗版 ${i}`,
        // Every one of these landed AFTER the target — same minute, same line.
        ts: targetTs + 1 + i,
      }),
    );
    // The target is NOT old: only one of the 40 above is newer than it by more
    // than a minute, and it is younger than nothing on screen by age alone.
    const target = mkMsg({
      id: "c-target",
      body: "被洗掉的那一則的全文",
      ts: targetTs,
    });
    const get = vi.spyOn(api, "getChatMessage").mockResolvedValue(target);

    const { container, getByText } = renderHarness(loaded, "c-target");
    // Precondition: it really is absent from everything on screen.
    expect(container.querySelector("[data-msg-id='c-target']")).toBeNull();

    await act(async () => {
      fireEvent.click(getByText("看原訊息"));
    });

    expect(get).toHaveBeenCalledWith("c-target");
    const overlay = document.querySelector(".md-preview")!;
    expect(overlay, "the window must have no say in this").toBeTruthy();
    expect(overlay.textContent).toContain("被洗掉的那一則的全文");
    // and it did NOT fall back to whatever the window happened to hold.
    expect(overlay.textContent).not.toContain("後續洗版");
  });

  it("says the read failed, on screen, and opens nothing", async () => {
    vi.spyOn(api, "getChatMessage").mockRejectedValue(new Error("boom"));

    const { getByTestId, getByText } = renderHarness([], "c-1");
    await act(async () => {
      fireEvent.click(getByText("看原訊息"));
    });

    expect(getByTestId("msg-quote-error").textContent).toBe(
      zh.chat.replyQuoteOpenFailed,
    );
    expect(document.querySelector(".md-preview")).toBeNull();
  });

  it("is a single request per click, however impatiently the button is pressed", async () => {
    const get = vi
      .spyOn(api, "getChatMessage")
      .mockResolvedValue(mkMsg({ id: "c-1", body: "全文" }));

    const { getByText } = renderHarness([], "c-1");
    await act(async () => {
      fireEvent.click(getByText("看原訊息"));
      fireEvent.click(getByText("看原訊息"));
    });

    expect(get).toHaveBeenCalledTimes(1);
  });

  // 🔴 THE ANTI-SECOND-COPY CLAUSE. Behaviour tests cannot see a duplicate:
  // a call site that grew its OWN api.getChatMessage + its own error state
  // would draw the same pixels and pass every test above. This reads SOURCE
  // TEXT instead. Its exact reach — and what walks through it — is written at
  // the top of this file; the name of this test says only what it enforces.
  it("no component names getChatMessage — the hook is the only one that does", () => {
    // vitest runs with the frontend package as cwd.
    const root = resolve(process.cwd(), "src");
    // The THREE KNOWN call sites must actually take the shared exit. Named, so
    // one quietly dropping the hook goes red here rather than only in a
    // behaviour test that a second copy would keep green.
    for (const rel of [
      "src/components/ChatArea.tsx",
      "src/components/RepliesPage.tsx",
      "src/components/TaskReplyCard.tsx",
    ]) {
      const src = readFileSync(resolve(process.cwd(), rel), "utf8");
      expect(src, `${rel} must take the shared exit`).toContain(
        "useQuotedMessageOverlay(",
      );
    }
    // 🔴 AND THE SWEEP IS THE WHOLE TREE, not those three paths. The version
    // before this one checked only the three files it already knew about, so a
    // FOURTH component with its own copy — a new screen, a new card — passed
    // untouched. That is the copy that actually happens: nobody edits the three
    // guarded files to add a duplicate, they write a new one.
    const offenders: string[] = [];
    const walk = (dir: string) => {
      for (const e of readdirSync(dir, { withFileTypes: true })) {
        const full = resolve(dir, e.name);
        if (e.isDirectory()) {
          walk(full);
          continue;
        }
        if (!/\.(ts|tsx)$/.test(e.name)) continue;
        // Tests may name it (they mock it). The api layer DEFINES it. The hook
        // is the one place allowed to CALL it.
        if (/\.test\.tsx?$/.test(e.name)) continue;
        const rel = full.slice(root.length + 1);
        if (rel.startsWith("api/")) continue;
        if (rel === "hooks/useQuotedMessageOverlay.tsx") continue;
        const code = readFileSync(full, "utf8")
          .split("\n")
          .filter((l) => !/^\s*(\/\/|\*|\/\*)/.test(l))
          .join("\n");
        if (code.includes("getChatMessage")) offenders.push(rel);
      }
    };
    walk(root);
    expect(
      offenders,
      "these files fetch the quoted message themselves instead of taking the " +
        "shared exit (hooks/useQuotedMessageOverlay)",
    ).toEqual([]);
  });
});
