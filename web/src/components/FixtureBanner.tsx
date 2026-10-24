// SPDX-License-Identifier: Apache-2.0
import { useI18n } from "../i18n";

export default function FixtureBanner() {
  const { t } = useI18n();
  return (
    <div className="fixture-banner" role="status">
      <strong>FIXTURE</strong>
      <span>{t("fixture.banner")}</span>
    </div>
  );
}
