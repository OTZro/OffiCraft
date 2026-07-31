// hooks/useTaskCount.ts — the 任務 nav badge's open-task count, kept live, plus
// the unfiltered task TOTAL (T-a3e4).
// Mirrors useReplyCardCount one-for-one: the badge mounts app-wide (App's nav
// bar) and must stay cheap — it rides the dedicated count endpoint
// (GET /api/tasks/count) and refetches on every "task" SSE delta, without ever
// pulling the full task list. `open` is the NON-TERMINAL total (尚未執行/進行中/
// 等我回覆/等待外部; 已完成/終止 never count — spec §1).
//
// `total` (every task, terminal included) exists for the 任務頁's empty state:
// its list fetch asks for the ticked STATUS SET, so an empty list cannot by
// itself justify 「目前沒有任務」 — that is a claim about the whole workshop.
// This is the cheap way to know it, and it is deliberately not a list fetch:
// widening the list just to word a screen is the thing T-a3e4 removed.

import { useEffect, useState } from "react";
import { api } from "../api";

export function useTaskCount(): { open: number; total: number } {
  const [counts, setCounts] = useState({ open: 0, total: 0 });

  useEffect(() => {
    let alive = true;

    const refetch = () => {
      api
        .getTaskCount()
        .then((c) => {
          if (alive) setCounts(c);
        })
        .catch((e) => console.warn("useTaskCount: fetch failed", e));
    };

    refetch();
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic === "task") refetch();
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, []);

  return counts;
}
