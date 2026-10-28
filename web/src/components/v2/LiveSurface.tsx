// SPDX-License-Identifier: Apache-2.0
import { useEffect, useMemo, useRef, useState } from "react";
import type { OpenWorldNode, OpenWorldState, WorldViewNode } from "../../api";
import { useI18n } from "../../i18n";

const WORLD_WIDTH = 1600;
const WORLD_HEIGHT = 1000;
const CAMERA_WIDTH = 1000;
const CAMERA_HEIGHT = 700;

type LiveView = {
  kind_id: string;
  path: string;
  size?: number;
  fill_hue?: number;
  opacity?: number;
  paint?: string;
  label?: string;
  layer?: string;
  motion?: string;
  hide_done?: boolean;
  bindings?: {
    field: string;
    target: string;
    in_min: number;
    in_max: number;
    out_min: number;
    out_max: number;
  }[];
};

type EntityAction = { entity_id: string; behavior_id: string; label?: string };
type LiveTrack = { entity_id: string; property: string; keys: { t: number; v: number }[] };
type LiveGoal = {
  type: string;
  label?: string;
  kind_id?: string;
  count?: number;
  entity_id?: string;
  field?: string;
  variable?: string;
  target?: number;
};
type ParticleSpec = { kind: string; count?: number; hue?: number; size?: number; speed?: number };

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function asArray(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function numberValue(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function stringValue(value: unknown, fallback = ""): string {
  return typeof value === "string" ? value : fallback;
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function interpolate(track: LiveTrack, time: number): number | null {
  const keys = track.keys ?? [];
  if (keys.length === 0) {
    return null;
  }
  const first = keys[0]!;
  if (time <= first.t) {
    return first.v;
  }
  for (let index = 1; index < keys.length; index += 1) {
    const previous = keys[index - 1]!;
    const next = keys[index]!;
    if (time <= next.t) {
      const span = next.t - previous.t;
      const ratio = span <= 0 ? 0 : (time - previous.t) / span;
      return previous.v + ratio * (next.v - previous.v);
    }
  }
  return keys[keys.length - 1]!.v;
}

function seeded(index: number, salt: number): number {
  const value = Math.sin(index * 12.9898 + salt * 78.233) * 43758.5453;
  return value - Math.floor(value);
}

function visualFor(node: OpenWorldNode, view: LiveView, time: number,
  tracks: Map<string, LiveTrack[]>) {
  let size = view.size ?? 56;
  let opacity = view.opacity ?? 0.9;
  let hue = view.fill_hue ?? 198;
  let x = node.x;
  let y = node.y;
  let rotate = 0;
  let scale = 1;
  for (const binding of view.bindings ?? []) {
    const raw = node.values?.[binding.field];
    const numeric = typeof raw === "boolean" ? (raw ? 1 : 0) : raw;
    if (typeof numeric !== "number" || !Number.isFinite(numeric)) {
      continue;
    }
    const span = binding.in_max - binding.in_min;
    if (span === 0) {
      continue;
    }
    const ratio = clamp((numeric - binding.in_min) / span, 0, 1);
    const value = binding.out_min + ratio * (binding.out_max - binding.out_min);
    switch (binding.target) {
      case "size":
        scale = value;
        break;
      case "opacity":
        opacity = value;
        break;
      case "hue":
        hue += value;
        break;
    }
  }
  for (const track of tracks.get(node.id) ?? []) {
    const value = interpolate(track, time);
    if (value === null) {
      continue;
    }
    switch (track.property) {
      case "x":
        x = value;
        break;
      case "y":
        y = value;
        break;
      case "scale":
        scale = value;
        break;
      case "opacity":
        opacity = value;
        break;
      case "rotate":
        rotate = value;
        break;
      case "hue":
        hue = value;
        break;
    }
  }
  return { x, y, size, opacity: clamp(opacity, 0, 1), hue, rotate, scale };
}

function goalProgress(goal: LiveGoal | null, world: OpenWorldState): {
  current: number;
  target: number;
  done: boolean;
} {
  if (!goal) {
    return { current: 0, target: 0, done: false };
  }
  switch (goal.type) {
    case "collect": {
      const target = Math.max(1, goal.count ?? 1);
      const current = world.nodes.filter(
        (node) => node.kind_id === goal.kind_id && node.status === "done"
      ).length;
      return { current, target, done: current >= target };
    }
    case "score": {
      const target = goal.target ?? 1;
      let current = 0;
      if (goal.variable) {
        const raw = world.variables?.[goal.variable];
        current = typeof raw === "number" ? raw : 0;
      } else if (goal.entity_id) {
        const node = world.nodes.find((candidate) => candidate.id === goal.entity_id);
        const raw = node?.values?.[goal.field ?? ""];
        current = typeof raw === "number" ? raw : 0;
      }
      return { current, target, done: current >= target };
    }
    default:
      return { current: 0, target: 0, done: false };
  }
}

export default function LiveSurface({
  node,
  world,
  canContribute,
  busy,
  onInvoke
}: {
  node: WorldViewNode;
  world: OpenWorldState;
  canContribute: boolean;
  busy: boolean;
  onInvoke: (behaviorID: string, entityID?: string, value?: unknown) => void;
}) {
  const { lang } = useI18n();
  const zh = lang === "zh";
  const props = node.props ?? {};
  const background = asRecord(props.background);
  const views = useMemo(
    () => asArray(props.views).map((raw) => asRecord(raw) as unknown as LiveView),
    [props.views]
  );
  const viewByKind = useMemo(() => {
    const map = new Map<string, LiveView>();
    for (const view of views) {
      map.set(view.kind_id, view);
    }
    return map;
  }, [views]);
  const entityActions = useMemo(() => {
    const map = new Map<string, EntityAction>();
    for (const raw of asArray(props.entity_actions)) {
      const action = asRecord(raw) as unknown as EntityAction;
      if (action.entity_id && action.behavior_id) {
        map.set(action.entity_id, action);
      }
    }
    return map;
  }, [props.entity_actions]);
  const avatar = props.avatar ? (asRecord(props.avatar) as unknown as {
    path: string; size?: number; hue?: number; opacity?: number;
  }) : null;
  const spawn = asRecord(props.spawn);
  const speed = numberValue(props.speed, 1.8);
  const interactRadius = numberValue(props.interact_radius, 96);
  const solidKinds = useMemo(
    () => new Set(asArray(props.solid_kinds).filter((item): item is string => typeof item === "string")),
    [props.solid_kinds]
  );
  const timeline = props.timeline ? (asRecord(props.timeline) as unknown as {
    duration: number; loop?: boolean; autoplay?: boolean; tracks?: LiveTrack[];
  }) : null;
  const duration = Math.max(1, numberValue(timeline?.duration, 12));
  const trackByEntity = useMemo(() => {
    const map = new Map<string, LiveTrack[]>();
    for (const raw of timeline?.tracks ?? []) {
      const track = raw as LiveTrack;
      if (!map.has(track.entity_id)) {
        map.set(track.entity_id, []);
      }
      map.get(track.entity_id)!.push(track);
    }
    return map;
  }, [timeline]);
  const goal = props.goal ? (asRecord(props.goal) as unknown as LiveGoal) : null;
  const particles = props.particles ? (asRecord(props.particles) as unknown as ParticleSpec) : null;
  const timerSeconds = numberValue(props.timer, 0);
  const title = stringValue(props.title, "");
  const hint = stringValue(props.hint, "");
  const showLinks = props.show_links !== false;
  const linkStyle = stringValue(props.link_style, "curve");
  const playable = Boolean(avatar);
  const initialX = numberValue(spawn.x, world.nodes[0]?.x ?? 800);
  const initialY = numberValue(spawn.y, world.nodes[0]?.y ?? 500);
  const [position, setPosition] = useState({ x: initialX, y: initialY });
  const [time, setTime] = useState(0);
  const [playing, setPlaying] = useState(timeline?.autoplay !== false);
  const [nearID, setNearID] = useState("");
  const [won, setWon] = useState(false);
  const [lost, setLost] = useState(false);
  const [timerLeft, setTimerLeft] = useState(timerSeconds);
  const held = useRef(new Set<string>());
  const positionRef = useRef(position);
  const containerRef = useRef<HTMLDivElement>(null);
  const frameRef = useRef<number | null>(null);
  const lastFrame = useRef<number | null>(null);

  positionRef.current = position;

  useEffect(() => {
    containerRef.current?.focus();
  }, []);

  const progress = goalProgress(goal, world);
  const reachDone = Boolean(
    goal?.type === "reach" && goal.entity_id &&
    (() => {
      const target = world.nodes.find((candidate) => candidate.id === goal.entity_id);
      return target
        ? Math.hypot(position.x - target.x, position.y - target.y) <= interactRadius
        : false;
    })()
  );

  useEffect(() => {
    if (goal?.type === "collect" || goal?.type === "score") {
      if (progress.done) {
        setWon(true);
      }
    }
    if (goal?.type === "reach" && reachDone) {
      setWon(true);
    }
  }, [goal?.type, progress.done, reachDone]);

  useEffect(() => {
    const step = (timestamp: number) => {
      if (lastFrame.current === null) {
        lastFrame.current = timestamp;
      }
      const delta = Math.min(0.1, (timestamp - lastFrame.current) / 1000);
      lastFrame.current = timestamp;
      if (timeline && playing && !won && !lost) {
        setTime((current) => {
          const next = current + delta;
          return timeline.loop !== false ? next % duration : Math.min(duration, next);
        });
      }
      if (playable) {
        const current = positionRef.current;
        let dx = 0;
        let dy = 0;
        if (held.current.has("ArrowLeft") || held.current.has("a")) {
          dx -= 1;
        }
        if (held.current.has("ArrowRight") || held.current.has("d")) {
          dx += 1;
        }
        if (held.current.has("ArrowUp") || held.current.has("w")) {
          dy -= 1;
        }
        if (held.current.has("ArrowDown") || held.current.has("s")) {
          dy += 1;
        }
        if (dx !== 0 || dy !== 0) {
          const length = Math.hypot(dx, dy) || 1;
          const stepX = (dx / length) * speed * 120 * delta;
          const stepY = (dy / length) * speed * 120 * delta;
          const nextX = clamp(current.x + stepX, 40, WORLD_WIDTH - 40);
          const nextY = clamp(current.y + stepY, 40, WORLD_HEIGHT - 60);
          const radius = avatar ? (avatar.size ?? 34) * 0.45 : 16;
          const blocked = world.nodes.some((candidate) => {
            if (!solidKinds.has(candidate.kind_id)) {
              return false;
            }
            const view = viewByKind.get(candidate.kind_id);
            const obstacleRadius = (view?.size ?? 56) * 0.56;
            return Math.hypot(nextX - candidate.x, nextY - candidate.y) <
              radius + obstacleRadius;
          });
          if (!blocked) {
            setPosition({ x: nextX, y: nextY });
          }
        }
        let nearest = "";
        let nearestDistance = Number.POSITIVE_INFINITY;
        for (const candidate of world.nodes) {
          if (!entityActions.has(candidate.id)) {
            continue;
          }
          const distance = Math.hypot(current.x - candidate.x, current.y - candidate.y);
          if (distance < nearestDistance) {
            nearestDistance = distance;
            nearest = candidate.id;
          }
        }
        setNearID(nearestDistance <= interactRadius ? nearest : "");
      }
      if (timerSeconds > 0 && !won && !lost) {
        setTimerLeft((current) => {
          const next = Math.max(0, current - delta);
          if (next <= 0) {
            if (goal?.type === "survive") {
              setWon(true);
            } else {
              setLost(true);
            }
          }
          return next;
        });
      }
      frameRef.current = requestAnimationFrame(step);
    };
    frameRef.current = requestAnimationFrame(step);
    return () => {
      if (frameRef.current !== null) {
        cancelAnimationFrame(frameRef.current);
      }
      frameRef.current = null;
      lastFrame.current = null;
    };
  }, [playable, avatar, speed, solidKinds, viewByKind, world.nodes, entityActions,
    interactRadius, timeline, playing, duration, won, lost, timerSeconds, goal?.type]);

  function interact(entityID: string, behaviorID: string) {
    if (!canContribute || busy || won || lost) {
      return;
    }
    if (playable) {
      const target = world.nodes.find((candidate) => candidate.id === entityID);
      if (!target) {
        return;
      }
      if (Math.hypot(position.x - target.x, position.y - target.y) > interactRadius) {
        return;
      }
    }
    onInvoke(behaviorID, entityID);
  }

  function onKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    const key = event.key.length === 1 ? event.key.toLowerCase() : event.key;
    if (["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", " ", "w", "a", "s", "d"].includes(key)) {
      event.preventDefault();
    }
    if (key === "e" || key === " " || key === "Enter") {
      const action = nearID ? entityActions.get(nearID) : undefined;
      if (action) {
        interact(nearID, action.behavior_id);
      }
      return;
    }
    held.current.add(key);
  }

  function onKeyUp(event: React.KeyboardEvent<HTMLDivElement>) {
    const key = event.key.length === 1 ? event.key.toLowerCase() : event.key;
    held.current.delete(key);
  }

  function touchHold(key: string, pressed: boolean) {
    if (pressed) {
      held.current.add(key);
    } else {
      held.current.delete(key);
    }
  }

  function restart() {
    setPosition({ x: initialX, y: initialY });
    setWon(false);
    setLost(false);
    setTimerLeft(timerSeconds);
  }

  const rendered = useMemo(() => {
    return world.nodes
      .filter((candidate) => viewByKind.has(candidate.kind_id))
      .filter((candidate) => {
        const view = viewByKind.get(candidate.kind_id);
        return !(view?.hide_done && candidate.status === "done");
      });
  }, [world.nodes, viewByKind]);

  const camera = useMemo(() => {
    if (!playable) {
      return { x: 0, y: 0, width: WORLD_WIDTH, height: WORLD_HEIGHT };
    }
    const x = clamp(position.x - CAMERA_WIDTH / 2, 0, WORLD_WIDTH - CAMERA_WIDTH);
    const y = clamp(position.y - CAMERA_HEIGHT / 2, 0, WORLD_HEIGHT - CAMERA_HEIGHT);
    return { x, y, width: CAMERA_WIDTH, height: CAMERA_HEIGHT };
  }, [playable, position.x, position.y]);

  const skyHue = numberValue(background.hue, 222);
  const groundHue = numberValue(background.hue2, 205);
  const lightness = numberValue(background.lightness, 10);
  const accentHue = numberValue(background.accent_hue, 42);
  const linkColor = `hsl(${accentHue} 70% 62%)`;
  const showAnimationControls = !playable && Boolean(timeline);

  return (
    <div
      ref={containerRef}
      className="dw-live-surface"
      data-testid="world-live"
      tabIndex={0}
      role="application"
      aria-label={title || world.title}
      onKeyDown={onKeyDown}
      onKeyUp={onKeyUp}
      onPointerDown={() => containerRef.current?.focus()}
      style={{ outline: "none" }}
    >
      <div className="dw-live-head">
        <div>
          <span className="dw-live-kicker">
            {playable
              ? goal
                ? (zh ? "可游玩的世界" : "A playable world")
                : (zh ? "可行走的世界" : "A walkable world")
              : (zh ? "正在发生的世界" : "A world in motion")}
          </span>
          <h2>{title || world.title}</h2>
        </div>
        {goal ? (
          <div className="dw-live-goal" data-testid="world-live-goal">
            <strong>{goal.label || (zh ? "目标" : "Goal")}</strong>
            <span>
              {goal.type === "collect" || goal.type === "score"
                ? `${Math.round(progress.current)} / ${Math.round(progress.target)}`
                : goal.type === "reach"
                  ? (zh ? "靠近目标" : "Reach the target")
                  : (zh ? "坚持计时" : "Survive the timer")}
            </span>
            {goal.type === "collect" || goal.type === "score" ? (
              <span className="dw-live-goal-bar">
                <span style={{ width: `${Math.min(100, (progress.current / Math.max(1, progress.target)) * 100)}%` }} />
              </span>
            ) : null}
          </div>
        ) : null}
        {timerSeconds > 0 ? (
          <div className={`dw-live-timer${timerLeft < 10 ? " is-urgent" : ""}`}>
            {Math.ceil(timerLeft)}s
          </div>
        ) : null}
      </div>
      <div className="dw-live-stage">
        <svg
          viewBox={`${camera.x} ${camera.y} ${camera.width} ${camera.height}`}
          preserveAspectRatio="xMidYMid meet"
          style={{ width: "100%", height: "auto", display: "block" }}
        >
          <defs>
            <linearGradient id={`live-sky-${node.id}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={`hsl(${skyHue} 52% ${Math.max(4, lightness)}%)`} />
              <stop offset="100%" stopColor={`hsl(${groundHue} 58% ${Math.max(2, lightness - 4)}%)`} />
            </linearGradient>
            <clipPath id={`live-clip-${node.id}`}>
              <rect x={0} y={0} width={WORLD_WIDTH} height={WORLD_HEIGHT} />
            </clipPath>
          </defs>
          <g clipPath={`url(#live-clip-${node.id})`}>
            <rect x={0} y={0} width={WORLD_WIDTH} height={WORLD_HEIGHT}
              fill={`url(#live-sky-${node.id})`} />
            {particles && particles.kind !== "none" ? (
              <g aria-hidden="true">
                {Array.from({ length: Math.min(120, Math.max(0, particles.count ?? 40)) }).map((_, index) => {
                  const hue = particles.hue ?? 200;
                  const size = particles.size ?? 2;
                  const particleSpeed = particles.speed ?? 1;
                  if (particles.kind === "rain") {
                    const x = seeded(index, 1) * WORLD_WIDTH;
                    const y = (seeded(index, 2) * WORLD_HEIGHT + time * 180 * particleSpeed) % WORLD_HEIGHT;
                    return (
                      <line key={index} x1={x} y1={y} x2={x - 3} y2={y + 14}
                        stroke={`hsl(${hue} 70% 70%)`} strokeOpacity={0.4}
                        strokeWidth={Math.max(1, size * 0.7)} />
                    );
                  }
                  const x = (seeded(index, 3) * WORLD_WIDTH + time * 9 * particleSpeed) % WORLD_WIDTH;
                  const y = (seeded(index, 4) * WORLD_HEIGHT + time * 6 * particleSpeed) % WORLD_HEIGHT;
                  return (
                    <circle key={index} cx={x} cy={y} r={Math.max(1, size * 0.8)}
                      fill={`hsl(${hue} 70% 72%)`} opacity={0.25 + seeded(index, 5) * 0.5} />
                  );
                })}
              </g>
            ) : null}
            {showLinks ? (
              <g aria-hidden="true">
                {world.links.map((link) => {
                  const from = world.nodes.find((candidate) => candidate.id === link.from);
                  const to = world.nodes.find((candidate) => candidate.id === link.to);
                  if (!from || !to || !viewByKind.has(from.kind_id) || !viewByKind.has(to.kind_id)) {
                    return null;
                  }
                  const middleX = (from.x + to.x) / 2;
                  return (
                    <path
                      key={link.id}
                      d={linkStyle === "line"
                        ? `M ${from.x} ${from.y} L ${to.x} ${to.y}`
                        : `M ${from.x} ${from.y} C ${middleX} ${from.y}, ${middleX} ${to.y}, ${to.x} ${to.y}`}
                      fill="none" stroke={linkColor} strokeOpacity={0.2} strokeWidth={1.5}
                    />
                  );
                })}
              </g>
            ) : null}
            {rendered.map((entity) => {
              const view = viewByKind.get(entity.kind_id)!;
              const visual = visualFor(entity, view, time, trackByEntity);
              const color = `hsl(${visual.hue} 70% 62%)`;
              const action = entityActions.get(entity.id);
              const actionable = Boolean(action) && canContribute && !busy && !won && !lost;
              const isNear = nearID === entity.id;
              const strokeWidth = view.paint === "stroke" ? 4 : 1.5;
              return (
                <g
                  key={entity.id}
                  data-testid="world-live-entity"
                  data-entity-id={entity.id}
                  data-kind-id={entity.kind_id}
                  data-x={entity.x.toFixed(2)}
                  data-y={entity.y.toFixed(2)}
                  data-near={isNear ? "true" : "false"}
                  className={view.motion && view.motion !== "none" ? `scene-motion-${view.motion}` : undefined}
                  style={{ animationDuration: "6s" }}
                  opacity={visual.opacity}
                  role={actionable ? "button" : "img"}
                  tabIndex={actionable ? 0 : undefined}
                  aria-label={entity.name}
                  onClick={() => {
                    if (action && actionable) {
                      interact(entity.id, action.behavior_id);
                    }
                  }}
                >
                  <circle cx={visual.x} cy={visual.y} r={visual.size * 0.62}
                    fill={`hsl(${visual.hue} 70% 62% / 0.1)`} />
                  <g transform={`translate(${visual.x} ${visual.y}) rotate(${visual.rotate}) scale(${(visual.size / 100) * visual.scale})`}>
                    <path d={view.path} fill={view.paint === "stroke" ? "none" : color}
                      stroke={color} strokeWidth={strokeWidth} strokeLinejoin="round"
                      strokeLinecap="round" />
                  </g>
                  {view.label !== "hidden" ? (
                    <text x={visual.x} y={visual.y + visual.size * 0.62 + 16}
                      textAnchor="middle" fill="rgba(244,241,232,0.82)"
                      fontSize={Math.max(11, Math.min(18, visual.size * 0.24))}>
                      {entity.name}
                    </text>
                  ) : null}
                  {isNear && action ? (
                    <text x={visual.x} y={visual.y - visual.size * 0.62 - 10}
                      textAnchor="middle" fill={color} fontSize={15}>
                      {action.label || (zh ? "互动" : "Interact")}
                    </text>
                  ) : null}
                </g>
              );
            })}
            {avatar ? (
              <g data-testid="world-live-avatar"
                data-x={position.x.toFixed(2)} data-y={position.y.toFixed(2)}>
                <circle cx={position.x} cy={position.y} r={(avatar.size ?? 34) * 0.78}
                  fill={`hsl(${avatar.hue ?? 198} 80% 65% / 0.18)`} />
                <g transform={`translate(${position.x} ${position.y}) scale(${(avatar.size ?? 34) / 100})`}>
                  <path d={avatar.path} fill={`hsl(${avatar.hue ?? 198} 78% 68%)`}
                    opacity={avatar.opacity ?? 1} strokeLinejoin="round" />
                </g>
              </g>
            ) : null}
          </g>
        </svg>
        {showAnimationControls ? (
          <div className="dw-live-controls" data-testid="world-live-controls">
            <button type="button" className="dw-button dw-button-small dw-button-ghost"
              onClick={() => setPlaying((current) => !current)}>
              {playing ? (zh ? "暂停" : "Pause") : (zh ? "播放" : "Play")}
            </button>
            <input type="range" min={0} max={duration} step={0.05} value={time}
              aria-label={zh ? "时间" : "Time"}
              onChange={(event) => {
                setPlaying(false);
                setTime(Number(event.target.value));
              }} />
            <small>{time.toFixed(1)}s / {duration.toFixed(0)}s</small>
          </div>
        ) : null}
        {playable && !won && !lost ? (
          <div className="dw-live-touch" aria-hidden="true">
            <button type="button" onPointerDown={() => touchHold("ArrowUp", true)}
              onPointerUp={() => touchHold("ArrowUp", false)}
              onPointerLeave={() => touchHold("ArrowUp", false)}>↑</button>
            <div>
              <button type="button" onPointerDown={() => touchHold("ArrowLeft", true)}
                onPointerUp={() => touchHold("ArrowLeft", false)}
                onPointerLeave={() => touchHold("ArrowLeft", false)}>←</button>
              <button type="button" onPointerDown={() => touchHold("ArrowDown", true)}
                onPointerUp={() => touchHold("ArrowDown", false)}
                onPointerLeave={() => touchHold("ArrowDown", false)}>↓</button>
              <button type="button" onPointerDown={() => touchHold("ArrowRight", true)}
                onPointerUp={() => touchHold("ArrowRight", false)}
                onPointerLeave={() => touchHold("ArrowRight", false)}>→</button>
            </div>
          </div>
        ) : null}
        {won ? (
          <div className="dw-live-result is-win" data-testid="world-live-win">
            <strong>{zh ? "你做到了。" : "You did it."}</strong>
            <p>{goal?.label || (zh ? "这个世界记住了你的选择。" : "The world remembers your choice.")}</p>
            <button type="button" className="dw-button dw-button-primary dw-button-small"
              onClick={restart}>
              {zh ? "再来一次" : "Again"}
            </button>
          </div>
        ) : null}
        {lost ? (
          <div className="dw-live-result is-lose" data-testid="world-live-lose">
            <strong>{zh ? "时间到了。" : "Time is up."}</strong>
            <p>{zh ? "你已经走过的路仍会留在世界里。" : "The path you walked is still part of the world."}</p>
            <button type="button" className="dw-button dw-button-primary dw-button-small"
              onClick={restart}>
              {zh ? "再试一次" : "Try again"}
            </button>
          </div>
        ) : null}
      </div>
      <p className="dw-live-hint" data-testid="world-live-hint">
        {hint ||
          (playable
            ? zh
              ? "用方向键或 WASD 走动；靠近后点击或按 E 互动。"
              : "Move with the arrow keys or WASD; approach and click or press E to interact."
            : zh
              ? "这是一幅会自己变化的画面；可以播放、暂停和拖动时间。"
              : "This is a world in motion; play, pause and scrub time.")}
      </p>
    </div>
  );
}
