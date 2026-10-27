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
  const [nodePositions, setNodePositions] = useState<Record<string, { x: number; y: number }>>({});
  const layoutKey = `dw:layout:v2:${user?.id ?? "anonymous"}`;
  useEffect(() => {
    try {
      const saved: unknown = JSON.parse(window.localStorage.getItem(layoutKey) || "{}");
      const validated: Record<string, { x: number; y: number }> = {};
      if (saved && typeof saved === "object" && !Array.isArray(saved)) {
        for (const [id, point] of Object.entries(saved)) {
          const p = point as { x?: number; y?: number } | null;
          if (/^[0-9a-f-]{36}$/i.test(id) && p &&
              Number.isFinite(p.x) && Number.isFinite(p.y) &&
              p.x! >= 110 && p.x! < 20000 && p.y! >= 110 && p.y! < 20000) {
            validated[id] = { x: p.x!, y: p.y! };
          }
        }
      }
      setNodePositions(validated);
    } catch { setNodePositions({}); }
  }, [layoutKey]);
  const moveWorldNode = useCallback((id: string, x: number, y: number) => {
    setNodePositions((previous) => {
      const next = { ...previous, [id]: { x, y } };
      try { window.localStorage.setItem(layoutKey, JSON.stringify(next)); } catch { /* optional storage */ }
      return next;
    });
  }, [layoutKey]);
  const nodeDrag = useRef<{ id: string; pointer: number; startX: number; startY: number; nodeX: number; nodeY: number } | null>(null);
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


  const graph = useMemo(() => {
    const nodes = stage?.objects ?? [];
    const byId = new Map(nodes.map((node) => [node.id, node]));
    const placed = new Map<string, { x: number; y: number; depth: number }>();
    const visiting = new Set<string>();
    let nextLeaf = 0;
    let maxDepth = 0;

    const visit = (object: StageObject, depth: number): { x: number; y: number; depth: number } | null => {
      const existing = placed.get(object.id);
      if (existing) return existing;
      if (visiting.has(object.id) || depth > 24) return null;
      visiting.add(object.id);
      maxDepth = Math.max(maxDepth, depth);
      const children = (childrenOf.get(object.id) ?? []).filter((child) => byId.has(child.id));
      const childPositions = children
        .map((child) => visit(child, depth + 1))
        .filter((position): position is { x: number; y: number; depth: number } => position !== null);
      const x = childPositions.length
        ? ((childPositions[0]?.x ?? 200) + (childPositions[childPositions.length - 1]?.x ?? 200)) / 2
        : 200 + nextLeaf++ * 290;
      const result = { x, y: 178 + depth * 278, depth };
      placed.set(object.id, result);
      visiting.delete(object.id);
      return result;
    };
    roots.forEach((root) => visit(root, 0));
    nodes.forEach((node) => visit(node, 0));
    const xs = [...placed.values()].map((position) => position.x);
    const minX = Math.min(200, ...xs);
    const maxX = Math.max(200, ...xs);
    const width = Math.max(940, maxX - minX + 400);
    const offset = width / 2 - (maxX + minX) / 2;
    const positions = new Map(
      [...placed].map(([id, p]) => [id, { x: p.x + offset, y: p.y, depth: p.depth }])
    );
    const height = Math.max(580, 178 + maxDepth * 278 + 200);
    for (const [id, point] of Object.entries(nodePositions)) {
      const prior = positions.get(id);
      if (prior) positions.set(id, { ...prior, x: point.x, y: point.y });
    }
    const edges = (stage?.edges ?? [])
      .map((edge) => {
        const parent = positions.get(edge.parent_id);
        const child = positions.get(edge.child_id);
        return parent && child ? { id: `${edge.parent_id}:${edge.child_id}`, parent, child } : null;
      })
      .filter((edge): edge is { id: string; parent: {x:number;y:number;depth:number}; child: {x:number;y:number;depth:number} } => edge !== null);
    return { positions, width, height, edges };
  }, [stage, roots, childrenOf, nodePositions]);

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


  function renderNode(object: StageObject) {
    const point = graph.positions.get(object.id);
    if (!point) return null;
    const canDerive = object.permissions.includes("derive");
    return (
      <li
        key={object.id}
        className="stage-node-wrap dw-orb-wrap"
        style={{ left: point.x - 110, top: point.y - 110 }}
      >
        <article
          className={`stage-node dw-world-orb stage-node-${object.object_type}${
            selectedId === object.id ? " stage-node-selected" : ""
          }`}
          data-testid="dw-stage-node"
          aria-current={selectedId === object.id ? "true" : undefined}
          draggable={canDerive}
          onDragStart={(event) => {
            if (!canDerive) return;
            event.dataTransfer.setData("text/x-derive-object", object.id);
            event.dataTransfer.effectAllowed = "copy";
            setDraggingId(object.id);
          }}
          onDragEnd={() => setDraggingId(null)}
        >
          <button
            className="dw-node-move-handle"
            type="button"
            title={lang === "zh" ? "方向键移动世界节点，Shift 加速" : "Arrow keys move this world node; Shift for larger steps"}
            aria-label={lang === "zh" ? `移动 ${object.title}` : `Move ${object.title}`}
            onPointerDown={(event) => {
              if (event.button !== 0) return;
              event.preventDefault();
              event.stopPropagation();
              event.currentTarget.setPointerCapture(event.pointerId);
              nodeDrag.current = { id: object.id, pointer: event.pointerId, startX: event.clientX,
                startY: event.clientY, nodeX: point.x, nodeY: point.y };
            }}
            onPointerMove={(event) => {
              const drag = nodeDrag.current;
              if (!drag || drag.id !== object.id || drag.pointer !== event.pointerId) return;
              event.preventDefault();
              moveWorldNode(object.id,
                Math.max(110, Math.min(graph.width - 110, drag.nodeX + (event.clientX - drag.startX) / zoom)),
                Math.max(110, Math.min(graph.height - 110, drag.nodeY + (event.clientY - drag.startY) / zoom)));
            }}
            onPointerUp={(event) => {
              if (nodeDrag.current?.pointer === event.pointerId) nodeDrag.current = null;
              if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
            }}
            onPointerCancel={() => { nodeDrag.current = null; }}
            onKeyDown={(event) => {
              const step = event.shiftKey ? 40 : 12;
              const delta = event.key === "ArrowLeft" ? [-step, 0] :
                event.key === "ArrowRight" ? [step, 0] :
                event.key === "ArrowUp" ? [0, -step] :
                event.key === "ArrowDown" ? [0, step] : null;
              if (!delta) return;
              event.preventDefault();
              moveWorldNode(object.id, Math.max(110, Math.min(graph.width - 110, point.x + delta[0])),
                Math.max(110, Math.min(graph.height - 110, point.y + delta[1])));
            }}
          >⠿</button>
          <div className="stage-node-top">
            <span className={`template-badge template-${object.object_type}`}>{typeLabel(object.object_type)}</span>
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
              <span className="dw-orb-mark" aria-hidden="true">✧</span>
              <span className="dw-orb-name">{object.title || t("common.untitled")}</span>
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
          {selectedId === object.id ? (
            <div className="stage-node-actions">
              <button className="button button-small" type="button" onClick={() => openInspector(object.id, "work")}>
                {t("stage.workHere")}
              </button>
              {canDerive ? <button className="button button-ghost button-small" type="button" onClick={() => openInspector(object.id, "branch")}>{t("stage.branchHere")}</button> : null}
              <button className="button button-ghost button-small" type="button" onClick={() => openInspector(object.id, "lens")}>{t("stage.lensHere")}</button>
              <button className="button button-ghost button-small" type="button" onClick={() => openInspector(object.id, "ai")}>{t("stage.aiHere")}</button>
              <Link className="text-link" to={`/objects/${object.id}`}>{t("stage.openPage")}</Link>
            </div>
          ) : null}
        </article>
      </li>
    );
  }

  return (
    <div className="page page-wide stage-page" data-testid="world-stage">
      <div className="page-heading with-actions stage-heading">
        <div>
          <p className="eyebrow">{t("stage.eyebrow")}</p>
          <h1 className="dw-world-hero-title">{lang === "zh" ? <>让一个念头，<em>长出自己的未来。</em></> : <>Let an idea <em>grow its own future.</em></>}</h1>
          <p className="lead">{lang === "zh" ? "触碰一个创造物，直接改变它。衍生一条分支，看看不同的选择会走向哪里。" : "Touch a creation to change it. Derive a new direction and see what becomes possible."}</p>
          <p className="dw-world-hello">{lang === "zh" ? `属于 ${displayName} 的持续世界` : `${displayName}'s living world`}</p>
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

      <div className="stage-stats dw-world-facts">
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

      <div className="stage-layout dw-focused-world">
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
            <div className="stage-empty dw-world-first-scene">
              <h2>{t("stage.emptyTitle")}</h2>
              <p>{t("stage.emptyBody")}</p>
              <div className="stage-empty-actions dw-world-first-actions">
                <Link
                  className="stage-entry dw-first-choice"
                  to="/objects/new?template=living_scene&purpose=project&first=1"
                >
                  <strong>{t("stage.entryProject")}</strong>
                  <small>{t("stage.entryProjectHint")}</small>
                </Link>
                <Link
                  className="stage-entry dw-first-choice"
                  to="/objects/new?template=living_scene&purpose=story&first=1"
                >
                  <strong>{t("stage.entryStory")}</strong>
                  <small>{t("stage.entryStoryHint")}</small>
                </Link>
                <Link className="stage-entry dw-first-choice" to="/explore">
                  <strong>{t("stage.entryExplore")}</strong>
                  <small>{t("stage.entryExploreHint")}</small>
                </Link>
              </div>
            </div>
          ) : (
            <div className="dw-stage-scrollspace" style={{ width: graph.width * zoom, height: graph.height * zoom }}>
              <div className="dw-stage-viewport" data-testid="dw-stage-viewport" data-zoom={zoom.toFixed(2)} style={{ width: graph.width, height: graph.height, transform: `scale(${zoom})` }}>
                <svg className="dw-world-lines" viewBox={`0 0 ${graph.width} ${graph.height}`} aria-hidden="true" preserveAspectRatio="none">
                  {graph.edges.map((edge) => (
                    <path key={edge.id} d={`M ${edge.parent.x} ${edge.parent.y} C ${edge.parent.x} ${edge.parent.y + 145}, ${edge.child.x} ${edge.child.y - 145}, ${edge.child.x} ${edge.child.y}`} />
                  ))}
                </svg>
                <ul className="stage-roots">{(stage?.objects ?? []).map((object) => renderNode(object))}</ul>
              </div>
            </div>
          )}
          {continueObject ? (
            <button className="dw-ai-orb" type="button" onClick={() => openInspector(selectedId ?? continueObject.id, "ai")} aria-label={lang === "zh" ? "向当前世界的 Personal AI 寻求协助" : "Ask your Personal AI about this world"}>
              <span aria-hidden="true">✧</span><small>{lang === "zh" ? "AI 在此" : "AI here"}</small>
            </button>
          ) : null}
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
        <aside className="stage-side dw-context-hub">
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
          <details className="dw-context-detail"><summary>{t("stage.approvals")}{confirmations.length > 0 ? ` (${confirmations.length})` : ""}</summary>
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
          </details>
          <details className="stage-quick dw-stage-quick"><summary>{t("stage.quick")}</summary>
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
