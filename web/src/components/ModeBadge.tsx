// SPDX-License-Identifier: Apache-2.0
import { useI18n } from "../i18n";
import type { ModelMode } from "../meta";

const labels = {
  fixture: "mode.fixture",
  real: "mode.real",
  unknown: "mode.unknown"
} as const;

export default function ModeBadge({ mode }: { mode: ModelMode }) {
  const { t } = useI18n();
  return (
    <span
      className={`mode-badge mode-${mode}`}
      title={t("mode.label")}
      data-model-mode={mode}
    >
      {t(labels[mode])}
    </span>
  );
}
