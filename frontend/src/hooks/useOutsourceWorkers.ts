// hooks/useOutsourceWorkers.ts — the office 外包 panel's data (SPEC §4): the
// LIVE outsource-worker roster (codename · 任務狀態 + 任務標題 + the bound task's
// T-xxxx / type / created stamp, ALL riding the worker DTO), ordered 依任務建立
// 時間新→舊, plus the global parallel cap (settings.outsource_max_parallel)
// behind the panel's 「N / 上限」 + 齒輪.
//
// Reconcile-by-refetch (contract B): "outsource_worker" (assignment / release),
// "task" (the bound task's status/title/type echo + the created_ts sort key) and
// "chat" / "chat_read" (the row's unread badge) all re-pull the SAME small list
// — `GET /api/outsource-workers`, nothing else.
//
// 🔴 T-a3e4: it used to also pull `GET /api/tasks` (UNFILTERED — the entire task
// history) and `GET /api/task-manuals` on every worker/task delta, purely to
// join a sort key and two labels onto a handful of rows. The server folds those
// into the worker DTO now (task_no / task_created_ts / task_type_key /
// task_type_name), so the join is gone and with it the split "full vs chat-only"
// refetch that existed only to dodge that download (T-ec2c). Do NOT re-add a
// task-list fetch here: the DTO already carries every field this panel renders.
//
// The cap knob has no SSE topic — the PATCH echo is adopted directly (the same
// server-confirmed-values rule as useServerSettings).

import { useCallback, useEffect, useState } from "react";
import type { OutsourceWorkerView } from "../api/adapter";
import { api } from "../api";
import {
  adoptServerSettings,
  loadServerSettings,
} from "./sharedServerSettings";

interface UseOutsourceWorkers {
  /** LIVE workers, sorted by the bound task's created_ts DESC (新→舊). */
  workers: OutsourceWorkerView[];
  /** True until the first mount fetch settles (parity with useMembers). A
   * caller resolving a chat peer from a worker id must wait for this: an
   * `ow-` chatId that is simply not-yet-loaded is NOT a released worker, and
   * treating it as one would flash the released-peer identity before the live
   * list arrives. */
  loading: boolean;
  /** True when the mount fetch REJECTED — a failed load must never read as
   * "no outsource workers". */
  error: boolean;
  /** The global cap (0 ⇒ assignment paused); null until the settings load
   * (or when it failed) — the panel then omits the cap display honestly. */
  maxParallel: number | null;
  /** PATCH outsource_max_parallel (0..20); adopts the server echo. Rejects
   * (422/network) propagate to the caller for inline error surfacing. */
  saveMaxParallel: (n: number) => Promise<void>;
}

// sortWorkers orders the panel rows 依綁定任務建立時間新→舊. The key is the wire
// `task_created_ts`; a worker whose task cannot be resolved (0) falls back to its
// own mint stamp — an honest proxy, never fabricated.
function sortWorkers(workers: OutsourceWorkerView[]): OutsourceWorkerView[] {
  const sortKey = (x: OutsourceWorkerView) =>
    x.taskCreatedTs || x.createdTs || 0;
  return [...workers].sort((a, b) => sortKey(b) - sortKey(a));
}

export function useOutsourceWorkers(): UseOutsourceWorkers {
  const [workers, setWorkers] = useState<OutsourceWorkerView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const [maxParallel, setMaxParallel] = useState<number | null>(null);

  // ONE refetch path: the workers list carries everything the rows render, so
  // there is nothing left for a delta-specific cheaper variant to skip.
  const refetch = useCallback(async () => {
    setWorkers(sortWorkers(await api.listOutsourceWorkers()));
    setError(false);
  }, []);

  useEffect(() => {
    let alive = true;

    refetch()
      .catch((e) => {
        console.warn("useOutsourceWorkers: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });

    loadServerSettings()
      .then((s) => {
        if (alive) setMaxParallel(s.outsourceMaxParallel);
      })
      .catch((e) =>
        console.warn("useOutsourceWorkers: settings load failed", e)
      );

    const unsubscribe = api.subscribeEvents((topic) => {
      if (
        topic === "outsource_worker" ||
        topic === "task" ||
        topic === "chat" ||
        topic === "chat_read"
      ) {
        refetch().catch((e) =>
          console.warn("useOutsourceWorkers: SSE refetch failed", e)
        );
      }
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [refetch]);

  const saveMaxParallel = useCallback(async (n: number) => {
    const next = await api.patchServerSettings({ outsourceMaxParallel: n });
    adoptServerSettings(next); // shared snapshot invalidation point (T-8115)
    setMaxParallel(next.outsourceMaxParallel);
  }, []);

  return { workers, loading, error, maxParallel, saveMaxParallel };
}
