// hooks/useDocumentHistory.ts — the retained revisions of ONE editable
// long-form document (T-7d33).
//
// Mirrors useGlobalContext / useLessons: mount-fetch + reconcile-by-refetch on
// the document's OWN SSE topic (a restore republishes exactly that topic, so
// the list and the visible doc reconcile off the same signal). `restore` is
// deliberately NOT self-healing: it re-reads the list itself, and leaves
// refreshing the VISIBLE document to the caller that owns it — this hook does
// not know which doc hook is on screen.

import { useCallback, useEffect, useState } from "react";
import type { DocumentHistoryView, DocumentKind } from "../types";
import { api } from "../api";

/** The SSE topic each document kind fans on a write. One home for the mapping,
 * so this list can never end up watching a topic the doc does not publish.
 *
 * NOTE role_definition → "role_def": that is the topic the doc's own writes
 * publish (api_roles.go), the one useRoles listens on, and — since the restore
 * path was fixed on this branch — the one a RESTORE publishes too
 * (publishDocumentHistoryRestore). It used to publish "role", which is outside
 * the closed 12-topic set (hub.go sseTopics) and was dropped at the publish
 * seam, so a restore fanned nothing at all; watching the document's own topic
 * was right then and is what keeps this list reconciling now. */
const TOPIC_OF: Record<DocumentKind, string> = {
  global_context: "global_context",
  role_definition: "role_def",
  lessons: "lessons",
  task_manual: "task_manual",
};

interface UseDocumentHistory {
  /** Retained revisions, newest first (server-ordered, at most 3). */
  versions: DocumentHistoryView[];
  loading: boolean;
  /** True when the mount fetch REJECTED — an honest "could not load" is not
   * the same screen as "this doc has never been edited". */
  error: boolean;
  refetch: () => Promise<void>;
  /** Restore one revision over the live doc, then re-read the list. Rejects on
   * failure so the caller can surface it. */
  restore: (id: number) => Promise<void>;
}

export function useDocumentHistory(
  kind: DocumentKind,
  key: string
): UseDocumentHistory {
  const [versions, setVersions] = useState<DocumentHistoryView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const refetch = useCallback(async () => {
    setVersions(await api.listDocumentHistory(kind, key));
  }, [kind, key]);

  const restore = useCallback(
    async (id: number) => {
      await api.restoreDocumentHistory(kind, key, id);
      setVersions(await api.listDocumentHistory(kind, key));
    },
    [kind, key]
  );

  useEffect(() => {
    let alive = true;

    const load = (onFail: (e: unknown) => void) =>
      api
        .listDocumentHistory(kind, key)
        .then((next) => {
          if (alive) {
            setVersions(next);
            setError(false);
          }
        })
        .catch(onFail);

    load((e) => {
      console.warn("useDocumentHistory: initial load failed", e);
      if (alive) setError(true);
    }).finally(() => {
      if (alive) setLoading(false);
    });

    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic.includes(TOPIC_OF[kind])) {
        void load((e) =>
          console.warn("useDocumentHistory: SSE refetch failed", e)
        );
      }
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [kind, key]);

  return { versions, loading, error, refetch, restore };
}
