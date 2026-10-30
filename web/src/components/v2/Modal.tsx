// SPDX-License-Identifier: Apache-2.0
import {
  useEffect,
  useId,
  useRef,
  type ReactNode
} from "react";
import { createPortal } from "react-dom";
import Icon from "./Icon";

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

// Only the most recently opened modal may react to Escape. Capture listeners
// on the document otherwise let one Escape close every stacked modal.
const modalStack: string[] = [];
let bodyOverflowBeforeModals = "";

export default function Modal({
  open,
  onClose,
  title,
  kicker,
  children,
  actions,
  wide = false,
  className = ""
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  kicker?: string;
  children: ReactNode;
  actions?: ReactNode;
  wide?: boolean;
  className?: string;
}) {
  const panelRef = useRef<HTMLElement | null>(null);
  const restoreRef = useRef<HTMLElement | null>(null);
  const onCloseRef = useRef(onClose);
  const titleID = useId();
  const modalKey = useId();
  onCloseRef.current = onClose;

  useEffect(() => {
    if (!open) {
      return;
    }
    if (modalStack.length === 0) {
      bodyOverflowBeforeModals = document.body.style.overflow;
    }
    modalStack.push(modalKey);
    restoreRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    document.body.style.overflow = "hidden";
    const panel = panelRef.current;
    const first = panel?.querySelector<HTMLElement>(FOCUSABLE);
    window.requestAnimationFrame(() => (first ?? panel)?.focus());

    function onKeyDown(event: KeyboardEvent) {
      if (modalStack[modalStack.length - 1] !== modalKey) {
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        onCloseRef.current();
        return;
      }
      if (event.key !== "Tab" || !panel) {
        return;
      }
      const focusable = Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
        (element) => !element.hasAttribute("hidden") && element.offsetParent !== null
      );
      if (focusable.length === 0) {
        event.preventDefault();
        panel.focus();
        return;
      }
      const firstElement = focusable[0];
      const lastElement = focusable[focusable.length - 1];
      if (!firstElement || !lastElement) {
        return;
      }
      if (event.shiftKey && document.activeElement === firstElement) {
        event.preventDefault();
        lastElement.focus();
      } else if (!event.shiftKey && document.activeElement === lastElement) {
        event.preventDefault();
        firstElement.focus();
      }
    }

    document.addEventListener("keydown", onKeyDown, true);
    return () => {
      document.removeEventListener("keydown", onKeyDown, true);
      const index = modalStack.lastIndexOf(modalKey);
      if (index >= 0) {
        modalStack.splice(index, 1);
      }
      if (modalStack.length === 0) {
        document.body.style.overflow = bodyOverflowBeforeModals;
      }
      restoreRef.current?.focus();
    };
  }, [open, modalKey]);

  if (!open || typeof document === "undefined") {
    return null;
  }

  return createPortal(
    <div
      className="dw-modal-backdrop"
      onPointerDown={(event) => {
        if (event.target === event.currentTarget) {
          onClose();
        }
      }}
    >
      <section
        ref={panelRef}
        className={`dw-modal ${wide ? "dw-modal-wide" : ""} ${className}`.trim()}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleID}
        tabIndex={-1}
      >
        <button
          className="dw-modal-close"
          type="button"
          onClick={onClose}
          aria-label={title}
        >
          <Icon name="close" size={18} />
        </button>
        {kicker ? (
          <p className="dw-eyebrow">
            <span className="dw-eyebrow-line" />
            {kicker}
          </p>
        ) : null}
        <h2 id={titleID}>{title}</h2>
        <div className="dw-modal-body">{children}</div>
        {actions ? <div className="dw-modal-actions">{actions}</div> : null}
      </section>
    </div>,
    document.body
  );
}
