// hooks/useTasks.ts — the 任務頁's data: the full task list + the live
// outsource-worker roster + the task-type list, kept fresh the same way as
// useReplyCards. Reconcile-by-refetch (contract B): a "task" SSE delta (create
// / plan / status / priority / terminate, from ANY entry point) → REFETCH the
// list, never merge an event payload; "outsource_worker" refreshes the worker
// roster (assignment / release) and "task_manual" the type-filter options. The
// owner actions (terminate / priority / reassign / message) also refetch
// directly so the mock behaves identically.
//
// T-a3e4: the list is fetched by STATUS SET (`setStatuses`), not by a boolean
// "also include closed". The page hands down what its 狀態 dropdown has ticked
// and the server answers with those rows only, so an SSE-triggered refetch
// costs the ~20 rows on screen instead of the whole task history. `interface
// UseTasks` documents why the boolean had to go rather than be extended.

import { useCallback, useEffect, useRef, useState } from "react";
import type {
  TaskView,
  TaskMessageInput,
  TaskReassignInput,
  OutsourceWorkerView,
  TaskTypeView,
} from "../api/adapter";
import { api } from "../api";
import { createDeltaSink, narrowToHeld } from "../lib/deltaSink";

interface UseTasks {
  /** The tasks the LAST fetch asked for — every status when no set was given,
   * otherwise exactly the ticked ones (T-a3e4). Still unordered/unpartitioned:
   * the page partitions + orders, and applies the executor/type axes, which
   * stay client-side (they are not what made this payload 400 KB). */
  tasks: TaskView[];
  /** LIVE outsource workers — the 外包 executor display resolves through this. */
  workers: OutsourceWorkerView[];
  /** Task types (任務手冊) — the type filter's 各手冊類型 options. */
  taskTypes: TaskTypeView[];
  loading: boolean;
  /** True when the mount fetch REJECTED (500/network; 401 already bounced) —
   * so a failed load never masquerades as the 目前沒有任務 empty state. */
  error: boolean;
  /** Terminate (owner double-confirmed upstream), then refetch. */
  terminate: (id: string) => Promise<void>;
  /** Mark a task a duplicate of the original (T-02c9), then refetch. */
  markDuplicate: (id: string, duplicateOf: string) => Promise<void>;
  /** Priority change incl. freeze/unfreeze, then refetch. */
  setPriority: (id: string, priority: string) => Promise<void>;
  /** 轉派 (T-160e): re-point the task at a member / a freshly minted 外包, then
   * refetch — the move lands the task in `reassigning` and (on an outsource
   * target) changes the worker roster too. */
  reassign: (id: string, input: TaskReassignInput) => Promise<void>;
  /** The task-card message box send (owner → executor). */
  sendMessage: (id: string, msg: TaskMessageInput) => Promise<void>;
  /** Fetch ONE task's FULL detail (steps + description) — the per-card expand
   * hydration path, since the list itself carries only the light projection. */
  getDetail: (id: string) => Promise<TaskView>;
  /** Owner/admin un-pin of one task artifact (T-3dc5), then refetch. */
  removeArtifact: (taskId: string, artifactId: string) => Promise<void>;
  /** Ask the server for EXACTLY these statuses (T-a3e4): the page hands down
   * the set its 狀態 dropdown has ticked and the fetch sends one `?statuses=`
   * per state. `undefined` (or an empty array) = no status constraint, i.e. the
   * full population — what 清除篩選 全部 and a single-task jump anchor need.
   *
   * This REPLACED a boolean `includeClosed` (T-2b9d's `?open=true`). open-only
   * removed the archive but still shipped every live task regardless of the
   * filter, so the default view downloaded rows it had already decided not to
   * render; and the flag had to be re-derived from the rendered list, which is
   * what made it latch. Asking for what is ticked needs no derivation. */
  setStatuses: (statuses: string[] | undefined) => void;
}

