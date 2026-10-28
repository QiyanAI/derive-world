// SPDX-License-Identifier: Apache-2.0
import { useMemo, type CSSProperties } from "react";
import type {
  OpenWorldBinding,
  OpenWorldBindingTarget,
  OpenWorldKind,
  OpenWorldNode,
  OpenWorldPresentation,
  OpenWorldState
} from "../../api";
import { useI18n } from "../../i18n";

const WORLD_WIDTH = 1600;
const WORLD_HEIGHT = 1000;

type Visual = {
  size: number;
  glow: number;
  opacity: number;
  hue: number;
  speed: number;
  active: boolean;
  label: string;
  layer: string;
  shape: string;
  motion: string;
  intensity: number;
};

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function hsl(hue: number, saturation: number, lightness: number, alpha = 1): string {
  return `hsla(${((hue % 360) + 360) % 360} ${saturation}% ${lightness}% / ${alpha})`;
}

function bindingFor(kind: OpenWorldKind, target: OpenWorldBindingTarget): OpenWorldBinding | undefined {
  return kind.bindings.find((binding) => binding.target === target);
}

function bindingValue(kind: OpenWorldKind, node: OpenWorldNode, target: OpenWorldBindingTarget): number | null {
  const binding = bindingFor(kind, target);
  if (!binding) {
    return null;
  }
  const raw = node.values[binding.field];
  let t: number;
  if (typeof raw === "boolean") {
    t = raw ? 1 : 0;
  } else if (typeof raw === "number" && Number.isFinite(raw)) {
    const span = binding.in_max - binding.in_min;
    if (span === 0) {
      return binding.out_min;
    }
    t = clamp((raw - binding.in_min) / span, 0, 1);
  } else {
    return null;
  }
  return binding.out_min + t * (binding.out_max - binding.out_min);
}

function visualFor(kind: OpenWorldKind, node: OpenWorldNode): Visual {
  const scale = bindingValue(kind, node, "scale") ?? 1;
  const glow = bindingValue(kind, node, "glow") ?? kind.appearance.glow;
  const opacity = bindingValue(kind, node, "opacity") ?? 1;
  const hueShift = bindingValue(kind, node, "hue_shift") ?? 0;
  const speed = bindingValue(kind, node, "speed") ?? 1;
  return {
    size: clamp(kind.appearance.size * scale, 6, 420),
    glow: clamp(glow, 0, 100),
    opacity: clamp(opacity, 0, 1),
    hue: kind.hue + hueShift,
    speed: clamp(speed, 0.2, 3),
    active: node.status === "done",
    label: kind.appearance.label,
    layer: kind.appearance.layer,
    shape: kind.appearance.shape,
    motion: kind.appearance.motion,
    intensity: kind.appearance.intensity
  };
}

function motionDuration(visual: Visual): number {
  const intensity = Math.max(visual.intensity, 12) / 50;
  return clamp(6 / (intensity * visual.speed), 0.8, 16);
}

function layerOrder(layer: string): number {
  switch (layer) {
    case "back":
      return 0;
    case "front":
      return 2;
    default:
      return 1;
  }
}

function pseudoRandom(index: number, salt: number): number {
  const value = Math.sin(index * 127.1 + salt * 311.7) * 43758.5453123;
  return value - Math.floor(value);
}

function particleCount(density: number): number {
  return Math.min(24, Math.round((clamp(density, 0, 100) / 100) * 22));
}

function linkPath(from: OpenWorldNode, to: OpenWorldNode, style: OpenWorldPresentation["link_style"]): string | null {
  if (style === "hidden") {
    return null;
  }
  const midX = (from.x + to.x) / 2;
  const midY = (from.y + to.y) / 2;
  if (style === "arc" || style === "orbit") {
    const offset = style === "orbit" ? 180 : 90;
    const dx = to.x - from.x;
    const dy = to.y - from.y;
    const length = Math.max(1, Math.hypot(dx, dy));
    const controlX = midX - (dy / length) * offset;
    const controlY = midY + (dx / length) * offset;
    return `M ${from.x} ${from.y} Q ${controlX} ${controlY} ${to.x} ${to.y}`;
  }
  return `M ${from.x} ${from.y} C ${midX} ${from.y}, ${midX} ${to.y}, ${to.x} ${to.y}`;
}

