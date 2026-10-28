// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useRef, useState, type FormEvent, type PointerEvent as ReactPointerEvent } from "react";
import { Link, useParams } from "react-router";
import {
  applyAction,
  errorMessage,
  getObject,
  openWorldState,
  type ObjectView,
  type OpenWorldField,
  type OpenWorldKind,
  type OpenWorldNode,
  type OpenWorldState,
  type OpenWorldView
} from "../api";
import AIPanel from "../components/AIPanel";
import Icon from "../components/v2/Icon";
import { useI18n } from "../i18n";

const WORLD_WIDTH = 1600;
const WORLD_HEIGHT = 1000;

type DragState = {
  id: string;
  startX: number;
  startY: number;
  originX: number;
  originY: number;
  moved: boolean;
};

type ViewMode = "structure" | "ai";

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function kindFor(state: OpenWorldState, kindID: string): OpenWorldKind | undefined {
  return state.kinds.find((kind) => kind.id === kindID);
}

function fieldFor(state: OpenWorldState, node: OpenWorldNode, key: string): OpenWorldField | undefined {
  const kind = kindFor(state, node.kind_id);
  return kind?.fields.find((field) => field.key === key);
}

function kindColor(kind: OpenWorldKind | undefined): string {
  const hue = kind?.hue ?? 198;
  return `hsl(${hue} 62% 58%)`;
}

function valueLabel(value: unknown): string {
  if (typeof value === "boolean") {
    return value ? "on" : "off";
  }
  if (typeof value === "number") {
    return String(value);
  }
  return typeof value === "string" ? value : "";
}

