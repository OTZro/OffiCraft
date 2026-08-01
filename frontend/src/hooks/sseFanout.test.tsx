// SSE fan-out: what ONE notification costs, and what it must still update
// (T-8115 step 4). One harness mounts the six delta-backed cockpit hooks
// together, because none of the three properties pinned here is visible from a
// single hook in isolation:
//
//   1. A delta re-pulls only the ITEM it named. A chat line in someone else's
//      conversation must not re-download the roster, the 外包 rail, or the open
//      thread.
//   2. ONE resync (http.ts fans one synthetic delta per closed topic, 12 of
//      them, synchronously) costs each hook ONE refetch — not one per topic it
//      happens to listen to.
//   3. Reading a thread is itself a durable write (`GET /api/chat?with=` = 列表
//      即讀), so the server fans a `chat_read` delta BACK at us. That echo must
//      not re-run the fan-out, and above all a delta about ANOTHER conversation
//      must not re-enter the marking read — that is the cockpit driving its own
//      event loop.
//
// 🔴 EVERY cost assertion here is paired with a VALUE assertion, because "fewer
// requests" is trivially satisfied by a hook that stopped updating. The numbers
// below are measured (the tally is the mock itself), and the values asserted are
// the ones the server said, on the row the delta actually named.
//
// Measured before/after on this harness (requests caused by ONE delta, mount
// excluded): chat line in another conversation 5 → 2; the read echo 4 → 2; one
// resync 21 → 9; one inbound message including its echo round 9 → 6.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import type { SseDelta } from "../api/adapter";

const h = vi.hoisted(() => ({
  counts: {} as Record<string, number>,
  handlers: [] as ((topic: string, delta?: unknown) => void)[],
  members: [] as Record<string, unknown>[],
  workers: [] as Record<string, unknown>[],
  tasks: [] as Record<string, unknown>[],
  unread: 0,
}));

function bump(name: string): void {
  h.counts[name] = (h.counts[name] ?? 0) + 1;
}

vi.mock("../api", () => ({
  api: {
    listMembers: async () => {
      bump("listMembers");
      return h.members;
    },
    getMember: async (id: string) => {
      bump("getMember");
      const found = h.members.find((m) => m.id === id);
      if (!found) throw new Error(`no member ${id}`);
      // Answered through the gap table, which is what keeps a fake from being
      // more generous than the wire — the mistake that let the badge regression
      // ship green (api/dtoParity.ts).
      // ⚠️ For `member` that projection is TODAY THE IDENTITY: the server was
      // fixed, so `PER_ITEM_DTO_GAPS.member` is empty and this returns the list
      // row unchanged. So this line currently buys NO protection (measured
      // 2026-08-01: replacing it with a bare `return found` leaves all 14 tests
      // green). Keep it anyway — it is what makes this fake correct again the
      // moment a member-side gap reappears. The guard that really holds the
      // member badge is the Go parity test.
      return projectSingleItem("member", found);
    },
    listOutsourceWorkers: async () => {
      bump("listOutsourceWorkers");
      return h.workers;
    },
    getOutsourceWorker: async (id: string) => {
      bump("getOutsourceWorker");
      const found = h.workers.find((w) => w.id === id);
      if (!found) throw new Error(`no worker ${id}`);
      // This one IS a faithful superset of the list row (same projectWorker on
      // the server), so the projection is a no-op — asserted in dtoParity.test.
      return projectSingleItem("outsourceWorker", found);
    },
    listTasks: async (opts?: { statuses?: string[] }) => {
      bump("listTasks");
      // The status set is a SERVER-side filter (`?statuses=`), so the fake must
      // apply it — a fake that returns every row no matter what is asked cannot
      // show that a task which left the filter leaves the list.
      const want = opts?.statuses;
      return want === undefined
        ? h.tasks
        : h.tasks.filter((t) => want.includes(t.status as string));
    },
    getTask: async (id: string) => {
      bump("getTask");
      const found = h.tasks.find((t) => t.id === id);
      if (!found) throw new Error(`no task ${id}`);
      // 🔴 TaskDTO carries no dep_tasks (the join is on the LIGHT list only), so
      // a per-task read cannot serve the card's dep rows — api/dtoParity.ts.
      return projectSingleItem("task", found);
    },
    listTaskTypes: async () => {
      bump("listTaskTypes");
      return [];
    },
    getTaskCount: async () => {
      bump("getTaskCount");
      return { total: 0, open: 0 };
    },
    getChatUnreadCount: async () => {
      bump("getChatUnreadCount");
      return h.unread;
    },
    listChat: async () => {
      bump("listChat");
      return [];
    },
    peekChat: async () => {
      bump("peekChat");
      return [];
    },
    listChatReads: async () => {
      bump("listChatReads");
      return [];
    },
    getServerSettings: async () => {
      bump("getServerSettings");
      return { outsourceMaxParallel: 3 };
    },
    subscribeEvents: (cb: (topic: string, delta?: unknown) => void) => {
      h.handlers.push(cb);
      return () => {
        h.handlers = h.handlers.filter((x) => x !== cb);
      };
    },
  },
}));

