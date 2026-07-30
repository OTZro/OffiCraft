// CT stories for the 改機器 progress states (T-e0e3): the REAL MemberDetailPanel
// and the REAL WorkerDetailPanel, so the guard measures the ACTUAL 機器 label row
// (`.mp-field__head`, flex + space-between) with the ACTUAL app CSS — the row the
// new `.mp-relocate` column and its notice line now live in.
//
// Why the mock needs a nudge: `useRelocateMachine` disables the button when no
// machine is ONLINE, and the mock's seed warden is offline by design (it never
// fabricates a reachable machine). `__setMockMemberOnline` is the repo's own
// test/dev hook for exactly this; flipping the warden online gives the panels the
// 1-online-machine path, i.e. a click relocates straight to it with no picker.
import { useEffect, useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { MemberDetailPanel } from "../../src/components/MemberDetailPanel";
import { WorkerDetailPanel } from "../../src/components/WorkerDetailPanel";
import type { Member } from "../../src/types";
import type { OutsourceWorkerView } from "../../src/api/adapter";

/** Flip every mock machine online, then report done so the panels mount AFTER
 * the registry says one is reachable (the panels fetch machines on mount). */
function useOnlineMachines(): boolean {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    // DYNAMIC import on purpose: a static one makes Playwright's node-side
    // transform parse mock.ts's `?raw` seed-markdown imports as JavaScript and
    // the whole spec fails to collect. In the browser vite resolves it normally.
    void (async () => {
      const { mockApi, __setMockMemberOnline } = await import(
        "../../src/api/mock"
      );
      const ms = await mockApi.listMachines();
      for (const m of ms) __setMockMemberOnline(m.machineId, true);
      setReady(true);
    })();
  }, []);
  return ready;
}

/** A relocate that is accepted and then never lands — the gap the progress state
 * exists to cover, and the only way to see the 30s timeout at all. */
const neverLands = () => new Promise<void>(() => {});

const member: Member = {
  id: "mira",
  memberId: "MB-AST001",
  name: "Mira",
  role: "assistant",
  status: "offline",
  lifecycle: "offline",
  model: "claude-opus-4-8",
  effort: "high",
  kind: "assistant",
  desiredMachineId: "",
  machine: null,
  account: "shawn-claude",
  contextPct: 42,
  estimatedCost: 7,
  bankedCost: 0,
  tmuxSession: "member-mira",
  refocusSince: null,
  lastOp: "",
  lastOpOk: null,
  lastOpLog: "",
  lastOpAt: null,
  unreadCount: 0,
};

const worker: OutsourceWorkerView = {
  id: "ow-1",
  codename: "O-19",
  model: "claude-opus-4-8",
  effort: "high",
  status: "active",
  taskId: "t-1",
  taskTitle: "改機器進度狀態",
  taskStatus: "in_progress",
  taskNo: "T-e0e3",
  taskTypeKey: "tm-05f7c776d6ff",
  taskTypeName: "OffiCraft 開發",
  presence: "online",
  machine: "Warden · mbp5",
  desiredMachineId: "",
  account: "shawn-claude",
  contextPct: 42,
  cost: 7,
  bankedCost: 0,
  creatorId: "owner",
  delegatedBy: "",
};

/** The member panel's replacement hotspot (T-927a). 改機器 is gone from this
 * panel — the visible pending state is now the 「→ 要換到 ○○」 transition label
 * sitting in the SAME `.mp-field__head` row as the 機器 value, plus the action row
 * that gained a second button (喚醒／更改 beside Stop). Both are width risks jsdom
 * cannot see. Machine ids that the registry does not know fall back to the raw id
 * (MemberDetailPanel's own resolution), which is how this stages the long-label
 * worst case without inventing a mock machine. */
const OBSERVED_MACHINE =
  "eva-m5-mac-studio-primary-worker-node-with-a-very-long-owner-set-label";
const PENDING_MACHINE =
  "seth-m1-mac-mini-spare-runner-node-with-an-equally-long-owner-set-label";

const movingMember: Member = {
  ...member,
  status: "online",
  lifecycle: "online",
  machine: OBSERVED_MACHINE,
  desiredMachineId: PENDING_MACHINE,
};

export function MemberMachineTransitionStory() {
  const ready = useOnlineMachines();
  return (
    <I18nProvider>
      <div className="app__main">
        {ready && (
          <MemberDetailPanel
            member={movingMember}
            onBack={() => {}}
            onRelocate={neverLands}
          />
        )}
      </div>
    </I18nProvider>
  );
}

export function WorkerRelocateProgressStory() {
  const ready = useOnlineMachines();
  return (
    <I18nProvider>
      <div className="app__main">
        {ready && (
          <WorkerDetailPanel
            worker={worker}
            onBack={() => {}}
            onRelocate={neverLands}
          />
        )}
      </div>
    </I18nProvider>
  );
}