export default function OpenWorldPage() {
  const { id = "" } = useParams();
  const { t, lang } = useI18n();
  const zh = lang === "zh";
  const [object, setObject] = useState<ObjectView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState("");
  const [view, setView] = useState<OpenWorldView>("stage");
  const [panel, setPanel] = useState<ViewMode>("structure");
  const [premiseEditing, setPremiseEditing] = useState(false);
  const [premiseDraft, setPremiseDraft] = useState("");
  const [drag, setDrag] = useState<{ id: string; x: number; y: number } | null>(null);
  const [dragging, setDragging] = useState(false);
  const dragRef = useRef<DragState | null>(null);
  const canvasRef = useRef<SVGSVGElement | null>(null);

  const load = useCallback(async () => {
    setError(null);
    try {
      const value = await getObject(id);
      setObject(value);
      setPremiseDraft(typeof value.state.premise === "string" ? value.state.premise : "");
    } catch (failure) {
      setError(failure);
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    void load();
  }, [load]);

  const world = object ? openWorldState(object) : null;
  const canContribute = object?.permissions.includes("contribute") ?? false;

  useEffect(() => {
    if (world && !world.views.includes(view) && world.views[0]) {
      setView(world.views[0]);
    }
  }, [world, view]);

  useEffect(() => {
    function onMove(event: PointerEvent) {
      const dragState = dragRef.current;
      const canvas = canvasRef.current;
      if (!dragState || !canvas) {
        return;
      }
      const rect = canvas.getBoundingClientRect();
      if (rect.width < 20 || rect.height < 20) {
        return;
      }
      const scaleX = WORLD_WIDTH / rect.width;
      const scaleY = WORLD_HEIGHT / rect.height;
      const dx = (event.clientX - dragState.startX) * scaleX;
      const dy = (event.clientY - dragState.startY) * scaleY;
      if (Math.abs(dx) + Math.abs(dy) > 6) {
        dragState.moved = true;
        setDragging(true);
      }
      if (dragState.moved) {
        setDrag({
          id: dragState.id,
          x: clamp(dragState.originX + dx, 40, WORLD_WIDTH - 40),
          y: clamp(dragState.originY + dy, 40, WORLD_HEIGHT - 60)
        });
      }
    }
    function onUp() {
      const dragState = dragRef.current;
      if (!dragState) {
        return;
      }
      dragRef.current = null;
      setDragging(false);
      const position = drag;
      setDrag(null);
      if (dragState.moved && position && position.id === dragState.id) {
        void run("move_node", { node_id: dragState.id, x: Math.round(position.x), y: Math.round(position.y) });
      } else if (!dragState.moved) {
        void run("touch_node", { node_id: dragState.id });
      }
    }
    function onCancel() {
      dragRef.current = null;
      setDragging(false);
      setDrag(null);
    }
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onCancel);
    return () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onCancel);
    };
  }, [drag]);

  async function run(action: string, input: Record<string, unknown>) {
    if (!object || busy) {
      return null;
    }
    setBusy(action);
    setError(null);
    setNotice("");
    try {
      const updated = await applyAction(object.id, action, input, object.version);
      setObject(updated);
      setNotice(action === "touch_node" ? t("openWorld.touched") : t("openWorld.saved"));
      return updated;
    } catch (failure) {
      setError(failure);
      await load();
      return null;
    } finally {
      setBusy("");
    }
  }

  async function savePremise() {
    if (!world || premiseDraft.trim() === world.premise) {
      setPremiseEditing(false);
      return;
    }
    const updated = await run("set_premise", { premise: premiseDraft });
    if (updated) {
      setPremiseEditing(false);
    }
  }

  function beginDrag(event: ReactPointerEvent<SVGGElement>, node: OpenWorldNode) {
    if (!canContribute || busy) {
      return;
    }
    event.stopPropagation();
    dragRef.current = {
      id: node.id,
      startX: event.clientX,
      startY: event.clientY,
      originX: node.x,
      originY: node.y,
      moved: false
    };
  }

  if (loading && !object) {
    return (
      <div className="dw-page">
        <p className="dw-page-loading" role="status">
          {t("common.loading")}
        </p>
      </div>
    );
  }

  if (!object || !world) {
    return (
      <div className="dw-page">
        <div className="dw-empty-state">
          <h1>{t("page.notFound.title")}</h1>
          <p>{errorMessage(error)}</p>
          <Link className="dw-button dw-button-primary" to="/">
            {t("openWorld.back")}
          </Link>
        </div>
      </div>
    );
  }

  if (object.object_type !== "open_world") {
    return (
      <div className="dw-page">
        <div className="dw-empty-state">
          <h1>{t("openWorld.kicker")}</h1>
          <p>{t("openWorld.unsupported")}</p>
          <Link className="dw-button dw-button-primary" to={`/objects/${object.id}`}>
            {t("common.open")}
          </Link>
        </div>
      </div>
    );
  }

  const nodeByID = new Map(world.nodes.map((node) => [node.id, node]));

  return (
    <div className="dw-page dw-open-world-page" data-testid="open-world-page">
      <header className="dw-open-header">
        <div className="dw-open-head-left">
          <Link className="dw-back-link" to="/">
            <Icon name="arrow" size={14} />
            {t("openWorld.back")}
          </Link>
          <p className="dw-eyebrow">
            <span className="dw-eyebrow-line" />
            {t("openWorld.kicker")}
          </p>
          <h1>{world.title || t("common.untitled")}</h1>
          <p className="dw-open-summary">
            {t("openWorld.summary", {
              kinds: world.kinds.length,
              nodes: world.nodes.length,
              rules: world.rules.length
            })}
          </p>
        </div>
        <div className="dw-open-head-actions">
          <button
            className={`dw-button dw-button-small ${panel === "structure" ? "dw-button-primary" : "dw-button-ghost"}`}
            type="button"
            onClick={() => setPanel("structure")}
          >
            <Icon name="layers" size={15} />
            {t("openWorld.structure")}
          </button>
          <button
            className={`dw-button dw-button-small ${panel === "ai" ? "dw-button-primary" : "dw-button-ghost"}`}
            type="button"
            onClick={() => setPanel("ai")}
          >
            <Icon name="spark" size={15} />
            {t("openWorld.ai")}
          </button>
          {object.permissions.includes("derive") ? (
            <Link className="dw-button dw-button-small dw-button-ghost" to={`/objects/${object.id}/derive`}>
              <Icon name="branch" size={15} />
              {t("stage.tab.branch")}
            </Link>
          ) : null}
        </div>
      </header>

      {error ? (
        <p className="dw-form-error" role="alert">
          {errorMessage(error)}
        </p>
      ) : null}
      {notice ? (
        <p className="dw-form-success" role="status">
          {notice}
        </p>
      ) : null}

      <div className="dw-open-layout">
        <main className="dw-open-main">
          <section className="dw-open-premise-card">
            {premiseEditing ? (
              <>
                <textarea
                  className="dw-input dw-textarea"
                  rows={3}
                  maxLength={2000}
                  value={premiseDraft}
                  onChange={(event) => setPremiseDraft(event.target.value)}
                  aria-label={t("openWorld.premiseEmpty")}
                  autoFocus
                />
                <div className="dw-inline-actions">
                  <button className="dw-button dw-button-small dw-button-primary" type="button" onClick={() => void savePremise()} disabled={busy !== ""}>
                    {t("common.save")}
                  </button>
                  <button className="dw-button dw-button-small dw-button-ghost" type="button" onClick={() => setPremiseEditing(false)}>
                    {t("common.cancel")}
                  </button>
                </div>
              </>
            ) : (
              <>
                <p>{world.premise || t("openWorld.premiseEmpty")}</p>
                {canContribute ? (
                  <button className="dw-text-button" type="button" onClick={() => setPremiseEditing(true)}>
                    {t("common.edit")}
                  </button>
                ) : null}
              </>
            )}
          </section>

          <div className="dw-open-viewbar" role="group" aria-label={t("openWorld.viewLabel")}>
            {world.views.map((mode) => (
              <button
                key={mode}
                type="button"
                className={`dw-open-view${view === mode ? " is-active" : ""}`}
                aria-pressed={view === mode}
                onClick={() => setView(mode)}
              >
                {t(`openWorld.view.${mode}` as never)}
              </button>
            ))}
            <span className="dw-open-kinds-legend">
              {world.kinds.map((kind) => (
                <span key={kind.id} className="dw-open-kind-chip" style={{ ["--kind-hue" as string]: `${kind.hue}` }}>
                  <i style={{ background: kindColor(kind) }} />
                  {kind.name}
                </span>
              ))}
            </span>
          </div>

          {world.nodes.length === 0 ? (
            <div className="dw-open-empty">
              <Icon name="world" size={26} />
              <p>{t("openWorld.empty")}</p>
              {canContribute ? (
                <button className="dw-button dw-button-primary dw-button-small" type="button" onClick={() => setPanel("structure")}>
                  {world.kinds.length === 0 ? t("openWorld.createFirstKind") : t("openWorld.createFirstThing")}
                </button>
              ) : null}
            </div>
          ) : view === "stage" ? (
            <div className={`dw-open-stage${dragging ? " is-dragging" : ""}`} data-testid="open-world-stage">
              <svg
                ref={canvasRef}
                viewBox={`0 0 ${WORLD_WIDTH} ${WORLD_HEIGHT}`}
                role="img"
                aria-label={t("openWorld.kicker")}
              >
                {world.links.map((link) => {
                  const from = nodeByID.get(link.from);
                  const to = nodeByID.get(link.to);
                  if (!from || !to) {
                    return null;
                  }
                  const midX = (from.x + to.x) / 2;
                  const midY = (from.y + to.y) / 2;
                  return (
                    <g key={link.id} className="dw-open-link">
                      <path d={`M ${from.x} ${from.y} C ${midX} ${from.y}, ${midX} ${to.y}, ${to.x} ${to.y}`} />
                      {link.label ? (
                        <text x={midX} y={midY - 8} textAnchor="middle">
                          {link.label}
                        </text>
                      ) : null}
                    </g>
                  );
                })}
                {world.nodes.map((node) => {
                  const kind = kindFor(world, node.kind_id);
                  const position = drag && drag.id === node.id ? drag : { x: node.x, y: node.y };
                  const color = kindColor(kind);
                  return (
                    <g
                      key={node.id}
                      className={`dw-open-node${node.status === "done" ? " is-done" : ""}`}
                      transform={`translate(${position.x} ${position.y})`}
                      onPointerDown={(event) => beginDrag(event, node)}
                      data-testid="open-world-node"
                      data-node-id={node.id}
                    >
                      <circle r={62} fill="rgba(10, 16, 26, 0.9)" stroke={color} strokeWidth={2.5} />
                      <circle r={6} cy={-30} fill={color} />
                      <text className="dw-open-node-title" textAnchor="middle" y={6}>
                        {node.name}
                      </text>
                      <text className="dw-open-node-kind" textAnchor="middle" y={30}>
                        {kind?.name ?? ""}
                        {node.status === "done" ? ` · ${t("openWorld.status.done")}` : ""}
                      </text>
                    </g>
                  );
                })}
              </svg>
              <p className="dw-open-stage-hint">
                {canContribute
                  ? zh
                    ? "触碰一个存在，让规则发生；拖动它，改变它在世界里的位置。"
                    : "Touch something to let its rules happen; drag it to change where it lives."
                  : t("workspace.permission.read")}
              </p>
            </div>
          ) : view === "cards" ? (
            <div className="dw-open-cards" data-testid="open-world-cards">
              {world.kinds.map((kind) => {
                const nodes = world.nodes.filter((node) => node.kind_id === kind.id);
                if (nodes.length === 0) {
                  return null;
                }
                return (
                  <section key={kind.id} className="dw-open-kind-section">
                    <h2>
                      <i style={{ background: kindColor(kind) }} />
                      {kind.name}
                    </h2>
                    <div className="dw-open-card-grid">
                      {nodes.map((node) => (
                        <NodeCard
                          key={node.id}
                          world={world}
                          node={node}
                          canContribute={canContribute}
                          busy={busy !== ""}
                          onTouch={() => void run("touch_node", { node_id: node.id })}
                          onField={(field, value) => void run("set_node_field", { node_id: node.id, field, value })}
                          onRemove={() => void run("remove_node", { node_id: node.id })}
                        />
                      ))}
                    </div>
                  </section>
                );
              })}
            </div>
          ) : (
            <ol className="dw-open-timeline" data-testid="open-world-timeline">
              {world.nodes.map((node, index) => {
                const kind = kindFor(world, node.kind_id);
                return (
                  <li key={node.id} className={node.status === "done" ? "is-done" : ""}>
                    <span className="dw-open-timeline-index">{index + 1}</span>
                    <div>
                      <p className="dw-open-timeline-kind" style={{ color: kindColor(kind) }}>
                        {kind?.name ?? ""}
                      </p>
                      <strong>{node.name}</strong>
                      {node.text ? <p>{node.text}</p> : null}
                      <Values world={world} node={node} />
                    </div>
                    <button
                      className="dw-button dw-button-small dw-button-ghost"
                      type="button"
                      disabled={!canContribute || busy !== ""}
                      onClick={() => void run("touch_node", { node_id: node.id })}
                    >
                      {node.status === "done" ? t("openWorld.status.done") : t("openWorld.touch")}
                    </button>
                  </li>
                );
              })}
            </ol>
          )}
        </main>

        <aside className="dw-open-panel">
          {panel === "ai" ? (
            <AIPanel object={object} canContribute={canContribute} onChanged={() => void load()} />
          ) : (
            <StructurePanel
              world={world}
              busy={busy !== ""}
              canContribute={canContribute}
              onAddKind={(input) => void run("add_kind", input)}
              onUpdateKind={(input) => void run("update_kind", input)}
              onRemoveKind={(kindID) => void run("remove_kind", { kind_id: kindID })}
              onAddNode={(input) => void run("add_node", input)}
              onRemoveNode={(nodeID) => void run("remove_node", { node_id: nodeID })}
              onAddLink={(input) => void run("add_link", input)}
              onRemoveLink={(linkID) => void run("remove_link", { link_id: linkID })}
              onAddRule={(input) => void run("add_rule", input)}
              onRemoveRule={(ruleID) => void run("remove_rule", { rule_id: ruleID })}
            />
          )}
        </aside>
      </div>
    </div>
  );
}

