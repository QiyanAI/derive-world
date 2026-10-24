// SPDX-License-Identifier: Apache-2.0
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode
} from "react";
import { en, type MessageKey } from "./en";
import { zh } from "./zh";

export type { MessageKey } from "./en";

export type Lang = "en" | "zh";

const dictionaries = { en, zh } as const;
const STORAGE_KEY = "dw_lang";

interface I18nValue {
  lang: Lang;
  setLang: (lang: Lang) => void;
  toggleLang: () => void;
  t: (key: MessageKey, vars?: Record<string, string | number>) => string;
}

const I18nContext = createContext<I18nValue | null>(null);

function readLang(): Lang {
  try {
    return localStorage.getItem(STORAGE_KEY) === "zh" ? "zh" : "en";
  } catch {
    return "en";
  }
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(readLang);

  useEffect(() => {
    document.documentElement.lang = lang === "zh" ? "zh-CN" : "en";
    try {
      localStorage.setItem(STORAGE_KEY, lang);
    } catch {
      // localStorage may be unavailable in privacy mode; in-memory state still works.
    }
  }, [lang]);

  const setLang = useCallback((next: Lang) => setLangState(next), []);
  const toggleLang = useCallback(
    () => setLangState((previous) => (previous === "en" ? "zh" : "en")),
    []
  );

  const t = useCallback(
    (key: MessageKey, vars?: Record<string, string | number>) => {
      const dictionary = dictionaries[lang];
      let output: string = dictionary[key] ?? en[key];
      if (vars) {
        for (const [name, value] of Object.entries(vars)) {
          output = output.replaceAll(`{${name}}`, String(value));
        }
      }
      return output;
    },
    [lang]
  );

  const value = useMemo(
    () => ({ lang, setLang, toggleLang, t }),
    [lang, setLang, toggleLang, t]
  );

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nValue {
  const value = useContext(I18nContext);
  if (!value) {
    throw new Error("useI18n must be used inside I18nProvider");
  }
  return value;
}
