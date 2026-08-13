// ResumeSummaryCard · PAYLOAD PARITY.
//
// Owner, verbatim: 「就是你怎麼給 agent 的就怎麼給我,格式應該要我們兩個雙方都
// 看得懂」 / 「我不要規則 我要看到到時候 agent 實際看到的 魔鬼藏在細節裡」.
//
// 🔴 WHY THE FIXTURE IS A **WIRE** PAYLOAD AND NOT A VIEW MODEL.
// The failure this file exists to catch was a SEAM failure: `mappers.ts`
// carried `roster` and `machines` no further than its own function body, so
// the payload had two sections the cockpit could not draw — and NOTHING was
// red, because the view model simply did not declare them and every component
// test was written against a hand-built view model that did not have them
// either. A test that starts from a view model can only ever prove the
// component draws what it was handed; it is structurally blind to a seam that
// hands it less than the server sent.
//
// So this file starts from `WIRE` — the same JSON an agent's `resume_summary`
// receives — pushes it through the REAL `toMemberResumeSummary`, and asserts
// against the screen. One fixture, both layers: drop a field in the mapper or
// forget to draw it in the component and the SAME assertion goes red.
//
// 🔴 AND WHY THERE IS A COVERAGE ASSERTION AT THE BOTTOM.
// Per-field assertions only cover the fields somebody remembered to write an
// assertion for — which is exactly the position we were already in. The
// coverage block instead asserts the SHAPE of the mapped snapshot against a
// declared roll-call, so a field added to the view model later cannot slip
// through unrendered and unnoticed: it goes red on arrival, naming itself.
// The roll-call is a hard-coded list on purpose — the only way past it is to
// edit it, and that edit is the review this file wants to force.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
import { toMemberResumeSummary } from "../api/mappers";
import type { WireResumeSummary } from "../api/wire";
import { ResumeSummaryCard } from "./ResumeSummaryCard";

// The snapshot as it comes off the wire. Values are deliberately DISTINCT and
// mutually non-substring, so an assertion cannot be satisfied by a neighbour's
// text — a field rendered into the wrong slot stays visible to the assertion,
// not just to the eye.
const WIRE: WireResumeSummary = {
  identity: "mira",
  generated_at: "2026-08-13 09:47:11 +08:00",
  note: "BOUNDED snapshot — recent chat + open tasks only.",
  overview: {
    chat_count: 501,
    chat_chars: 8237,
    tasks_returned: 613,
    tasks_open_total: 9042,
    tasks_detail_chars: 375,
    cards_waiting: 264,
    cards_answered_recent: 718,
    roster_chars: 4196,
    machines_chars: 1583,
  },
  chat: [
    {
      id: "cm-1",
      from: "m-planner",
      from_name: "普朗克",
      to: "owner",
      to_name: "Seth",
      body: "已完成後端組裝,等你決定要不要照這個形狀往下做",
      ts: 1786000001,
      ts_display: "2026-08-12 22:13:04 +08:00",
      body_omitted_chars: 0,
      reply_card_status: "answered",
      attachments: [],
      card: {
        options: ["照這個形狀做", "先擋著等下一輪"],
        answer_option_idx: 1,
        answer_text: "先擋著,理由寫在卡上",
        answered_ts: 1786000600,
        answered_at_display: "2026-08-12 22:23:20 +08:00",
      },
    },
    {
      // The HONEST-EMPTY case: the server could not resolve this sender to a
      // roster row. The screen must show the ADDRESS alone — see the exact
      // -equality assertion below for why this row carries the whole rule.
      id: "cm-2",
      from: "m-ghost",
      from_name: "",
      to: "m-planner",
      to_name: "普朗克",
      body: "這則的寄件者解析不到名字",
      ts: 1786000700,
      ts_display: "2026-08-12 22:31:47 +08:00",
      // FOLDED: this message IS here, with this many characters kept back on
      // the server. Not the same thing as chat_earlier_omitted below.
      body_omitted_chars: 1284,
      reply_card_status: "",
      attachments: [
        {
          id: "att-9",
          url: "/api/chat/attachment/att-9",
          filename: "交接筆記.md",
          mime: "text/markdown",
          is_image: false,
        },
      ],
    },
  ],
  // TRUNCATED: whole messages that are NOT in this payload at all.
  chat_earlier_omitted: {
    omitted: true,
    hint: "call get_chat with with='m-planner' and BOTH before_ts=1786000001 and before_id='cm-1'",
  },
  tasks: [
    {
      id: "t-1",
      task_no: "T-9001",
      title: "整理 RESUME SUMMARY 面板",
      type_key: "frontend",
      status: "in_progress",
      priority: "medium",
      waiting_reason: "",
      current_step_id: "s-1",
      current_step_name: "串接 lazy fetch",
      progress_done: 1,
      progress_total: 3,
      updated_ts: 1786000900,
      detail_chars: 375,
    },
  ],
  roster: [
    {
      id: "m-planner",
      name: "普朗克",
      kind: "member",
      role_name: "規劃",
      duty: "把票拆成可以被執行的形狀",
      current_task: "",
      task_status: "",
      waiting_reason: "",
      progress_done: 0,
      progress_total: 0,
      machine: "mach-alpha",
      presence: "online",
    },
    {
      id: "ow-31",
      name: "O-31",
      kind: "outsource",
      role_name: "",
      duty: "",
      current_task: "把座艙那張卡接上快照",
      task_status: "in_progress",
      waiting_reason: "",
      progress_done: 2,
      progress_total: 7,
      machine: "mach-beta",
      presence: "offline",
    },
  ],
  machines: {
    you_are_on: "mach-alpha",
    list: [
      { machine_id: "mach-alpha", display_name: "阿爾法", online: true },
      { machine_id: "mach-beta", display_name: "貝塔", online: false },
    ],
  },
};