import { projectSingleItem } from "../api/dtoParity";
import { useMembers } from "./useMembers";
import { useOutsourceWorkers } from "./useOutsourceWorkers";
import { useChatUnread } from "./useChatUnread";
import { useTaskCount } from "./useTaskCount";
import { useChat } from "./useChat";
import { useTasks } from "./useTasks";

/** The conversation the cockpit has OPEN throughout. */
const OPEN_PEER = "m-open";
/** The status set the 任務頁 filter opens on. */
const FILTER = ["in_progress"];

function member(id: string, unreadCount = 0) {
  return { id, name: id, status: "active", lifecycle: "online", unreadCount };
}

function worker(id: string, unreadCount = 0) {
  return {
    id,
    codename: id.toUpperCase(),
    model: "claude-opus-5",
    effort: "medium",
    taskId: "t-bound",
    taskCreatedTs: 100,
    createdTs: 100,
    unreadCount,
  };
}

function task(id: string, status = "in_progress", over = {}) {
  return {
    id,
    status,
    title: id,
    priority: "medium",
    executorKind: "member",
    executorId: "m-open",
    deps: ["t-dep"],
    // The server-side dep join the LIGHT list carries (T-a3e4). It is what lets
    // the card say 「等 T-dep <標題>」 instead of a bare short id, and it is
    // ABSENT from TaskDTO — so it is also the value a per-task refetch loses.
    depTasks: [{ id: "t-dep", title: "the blocker", status: "done" }],
    ...over,
  };
}

function useCockpit() {
  return {
    members: useMembers().members,
    workers: useOutsourceWorkers().workers,
    unread: useChatUnread(),
    taskCount: useTaskCount(),
    chat: useChat(OPEN_PEER),
    tasks: useTasks(FILTER).tasks,
  };
}

beforeEach(() => {
  // The owner is LOOKING. jsdom's hasFocus() is false, which would route every
  // thread load through the read-only peek and hide the self-drive entirely.
  document.hasFocus = () => true;
  h.counts = {};
  h.handlers = [];
  h.members = [member(OPEN_PEER, 3), member("m-other", 1), member("m-third")];
  h.workers = [worker("ow-1"), worker("ow-2")];
  h.tasks = [task("t-aaa"), task("t-bbb")];
  h.unread = 4;
});

/** Deliver one real delta to every subscriber exactly as http.ts's onmessage
 * does: synchronously, in subscription order.
 *
 * ⚠️ 這個 `act()` **不切 burst** —— 一陣的邊界是 `await`,不是 `act()`。連呼
 * `emit()` 而中間不 await 是**一陣**,不是 N 陣。要量 k 之前先讀 k 上界那條
 * 測試上方的表(實測數字在那裡),否則量到的 k 不是你以為的 k。 */
function emit(delta: SseDelta) {
  act(() => {
    for (const cb of [...h.handlers]) cb(delta.topic, delta);
  });
}

/** The 12 closed topics resyncAll replays, naming NOTHING (a resync means "you
 * may have missed anything"), fanned synchronously topic-major. */
const RESYNC_TOPICS = [
  "member",
  "chat",
  "chat_read",
  "reply_card",
  "task",
  "outsource_worker",
  "task_manual",
  "global_context",
  "role_def",
  "lessons",
  "context",
  "monitoring",
];

function emitResync() {
  act(() => {
    for (const topic of RESYNC_TOPICS) {
      for (const cb of [...h.handlers]) cb(topic, { topic, names: {}, ids: [] });
    }
  });
}

async function settle() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** Mount, drain everything the mount fetches, then zero the tally so a
 * scenario's numbers are its own. */
