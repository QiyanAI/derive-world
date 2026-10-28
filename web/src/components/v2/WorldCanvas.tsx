// SPDX-License-Identifier: Apache-2.0
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type ReactNode
} from "react";
import type { StageObject, WorldStage } from "../../api";
import { useI18n } from "../../i18n";
import Icon from "./Icon";

export interface WorldPosition {
  x: number;
  y: number;
}

interface PositionedNode {
  object: StageObject;
  x: number;
  y: number;
  depth: number;
  size: number;
  tone: "mint" | "violet" | "peach" | "blue";
}

interface PositionedEdge {
  id: string;
  parent: WorldPosition;
  child: WorldPosition;
}

const WORLD_WIDTH = 1500;
const WORLD_HEIGHT = 950;
const MIN_ZOOM = 0.42;
const MAX_ZOOM = 1.45;
const tones = ["mint", "violet", "peach", "blue"] as const;

// Overlays inside the stage own their own wheel, touch, and pointer gestures.
// The marker attribute is authoritative; the class list keeps older DOM
// variants safe. Keep this list in sync with the elements rendered below.
const STAGE_UI_SELECTOR = [
  "[data-dw-stage-ui]",
  ".dw-stage-top",
  ".dw-stage-bottom",
  ".dw-stage-inspector-shell",
  ".dw-stage-inspector",
  ".dw-inspector",
  ".dw-stage-onboarding",
  ".dw-canvas-tools",
  ".dw-ai-orb",
  ".dw-pulse-mini"
].join(", ");

