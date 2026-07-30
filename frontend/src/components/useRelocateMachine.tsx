import { useEffect, useRef, useState, type ReactNode } from "react";
import { useI18n } from "../i18n";
import type { MachineView, MemberRelocateResult } from "../types";
import { MachinePicker } from "./MachinePicker";
import { PencilIcon } from "./icons";

/** How long 「更換中…」 may stay up before the control calls it a timeout.
 *
 * A relocate lands ASYNCHRONOUSLY: the POST answering 200 only means the pin was
 * written — the agent actually arriving on the new machine comes back later as an
 * SSE delta (`landed`). So the in-progress state cannot end at the promise, and
 * without a ceiling it would spin forever whenever the move never lands. ONE
 * named constant, so the wait is tunable in exactly one place.
 *
 * ⚠️ KNOWN BOUNDARY, decided not overlooked (owner ruling 2): the progress state
 * is purely IN-MEMORY. A page reload or a component remount forgets that a move
 * was fired, and this clock restarts from zero. Surviving a reload would mean a
 * SECOND source of truth for "a relocate is in flight" (server-side fired-at, or
 * localStorage) — judged not worth its cost against `landed`, which already
 * self-heals whatever the owner sees after a reload. */
export const RELOCATE_TIMEOUT_MS = 30_000;

/** How long the one-shot 「已送出」 chip stays up after a relocate that has no
 * observable landing to wait for (the same-machine case below).
 *
 * It is NOT a small timeout and must never be read as one: it promises nothing
 * about the move landing, it only states that the request left. Being transient
 * is the point — a permanent chip would start looking like a status. */
export const RELOCATE_SENT_NOTICE_MS = 2_000;

/** The 改機器 control's visible progress state.
 *
 *   idle       — the ordinary 編輯 affordance.
 *   relocating — fired, not yet landed: 「更換中…」 + no re-fire (owner: 「一直狀態
 *                沒變，按鈕沒變，會讓使用者誤以為沒有按到」).
 *   timeout    — RELOCATE_TIMEOUT_MS elapsed without `landed`. Honest dead end,
 *                and RETRYABLE: the button goes live again.
 *   failed     — `onRelocate` rejected (a real path: the openapi-fetch middleware
 *                throws on every non-2xx). Also retryable.
 */
type RelocatePhase = "idle" | "relocating" | "timeout" | "failed";

interface UseRelocateMachineOpts {
  /** The agent this control is bound to (`member.id` / `worker.id`).
   *
   * 🔴 REQUIRED, and it is the notice's identity — not decoration (review r2,
   * the relocate twin of r1 SHOULD-1). Neither detail panel is remounted when
   * the owner selects a different agent (no `key` at either call site), so this
   * hook's `undispatched` verdict outlives the agent it was decided for unless
   * something resets it.
   *
   * ⚠️ SCOPE OF THAT CLAIM — and this comment splits by PROVENANCE, because an
   * earlier draft of it over-claimed twice (see the commit that fixed it):
   *
   *   MEASURED (r3's 2-factor experiment): reverting the observability gate did
   *   NOT turn the leak test green. So this is a PRE-EXISTING leak that three
   *   review rounds missed — NOT a regression introduced alongside that gate.
   *
   *   DERIVED, not measured (from r2's probe P2 plus the `undispatched &&
   *   !landed` boolean; nobody ran a positive control for it): the gate masks
   *   the leak only for agents that are BOTH unobservable AND pinned, where
   *   `landed` comes out accidentally true and swallows it. Treat this half as
   *   a reading of the code, not as an experimental result. */
  subjectId: string;
  /** ALL machines (online + offline) — the caller's ONE useMachines() result. */
  machines: MachineView[];
  /** The agent's owner-pinned machine id (desiredMachineId), or null. */
  boundMachineId: string | null;
  /** Fire the relocate. Undefined ⇒ no button (the affordance is hidden). The
   * panels lean on the member / outsource_worker SSE refetch for the post-move
   * refresh, so the handler need only fire. */
  onRelocate?: (
    machineId: string,
  ) => void | Promise<MemberRelocateResult | void>;
  testId: string;
  pickerTitle: string;
  pickerConfirmLabel: string;
  /** Tooltip shown on the disabled button when no machine is online. */
  noOnlineTitle: string;
  /** Render the pencil icon inside the button (the member panel's look). */
  withIcon?: boolean;
  /** The agent's CURRENT machine (`member.machine`), i.e. where it actually is —
   * as opposed to `boundMachineId`, which is only where the owner PINNED it.
   *
   * 🔴 This is the self-heal signal for the "move scheduled, not landed" notice
   * (review r1 SHOULD-2). Without it `undispatched` was cleared ONLY by another
   * relocate attempt, so once the background cadence actually landed the move,
   * the panel kept telling the owner it had not — the notice's own promise
   * ("the server keeps retrying") had no path back. When the current machine
   * reaches the pin, the move HAS landed and the notice is false.
   *
   * 🔴 CALLERS MUST PASS null WHEN THE PLACEMENT IS NOT OBSERVABLE (review r2).
   * `member.machine` is NOT a pure observation: `observedHost`
   * (`server/ocserverd/api_helpers.go:240-253`) falls back to
   * `m.DesiredMachineID` when the hub has no session and telemetry has nothing
   * — so for a member nobody can see, `member.machine === desiredMachineId` is
   * TRUE BY CONSTRUCTION and means "we do not know where it is", never "it got
   * there". Feeding that in raw made the notice self-heal on a move that had
   * not happened. MemberDetailPanel already gates its 機器 cell on `awake` for
   * this exact reason (its own comment names the desired_machine residual); the
   * self-heal signal is gated with it. */
  currentMachineId?: string | null;
  /** The subject's LAST-OPERATION RECEIPT, when the wire carries one (outsource
   * worker rows do: `last_op` / `last_op_ok` / `last_op_reason` / `last_op_at`).
   *
   * 🔴 This is the FAILURE half of the relocate lifecycle. `landed` only ever
   * says "it worked"; without a receipt the ONLY exit from 「更換中…」 for a move
   * the server refused is the full RELOCATE_TIMEOUT_MS — 30 seconds of spinner
   * for an answer the server already wrote down. When the receipt CHANGES while
   * a relocate is in flight and reports `ok === false`, the wait ends
   * immediately, the reason is shown verbatim, and the button becomes the retry.
   *
   * Edge-detected on `at`, never on the value: a stale receipt from BEFORE this
   * relocate must not fail the new attempt, and comparing timestamps against the
   * browser's clock would make that correctness depend on clock skew. The `at`
   * seen at fire time is the baseline; any different `at` afterwards is news. */
  dispatchReceipt?: {
    at?: number | null;
    ok?: boolean | null;
    reason?: string;
  } | null;
}

