import { useRef, type ReactNode } from "react";
import "./confirm-modal.css";
import { useEscapeLayer } from "../lib/useEscapeLayer";

/**
 * Minimal reusable centered confirm modal (overlay + card), dark-themed to
 * match the existing dialog language (mon-confirm / machine-picker) without
 * coupling to their page-scoped stylesheets. No third-party lib.
 *
 * Behavior contract:
 *   - Esc or the cancel button closes (both are ignored while `busy` — a
 *     committed action must not lose its pending state mid-flight).
 *   - The confirm button runs the caller's action; the caller decides whether
 *     to close (success) or surface `error` and keep the modal open.
 */
export function ConfirmModal({
  body,
  error,
  cancelLabel,
  confirmLabel,
  busy = false,
  danger = false,
  onCancel,
  onConfirm,
  testId,
  confirmTestId,
}: {
  body: ReactNode;
  /** Honest inline failure line; the modal stays open so the user can retry. */
  error?: string | null;
  cancelLabel: string;
  confirmLabel: string;
  busy?: boolean;
  /** Red-accented confirm for destructive actions. */
  danger?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
  testId?: string;
  confirmTestId?: string;
}) {
  // Esc cancels — and while the confirm is in flight the modal SWALLOWS it
  // rather than letting it fall through to whatever is underneath: a busy modal
  // is still the top layer, it just has nothing to do with the key yet.
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(() => {
    if (!busy) onCancel();
  }, rootRef);

  return (
    <div
      ref={rootRef}
      className="confirm-modal"
      data-testid={testId}
      role="dialog"
      aria-modal="true"
    >
      <div
        className={`confirm-modal__box${danger ? " confirm-modal__box--danger" : ""}`}
      >
        <div className="confirm-modal__body">{body}</div>
        {error && <div className="confirm-modal__error">{error}</div>}
        <div className="confirm-modal__actions">
          <button
            type="button"
            className="confirm-modal__btn"
            disabled={busy}
            onClick={onCancel}
          >
            {cancelLabel}
          </button>
          <button
            type="button"
            className={`confirm-modal__btn${danger ? " confirm-modal__btn--danger" : " confirm-modal__btn--accent"}`}
            data-testid={confirmTestId}
            disabled={busy}
            onClick={onConfirm}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