function Values({ world, node }: { world: OpenWorldState; node: OpenWorldNode }) {
  const entries = Object.entries(node.values);
  if (entries.length === 0) {
    return null;
  }
  return (
    <dl className="dw-open-values">
      {entries.map(([key, value]) => {
        const field = fieldFor(world, node, key);
        return (
          <div key={key}>
            <dt>{field?.label || key}</dt>
            <dd>{valueLabel(value)}</dd>
          </div>
        );
      })}
    </dl>
  );
}

function NodeCard({
  world,
  node,
  canContribute,
  busy,
  onTouch,
  onField,
  onRemove
}: {
  world: OpenWorldState;
  node: OpenWorldNode;
  canContribute: boolean;
  busy: boolean;
  onTouch: () => void;
  onField: (field: string, value: string | number | boolean) => void;
  onRemove: () => void;
}) {
  const { t } = useI18n();
  const kind = kindFor(world, node.kind_id);
  const fields = kind?.fields ?? [];
  return (
    <article className={`dw-open-card${node.status === "done" ? " is-done" : ""}`} data-testid="open-world-card">
      <header>
        <span className="dw-open-card-kind" style={{ color: kindColor(kind) }}>
          {kind?.name ?? ""}
        </span>
        <button
          className="dw-icon-button"
          type="button"
          disabled={!canContribute || busy}
          aria-label={canContribute ? t("openWorld.touch") : t("workspace.permission.read")}
          onClick={onTouch}
        >
          <Icon name={node.status === "done" ? "check" : "spark"} size={14} />
        </button>
      </header>
      <strong>{node.name}</strong>
      {node.text ? <p>{node.text}</p> : null}
      {fields.length > 0 ? (
        <div className="dw-open-card-fields">
          {fields.map((field) => (
            <FieldControl
              key={field.key}
              field={field}
              value={node.values[field.key]}
              disabled={!canContribute || busy}
              onCommit={(value) => onField(field.key, value)}
            />
          ))}
        </div>
      ) : null}
      {canContribute ? (
        <button className="dw-text-button dw-open-remove" type="button" disabled={busy} onClick={onRemove}>
          {t("openWorld.remove")}
        </button>
      ) : null}
    </article>
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
  const [draft, setDraft] = useState(valueLabel(value));
  useEffect(() => {
    setDraft(valueLabel(value));
  }, [value]);
  if (field.type === "flag") {
    return (
      <label className="dw-open-field">
        <span>{field.label}</span>
        <button
          type="button"
          className={`dw-open-flag${value === true ? " is-on" : ""}`}
          aria-pressed={value === true}
          disabled={disabled}
          onClick={() => onCommit(value !== true)}
        >
          {value === true ? t("openWorld.status.done") : t("openWorld.status.open")}
        </button>
      </label>
    );
  }
  return (
    <form
      className="dw-open-field"
      onSubmit={(event) => {
        event.preventDefault();
        if (field.type === "number") {
          const parsed = Number(draft);
          if (!Number.isFinite(parsed)) {
            return;
          }
          onCommit(parsed);
          return;
        }
        onCommit(draft);
      }}
    >
      <span>{field.label}</span>
      <span className="dw-open-field-row">
        <input
          className="dw-input"
          type={field.type === "number" ? "number" : "text"}
          value={draft}
          disabled={disabled}
          onChange={(event) => setDraft(event.target.value)}
        />
        <button className="dw-icon-button" type="submit" disabled={disabled} aria-label={t("common.save")}>
          <Icon name="check" size={14} />
        </button>
      </span>
    </form>
  );
}

function StructurePanel({
  world,
  busy,
  canContribute,
  onAddKind,
  onUpdateKind,
  onRemoveKind,
  onAddNode,
  onRemoveNode,
  onAddLink,
  onRemoveLink,
  onAddRule,
  onRemoveRule
}: {
  world: OpenWorldState;
  busy: boolean;
  canContribute: boolean;
  onAddKind: (input: Record<string, unknown>) => void;
  onUpdateKind: (input: Record<string, unknown>) => void;
  onRemoveKind: (kindID: string) => void;
  onAddNode: (input: Record<string, unknown>) => void;
  onRemoveNode: (nodeID: string) => void;
  onAddLink: (input: Record<string, unknown>) => void;
  onRemoveLink: (linkID: string) => void;
  onAddRule: (input: Record<string, unknown>) => void;
  onRemoveRule: (ruleID: string) => void;
}) {
  const { t } = useI18n();
  const [kindName, setKindName] = useState("");
  const [kindHue, setKindHue] = useState(168);
  const [fields, setFields] = useState<Array<{ key: string; label: string; type: "text" | "number" | "flag" }>>([]);
  const [fieldKey, setFieldKey] = useState("");
  const [fieldLabel, setFieldLabel] = useState("");
  const [fieldType, setFieldType] = useState<"text" | "number" | "flag">("text");
  const [nodeKind, setNodeKind] = useState("");
  const [nodeName, setNodeName] = useState("");
  const [nodeText, setNodeText] = useState("");
  const [linkFrom, setLinkFrom] = useState("");
  const [linkTo, setLinkTo] = useState("");
  const [linkLabel, setLinkLabel] = useState("");
  const [ruleTrigger, setRuleTrigger] = useState("");
  const [effectTarget, setEffectTarget] = useState("");
  const [effectField, setEffectField] = useState("status");
  const [effectOp, setEffectOp] = useState<"set" | "add" | "toggle">("set");
  const [effectValue, setEffectValue] = useState("");
  const [kindFieldDrafts, setKindFieldDrafts] = useState<Record<string, { key: string; label: string; type: "text" | "number" | "flag" }>>({});

  const firstKind = world.kinds[0]?.id ?? "";
  useEffect(() => {
    if (!nodeKind && firstKind) {
      setNodeKind(firstKind);
    }
    if (!ruleTrigger && world.nodes[0]) {
      setRuleTrigger(world.nodes[0].id);
    }
    if (!effectTarget && world.nodes[0]) {
      setEffectTarget(world.nodes[0].id);
    }
    if (!linkFrom && world.nodes[0]) {
      setLinkFrom(world.nodes[0].id);
    }
    if (!linkTo && world.nodes[1]) {
      setLinkTo(world.nodes[1].id);
    }
  }, [firstKind, nodeKind, ruleTrigger, effectTarget, linkFrom, linkTo, world.nodes]);

  const targetNode = world.nodes.find((node) => node.id === effectTarget);
  const targetKind = targetNode ? kindFor(world, targetNode.kind_id) : undefined;
  const targetField = effectField === "status"
    ? undefined
    : targetKind?.fields.find((field) => field.key === effectField);
  const opOptions = effectField === "status"
    ? (["set", "toggle"] as const)
    : targetField?.type === "number"
      ? (["set", "add"] as const)
      : targetField?.type === "flag"
        ? (["set", "toggle"] as const)
        : (["set"] as const);

  useEffect(() => {
    if (!opOptions.includes(effectOp as never)) {
      setEffectOp(opOptions[0] as "set" | "add" | "toggle");
    }
  }, [opOptions, effectOp]);

  function addDraftField() {
    const key = fieldKey.trim();
    if (!key) {
      return;
    }
    if (fields.some((field) => field.key === key)) {
      return;
    }
    setFields((current) => [...current, { key, label: fieldLabel.trim() || key, type: fieldType }]);
    setFieldKey("");
    setFieldLabel("");
  }

  function submitKind(event: FormEvent) {
    event.preventDefault();
    if (!kindName.trim()) {
      return;
    }
    onAddKind({ name: kindName.trim(), hue: kindHue, fields });
    setKindName("");
    setKindHue(168);
    setFields([]);
  }

  function submitNode(event: FormEvent) {
    event.preventDefault();
    if (!nodeKind || !nodeName.trim()) {
      return;
    }
    onAddNode({ kind_id: nodeKind, name: nodeName.trim(), text: nodeText });
    setNodeName("");
    setNodeText("");
  }

  function submitLink(event: FormEvent) {
    event.preventDefault();
    if (!linkFrom || !linkTo || linkFrom === linkTo) {
      return;
    }
    onAddLink({ from: linkFrom, to: linkTo, label: linkLabel });
    setLinkLabel("");
  }

  function submitRule(event: FormEvent) {
    event.preventDefault();
    if (!ruleTrigger || !effectTarget) {
      return;
    }
    if (effectField !== "status" && !targetKind?.fields.some((field) => field.key === effectField)) {
      return;
    }
    let value: string | number | boolean | undefined;
    if (effectField === "status") {
      if (effectOp === "set") {
        value = effectValue === "done" ? "done" : "open";
      }
    } else if (targetField?.type === "number") {
      const parsed = Number(effectValue);
      if (!Number.isFinite(parsed)) {
        return;
      }
      value = parsed;
    } else if (targetField?.type === "flag") {
      if (effectOp === "set") {
        value = effectValue === "true";
      }
    } else {
      value = effectValue;
    }
    const effect: Record<string, unknown> = {
      node_id: effectTarget,
      field: effectField,
      op: effectOp
    };
    if (value !== undefined) {
      effect.value = value;
    }
    onAddRule({ trigger_id: ruleTrigger, effects: [effect] });
  }

  const effectFields = targetKind?.fields ?? [];

  return (
    <div className="dw-open-structure" data-testid="open-world-structure">
      <section>
        <h2>{t("openWorld.kinds")}</h2>
        <p className="dw-open-hint">{t("openWorld.kindsHint")}</p>
        <ul className="dw-open-structure-list">
          {world.kinds.map((kind) => (
            <li key={kind.id}>
              <span className="dw-open-kind-dot" style={{ background: kindColor(kind) }} />
              <div>
                <strong>{kind.name}</strong>
                <small>{kind.fields.map((field) => `${field.label || field.key}:${t(`openWorld.field.${field.type}` as never)}`).join(" · ") || "—"}</small>
              </div>
              {canContribute ? (
                <button className="dw-icon-button" type="button" disabled={busy} aria-label={t("openWorld.remove")} onClick={() => onRemoveKind(kind.id)}>
                  <Icon name="trash" size={13} />
                </button>
              ) : null}
            </li>
          ))}
        </ul>
        {canContribute ? (
          <details className="dw-open-add">
            <summary>{t("openWorld.addKind")}</summary>
            <form onSubmit={submitKind}>
              <label className="dw-field-label" htmlFor="ow-kind-name">{t("openWorld.kindName")}</label>
              <input id="ow-kind-name" className="dw-input" value={kindName} maxLength={60} onChange={(event) => setKindName(event.target.value)} />
              <label className="dw-field-label" htmlFor="ow-kind-hue">
                {t("openWorld.hue")} · {kindHue}
              </label>
              <input id="ow-kind-hue" type="range" min={0} max={359} value={kindHue} onChange={(event) => setKindHue(Number(event.target.value))} />
              <div className="dw-open-field-editor">
                {fields.map((field) => (
                  <span key={field.key} className="dw-open-field-chip">
                    {field.label} · {t(`openWorld.field.${field.type}` as never)}
                    <button type="button" onClick={() => setFields((current) => current.filter((entry) => entry.key !== field.key))} aria-label={t("openWorld.remove")}>
                      ×
                    </button>
                  </span>
                ))}
              </div>
              <div className="dw-open-inline-form">
                <input className="dw-input" placeholder={t("openWorld.fieldKey")} value={fieldKey} maxLength={40} onChange={(event) => setFieldKey(event.target.value)} />
                <input className="dw-input" placeholder={t("openWorld.fieldLabel")} value={fieldLabel} maxLength={80} onChange={(event) => setFieldLabel(event.target.value)} />
                <select className="dw-input" value={fieldType} onChange={(event) => setFieldType(event.target.value as "text" | "number" | "flag")}>
                  <option value="text">{t("openWorld.field.text")}</option>
                  <option value="number">{t("openWorld.field.number")}</option>
                  <option value="flag">{t("openWorld.field.flag")}</option>
                </select>
                <button className="dw-button dw-button-small dw-button-ghost" type="button" onClick={addDraftField}>
                  {t("openWorld.addField")}
                </button>
              </div>
              <button className="dw-button dw-button-small dw-button-primary" type="submit" disabled={busy || !kindName.trim()}>
                {t("openWorld.addKind")}
              </button>
            </form>
          </details>
        ) : null}
        {canContribute && world.kinds.length > 0 ? (
          <div className="dw-open-kind-field-add">
            {world.kinds.map((kind) => (
              <details key={kind.id}>
                <summary>{kind.name} · {t("openWorld.addField")}</summary>
                <form
                  className="dw-open-inline-form"
                  onSubmit={(event) => {
                    event.preventDefault();
                    const draft = kindFieldDrafts[kind.id];
                    if (!draft || !draft.key.trim()) {
                      return;
                    }
                    onUpdateKind({
                      kind_id: kind.id,
                      fields: [...kind.fields, { key: draft.key.trim(), label: draft.label.trim() || draft.key.trim(), type: draft.type }]
                    });
                    setKindFieldDrafts((current) => ({ ...current, [kind.id]: { key: "", label: "", type: "text" } }));
                  }}
                >
                  <input
                    className="dw-input"
                    placeholder={t("openWorld.fieldKey")}
                    value={kindFieldDrafts[kind.id]?.key ?? ""}
                    onChange={(event) => setKindFieldDrafts((current) => ({ ...current, [kind.id]: { key: event.target.value, label: current[kind.id]?.label ?? "", type: current[kind.id]?.type ?? "text" } }))}
                  />
                  <input
                    className="dw-input"
                    placeholder={t("openWorld.fieldLabel")}
                    value={kindFieldDrafts[kind.id]?.label ?? ""}
                    onChange={(event) => setKindFieldDrafts((current) => ({ ...current, [kind.id]: { key: current[kind.id]?.key ?? "", label: event.target.value, type: current[kind.id]?.type ?? "text" } }))}
                  />
                  <select
                    className="dw-input"
                    value={kindFieldDrafts[kind.id]?.type ?? "text"}
                    onChange={(event) => setKindFieldDrafts((current) => ({ ...current, [kind.id]: { key: current[kind.id]?.key ?? "", label: current[kind.id]?.label ?? "", type: event.target.value as "text" | "number" | "flag" } }))}
                  >
                    <option value="text">{t("openWorld.field.text")}</option>
                    <option value="number">{t("openWorld.field.number")}</option>
                    <option value="flag">{t("openWorld.field.flag")}</option>
                  </select>
                  <button className="dw-button dw-button-small dw-button-ghost" type="submit" disabled={busy}>
                    {t("common.save")}
                  </button>
                </form>
              </details>
            ))}
          </div>
        ) : null}
      </section>

      <section>
        <h2>{t("openWorld.nodes")}</h2>
        <ul className="dw-open-structure-list">
          {world.nodes.map((node) => (
            <li key={node.id}>
              <span className="dw-open-kind-dot" style={{ background: kindColor(kindFor(world, node.kind_id)) }} />
              <div>
                <strong>{node.name}</strong>
                <small>{kindFor(world, node.kind_id)?.name ?? ""} · {node.status === "done" ? t("openWorld.status.done") : t("openWorld.status.open")}</small>
              </div>
              {canContribute ? (
                <button className="dw-icon-button" type="button" disabled={busy} aria-label={t("openWorld.remove")} onClick={() => onRemoveNode(node.id)}>
                  <Icon name="trash" size={13} />
                </button>
              ) : null}
            </li>
          ))}
        </ul>
        {canContribute && world.kinds.length > 0 ? (
          <details className="dw-open-add">
            <summary>{t("openWorld.addNode")}</summary>
            <form onSubmit={submitNode}>
              <label className="dw-field-label" htmlFor="ow-node-kind">{t("openWorld.nodeKind")}</label>
              <select id="ow-node-kind" className="dw-input" value={nodeKind} onChange={(event) => setNodeKind(event.target.value)}>
                {world.kinds.map((kind) => (
                  <option key={kind.id} value={kind.id}>{kind.name}</option>
                ))}
              </select>
              <label className="dw-field-label" htmlFor="ow-node-name">{t("openWorld.nodeName")}</label>
              <input id="ow-node-name" className="dw-input" value={nodeName} maxLength={120} onChange={(event) => setNodeName(event.target.value)} />
              <label className="dw-field-label" htmlFor="ow-node-text">{t("openWorld.nodeText")}</label>
              <textarea id="ow-node-text" className="dw-input dw-textarea" rows={2} value={nodeText} maxLength={1200} onChange={(event) => setNodeText(event.target.value)} />
              <button className="dw-button dw-button-small dw-button-primary" type="submit" disabled={busy || !nodeName.trim()}>
                {t("openWorld.addNode")}
              </button>
            </form>
          </details>
        ) : null}
      </section>

      <section>
        <h2>{t("openWorld.links")}</h2>
        <ul className="dw-open-structure-list">
          {world.links.map((link) => (
            <li key={link.id}>
              <Icon name="link" size={14} />
              <div>
                <strong>
                  {world.nodes.find((node) => node.id === link.from)?.name ?? "?"} → {world.nodes.find((node) => node.id === link.to)?.name ?? "?"}
                </strong>
                <small>{link.label || "—"}</small>
              </div>
              {canContribute ? (
                <button className="dw-icon-button" type="button" disabled={busy} aria-label={t("openWorld.remove")} onClick={() => onRemoveLink(link.id)}>
                  <Icon name="trash" size={13} />
                </button>
              ) : null}
            </li>
          ))}
        </ul>
        {canContribute && world.nodes.length > 1 ? (
          <details className="dw-open-add">
            <summary>{t("openWorld.addLink")}</summary>
            <form onSubmit={submitLink}>
              <label className="dw-field-label" htmlFor="ow-link-from">{t("openWorld.linkFrom")}</label>
              <select id="ow-link-from" className="dw-input" value={linkFrom} onChange={(event) => setLinkFrom(event.target.value)}>
                {world.nodes.map((node) => (
                  <option key={node.id} value={node.id}>{node.name}</option>
                ))}
              </select>
              <label className="dw-field-label" htmlFor="ow-link-to">{t("openWorld.linkTo")}</label>
              <select id="ow-link-to" className="dw-input" value={linkTo} onChange={(event) => setLinkTo(event.target.value)}>
                {world.nodes.map((node) => (
                  <option key={node.id} value={node.id}>{node.name}</option>
                ))}
              </select>
              <label className="dw-field-label" htmlFor="ow-link-label">{t("openWorld.linkLabel")}</label>
              <input id="ow-link-label" className="dw-input" value={linkLabel} maxLength={80} onChange={(event) => setLinkLabel(event.target.value)} />
              <button className="dw-button dw-button-small dw-button-primary" type="submit" disabled={busy || linkFrom === linkTo}>
                {t("openWorld.addLink")}
              </button>
            </form>
          </details>
        ) : null}
      </section>

      <section>
        <h2>{t("openWorld.rules")}</h2>
        <p className="dw-open-hint">{t("openWorld.rulesHint")}</p>
        <ul className="dw-open-structure-list">
          {world.rules.map((rule) => (
            <li key={rule.id}>
              <Icon name="branch" size={14} />
              <div>
                <strong>
                  {world.nodes.find((node) => node.id === rule.trigger_id)?.name ?? "?"}
                </strong>
                <small>
                  {rule.effects
                    .map((effect) => `${world.nodes.find((node) => node.id === effect.node_id)?.name ?? "?"} · ${effect.field} ${effect.op}${effect.value !== undefined ? ` ${valueLabel(effect.value)}` : ""}`)
                    .join(" / ")}
                </small>
              </div>
              {canContribute ? (
                <button className="dw-icon-button" type="button" disabled={busy} aria-label={t("openWorld.remove")} onClick={() => onRemoveRule(rule.id)}>
                  <Icon name="trash" size={13} />
                </button>
              ) : null}
            </li>
          ))}
        </ul>
        {canContribute && world.nodes.length > 0 ? (
          <details className="dw-open-add">
            <summary>{t("openWorld.addRule")}</summary>
            <form onSubmit={submitRule}>
              <label className="dw-field-label" htmlFor="ow-rule-trigger">{t("openWorld.ruleTrigger")}</label>
              <select id="ow-rule-trigger" className="dw-input" value={ruleTrigger} onChange={(event) => setRuleTrigger(event.target.value)}>
                {world.nodes.map((node) => (
                  <option key={node.id} value={node.id}>{node.name}</option>
                ))}
              </select>
              <label className="dw-field-label" htmlFor="ow-effect-target">{t("openWorld.effectTarget")}</label>
              <select
                id="ow-effect-target"
                className="dw-input"
                value={effectTarget}
                onChange={(event) => {
                  setEffectTarget(event.target.value);
                  setEffectField("status");
                  setEffectValue("");
                }}
              >
                {world.nodes.map((node) => (
                  <option key={node.id} value={node.id}>{node.name}</option>
                ))}
              </select>
              <label className="dw-field-label" htmlFor="ow-effect-field">{t("openWorld.effectField")}</label>
              <select id="ow-effect-field" className="dw-input" value={effectField} onChange={(event) => setEffectField(event.target.value)}>
                <option value="status">{t("openWorld.statusField")}</option>
                {effectFields.map((field) => (
                  <option key={field.key} value={field.key}>{field.label || field.key}</option>
                ))}
              </select>
              <label className="dw-field-label" htmlFor="ow-effect-op">{t("openWorld.effectOp")}</label>
              <select id="ow-effect-op" className="dw-input" value={effectOp} onChange={(event) => setEffectOp(event.target.value as "set" | "add" | "toggle")}>
                {opOptions.map((op) => (
                  <option key={op} value={op}>{t(`openWorld.op.${op}` as never)}</option>
                ))}
              </select>
              {effectOp !== "toggle" ? (
                <>
                  <label className="dw-field-label" htmlFor="ow-effect-value">{t("openWorld.effectValue")}</label>
                  {effectField === "status" ? (
                    <select id="ow-effect-value" className="dw-input" value={effectValue} onChange={(event) => setEffectValue(event.target.value)}>
                      <option value="done">{t("openWorld.status.done")}</option>
                      <option value="open">{t("openWorld.status.open")}</option>
                    </select>
                  ) : targetField?.type === "flag" ? (
                    <select id="ow-effect-value" className="dw-input" value={effectValue} onChange={(event) => setEffectValue(event.target.value)}>
                      <option value="true">{t("openWorld.status.done")}</option>
                      <option value="false">{t("openWorld.status.open")}</option>
                    </select>
                  ) : (
                    <input
                      id="ow-effect-value"
                      className="dw-input"
                      type={targetField?.type === "number" ? "number" : "text"}
                      value={effectValue}
                      onChange={(event) => setEffectValue(event.target.value)}
                    />
                  )}
                </>
              ) : null}
              <button className="dw-button dw-button-small dw-button-primary" type="submit" disabled={busy || !ruleTrigger || !effectTarget}>
                {t("openWorld.addRule")}
              </button>
            </form>
          </details>
        ) : null}
      </section>

      <p className="dw-fineprint">
        <Icon name="shield" size={13} />
        {t("openWorld.unsupported")}
      </p>
    </div>
  );
}