/**
 * The ONE 改機器 control both detail panels render (P7b convergence): the 編輯
 * button next to the 機器 label plus its machine-picker overlay, with the
 * shared 0/1/2+ online rule — 0 → disabled (tooltip), 1 → move straight to it,
 * 2+ → open the picker. Placement-only: it never wakes the agent.
 */
export function useRelocateMachine({
  subjectId,
  machines,
  boundMachineId,
  onRelocate,
  testId,
  pickerTitle,
  pickerConfirmLabel,
  noOnlineTitle,
  withIcon,
  currentMachineId,
  dispatchReceipt,
}: UseRelocateMachineOpts): {
  relocateAction: ReactNode | undefined;
  relocatePicker: ReactNode | undefined;
  /** True once a relocate came back `relocation_pending` (T-7fa1). */
  relocateUndispatched: boolean;
} {
  const { t } = useI18n();
  const [pickerOpen, setPickerOpen] = useState(false);
  const [phase, setPhase] = useState<RelocatePhase>("idle");
  // T-7fa1: the relocate answered 200 but its recycle STOP/START never reached a
  // warden — pinned, not landed. Surfaced by the caller as a DispatchAlert.
  //
  // 🔴🔴 TWIN IMPLEMENTATION — the notice hygiene below (self-heal, the
  // observability gate, the subject-keyed reset, the in-flight subject guard)
  // also exists, hand-written, in MemberDetailPanel: that panel folded relocate
  // into its unified settings submit and stopped driving this hook. Change the
  // two TOGETHER. They were allowed to drift once, and the missing half shipped
  // as a notice that never went away.
  const [undispatched, setUndispatched] = useState(false);
  // The verdict AND the progress phase are about ONE agent: drop both when the
  // panel switches to another. Neither panel is remounted on that switch (no
  // `key` at either call site — see subjectId's doc), so a 「更換中…」 left over
  // from the previous agent would otherwise be shown against the new one.
  // Bumped on every fire that shows the one-shot 「已送出」 chip; 0 = no chip. A
  // NONCE rather than a bool so a second fire re-arms the 2s window instead of
  // riding out the first one's.
  const [sentNonce, setSentNonce] = useState(0);
  // The server's own words for why the last operation failed, shown under the
  // failure notice. Cleared with every fresh attempt so a healed row cannot keep
  // explaining a move that already succeeded.
  const [failureReason, setFailureReason] = useState("");
  useEffect(() => {
    setUndispatched(false);
    setPhase("idle");
    setSentNonce(0);
    setFailureReason("");
    setPickerOpen(false);
  }, [subjectId]);
  useEffect(() => {
    if (sentNonce === 0) return;
    const timer = setTimeout(() => setSentNonce(0), RELOCATE_SENT_NOTICE_MS);
    return () => clearTimeout(timer);
  }, [sentNonce]);
  // …and the reset above is a reset, not a CANCEL: a relocate still in flight
  // during the switch resolves afterwards. Same render-time ref discipline as
  // MemberDetailPanel's wake.
  const subjectIdRef = useRef(subjectId);
  subjectIdRef.current = subjectId;
  const onlineMachines = machines.filter((m) => m.online);

  // Self-heal: the pin is where it was ASKED to be, `currentMachineId` is where
  // it is OBSERVED to be (null when nobody can see it — see the prop doc).
  // Satisfied ⇒ the move landed ⇒ the notice is stale.
  //
  // The null/"" guards are load-bearing, not defensive noise: an unpinned member
  // (`desiredMachineId: ""` → boundMachineId null) with no observed machine
  // would otherwise compare null === null and swallow a live verdict.
  //
  // A pin is now always a CONCRETE machine id (`types.ts`: "" = unpinned, else a
  // machine id) — the "auto" pseudo-pin it used to have to special-case is gone,
  // rejected at the API and normalized out of storage, so plain equality is the
  // whole rule again: the move landed exactly when the member is observed on the
  // machine it was pinned to.
  const observedMachineId =
    currentMachineId != null && currentMachineId !== "" ? currentMachineId : null;
  const landed =
    observedMachineId != null && observedMachineId === boundMachineId;
  // `landed` is also what ENDS 「更換中…」: the move is over when the agent is
  // observed where it was pinned, not when the POST answers. Clearing from
  // `timeout` too is deliberate — a late-landing move heals its own dead end.
  useEffect(() => {
    if (landed) {
      setUndispatched(false);
      setPhase("idle");
    }
  }, [landed]);

  // ── the server-recorded outcome (the FAILURE exit from 「更換中…」) ──────────
  // A relocate the server refused is knowable long before the 30s ceiling: the
  // worker row carries a receipt for every non-dispatch. Baseline is whatever
  // receipt existed WHEN THE MOVE WAS FIRED, so only a NEW one is this move's
  // answer — an old failure must not condemn a fresh attempt.
  const receiptAt = dispatchReceipt?.at ?? null;
  const receiptBaselineRef = useRef<number | null>(receiptAt);
  useEffect(() => {
    if (phase !== "relocating") return;
    if (receiptAt == null || receiptAt === receiptBaselineRef.current) return;
    receiptBaselineRef.current = receiptAt;
    // ok === false is the only definite refusal. `true`/null mean the server has
    // nothing bad to say, and success is `landed`'s job to declare — reading a
    // successful receipt as "arrived" would repeat the exact mistake this whole
    // change exists to stop (a dispatch is not an arrival).
    if (dispatchReceipt?.ok === false) {
      setFailureReason((dispatchReceipt.reason ?? "").trim());
      setPhase("failed");
    }
  }, [phase, receiptAt, dispatchReceipt?.ok, dispatchReceipt?.reason]);

  // The ceiling on 「更換中…」. Re-armed on every entry into `relocating`, torn
  // down the moment the phase leaves it (landed / failed / agent switch), so a
  // move that lands can never be reported as a timeout afterwards.
  useEffect(() => {
    if (phase !== "relocating") return;
    const timer = setTimeout(() => setPhase("timeout"), RELOCATE_TIMEOUT_MS);
    return () => clearTimeout(timer);
  }, [phase]);

  // Read at fire time, so two clicks inside ONE render batch (both closing over
  // the same stale `phase`) still only relocate once.
  const phaseRef = useRef(phase);
  phaseRef.current = phase;

  const run = (machineId: string) => {
    if (phaseRef.current === "relocating") return;
    setPickerOpen(false);
    setUndispatched(false); // a fresh attempt clears the previous verdict
    setFailureReason("");
    // Re-baseline: from here on, only a receipt written AFTER this click is an
    // answer to it.
    receiptBaselineRef.current = receiptAt;
    // 🔴 A move to the machine the agent is ALREADY OBSERVED ON never enters
    // 「更換中…」 (owner ruling 3a). There is no observable transition to wait
    // for — `observedMachineId` is already the target and stays it right through
    // the server's same-machine recycle — so the progress state would have
    // exactly ONE exit, the timeout, and would end up asserting a move had not
    // happened when in fact one had. The relocate is still FIRED (this is not a
    // no-op short-circuit); only the unwinnable wait is skipped, so the button
    // stays live.
    const observable = machineId !== observedMachineId;
    if (observable) {
      phaseRef.current = "relocating";
      setPhase("relocating");
      setSentNonce(0); // 「更換中…」 says everything the chip would
    } else {
      setPhase("idle"); // also drops a stale timeout/failed notice
      // …but 「狀態沒變、按鈕沒變」 is EXACTLY the complaint this ticket exists to
      // fix (owner's own words), and this branch changes neither. So it gets the
      // one thing we honestly know: the request went out. A fact already true at
      // this point — no promise about landing, no 30s clock.
      setSentNonce((n) => n + 1);
    }
    const firedFor = subjectId; // whose move this verdict belongs to
    void (async () => {
      try {
        const result = await onRelocate?.(machineId);
        if (subjectIdRef.current !== firedFor) return;
        // 🔴 PENDING IS A VERDICT, NOT PROGRESS (owner ruling 1). The server has
        // given a definitive answer — nothing went out — so holding 「更換中…」
        // would be pretending something is still under way. The DispatchAlert
        // REPLACES the spinner rather than stacking on top of it (the same call
        // the wake side made in T-7fa1: that copy exists precisely to take the
        // place of 「永遠轉不完的『喚醒中…』」). Nothing is in flight, so the button
        // goes live again; the alert self-heals on `landed`.
        if (result?.relocationPending) {
          setUndispatched(true);
          setPhase("idle");
        }
      } catch {
        if (subjectIdRef.current !== firedFor) return;
        setPhase("failed");
        // NIT-3 (review r1): the openapi-fetch middleware throws on EVERY
        // non-2xx, so a rejected relocate is a real path, not a theoretical
        // one. Before this it escaped as an unhandled rejection. There is no
        // verdict to show — a rejected relocate never reached the server's
        // pending determination — so we only make sure a STALE verdict does
        // not survive the new attempt.
        setUndispatched(false);
      }
      // NO `finally` that ends the progress state: the promise resolving means
      // the pin was written, NOT that the agent moved. 「更換中…」 outlives the
      // await and is ended by `landed` or by RELOCATE_TIMEOUT_MS.
    })();
  };

  const relocating = phase === "relocating";
  const canRelocate = Boolean(onRelocate) && onlineMachines.length >= 1;
  const handleClick =
    canRelocate && !relocating
      ? () => {
          if (onlineMachines.length === 1) run(onlineMachines[0].machineId);
          else setPickerOpen(true);
        }
      : undefined;

  // The dead end (timeout / rejected) is stated next to the button rather than
  // inside it, so the button itself can stay the RETRY affordance.
  const notice =
    phase === "timeout"
      ? t.machine.relocateTimeout
      : phase === "failed"
        ? t.machine.relocateFailed
        : undefined;

  const relocateAction = onRelocate ? (
    <div className="mp-relocate">
      <button
        type="button"
        className="doc-btn doc-btn--edit"
        data-testid={testId}
        disabled={!handleClick}
        title={onlineMachines.length === 0 ? noOnlineTitle : undefined}
        onClick={handleClick}
      >
        {withIcon && !relocating && <PencilIcon size={14} />}
        <span>{relocating ? t.machine.relocating : t.settings.edit}</span>
      </button>
      {notice ? (
        <>
          <div
            className="mp-field__hint mp-info2__error"
            data-testid={`${testId}-notice`}
          >
            {notice}
          </div>
          {phase === "failed" && failureReason ? (
            // The server's own receipt, verbatim. It names the machine and the
            // cause; paraphrasing it here would be a second, drifting copy of a
            // diagnosis that only the server can make.
            <div
              className="mp-field__hint mp-relocate__reason"
              data-testid={`${testId}-reason`}
            >
              {failureReason}
            </div>
          ) : null}
        </>
      ) : sentNonce > 0 ? (
        <div className="mp-field__hint" data-testid={`${testId}-sent`}>
          {t.machine.relocateSent}
        </div>
      ) : null}
    </div>
  ) : undefined;

  const relocatePicker = pickerOpen ? (
    <MachinePicker
      machines={machines}
      boundMachineId={boundMachineId}
      title={pickerTitle}
      confirmLabel={pickerConfirmLabel}
      onConfirm={run}
      onCancel={() => setPickerOpen(false)}
    />
  ) : undefined;

  return {
    relocateAction,
    relocatePicker,
    // `landed` wins over a stale flag even before the effect flushes.
    relocateUndispatched: undispatched && !landed,
  };
}