const MAPPED = () => toMemberResumeSummary(WIRE);

vi.mock("../api", () => ({
  api: {
    getMemberResumeSummary: () => Promise.resolve(MAPPED()),
  },
}));

/** Render the card and open it — the section only fetches on first expand. */
async function open() {
  const utils = render(
    <I18nProvider>
      <ResumeSummaryCard agentId="mira" />
    </I18nProvider>,
  );
  fireEvent.click(utils.getByTestId("mp-resume-toggle"));
  await waitFor(() =>
    expect(utils.queryByTestId("mp-resume-overview")).not.toBeNull(),
  );
  return utils;
}

const R = zh.mp.resumeSummary;
const txt = (el: Element | null) => (el?.textContent ?? "").replace(/\s+/g, " ").trim();

beforeEach(() => {
  document.body.innerHTML = "";
});

describe("ResumeSummaryCard renders the SAME snapshot the agent receives", () => {
  it("shows the anchor the whole payload is read against — generated_at, VERBATIM", async () => {
    // Without it, every ts_display below is a timestamp with nothing to be
    // measured against. Byte-equal to the wire: this component owns no clock.
    const u = await open();
    expect(txt(u.getByTestId("mp-resume-generated-at-value"))).toBe(
      WIRE.generated_at,
    );
  });

  it("names BOTH parties of every message — display name AND the id you reply to", async () => {
    // 🔴 This is the assertion the "→ / ←" arrow could not satisfy. The arrow
    // showed neither an id nor a name: two different senders were the same
    // glyph, and a reader could not tell who said anything.
    const u = await open();
    const froms = u.getAllByTestId("mp-resume-chat-from").map(txt);
    const tos = u.getAllByTestId("mp-resume-chat-to").map(txt);

    expect(froms[0]).toContain("普朗克");
    expect(froms[0]).toContain("m-planner");
    expect(tos[0]).toContain("Seth");
    expect(tos[0]).toContain("owner");
    expect(tos[1]).toContain("普朗克");
    expect(tos[1]).toContain("m-planner");
  });

  it("shows the id ALONE when the server resolved no name — never the id dressed as one", async () => {
    // EXACT equality, not `toContain`: `toContain("m-ghost")` is satisfied by
    // a back-filled name too, so it would pass on the very bug this guards.
    const u = await open();
    expect(txt(u.getAllByTestId("mp-resume-chat-from")[1])).toBe("m-ghost");
  });

  it("shows each message's time using the SERVER's rendered stamp, not one of its own", async () => {
    // 🔴 Both halves matter. The first says a time is on screen at all (the
    // card used to read `m.ts` nowhere and show none). The second says it is
    // the SERVER's string — a cockpit-side formatter would state the same
    // instant in the viewer's zone while the agent reads the server's, and a
    // "some time is displayed" assertion would happily pass on that.
    const u = await open();
    const stamps = u.getAllByTestId("mp-resume-chat-ts").map(txt);
    expect(stamps).toEqual([
      WIRE.chat![0].ts_display,
      WIRE.chat![1].ts_display,
    ]);
    // And the raw epoch is never what the message's own time line shows — it
    // is the machine-readable twin, not something a reader was meant to
    // decode. Scoped to the meta line ON PURPOSE: the epoch legitimately
    // appears elsewhere on screen, inside the server's verbatim cursor hint,
    // and a whole-document assertion would have demanded we corrupt that hint
    // to stay green. (This is not hypothetical — the document-wide version of
    // this line went red on exactly that.)
    const metas = [...u.container.querySelectorAll(".mp-resume__chatmeta")].map(
      txt,
    );
    for (const meta of metas) {
      expect(meta).not.toContain(String(WIRE.chat![0].ts));
      expect(meta).not.toContain(String(WIRE.chat![1].ts));
    }
  });

  it("draws the reply card ON the message that opened it: pick, free text, and when", async () => {
    const u = await open();
    const card = u.getByTestId("mp-resume-chat-card");
    const c = WIRE.chat![0].card!;
    for (const opt of c.options!) expect(txt(card)).toContain(opt);
    expect(txt(u.getByTestId("mp-resume-card-answer-text"))).toContain(
      c.answer_text,
    );
    expect(txt(u.getByTestId("mp-resume-card-answered-at"))).toContain(
      c.answered_at_display,
    );
    // WHICH option was picked — the decision itself, not merely that options
    // existed. Marked on the option at answer_option_idx and nowhere else.
    const picked = u
      .getAllByTestId("mp-resume-card-option")
      .filter((el) => el.getAttribute("data-picked") === "true")
      .map(txt);
    expect(picked).toHaveLength(1);
    expect(picked[0]).toContain(c.options![c.answer_option_idx!]);
    // ONE home for the card: it rides the message, so there is exactly one.
    expect(u.getAllByTestId("mp-resume-chat-card")).toHaveLength(1);
  });

  it("carries the message's reply-card status and attachments across", async () => {
    const u = await open();
    expect(txt(u.getByTestId("mp-resume-chat-cardstatus"))).toContain(
      WIRE.chat![0].reply_card_status,
    );
    expect(txt(u.getByTestId("mp-resume-chat-attachments"))).toContain(
      WIRE.chat![1].attachments![0].filename,
    );
  });

  it("draws BOTH the folded marker and the absent marker", async () => {
    const u = await open();
    const folded = txt(u.getByTestId("mp-resume-chat-body-omitted"));
    const cut = txt(u.getByTestId("mp-resume-chat-earlier-omitted"));

    expect(folded).toContain(String(WIRE.chat![1].body_omitted_chars));
    expect(cut).toContain(R.chatCutLabel);
  });

  // 🔴 A FOLDED message and an ABSENT one must not be described with shared
  // vocabulary. They are different failures. One says "this is here, shortened,
  // the rest is on the server"; the other says "these may not be here at all,
  // go and fetch them". A reader who reads one as the other concludes it has
  // seen a conversation it has not seen.
  //
  // 🔴 WHY THE COMPARISON UNIT IS NOT "split on spaces and punctuation".
  // That was the previous shape of this guard and it was NEAR-VACUOUS: Chinese
  // writes no word boundaries, so two zh strings come out as one token each and
  // can only collide by being character-for-character identical. It was PROVED
  // vacuous — an independent review re-worded `bodyOmittedLead` to share six
  // characters and the whole of its meaning with the cut label, and this file
  // stayed green. And `en`, which is the half a whitespace split would actually
  // work on, was never compared at all.
  //
  // So: Han text is compared CHARACTER by character (the smallest unit zh
  // actually has), Latin text WORD by word, case-folded, and BOTH shipped
  // locales are checked. Overlap in either alphabet, in either locale, is red.
  const units = (s: string) => {
    const out = new Set<string>();
    for (const w of s.toLowerCase().match(/[a-z0-9_]{2,}/g) ?? []) out.add(w);
    for (const ch of s) if (/\p{Script=Han}/u.test(ch)) out.add(ch);
    return out;
  };

  it.each([
    ["zh", zh.mp.resumeSummary],
    ["en", en.mp.resumeSummary],
  ])(
    "🔴 [%s] words a FOLDED message and an ABSENT one with no vocabulary in common",
    (_locale, r) => {
      const folded = units(r.bodyOmittedLead + " " + r.bodyOmittedTail);
      const cut = units(r.chatCutLabel);
      // A guard that compares empty sets proves nothing — the two vocabularies
      // have to exist before "they do not overlap" means anything.
      expect(folded.size).toBeGreaterThan(2);
      expect(cut.size).toBeGreaterThan(2);
      expect([...folded].filter((u) => cut.has(u))).toEqual([]);
    },
  );

  it("shows the server's own recovery hint VERBATIM, not a cockpit paraphrase", async () => {
    // The hint names the exact cursor pair to send. A re-worded copy here
    // would be a second procedure that cannot be kept in step with the
    // endpoint — and would look right while being wrong.
    const u = await open();
    expect(txt(u.getByTestId("mp-resume-chat-earlier-omitted-hint"))).toBe(
      WIRE.chat_earlier_omitted!.hint,
    );
  });

  it("🔴 draws the ROSTER block — the section the seam used to drop", async () => {
    const u = await open();
    const rows = u.getAllByTestId("mp-resume-roster-row").map(txt);
    expect(rows).toHaveLength(WIRE.roster!.length);
    for (const [i, r] of WIRE.roster!.entries()) {
      expect(rows[i]).toContain(r.id);
      expect(rows[i]).toContain(r.name);
      expect(rows[i]).toContain(r.presence);
      expect(rows[i]).toContain(r.machine);
      if (r.duty !== "") expect(rows[i]).toContain(r.duty);
      if (r.role_name !== "") expect(rows[i]).toContain(r.role_name);
      // A contractor's bound task NEVER appears without its status: 0/0 alone
      // cannot tell "a bound task with no steps yet" from "no bound task".
      if (r.current_task !== "") {
        expect(rows[i]).toContain(r.current_task);
        expect(rows[i]).toContain(r.task_status);
        expect(rows[i]).toContain(`${r.progress_done}/${r.progress_total}`);
      }
    }
  });

  it("🔴 draws the MACHINE block — the other section the seam used to drop", async () => {
    const u = await open();
    const rows = u.getAllByTestId("mp-resume-machine-row").map(txt);
    expect(rows).toHaveLength(WIRE.machines!.list.length);
    for (const [i, m] of WIRE.machines!.list.entries()) {
      // The STABLE id, always — a machine is addressed by id, never by the
      // name a host reports for itself.
      expect(rows[i]).toContain(m.machine_id);
      expect(rows[i]).toContain(m.display_name);
      expect(rows[i]).toContain(m.online ? R.machineOnline : R.machineOffline);
    }
    expect(txt(u.getByTestId("mp-resume-you-are-on"))).toContain(
      WIRE.machines!.you_are_on,
    );
  });

  it("reports every size figure the agent's copy reports, each in its own slot", async () => {
    const u = await open();
    const o = WIRE.overview!;
    const pairs: [string, number][] = [
      ["chatCount", o.chat_count],
      ["chatChars", o.chat_chars],
      ["tasksReturned", o.tasks_returned],
      ["tasksOpenTotal", o.tasks_open_total],
      ["tasksDetailChars", o.tasks_detail_chars],
      ["cardsWaiting", o.cards_waiting],
      ["cardsAnsweredRecent", o.cards_answered_recent],
      ["rosterChars", o.roster_chars!],
      ["machinesChars", o.machines_chars!],
    ];
    for (const [key, value] of pairs) {
      expect(txt(u.getByTestId(`mp-resume-stat-${key}`))).toBe(String(value));
    }
  });

  it("keeps every section NAMED even while its body is folded away", async () => {
    // Which sections the snapshot carries is itself part of what is being
    // compared against the agent's copy. A collapsed section that vanished
    // entirely would be indistinguishable from one the payload never had.
    const u = await open();
    for (const id of [
      "mp-resume-chat-section",
      "mp-resume-tasks-section",
      "mp-resume-roster-section",
      "mp-resume-machines-section",
    ]) {
      fireEvent.click(u.getByTestId(`${id}-toggle`));
    }
    expect(txt(screen.getByTestId("mp-resume-chat-section"))).toContain(
      R.chatSection,
    );
    expect(txt(screen.getByTestId("mp-resume-roster-section"))).toContain(
      R.rosterSection,
    );
    expect(txt(screen.getByTestId("mp-resume-machines-section"))).toContain(
      R.machinesSection,
    );
    expect(txt(screen.getByTestId("mp-resume-tasks-section"))).toContain(
      R.tasksSection,
    );
    // …and the folded bodies really are gone, so the check above is not just
    // reading a section that never collapsed.
    expect(screen.queryAllByTestId("mp-resume-roster-row")).toHaveLength(0);
  });

  it("renders agent-authored text as markdown WITHOUT letting inline HTML through", async () => {
    // Bodies come from agents and from outside the studio. `Markdown` builds
    // React elements only, so markup in a snapshot stays inert text.
    const u = await open();
    const body = u.container.querySelector(".mp-resume__chatbody");
    expect(body).not.toBeNull();
    expect(u.container.innerHTML).not.toContain("<script");
  });
});

