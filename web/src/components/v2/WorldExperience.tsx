// SPDX-License-Identifier: Apache-2.0
import { useEffect, useId, useState, type CSSProperties, type ReactNode } from "react";
import { useI18n, type MessageKey } from "../../i18n";
import type {
  OpenWorldField,
  OpenWorldKind,
  OpenWorldNode,
  OpenWorldState,
  WorldCondition,
  WorldExperience,
  WorldViewNode
} from "../../api";
import CustomSurface from "./CustomSurface";
import LiveSurface from "./LiveSurface";
import StorySurface from "./StorySurface";
import WorkspaceSurface from "./WorkspaceSurface";

const WORLD_WIDTH = 1600;
const WORLD_HEIGHT = 1000;
const TEMPLATE_TOKEN = /\{\{([^{}]{1,200})\}\}/g;

type Scope = { item?: OpenWorldNode };

type ExperienceContext = {
  world: OpenWorldState;
  canContribute: boolean;
  busy: boolean;
  onInvoke: (behaviorID: string, entityID?: string, value?: unknown) => void;
  onSetField: (entityID: string, field: string, value: string | number | boolean) => void;
  onCustom: (
    surfaceID: string,
    event: "click" | "key" | "input" | "drag",
    payload: { nodeID?: string; key?: string; value?: unknown }
  ) => void;
};

export default function WorldExperience({
  world,
  experience,
  canContribute,
  busy,
  onInvoke,
  onSetField,
  onCustom
}: {
  world: OpenWorldState;
  experience: WorldExperience;
  canContribute: boolean;
  busy: boolean;
  onInvoke: (behaviorID: string, entityID?: string, value?: unknown) => void;
  onSetField: (entityID: string, field: string, value: string | number | boolean) => void;
  onCustom: (
    surfaceID: string,
    event: "click" | "key" | "input" | "drag",
    payload: { nodeID?: string; key?: string; value?: unknown }
  ) => void;
}) {
  const context: ExperienceContext = {
    world, canContribute, busy, onInvoke, onSetField, onCustom
  };
  return (
    <div className="dw-experience" data-testid="world-experience" data-protocol={experience.protocol}>
      <NodeView node={experience.root} scope={{}} context={context} />
    </div>
  );
}

function NodeView({
  node,
  scope,
  context,
  itemKey
}: {
  node: WorldViewNode;
  scope: Scope;
  context: ExperienceContext;
  itemKey?: string;
}) {
  if (node.visible_when && !conditionHolds(node.visible_when, context.world, scope)) {
    return null;
  }
  const props = asRecord(node.props);
  const children = Array.isArray(node.children) ? node.children : [];
  const renderChildren = (childScope: Scope = scope) =>
    children.map((child) => (
      <NodeView
        key={`${itemKey ?? "root"}:${child.id}`}
        node={child}
        scope={childScope}
        context={context}
        itemKey={childScope.item?.id ?? itemKey}
      />
    ));

  switch (node.capability) {
    case "core.stack":
      return <StackNode props={props} node={node}>{renderChildren()}</StackNode>;
    case "core.text":
      return <TextNode props={props} context={context} scope={scope} node={node} />;
    case "core.space":
      return <SpaceNode props={props} context={context} node={node} />;
    case "core.collection":
      return (
        <CollectionNode
          props={props}
          context={context}
          node={node}
          renderChild={(item) => renderChildren({ item })}
        />
      );
    case "core.table":
      return <TableNode props={props} context={context} node={node} />;
    case "core.field":
      return <FieldNode props={props} context={context} scope={scope} node={node} />;
    case "core.button":
      return <ButtonNode props={props} context={context} scope={scope} node={node} />;
    case "core.choice":
      return <ChoiceNode props={props} context={context} scope={scope} node={node} />;
    case "core.meter":
      return <MeterNode props={props} context={context} scope={scope} node={node} />;
    case "core.live":
      return (
        <LiveSurface
          node={node}
          world={context.world}
          canContribute={context.canContribute}
          busy={context.busy}
          onInvoke={context.onInvoke}
        />
      );
    case "core.story":
      return (
        <StorySurface
          node={node}
          world={context.world}
          canContribute={context.canContribute}
          busy={context.busy}
          onInvoke={context.onInvoke}
        />
      );
    case "core.workspace":
      return (
        <WorkspaceSurface
          node={node}
          world={context.world}
          canContribute={context.canContribute}
          busy={context.busy}
          onInvoke={context.onInvoke}
          onSetField={context.onSetField}
        />
      );
    case "core.custom":
      return (
        <CustomSurface
          surfaceID={node.id}
          props={props}
          world={context.world}
          canContribute={context.canContribute}
          busy={context.busy}
          onCustom={context.onCustom}
        />
      );
    case "core.tabs":
      return (
        <TabsNode props={props} node={node} scope={scope} context={context} />
      );
    default:
      return (
        <p className="dw-exp-unsupported" data-testid="world-experience-unsupported">
          {node.capability}
        </p>
      );
  }
}

