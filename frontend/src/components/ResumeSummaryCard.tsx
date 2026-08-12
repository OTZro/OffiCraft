import { useState, useEffect, useRef } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import type { MemberResumeSummaryView } from "../api/adapter";
import { ChevronDownIcon, ChevronRightIcon, ClockIcon } from "./icons";
// 🔴 This component draws itself with the `.mp-resume__*` block, so it OWNS
// that stylesheet's import (styleOwnership.test.ts). Both detail panels
// already import member-detail.css, but relying on that is what makes a
// component silently unstyled the day someone renders it somewhere else.
import "./member-detail.css";

// ResumeSummaryCard — 履歷摘要, the wake snapshot the cockpit shows for ONE
// agent, under 初始 PROMPT (afterPromptCards, the panel's last slot).
//
// It lives in its own file because BOTH detail panels render it: the staff
// panel always did, and the outsource panel gained it once the owner released
// the target read for workers (T-4595, ruling rc-64b712bfc703 option ①). The
// alternative — a second copy in WorkerDetailPanel — would have been two
// renderings of the same server payload, free to drift apart with nothing
// watching. `agentId` is the TARGET whose snapshot is fetched, so a worker id
// works exactly as a member id does.
//
// 🔴 HARD REQUIREMENT the panel's default load must not break: no request is
// issued for this section until the FIRST EXPAND. That is why the fetch fn is
// read through a ref (never in the effect's deps — an inline arrow rebuilt
// every render would tear the effect down mid-flight on any repaint, T-7526),
// the effect deps are `[showResumeSummary, agentId]` only, and the loaded
// stamp is written on ARRIVAL (not at fetch start) so a failed read retries.
//
// The `mp-resume-*` test ids are deliberately unchanged from when this lived
// inside MemberDetailPanel: the existing staff-panel tests are the regression
// net for the extraction itself, and renaming them would have thrown that net
// away in the same commit that needed it most.
export function ResumeSummaryCard({ agentId }: { agentId: string }) {
  const { t } = useI18n();
  const [show, setShow] = useState(false);
  const [state, setState] = useState<{
    data: MemberResumeSummaryView | null;
    loading: boolean;
    error: boolean;
  }>({ data: null, loading: false, error: false });
  const loadedKeyRef = useRef<string | null>(null);
  const inFlightKeyRef = useRef<string | null>(null);
  const fetchRef = useRef<() => Promise<MemberResumeSummaryView>>(() =>
    api.getMemberResumeSummary(agentId),
  );
  fetchRef.current = () => api.getMemberResumeSummary(agentId);

  function runFetch(key: string) {
    inFlightKeyRef.current = key;
    setState({ data: null, loading: true, error: false });
    fetchRef
      .current()
      .then((data) => {
        if (inFlightKeyRef.current !== key) return;
        inFlightKeyRef.current = null;
        loadedKeyRef.current = key; // stamped on ARRIVAL only
        setState({ data, loading: false, error: false });
      })
      .catch(() => {
        if (inFlightKeyRef.current !== key) return;
        inFlightKeyRef.current = null;
        // No stamp: the read failed, so re-expanding (or 重試) reads again.
        setState({ data: null, loading: false, error: true });
      });
  }

  useEffect(() => {
    if (!show) return;
    if (loadedKeyRef.current === agentId) return;
    if (inFlightKeyRef.current === agentId) return;
    runFetch(agentId);
    // NO cleanup that cancels the read (a repaint/unmount is not a
    // cancellation); staleness is decided by comparing the key, not an
    // `alive` flag a repaint can flip.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [show, agentId]);

  const stats: Array<[string, string, number]> = state.data
    ? [
        ["chatCount", t.mp.resumeSummary.chatCount, state.data.overview.chatCount],
        ["chatChars", t.mp.resumeSummary.chatChars, state.data.overview.chatChars],
        [
          "tasksReturned",
          t.mp.resumeSummary.tasksReturned,
          state.data.overview.tasksReturned,
        ],
        [
          "tasksOpenTotal",
          t.mp.resumeSummary.tasksOpenTotal,
          state.data.overview.tasksOpenTotal,
        ],
        [
          "tasksDetailChars",
          t.mp.resumeSummary.tasksDetailChars,
          state.data.overview.tasksDetailChars,
        ],
        [
          "cardsWaiting",
          t.mp.resumeSummary.cardsWaiting,
          state.data.overview.cardsWaiting,
        ],
        [
          "cardsAnsweredRecent",
          t.mp.resumeSummary.cardsAnsweredRecent,
          state.data.overview.cardsAnsweredRecent,
        ],
      ]
    : [];

  return (
    <div className="mp-card mp-expand">
      <button
        type="button"
        className="mp-expand__head"
        aria-expanded={show}
        onClick={() => setShow((v) => !v)}
        data-testid="mp-resume-toggle"
      >
        <ClockIcon size={15} className="mp-expand__icon" />
        <span className="mp-expand__title">{t.mp.resumeSummary.title}</span>
        {show ? (
          <ChevronDownIcon size={16} className="mp-expand__chevron" />
        ) : (
          <ChevronRightIcon size={16} className="mp-expand__chevron" />
        )}
      </button>
      {show && (
        <div className="mp-expand__body" data-testid="mp-resume-body">
          {/* `!state.data && !state.error` covers the one render tick between
           * the toggle click and the effect's own setState — treated as
           * loading, not as a fabricated empty state. */}
          {state.loading || (!state.data && !state.error) ? (
            t.mp.resumeSummary.loading
          ) : state.error ? (
            <div data-testid="mp-resume-error">
              <span>{t.mp.resumeSummary.error}</span>{" "}
              <button
                type="button"
                className="doc-btn"
                data-testid="mp-resume-retry"
                onClick={() => runFetch(agentId)}
              >
                {t.mp.resumeSummary.retry}
              </button>
            </div>
          ) : state.data ? (
            <>
              <div className="mp-resume__note">{state.data.note}</div>
              <div
                className="mp-resume__statsgrid"
                data-testid="mp-resume-overview"
              >
                {stats.map(([key, label, value]) => (
                  <div className="mp-resume__stat" key={key}>
                    <div className="mp-resume__statlabel">{label}</div>
                    <div
                      className="mp-resume__statvalue"
                      data-testid={`mp-resume-stat-${key}`}
                    >
                      {value}
                    </div>
                  </div>
                ))}
              </div>

              <div className="mp-resume__section">
                <div className="mp-resume__sectiontitle">
                  {t.mp.resumeSummary.chatSection}
                </div>
                {state.data.chat.length === 0 ? (
                  <div className="mp-resume__empty">
                    {t.mp.resumeSummary.chatEmpty}
                  </div>
                ) : (
                  state.data.chat.map((m) => (
                    <div className="mp-resume__chatrow" key={m.id}>
                      <span className="mp-resume__chatfrom">
                        {m.from === agentId ? "→" : "←"}
                      </span>
                      <span className="mp-resume__chatbody">{m.body}</span>
                    </div>
                  ))
                )}
              </div>

              <div className="mp-resume__section">
                <div className="mp-resume__sectiontitle">
                  {t.mp.resumeSummary.tasksSection}
                </div>
                {state.data.tasks.length === 0 ? (
                  <div className="mp-resume__empty">
                    {t.mp.resumeSummary.tasksEmpty}
                  </div>
                ) : (
                  state.data.tasks.map((rt) => (
                    <div className="mp-resume__taskrow" key={rt.id}>
                      <code className="mp-resume__taskno">{rt.taskNo}</code>
                      <span className="mp-resume__tasktitle">{rt.title}</span>
                      <span className="mp-resume__taskstatus">{rt.status}</span>
                    </div>
                  ))
                )}
              </div>
            </>
          ) : (
            <div className="mp-resume__empty" data-testid="mp-resume-empty">
              {t.mp.resumeSummary.chatEmpty}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
