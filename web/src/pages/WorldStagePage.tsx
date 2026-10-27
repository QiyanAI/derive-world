// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";
import {
  getObject,
  getWorldStage,
  listConfirmations,
  type Confirmation,
  type ObjectView,
  type StageObject,
  type WorldStage
} from "../api";
import PulsePanel from "../components/PulsePanel";
import StageInspector, { type StageInspectorTab } from "../components/StageInspector";
import { ErrorNotice } from "../errors";
import { actionKey, eventKey } from "../humanize";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";

export default function WorldStagePage() {
  const { t, lang } = useI18n();
  const { user } = useAuth();
  const [stage, setStage] = useState<WorldStage | null>(null);
  const [confirmations, setConfirmations] = useState<Confirmation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [selected, setSelected] = useState<ObjectView | null>(null);
  const [selectedLoading, setSelectedLoading] = useState(false);
  const [selectedError, setSelectedError] = useState<unknown>(null);
  const [inspectorOpen, setInspectorOpen] = useState(false);
  const [inspectorTab, setInspectorTab] = useState<StageInspectorTab>("work");
  const [draggingId, setDraggingId] = useState<string | null>(null);
  const [zoom, setZoom] = useState(1);
  const selectRequest = useRef(0);

  const load = useCallback(async (signal?: AbortSignal) => {
    setError(null);
    try {
      const [data, pending] = await Promise.all([
        getWorldStage(signal),
        listConfirmations("pending", signal).catch(() => ({ items: [] }))
      ]);
      if (!signal?.aborted) {
        setStage(data);
        setConfirmations(pending.items);
      }
    } catch (failure) {
      if (!signal?.aborted) {
        setError(failure);
      }
    } finally {
      if (!signal?.aborted) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  useEffect(() => {
    let source: EventSource | null = null;
    let fallback: number | null = null;
    const startPolling = () => {
      if (fallback !== null) {
        return;
      }
      fallback = window.setInterval(() => {
        if (!document.hidden) {
          void load();
        }
      }, 15000);
    };
    try {
      source = new EventSource("/api/v1/stream");
      source.addEventListener("refresh", () => void load());
      source.addEventListener("reconnect", () => {
        source?.close();
        startPolling();
      });
      source.onerror = () => {
        source?.close();
        source = null;
        startPolling();
      };
    } catch {
      startPolling();
    }
    const onVisible = () => {
      if (!document.hidden) {
        void load();
      }
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      source?.close();
      if (fallback !== null) {
        window.clearInterval(fallback);
      }
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [load]);

  const loadSelected = useCallback(async (objectID: string) => {
    const request = selectRequest.current + 1;
    selectRequest.current = request;
    setSelectedLoading(true);
    setSelectedError(null);
    try {
      const object = await getObject(objectID);
      if (selectRequest.current === request) {
        setSelected(object);
        setSelectedId(objectID);
      }
    } catch (failure) {
      if (selectRequest.current === request) {
        setSelectedError(failure);
        setSelectedId(objectID);
      }
    } finally {
      if (selectRequest.current === request) {
        setSelectedLoading(false);
      }
    }
  }, []);

  const openInspector = useCallback(
    (objectID: string, tab: StageInspectorTab) => {
      setInspectorOpen(true);
      setInspectorTab(tab);
      setSelectedId(objectID);
      void loadSelected(objectID);
    },
    [loadSelected]
  );

  const closeInspector = useCallback(() => {
    const previousID = selectedId;
    selectRequest.current += 1;
    setInspectorOpen(false);
    setSelected(null);
    setSelectedId(null);
    if (previousID) {
      window.setTimeout(() => {
        const trigger = document.querySelector<HTMLElement>(
          `[data-stage-object="${previousID}"]`
        );
        trigger?.focus();
      }, 50);
    }
  }, [selectedId]);

  const handleObjectUpdated = useCallback(
    async (updated: ObjectView) => {
      setSelected(updated);
      await load();
    },
    [load]
  );

  const handleObjectReload = useCallback(async () => {
    if (selectedId) {
      await loadSelected(selectedId);
    }
  }, [loadSelected, selectedId]);

  const handleDerived = useCallback((child: ObjectView) => {
    setSelected(child);
    setSelectedId(child.id);
    setInspectorTab("work");
    setInspectorOpen(true);
  }, []);

  useEffect(() => {
    if (!selectedId || selectedLoading || !selected || !stage) {
      return;
    }
    const stageObject = stage.objects.find((object) => object.id === selectedId);
    if (stageObject && stageObject.version !== selected.version) {
      void loadSelected(selectedId);
    }
  }, [stage, selectedId, selected, selectedLoading, loadSelected]);

  const { roots, childrenOf } = useMemo(() => {
    const objects = stage?.objects ?? [];
    const edges = stage?.edges ?? [];
    const byId = new Map(objects.map((object) => [object.id, object]));
    const childIDs = new Set(edges.map((edge) => edge.child_id));
    const childMap = new Map<string, StageObject[]>();
    for (const edge of edges) {
      const child = byId.get(edge.child_id);
      if (!child) {
        continue;
      }
      const list = childMap.get(edge.parent_id) ?? [];
      list.push(child);
      childMap.set(edge.parent_id, list);
    }
    for (const list of childMap.values()) {
      list.sort((left, right) => right.updated_at.localeCompare(left.updated_at));
    }
    return {
      roots: objects
        .filter((object) => !childIDs.has(object.id))
        .sort((left, right) => right.updated_at.localeCompare(left.updated_at)),
      childrenOf: childMap
    };
  }, [stage]);

  const continueObject = useMemo(() => {
    const objects = stage?.objects ?? [];
    if (objects.length === 0) {
      return null;
    }
    const preferred = objects.filter(
      (object) => object.permissions.includes("contribute") || object.permissions.includes("manage")
    );
    const candidates = preferred.length > 0 ? preferred : objects;
    return [...candidates].sort((left, right) => right.updated_at.localeCompare(left.updated_at))[0] ?? null;
  }, [stage]);

  const branchCount = stage?.edges.length ?? 0;
  const displayName = user?.display_name ?? "";

  function typeLabel(type: StageObject["object_type"]): string {
    if (type === "living_scene") {
      return t("object.create.scene");
    }
    if (type === "interactive") {
      return t("object.create.interactive");
    }
    return t("object.create.collaborative");
  }

  function renderNode(object: StageObject, depth: number, visited: Set<string>) {
    if (visited.has(object.id) || depth > 5) {
      return null;
    }
    const nextVisited = new Set(visited);
    nextVisited.add(object.id);
    const children = childrenOf.get(object.id) ?? [];
    return (
      <li key={object.id} className="stage-node-wrap">
        <article
          data-testid="dw-stage-node"
          className={`stage-node stage-node-${object.object_type}${
            selectedId === object.id ? " stage-node-selected" : ""
          }`}
          aria-current={selectedId === object.id ? "true" : undefined}
          draggable={object.permissions.includes("derive")}
          onDragStart={(event) => {
            event.dataTransfer.setData("text/x-derive-object", object.id);
            event.dataTransfer.effectAllowed = "copy";
            setDraggingId(object.id);
          }}
          onDragEnd={() => setDraggingId(null)}
        >
          <div className="stage-node-top">
            <span className={`template-badge template-${object.object_type}`}>
              {typeLabel(object.object_type)}
            </span>
            <span className="version-badge">v{object.version}</span>
          </div>
          <h3>
            <button
              className="stage-node-title-button"
              type="button"
              onClick={() => openInspector(object.id, "work")}
              data-stage-object={object.id}
              aria-label={t("stage.selectNamed", { title: object.title || t("common.untitled") })}
            >
              {object.title || t("common.untitled")}
            </button>
          </h3>
          <p className="stage-node-meta">
            {object.member_count > 1
              ? t("stage.members", { count: object.member_count })
              : t("stage.solo")}
            {object.branch_count > 0 ? ` · ${t("stage.branches", { count: object.branch_count })}` : ""}
          </p>
          {object.latest_event ? (
            <p className="stage-node-event">
              <span className="stage-event-dot" aria-hidden="true" />
              {object.latest_event.action
                ? t(actionKey(object.latest_event.action))
                : t(eventKey(object.latest_event.event_type))}
              {object.latest_event.actor_name ? ` · ${object.latest_event.actor_name}` : ""}
            </p>
          ) : null}
          {selectedId === object.id ? <div className="stage-node-actions">
            <button
              className="button button-small"
              type="button"
              onClick={() => openInspector(object.id, "work")}
            >
              {t("stage.workHere")}
            </button>
            {object.permissions.includes("derive") ? (
              <button
                className="button button-ghost button-small"
                type="button"
                onClick={() => openInspector(object.id, "branch")}
              >
                {t("stage.branchHere")}
              </button>
            ) : null}
            <button
              className="button button-ghost button-small"
              type="button"
              onClick={() => openInspector(object.id, "lens")}
            >
              {t("stage.lensHere")}
            </button>
            <button
              className="button button-ghost button-small"
              type="button"
              onClick={() => openInspector(object.id, "ai")}
            >
              {t("stage.aiHere")}
            </button>
            <Link className="text-link" to={`/objects/${object.id}`}>
              {t("stage.openPage")}
            </Link>
          </div> : null}
          {object.permissions.includes("derive") ? (
            <p className="stage-drag-hint">{t("stage.dragHint")}</p>
          ) : null}
        </article>
        {children.length > 0 ? (
          <ul className="stage-children">
            {children.map((child) => renderNode(child, depth + 1, nextVisited))}
          </ul>
        ) : null}
      </li>
    );
  }

  return (
    <div className="page page-wide stage-page" data-testid="world-stage">
      <div className="page-heading with-actions stage-heading">
        <div>
          <p className="eyebrow">{t("stage.eyebrow")}</p>
          <h1>{t("stage.title", { name: displayName })}</h1>
          <p className="lead">{t("stage.lead")}</p>
        </div>
        <div className="stage-create">
          <Link className="button" to="/objects/new?template=living_scene&purpose=project">
            {t("stage.createProject")}
          </Link>
          <Link className="button button-ghost" to="/objects/new?template=living_scene&purpose=story">
            {t("stage.createStory")}
          </Link>
          <Link className="button button-ghost" to="/explore">
            {t("stage.explore")}
          </Link>
        </div>
      </div>

      {error ? <ErrorNotice error={error} onRetry={() => void load()} /> : null}

      <div className="stage-stats">
        <span>
          <strong>{stage?.objects.length ?? 0}</strong> {t("stage.statObjects")}
        </span>
        <span>
          <strong>{branchCount}</strong> {t("stage.statBranches")}
        </span>
        <span>
          <strong>{roots.length}</strong> {t("stage.statRoots")}
        </span>
        <button className="button button-ghost button-small" type="button" onClick={() => void load()}>
          {t("common.refresh")}
        </button>
      </div>

      <div className="stage-layout">
        <section
          className={`stage-board${draggingId ? " stage-board-dragging" : ""}`}
          aria-label={t("stage.boardLabel")}
          onDragOver={(event) => {
            if (Array.from(event.dataTransfer.types).includes("text/x-derive-object")) {
              event.preventDefault();
              event.dataTransfer.dropEffect = "copy";
            }
          }}
          onDrop={(event) => {
            const droppedID = event.dataTransfer.getData("text/x-derive-object");
            setDraggingId(null);
            if (droppedID) {
              event.preventDefault();
              openInspector(droppedID, "branch");
            }
          }}
        >
          <div className="dw-stage-topbar">
            <div className="dw-stage-topbar-title">
              <strong>{lang === "zh" ? "正在演化的世界" : "Your living world"}</strong>
              <small>{lang === "zh" ? "选择一个创造物，直接改变或衍生" : "Select a creation to change or derive it"}</small>
            </div>
            <div className="dw-stage-zoom-controls" role="group" aria-label={lang === "zh" ? "世界视角缩放" : "World zoom controls"}>
              <button type="button" data-testid="dw-stage-zoom-out" aria-label={lang === "zh" ? "缩小世界" : "Zoom out"} disabled={zoom <= 0.7} onClick={() => setZoom((value) => Math.max(0.7, Math.round((value - 0.15) * 100) / 100))}>−</button>
              <output aria-live="off" aria-label={lang === "zh" ? "缩放比例" : "Zoom level"}>{Math.round(zoom * 100)}%</output>
              <button type="button" data-testid="dw-stage-zoom-in" aria-label={lang === "zh" ? "放大世界" : "Zoom in"} disabled={zoom >= 1.45} onClick={() => setZoom((value) => Math.min(1.45, Math.round((value + 0.15) * 100) / 100))}>+</button>
              <button type="button" data-testid="dw-stage-zoom-reset" aria-label={lang === "zh" ? "恢复世界视角" : "Reset view"} onClick={() => setZoom(1)}>↺</button>
            </div>
          </div>
          {draggingId ? <div className="stage-drop-target">{t("stage.dropBranch")}</div> : null}
          {loading && !stage ? <p className="muted">{t("common.loading")}</p> : null}
          {!loading && (stage?.objects.length ?? 0) === 0 ? (
            <div className="stage-empty">
              <h2>{t("stage.emptyTitle")}</h2>
              <p>{t("stage.emptyBody")}</p>
              <div className="stage-empty-actions">
                <Link
                  className="card stage-entry"
                  to="/objects/new?template=living_scene&purpose=project&first=1"
                >
                  <strong>{t("stage.entryProject")}</strong>
                  <small>{t("stage.entryProjectHint")}</small>
                </Link>
                <Link
                  className="card stage-entry"
                  to="/objects/new?template=living_scene&purpose=story&first=1"
                >
                  <strong>{t("stage.entryStory")}</strong>
                  <small>{t("stage.entryStoryHint")}</small>
                </Link>
                <Link className="card stage-entry" to="/explore">
                  <strong>{t("stage.entryExplore")}</strong>
                  <small>{t("stage.entryExploreHint")}</small>
                </Link>
              </div>
            </div>
          ) : (
            <div className="dw-stage-viewport" data-testid="dw-stage-viewport" data-zoom={zoom.toFixed(2)} style={{ transform: `scale(${zoom})` }}>
              <ul className="stage-roots">{roots.map((object) => renderNode(object, 0, new Set()))}</ul>
            </div>
          )}
          {inspectorOpen ? <div className="dw-stage-inspector-dock">
{inspectorOpen ? (
            <StageInspector
              key={selected?.id ?? "none"}
              object={selected}
              tab={inspectorTab}
              loading={selectedLoading}
              error={selectedError}
              onTab={setInspectorTab}
              onClose={closeInspector}
              onUpdated={handleObjectUpdated}
              onReload={handleObjectReload}
              onRefresh={() => load()}
              onDerived={handleDerived}
            />
          ) : null}
          </div> : null}
        </section>
        <aside className="stage-side">
          {continueObject ? (
            <section className="card stage-continue" data-testid="stage-continue">
              <p className="eyebrow">{t("stage.continueEyebrow")}</p>
              <h2>{continueObject.title || t("common.untitled")}</h2>
              <p className="muted">
                {t("stage.continueHint")} · v{continueObject.version}
                {continueObject.latest_event?.actor_name
                  ? ` · ${continueObject.latest_event.actor_name}`
                  : ""}
              </p>
              <Link
                className="button button-small"
                to={`/objects/${continueObject.id}?tab=work`}
              >
                {t("stage.continueAction")}
              </Link>
            </section>
          ) : null}
          <PulsePanel compact refreshKey={stage?.generated_at ?? ""} />
          <section className="card">
            <h2>{t("stage.approvals")}</h2>
            {confirmations.length === 0 ? (
              <p className="muted">{t("stage.noApprovals")}</p>
            ) : (
              <ul className="pulse-list">
                {confirmations.slice(0, 5).map((confirmation) => (
                  <li key={confirmation.id}>
                    <div>
                      <strong>{confirmation.target_object_title || confirmation.action}</strong>
                      <small>{confirmation.permission_summary}</small>
                    </div>
                    <Link className="text-link" to="/self">
                      {t("stage.review")}
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </section>
          <details className="card stage-quick dw-stage-quick"><summary>{t("stage.quick")}</summary>
            <Link to="/objects">{t("nav.objects")}</Link>
            <Link to="/friends">{t("nav.friends")}</Link>
            <Link to="/ai">{t("nav.ai")}</Link>
            <Link to="/welcome">{t("nav.welcome")}</Link>
            <Link to="/settings">{t("nav.settings")}</Link>
          </details>
        </aside>
      </div>
    </div>
  );
}