// initialStatuses is the status set the CALLER's filter opens on — the page's
// own default, passed in rather than duplicated here. It matters for exactly one
// frame and that frame is the expensive one: the mount fetch happens before any
// effect can state the filter, so a hook that defaulted to "no constraint" would
// download the whole archive on every page open and then immediately re-ask for
// the twenty rows it renders. Pass [] to genuinely open on 所有狀態.
export function useTasks(initialStatuses: string[]): UseTasks {
  const [tasks, setTasks] = useState<TaskView[]>([]);
  const [workers, setWorkers] = useState<OutsourceWorkerView[]>([]);
  const [taskTypes, setTaskTypes] = useState<TaskTypeView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  // The status set the page is currently asking for (T-a3e4). undefined until
  // the page states its filter, and undefined again whenever the view genuinely
  // needs every status (清除篩選 全部 / a single-task anchor that may point at a
  // closed task). Held as a joined KEY, not an array, so that a re-render
  // producing an equal-but-new array does not re-fire the fetch — the page
  // rebuilds its Set on every render, and a raw array in the dep list would
  // refetch the list on every keystroke elsewhere on the page.
  const [statusKey, setStatusKey] = useState<string | undefined>(() =>
    initialStatuses.length === 0
      ? undefined
      : [...initialStatuses].sort().join(",")
  );
  const setStatuses = useCallback((next: string[] | undefined) => {
    setStatusKey(
      next === undefined || next.length === 0
        ? undefined
        : [...next].sort().join(",")
    );
  }, []);

  // The ids on screen + the status set they were asked for, readable from the
  // SSE callback without a stale-closure state read.
  const heldRef = useRef<Set<string>>(new Set());
  // The worker ids the executor display can currently resolve — a task that
  // moved to an 外包 outside this set cannot be rendered from a per-task read.
  const workersRef = useRef<Set<string>>(new Set());
  const statusKeyRef = useRef<string | undefined>(statusKey);
  statusKeyRef.current = statusKey;

  const refetch = useCallback(async () => {
    const [t, w] = await Promise.all([
      api.listTasks(
        statusKey === undefined ? undefined : { statuses: statusKey.split(",") }
      ),
      api.listOutsourceWorkers(),
    ]);
    heldRef.current = new Set(t.map((x) => x.id));
    workersRef.current = new Set(w.map((x) => x.id));
    setTasks(t);
    setWorkers(w);
    setError(false);
  }, [statusKey]);

  const refetchTypes = useCallback(async () => {
    setTaskTypes(await api.listTaskTypes());
  }, []);

  useEffect(() => {
    let alive = true;

    Promise.all([refetch(), refetchTypes()])
      .catch((e) => {
        console.warn("useTasks: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });

    // Re-read exactly the named tasks. The full path costs `?statuses=` rows +
    // the worker roster; a status/priority/plan write on ONE task that is
    // already on screen costs one GET /api/tasks/{id}.
    //
    // Two things make that safe rather than a shortcut. (a) The status set is a
    // SERVER-side filter, so a task whose status left the filter must LEAVE the
    // list — it is dropped here rather than left rendering under a filter it no
    // longer matches. (b) The executor display resolves through `workers`, so a
    // task that moved to an 外包 this hook has never seen falls back to the full
    // path, which re-pulls the roster too. Anything unnamed, or naming a task not
    // on screen (a NEW task — list membership, undiscoverable per-item), is a
    // full re-pull as before.
    const inFilter = (status: string) => {
      const key = statusKeyRef.current;
      return key === undefined || key.split(",").includes(status);
    };
    const patchOne = (ids: string[]) =>
      Promise.all(ids.map((id) => api.getTask(id)))
        .then((fresh) => {
          if (!alive) return false;
          const unknownExecutor = fresh.some(
            (t) =>
              t.executorId.startsWith("ow-") &&
              !workersRef.current.has(t.executorId)
          );
          if (unknownExecutor) return false;
          setTasks((prev) => {
            const next = prev
              .map((t) => fresh.find((f) => f.id === t.id) ?? t)
              .filter((t) => inFilter(t.status));
            heldRef.current = new Set(next.map((t) => t.id));
            return next;
          });
          setError(false);
          return true;
        })
        .catch((e) => {
          console.warn("useTasks: task refetch failed", e);
          return false;
        });

    const full = () =>
      refetch().catch((e) => console.warn("useTasks: SSE refetch failed", e));

    // ONE decision per burst: a resync fans 12 topics synchronously and three of
    // them land here, which used to be two identical list re-pulls plus a types
    // re-pull for one reconnect.
    const unsubscribe = api.subscribeEvents(
      createDeltaSink((batch) => {
        if (batch.topics.has("task_manual")) {
          refetchTypes().catch((e) =>
            console.warn("useTasks: SSE types refetch failed", e)
          );
        }
        const listTopics = [...batch.topics].filter(
          (t) => t === "task" || t === "outsource_worker"
        );
        if (listTopics.length === 0) return;
        const taskOnly = listTopics.every((t) => t === "task");
        // An empty narrowing is a full re-pull HERE (unlike the roster): a task
        // delta naming an id not on screen is most likely a NEW task, and list
        // membership is only discoverable from the list.
        const touched = taskOnly
          ? (narrowToHeld(batch, (id) => heldRef.current.has(id)) ?? [])
          : [];
        if (touched.length === 0) {
          void full();
          return;
        }
        void patchOne(touched).then((ok) => {
          if (!ok) void full();
        });
      })
    );

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [refetch, refetchTypes]);

  const terminate = useCallback(
    async (id: string) => {
      await api.terminateTask(id);
      await refetch();
    },
    [refetch]
  );

  const markDuplicate = useCallback(
    async (id: string, duplicateOf: string) => {
      await api.markTaskDuplicate(id, duplicateOf);
      await refetch();
    },
    [refetch]
  );

  const setPriority = useCallback(
    async (id: string, priority: string) => {
      await api.setTaskPriority(id, priority);
      await refetch();
    },
    [refetch]
  );

  const reassign = useCallback(
    async (id: string, input: TaskReassignInput) => {
      await api.reassignTask(id, input);
      await refetch();
    },
    [refetch]
  );

  const sendMessage = useCallback(
    async (id: string, msg: TaskMessageInput) => {
      await api.postTaskMessage(id, msg);
    },
    []
  );

  const getDetail = useCallback((id: string) => api.getTask(id), []);

  const removeArtifact = useCallback(
    async (taskId: string, artifactId: string) => {
      await api.removeTaskArtifact(taskId, artifactId);
      await refetch();
    },
    [refetch]
  );

  return {
    tasks,
    workers,
    taskTypes,
    loading,
    error,
    terminate,
    markDuplicate,
    setPriority,
    reassign,
    sendMessage,
    getDetail,
    removeArtifact,
    setStatuses,
  };
}