function StackNode({
  props,
  node,
  children
}: {
  props: Record<string, unknown>;
  node: WorldViewNode;
  children: ReactNode;
}) {
  const direction = stringValue(props.direction, "vertical");
  const style: CSSProperties = {
    display: "flex",
    flexDirection: direction === "horizontal" ? "row" : "column",
    gap: numberValue(props.gap, 12),
    padding: numberValue(props.padding, 0),
    alignItems:
      stringValue(props.align, "stretch") === "center"
        ? "center"
        : stringValue(props.align, "stretch") === "start"
          ? "flex-start"
          : "stretch",
    flexWrap: direction === "horizontal" ? "wrap" : "nowrap"
  };
  return (
    <section
      className={`dw-exp-stack dw-exp-tone-${stringValue(props.tone, "plain")}`}
      style={style}
      data-testid="world-experience-stack"
      data-capability-node={node.id}
    >
      {children}
    </section>
  );
}

function TextNode({
  props,
  context,
  scope,
  node
}: {
  props: Record<string, unknown>;
  context: ExperienceContext;
  scope: Scope;
  node: WorldViewNode;
}) {
  const weight = stringValue(props.weight, "regular");
  return (
    <p
      className={`dw-exp-text dw-exp-tone-${stringValue(props.tone, "default")}`}
      style={{
        fontSize: numberValue(props.size, 18),
        fontWeight: weight === "bold" ? 700 : weight === "medium" ? 600 : 400,
        textAlign: stringValue(props.align, "left") as CSSProperties["textAlign"]
      }}
      data-testid="world-experience-text"
      data-capability-node={node.id}
    >
      {interpolate(stringValue(props.text, ""), context.world, scope)}
    </p>
  );
}

function ButtonNode({
  props,
  context,
  scope,
  node
}: {
  props: Record<string, unknown>;
  context: ExperienceContext;
  scope: Scope;
  node: WorldViewNode;
}) {
  const disabled = !context.canContribute || context.busy;
  const behaviorID = stringValue(props.behavior_id, "");
  return (
    <button
      type="button"
      className={`dw-button dw-button-small ${stringValue(props.tone, "primary") === "ghost" ? "dw-button-ghost" : "dw-button-primary"} dw-exp-button`}
      disabled={disabled || !behaviorID}
      onClick={() => context.onInvoke(behaviorID, scope.item?.id)}
      data-testid="world-experience-button"
      data-behavior-id={behaviorID}
      data-capability-node={node.id}
    >
      {stringValue(props.label, "…")}
    </button>
  );
}