function isStageUI(target: EventTarget | null): boolean {
  return Boolean((target as HTMLElement | null)?.closest?.(STAGE_UI_SELECTOR));
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function hash(value: string): number {
  let result = 0;
  for (let index = 0; index < value.length; index += 1) {
    result = (result * 31 + value.charCodeAt(index)) >>> 0;
  }
  return result;
}

function nodeSize(depth: number): number {
  if (depth === 0) {
    return 232;
  }
  if (depth === 1) {
    return 184;
  }
  return 158;
}

function toneFor(object: StageObject, depth: number): PositionedNode["tone"] {
  if (depth === 0) {
    return object.purpose === "story" ? "violet" : "mint";
  }
  return tones[hash(object.id) % tones.length] ?? "blue";
}

function nodeIcon(object: StageObject): "idea" | "book" | "layers" {
  if (object.purpose === "story") {
    return "book";
  }
  if (object.purpose === "project") {
    return "idea";
  }
  return object.object_type === "living_scene" ? "layers" : "idea";
}

export default function WorldCanvas({
  stage,
  layout,
  selectedId,
  focusId,
  onSelect,
  onBranch,
  onPositionChange,
  onBackgroundClick,
  pulse,
  aiOrb,
  inspector,
  emptyState
}: {
  stage: WorldStage | null;
  layout: Record<string, WorldPosition>;
  selectedId: string | null;
  focusId?: string | null;
  onSelect: (id: string) => void;
  onBranch: (id: string, at: WorldPosition) => void;
  onPositionChange: (id: string, position: WorldPosition) => void;
  onBackgroundClick: () => void;
  pulse?: ReactNode;
  aiOrb?: ReactNode;
  inspector?: ReactNode;
  emptyState?: ReactNode;
}) {
  const { lang } = useI18n();
  const zh = lang === "zh";
  const containerRef = useRef<HTMLDivElement>(null);
  const worldRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<
    | { kind: "pan"; clientX: number; clientY: number; originX: number; originY: number; moved: boolean }
    | { kind: "node"; id: string; clientX: number; clientY: number; originX: number; originY: number; moved: boolean }
    | { kind: "branch"; id: string; moved: boolean }
    | null
  >(null);
  const callbacks = useRef({ onSelect, onBranch, onPositionChange, onBackgroundClick });
  callbacks.current = { onSelect, onBranch, onPositionChange, onBackgroundClick };
  const interacted = useRef(false);
  const branchPointerUsed = useRef(false);
  const [view, setView] = useState({ x: 0, y: 0, zoom: 0.9 });
  const [dragPosition, setDragPosition] = useState<{ id: string; x: number; y: number } | null>(null);
  const dragPositionRef = useRef<{ id: string; x: number; y: number } | null>(null);
  const [ghost, setGhost] = useState<{ x: number; y: number } | null>(null);
  const [draggingNode, setDraggingNode] = useState(false);
  const [stageWidth, setStageWidth] = useState(0);
  const inspectorOpen = Boolean(inspector);
  const inspectorRef = useRef(inspectorOpen);
  inspectorRef.current = inspectorOpen;

  const graph = useMemo(() => {
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
    const roots = objects
      .filter((object) => !childIDs.has(object.id))
      .sort((left, right) => right.updated_at.localeCompare(left.updated_at));
    const positions = new Map<string, { x: number; y: number; depth: number }>();
    const visiting = new Set<string>();

    function place(object: StageObject, depth: number, x: number, y: number) {
      if (positions.has(object.id) || visiting.has(object.id) || depth > 12) {
        return;
      }
      visiting.add(object.id);
      positions.set(object.id, {
        x: clamp(x, 110, WORLD_WIDTH - 110),
        y: clamp(y, 100, WORLD_HEIGHT - 100),
        depth
      });
      const children = (childMap.get(object.id) ?? []).filter((child) => byId.has(child.id));
      const radius = depth === 0 ? 282 : 226;
      children.forEach((child, index) => {
        const angle = (Math.PI * 2 * index) / Math.max(children.length, 1) + Math.PI / 4;
        place(
          child,
          depth + 1,
          x + Math.cos(angle) * radius,
          y + Math.sin(angle) * radius
        );
      });
      visiting.delete(object.id);
    }

    if (roots.length === 1) {
      const root = roots[0];
      if (root) {
        place(root, 0, WORLD_WIDTH / 2, 390);
      }
    } else {
      roots.forEach((root, index) => {
        const columns = Math.min(3, Math.max(1, Math.ceil(Math.sqrt(roots.length))));
        const column = index % columns;
        const row = Math.floor(index / columns);
        const stepX = WORLD_WIDTH / (columns + 1);
        place(root, 0, stepX * (column + 1), 245 + row * 270);
      });
    }
    // The stage can contain a cycle or an orphan that is not reachable from a
    // root. Keep every visible node on the canvas rather than dropping it.
    objects.forEach((object, index) => {
      if (!positions.has(object.id)) {
        place(object, 0, 240 + (index % 4) * 330, 220 + Math.floor(index / 4) * 260);
      }
    });

    for (const [id, point] of Object.entries(layout)) {
      const existing = positions.get(id);
      if (existing) {
        positions.set(id, { ...existing, x: point.x, y: point.y });
      }
    }
    if (dragPosition) {
      const existing = positions.get(dragPosition.id);
      if (existing) {
        positions.set(dragPosition.id, { ...existing, x: dragPosition.x, y: dragPosition.y });
      }
    }

    const nodes: PositionedNode[] = [];
    for (const object of objects) {
      const point = positions.get(object.id);
      if (!point) {
        continue;
      }
      nodes.push({
        object,
        x: point.x,
        y: point.y,
        depth: point.depth,
        size: nodeSize(point.depth),
        tone: toneFor(object, point.depth)
      });
    }
    const graphEdges: PositionedEdge[] = [];
    for (const edge of edges) {
      const parent = positions.get(edge.parent_id);
      const child = positions.get(edge.child_id);
      if (parent && child) {
        graphEdges.push({ id: `${edge.parent_id}:${edge.child_id}`, parent, child });
      }
    }
    return { nodes, edges: graphEdges };
  }, [stage, layout, dragPosition]);

  const fit = useCallback(
    (focus?: string | null) => {
      const element = containerRef.current;
      if (!element) {
        return;
      }
      const rect = element.getBoundingClientRect();
      if (rect.width < 40 || rect.height < 40) {
        return;
      }
      if (focus) {
        const node = graph.nodes.find((entry) => entry.object.id === focus);
        if (node) {
          const inspectReserve = inspectorRef.current ? Math.min(400, rect.width * 0.32) : 0;
          const zoom = clamp(
            Math.min((rect.width - inspectReserve) / 1050, rect.height / 760),
            MIN_ZOOM,
            1.05
          );
          setView({
            x: (rect.width - inspectReserve) / 2 - node.x * zoom,
            y: rect.height / 2 - node.y * zoom,
            zoom
          });
          return;
        }
      }
      if (graph.nodes.length === 0) {
        const zoom = clamp(Math.min(rect.width / WORLD_WIDTH, rect.height / WORLD_HEIGHT) * 0.96, MIN_ZOOM, 1.05);
        setView({
          x: (rect.width - WORLD_WIDTH * zoom) / 2,
          y: (rect.height - WORLD_HEIGHT * zoom) / 2,
          zoom
        });
        return;
      }
      const minX = Math.min(...graph.nodes.map((node) => node.x - node.size / 2));
      const maxX = Math.max(...graph.nodes.map((node) => node.x + node.size / 2));
      const minY = Math.min(...graph.nodes.map((node) => node.y - node.size / 2));
      const maxY = Math.max(...graph.nodes.map((node) => node.y + node.size / 2));
      const width = Math.max(420, maxX - minX);
      const height = Math.max(360, maxY - minY);
      const zoom = clamp(
        Math.min(rect.width / (width + 260), rect.height / (height + 220)),
        MIN_ZOOM,
        1.05
      );
      setView({
        x: (rect.width - (minX + maxX) * zoom) / 2,
        y: (rect.height - (minY + maxY) * zoom) / 2,
        zoom
      });
    },
    [graph.nodes]
  );

  useEffect(() => {
    const element = containerRef.current;
    if (!element) {
      return;
    }
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (!entry) {
        return;
      }
      setStageWidth(entry.contentRect.width);
      if (!interacted.current) {
        fit();
      }
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [fit]);

  useEffect(() => {
    if (!interacted.current) {
      fit();
    }
  }, [stage?.generated_at, graph.nodes.length, fit]);

  const fitRef = useRef(fit);
  fitRef.current = fit;
  useEffect(() => {
    if (focusId) {
      fitRef.current(focusId);
      interacted.current = true;
    }
  }, [focusId, inspectorOpen]);

  useEffect(() => {
    const element = containerRef.current;
    if (!element) {
      return;
    }
    function onWheel(event: WheelEvent) {
      // A wheel gesture that starts inside an overlay belongs to that overlay.
      // Never preventDefault here: the inspector and other panels keep native
      // scrolling (including scroll chaining inside their own bounds).
      if (isStageUI(event.target)) {
        return;
      }
      event.preventDefault();
      interacted.current = true;
      const rect = element!.getBoundingClientRect();
      const cursorX = event.clientX - rect.left;
      const cursorY = event.clientY - rect.top;
      setView((current) => {
        const next = clamp(current.zoom * (event.deltaY > 0 ? 0.91 : 1.1), MIN_ZOOM, MAX_ZOOM);
        return {
          zoom: next,
          x: cursorX - ((cursorX - current.x) / current.zoom) * next,
          y: cursorY - ((cursorY - current.y) / current.zoom) * next
        };
      });
    }
    element.addEventListener("wheel", onWheel, { passive: false });
    return () => element.removeEventListener("wheel", onWheel);
  }, []);

  useEffect(() => {
    function onMove(event: PointerEvent) {
      const drag = dragRef.current;
      if (!drag || drag.kind === "branch") {
        return;
      }
      const dx = event.clientX - drag.clientX;
      const dy = event.clientY - drag.clientY;
      if (Math.abs(dx) + Math.abs(dy) > 6) {
        drag.moved = true;
      }
      if (drag.kind === "pan") {
        interacted.current = true;
        setView((current) => ({
          ...current,
          x: drag.originX + dx,
          y: drag.originY + dy
        }));
      } else if (drag.moved) {
        setDraggingNode(true);
        const position = {
          x: clamp(drag.originX + dx / view.zoom, 80, WORLD_WIDTH - 80),
          y: clamp(drag.originY + dy / view.zoom, 80, WORLD_HEIGHT - 70)
        };
        dragPositionRef.current = { id: drag.id, ...position };
        setDragPosition(dragPositionRef.current);
      }
    }

    function onUp(event: PointerEvent) {
      const drag = dragRef.current;
      if (!drag) {
        return;
      }
      dragRef.current = null;
      document.body.classList.remove("dw-canvas-dragging");
      if (drag.kind === "branch") {
        setGhost(null);
        const element = containerRef.current;
        if (element) {
          const rect = element.getBoundingClientRect();
          callbacks.current.onBranch(drag.id, {
            x: (event.clientX - rect.left - view.x) / view.zoom,
            y: (event.clientY - rect.top - view.y) / view.zoom
          });
        }
        return;
      }
      if (drag.kind === "node") {
        setDraggingNode(false);
        const finalPosition = dragPositionRef.current;
        dragPositionRef.current = null;
        setDragPosition(null);
        if (drag.moved && finalPosition && finalPosition.id === drag.id) {
          callbacks.current.onPositionChange(drag.id, {
            x: finalPosition.x,
            y: finalPosition.y
          });
        } else if (!drag.moved) {
          callbacks.current.onSelect(drag.id);
        }
        return;
      }
      if (drag.kind === "pan" && !drag.moved) {
        callbacks.current.onBackgroundClick();
      }
    }

    function onCancel() {
      if (!dragRef.current) {
        return;
      }
      dragRef.current = null;
      document.body.classList.remove("dw-canvas-dragging");
      setDraggingNode(false);
      dragPositionRef.current = null;
      setDragPosition(null);
      setGhost(null);
    }

    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onCancel);
    return () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onCancel);
    };
  }, [view.x, view.y, view.zoom]);

  function startPan(event: ReactPointerEvent<HTMLDivElement>) {
    if (event.button !== 0) {
      return;
    }
    // Pointer gestures that begin inside any stage overlay must never pan the
    // canvas or turn into a background click. The previous selector referenced
    // `.dw-stage-inspector`, which does not exist in the rendered DOM, so a
    // pointerdown in the inspector started a pan and its pointerup closed the
    // panel. See the regression test in tests/e2e/specs/v2-scroll.spec.ts.
    if (isStageUI(event.target)) {
      return;
    }
    if ((event.target as HTMLElement).closest("button, input, textarea, select, a")) {
      return;
    }
    dragRef.current = {
      kind: "pan",
      clientX: event.clientX,
      clientY: event.clientY,
      originX: view.x,
      originY: view.y,
      moved: false
    };
    document.body.classList.add("dw-canvas-dragging");
  }

  function startNode(event: ReactPointerEvent, node: PositionedNode) {
    if (event.button !== 0) {
      return;
    }
    if ((event.target as HTMLElement).closest("[data-branch-handle]")) {
      return;
    }
    event.stopPropagation();
    interacted.current = true;
    dragPositionRef.current = null;
    dragRef.current = {
      kind: "node",
      id: node.object.id,
      clientX: event.clientX,
      clientY: event.clientY,
      originX: node.x,
      originY: node.y,
      moved: false
    };
    document.body.classList.add("dw-canvas-dragging");
  }

  function startBranch(event: ReactPointerEvent, node: PositionedNode) {
    event.preventDefault();
    event.stopPropagation();
    branchPointerUsed.current = true;
    dragRef.current = { kind: "branch", id: node.object.id, moved: false };
    const element = containerRef.current;
    if (element) {
      const rect = element.getBoundingClientRect();
      setGhost({ x: event.clientX - rect.left, y: event.clientY - rect.top });
    }
    document.body.classList.add("dw-canvas-dragging");
  }

  function onBranchMove(event: ReactPointerEvent, node: PositionedNode) {
    const drag = dragRef.current;
    if (!drag || drag.kind !== "branch" || drag.id !== node.object.id) {
      return;
    }
    const element = containerRef.current;
    if (element) {
      const rect = element.getBoundingClientRect();
      setGhost({ x: event.clientX - rect.left, y: event.clientY - rect.top });
    }
  }

  function zoomBy(factor: number) {
    const element = containerRef.current;
    if (!element) {
      return;
    }
    const rect = element.getBoundingClientRect();
    interacted.current = true;
    setView((current) => {
      const next = clamp(current.zoom * factor, MIN_ZOOM, MAX_ZOOM);
      const cx = rect.width / 2;
      const cy = rect.height / 2;
      return {
        zoom: next,
        x: cx - ((cx - current.x) / current.zoom) * next,
        y: cy - ((cy - current.y) / current.zoom) * next
      };
    });
  }

  function resetView() {
    interacted.current = false;
    fit();
  }

  const hasObjects = (stage?.objects.length ?? 0) > 0;
  const selectedNode = selectedId
    ? graph.nodes.find((node) => node.object.id === selectedId)
    : null;
  const inspectorOnLeft = Boolean(selectedNode && selectedNode.x > WORLD_WIDTH / 2);

  return (
    <div
      ref={containerRef}
      className={`dw-stage${inspector ? " has-inspector" : ""}${draggingNode ? " is-dragging-node" : ""}`}
      onPointerDown={startPan}
      aria-label={zh ? "可以拖动、缩放与衍生的世界舞台" : "A world stage you can drag, zoom, and derive from"}
      data-testid="world-stage"
    >
      {/* A dedicated touch surface for panning. It sits below every overlay,
          so overlays keep native scrolling while the empty canvas still owns
          vertical/horizontal touch gestures. */}
      <div className="dw-stage-pan-layer" aria-hidden="true" />
      <div className="dw-stage-grid" aria-hidden="true" />
      <div className="dw-stage-orbital" aria-hidden="true" />

      <div className="dw-stage-top" data-dw-stage-ui>
        <div className="dw-stage-label">
          <span className="dw-stage-live-dot" aria-hidden="true" />
          <span>{zh ? "LIVE CANVAS" : "LIVE CANVAS"}</span>
          <span className="dw-stage-label-sep">/</span>
          <span>{zh ? "你正在创造的世界" : "the world you are creating"}</span>
        </div>
        <div className="dw-canvas-tools" role="group" aria-label={zh ? "世界视角" : "World view"}>
          <button type="button" data-testid="dw-stage-zoom-out" onClick={() => zoomBy(1 / 1.15)} disabled={view.zoom <= MIN_ZOOM + 0.01} aria-label={zh ? "缩小" : "Zoom out"}>
            −
          </button>
          <button type="button" data-testid="dw-stage-zoom-in" onClick={() => zoomBy(1.15)} disabled={view.zoom >= MAX_ZOOM - 0.01} aria-label={zh ? "放大" : "Zoom in"}>
            +
          </button>
          <button type="button" data-testid="dw-stage-zoom-reset" onClick={resetView} aria-label={zh ? "恢复视角" : "Reset view"}>
            <Icon name="reset" size={15} />
          </button>
        </div>
      </div>

      <div
        className="dw-stage-world"
        ref={worldRef}
        style={{
          width: WORLD_WIDTH,
          height: WORLD_HEIGHT,
          transform: `translate(${view.x}px, ${view.y}px) scale(${view.zoom})`
        }}
        onPointerMove={(event) => {
          const drag = dragRef.current;
          if (drag?.kind === "branch") {
            const node = graph.nodes.find((entry) => entry.object.id === drag.id);
            if (node) {
              onBranchMove(event, node);
            }
          }
        }}
        data-testid="dw-stage-viewport"
        data-zoom={view.zoom.toFixed(2)}
        data-pan-x={Math.round(view.x)}
        data-pan-y={Math.round(view.y)}
      >
        <svg className="dw-world-links" viewBox={`0 0 ${WORLD_WIDTH} ${WORLD_HEIGHT}`} aria-hidden="true" preserveAspectRatio="none">
          {graph.edges.map((edge) => {
            const midX = (edge.parent.x + edge.child.x) / 2;
            return (
              <path
                key={edge.id}
                d={`M ${edge.parent.x} ${edge.parent.y} C ${midX} ${edge.parent.y}, ${midX} ${edge.child.y}, ${edge.child.x} ${edge.child.y}`}
              />
            );
          })}
        </svg>

        <ul className="dw-world-nodes">
          {graph.nodes.map((node) => {
            const object = node.object;
            const selected = selectedId === object.id;
            const canDerive = object.permissions.includes("derive");
            return (
              <li
                key={object.id}
                className="dw-world-node-wrap"
                style={{
                  left: node.x - node.size / 2,
                  top: node.y - node.size / 2,
                  width: node.size,
                  height: node.size
                }}
              >
                <article
                  className={`dw-world-node tone-${node.tone}${node.depth === 0 ? " is-main" : ""}${selected ? " is-selected" : ""}`}
                  style={{ width: node.size, height: node.size }}
                  role="button"
                  tabIndex={0}
                  aria-label={zh ? `打开 ${object.title || "未命名世界"}` : `Open ${object.title || "untitled world"}`}
                  aria-pressed={selected}
                  data-testid="dw-stage-node"
                  data-stage-object={object.id}
                  onPointerDown={(event) => startNode(event, node)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" || event.key === " ") {
                      event.preventDefault();
                      onSelect(object.id);
                    }
                  }}
                >
                  <span className="dw-node-disc" aria-hidden="true" />
                  <span className="dw-node-content">
                    <span className="dw-node-sigil"><Icon name={nodeIcon(object)} size={19} /></span>
                    <strong className="dw-node-title">{object.title || (zh ? "未命名世界" : "Untitled world")}</strong>
                    {object.note ? <span className="dw-node-note">{object.note}</span> : null}
                    <span className="dw-node-footer">
                      {object.purpose === "story"
                        ? zh ? "自由创造" : "Story"
                        : object.purpose === "project"
                          ? zh ? "实用创造" : "Project"
                          : object.purpose === "open"
                            ? zh ? "自由世界" : "Free world"
                            : zh ? "共创世界" : "Shared world"}
                      {" · "}
                      {object.total_count > 0
                        ? zh
                          ? `${object.done_count}/${object.total_count} 个进展`
                          : `${object.done_count}/${object.total_count} steps`
                        : zh
                          ? "新的起点"
                          : "A new beginning"}
                    </span>
                  </span>
                  {canDerive ? (
                    <button
                      type="button"
                      className="dw-branch-handle"
                      data-branch-handle
                      aria-label={zh ? `从 ${object.title || "这个世界"} 衍生新分支` : `Derive a branch from ${object.title || "this world"}`}
                      title={zh ? "按住并拖出新分支；点击也可打开衍生面板" : "Drag out a branch, or click to open the derive panel"}
                      onPointerDown={(event) => startBranch(event, node)}
                      onClick={(event) => {
                        event.stopPropagation();
                        if (branchPointerUsed.current) {
                          branchPointerUsed.current = false;
                          return;
                        }
                        onBranch(object.id, { x: node.x + 120, y: node.y + 120 });
                      }}
                    >
                      <Icon name="branch" size={18} />
                    </button>
                  ) : null}
                  {selected ? (
                    <span className="dw-node-hint">{zh ? "拖动位置 · 点开改变" : "Drag · tap to change"}</span>
                  ) : null}
                </article>
              </li>
            );
          })}
        </ul>
      </div>

      {ghost ? (
        <div className="dw-stage-ghost" style={{ left: ghost.x, top: ghost.y }} aria-hidden="true">
          {zh ? "新的可能性" : "A new possibility"}
        </div>
      ) : null}

      {aiOrb}
      {inspector ? (
        <div
          className={`dw-stage-inspector-shell${inspectorOnLeft ? " is-left" : ""}`}
          data-dw-stage-ui
        >
          {inspector}
        </div>
      ) : null}
      {!hasObjects ? emptyState : null}

      <div className="dw-stage-bottom" data-dw-stage-ui>
        <div className="dw-stage-guide">
          <Icon name="move" size={14} />
          <span>
            {zh
              ? "点击探索 · 拖动布局 · 滚轮缩放 · 从节点旁拉出新分支"
              : "Tap to explore · drag to arrange · wheel to zoom · pull a branch from a node"}
          </span>
        </div>
        {pulse}
      </div>
      <span className="dw-stage-width" data-stage-width={Math.round(stageWidth)} aria-hidden="true" />
    </div>
  );
}