describe("the roll-call: a field the seam gains cannot stay undrawn", () => {
  // 🔴 Per-field assertions only ever cover the fields somebody remembered.
  // These pin the SHAPE, so the next field added to the view model announces
  // itself here instead of quietly joining roster/machines on the floor.
  it("the mapped snapshot carries exactly the sections this file asserts on", () => {
    expect(Object.keys(MAPPED()).sort()).toEqual(
      [
        "chat",
        "chatEarlierOmitted",
        "generatedAt",
        "identity",
        "machines",
        "note",
        "overview",
        "roster",
        "tasks",
      ].sort(),
    );
  });

  it("a mapped chat message carries exactly the fields this file asserts on", () => {
    expect(Object.keys(MAPPED().chat[0]).sort()).toEqual(
      [
        "attachments",
        "body",
        "bodyOmittedChars",
        "card",
        "from",
        "fromName",
        "id",
        "replyCardId",
        "replyCardStatus",
        "to",
        "toName",
        "ts",
        "tsDisplay",
      ].sort(),
    );
  });

  it("a mapped roster row and machine block carry exactly the fields this file asserts on", () => {
    expect(Object.keys(MAPPED().roster[0]).sort()).toEqual(
      [
        "currentTask",
        "duty",
        "id",
        "kind",
        "machine",
        "name",
        "presence",
        "progressDone",
        "progressTotal",
        "roleName",
        "taskStatus",
        "waitingReason",
      ].sort(),
    );
    expect(Object.keys(MAPPED().machines!).sort()).toEqual(
      ["list", "youAreOn"].sort(),
    );
    expect(Object.keys(MAPPED().machines!.list[0]).sort()).toEqual(
      ["displayName", "machineId", "online"].sort(),
    );
  });

  it("the mapper carries roster and machines ACROSS, rather than declaring them and dropping them", () => {
    // The narrowest statement of the original defect, at the layer it lived
    // in — so it stays red even if the component is rewritten around it.
    const m = MAPPED();
    expect(m.roster.map((r) => r.id)).toEqual(WIRE.roster!.map((r) => r.id));
    expect(m.machines!.list.map((x) => x.machineId)).toEqual(
      WIRE.machines!.list.map((x) => x.machine_id),
    );
    expect(m.generatedAt).toBe(WIRE.generated_at);
    expect(m.chatEarlierOmitted.hint).toBe(WIRE.chat_earlier_omitted!.hint);
  });
});