function ChoiceNode({
  props,
  context,
  scope,
  node
}: {
  props: Record<string, unknown>;
  context: ExperienceContext;
  scope: Scope;
  node: WorldViewNode;
}) {
  const disabled = !context.canContribute || context.busy;
  const prompt = stringValue(props.prompt, "");
  return (
    <section className="dw-exp-choice" data-testid="world-experience-choice" data-capability-node={node.id}>
      {prompt ? <p className="dw-exp-choice-prompt">{interpolate(prompt, context.world, scope)}</p> : null}
      <div className="dw-exp-choice-options">
        {asArray(props.options).map((raw, index) => {
          const option = asRecord(raw);
          const behaviorID = stringValue(option.behavior_id, "");
          return (
            <button
              key={`${behaviorID}:${index}`}
              type="button"
              className="dw-button dw-button-small dw-button-ghost dw-exp-choice-option"
              disabled={disabled || !behaviorID}
              onClick={() => context.onInvoke(behaviorID, scope.item?.id)}
              data-testid="world-experience-choice-option"
            >
              {stringValue(option.label, "…")}
            </button>
          );
        })}
      </div>
    </section>
  );
}

function FieldNode({
  props,
  context,
  scope,
  node
}: {
  props: Record<string, unknown>;
  context: ExperienceContext;
  scope: Scope;
  node: WorldViewNode;
}) {
  const entityID = resolveEntityID(props.entity_id, scope);
  const fieldKey = stringValue(props.field, "");
  const entity = context.world.nodes.find((candidate) => candidate.id === entityID);
  const kind = entity ? context.world.kinds.find((candidate) => candidate.id === entity.kind_id) : undefined;
  const field = kind?.fields.find((candidate) => candidate.key === fieldKey);
  if (!entityID || !entity || !field) {
    return <p className="dw-exp-unsupported">{fieldKey}</p>;
  }
  return (
    <div className="dw-exp-field" data-testid="world-experience-field" data-capability-node={node.id}>
      <span>{stringValue(props.label, "") || field.label || field.key}</span>
      <FieldControl
        field={field}
        value={entity.values[field.key]}
        disabled={!context.canContribute || context.busy}
        onCommit={(value) => context.onSetField(entityID, field.key, value)}
      />
    </div>
  );
}

function FieldControl({
  field,
  value,
  disabled,
  onCommit
}: {
  field: OpenWorldField;
  value: unknown;
  disabled: boolean;
  onCommit: (value: string | number | boolean) => void;
}) {
  const { t } = useI18n();
  const [draft, setDraft] = useState(displayValue(value));
  useEffect(() => {
    setDraft(displayValue(value));
  }, [value]);
  if (field.type === "flag") {
    return (
      <button
        type="button"
        className={`dw-open-flag${value === true ? " is-on" : ""}`}
        aria-pressed={value === true}
        disabled={disabled}
        onClick={() => onCommit(value !== true)}
      >
        {value === true ? t("openWorld.on") : t("openWorld.off")}
      </button>
    );
  }
  return (
    <form
      className="dw-exp-field-form"
      onSubmit={(event) => {
        event.preventDefault();
        if (field.type === "number") {
          const parsed = Number(draft);
          if (Number.isFinite(parsed)) {
            onCommit(parsed);
          }
          return;
        }
        onCommit(draft);
      }}
    >
      <input
        className="dw-input"
        type={field.type === "number" ? "number" : "text"}
        step={field.type === "number" ? "any" : undefined}
        value={draft}
        disabled={disabled}
        onChange={(event) => setDraft(event.target.value)}
        aria-label={field.label || field.key}
      />
      <button className="dw-button dw-button-small dw-button-ghost" type="submit" disabled={disabled}>
        {t("common.save")}
      </button>
    </form>
  );
}

