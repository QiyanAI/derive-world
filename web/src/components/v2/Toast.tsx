// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useRef, useState } from "react";

interface ToastEntry {
  id: number;
  text: string;
  tone: "ok" | "error";
}

export function useToast() {
  const [entries, setEntries] = useState<ToastEntry[]>([]);
  const nextID = useRef(1);
  const timers = useRef<number[]>([]);

  useEffect(() => {
    const pending = timers.current;
    return () => {
      pending.forEach((timer) => window.clearTimeout(timer));
    };
  }, []);

  const push = useCallback((text: string, tone: "ok" | "error" = "ok") => {
    const id = nextID.current++;
    setEntries((current) => [...current.slice(-2), { id, text, tone }]);
    const timer = window.setTimeout(() => {
      setEntries((current) => current.filter((entry) => entry.id !== id));
    }, 4200);
    timers.current.push(timer);
  }, []);

  const view = (
    <div className="dw-toasts" aria-live="polite" aria-atomic="false">
      {entries.map((entry) => (
        <div key={entry.id} className={`dw-toast dw-toast-${entry.tone}`} role="status">
          {entry.text}
        </div>
      ))}
    </div>
  );

  return { push, view };
}
