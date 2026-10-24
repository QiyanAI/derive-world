// SPDX-License-Identifier: Apache-2.0
import { Link } from "react-router";
import type { ObjectListItem } from "../api";
import { useI18n } from "../i18n";

export default function ObjectCard({ item }: { item: ObjectListItem }) {
  const { t, lang } = useI18n();
  const formatter = new Intl.DateTimeFormat(lang === "zh" ? "zh-CN" : "en", {
    dateStyle: "medium",
    timeStyle: "short"
  });
  const collaborative = item.object_type === "collaborative";
  return (
    <Link
      className={`object-card object-card-${item.object_type}`}
      to={`/objects/${item.id}`}
      data-testid="object-card"
      data-object-id={item.id}
    >
      <div className="object-card-top">
        <span className={`template-badge template-${item.object_type}`}>
          {collaborative ? t("object.create.collaborative") : t("object.create.interactive")}
        </span>
        <span className="version-badge">{t("objects.version", { version: item.version })}</span>
      </div>
      <h2>{item.title || t("common.unknown")}</h2>
      <div className="object-card-meta">
        <span>{t("objects.role", { role: t(`role.${item.role}`) })}</span>
        <span>{t("objects.updated", { time: formatter.format(new Date(item.updated_at)) })}</span>
      </div>
      <span className="object-card-open" aria-hidden="true">
        →
      </span>
    </Link>
  );
}