function MeterNode({
  props,
  context,
  scope,
  node
}: {
  props: Record<string, unknown>;
  context: ExperienceContext;
  scope: Scope;
  node: WorldViewNode;
}) {
  const entityID = resolveEntityID(props.entity_id, scope);
  const fieldKey = stringValue(props.field, "");
  const entity = context.world.nodes.find((candidate) => candidate.id === entityID);
  const raw = entity?.values[fieldKey];
  const value = typeof raw === "number" && Number.isFinite(raw) ? raw : 0;
  const minimum = numberValue(props.min, 0);
  const maximum = numberValue(props.max, 10);
  const span = maximum - minimum;
  const percent = span > 0 ? Math.max(0, Math.min(100, ((value - minimum) / span) * 100)) : 0;
  return (
    <div
      className={`dw-exp-meter dw-exp-meter-${stringValue(props.tone, "accent")}`}
      role="meter"
      aria-valuemin={minimum}
      aria-valuemax={maximum}
      aria-valuenow={value}
      aria-label={stringValue(props.label, "") || fieldKey}
      data-testid="world-experience-meter"
      data-capability-node={node.id}
      data-value={value}
    >
      <div className="dw-exp-meter-head">
        <span>{stringValue(props.label, "") || fieldKey}</span>
        <strong>
          {formatNumber(value)} / {formatNumber(maximum)}
        </strong>
      </div>
      <div className="dw-exp-meter-track">
        <i style={{ width: `${percent}%` }} />
      </div>
    </div>
  );
}

function TableNode({
  props,
  context,
  node
}: {
  props: Record<string, unknown>;
  context: ExperienceContext;
  node: WorldViewNode;
}) {
  const { t } = useI18n();
  const kindID = stringValue(props.kind_id, "");
  const kind = context.world.kinds.find((candidate) => candidate.id === kindID);
  const columns = asArray(props.columns).map(asRecord);
  const rows = context.world.nodes
    .filter((candidate) => candidate.kind_id === kindID)
    .sort((left, right) => left.name.localeCompare(right.name));
  const rowBehaviorID = stringValue(props.row_behavior_id, "");
  const interactive = Boolean(rowBehaviorID) && context.canContribute && !context.busy;
  return (
    <div className="dw-exp-table-wrap" data-testid="world-experience-table" data-capability-node={node.id}>
      <table className="dw-exp-table">
        <thead>
          <tr>
            {columns.map((column, index) => (
              <th key={`${stringValue(column.source, "")}:${index}`}>
                {stringValue(column.label, "") || labelForSource(kind, stringValue(column.source, ""), t)}
              </th>
            ))}
            {rowBehaviorID ? (
              <th scope="col">
                <span className="dw-visually-hidden">Action</span>
              </th>
            ) : null}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr
              key={row.id}
              className={interactive ? "is-interactive" : ""}
              onClick={() => {
                if (interactive) {
                  context.onInvoke(rowBehaviorID, row.id);
                }
              }}
              data-testid="world-experience-row"
              data-entity-id={row.id}
            >
              {columns.map((column, index) => (
                <td key={`${stringValue(column.source, "")}:${index}`}>
                  {formatCell(row, stringValue(column.source, ""), kind)}
                </td>
              ))}
              {rowBehaviorID ? (
                <td className="dw-exp-table-action">
                  <button
                    type="button"
                    className="dw-icon-button dw-exp-row-button"
                    disabled={!interactive}
                    aria-label={stringValue(props.row_behavior_label, "") || t("openWorld.run")}
                    onClick={(event) => {
                      event.stopPropagation();
                      if (interactive) {
                        context.onInvoke(rowBehaviorID, row.id);
                      }
                    }}
                  >
                    ›
                  </button>
                </td>
              ) : null}
            </tr>
          ))}
        </tbody>
      </table>
      {rows.length === 0 ? <p className="dw-exp-empty">—</p> : null}
    </div>
  );
}

