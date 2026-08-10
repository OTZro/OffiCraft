import { useState } from "react";
import { useI18n } from "../i18n";
import { useScheduledMessages } from "../hooks/useScheduledMessages";
import { formatAbsolute } from "../lib/dateFormat";
import type {
  ScheduleCadence,
  ScheduledMessage,
  ScheduledMessageCreateInput,
} from "../api/adapter";
import { ConfirmModal } from "./ConfirmModal";
import {
  ChevronDownIcon,
  ChevronRightIcon,
  ClockIcon,
  PencilIcon,
  TrashIcon,
} from "./icons";
// 🔴 This component draws itself with the `.mp-schedmsg__*` block, so it OWNS
// that stylesheet's import (T-7526 / styleOwnership.test.ts). Both detail
// panels already import member-detail.css, but relying on that is exactly the
// transitive free ride the guard exists to forbid.
import "./member-detail.css";

/** The zone a fresh schedule starts on. It is a starting VALUE, not a floor:
 * the field below is editable, because a defaulted-and-locked zone is how
 * "whenever the server happens to run" creeps back in. */
const DEFAULT_TIMEZONE = "Asia/Taipei";

/** Days that do not exist in every month. Choosing one of these is legal
 * (owner ruling 2026-08-10, card rc-aeef15360ab5: keep the RFC 5545 range) and
 * costs the months that lack the day — which is why picking one shows the
 * skip hint right beside the field. */
const SPARSE_DAYS = new Set([29, 30, 31]);

/** How many lines of a message body the row shows before the 展開 control
 * appears. Mirrors `-webkit-line-clamp` in `.mp-schedmsg__text--clamped`. */
const CLAMP_LINES = 3;

/** Roughly what fits on one clamped line at the detail panel's width. It only
 * decides whether the 展開/收合 control is OFFERED; the clamp itself is CSS and
 * the stored text is never rewritten. */
const CLAMP_CHARS_PER_LINE = 46;

const HOURS = Array.from({ length: 24 }, (_, i) => i);
const MINUTES = Array.from({ length: 60 }, (_, i) => i);
const DAYS_OF_MONTH = Array.from({ length: 31 }, (_, i) => i + 1);

function pad2(n: number): string {
  return String(n).padStart(2, "0");
}

/** Whether a body is long enough that the clamp would hide part of it. A
 * DISPLAY question only — nothing here reaches the value that is stored or
 * sent. */
function isClampable(body: string): boolean {
  return (
    body.split("\n").length > CLAMP_LINES ||
    body.length > CLAMP_LINES * CLAMP_CHARS_PER_LINE
  );
}

/** Everything about a schedule an owner can set. The create form and the
 * per-row edit form drive the SAME shape through the same fields, so the two
 * cannot drift into offering different settings. */
interface ScheduleFormValues {
  label: string;
  body: string;
  cadence: ScheduleCadence;
  dayOfWeek: number;
  dayOfMonth: number;
  hour: number;
  minute: number;
  timezone: string;
}

const BLANK_FORM: ScheduleFormValues = {
  label: "",
  body: "",
  cadence: "daily",
  dayOfWeek: 1,
  dayOfMonth: 1,
  hour: 9,
  minute: 0,
  timezone: DEFAULT_TIMEZONE,
};

function formValuesOf(m: ScheduledMessage): ScheduleFormValues {
  return {
    label: m.label,
    body: m.body,
    cadence: m.cadence,
    dayOfWeek: m.dayOfWeek,
    dayOfMonth: m.dayOfMonth,
    hour: m.hour,
    minute: m.minute,
    timezone: m.timezone,
  };
}

/** The wire payload for a create AND for a save — same fields either way, so an
 * edit can reach every setting a create can. */
function wirePayload(v: ScheduleFormValues): ScheduledMessageCreateInput {
  return {
    label: v.label.trim(),
    body: v.body.trim(),
    cadence: v.cadence,
    // Only the field the chosen cadence actually reads rides the wire —
    // sending the other one would write a value nothing will ever apply.
    ...(v.cadence === "weekly" ? { dayOfWeek: v.dayOfWeek } : {}),
    ...(v.cadence === "monthly" ? { dayOfMonth: v.dayOfMonth } : {}),
    hour: v.hour,
    minute: v.minute,
    timezone: v.timezone.trim(),
  };
}

function incomplete(v: ScheduleFormValues): boolean {
  return v.body.trim() === "" || v.timezone.trim() === "";
}