async function mountedCockpit() {
  const view = renderHook(() => useCockpit());
  await waitFor(() => expect(h.counts.listMembers).toBe(1));
  await waitFor(() => expect(h.counts.listTasks).toBe(1));
  await settle();
  h.counts = {};
  return view;
}

function totalRequests(): number {
  return Object.values(h.counts).reduce((a, b) => a + b, 0);
}

describe("one delta re-pulls only what it named (T-8115)", () => {
  it("a chat line in ANOTHER conversation re-reads that ONE member — and the badge really moves", async () => {
    const view = await mountedCockpit();
    // The server's new truth for m-other: the badge went 1 → 6.
    h.members = [member(OPEN_PEER, 3), member("m-other", 6), member("m-third")];
    h.unread = 9;

    emit({
      topic: "chat",
      names: { id: "cm-1", from: "m-other", to: "owner" },
      ids: ["cm-1", "m-other", "owner"],
    });
    await settle();

    // VALUE first: the named card carries the server's number, the others are
    // untouched, and the roster order (by name, server-decided) is preserved.
    expect(view.result.current.members.map((m) => m.unreadCount)).toEqual([
      3, 6, 0,
    ]);
    expect(view.result.current.members.map((m) => m.id)).toEqual([
      OPEN_PEER,
      "m-other",
      "m-third",
    ]);
    expect(view.result.current.unread).toBe(9);

    // COST: one member read, not the company. The VALUE assertion above is what
    // makes this safe to want: the fake answers `GET /{id}` through
    // projectSingleItem, so if that endpoint ever stops computing unread_count
    // this pair goes red instead of quietly zeroing the badge (api/dtoParity.ts).
    expect(h.counts.getMember).toBe(1);
    expect(h.counts.listMembers ?? 0).toBe(0);
    // The open thread belongs to someone else — no reload, and above all no
    // marking read (that read is what fans the echo round).
    expect(h.counts.listChat ?? 0).toBe(0);
    expect(h.counts.peekChat ?? 0).toBe(0);
    expect(h.counts.listChatReads ?? 0).toBe(0);
    // A chat line cannot assign or release an 外包, so the rail has nothing to do.
    expect(h.counts.listOutsourceWorkers ?? 0).toBe(0);
    expect(h.counts.getOutsourceWorker ?? 0).toBe(0);
    // Nothing about a chat line changes the settings snapshot (T-8115 step 3's
    // shared cache is not re-entered either).
    expect(h.counts.getServerSettings ?? 0).toBe(0);
    expect(totalRequests()).toBe(2); // getMember + the office total
  });

  // ────────────────────────────────────────────────────────────────────────
  // 🔴 讀下面這條測試之前:一「陣」(burst)的邊界是 `await`,不是 `act()`。
  //
  // 這件事會讓下一個量 k 的人量出錯誤的數字而毫無察覺,而且**兩個方向的誤解
  // 都落向「看起來沒事」**,所以兩個都寫在這裡:
  //
  //   `emit()` 自帶 `act()`,但那個 `act()` 收的是**同步** callback ⇒ 它不會
  //   (在 JS 裡也不可能)排空 microtask queue,而 `deltaSink` 正是靠
  //   `queueMicrotask` 收整陣的(lib/deltaSink.ts)。所以只要中間沒有 `await`,
  //   連呼 N 次 `emit()` 是**一陣 k=N**,不是 N 陣。
  //
  // 實測(2026-08-02,本檔 harness 加三條探針跑出來的,不是推的;三則 chat delta
  // 各指一個名冊成員,量 useMembers 的兩個 counter):
  //
  //   | 寫法                                   | getMember | listMembers |
  //   |---------------------------------------|-----------|-------------|
  //   | `emit(); emit(); emit(); await settle()` |     0     |      1      |  ← 一陣 k=3
  //   | 三則 delta 塞進同一個 `act()`            |     0     |      1      |  ← 一陣 k=3
  //   | `emit(); await settle();` × 3           |     3     |      0      |  ← 三陣 k=1
  //
  //   兩種誤解各自怎麼騙人:
  //   (a) 想要**三個獨立的 k=1**、寫成連呼三次不 await ⇒ 拿到一陣 k=3,量到
  //       「1 次 listMembers」,會被讀成「逐項路徑沒被觸發 / 沒事發生」——實際上
  //       是 k>1 的清單路徑開火了。
  //   (b) 想要**一陣 k=3**、寫成中間 await ⇒ 拿到三陣 k=1,量到「3 個 GET」,
  //       會被讀成「k=3 也才 3 次,還好」——實際上那是三個 k=1,而真正的一陣
  //       k=3 是 1 次清單 GET。放大從來不在這裡發生。
  //
  // ⇒ **要構造真正的單一 burst,就把多則 delta 送進同一個 `act()`**(`emitResync`
  //    就是這個形狀),或連呼 `emit()` 但中間一個 `await` 都不放;**要構造 N 個
  //    獨立 burst,每次 emit 後面都要 `await settle()`**。
  //
  // ⚠️ 但下面這條測試**一則 delta 都不用湊** —— agent↔agent 那條路天生就是 k=2:
  //    一則 chat delta 的 `ids` 同時含 `from` 與 `to`(見下方 fixture),所以
  //    **單一 emit 就是 k=2**(實測:getMember 0 / listMembers 1)。要量 k>1
  //    的成本**不需要**任何 burst 構造技巧,別因為以為構造很貴就放棄量它。
  // ────────────────────────────────────────────────────────────────────────
  it("🔴 ONE agent-to-agent line names TWO members — that re-pulls the LIST, not two reads", async () => {
    // THE REAL SHAPE, not a synthetic multi-member burst. A chat delta carries
    // {id, from, to} (api_chat.go), `toSseDelta` keeps all of them, and the hub
    // delivers EVERY delta to the owner/dashboard connection — so one member
    // talking to another member names TWO held cards in a SINGLE frame, with no
    // burst coalescing involved. Agents talking to each other is ordinary here,
    // so this is a normal path, not a corner case.
    //
    // COST is why it must not fan out: `unreadCountsForRequest` runs a full
    // ListChat() scan PER REQUEST, so k per-item reads cost k GETs + k full
    // scans, while the list is 1 + 1 for any k (measured 2026-08-01). The
    // crossover is at 2 — k=1 ties on cost and wins on payload, k>=2 loses.
    const view = await mountedCockpit();
    // The server's new truth: both ends of that conversation moved.
    h.members = [member(OPEN_PEER, 3), member("m-other", 6), member("m-third", 2)];
    h.unread = 11;

    const delta = {
      topic: "chat",
      names: { id: "cm-a2a", from: "m-other", to: "m-third" },
      ids: ["cm-a2a", "m-other", "m-third"],
    };
    // PREMISE: this delta really does name TWO cards this roster holds. Without
    // this the test would still pass against a fan-out that simply never fired.
    const held = view.result.current.members.map((m) => m.id);
    expect(delta.ids.filter((id) => held.includes(id))).toEqual([
      "m-other",
      "m-third",
    ]);

    emit(delta);
    await settle();

    // COST: ONE list read, and NOT two per-item reads.
    expect(h.counts.listMembers).toBe(1);
    expect(h.counts.getMember ?? 0).toBe(0);

    // VALUE: the list is still the source of every badge, so both ends moved.
    // (A cost assertion alone is satisfied by a hook that stopped updating.)
    expect(view.result.current.members.map((m) => m.unreadCount)).toEqual([
      3, 6, 2,
    ]);
    expect(view.result.current.members.map((m) => m.id)).toEqual([
      OPEN_PEER,
      "m-other",
      "m-third",
    ]);
  });

  it("a chat line with an 外包 re-reads that ONE worker row, and leaves the roster alone", async () => {
    const view = await mountedCockpit();
    h.workers = [worker("ow-1", 5), worker("ow-2")];

    emit({
      topic: "chat",
      names: { id: "cm-2", from: "ow-1", to: "owner" },
      ids: ["cm-2", "ow-1", "owner"],
    });
    await settle();

    expect(view.result.current.workers.map((w) => w.unreadCount)).toEqual([
      5, 0,
    ]);
    expect(view.result.current.workers.map((w) => w.id)).toEqual([
      "ow-1",
      "ow-2",
    ]);
    expect(h.counts.getOutsourceWorker).toBe(1);
    expect(h.counts.listOutsourceWorkers ?? 0).toBe(0);
    // An `ow-` peer is not in the roster, and a chat line cannot add a member.
    expect(h.counts.listMembers ?? 0).toBe(0);
    expect(h.counts.getMember ?? 0).toBe(0);
  });

  it("a task delta re-pulls the list: the row updates, the filter drops it, and the dep join SURVIVES", async () => {
    const view = await mountedCockpit();
    expect(view.result.current.tasks.map((t) => t.id)).toEqual([
      "t-aaa",
      "t-bbb",
    ]);
    // t-aaa was terminated — it no longer matches the ticked status set, so it
    // must not keep rendering under a filter it fails.
    h.tasks = [task("t-aaa", "terminated"), task("t-bbb")];

    emit({ topic: "task", names: { id: "t-aaa" }, ids: ["t-aaa"] });
    await settle();

    expect(view.result.current.tasks.map((t) => t.id)).toEqual(["t-bbb"]);
    expect(h.counts.listTasks).toBe(1);
    expect(h.counts.getTask ?? 0).toBe(0);
    // TWO worker-list reads, and this is the honest price of giving the per-task
    // shortcut back: useTasks' full path re-pulls the worker roster (it resolves
    // the executor chip) and the 外包 RAIL re-pulls its own, because a task delta
    // names a TASK id and the task→worker binding lives on the server. That is
    // exactly the pre-T-8115 cost for this case — the delta's win here is the
    // COALESCING (one decision per burst), not a narrower fetch.
    expect(h.counts.listOutsourceWorkers).toBe(2);
  });

  it("🔴 the NAMED row keeps its server-side dep join — a per-task read would lose it", async () => {
    // The row the delta names must be READ FROM THE LIST, because
    // `GET /api/tasks/{id}` carries no dep_tasks at all (api/dtoParity.ts). Patch
    // this row from the single-item endpoint instead and 「等 T-dep the blocker」
    // collapses to a bare unresolved short id on the card (T-a3e4 regression).
    // NOTE: assert on the row the delta NAMED — a row nobody touched keeps its
    // dep join no matter how the refetch was done, so asserting there proves
    // nothing (that mistake let this very regression through once already).
    const view = await mountedCockpit();
    h.tasks = [task("t-aaa"), task("t-bbb", "in_progress", { title: "renamed" })];

    emit({ topic: "task", names: { id: "t-bbb" }, ids: ["t-bbb"] });
    await settle();

    const named = view.result.current.tasks.find((t) => t.id === "t-bbb");
    expect(named?.title).toBe("renamed"); // the write really landed
    expect(named?.depTasks).toEqual([
      { id: "t-dep", title: "the blocker", status: "done" },
    ]);
  });

  it("a task delta naming a task NOT on screen still re-pulls the list — a new task must appear", async () => {
    const view = await mountedCockpit();
    h.tasks = [task("t-aaa"), task("t-bbb"), task("t-new")];

    emit({ topic: "task", names: { id: "t-new" }, ids: ["t-new"] });
    await settle();

    // The narrowing must never cost us list MEMBERSHIP: a per-task read cannot
    // discover a task the list has never seen.
    expect(view.result.current.tasks.map((t) => t.id)).toEqual([
      "t-aaa",
      "t-bbb",
      "t-new",
    ]);
    expect(h.counts.listTasks).toBe(1);
  });

  it("an outsource_worker delta still re-pulls the rail — release is list membership", async () => {
    const view = await mountedCockpit();
    h.workers = [worker("ow-2")]; // ow-1 was released → it drops out

    emit({
      topic: "outsource_worker",
      names: { id: "ow-1" },
      ids: ["ow-1"],
    });
    await settle();

    expect(view.result.current.workers.map((w) => w.id)).toEqual(["ow-2"]);
    expect(h.counts.listOutsourceWorkers).toBeGreaterThanOrEqual(1);
  });
});