function CollectionNode({
  props,
  context,
  node,
  renderChild
}: {
  props: Record<string, unknown>;
  context: ExperienceContext;
  node: WorldViewNode;
  renderChild: (item: OpenWorldNode) => ReactNode;
}) {
  const kindID = stringValue(props.kind_id, "");
  const items = context.world.nodes
    .filter((candidate) => candidate.kind_id === kindID)
    .sort((left, right) => left.y - right.y || left.name.localeCompare(right.name));
  const layout = stringValue(props.layout, "list");
  const columns = Math.max(1, Math.min(6, Math.round(numberValue(props.columns, 2))));
  return (
    <section
      className={`dw-exp-collection dw-exp-collection-${layout}`}
      style={layout === "grid" ? { gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))` } : undefined}
      data-testid="world-experience-collection"
      data-capability-node={node.id}
    >
      {items.map((item) => (
        <article key={item.id} className="dw-exp-collection-item" data-entity-id={item.id}>
          {renderChild(item)}
        </article>
      ))}
      {items.length === 0 ? <p className="dw-exp-empty">{stringValue(props.empty_text, "—")}</p> : null}
    </section>
  );
}

function SpaceNode({
  props,
  context,
  node
}: {
  props: Record<string, unknown>;
  context: ExperienceContext;
  node: WorldViewNode;
}) {
  const { t } = useI18n();
  const uid = useId().replace(/[^a-zA-Z0-9_-]/g, "");
  const background = asRecord(props.background);
  const views = asArray(props.views).map(asRecord);
  const viewByKind = new Map(views.map((view) => [stringValue(view.kind_id, ""), view]));
  const mode = stringValue(background.mode, "gradient");
  const skyHue = numberValue(background.hue, 222);
  const groundHue = numberValue(background.hue2, 205);
  const lightness = numberValue(background.lightness, 10);
  const accentHue = numberValue(background.accent_hue, 42);
  const showLinks = props.show_links !== false;
  const linkStyle = stringValue(props.link_style, "curve");
  const activateBehaviorID = stringValue(props.activate_behavior_id, "");
  const entityBehaviors = new Map<string, string>();
  for (const raw of asArray(props.entity_actions)) {
    const action = asRecord(raw);
    const entityID = stringValue(action.entity_id, "");
    const behaviorID = stringValue(action.behavior_id, "");
    if (entityID && behaviorID) {
      entityBehaviors.set(entityID, behaviorID);
    }
  }
  const nodeById = new Map(context.world.nodes.map((candidate) => [candidate.id, candidate]));
  const rendered = context.world.nodes
    .filter((candidate) => viewByKind.has(candidate.kind_id))
    .filter((candidate) => {
      const view = viewByKind.get(candidate.kind_id);
      return !(view?.hide_done && candidate.status === "done");
    })
    .sort((left, right) => {
      const leftView = viewByKind.get(left.kind_id);
      const rightView = viewByKind.get(right.kind_id);
      const order = layerOrder(stringValue(leftView?.layer, "mid")) - layerOrder(stringValue(rightView?.layer, "mid"));
      return order !== 0 ? order : left.y - right.y;
    });
  const linkColor = `hsl(${accentHue} 70% 62%)`;
  return (
    <div className="dw-exp-space" data-testid="world-experience-space" data-capability-node={node.id}>
      <svg
        viewBox={`0 0 ${WORLD_WIDTH} ${WORLD_HEIGHT}`}
        role={activateBehaviorID && context.canContribute ? "group" : "img"}
        aria-label={context.world.title}
        preserveAspectRatio="xMidYMid meet"
      >
        <defs>
          <linearGradient id={`${uid}-sky`} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={`hsl(${skyHue} 52% ${Math.max(4, lightness)}%)`} />
            <stop offset="100%" stopColor={`hsl(${groundHue} 58% ${Math.max(2, lightness - 4)}%)`} />
          </linearGradient>
        </defs>
        {mode === "gradient" ? <rect width={WORLD_WIDTH} height={WORLD_HEIGHT} fill={`url(#${uid}-sky)`} /> : null}
        {mode === "solid" ? <rect width={WORLD_WIDTH} height={WORLD_HEIGHT} fill={`hsl(${skyHue} 52% ${lightness}%)`} /> : null}
        {showLinks ? (
          <g className="dw-exp-space-links" aria-hidden="true">
            {context.world.links.map((link) => {
              const from = nodeById.get(link.from);
              const to = nodeById.get(link.to);
              if (!from || !to || !viewByKind.has(from.kind_id) || !viewByKind.has(to.kind_id)) {
                return null;
              }
              const middleX = (from.x + to.x) / 2;
              return (
                <path
                  key={link.id}
                  d={
                    linkStyle === "line"
                      ? `M ${from.x} ${from.y} L ${to.x} ${to.y}`
                      : `M ${from.x} ${from.y} C ${middleX} ${from.y}, ${middleX} ${to.y}, ${to.x} ${to.y}`
                  }
                  fill="none"
                  stroke={linkColor}
                  strokeOpacity={0.22}
                  strokeWidth={1.5}
                />
              );
            })}
          </g>
        ) : null}
        {rendered.map((entity) => {
          const view = viewByKind.get(entity.kind_id)!;
          const visual = spaceVisual(view, entity);
          const color = `hsl(${visual.hue} 70% 62%)`;
          const behaviorID = entityBehaviors.get(entity.id) ?? activateBehaviorID;
          const interactive = Boolean(behaviorID) && context.canContribute && !context.busy;
          return (
            <g
              key={entity.id}
              className={`dw-exp-space-entity${view.motion && view.motion !== "none" ? ` scene-motion-${view.motion}` : ""}`}
              data-testid="world-experience-entity"
              data-motion={stringValue(view.motion, "")}
              data-entity-id={entity.id}
              data-kind-id={entity.kind_id}
              data-size={Math.round(visual.size)}
              data-opacity={visual.opacity.toFixed(2)}
              data-fill-hue={Math.round(visual.hue)}
              role={interactive ? "button" : "img"}
              tabIndex={interactive ? 0 : undefined}
              aria-label={entity.name}
              style={{ cursor: interactive ? "pointer" : "default", opacity: visual.opacity }}
              onClick={() => {
                if (interactive) {
                  context.onInvoke(behaviorID, entity.id);
                }
              }}
              onKeyDown={(event) => {
                if (interactive && (event.key === "Enter" || event.key === " ")) {
                  event.preventDefault();
                  context.onInvoke(behaviorID, entity.id);
                }
              }}
            >
              <circle cx={entity.x} cy={entity.y} r={visual.size * 0.62} fill={color} opacity={0.1} />
              <g transform={`translate(${entity.x} ${entity.y}) scale(${visual.size / 100})`}>
                <path
                  d={stringValue(view.path, "")}
                  fill={stringValue(view.paint, "fill") === "stroke" ? "none" : color}
                  stroke={color}
                  strokeWidth={stringValue(view.paint, "fill") === "stroke" ? 4 : 1.5}
                  strokeLinejoin="round"
                  strokeLinecap="round"
                />
              </g>
              {stringValue(view.label, "always") === "hidden" ? null : (
                <text
                  x={entity.x}
                  y={entity.y + visual.size * 0.62}
                  textAnchor="middle"
                  className={`dw-exp-space-label${stringValue(view.label, "always") === "hover" ? " is-hover" : ""}`}
                >
                  {entity.name}
                </text>
              )}
            </g>
          );
        })}
      </svg>
      {context.canContribute && activateBehaviorID ? (
        <p className="dw-exp-space-hint">
          {t("openWorld.spaceHint")}
        </p>
      ) : null}
    </div>
  );
}

type SpaceVisual = { size: number; opacity: number; hue: number };

function spaceVisual(view: Record<string, unknown>, entity: OpenWorldNode): SpaceVisual {
  const bindings = asArray(view.bindings).map(asRecord);
  let size = numberValue(view.size, 56);
  let opacity = numberValue(view.opacity, 0.9);
  let hue = numberValue(view.fill_hue, 198);
  for (const binding of bindings) {
    const target = stringValue(binding.target, "");
    const bound = bindingValue(binding, entity);
    if (bound === null) {
      continue;
    }
    if (target === "size") {
      size *= bound;
    } else if (target === "opacity") {
      opacity = bound;
    } else if (target === "hue") {
      hue += bound;
    }
  }
  return {
    size: Math.max(6, Math.min(600, size)),
    opacity: Math.max(0, Math.min(1, opacity)),
    hue: ((hue % 360) + 360) % 360
  };
}

function bindingValue(binding: Record<string, unknown>, entity: OpenWorldNode): number | null {
  const raw = entity.values[stringValue(binding.field, "")];
  const inMin = numberValue(binding.in_min, 0);
  const inMax = numberValue(binding.in_max, 1);
  const outMin = numberValue(binding.out_min, 0);
  const outMax = numberValue(binding.out_max, 1);
  if (inMax === inMin) {
    return outMin;
  }
  let ratio: number;
  if (typeof raw === "boolean") {
    ratio = raw ? 1 : 0;
  } else if (typeof raw === "number" && Number.isFinite(raw)) {
    ratio = Math.max(0, Math.min(1, (raw - inMin) / (inMax - inMin)));
  } else {
    return null;
  }
  return outMin + ratio * (outMax - outMin);
}

function layerOrder(layer: string): number {
  if (layer === "back") {
    return 0;
  }
  if (layer === "front") {
    return 2;
  }
  return 1;
}

function conditionHolds(condition: WorldCondition, world: OpenWorldState, scope: Scope): boolean {
  if (condition.not) {
    return !conditionHolds(condition.not, world, scope);
  }
  if (condition.all && condition.all.length > 0) {
    return condition.all.every((entry) => conditionHolds(entry, world, scope));
  }
  if (condition.any && condition.any.length > 0) {
    return condition.any.some((entry) => conditionHolds(entry, world, scope));
  }
  const entityID = condition.entity_id === "$item" ? scope.item?.id : condition.entity_id;
  if (!entityID) {
    return false;
  }
  const entity = world.nodes.find((candidate) => candidate.id === entityID);
  if (!entity) {
    return false;
  }
  if (condition.field === "status") {
    const target = condition.value;
    if (typeof target !== "string") {
      return false;
    }
    return condition.op === "eq" ? entity.status === target : condition.op === "ne" ? entity.status !== target : false;
  }
  const kind = world.kinds.find((candidate) => candidate.id === entity.kind_id);
  const field = kind?.fields.find((candidate) => candidate.key === condition.field);
  if (!field) {
    return false;
  }
  const current = entity.values[field.key];
  if (field.type === "flag") {
    if (condition.op === "truthy") {
      return current === true;
    }
    if (condition.op === "falsy") {
      return current !== true;
    }
    if (typeof condition.value !== "boolean") {
      return false;
    }
    return condition.op === "eq" ? current === condition.value : condition.op === "ne" ? current !== condition.value : false;
  }
  if (field.type === "number") {
    const left = typeof current === "number" ? current : 0;
    const right = typeof condition.value === "number" ? condition.value : Number.NaN;
    if (!Number.isFinite(right)) {
      return false;
    }
    switch (condition.op) {
      case "eq":
        return left === right;
      case "ne":
        return left !== right;
      case "gt":
        return left > right;
      case "gte":
        return left >= right;
      case "lt":
        return left < right;
      case "lte":
        return left <= right;
      default:
        return false;
    }
  }
  const left = typeof current === "string" ? current : "";
  const right = typeof condition.value === "string" ? condition.value : "";
  return condition.op === "eq" ? left === right : condition.op === "ne" ? left !== right : false;
}

function interpolate(text: string, world: OpenWorldState, scope: Scope): string {
  return text.replace(TEMPLATE_TOKEN, (_match, rawToken: string) => {
    const token = rawToken.trim();
    if (token === "world.title") {
      return world.title;
    }
    if (token === "world.premise") {
      return world.premise;
    }
    const parts = token.split(".");
    if (parts[0] === "item") {
      return scope.item ? entityTokenValue(scope.item, parts.slice(1)) : "";
    }
    if (parts[0] === "entity" && parts.length >= 3) {
      const entity = world.nodes.find((candidate) => candidate.id === parts[1]);
      return entity ? entityTokenValue(entity, parts.slice(2)) : "";
    }
    return "";
  });
}

function entityTokenValue(entity: OpenWorldNode, parts: string[]): string {
  if (parts.length === 1) {
    if (parts[0] === "name") {
      return entity.name;
    }
    if (parts[0] === "text") {
      return entity.text;
    }
    if (parts[0] === "status") {
      return entity.status;
    }
    return "";
  }
  if (parts.length === 2 && parts[0] === "field") {
    const key = parts[1];
    return key ? formatUnknown(entity.values[key]) : "";
  }
  return "";
}

function resolveEntityID(value: unknown, scope: Scope): string {
  if (value === "$item") {
    return scope.item?.id ?? "";
  }
  return typeof value === "string" ? value : "";
}

function labelForSource(
  kind: OpenWorldKind | undefined,
  source: string,
  t: (key: MessageKey) => string
): string {
  if (source === "name") {
    return t("openWorld.nodeName");
  }
  if (source === "text") {
    return t("openWorld.nodeText");
  }
  if (source === "status") {
    return t("openWorld.statusField");
  }
  return kind?.fields.find((field) => field.key === source)?.label || source;
}

function formatCell(entity: OpenWorldNode, source: string, kind?: OpenWorldKind): string {
  if (source === "name") {
    return entity.name;
  }
  if (source === "text") {
    return entity.text;
  }
  if (source === "status") {
    return entity.status;
  }
  const raw = entity.values[source];
  if (raw === undefined) {
    const field = kind?.fields.find((candidate) => candidate.key === source);
    if (field?.type === "number") {
      return "0";
    }
    if (field?.type === "flag") {
      return "off";
    }
    return "";
  }
  return formatUnknown(raw);
}

function formatUnknown(value: unknown): string {
  if (typeof value === "string") {
    return value;
  }
  if (typeof value === "number" && Number.isFinite(value)) {
    return formatNumber(value);
  }
  if (typeof value === "boolean") {
    return value ? "on" : "off";
  }
  return "";
}

function displayValue(value: unknown): string {
  if (typeof value === "number" && Number.isFinite(value)) {
    return String(value);
  }
  return formatUnknown(value);
}

function formatNumber(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(2);
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function asArray(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function stringValue(value: unknown, fallback: string): string {
  return typeof value === "string" ? value : fallback;
}

function numberValue(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}


function TabsNode({
  props,
  node,
  scope,
  context
}: {
  props: Record<string, unknown>;
  node: WorldViewNode;
  scope: Scope;
  context: ExperienceContext;
}) {
  const { t } = useI18n();
  const tabs = asArray(props.tabs).map(asRecord);
  const children = Array.isArray(node.children) ? node.children : [];
  const fallback = stringValue(props.default, tabs[0]?.child_id ? stringValue(tabs[0].child_id, "") : "");
  const [active, setActive] = useState<string>(fallback);
  const activeChild = children.find((child) => child.id === active) ?? children[0];
  return (
    <section className="dw-exp-tabs" data-testid="world-experience-tabs">
      <div className="dw-exp-tabbar" role="tablist" aria-label={t("openWorld.experience")}>
        {tabs.map((tab, index) => {
          const id = stringValue(tab.child_id, "");
          const label = stringValue(tab.label, "…");
          return (
            <button
              key={`${id}:${index}`}
              type="button"
              role="tab"
              aria-selected={id === (activeChild?.id ?? "")}
              className={`dw-exp-tab${id === (activeChild?.id ?? "") ? " is-active" : ""}`}
              onClick={() => setActive(id)}
            >
              {label}
            </button>
          );
        })}
      </div>
      <div className="dw-exp-tabpanel" role="tabpanel">
        {activeChild ? (
          <NodeView
            node={activeChild}
            scope={scope}
            context={context}
            itemKey={activeChild.id}
          />
        ) : null}
      </div>
    </section>
  );
}