interface ScheduleFieldsProps {
  /** Prefix for this form instance's test ids. The create form and any open
   * row editor are on screen at the same time, so their fields cannot share
   * one id. */
  idPrefix: string;
  values: ScheduleFormValues;
  onChange: (patch: Partial<ScheduleFormValues>) => void;
  autoFocus?: boolean;
}

/** The schedule fields themselves — one set, rendered by both the create form
 * and the per-row editor. */
function ScheduleFields({
  idPrefix,
  values,
  onChange,
  autoFocus,
}: ScheduleFieldsProps) {
  const { t } = useI18n();
  const s = t.mp.schedmsg;
  const weekdayNames = weekdayNamesOf(s);

  // The form's OWN day-of-month choice is what the hint reacts to, so the cost
  // is visible WHILE choosing rather than inferred months later from "why has
  // February been silent".
  const showSkipHint =
    values.cadence === "monthly" && SPARSE_DAYS.has(values.dayOfMonth);

  return (
    <>
      <label className="mp-schedmsg__field">
        <span className="mp-schedmsg__fieldlabel">{s.labelLabel}</span>
        <input
          type="text"
          className="mp-schedmsg__input"
          placeholder={s.labelPlaceholder}
          value={values.label}
          onChange={(e) => onChange({ label: e.target.value })}
          autoFocus={autoFocus}
          data-testid={`${idPrefix}-label-input`}
        />
      </label>
      <label className="mp-schedmsg__field">
        <span className="mp-schedmsg__fieldlabel">{s.bodyLabel}</span>
        <textarea
          className="mp-schedmsg__input mp-schedmsg__textarea"
          placeholder={s.bodyPlaceholder}
          rows={3}
          value={values.body}
          onChange={(e) => onChange({ body: e.target.value })}
          data-testid={`${idPrefix}-body-input`}
        />
      </label>
      <label className="mp-schedmsg__field">
        <span className="mp-schedmsg__fieldlabel">{s.cadenceLabel}</span>
        <select
          className="mp-schedmsg__input mp-schedmsg__select"
          value={values.cadence}
          onChange={(e) =>
            onChange({ cadence: e.target.value as ScheduleCadence })
          }
          data-testid={`${idPrefix}-cadence`}
        >
          <option value="daily">{s.cadenceDaily}</option>
          <option value="weekly">{s.cadenceWeekly}</option>
          <option value="monthly">{s.cadenceMonthly}</option>
        </select>
      </label>
      {/* The day field belongs to exactly one cadence, so the other one is not
          rendered at all — a disabled-but-visible control would suggest the
          value still means something. */}
      {values.cadence === "weekly" && (
        <label className="mp-schedmsg__field">
          <span className="mp-schedmsg__fieldlabel">{s.dayOfWeekLabel}</span>
          <select
            className="mp-schedmsg__input mp-schedmsg__select"
            value={values.dayOfWeek}
            onChange={(e) => onChange({ dayOfWeek: Number(e.target.value) })}
            data-testid={`${idPrefix}-dayofweek`}
          >
            {weekdayNames.map((name, i) => (
              <option key={i} value={i}>
                {name}
              </option>
            ))}
          </select>
        </label>
      )}
      {values.cadence === "monthly" && (
        <label className="mp-schedmsg__field">
          <span className="mp-schedmsg__fieldlabel">{s.dayOfMonthLabel}</span>
          <select
            className="mp-schedmsg__input mp-schedmsg__select"
            value={values.dayOfMonth}
            onChange={(e) => onChange({ dayOfMonth: Number(e.target.value) })}
            data-testid={`${idPrefix}-dayofmonth`}
          >
            {DAYS_OF_MONTH.map((d) => (
              <option key={d} value={d}>
                {d}
              </option>
            ))}
          </select>
          {showSkipHint && (
            <span
              className="mp-schedmsg__skiphint"
              data-testid={`${idPrefix}-skip-hint`}
            >
              {s.dayOfMonthSkipHint}
            </span>
          )}
        </label>
      )}
      <div className="mp-schedmsg__timerow">
        <label className="mp-schedmsg__field">
          <span className="mp-schedmsg__fieldlabel">{s.hourLabel}</span>
          <select
            className="mp-schedmsg__input mp-schedmsg__select"
            value={values.hour}
            onChange={(e) => onChange({ hour: Number(e.target.value) })}
            data-testid={`${idPrefix}-hour`}
          >
            {HOURS.map((h) => (
              <option key={h} value={h}>
                {pad2(h)}
              </option>
            ))}
          </select>
        </label>
        <label className="mp-schedmsg__field">
          <span className="mp-schedmsg__fieldlabel">{s.minuteLabel}</span>
          <select
            className="mp-schedmsg__input mp-schedmsg__select"
            value={values.minute}
            onChange={(e) => onChange({ minute: Number(e.target.value) })}
            data-testid={`${idPrefix}-minute`}
          >
            {MINUTES.map((m) => (
              <option key={m} value={m}>
                {pad2(m)}
              </option>
            ))}
          </select>
        </label>
      </div>
      <label className="mp-schedmsg__field">
        <span className="mp-schedmsg__fieldlabel">{s.timezoneLabel}</span>
        <input
          type="text"
          className="mp-schedmsg__input"
          placeholder={s.timezonePlaceholder}
          value={values.timezone}
          onChange={(e) => onChange({ timezone: e.target.value })}
          data-testid={`${idPrefix}-timezone`}
        />
      </label>
    </>
  );
}

