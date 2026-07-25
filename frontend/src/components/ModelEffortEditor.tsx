// ModelEffortEditor — the SHARED model/effort quick-pick editor (owner M2).
//
// Presentation conventions inherited from the retired 新增角色 form (the
// inline-row redesign removed it from Settings; the pattern lives on here as a
// reusable piece):
//   • Claude model = fable/opus/sonnet/haiku quick-pick CHIPS + a free custom
//     string input (spawn --model is a FREE string — the chips are safe
//     defaults, the input stays authoritative; BLANK ⇒ the server/CLI default).
//   • Codex model = closed quick-pick CHIPS only.  No selected chip delegates
//     to the Codex configuration on the selected machine, just like a blank
//     Claude custom-model field delegates to Claude's default.
//   • effort = a low/medium/high dropdown (closed vocabulary, server-422
//     outside it).
//
// Controlled + stateless: the caller owns the draft values (it decides when to
// PATCH); this component only renders the two fields and reports changes.

import { useEffect } from "react";
import { useI18n } from "../i18n";
import type { Effort } from "../types";
import "./model-effort-editor.css";

/** Quick-pick model chips (safe defaults — the input stays free-form). */
export const MODEL_QUICK_PICKS = ["fable", "opus", "sonnet", "haiku"] as const;
/** Exact Codex App Server model identifiers supported by OffiCraft's picker. */
export const CODEX_MODEL_OPTIONS = ["gpt-5.6-terra", "gpt-5.6-sol"] as const;
/** The closed effort vocabulary (server 422s anything else). */
export const EFFORTS: readonly Effort[] = ["low", "medium", "high"] as const;

/** A closed picker: Codex model identifiers are not short aliases. */
export function CodexModelSelect({
  model,
  onModelChange,
  className = "",
  testId = "me-codex-model-select",
}: {
  model: string;
  onModelChange: (model: string) => void;
  className?: string;
  testId?: string;
}) {
  const { t } = useI18n();
  const known = model === "" || (CODEX_MODEL_OPTIONS as readonly string[]).includes(model);
  useEffect(() => {
    if (!known) onModelChange("");
  }, [known, onModelChange]);
  return (
    <div
      className={`me-editor__chips${className ? ` ${className}` : ""}`}
      aria-label={t.mp.model}
      data-testid={testId}
    >
      {CODEX_MODEL_OPTIONS.map((id) => (
        <button
          type="button"
          key={id}
          className={`doc-btn${model === id ? " doc-btn--accent" : ""}`}
          data-testid={`${testId}-${id}`}
          onClick={() => onModelChange(model === id ? "" : id)}
        >
          {id}
        </button>
      ))}
      {model === "" && (
        <span className="me-editor__hint">{t.mp.modelMachineDefault}</span>
      )}
    </div>
  );
}

export function ModelEffortEditor({
  runtime,
  model,
  effort,
  onRuntimeChange,
  onModelChange,
  onEffortChange,
}: {
  runtime?: "claude" | "codex";
  model: string;
  effort: string;
  onRuntimeChange?: (runtime: "claude" | "codex") => void;
  onModelChange: (model: string) => void;
  onEffortChange: (effort: string) => void;
}) {
  const { t } = useI18n();
  return (
    <>
      {runtime && onRuntimeChange && (
        <>
          <div className="me-editor__label">{t.mp.agentRuntime}</div>
          <select
            className="me-editor__select"
            value={runtime}
            aria-label={t.mp.agentRuntime}
            onChange={(e) => {
              const next = e.target.value as "claude" | "codex";
              if (next !== runtime) onModelChange("");
              onRuntimeChange(next);
            }}
            data-testid="me-runtime-select"
          >
            <option value="claude">Claude Code</option>
            <option value="codex">Codex</option>
          </select>
        </>
      )}
      <div className="me-editor__label">{t.mp.model}</div>
      {runtime !== "codex" && (
        <div className="me-editor__chips">
          {MODEL_QUICK_PICKS.map((m) => (
            <button
              type="button"
              key={m}
              className={`doc-btn${model === m ? " doc-btn--accent" : ""}`}
              data-testid={`me-model-chip-${m}`}
              onClick={() => onModelChange(m)}
            >
              {m}
            </button>
          ))}
        </div>
      )}
      {runtime === "codex" ? (
        <CodexModelSelect model={model} onModelChange={onModelChange} />
      ) : (
        <input
          className="me-editor__input"
          value={model}
          placeholder={t.mp.modelPlaceholder}
          aria-label={t.mp.model}
          onChange={(e) => onModelChange(e.target.value)}
          data-testid="me-model-input"
        />
      )}
      <div className="me-editor__label me-editor__label--stacked">
        {t.mp.effort}
      </div>
      <select
        className="me-editor__select"
        value={effort}
        aria-label={t.mp.effort}
        onChange={(e) => onEffortChange(e.target.value)}
        data-testid="me-effort-select"
      >
        {EFFORTS.map((e) => (
          <option key={e} value={e}>
            {t.mp.effortLevel(e)} ({e})
          </option>
        ))}
      </select>
    </>
  );
}
