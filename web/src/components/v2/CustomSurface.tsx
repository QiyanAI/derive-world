// SPDX-License-Identifier: Apache-2.0
import { useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, PointerEvent as ReactPointerEvent } from "react";
import type { OpenWorldState } from "../../api";
import { useI18n } from "../../i18n";

type CustomAction = {
  type: string;
  name?: string;
  value?: unknown;
  entity_id?: string;
  field?: string;
  cond?: CustomCondition;
  then?: CustomAction[];
  else?: CustomAction[];
  freq?: number;
  duration?: number;
  gain?: number;
  wave?: string;
};

type CustomCondition = {
  var?: string;
  entity_id?: string;
  field?: string;
  op?: string;
  value?: unknown;
  all?: CustomCondition[];
  any?: CustomCondition[];
  not?: CustomCondition;
};

type CustomBinding = {
  prop: string;
  var?: string;
  entity_id?: string;
  field?: string;
  in_min: number;
  in_max: number;
  out_min: number;
  out_max: number;
};

type CustomNode = {
  id: string;
  type: string;
  x: number;
  y: number;
  w?: number;
  h?: number;
  r?: number;
  rotate?: number;
  scale?: number;
  opacity?: number;
  hue?: number;
  stroke_hue?: number;
  stroke_width?: number;
  corner?: number;
  path?: string;
  text?: string;
  size?: number;
  align?: string;
  visible_when?: CustomCondition;
  bindings?: CustomBinding[];
  bind_text?: { var?: string; entity_id?: string; field?: string };
  on_click?: CustomAction[];
  input?: { var: string; type?: string; max_length?: number };
  drag?: { var: string; min: number; max: number; axis?: string };
  action_value?: string;
};

type CustomTrack = {
  node_id: string;
  property: string;
  keys: { t: number; v: number }[];
};

type CustomTimeline = {
  duration: number;
  loop?: boolean;
  autoplay?: boolean;
  tracks: CustomTrack[];
};

type CustomProgram = {
  title?: string;
  hint?: string;
  width?: number;
  height?: number;
  background?: Record<string, unknown>;
  nodes?: CustomNode[];
  keys?: { key: string; actions: CustomAction[] }[];
  timeline?: CustomTimeline;
};

type Values = Record<string, unknown>;

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

function variableValues(world: OpenWorldState, local: Values): Values {
  return { ...(world.variables ?? {}), ...local };
}

function fieldValue(world: OpenWorldState, entityID: string, field: string): unknown {
  const node = world.nodes.find((candidate) => candidate.id === entityID);
  if (!node) {
    return undefined;
  }
  if (field === "status") {
    return node.status;
  }
  return node.values?.[field];
}

function sourceValue(
  values: Values,
  world: OpenWorldState,
  variable?: string,
  entityID?: string,
  field?: string
): unknown {
  if (variable) {
    return values[variable];
  }
  if (entityID) {
    return fieldValue(world, entityID, field ?? "");
  }
  return undefined;
}

function truthy(value: unknown): boolean {
  return value === true || (typeof value === "number" && value !== 0) ||
    (typeof value === "string" && value.length > 0);
}

function equalScalar(left: unknown, right: unknown): boolean {
  if (left === null || left === undefined) {
    return right === null || right === undefined;
  }
  return typeof left === typeof right && left === right;
}

function conditionHolds(condition: CustomCondition | undefined, values: Values,
  world: OpenWorldState): boolean {
  if (!condition) {
    return true;
  }
  if (condition.all && condition.all.length > 0) {
    return condition.all.every((item) => conditionHolds(item, values, world));
  }
  if (condition.any && condition.any.length > 0) {
    return condition.any.some((item) => conditionHolds(item, values, world));
  }
  if (condition.not) {
    return !conditionHolds(condition.not, values, world);
  }
  const actual = sourceValue(values, world, condition.var, condition.entity_id, condition.field);
  switch (condition.op) {
    case "truthy":
      return truthy(actual);
    case "falsy":
      return !truthy(actual);
    case "eq":
      return equalScalar(actual, condition.value);
    case "ne":
      return !equalScalar(actual, condition.value);
    case "gt":
      return typeof actual === "number" && typeof condition.value === "number" &&
        actual > condition.value;
    case "gte":
      return typeof actual === "number" && typeof condition.value === "number" &&
        actual >= condition.value;
    case "lt":
      return typeof actual === "number" && typeof condition.value === "number" &&
        actual < condition.value;
    case "lte":
      return typeof actual === "number" && typeof condition.value === "number" &&
        actual <= condition.value;
    default:
      return false;
  }
}

