// SPDX-License-Identifier: Apache-2.0
import { Link } from "react-router";
import { useI18n } from "../i18n";
import type { MessageKey } from "../i18n/en";

export interface SectionTab {
  id: string;
  labelKey: MessageKey;
}

export default function SectionNav({
  objectID,
  tabs,
  active
}: {
  objectID: string;
  tabs: SectionTab[];
  active: string;
}) {
  const { t } = useI18n();
  return (
    <nav className="section-nav" aria-label={t("object.tab.label")}>
      {tabs.map((tab) => (
        <Link
          key={tab.id}
          to={`/objects/${objectID}?tab=${tab.id}`}
          className={active === tab.id ? "section-link section-link-active" : "section-link"}
          aria-current={active === tab.id ? "page" : undefined}
        >
          {t(tab.labelKey)}
        </Link>
      ))}
    </nav>
  );
}
