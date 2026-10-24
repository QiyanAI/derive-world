// SPDX-License-Identifier: Apache-2.0
import { Link } from "react-router";
import { useI18n } from "../i18n";

export default function NotFoundPage() {
  const { t } = useI18n();
  return (
    <section className="page">
      <p className="eyebrow">404</p>
      <h1>{t("page.notFound.title")}</h1>
      <p className="lead">{t("page.notFound.body")}</p>
      <Link className="text-link" to="/">
        {t("common.backHome")}
      </Link>
    </section>
  );
}