function bindingValue(binding: CustomBinding, values: Values, world: OpenWorldState,
  fallback: number): number {
  const raw = sourceValue(values, world, binding.var, binding.entity_id, binding.field);
  const numeric = typeof raw === "boolean" ? (raw ? 1 : 0) : raw;
  if (typeof numeric !== "number" || !Number.isFinite(numeric)) {
    return fallback;
  }
  const span = binding.in_max - binding.in_min;
  if (span === 0) {
    return fallback;
  }
  const ratio = Math.max(0, Math.min(1, (numeric - binding.in_min) / span));
  return binding.out_min + ratio * (binding.out_max - binding.out_min);
}

function timelineValue(track: CustomTrack, time: number): number | null {
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

function nodeVisual(node: CustomNode, time: number, values: Values, world: OpenWorldState,
  tracks: Map<string, CustomTrack[]>) {
  const visual = {
    x: node.x,
    y: node.y,
    w: node.w ?? 120,
    h: node.h ?? 80,
    r: node.r ?? 36,
    rotate: node.rotate ?? 0,
    scale: node.scale ?? 1,
    opacity: node.opacity ?? 1,
    hue: node.hue ?? 198,
    strokeHue: node.stroke_hue ?? node.hue ?? 198
  };
  const numeric: Record<string, number> = {
    x: visual.x, y: visual.y, w: visual.w, h: visual.h, r: visual.r,
    rotate: visual.rotate, scale: visual.scale, opacity: visual.opacity, hue: visual.hue,
    stroke_hue: visual.strokeHue
  };
  for (const binding of node.bindings ?? []) {
    const key = binding.prop as keyof typeof numeric;
    if (key in numeric) {
      numeric[key] = bindingValue(binding, values, world, numeric[key] ?? 0);
    }
  }
  for (const track of tracks.get(node.id) ?? []) {
    const value = timelineValue(track, time);
    const key = track.property as keyof typeof numeric;
    if (value !== null && key in numeric) {
      numeric[key] = value;
    }
  }
  visual.x = numeric.x ?? visual.x;
  visual.y = numeric.y ?? visual.y;
  visual.w = numeric.w ?? visual.w;
  visual.h = numeric.h ?? visual.h;
  visual.r = numeric.r ?? visual.r;
  visual.rotate = numeric.rotate ?? visual.rotate;
  visual.scale = numeric.scale ?? visual.scale;
  visual.opacity = Math.max(0.02, Math.min(1, numeric.opacity ?? visual.opacity));
  visual.hue = numeric.hue ?? visual.hue;
  visual.strokeHue = numeric.stroke_hue ?? visual.strokeHue;
  return visual;
}

function collectSounds(actions: CustomAction[], values: Values, world: OpenWorldState): CustomAction[] {
  const sounds: CustomAction[] = [];
  for (const action of actions) {
    if (action.type === "sound") {
      sounds.push(action);
      continue;
    }
    if (action.type === "if" && conditionHolds(action.cond, values, world)) {
      sounds.push(...collectSounds(action.then ?? [], values, world));
    }
  }
  return sounds;
}

let audioContext: AudioContext | null = null;

function playTone(action: CustomAction) {
  try {
    const Context = window.AudioContext ??
      (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!Context) {
      return;
    }
    if (!audioContext) {
      audioContext = new Context();
    }
    const frequency = Math.max(20, Math.min(12000, action.freq ?? 440));
    const duration = Math.max(0.03, Math.min(2, action.duration ?? 0.25));
    const gain = Math.max(0, Math.min(0.5, action.gain ?? 0.08));
    const oscillator = audioContext.createOscillator();
    const volume = audioContext.createGain();
    oscillator.type = (action.wave as OscillatorType) || "sine";
    oscillator.frequency.value = frequency;
    volume.gain.value = gain;
    volume.gain.exponentialRampToValueAtTime(0.0001, audioContext.currentTime + duration);
    oscillator.connect(volume);
    volume.connect(audioContext.destination);
    oscillator.start();
    oscillator.stop(audioContext.currentTime + duration);
  } catch {
    // Audio is an enhancement; the surface remains fully usable without it.
  }
}

export default function CustomSurface({
  surfaceID,
  props,
  world,
  canContribute,
  busy,
  onCustom
}: {
  surfaceID: string;
  props: Record<string, unknown>;
  world: OpenWorldState;
  canContribute: boolean;
  busy: boolean;
  onCustom: (surfaceID: string, event: "click" | "key" | "input" | "drag",
    payload: { nodeID?: string; key?: string; value?: unknown }) => void;
}) {
  const { lang } = useI18n();
  const zh = lang === "zh";
  const program = useMemo(() => props as CustomProgram, [props]);
  const width = numberValue(program.width, 1600);
  const height = numberValue(program.height, 1000);
  const nodes = useMemo(
    () => asArray(program.nodes).map((raw) => asRecord(raw) as unknown as CustomNode).filter((node) => node.id),
    [program.nodes]
  );
  const timeline = program.timeline;
  const duration = Math.max(1, numberValue(timeline?.duration, 12));
  const trackByNode = useMemo(() => {
    const map = new Map<string, CustomTrack[]>();
    for (const raw of timeline?.tracks ?? []) {
      const track = raw as CustomTrack;
      if (!map.has(track.node_id)) {
        map.set(track.node_id, []);
      }
      map.get(track.node_id)!.push(track);
    }
    return map;
  }, [timeline]);
  const [time, setTime] = useState(0);
  const [playing, setPlaying] = useState(timeline?.autoplay !== false);
  const [local, setLocal] = useState<Values>({});
  const [drag, setDrag] = useState<{ nodeID: string; value: number } | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const animationRef = useRef<number | null>(null);
  const lastTick = useRef<number | null>(null);

  const values = variableValues(world, local);
  const background = asRecord(program.background);
  const mode = stringValue(background.mode, "gradient");
  const skyHue = numberValue(background.hue, 222);
  const groundHue = numberValue(background.hue2, 205);
  const lightness = numberValue(background.lightness, 10);

  useEffect(() => {
    if (!timeline || !playing) {
      lastTick.current = null;
      if (animationRef.current !== null) {
        cancelAnimationFrame(animationRef.current);
        animationRef.current = null;
      }
      return;
    }
    const step = (timestamp: number) => {
      if (lastTick.current === null) {
        lastTick.current = timestamp;
      }
      const delta = (timestamp - lastTick.current) / 1000;
      lastTick.current = timestamp;
      setTime((current) => {
        const next = current + delta;
        if (timeline.loop !== false) {
          return next % duration;
        }
        return Math.min(duration, next);
      });
      animationRef.current = requestAnimationFrame(step);
    };
    animationRef.current = requestAnimationFrame(step);
    return () => {
      if (animationRef.current !== null) {
        cancelAnimationFrame(animationRef.current);
      }
      animationRef.current = null;
      lastTick.current = null;
    };
  }, [timeline, playing, duration]);

  function invokeNode(node: CustomNode, event: "click") {
    if (!canContribute || busy) {
      return;
    }
    const sounds = collectSounds(node.on_click ?? [], values, world);
    for (const sound of sounds) {
      playTone(sound);
    }
    onCustom(surfaceID, event, {
      nodeID: node.id,
      value: node.action_value ?? undefined
    });
  }

  function onKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    const binding = (program.keys ?? []).find((item) => item.key === event.key);
    if (!binding || !canContribute || busy) {
      return;
    }
    event.preventDefault();
    for (const sound of collectSounds(binding.actions, values, world)) {
      playTone(sound);
    }
    onCustom(surfaceID, "key", { key: event.key });
  }

  function startDrag(event: ReactPointerEvent<SVGGElement>, node: CustomNode) {
    if (!node.drag || !canContribute || busy) {
      return;
    }
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    const current = values[node.drag.var];
    const startValue = typeof current === "number" ? current : node.drag.min;
    setDrag({ nodeID: node.id, value: startValue });
  }

  function moveDrag(event: ReactPointerEvent<SVGGElement>, node: CustomNode) {
    if (!drag || drag.nodeID !== node.id || !node.drag) {
      return;
    }
    const bounds = event.currentTarget.ownerSVGElement?.getBoundingClientRect();
    if (!bounds) {
      return;
    }
    const axis = node.drag.axis === "y" ? "y" : "x";
    const size = axis === "x" ? Math.max(1, bounds.width) : Math.max(1, bounds.height);
    const delta = axis === "x" ? event.movementX : event.movementY;
    const span = node.drag.max - node.drag.min;
    const next = Math.max(node.drag.min, Math.min(node.drag.max,
      drag.value + (delta / size) * span));
    setDrag({ nodeID: node.id, value: next });
    setLocal((current) => ({ ...current, [node.drag!.var]: next }));
  }

  function endDrag(event: ReactPointerEvent<SVGGElement>, node: CustomNode) {
    if (!drag || drag.nodeID !== node.id || !node.drag) {
      return;
    }
    event.currentTarget.releasePointerCapture?.(event.pointerId);
    const value = drag.value;
    setDrag(null);
    if (canContribute && !busy) {
      onCustom(surfaceID, "drag", { nodeID: node.id, value });
    }
  }

  function inputValue(node: CustomNode): string {
    if (!node.input) {
      return "";
    }
    const current = values[node.input.var];
    if (local[node.input.var] !== undefined) {
      return String(local[node.input.var]);
    }
    return current === undefined || current === null ? "" : String(current);
  }

  function commitInput(node: CustomNode, value: string) {
    if (!node.input || !canContribute || busy) {
      return;
    }
    const parsed = node.input.type === "number" ? Number(value) : value;
    if (node.input.type === "number" && !Number.isFinite(parsed)) {
      return;
    }
    setLocal((current) => ({ ...current, [node.input!.var]: parsed }));
    onCustom(surfaceID, "input", { nodeID: node.id, value: parsed });
  }

  const showControls = Boolean(timeline);

  return (
    <div
      ref={containerRef}
      className="dw-custom-surface"
      data-testid="world-custom"
      data-surface-id={surfaceID}
      tabIndex={0}
      role="application"
      aria-label={program.title ?? (zh ? "可交互表面" : "Interactive surface")}
      onKeyDown={onKeyDown}
      style={{ outline: "none" }}
    >
      <div
        className="dw-custom-stage"
        style={{ position: "relative", width: "100%", aspectRatio: `${width} / ${height}` }}
      >
        <svg
          viewBox={`0 0 ${width} ${height}`}
          preserveAspectRatio="xMidYMid meet"
          style={{ width: "100%", height: "100%", display: "block", borderRadius: 16 }}
        >
          <defs>
            <linearGradient id={`custom-sky-${surfaceID}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={`hsl(${skyHue} 52% ${Math.max(4, lightness)}%)`} />
              <stop offset="100%" stopColor={`hsl(${groundHue} 58% ${Math.max(2, lightness - 4)}%)`} />
            </linearGradient>
          </defs>
          {mode === "gradient" ? (
            <rect width={width} height={height} fill={`url(#custom-sky-${surfaceID})`} />
          ) : null}
          {mode === "solid" ? (
            <rect width={width} height={height} fill={`hsl(${skyHue} 52% ${lightness}%)`} />
          ) : null}
          {nodes.map((node) => {
            if (!conditionHolds(node.visible_when, values, world)) {
              return null;
            }
            const visual = nodeVisual(node, time, values, world, trackByNode);
            const color = `hsl(${visual.hue} 70% 62%)`;
            const stroke = `hsl(${visual.strokeHue} 70% 66%)`;
            const strokeWidth = node.stroke_width ?? 0;
            const fill = `${color}`;
            const transform = `translate(${visual.x} ${visual.y}) rotate(${visual.rotate}) scale(${visual.scale})`;
            const interactive = Boolean(node.on_click?.length || node.drag) && canContribute && !busy;
            const common = {
              "data-custom-node": node.id,
              "data-testid": `custom-node-${node.id}`,
              opacity: visual.opacity,
              style: { cursor: interactive ? "pointer" : "default" },
              role: interactive ? "button" : "img",
              tabIndex: interactive ? 0 : undefined,
              "aria-label": node.text ?? node.id
            } as const;
            if (node.type === "input") {
              return null;
            }
            if (node.type === "text") {
              const bound = node.bind_text?.var
                ? values[node.bind_text.var]
                : node.bind_text?.entity_id
                  ? fieldValue(world, node.bind_text.entity_id, node.bind_text.field ?? "")
                  : undefined;
              const content = bound !== undefined ? String(bound) : stringValue(node.text);
              return (
                <g key={node.id} transform={transform} {...common}
                  onClick={() => invokeNode(node, "click")}>
                  <text
                    x={0}
                    y={0}
                    fill={color}
                    fontSize={node.size ?? 20}
                    textAnchor={node.align === "middle" ? "middle" : node.align === "end" ? "end" : "start"}
                    dominantBaseline="middle"
                  >
                    {content}
                  </text>
                </g>
              );
            }
            if (node.type === "path") {
              return (
                <g key={node.id} transform={transform} {...common}
                  onClick={() => invokeNode(node, "click")}>
                  <path
                    d={node.path ?? ""}
                    fill={strokeWidth > 0 ? "none" : fill}
                    stroke={strokeWidth > 0 ? stroke : "transparent"}
                    strokeWidth={strokeWidth || 0}
                    strokeLinejoin="round"
                    strokeLinecap="round"
                  />
                </g>
              );
            }
            if (node.type === "dot") {
              return (
                <g key={node.id} transform={transform} {...common}
                  onPointerDown={(event) => startDrag(event, node)}
                  onPointerMove={(event) => moveDrag(event, node)}
                  onPointerUp={(event) => endDrag(event, node)}
                  onClick={() => {
                    if (!drag) {
                      invokeNode(node, "click");
                    }
                  }}>
                  <circle r={Math.max(4, visual.r)} fill={fill} stroke={stroke}
                    strokeWidth={strokeWidth} />
                </g>
              );
            }
            return (
              <g key={node.id} transform={transform} {...common}
                onPointerDown={(event) => startDrag(event, node)}
                onPointerMove={(event) => moveDrag(event, node)}
                onPointerUp={(event) => endDrag(event, node)}
                onClick={() => {
                  if (!drag) {
                    invokeNode(node, "click");
                  }
                }}>
                <rect
                  x={-visual.w / 2}
                  y={-visual.h / 2}
                  width={Math.max(1, visual.w)}
                  height={Math.max(1, visual.h)}
                  rx={node.corner ?? 12}
                  fill={fill}
                  stroke={stroke}
                  strokeWidth={strokeWidth}
                />
              </g>
            );
          })}
        </svg>
        {nodes.filter((node) => node.type === "input").map((node) => {
          if (!conditionHolds(node.visible_when, values, world)) {
            return null;
          }
          const visual = nodeVisual(node, time, values, world, trackByNode);
          const style: CSSProperties = {
            position: "absolute",
            left: `${(visual.x / width) * 100}%`,
            top: `${(visual.y / height) * 100}%`,
            width: `${(Math.max(40, visual.w) / width) * 100}%`,
            height: `${(Math.max(24, visual.h) / height) * 100}%`,
            transform: "translate(-50%, -50%)",
            borderRadius: node.corner ?? 10,
            border: `1px solid hsl(${visual.strokeHue} 60% 55%)`,
            background: `hsla(${visual.hue} 40% 12% / 0.9)`,
            color: "#f4f1e8",
            padding: "0.35rem 0.6rem",
            fontSize: `${Math.max(12, Math.min(24, (node.size ?? 18) * 0.9))}px`,
            outline: "none"
          };
          return (
            <input
              key={node.id}
              data-testid={`custom-input-${node.id}`}
              style={style}
              type={node.input?.type === "number" ? "number" : "text"}
              maxLength={node.input?.max_length ?? 1200}
              value={inputValue(node)}
              disabled={!canContribute || busy}
              onChange={(event) => {
                const value = event.target.value;
                setLocal((current) => ({ ...current, [node.input!.var]: value }));
              }}
              onBlur={(event) => commitInput(node, event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.currentTarget.blur();
                }
              }}
            />
          );
        })}
      </div>
      {showControls ? (
        <div className="dw-custom-controls">
          <button
            type="button"
            className="dw-button dw-button-small dw-button-ghost"
            onClick={() => setPlaying((current) => !current)}
          >
            {playing ? (zh ? "暂停" : "Pause") : (zh ? "播放" : "Play")}
          </button>
          <input
            type="range"
            min={0}
            max={duration}
            step={0.05}
            value={time}
            aria-label={zh ? "时间" : "Time"}
            onChange={(event) => {
              setPlaying(false);
              setTime(Number(event.target.value));
            }}
          />
          <small>{time.toFixed(1)}s / {duration.toFixed(0)}s</small>
        </div>
      ) : null}
      <p className="dw-custom-hint">
        {program.hint ??
          (zh
            ? "点击、拖动、输入或使用方向键；每次真实改变都会保存。"
            : "Click, drag, type or use the arrow keys; every real change is saved.")}
        {busy ? ` · ${zh ? "正在保存…" : "Saving…"}` : ""}
      </p>
    </div>
  );
}