describe("one resync costs one refetch per hook (T-8115)", () => {
  it("fans 12 topics and every hook refetches exactly once", async () => {
    const view = await mountedCockpit();
    h.members = [member(OPEN_PEER, 8), member("m-other"), member("m-third")];

    emitResync();
    await settle();

    // VALUE: a resync is the missed-gap correction, so every snapshot really is
    // re-pulled — this is not a "do less" assertion in disguise.
    expect(view.result.current.members[0].unreadCount).toBe(8);

    // COST: useMembers listens to 4 of the 12 topics, useOutsourceWorkers to 4,
    // useChatUnread to 4, useTasks to 3, useChat to 2. One each.
    expect(h.counts.listMembers).toBe(1);
    expect(h.counts.getChatUnreadCount).toBe(1);
    expect(h.counts.listTasks).toBe(1);
    expect(h.counts.listChat).toBe(1);
    expect(h.counts.listChatReads).toBe(1);
    expect(h.counts.listTaskTypes).toBe(1);
    // Two hooks hold the worker list (useOutsourceWorkers + useTasks) — one each.
    expect(h.counts.listOutsourceWorkers).toBe(2);
    // A resync names nothing, so NO per-item read may be attempted from it.
    expect(h.counts.getMember ?? 0).toBe(0);
    expect(h.counts.getTask ?? 0).toBe(0);
    expect(h.counts.getOutsourceWorker ?? 0).toBe(0);
    // The settings snapshot is shared and generation-guarded — a resync has no
    // settings topic to ride (T-8115 step 3), and must not invent one.
    expect(h.counts.getServerSettings ?? 0).toBe(0);
  });
});