type SchedMsgStrings = ReturnType<typeof useI18n>["t"]["mp"]["schedmsg"];

function weekdayNamesOf(s: SchedMsgStrings): string[] {
  return [
    s.weekdaySun,
    s.weekdayMon,
    s.weekdayTue,
    s.weekdayWed,
    s.weekdayThu,
    s.weekdayFri,
    s.weekdaySat,
  ];
}

interface ScheduledMessagesCardProps {
  /** The recipient. May be an assistant OR an `ow-` outsource worker — the same
   * recipient rule ordinary chat uses, which is why BOTH detail panels render
   * this one component instead of each keeping a copy. */
  memberId: string;
}

/**
 * 定期訊息 · the collapsible schedule card (T-f059) — the webhook card's
 * clock-driven twin: a schedule fires on a recurring wall-clock slot and the
 * server delivers its text as an ordinary chat message to the bound member.
 *
 * ONE component, rendered by BOTH detail panels through the shared panel's
 * `extraExpandCards` slot (member: MemberDetailPanel, outsource:
 * WorkerDetailPanel). A second copy of this JSX is the bug, not the feature.
 */
export function ScheduledMessagesCard({ memberId }: ScheduledMessagesCardProps) {
  const { t } = useI18n();
  const s = t.mp.schedmsg;
  const {
    items,
    error: loadError,
    create,
    update,
    remove,
  } = useScheduledMessages(memberId);

  const [expanded, setExpanded] = useState(false);
  const [adding, setAdding] = useState(false);
  const [newValues, setNewValues] = useState<ScheduleFormValues>(BLANK_FORM);
  const [createBusy, setCreateBusy] = useState(false);
  const [createError, setCreateError] = useState(false);
  const [editId, setEditId] = useState<string | null>(null);
  const [editValues, setEditValues] = useState<ScheduleFormValues>(BLANK_FORM);
  const [editBusy, setEditBusy] = useState(false);
  const [editError, setEditError] = useState(false);
  const [toggleBusyId, setToggleBusyId] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ScheduledMessage | null>(
    null
  );
  const [deleteBusy, setDeleteBusy] = useState(false);
  // Per ROW, not per card: one shared switch would expand every message the
  // moment an owner wanted to read one of them.
  const [shownInFull, setShownInFull] = useState<string[]>([]);

  const weekdayNames = weekdayNamesOf(s);

  function cadenceText(m: ScheduledMessage): string {
    if (m.cadence === "weekly") return s.weeklyOn(weekdayNames[m.dayOfWeek] ?? "");
    if (m.cadence === "monthly") return s.monthlyOn(m.dayOfMonth);
    return s.cadenceDaily;
  }

  function resetForm() {
    setAdding(false);
    setNewValues(BLANK_FORM);
    setCreateError(false);
  }

  const createDisabled = createBusy || incomplete(newValues);

  async function submitCreate() {
    if (createDisabled) return;
    setCreateBusy(true);
    setCreateError(false);
    try {
      await create(wirePayload(newValues));
      resetForm();
    } catch {
      setCreateError(true);
    } finally {
      setCreateBusy(false);
    }
  }

  /** Enter the row editor seeded from what the SERVER currently holds. Cancel
   * throws the draft away and the row goes back to reading from `items`, so
   * there is no edited-but-unsaved value left anywhere. */
  function beginEdit(m: ScheduledMessage) {
    setEditId(m.id);
    setEditValues(formValuesOf(m));
    setEditError(false);
  }

  function cancelEdit() {
    setEditId(null);
    setEditValues(BLANK_FORM);
    setEditError(false);
  }

  const editDisabled = editBusy || incomplete(editValues);

  async function submitEdit(scheduleId: string) {
    if (editDisabled) return;
    setEditBusy(true);
    setEditError(false);
    try {
      await update(scheduleId, wirePayload(editValues));
      // Only a resolved save closes the editor. A failed one leaves the draft
      // on screen with its error — the row must never re-appear wearing values
      // the server never accepted.
      setEditId(null);
      setEditValues(BLANK_FORM);
    } catch {
      setEditError(true);
    } finally {
      setEditBusy(false);
    }
  }

  async function toggle(m: ScheduledMessage) {
    if (toggleBusyId) return;
    setToggleBusyId(m.id);
    try {
      await update(m.id, {
        status: m.status === "enabled" ? "disabled" : "enabled",
      });
    } catch {
      // the refetch inside `update` keeps truth; the row stays on its prior state
    } finally {
      setToggleBusyId(null);
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    setDeleteBusy(true);
    try {
      await remove(deleteTarget.id);
      setDeleteTarget(null);
    } finally {
      setDeleteBusy(false);
    }
  }

  function toggleFullBody(id: string) {
    setShownInFull((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]
    );
  }

  // Only decides whether formatAbsolute prefixes the year, so a render-time
  // read is enough (the TaskArtifactsPopover pattern) — no ticking counter.
  const nowTs = Date.now() / 1000;

  return (
    <>
      <div className="mp-card mp-expand mp-schedmsg">
        <button
          type="button"
          className="mp-expand__head"
          aria-expanded={expanded}
          onClick={() => setExpanded((v) => !v)}
          data-testid="mp-schedmsg-toggle"
        >
          <ClockIcon size={15} className="mp-expand__icon" />
          <span className="mp-expand__title">{s.title}</span>
          {items.length > 0 && (
            <span className="mp-schedmsg__count">{items.length}</span>
          )}
          {expanded ? (
            <ChevronDownIcon size={16} className="mp-expand__chevron" />
          ) : (
            <ChevronRightIcon size={16} className="mp-expand__chevron" />
          )}
        </button>
        {expanded && (
          <div className="mp-expand__body mp-schedmsg__body">
            {loadError ? (
              <div className="mp-schedmsg__error" data-testid="mp-schedmsg-error">
                {s.loadError}
              </div>
            ) : (
              <>
                {items.length === 0 && !adding && (
                  <div
                    className="mp-schedmsg__empty"
                    data-testid="mp-schedmsg-empty"
                  >
                    {s.empty}
                  </div>
                )}
                {items.map((m) => {
                  if (m.id === editId) {
                    const prefix = `mp-schedmsg-edit-${m.id}`;
                    return (
                      <div
                        className="mp-schedmsg__form"
                        key={m.id}
                        data-testid={`mp-schedmsg-editform-${m.id}`}
                      >
                        <ScheduleFields
                          idPrefix={prefix}
                          values={editValues}
                          onChange={(patch) =>
                            setEditValues((v) => ({ ...v, ...patch }))
                          }
                          autoFocus
                        />
                        {editError && (
                          <div
                            className="mp-schedmsg__error"
                            data-testid={`mp-schedmsg-edit-error-${m.id}`}
                          >
                            {s.updateError}
                          </div>
                        )}
                        <div className="mp-schedmsg__formactions">
                          <button
                            type="button"
                            className="btn"
                            onClick={cancelEdit}
                            disabled={editBusy}
                            data-testid={`mp-schedmsg-edit-cancel-${m.id}`}
                          >
                            {s.cancel}
                          </button>
                          <button
                            type="button"
                            className="btn mp-schedmsg__submit"
                            onClick={() => submitEdit(m.id)}
                            disabled={editDisabled}
                            data-testid={`mp-schedmsg-edit-save-${m.id}`}
                          >
                            {s.save}
                          </button>
                        </div>
                      </div>
                    );
                  }

                  const on = m.status === "enabled";
                  const clampable = isClampable(m.body);
                  const inFull = shownInFull.includes(m.id);
                  const clamped = clampable && !inFull;
                  return (
                    <div
                      className="mp-schedmsg__row"
                      key={m.id}
                      data-testid={`mp-schedmsg-row-${m.id}`}
                    >
                      <div className="mp-schedmsg__rowhead">
                        <span
                          className={`mp-schedmsg__dot mp-schedmsg__dot--${on ? "on" : "off"}`}
                        />
                        <span className="mp-schedmsg__label">
                          {m.label || s.unlabeled}
                        </span>
                        <span className="mp-schedmsg__spacer" />
                        <span className="mp-schedmsg__statusword">
                          {on ? s.enabled : s.disabled}
                        </span>
                        <button
                          type="button"
                          role="switch"
                          aria-checked={on}
                          aria-label={`${s.title} ${m.label || m.id}`}
                          disabled={toggleBusyId === m.id}
                          className={`mp-toggle${on ? " mp-toggle--on" : ""}`}
                          onClick={() => toggle(m)}
                          data-testid={`mp-schedmsg-status-${m.id}`}
                        >
                          <span className="mp-toggle__knob" />
                        </button>
                        <button
                          type="button"
                          className="mp-schedmsg__rowbtn"
                          aria-label={s.editLabel}
                          onClick={() => beginEdit(m)}
                          data-testid={`mp-schedmsg-edit-${m.id}`}
                        >
                          <PencilIcon size={15} />
                        </button>
                        <button
                          type="button"
                          className="mp-schedmsg__rowbtn mp-schedmsg__delete"
                          aria-label={s.deleteLabel}
                          onClick={() => setDeleteTarget(m)}
                          data-testid={`mp-schedmsg-delete-${m.id}`}
                        >
                          <TrashIcon size={15} />
                        </button>
                      </div>
                      <div className="mp-schedmsg__when">
                        <span>{cadenceText(m)}</span>
                        <span className="mp-schedmsg__time">
                          {pad2(m.hour)}:{pad2(m.minute)}
                        </span>
                        <span className="mp-schedmsg__tz">{m.timezone}</span>
                      </div>
                      {/* The clamp is a class on the SAME full text node — the
                          row never renders a shortened string, so nothing
                          downstream can mistake the display for the value. */}
                      <div
                        className={`mp-schedmsg__text${clamped ? " mp-schedmsg__text--clamped" : ""}`}
                        data-testid={`mp-schedmsg-text-${m.id}`}
                      >
                        {m.body}
                      </div>
                      {clampable && (
                        <button
                          type="button"
                          className="mp-schedmsg__textmore"
                          aria-expanded={inFull}
                          onClick={() => toggleFullBody(m.id)}
                          data-testid={`mp-schedmsg-text-toggle-${m.id}`}
                        >
                          {inFull ? s.bodyCollapse : s.bodyExpand}
                        </button>
                      )}
                      <div
                        className="mp-schedmsg__lastfired"
                        data-testid={`mp-schedmsg-lastfired-${m.id}`}
                      >
                        <span className="mp-schedmsg__lastfiredlabel">
                          {s.lastFiredLabel}
                        </span>
                        <span className="mp-schedmsg__lastfiredvalue">
                          {m.lastFiredTs > 0
                            ? formatAbsolute(m.lastFiredTs, nowTs)
                            : s.lastFiredNever}
                        </span>
                      </div>
                    </div>
                  );
                })}

                {adding ? (
                  <div className="mp-schedmsg__form">
                    <ScheduleFields
                      idPrefix="mp-schedmsg"
                      values={newValues}
                      onChange={(patch) =>
                        setNewValues((v) => ({ ...v, ...patch }))
                      }
                      autoFocus
                    />
                    {createError && (
                      <div
                        className="mp-schedmsg__error"
                        data-testid="mp-schedmsg-create-error"
                      >
                        {s.createError}
                      </div>
                    )}
                    <div className="mp-schedmsg__formactions">
                      <button
                        type="button"
                        className="btn"
                        onClick={resetForm}
                        disabled={createBusy}
                        data-testid="mp-schedmsg-cancel"
                      >
                        {s.cancel}
                      </button>
                      <button
                        type="button"
                        className="btn mp-schedmsg__submit"
                        onClick={submitCreate}
                        disabled={createDisabled}
                        data-testid="mp-schedmsg-create"
                      >
                        {s.create}
                      </button>
                    </div>
                  </div>
                ) : (
                  <button
                    type="button"
                    className="mp-schedmsg__add"
                    onClick={() => setAdding(true)}
                    data-testid="mp-schedmsg-add"
                  >
                    + {s.add}
                  </button>
                )}
              </>
            )}
          </div>
        )}
      </div>

      {deleteTarget && (
        <ConfirmModal
          body={s.deleteConfirm}
          cancelLabel={s.cancel}
          confirmLabel={s.deleteLabel}
          danger
          busy={deleteBusy}
          onCancel={() => setDeleteTarget(null)}
          onConfirm={confirmDelete}
          testId="mp-schedmsg-delete-confirm"
          confirmTestId="mp-schedmsg-delete-confirm-ok"
        />
      )}
    </>
  );
}
