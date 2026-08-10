import { useState } from "react";
import { useI18n } from "../i18n";
import { useScheduledMessages } from "../hooks/useScheduledMessages";
import { formatAbsolute } from "../lib/dateFormat";
import type { ScheduleCadence, ScheduledMessage } from "../api/adapter";
import { ConfirmModal } from "./ConfirmModal";
import { ChevronDownIcon, ChevronRightIcon, ClockIcon, TrashIcon } from "./icons";
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

const HOURS = Array.from({ length: 24 }, (_, i) => i);
const MINUTES = Array.from({ length: 60 }, (_, i) => i);
const DAYS_OF_MONTH = Array.from({ length: 31 }, (_, i) => i + 1);

function pad2(n: number): string {
  return String(n).padStart(2, "0");
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
  const [newLabel, setNewLabel] = useState("");
  const [newBody, setNewBody] = useState("");
  const [newCadence, setNewCadence] = useState<ScheduleCadence>("daily");
  const [newDayOfWeek, setNewDayOfWeek] = useState(1);
  const [newDayOfMonth, setNewDayOfMonth] = useState(1);
  const [newHour, setNewHour] = useState(9);
  const [newMinute, setNewMinute] = useState(0);
  const [newTimezone, setNewTimezone] = useState(DEFAULT_TIMEZONE);
  const [createBusy, setCreateBusy] = useState(false);
  const [createError, setCreateError] = useState(false);
  const [toggleBusyId, setToggleBusyId] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ScheduledMessage | null>(
    null
  );
  const [deleteBusy, setDeleteBusy] = useState(false);

  const weekdayNames = [
    s.weekdaySun,
    s.weekdayMon,
    s.weekdayTue,
    s.weekdayWed,
    s.weekdayThu,
    s.weekdayFri,
    s.weekdaySat,
  ];

  function cadenceText(m: ScheduledMessage): string {
    if (m.cadence === "weekly") return s.weeklyOn(weekdayNames[m.dayOfWeek] ?? "");
    if (m.cadence === "monthly") return s.monthlyOn(m.dayOfMonth);
    return s.cadenceDaily;
  }

  function resetForm() {
    setAdding(false);
    setNewLabel("");
    setNewBody("");
    setNewCadence("daily");
    setNewDayOfWeek(1);
    setNewDayOfMonth(1);
    setNewHour(9);
    setNewMinute(0);
    setNewTimezone(DEFAULT_TIMEZONE);
    setCreateError(false);
  }

  const createDisabled =
    createBusy || newBody.trim() === "" || newTimezone.trim() === "";

  async function submitCreate() {
    if (createDisabled) return;
    setCreateBusy(true);
    setCreateError(false);
    try {
      await create({
        label: newLabel.trim(),
        body: newBody.trim(),
        cadence: newCadence,
        // Only the field the chosen cadence actually reads rides the wire —
        // sending the other one would write a value nothing will ever apply.
        ...(newCadence === "weekly" ? { dayOfWeek: newDayOfWeek } : {}),
        ...(newCadence === "monthly" ? { dayOfMonth: newDayOfMonth } : {}),
        hour: newHour,
        minute: newMinute,
        timezone: newTimezone.trim(),
      });
      resetForm();
    } catch {
      setCreateError(true);
    } finally {
      setCreateBusy(false);
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

  // The create form's own day-of-month choice is what the hint reacts to, so
  // the cost is visible WHILE choosing rather than inferred months later from
  // "why has February been silent".
  const showSkipHint =
    newCadence === "monthly" && SPARSE_DAYS.has(newDayOfMonth);

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
                  const on = m.status === "enabled";
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
                          className="mp-schedmsg__delete"
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
                      <div className="mp-schedmsg__text">{m.body}</div>
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
                    <label className="mp-schedmsg__field">
                      <span className="mp-schedmsg__fieldlabel">
                        {s.labelLabel}
                      </span>
                      <input
                        type="text"
                        className="mp-schedmsg__input"
                        placeholder={s.labelPlaceholder}
                        value={newLabel}
                        onChange={(e) => setNewLabel(e.target.value)}
                        autoFocus
                        data-testid="mp-schedmsg-label-input"
                      />
                    </label>
                    <label className="mp-schedmsg__field">
                      <span className="mp-schedmsg__fieldlabel">
                        {s.bodyLabel}
                      </span>
                      <textarea
                        className="mp-schedmsg__input mp-schedmsg__textarea"
                        placeholder={s.bodyPlaceholder}
                        rows={3}
                        value={newBody}
                        onChange={(e) => setNewBody(e.target.value)}
                        data-testid="mp-schedmsg-body-input"
                      />
                    </label>
                    <label className="mp-schedmsg__field">
                      <span className="mp-schedmsg__fieldlabel">
                        {s.cadenceLabel}
                      </span>
                      <select
                        className="mp-schedmsg__input mp-schedmsg__select"
                        value={newCadence}
                        onChange={(e) =>
                          setNewCadence(e.target.value as ScheduleCadence)
                        }
                        data-testid="mp-schedmsg-cadence"
                      >
                        <option value="daily">{s.cadenceDaily}</option>
                        <option value="weekly">{s.cadenceWeekly}</option>
                        <option value="monthly">{s.cadenceMonthly}</option>
                      </select>
                    </label>
                    {/* The day field belongs to exactly one cadence, so the
                        other one is not rendered at all — a disabled-but-visible
                        control would suggest the value still means something. */}
                    {newCadence === "weekly" && (
                      <label className="mp-schedmsg__field">
                        <span className="mp-schedmsg__fieldlabel">
                          {s.dayOfWeekLabel}
                        </span>
                        <select
                          className="mp-schedmsg__input mp-schedmsg__select"
                          value={newDayOfWeek}
                          onChange={(e) =>
                            setNewDayOfWeek(Number(e.target.value))
                          }
                          data-testid="mp-schedmsg-dayofweek"
                        >
                          {weekdayNames.map((name, i) => (
                            <option key={i} value={i}>
                              {name}
                            </option>
                          ))}
                        </select>
                      </label>
                    )}
                    {newCadence === "monthly" && (
                      <label className="mp-schedmsg__field">
                        <span className="mp-schedmsg__fieldlabel">
                          {s.dayOfMonthLabel}
                        </span>
                        <select
                          className="mp-schedmsg__input mp-schedmsg__select"
                          value={newDayOfMonth}
                          onChange={(e) =>
                            setNewDayOfMonth(Number(e.target.value))
                          }
                          data-testid="mp-schedmsg-dayofmonth"
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
                            data-testid="mp-schedmsg-skip-hint"
                          >
                            {s.dayOfMonthSkipHint}
                          </span>
                        )}
                      </label>
                    )}
                    <div className="mp-schedmsg__timerow">
                      <label className="mp-schedmsg__field">
                        <span className="mp-schedmsg__fieldlabel">
                          {s.hourLabel}
                        </span>
                        <select
                          className="mp-schedmsg__input mp-schedmsg__select"
                          value={newHour}
                          onChange={(e) => setNewHour(Number(e.target.value))}
                          data-testid="mp-schedmsg-hour"
                        >
                          {HOURS.map((h) => (
                            <option key={h} value={h}>
                              {pad2(h)}
                            </option>
                          ))}
                        </select>
                      </label>
                      <label className="mp-schedmsg__field">
                        <span className="mp-schedmsg__fieldlabel">
                          {s.minuteLabel}
                        </span>
                        <select
                          className="mp-schedmsg__input mp-schedmsg__select"
                          value={newMinute}
                          onChange={(e) => setNewMinute(Number(e.target.value))}
                          data-testid="mp-schedmsg-minute"
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
                      <span className="mp-schedmsg__fieldlabel">
                        {s.timezoneLabel}
                      </span>
                      <input
                        type="text"
                        className="mp-schedmsg__input"
                        placeholder={s.timezonePlaceholder}
                        value={newTimezone}
                        onChange={(e) => setNewTimezone(e.target.value)}
                        data-testid="mp-schedmsg-timezone"
                      />
                    </label>
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
