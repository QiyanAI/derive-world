// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate } from "react-router";
import { listObjects, type ObjectListItem } from "../api";
import ObjectCard from "../components/ObjectCard";
import { ErrorNotice } from "../errors";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";

type Scope = "owned" | "shared" | "all";

export default function ObjectsPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { status: authStatus } = useAuth();
  const [scope, setScope] = useState<Scope>("all");
  const [items, setItems] = useState<ObjectListItem[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);

  const load = useCallback(
    async (selected: Scope, cursor: string | null, append: boolean, signal?: AbortSignal) => {
      setLoading(true);
      setError(null);
      try {
        const response = await listObjects(selected, cursor ?? undefined);
        if (signal?.aborted) {
          return;
        }
        setItems((previous) => (append ? [...previous, ...response.items] : response.items));
        setNextCursor(response.next_cursor);
      } catch (failure) {
        if (!signal?.aborted) {
          setError(failure);
        }
      } finally {
        if (!signal?.aborted) {
          setLoading(false);
        }
      }
    },
    []
  );

  useEffect(() => {
    if (authStatus !== "authenticated") {
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    setItems([]);
    setNextCursor(null);
    void load(scope, null, false, controller.signal);
    return () => controller.abort();
  }, [authStatus, scope, load]);

  if (authStatus === "loading") {
    return <p className="page">{t("common.loading")}</p>;
  }
  if (authStatus === "anonymous") {
    return (
      <div className="page">
        <h1>{t("objects.title")}</h1>
        <p className="notice">{t("auth.required")}</p>
        <Link className="button" to="/login?next=%2Fobjects">
          {t("nav.login")}
        </Link>
      </div>
    );
  }

  return (
    <div className="page page-wide">
      <div className="page-heading with-actions">
        <div>
          <p className="eyebrow">{t("app.name")}</p>
          <h1>{t("objects.title")}</h1>
          <p className="lead">{t("objects.lead")}</p>
        </div>
        <div className="dw-inline-actions">
          <button
            className="button"
            type="button"
            onClick={() => {
              navigate("/");
              window.setTimeout(() => window.dispatchEvent(new Event("dw:create-world")), 120);
            }}
          >
            {t("objects.create")}
          </button>
          <Link className="button button-ghost" to="/objects/new">
            {t("objects.createOther")}
          </Link>
        </div>
      </div>

      <div className="list-toolbar">
        <div className="tabs" role="group" aria-label={t("objects.title")}>
          {(["all", "owned", "shared"] as Scope[]).map((value) => (
            <button
              key={value}
              type="button"
              aria-pressed={scope === value}
              className={scope === value ? "tab tab-active" : "tab"}
              onClick={() => setScope(value)}
            >
              {t(`objects.tab.${value}`)}
            </button>
          ))}
        </div>
        <span className="muted">
          {loading ? t("common.loading") : t("objects.count", { count: items.length })}
        </span>
      </div>

      {error ? <ErrorNotice error={error} onRetry={() => void load(scope, null, false)} /> : null}

      {!error && items.length === 0 && !loading ? (
        <div className="empty-state">
          <p>
            {scope === "shared"
              ? t("objects.emptyShared")
              : scope === "all"
                ? t("objects.emptyAll")
                : t("objects.empty")}
          </p>
          <button
            className="button"
            type="button"
            onClick={() => {
              navigate("/");
              window.setTimeout(() => window.dispatchEvent(new Event("dw:create-world")), 120);
            }}
          >
            {t("objects.create")}
          </button>
        </div>
      ) : null}

      <ul className="object-grid">
        {items.map((item) => (
          <li key={item.id}>
            <ObjectCard item={item} />
          </li>
        ))}
      </ul>

      {nextCursor ? (
        <div className="load-more">
          <button
            className="button button-ghost"
            type="button"
            disabled={loading}
            onClick={() => void load(scope, nextCursor, true)}
          >
            {t("objects.loadMore")}
          </button>
        </div>
      ) : null}
    </div>
  );
}
