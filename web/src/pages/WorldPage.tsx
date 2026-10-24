// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router";
import {
  getLineage,
  listConfirmations,
  listConversations,
  listMembers,
  listObjects,
  type Confirmation,
  type Conversation,
  type Lineage,
  type ObjectListItem
} from "../api";
import ObjectCard from "../components/ObjectCard";
import { ErrorNotice } from "../errors";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";

interface Signals {
  membersByObject: Record<string, number>;
  lineages: Record<string, Lineage>;
}

const SIGNAL_OBJECT_LIMIT = 6;

function countDescendants(nodes: Lineage["descendants"]): number {
  return nodes.reduce((total, node) => total + 1 + countDescendants(node.children), 0);
}

function collectLineages(items: ObjectListItem[]): string[] {
  return items.slice(0, SIGNAL_OBJECT_LIMIT).map((item) => item.id);
}

export default function WorldPage() {
  const { t } = useI18n();
  const { user } = useAuth();
  const [items, setItems] = useState<ObjectListItem[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [confirmations, setConfirmations] = useState<Confirmation[]>([]);
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [signals, setSignals] = useState<Signals>({ membersByObject: {}, lineages: {} });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true);
      setError(null);
      try {
        const [objectPage, pending, conversationPage] = await Promise.all([
          listObjects("all"),
          listConfirmations("pending", signal),
          listConversations(undefined, signal)
        ]);
        if (signal?.aborted) {
          return;
        }
        setItems(objectPage.items);
        setHasMore(Boolean(objectPage.next_cursor));
        setConfirmations(pending.items);
        setConversations(conversationPage.items);

        const ids = collectLineages(objectPage.items);
        const detail = await Promise.all(
          ids.map(async (id) => {
            const [lineage, members] = await Promise.all([
              getLineage(id, signal).catch(() => null),
              listMembers(id, signal).catch(() => null)
            ]);
            return { id, lineage, memberCount: members?.items.length ?? 0 };
          })
        );
        if (signal?.aborted) {
          return;
        }
        const nextSignals: Signals = { membersByObject: {}, lineages: {} };
        for (const entry of detail) {
          nextSignals.membersByObject[entry.id] = entry.memberCount;
          if (entry.lineage) {
            nextSignals.lineages[entry.id] = entry.lineage;
          }
        }
        setSignals(nextSignals);
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
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  const owned = items.filter((item) => item.role === "owner");
  const shared = items.filter((item) => item.role !== "owner");
  const branchCount = Object.values(signals.lineages).reduce(
    (total, lineage) => total + countDescendants(lineage.descendants),
    0
  );
  const derived = Object.values(signals.lineages).some(
    (lineage) => lineage.ancestors.length > 0 || lineage.descendants.length > 0
  );
  const sharedWithSomeone = Object.values(signals.membersByObject).some((count) => count > 1);
  const aiUsed = conversations.length > 0;
  const hasObjects = items.length > 0;
  const firstObject = items[0];
  const firstOwned = owned[0] ?? firstObject;

  const checklist = [
    {
      key: "create",
      done: hasObjects,
      to: "/objects/new"
    },
    {
      key: "share",
      done: sharedWithSomeone,
      to: firstOwned ? `/objects/${firstOwned.id}?tab=access` : "/objects/new"
    },
    {
      key: "ai",
      done: aiUsed,
      to: "/ai"
    },
    {
      key: "derive",
      done: derived,
      to: firstOwned ? `/objects/${firstOwned.id}/derive` : "/objects/new"
    }
  ] as const;

  const displayName = user?.display_name ?? "";

  return (
    <div className="page page-wide world-page">
      <div className="page-heading with-actions">
        <div>
          <p className="eyebrow">{t("world.eyebrow")}</p>
          <h1>{t("world.title", { name: displayName })}</h1>
          <p className="lead">{t("world.lead")}</p>
        </div>
        <button className="button button-ghost button-small" type="button" onClick={() => void load()} disabled={loading}>
          {t("world.refresh")}
        </button>
      </div>

      {error ? <ErrorNotice error={error} onRetry={() => void load()} /> : null}

      <section className="world-stats" aria-label={t("world.eyebrow")}>
        <div className="stat-card">
          <strong>{loading ? "—" : owned.length}</strong>
          <span>{t("world.stat.owned")}</span>
        </div>
        <div className="stat-card">
          <strong>{loading ? "—" : shared.length}</strong>
          <span>{t("world.stat.shared")}</span>
        </div>
        <div className="stat-card">
          <strong>{loading ? "—" : branchCount}</strong>
          <span>{t("world.stat.branches")}</span>
        </div>
        <div className="stat-card">
          <strong>{loading ? "—" : confirmations.length}</strong>
          <span>{t("world.stat.pending")}</span>
        </div>
      </section>

      <div className="world-grid">
        <section className="world-main">
          <div className="section-heading">
            <h2>{t("world.recent.title")}</h2>
            <Link className="text-link" to="/objects">
              {t("world.objects.all")}
            </Link>
          </div>
          {loading ? <p className="muted">{t("common.loading")}</p> : null}
          {!loading && !hasObjects ? (
            <div className="empty-state">
              <h2>{t("world.empty.title")}</h2>
              <p>{t("world.empty.body")}</p>
              <Link className="button" to="/objects/new">
                {t("world.empty.create")}
              </Link>
            </div>
          ) : null}
          {hasObjects ? (
            <ul className="object-grid object-grid-compact">
              {items.slice(0, 4).map((item) => (
                <li key={item.id}>
                  <ObjectCard item={item} />
                </li>
              ))}
            </ul>
          ) : null}
          {hasMore ? <p className="muted">{t("objects.loadMore")} →</p> : null}

          <section className="card world-map-card">
            <h2>{t("world.map.title")}</h2>
            {hasObjects ? (
              <div className="world-map">
                <div className="map-column map-identity">
                  <div className="map-node map-node-you">
                    <span className="map-node-label">{t("world.map.you")}</span>
                    <strong>{displayName}</strong>
                  </div>
                  <div className="map-node map-node-ai">
                    <span className="map-node-label">{t("world.map.ai")}</span>
                    <strong>{conversations.length > 0 ? t("world.map.ai") : "—"}</strong>
                  </div>
                </div>
                <div className="map-connector" aria-hidden="true" />
                <div className="map-column">
                  <span className="map-column-label">{t("world.map.objects")}</span>
                  {owned.slice(0, 3).map((item) => (
                    <Link className="map-object" key={item.id} to={`/objects/${item.id}`}>
                      {item.title}
                    </Link>
                  ))}
                  {owned.length === 0 ? <span className="muted">—</span> : null}
                </div>
                <div className="map-connector" aria-hidden="true" />
                <div className="map-column">
                  <span className="map-column-label">{t("world.map.shared")}</span>
                  {shared.slice(0, 2).map((item) => (
                    <Link className="map-object" key={item.id} to={`/objects/${item.id}`}>
                      {item.title}
                    </Link>
                  ))}
                  {shared.length === 0 ? <span className="muted">—</span> : null}
                  <span className="map-column-label map-branch-label">
                    {t("world.map.branches")}: {branchCount}
                  </span>
                </div>
              </div>
            ) : (
              <p className="muted">{t("world.map.empty")}</p>
            )}
          </section>

          {!hasObjects ? (
            <section className="card">
              <h2>{t("world.start.title")}</h2>
              <p>{t("world.start.body")}</p>
              <div className="form-actions">
                <Link className="button" to="/objects/new?template=collaborative">
                  {t("object.create.collaborative")}
                </Link>
                <Link className="button button-ghost" to="/objects/new?template=interactive">
                  {t("object.create.interactive")}
                </Link>
              </div>
            </section>
          ) : null}
        </section>

        <aside className="world-side">
          <section className="card">
            <h2>{t("world.checklist.title")}</h2>
            <p className="muted">{t("world.checklist.lead")}</p>
            <ol className="onboarding-list">
              {checklist.map((item) => (
                <li key={item.key} className={item.done ? "onboarding-item onboarding-done" : "onboarding-item"}>
                  <span className="onboarding-mark" aria-hidden="true">
                    {item.done ? "✓" : "○"}
                  </span>
                  <div>
                    <strong>{t(`world.checklist.${item.key}`)}</strong>
                    <small>{item.done ? t("world.checklist.done") : t("world.checklist.todo")}</small>
                  </div>
                  {!item.done ? (
                    <Link className="text-link" to={item.to}>
                      {t("world.checklist.action")}
                    </Link>
                  ) : null}
                </li>
              ))}
            </ol>
          </section>

          <section className="card">
            <h2>{t("world.approvals.title")}</h2>
            {confirmations.length === 0 ? (
              <p className="muted">{t("world.approvals.none")}</p>
            ) : (
              <ul className="approval-mini-list">
                {confirmations.slice(0, 3).map((item) => (
                  <li key={item.id}>
                    <strong>{item.target_object_title || item.action}</strong>
                    <small>{item.permission_summary}</small>
                  </li>
                ))}
              </ul>
            )}
            <Link className="text-link" to="/self">
              {t("world.approvals.open")}
            </Link>
          </section>

          <section className="card">
            <h2>{t("world.quick.title")}</h2>
            <ul className="quick-links">
              <li>
                <Link to="/objects">{t("world.quick.objects")}</Link>
              </li>
              <li>
                <Link to="/ai">{t("world.quick.ai")}</Link>
              </li>
              <li>
                <Link to="/self">{t("world.quick.self")}</Link>
              </li>
            </ul>
          </section>

        </aside>
      </div>
    </div>
  );
}
