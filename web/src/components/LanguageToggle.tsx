// SPDX-License-Identifier: Apache-2.0
import { useI18n } from "../i18n";

export default function LanguageToggle() {
  const { t, toggleLang } = useI18n();
  return (
    <button
      type="button"
      className="button button-ghost language-toggle"
      onClick={toggleLang}
      aria-label={t("lang.switch")}
    >
      {t("lang.switch")}
    </button>
  );
}