describe("the read echo does not drive another round (T-8115)", () => {
  it("our own read watermark comes back as a delta: the badge clears, nothing re-reads the thread", async () => {
    const view = await mountedCockpit();
    expect(view.result.current.members[0].unreadCount).toBe(3);
    // The owner looked, so the server cleared OUR watermark for this peer and
    // fans it back at us naming US as the reader.
    h.members = [member(OPEN_PEER, 0), member("m-other", 1), member("m-third")];
    h.unread = 1;

    emit({
      topic: "chat_read",
      names: { reader: "owner", peer: OPEN_PEER },
      ids: ["owner", OPEN_PEER],
    });
    await settle();

    // VALUE: the badge really does clear — the echo is not ignored, it is
    // answered proportionately.
    expect(view.result.current.members[0].unreadCount).toBe(0);
    expect(view.result.current.unread).toBe(1);

    // COST: the echo must not re-enter the marking read (that is the loop), and
    // must not re-pull the receipts either — `peerLastReadTs` is the PEER's
    // watermark and this delta says we are the reader, so it cannot move.
    expect(h.counts.listChat ?? 0).toBe(0);
    expect(h.counts.peekChat ?? 0).toBe(0);
    expect(h.counts.listChatReads ?? 0).toBe(0);
    expect(h.counts.getMember).toBe(1);
    expect(h.counts.listMembers ?? 0).toBe(0);
  });

  it("the PEER reading our messages DOES re-pull the receipts", async () => {
    // The mirror image of the test above — without it, "skip the echo" would be
    // satisfied by a hook that stopped tracking read receipts altogether.
    await mountedCockpit();

    emit({
      topic: "chat_read",
      names: { reader: OPEN_PEER, peer: "owner" },
      ids: [OPEN_PEER, "owner"],
    });
    await settle();

    expect(h.counts.listChatReads).toBe(1);
  });

  it("ONE inbound message costs one round, not two — the whole echo path end to end", async () => {
    // The server's real sequence, with the read side effect wired up: the
    // marking listChat advances the watermark, so a chat_read comes back.
    let watermarkBehind = true;
    const echo = () => {
      for (const cb of [...h.handlers])
        cb("chat_read", {
          topic: "chat_read",
          names: { reader: "owner", peer: OPEN_PEER },
          ids: ["owner", OPEN_PEER],
        });
    };
    const view = await mountedCockpit();

    // Re-emit the echo whenever the marking read actually advances anything —
    // dal.go PutChatRead fans ONLY on an advance, so this terminates.
    const withEcho = () => {
      if (!watermarkBehind) return;
      watermarkBehind = false;
      queueMicrotask(() => act(() => echo()));
    };

    h.members = [member(OPEN_PEER, 0), member("m-other", 1), member("m-third")];
    emit({
      topic: "chat",
      names: { id: "cm-9", from: OPEN_PEER, to: "owner" },
      ids: ["cm-9", OPEN_PEER, "owner"],
    });
    withEcho();
    await settle();
    await settle();

    // The thread reloaded exactly ONCE (the message) and the echo did not make
    // it reload again — a second marking read would fan a second echo.
    expect(h.counts.listChat).toBe(1);
    // The roster never re-pulls the company for either delta: the message and the
    // echo each name ONE member, so each costs one member read.
    expect(h.counts.listMembers ?? 0).toBe(0);
    expect(h.counts.getMember).toBe(2);
    expect(view.result.current.members[0].unreadCount).toBe(0);
  });
});
