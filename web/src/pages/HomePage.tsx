// SPDX-License-Identifier: Apache-2.0
import { useAuth } from "../auth";
import { useI18n } from "../i18n";
import LandingPage from "./LandingPage";
import WorldPage from "./WorldPage";

export default function HomePage() {
  const { status } = useAuth();
  const { t } = useI18n();
  if (status === "loading") {
    return <p className="page">{t("common.loading")}</p>;
  }
  if (status === "anonymous") {
    return <LandingPage />;
  }
  return <WorldPage />;
}