function Backdrop({ presentation, uid }: { presentation: OpenWorldPresentation; uid: string }) {
  const scene = presentation.scene;
  const horizonY = (WORLD_HEIGHT * scene.horizon) / 100;
  const sky = hsl(scene.sky_hue, 52, 9);
  const skyHigh = hsl(scene.sky_hue + 18, 46, 20);
  const ground = hsl(scene.ground_hue, 58, 8);
  const groundLow = hsl(scene.ground_hue - 12, 62, 3);
  const accent = hsl(scene.accent_hue, 82, 64);
  const particles = particleCount(scene.ambient_density);

  if (scene.backdrop === "void") {
    return (
      <>
        <defs>
          <radialGradient id={`${uid}-void`} cx="50%" cy="46%" r="72%">
            <stop offset="0%" stopColor={hsl(scene.sky_hue, 62, 24)} />
            <stop offset="48%" stopColor={hsl(scene.ground_hue, 55, 9)} />
            <stop offset="100%" stopColor="#04050a" />
          </radialGradient>
        </defs>
        <rect width={WORLD_WIDTH} height={WORLD_HEIGHT} fill={`url(#${uid}-void)`} />
        <g className="scene-orbits" aria-hidden="true">
          {[170, 290, 410, 540].map((radius, index) => (
            <ellipse
              key={radius}
              cx={WORLD_WIDTH / 2}
              cy={WORLD_HEIGHT / 2}
              rx={radius * 1.55}
              ry={radius}
              fill="none"
              stroke={accent}
              strokeOpacity={0.09 - index * 0.014}
              strokeWidth={1}
              strokeDasharray={index % 2 === 0 ? "2 10" : "none"}
            />
          ))}
          <circle cx={WORLD_WIDTH / 2} cy={WORLD_HEIGHT / 2} r={120} fill={accent} opacity={0.06} />
        </g>
        <Ambient kind={scene.ambient} count={particles} horizonY={horizonY} accent={accent} />
      </>
    );
  }

  if (scene.backdrop === "loom") {
    return (
      <>
        <defs>
          <linearGradient id={`${uid}-loom`} x1="0" y1="0" x2="0.4" y2="1">
            <stop offset="0%" stopColor={hsl(scene.sky_hue, 35, 10)} />
            <stop offset="100%" stopColor={hsl(scene.ground_hue, 40, 4)} />
          </linearGradient>
          <pattern id={`${uid}-warp`} width="56" height="56" patternUnits="userSpaceOnUse">
            <path d="M 56 0 L 0 0 0 56" fill="none" stroke={accent} strokeOpacity="0.055" strokeWidth="1" />
          </pattern>
        </defs>
        <rect width={WORLD_WIDTH} height={WORLD_HEIGHT} fill={`url(#${uid}-loom)`} />
        <rect width={WORLD_WIDTH} height={WORLD_HEIGHT} fill={`url(#${uid}-warp)`} />
        <g aria-hidden="true" stroke={accent} strokeOpacity="0.08">
          {Array.from({ length: 9 }, (_, index) => (
            <line
              key={index}
              x1={0}
              x2={WORLD_WIDTH}
              y1={140 + index * 90}
              y2={140 + index * 90}
            />
          ))}
        </g>
        <Ambient kind={scene.ambient} count={particles} horizonY={horizonY} accent={accent} />
      </>
    );
  }

  return (
    <>
      <defs>
        <linearGradient id={`${uid}-sky`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={sky} />
          <stop offset="100%" stopColor={skyHigh} />
        </linearGradient>
        <linearGradient id={`${uid}-ground`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={ground} />
          <stop offset="100%" stopColor={groundLow} />
        </linearGradient>
        <radialGradient id={`${uid}-horizon-glow`} cx="50%" cy="50%" r="50%">
          <stop offset="0%" stopColor={accent} stopOpacity="0.34" />
          <stop offset="60%" stopColor={accent} stopOpacity="0.08" />
          <stop offset="100%" stopColor={accent} stopOpacity="0" />
        </radialGradient>
      </defs>
      <rect width={WORLD_WIDTH} height={WORLD_HEIGHT} fill={`url(#${uid}-sky)`} />
      <g className="scene-stars" aria-hidden="true">
        <Ambient kind={scene.ambient} count={particles} horizonY={horizonY} accent={accent} />
      </g>
      <circle
        cx={WORLD_WIDTH * 0.72}
        cy={horizonY * 0.34}
        r={150}
        fill={`url(#${uid}-horizon-glow)`}
        aria-hidden="true"
      />
      <circle cx={WORLD_WIDTH * 0.72} cy={horizonY * 0.34} r={36} fill={accent} opacity={0.5} aria-hidden="true" />
      <rect y={horizonY} width={WORLD_WIDTH} height={WORLD_HEIGHT - horizonY} fill={`url(#${uid}-ground)`} />
      <g className="scene-tide" aria-hidden="true" fill="none" stroke={accent} strokeOpacity="0.22">
        <path d={`M -80 ${horizonY + 16} Q 180 ${horizonY + 2} 440 ${horizonY + 16} T 960 ${horizonY + 16} T 1680 ${horizonY + 16}`} />
        <path d={`M -80 ${horizonY + 54} Q 220 ${horizonY + 36} 520 ${horizonY + 54} T 1040 ${horizonY + 54} T 1760 ${horizonY + 54}`} strokeOpacity="0.13" />
        <path d={`M -80 ${horizonY + 102} Q 260 ${horizonY + 80} 600 ${horizonY + 102} T 1120 ${horizonY + 102} T 1840 ${horizonY + 102}`} strokeOpacity="0.08" />
      </g>
    </>
  );
}

function Ambient({
  kind,
  count,
  horizonY,
  accent
}: {
  kind: OpenWorldPresentation["scene"]["ambient"];
  count: number;
  horizonY: number;
  accent: string;
}) {
  if (kind === "none" || count === 0) {
    return null;
  }
  const items = Array.from({ length: count }, (_, index) => {
    const x = pseudoRandom(index, 1) * WORLD_WIDTH;
    const y = kind === "stars"
      ? pseudoRandom(index, 2) * Math.max(80, horizonY - 40)
      : pseudoRandom(index, 2) * WORLD_HEIGHT;
    const radius = 1 + pseudoRandom(index, 3) * (kind === "embers" ? 3.4 : 2.2);
    const delay = -pseudoRandom(index, 4) * 12;
    const duration = 5 + pseudoRandom(index, 5) * 9;
    return { x, y, radius, delay, duration };
  });
  if (kind === "rain") {
    return (
      <g aria-hidden="true" stroke={accent} strokeOpacity="0.28" strokeWidth="1.2">
        {items.map((item, index) => (
          <line
            key={index}
            className="scene-rain-drop"
            x1={item.x}
            x2={item.x - 8}
            y1={item.y}
            y2={item.y + 24 + item.radius * 4}
            style={{ animationDelay: `${item.delay}s`, animationDuration: `${1.1 + item.duration / 8}s` }}
          />
        ))}
      </g>
    );
  }
  return (
    <g aria-hidden="true">
      {items.map((item, index) => (
        <circle
          key={index}
          className={`scene-particle scene-particle-${kind}`}
          cx={item.x}
          cy={item.y}
          r={item.radius}
          fill={accent}
          opacity={kind === "stars" ? 0.55 : 0.34}
          style={{
            animationDelay: `${item.delay}s`,
            animationDuration: `${item.duration}s`
          }}
        />
      ))}
    </g>
  );
}

function NodeShape({ visual, color }: { visual: Visual; color: string }) {
  const size = visual.size;
  const activeGlow = 0.45 + visual.glow / 160;
  switch (visual.shape) {
    case "beacon": {
      const width = size * 0.24;
      const height = size * 0.9;
      return (
        <>
          <path
            d={`M ${-width * 0.52} ${height * 0.45} L ${-width * 0.3} ${-height * 0.24} L ${width * 0.3} ${-height * 0.24} L ${width * 0.52} ${height * 0.45} Z`}
            fill={color}
            opacity={0.24}
          />
          <path
            d={`M ${-width * 0.18} ${height * 0.45} L ${-width * 0.11} ${-height * 0.22} L ${width * 0.11} ${-height * 0.22} L ${width * 0.18} ${height * 0.45} Z`}
            fill={color}
            opacity={0.78}
          />
          <path
            d={`M 0 ${-height * 0.3} L ${-width * 2.6} ${-height * 0.42} L ${-width * 2.6} ${-height * 0.1} Z`}
            fill={color}
            opacity={0.08 + visual.glow / 320}
          />
          <path
            d={`M 0 ${-height * 0.3} L ${width * 2.6} ${-height * 0.42} L ${width * 2.6} ${-height * 0.1} Z`}
            fill={color}
            opacity={0.08 + visual.glow / 320}
          />
          <circle cy={-height * 0.27} r={size * 0.11} fill="#fff8dc" opacity={clamp(activeGlow, 0, 1)} />
          <ellipse cy={height * 0.46} rx={width * 0.9} ry={size * 0.05} fill="#000" opacity={0.3} />
        </>
      );
    }
    case "wave":
      return (
        <>
          {[0, 1, 2].map((index) => (
            <path
              key={index}
              d={`M ${-size * 0.95} ${(index - 1) * size * 0.2} Q ${-size * 0.5} ${(index - 1) * size * 0.2 - size * 0.18} 0 ${(index - 1) * size * 0.2} T ${size * 0.95} ${(index - 1) * size * 0.2}`}
              fill="none"
              stroke={color}
              strokeWidth={3 - index * 0.6}
              strokeLinecap="round"
              opacity={0.62 - index * 0.12}
            />
          ))}
          <path
            d={`M ${-size * 0.8} ${size * 0.42} Q ${-size * 0.35} ${size * 0.3} 0 ${size * 0.42} T ${size * 0.8} ${size * 0.42}`}
            fill="none"
            stroke={color}
            strokeWidth={1.4}
            strokeOpacity={0.24}
            strokeDasharray="5 8"
          />
        </>
      );
    case "shard":
      return (
        <>
          <polygon
            points={`0,${-size * 0.56} ${size * 0.34},${-size * 0.1} ${size * 0.16},${size * 0.52} ${-size * 0.16},${size * 0.52} ${-size * 0.34},${-size * 0.1}`}
            fill={color}
            opacity={0.28}
            stroke={color}
            strokeWidth={2}
          />
          <polygon
            points={`0,${-size * 0.4} ${size * 0.16},${-size * 0.06} 0,${size * 0.34} ${-size * 0.16},${-size * 0.06}`}
            fill={color}
            opacity={0.62}
          />
          <line x1={0} y1={-size * 0.34} x2={0} y2={size * 0.3} stroke="#fff" strokeOpacity={0.35} strokeWidth={1} />
        </>
      );
    case "ring":
      return (
        <>
          <circle r={size * 0.42} fill="none" stroke={color} strokeWidth={size * 0.055} opacity={0.9} />
          <circle r={size * 0.3} fill="none" stroke={color} strokeWidth={1.5} strokeDasharray="3 7" opacity={0.5} />
          <circle r={size * 0.13} fill={color} opacity={0.8} />
        </>
      );
    case "glyph": {
      const points = Array.from({ length: 16 }, (_, index) => {
        const angle = (Math.PI * 2 * index) / 16 - Math.PI / 2;
        const radius = index % 2 === 0 ? size * 0.48 : size * 0.16;
        return `${Math.cos(angle) * radius},${Math.sin(angle) * radius}`;
      });
      return (
        <>
          <polygon points={points.join(" ")} fill="none" stroke={color} strokeWidth={2.2} strokeLinejoin="round" opacity={0.82} />
          <circle r={size * 0.1} fill={color} opacity={0.85} />
          <circle r={size * 0.46} fill="none" stroke={color} strokeOpacity={0.22} strokeWidth={1} strokeDasharray="2 9" />
        </>
      );
    }
    case "bloom":
      return (
        <>
          {[0, 1, 2, 3, 4, 5].map((index) => (
            <ellipse
              key={index}
              rx={size * 0.16}
              ry={size * 0.38}
              cy={-size * 0.2}
              fill={color}
              opacity={0.5}
              transform={`rotate(${index * 60})`}
            />
          ))}
          <circle r={size * 0.13} fill="#fff" opacity={0.78} />
          <circle r={size * 0.47} fill="none" stroke={color} strokeOpacity={0.2} strokeWidth={1} />
        </>
      );
    case "thread":
      return (
        <>
          {[0, 45, 90, 135].map((angle) => (
            <line
              key={angle}
              x1={0}
              y1={0}
              x2={Math.cos((angle * Math.PI) / 180) * size * 0.55}
              y2={Math.sin((angle * Math.PI) / 180) * size * 0.55}
              stroke={color}
              strokeOpacity={0.4}
              strokeWidth={1.2}
            />
          ))}
          <circle r={size * 0.15} fill={color} opacity={0.8} />
          <circle r={size * 0.48} fill="none" stroke={color} strokeOpacity={0.16} strokeWidth={1} strokeDasharray="2 8" />
        </>
      );
    default:
      return (
        <>
          <circle r={size * 0.4} fill={color} opacity={0.2} />
          <circle r={size * 0.29} fill={color} opacity={0.88} />
          <circle cx={-size * 0.08} cy={-size * 0.08} r={size * 0.08} fill="#fff" opacity={0.5} />
        </>
      );
  }
}

export default function WorldScene({
  world,
  worldID,
  canContribute,
  busy,
  onTouch
}: {
  world: OpenWorldState;
  worldID: string;
  canContribute: boolean;
  busy: boolean;
  onTouch: (nodeID: string) => void;
}) {
  const { t } = useI18n();
  const uid = `dw-scene-${worldID.replace(/[^a-zA-Z0-9_-]/g, "")}`;
  const presentation = world.presentation;
  const nodeByID = useMemo(() => new Map(world.nodes.map((node) => [node.id, node])), [world.nodes]);
  const kindByID = useMemo(() => new Map(world.kinds.map((kind) => [kind.id, kind])), [world.kinds]);
  const ordered = useMemo(
    () =>
      [...world.nodes].sort((left, right) => {
        const leftKind = kindByID.get(left.kind_id);
        const rightKind = kindByID.get(right.kind_id);
        const layerDiff = layerOrder(leftKind?.appearance.layer ?? "mid") - layerOrder(rightKind?.appearance.layer ?? "mid");
        return layerDiff !== 0 ? layerDiff : left.y - right.y;
      }),
    [world.nodes, kindByID]
  );

  return (
    <div
      className="dw-world-scene"
      data-testid="open-world-scene"
      data-backdrop={presentation.scene.backdrop}
      data-link-style={presentation.link_style}
      style={{ "--scene-accent-hue": presentation.scene.accent_hue } as CSSProperties}
    >
      <svg
        viewBox={`0 0 ${WORLD_WIDTH} ${WORLD_HEIGHT}`}
        role="img"
        aria-label={world.title}
        preserveAspectRatio="xMidYMid meet"
      >
        <defs>
          <filter id={`${uid}-blur`} x="-60%" y="-60%" width="220%" height="220%">
            <feGaussianBlur stdDeviation="9" />
          </filter>
        </defs>
        <Backdrop presentation={presentation} uid={uid} />
        <g className="scene-links" aria-hidden="true">
          {presentation.link_style === "orbit"
            ? [300, 460, 620].map((radius) => (
                <ellipse
                  key={radius}
                  cx={WORLD_WIDTH / 2}
                  cy={WORLD_HEIGHT / 2}
                  rx={radius * 1.5}
                  ry={radius}
                  fill="none"
                  stroke={hsl(presentation.scene.accent_hue, 70, 60)}
                  strokeOpacity={0.08}
                  strokeWidth={1}
                  strokeDasharray="3 12"
                />
              ))
            : null}
          {world.links.map((link) => {
            const from = nodeByID.get(link.from);
            const to = nodeByID.get(link.to);
            if (!from || !to) {
              return null;
            }
            const path = linkPath(from, to, presentation.link_style);
            if (!path) {
              return null;
            }
            const midX = (from.x + to.x) / 2;
            const midY = (from.y + to.y) / 2;
            return (
              <g key={link.id} className={`scene-link scene-link-${presentation.link_style}`}>
                <path
                  d={path}
                  fill="none"
                  stroke={hsl(presentation.scene.accent_hue, 70, 62)}
                  strokeOpacity={0.24}
                  strokeWidth={1.6}
                  strokeDasharray={presentation.link_style === "thread" ? undefined : "4 10"}
                />
                {link.label && presentation.link_style !== "orbit" ? (
                  <text x={midX} y={midY - 10} textAnchor="middle" className="scene-link-label">
                    {link.label}
                  </text>
                ) : null}
              </g>
            );
          })}
        </g>
        <g className="scene-nodes">
          {ordered.map((node) => {
            const kind = kindByID.get(node.kind_id);
            if (!kind) {
              return null;
            }
            const visual = visualFor(kind, node);
            const color = hsl(visual.hue, 68, 60);
            const duration = motionDuration(visual);
            const motionClass = visual.motion === "none" ? "" : `scene-motion-${visual.motion}`;
            return (
              <g
                key={node.id}
                transform={`translate(${node.x} ${node.y})`}
                className={`scene-node${visual.active ? " is-active" : ""}`}
                data-testid="world-scene-node"
                data-node-id={node.id}
                data-shape={visual.shape}
                data-motion={visual.motion}
                data-active={visual.active ? "true" : "false"}
                data-glow={Math.round(visual.glow)}
                data-size={Math.round(visual.size)}
                data-opacity={visual.opacity.toFixed(2)}
                role={canContribute ? "button" : "img"}
                tabIndex={canContribute ? 0 : undefined}
                aria-label={canContribute ? `${node.name}. ${t("openWorld.touch")}` : node.name}
                aria-disabled={busy || undefined}
                onClick={() => {
                  if (canContribute && !busy) {
                    onTouch(node.id);
                  }
                }}
                onKeyDown={(event) => {
                  if (canContribute && !busy && (event.key === "Enter" || event.key === " ")) {
                    event.preventDefault();
                    onTouch(node.id);
                  }
                }}
                style={{ opacity: visual.opacity, cursor: canContribute && !busy ? "pointer" : "default" }}
              >
                <circle
                  r={visual.size * 0.7}
                  fill={color}
                  opacity={clamp(visual.glow / 320, 0.02, 0.4)}
                  filter={`url(#${uid}-blur)`}
                />
                <g className={motionClass} style={{ animationDuration: `${duration}s` }}>
                  <NodeShape visual={visual} color={color} />
                </g>
                {visual.active ? (
                  <circle
                    r={visual.size * 0.62}
                    fill="none"
                    stroke={color}
                    strokeWidth={1.6}
                    strokeDasharray="4 7"
                    className="scene-active-ring"
                  />
                ) : null}
                {visual.label === "hidden" ? null : (
                  <text
                    y={visual.shape === "beacon" ? visual.size * 0.58 : visual.size * 0.74}
                    textAnchor="middle"
                    className={`scene-label${visual.label === "hover" ? " scene-label-hover" : ""}`}
                  >
                    {node.name}
                  </text>
                )}
              </g>
            );
          })}
        </g>
      </svg>
      <p className="dw-world-scene-hint">
        {canContribute ? t("openWorld.sceneHint") : t("workspace.permission.read")}
      </p>
    </div>
  );
}
