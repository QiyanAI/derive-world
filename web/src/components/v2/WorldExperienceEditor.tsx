// SPDX-License-Identifier: Apache-2.0
import { useState } from "react";
import type {
  LocalizedLabel,
  OpenWorldKind,
  OpenWorldState,
  WorldBehavior,
  WorldCapabilityAction,
  WorldCapabilityDescriptor,
  WorldCapabilityProperty,
  WorldCapabilitySchema,
  WorldCondition,
  WorldExperience,
  WorldViewNode
} from "../../api";
import { useI18n } from "../../i18n";

const DEFAULT_PATH = "M 0 -40 A 40 40 0 1 1 0 40 A 40 40 0 1 1 0 -40 Z";
const NODE_ID_PATTERN = /^[a-z][a-z0-9_-]{0,63}$/;

type EditorProps = {
  world: OpenWorldState;
  catalog: WorldCapabilityDescriptor[];
  busy: boolean;
  canContribute: boolean;
  onSave: (experience: WorldExperience | null) => void;
};

export default function WorldExperienceEditor({
  world,
  catalog,
  busy,
  canContribute,
  onSave
}: EditorProps) {
  const { t, lang } = useI18n();
  const zh = lang === "zh";
  const [draft, setDraft] = useState<WorldExperience | undefined>(world.experience);
  const [selectedID, setSelectedID] = useState(world.experience?.root.id ?? "root");
  const [localError, setLocalError] = useState("");

  const presentations = catalog.filter((capability) => capability.kind === "presentation");
  const behaviorsCapabilities = catalog.filter((capability) => capability.kind === "behavior");
  const behaviors = draft?.behaviors ?? [];
  const selectedNode = draft ? findNode(draft.root, selectedID) ?? draft.root : undefined;
  const selectedCapability = selectedNode
    ? presentations.find((capability) => capability.id === selectedNode.capability)
    : undefined;
  let addTarget: WorldViewNode | undefined;
  if (draft) {
    if (selectedNode && selectedCapability?.allows_children) {
      addTarget = selectedNode;
    } else {
      const parent = findParent(draft.root, selectedNode?.id ?? draft.root.id);
      if (parent && presentations.find((capability) => capability.id === parent.capability)?.allows_children) {
        addTarget = parent;
      }
    }
  }

  function updateDraft(next: WorldExperience) {
    setDraft(next);
    setLocalError("");
  }

  function updateSelectedNode(transform: (node: WorldViewNode) => WorldViewNode) {
    if (!draft) {
      return;
    }
    updateDraft({ ...draft, root: updateNode(draft.root, selectedNode?.id ?? draft.root.id, transform) });
  }

  function addCapability(capability: WorldCapabilityDescriptor) {
    if (!draft || !addTarget) {
      return;
    }
    const child = makeNode(capability, world, behaviors);
    updateDraft({
      ...draft,
      root: updateNode(draft.root, addTarget.id, (target) => ({
        ...target,
        children: [...(target.children ?? []), child]
      }))
    });
    setSelectedID(child.id);
  }

  function removeSelected() {
    if (!draft || !selectedNode || selectedNode.id === draft.root.id) {
      return;
    }
    const parent = findParent(draft.root, selectedNode.id);
    const nextRoot = removeNode(draft.root, selectedNode.id);
    if (!nextRoot) {
      return;
    }
    updateDraft({ ...draft, root: nextRoot });
    setSelectedID(parent?.id ?? draft.root.id);
  }

  function moveSelected(delta: number) {
    if (!draft || !selectedNode || selectedNode.id === draft.root.id) {
      return;
    }
    updateDraft({ ...draft, root: moveNode(draft.root, selectedNode.id, delta) });
  }

  function startScaffold(kind: "blank" | "text" | "space" | "workspace" | "composite") {
    const next = makeScaffold(kind, world, behaviors, zh);
    setDraft(next);
    setSelectedID(next.root.id);
    setLocalError("");
  }

  function save() {
    if (!draft) {
      onSave(null);
      return;
    }

    if (!draft.root || !NODE_ID_PATTERN.test(draft.root.id)) {
      setLocalError(zh ? "体验需要一个有效的根节点。" : "The experience needs a valid root node.");
      return;
    }
    onSave(draft);
  }

  if (!canContribute) {
    return (
      <p className="dw-open-hint" data-testid="world-experience-readonly">
        {t("workspace.permission.read")}
      </p>
    );
  }

  if (!draft) {
    return (
      <section className="dw-experience-editor" data-testid="world-experience-editor">
        <h2>{t("openWorld.experience")}</h2>
        <p className="dw-open-hint">{t("openWorld.experienceEmpty")}</p>
        <p className="dw-open-hint">{t("openWorld.startHint")}</p>
        <div className="dw-exp-scaffolds">
          {(["blank", "text", "space", "workspace", "composite"] as const).map((kind) => (
            <button
              key={kind}
              type="button"
              className="dw-button dw-button-small dw-button-ghost"
              onClick={() => startScaffold(kind)}
              data-testid={`world-experience-scaffold-${kind}`}
            >
              {t(`openWorld.scaffold.${kind}` as never)}
            </button>
          ))}
        </div>
      </section>
    );
  }

  return (
    <section className="dw-experience-editor" data-testid="world-experience-editor">
      <div className="dw-exp-editor-head">
        <div>
          <h2>{t("openWorld.experience")}</h2>
          <p className="dw-open-hint">{t("openWorld.experienceHint")}</p>
        </div>
        <span className="dw-exp-draft-badge">{t("openWorld.draft")}</span>
      </div>

      <div className="dw-exp-tree" data-testid="world-experience-tree">
        <TreeNode
          node={draft.root}
          depth={0}
          selectedID={selectedNode?.id ?? ""}
          presentations={presentations}
          onSelect={setSelectedID}
        />
      </div>

      {selectedNode && selectedCapability ? (
        <div className="dw-exp-inspector" data-testid="world-experience-inspector">
          <div className="dw-exp-inspector-head">
            <strong>{localized(selectedCapability.title, lang)}</strong>
            <code>{selectedNode.capability}</code>
          </div>
          <div className="dw-exp-node-actions">
            <button
              type="button"
              className="dw-button dw-button-small dw-button-ghost"
              onClick={() => moveSelected(-1)}
              disabled={selectedNode.id === draft.root.id}
            >
              {t("openWorld.moveUp")}
            </button>
            <button
              type="button"
              className="dw-button dw-button-small dw-button-ghost"
              onClick={() => moveSelected(1)}
              disabled={selectedNode.id === draft.root.id}
            >
              {t("openWorld.moveDown")}
            </button>
            <button
              type="button"
              className="dw-button dw-button-small dw-button-ghost"
              onClick={removeSelected}
              disabled={selectedNode.id === draft.root.id}
            >
              {t("openWorld.remove")}
            </button>
          </div>

          {selectedCapability.params ? (
            <SchemaFields
              schema={selectedCapability.params}
              value={selectedNode.props}
              onChange={(props) => updateSelectedNode((node) => ({ ...node, props }))}
              world={world}
              behaviors={behaviors}
              contextProps={selectedNode.props}
            />
          ) : (
            <p className="dw-open-hint">{t("openWorld.noProps")}</p>
          )}

          <details className="dw-exp-add" open={Boolean(addTarget)}>
            <summary>{t("openWorld.addCapability")}</summary>
            {addTarget ? (
              <CapabilityPalette
                capabilities={presentations}
                world={world}
                behaviors={behaviors}
                lang={lang}
                onAdd={addCapability}
              />
            ) : (
              <p className="dw-open-hint">{t("openWorld.selectContainer")}</p>
            )}
          </details>
        </div>
      ) : null}

      <BehaviorsEditor
        behaviors={behaviors}
        capabilities={behaviorsCapabilities}
        world={world}
        lang={lang}
        onChange={(next) => updateDraft({ ...draft, behaviors: next })}
      />

      {localError ? (
        <p className="dw-form-error" role="alert">
          {localError}
        </p>
      ) : null}

      <div className="dw-exp-save">
        <button
          type="button"
          className="dw-button dw-button-small dw-button-primary"
          disabled={busy}
          onClick={save}
          data-testid="world-experience-save"
        >
          {t("openWorld.saveExperience")}
        </button>
        <button
          type="button"
          className="dw-button dw-button-small dw-button-ghost"
          disabled={busy}
          onClick={() => {
            if (window.confirm(t("openWorld.clearExperienceConfirm"))) {
              setDraft(undefined);
              setSelectedID("root");
              onSave(null);
            }
          }}
        >
          {t("openWorld.clearExperience")}
        </button>
      </div>
    </section>
  );
}

function TreeNode({
  node,
  depth,
  selectedID,
  presentations,
  onSelect
}: {
  node: WorldViewNode;
  depth: number;
  selectedID: string;
  presentations: WorldCapabilityDescriptor[];
  onSelect: (id: string) => void;
}) {
  const { lang } = useI18n();
  const descriptor = presentations.find((capability) => capability.id === node.capability);
  return (
    <ul className="dw-exp-tree-list">
      <li>
        <button
          type="button"
          className={`dw-exp-tree-node${selectedID === node.id ? " is-selected" : ""}`}
          style={{ paddingLeft: 8 + depth * 14 }}
          onClick={() => onSelect(node.id)}
          data-testid={`world-experience-node-${node.id}`}
        >
          <span>{descriptor ? localized(descriptor.title, lang) : node.capability}</span>
          <code>{node.id}</code>
        </button>
        {node.children && node.children.length > 0 ? (
          <ul className="dw-exp-tree-children">
            {node.children.map((child) => (
              <TreeNode
                key={child.id}
                node={child}
                depth={depth + 1}
                selectedID={selectedID}
                presentations={presentations}
                onSelect={onSelect}
              />
            ))}
          </ul>
        ) : null}
      </li>
    </ul>
  );
}

function CapabilityPalette({
  capabilities,
  world,
  behaviors,
  lang,
  onAdd
}: {
  capabilities: WorldCapabilityDescriptor[];
  world: OpenWorldState;
  behaviors: WorldBehavior[];
  lang: "en" | "zh";
  onAdd: (capability: WorldCapabilityDescriptor) => void;
}) {
  return (
    <div className="dw-exp-palette">
      {capabilities.map((capability) => {
        const disabled = !canUseCapability(capability.id, world, behaviors);
        return (
          <button
            key={capability.id}
            type="button"
            className="dw-exp-palette-item"
            disabled={disabled}
            title={disabled ? unavailableReason(capability.id, world, behaviors, lang) : localized(capability.description, lang)}
            onClick={() => onAdd(capability)}
            data-testid={`world-experience-add-${capability.id}`}
          >
            <strong>{localized(capability.title, lang)}</strong>
            <small>{localized(capability.description, lang)}</small>
          </button>
        );
      })}
    </div>
  );
}

function BehaviorsEditor({
  behaviors,
  capabilities,
  world,
  lang,
  onChange
}: {
  behaviors: WorldBehavior[];
  capabilities: WorldCapabilityDescriptor[];
  world: OpenWorldState;
  lang: "en" | "zh";
  onChange: (behaviors: WorldBehavior[]) => void;
}) {
  const { t } = useI18n();
  const zh = lang === "zh";

  function addBehavior() {
    let index = 1;
    while (behaviors.some((behavior) => behavior.id === `act-${index}`)) {
      index += 1;
    }
    onChange([
      ...behaviors,
      {
        id: `act-${index}`,
        name: `${zh ? "行为" : "Behavior"} ${index}`,
        actions: [defaultAction(world)]
      }
    ]);
  }

  return (
    <section className="dw-exp-behaviors" data-testid="world-experience-behaviors">
      <div className="dw-exp-section-head">
        <h3>{t("openWorld.behaviors")}</h3>
        <button type="button" className="dw-button dw-button-small dw-button-ghost" onClick={addBehavior}>
          {t("openWorld.addBehavior")}
        </button>
      </div>
      {behaviors.length === 0 ? <p className="dw-open-hint">{t("openWorld.behaviorsEmpty")}</p> : null}
      {behaviors.map((behavior, index) => (
        <BehaviorCard
          key={behavior.id}
          behavior={behavior}
          capabilities={capabilities}
          world={world}
          lang={lang}
          onChange={(next) => onChange(behaviors.map((entry, entryIndex) => (entryIndex === index ? next : entry)))}
          onRemove={() => onChange(behaviors.filter((_entry, entryIndex) => entryIndex !== index))}
        />
      ))}
    </section>
  );
}

function BehaviorCard({
  behavior,
  capabilities,
  world,
  lang,
  onChange,
  onRemove
}: {
  behavior: WorldBehavior;
  capabilities: WorldCapabilityDescriptor[];
  world: OpenWorldState;
  lang: "en" | "zh";
  onChange: (behavior: WorldBehavior) => void;
  onRemove: () => void;
}) {
  const { t } = useI18n();
  const [conditional, setConditional] = useState(Boolean(behavior.condition));
  return (
    <article className="dw-exp-behavior" data-testid={`world-experience-behavior-${behavior.id}`}>
      <div className="dw-exp-behavior-head">
        <code>{behavior.id}</code>
        <input
          className="dw-input"
          value={behavior.name ?? ""}
          maxLength={120}
          aria-label={t("openWorld.behaviorName")}
          onChange={(event) => onChange({ ...behavior, name: event.target.value })}
        />
        <button type="button" className="dw-icon-button" aria-label={t("openWorld.remove")} onClick={onRemove}>
          ×
        </button>
      </div>
      <label className="dw-exp-check">
        <input
          type="checkbox"
          checked={conditional}
          onChange={(event) => {
            setConditional(event.target.checked);
            onChange({
              ...behavior,
              condition: event.target.checked ? defaultCondition(world) : undefined
            });
          }}
        />
        {t("openWorld.behaviorCondition")}
      </label>
      {conditional && behavior.condition ? (
        <ConditionEditor
          condition={behavior.condition}
          world={world}
          onChange={(condition) => onChange({ ...behavior, condition })}
        />
      ) : null}
      <div className="dw-exp-actions-list">
        {behavior.actions.map((action, index) => (
          <ActionEditor
            key={`${behavior.id}:${index}`}
            action={action}
            capabilities={capabilities}
            world={world}
            lang={lang}
            onChange={(next) =>
              onChange({
                ...behavior,
                actions: behavior.actions.map((entry, entryIndex) => (entryIndex === index ? next : entry))
              })
            }
            onRemove={() =>
              onChange({
                ...behavior,
                actions: behavior.actions.filter((_entry, entryIndex) => entryIndex !== index)
              })
            }
          />
        ))}
      </div>
      <button
        type="button"
        className="dw-button dw-button-small dw-button-ghost"
        onClick={() => onChange({ ...behavior, actions: [...behavior.actions, defaultAction(world)] })}
      >
        {t("openWorld.addAction")}
      </button>
    </article>
  );
}

function ActionEditor({
  action,
  capabilities,
  world,
  lang,
  onChange,
  onRemove
}: {
  action: WorldBehavior["actions"][number];
  capabilities: WorldCapabilityDescriptor[];
  world: OpenWorldState;
  lang: "en" | "zh";
  onChange: (action: WorldBehavior["actions"][number]) => void;
  onRemove: () => void;
}) {
  const { t } = useI18n();
  const capability = capabilities.find((entry) => entry.id === action.capability);
  const actionDefinition = capability?.actions?.find((entry) => entry.id === action.action);
  const params = action.params ?? {};
  const toggle = action.action === "toggle";
  const fieldType = fieldTypeFor(world, params.entity_id, params.field);
  return (
    <div className="dw-exp-action" data-testid="world-experience-action">
      <div className="dw-exp-action-head">
        <select
          className="dw-input"
          value={action.capability}
          aria-label={t("openWorld.actionCapability")}
          onChange={(event) => {
            const nextCapability = capabilities.find((entry) => entry.id === event.target.value);
            const nextAction = nextCapability?.actions?.[0];
            if (nextAction) {
              onChange({
                capability: nextCapability!.id,
                action: nextAction.id,
                params: defaultParamsForAction(nextAction, world)
              });
            }
          }}
        >
          {capabilities.map((entry) => (
            <option key={entry.id} value={entry.id}>
              {localized(entry.title, lang)}
            </option>
          ))}
        </select>
        <select
          className="dw-input"
          value={action.action}
          aria-label={t("openWorld.actionName")}
          onChange={(event) => {
            const nextAction = capability?.actions?.find((entry) => entry.id === event.target.value);
            if (nextAction) {
              onChange({
                capability: action.capability,
                action: nextAction.id,
                params: defaultParamsForAction(nextAction, world)
              });
            }
          }}
        >
          {capability?.actions?.map((entry) => (
            <option key={entry.id} value={entry.id}>
              {localized(entry.title, lang)}
            </option>
          ))}
        </select>
        <button type="button" className="dw-icon-button" aria-label={t("openWorld.remove")} onClick={onRemove}>
          ×
        </button>
      </div>
      {actionDefinition?.params ? (
        <>
          <SchemaFields
            schema={actionDefinition.params}
            value={params}
            onChange={(next) => {
              const cleaned = { ...next };
              if (toggle || action.capability === "core.state") {
                delete cleaned.value;
              }
              onChange({ ...action, params: cleaned });
            }}
            world={world}
            behaviors={[]}
            contextProps={params}
            hideKeys={toggle || action.capability === "core.state" ? ["value"] : []}
          />
          {!toggle && action.capability === "core.state" ? (
            <StateValueEditor
              value={params.value}
              fieldType={fieldType}
              onChange={(value) => onChange({ ...action, params: { ...params, value } })}
            />
          ) : null}
        </>
      ) : null}
    </div>
  );
}

function StateValueEditor({
  value,
  fieldType,
  onChange
}: {
  value: unknown;
  fieldType?: string;
  onChange: (value: unknown) => void;
}) {
  const { t } = useI18n();
  const eventValue = value === "$event.value";
  const type = fieldType ?? "text";
  return (
    <div className="dw-exp-state-value">
      <label className="dw-exp-check">
        <input
          type="checkbox"
          checked={eventValue}
          onChange={(event) => onChange(event.target.checked ? "$event.value" : type === "number" ? 0 : type === "flag" ? true : "")}
        />
        {t("openWorld.useEventValue")}
      </label>
      {eventValue ? (
        <code>$event.value</code>
      ) : type === "number" ? (
        <input
          className="dw-input"
          type="number"
          value={typeof value === "number" ? value : ""}
          onChange={(event) => onChange(Number(event.target.value))}
          aria-label={t("openWorld.value")}
        />
      ) : type === "flag" ? (
        <select
          className="dw-input"
          value={value === true ? "true" : "false"}
          onChange={(event) => onChange(event.target.value === "true")}
          aria-label={t("openWorld.value")}
        >
          <option value="true">{t("openWorld.on")}</option>
          <option value="false">{t("openWorld.off")}</option>
        </select>
      ) : type === "status" ? (
        <select
          className="dw-input"
          value={value === "done" ? "done" : "open"}
          onChange={(event) => onChange(event.target.value)}
          aria-label={t("openWorld.value")}
        >
          <option value="open">{t("openWorld.status.open")}</option>
          <option value="done">{t("openWorld.status.done")}</option>
        </select>
      ) : (
        <input
          className="dw-input"
          value={typeof value === "string" ? value : ""}
          onChange={(event) => onChange(event.target.value)}
          aria-label={t("openWorld.value")}
        />
      )}
    </div>
  );
}

function ConditionEditor({
  condition,
  world,
  onChange
}: {
  condition: WorldCondition;
  world: OpenWorldState;
  onChange: (condition: WorldCondition) => void;
}) {
  const { t } = useI18n();
  const fieldType = fieldTypeFor(world, condition.entity_id, condition.field);
  const needsValue = condition.op !== "truthy" && condition.op !== "falsy";
  const ops = condition.field === "status"
    ? ["eq", "ne"]
    : fieldType === "number"
      ? ["eq", "ne", "gt", "gte", "lt", "lte"]
      : fieldType === "flag"
        ? ["eq", "ne", "truthy", "falsy"]
        : fieldType === "text"
          ? ["eq", "ne"]
          : ["eq", "ne", "gt", "gte", "lt", "lte", "truthy", "falsy"];
  return (
    <div className="dw-exp-condition" data-testid="world-experience-condition">
      <select
        className="dw-input"
        value={condition.entity_id ?? ""}
        aria-label={t("openWorld.entity")}
        onChange={(event) => onChange({ ...condition, entity_id: event.target.value })}
      >
        <option value="$event.entity">{t("openWorld.eventEntity")}</option>
        {world.nodes.map((node) => (
          <option key={node.id} value={node.id}>
            {node.name}
          </option>
        ))}
      </select>
      <select
        className="dw-input"
        value={condition.field ?? ""}
        aria-label={t("openWorld.field")}
        onChange={(event) => onChange({ ...condition, field: event.target.value, value: undefined, op: "eq" })}
      >
        <option value="status">{t("openWorld.statusField")}</option>
        {fieldOptions(world, condition.entity_id, undefined).map((entry) => (
          <option key={entry.value} value={entry.value}>
            {entry.label}
          </option>
        ))}
      </select>
      <select
        className="dw-input"
        value={condition.op ?? "eq"}
        aria-label={t("openWorld.conditionOp")}
        onChange={(event) => onChange({ ...condition, op: event.target.value, value: undefined })}
      >
        {ops.map((op) => (
          <option key={op} value={op}>
            {t(`openWorld.cond.${op}` as never)}
          </option>
        ))}
      </select>
      {needsValue ? (
        fieldType === "status" ? (
          <select
            className="dw-input"
            value={condition.value === "done" ? "done" : "open"}
            aria-label={t("openWorld.value")}
            onChange={(event) => onChange({ ...condition, value: event.target.value })}
          >
            <option value="open">{t("openWorld.status.open")}</option>
            <option value="done">{t("openWorld.status.done")}</option>
          </select>
        ) : fieldType === "flag" ? (
          <select
            className="dw-input"
            value={condition.value === true ? "true" : "false"}
            aria-label={t("openWorld.value")}
            onChange={(event) => onChange({ ...condition, value: event.target.value === "true" })}
          >
            <option value="true">{t("openWorld.on")}</option>
            <option value="false">{t("openWorld.off")}</option>
          </select>
        ) : (
          <input
            className="dw-input"
            type={fieldType === "number" ? "number" : "text"}
            value={typeof condition.value === "string" || typeof condition.value === "number" ? String(condition.value) : ""}
            aria-label={t("openWorld.value")}
            onChange={(event) =>
              onChange({
                ...condition,
                value: fieldType === "number" ? Number(event.target.value) : event.target.value
              })
            }
          />
        )
      ) : null}
    </div>
  );
}

function SchemaFields({
  schema,
  value,
  onChange,
  world,
  behaviors,
  contextProps,
  hideKeys = []
}: {
  schema: WorldCapabilitySchema;
  value: Record<string, unknown>;
  onChange: (value: Record<string, unknown>) => void;
  world: OpenWorldState;
  behaviors: WorldBehavior[];
  contextProps: Record<string, unknown>;
  hideKeys?: string[];
}) {
  return (
    <div className="dw-exp-schema-fields">
      {Object.entries(schema.properties).map(([key, property]) => {
        if (hideKeys.includes(key)) {
          return null;
        }
        return (
          <SchemaField
            key={key}
            name={key}
            property={property}
            value={value[key]}
            onChange={(next) => onChange({ ...value, [key]: next })}
            world={world}
            behaviors={behaviors}
            contextProps={contextProps}
          />
        );
      })}
    </div>
  );
}

function SchemaField({
  name,
  property,
  value,
  onChange,
  world,
  behaviors,
  contextProps
}: {
  name: string;
  property: WorldCapabilityProperty;
  value: unknown;
  onChange: (value: unknown) => void;
  world: OpenWorldState;
  behaviors: WorldBehavior[];
  contextProps: Record<string, unknown>;
}) {
  const { t, lang } = useI18n();
  const label = localized(property.title, lang) || name;
  const description = property.description ? localized(property.description, lang) : "";

  if (property.type === "object" && property.properties) {
    const objectValue = isRecord(value) ? value : {};
    return (
      <fieldset className="dw-exp-fieldset">
        <legend>{label}</legend>
        <SchemaFields
          schema={{ type: "object", properties: property.properties, required: property.required }}
          value={objectValue}
          onChange={onChange}
          world={world}
          behaviors={behaviors}
          contextProps={{ ...contextProps, ...objectValue }}
        />
      </fieldset>
    );
  }

  if (property.type === "array" && property.items) {
    const rows = Array.isArray(value) ? value : [];
    return (
      <div className="dw-exp-array">
        <span className="dw-exp-field-label">{label}</span>
        {rows.map((row, index) => {
          const rowValue = isRecord(row) ? row : {};
          return (
            <div className="dw-exp-array-row" key={index}>
              <SchemaField
                name={`${name}-${index}`}
                property={property.items!}
                value={row}
                onChange={(next) =>
                  onChange(rows.map((entry, entryIndex) => (entryIndex === index ? next : entry)))
                }
                world={world}
                behaviors={behaviors}
                contextProps={{ ...contextProps, ...rowValue }}
              />
              <button
                type="button"
                className="dw-icon-button"
                aria-label={t("openWorld.remove")}
                onClick={() => onChange(rows.filter((_entry, entryIndex) => entryIndex !== index))}
              >
                ×
              </button>
            </div>
          );
        })}
        <button
          type="button"
          className="dw-button dw-button-small dw-button-ghost"
          onClick={() =>
            onChange([
              ...rows,
              defaultValueForProperty(property.items!, world, behaviors, contextProps)
            ])
          }
        >
          {t("openWorld.addListItem")}
        </button>
      </div>
    );
  }

  if (property.type === "boolean") {
    return (
      <label className="dw-exp-check">
        <input type="checkbox" checked={value === true} onChange={(event) => onChange(event.target.checked)} />
        {label}
      </label>
    );
  }

  if (property.type === "number" || property.type === "integer") {
    const numeric = typeof value === "number" && Number.isFinite(value) ? value : undefined;
    const minimum = property.minimum ?? 0;
    const maximum = property.maximum ?? Math.max(minimum + 100, 100);
    return (
      <label className="dw-exp-field-label">
        <span>
          {label}
          {numeric !== undefined ? ` · ${numeric}` : ""}
        </span>
        <input
          className="dw-input"
          type="number"
          value={numeric ?? ""}
          min={property.minimum}
          max={property.maximum}
          step={property.type === "integer" ? 1 : "any"}
          onChange={(event) => onChange(Number(event.target.value))}
          aria-label={label}
        />
        {maximum > minimum && maximum - minimum <= 400 ? (
          <input
            type="range"
            min={minimum}
            max={maximum}
            value={numeric ?? minimum}
            onChange={(event) => onChange(Number(event.target.value))}
            aria-label={`${label} slider`}
          />
        ) : null}
      </label>
    );
  }

  if (property.type === "string") {
    const text = typeof value === "string" ? value : "";
    const options = stringOptions(property, world, behaviors, contextProps, lang);
    if (options.length > 0) {
      return (
        <label className="dw-exp-field-label">
          <span>{label}</span>
          <select className="dw-input" value={text} onChange={(event) => onChange(event.target.value)} aria-label={label}>
            {!property.enum ? <option value="">—</option> : null}
            {options.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
          {description ? <small>{description}</small> : null}
        </label>
      );
    }
    if (property.format === "template" || property.format === "path") {
      return (
        <label className="dw-exp-field-label">
          <span>{label}</span>
          <textarea
            className={`dw-input dw-textarea${property.format === "path" ? " dw-exp-path" : ""}`}
            rows={property.format === "path" ? 3 : 4}
            value={text}
            maxLength={property.max_length}
            onChange={(event) => onChange(event.target.value)}
            aria-label={label}
          />
          {description ? <small>{description}</small> : null}
        </label>
      );
    }
    return (
      <label className="dw-exp-field-label">
        <span>{label}</span>
        <input
          className="dw-input"
          value={text}
          maxLength={property.max_length}
          onChange={(event) => onChange(event.target.value)}
          aria-label={label}
        />
        {description ? <small>{description}</small> : null}
      </label>
    );
  }

  return null;
}

function stringOptions(
  property: WorldCapabilityProperty,
  world: OpenWorldState,
  behaviors: WorldBehavior[],
  contextProps: Record<string, unknown>,
  lang: "en" | "zh"
): Array<{ value: string; label: string }> {
  if (property.enum) {
    return property.enum.map((value) => ({ value, label: value }));
  }
  switch (property.format) {
    case "kind":
      return world.kinds.map((kind) => ({ value: kind.id, label: kind.name }));
    case "entity":
      return [
        { value: "$event.entity", label: lang === "zh" ? "事件中的存在" : "The event's thing" },
        ...world.nodes.map((node) => ({ value: node.id, label: node.name }))
      ];
    case "item_entity":
      return [
        { value: "$item", label: lang === "zh" ? "当前对象" : "Current item" },
        ...world.nodes.map((node) => ({ value: node.id, label: node.name }))
      ];
    case "behavior":
      return behaviors.map((behavior) => ({ value: behavior.id, label: behavior.name || behavior.id }));
    case "field":
      return fieldOptions(world, contextProps.entity_id, contextProps.kind_id).map((entry) => ({
        value: entry.value,
        label: entry.label
      }));
    case "table_source": {
      const kind = world.kinds.find((entry) => entry.id === contextProps.kind_id);
      return [
        { value: "name", label: lang === "zh" ? "名称" : "Name" },
        { value: "text", label: lang === "zh" ? "描述" : "Text" },
        { value: "status", label: lang === "zh" ? "状态" : "Status" },
        ...(kind?.fields.map((field) => ({ value: field.key, label: field.label || field.key })) ?? [])
      ];
    }
    default:
      return [];
  }
}

function fieldOptions(
  world: OpenWorldState,
  entityID: unknown,
  kindID: unknown
): Array<{ value: string; label: string }> {
  let kind: OpenWorldKind | undefined;
  if (typeof kindID === "string") {
    kind = world.kinds.find((entry) => entry.id === kindID);
  } else if (typeof entityID === "string" && entityID !== "$item" && entityID !== "$event.entity") {
    const node = world.nodes.find((entry) => entry.id === entityID);
    kind = node ? world.kinds.find((entry) => entry.id === node.kind_id) : undefined;
  }
  if (kind) {
    return kind.fields.map((field) => ({ value: field.key, label: field.label || field.key }));
  }
  const seen = new Map<string, string>();
  for (const entry of world.kinds) {
    for (const field of entry.fields) {
      if (!seen.has(field.key)) {
        seen.set(field.key, field.label || field.key);
      }
    }
  }
  return Array.from(seen, ([value, label]) => ({ value, label }));
}

function fieldTypeFor(world: OpenWorldState, entityID: unknown, field: unknown): string | undefined {
  if (typeof field !== "string" || !field) {
    return undefined;
  }
  if (field === "status") {
    return "status";
  }
  if (typeof entityID === "string" && entityID !== "$item" && entityID !== "$event.entity") {
    const node = world.nodes.find((entry) => entry.id === entityID);
    if (node) {
      const kind = world.kinds.find((entry) => entry.id === node.kind_id);
      return kind?.fields.find((entry) => entry.key === field)?.type;
    }
  }
  const types = new Set<string>();
  for (const kind of world.kinds) {
    const definition = kind.fields.find((entry) => entry.key === field);
    if (definition) {
      types.add(definition.type);
    }
  }
  return types.size === 1 ? Array.from(types)[0] : undefined;
}

function defaultParamsForAction(action: WorldCapabilityAction, world: OpenWorldState): Record<string, unknown> {
  const params: Record<string, unknown> = {};
  const properties = action.params?.properties ?? {};
  for (const [key, property] of Object.entries(properties)) {
    if (key === "value") {
      continue;
    }
    params[key] = defaultValueForProperty(property, world, [], {});
  }
  return params;
}

function defaultAction(world: OpenWorldState): WorldBehavior["actions"][number] {
  const node = world.nodes[0];
  const kind = node ? world.kinds.find((entry) => entry.id === node.kind_id) : undefined;
  const field = kind?.fields.find((entry) => entry.type === "flag") ?? kind?.fields[0];
  const fieldType = field?.type ?? "status";
  return {
    capability: "core.state",
    action: "set",
    params: {
      entity_id: node?.id ?? "$event.entity",
      field: field?.key ?? "status",
      value: fieldType === "number" ? 0 : fieldType === "flag" ? true : fieldType === "text" ? "—" : "open"
    }
  };
}

function defaultCondition(world: OpenWorldState): WorldCondition {
  const node = world.nodes[0];
  const kind = node ? world.kinds.find((entry) => entry.id === node.kind_id) : undefined;
  const field = kind?.fields[0];
  return {
    entity_id: node?.id ?? "$event.entity",
    field: field?.key ?? "status",
    op: field ? "eq" : "eq",
    value: field?.type === "number" ? 0 : field?.type === "flag" ? true : field?.type === "text" ? "" : "open"
  };
}

function defaultValueForProperty(
  property: WorldCapabilityProperty,
  world: OpenWorldState,
  behaviors: WorldBehavior[],
  contextProps: Record<string, unknown>
): unknown {
  switch (property.type) {
    case "object": {
      const object: Record<string, unknown> = {};
      for (const [key, child] of Object.entries(property.properties ?? {})) {
        object[key] = defaultValueForProperty(child, world, behaviors, contextProps);
      }
      return object;
    }
    case "array":
      return [];
    case "boolean":
      return false;
    case "number":
    case "integer":
      return property.minimum ?? 0;
    case "string":
      if (property.enum) {
        return property.enum[0] ?? "";
      }
      if (property.format === "path") {
        return DEFAULT_PATH;
      }
      if (property.format === "kind") {
        return world.kinds[0]?.id ?? "";
      }
      if (property.format === "entity") {
        return "$event.entity";
      }
      if (property.format === "item_entity") {
        return "$item";
      }
      if (property.format === "behavior") {
        return behaviors[0]?.id ?? "";
      }
      if (property.format === "field") {
        return fieldOptions(world, contextProps.entity_id, contextProps.kind_id)[0]?.value ?? "";
      }
      if (property.format === "table_source") {
        return "name";
      }
      return "";
    default:
      return null;
  }
}

function makeNode(
  capability: WorldCapabilityDescriptor,
  world: OpenWorldState,
  behaviors: WorldBehavior[]
): WorldViewNode {
  const props: Record<string, unknown> = {};
  for (const [key, property] of Object.entries(capability.params?.properties ?? {})) {
    props[key] = defaultValueForProperty(property, world, behaviors, props);
  }
  if (capability.id === "core.space") {
    props.views = world.kinds.map((kind) => ({
      kind_id: kind.id,
      path: DEFAULT_PATH,
      size: 72,
      fill_hue: kind.hue,
      opacity: 0.9,
      paint: "fill",
      label: "always",
      layer: "mid",
      bindings: []
    }));
    props.background = { mode: "gradient", hue: 222, hue2: 205, lightness: 10, accent_hue: 42 };
    props.show_links = true;
    props.activate_behavior_id = "";
  }
  if (capability.id === "core.table" && world.kinds[0]) {
    const kind = world.kinds[0];
    props.kind_id = kind.id;
    props.columns = [
      { source: "name", label: kind.name },
      ...kind.fields.slice(0, 2).map((field) => ({ source: field.key, label: field.label || field.key }))
    ];
    props.row_behavior_id = "";
  }
  if (capability.id === "core.collection" && world.kinds[0]) {
    props.kind_id = world.kinds[0].id;
  }
  if (capability.id === "core.field" || capability.id === "core.meter") {
    const node = world.nodes[0];
    const kind = node ? world.kinds.find((entry) => entry.id === node.kind_id) : undefined;
    const field = capability.id === "core.meter"
      ? kind?.fields.find((entry) => entry.type === "number")
      : kind?.fields.find((entry) => entry.type !== "text") ?? kind?.fields[0];
    props.entity_id = node?.id ?? "";
    props.field = field?.key ?? "";
    if (capability.id === "core.meter") {
      props.min = 0;
      props.max = 10;
    }
  }
  if (capability.id === "core.button" || capability.id === "core.choice") {
    const behaviorID = behaviors[0]?.id ?? "";
    if (capability.id === "core.button") {
      props.label = "Do it";
      props.behavior_id = behaviorID;
    } else {
      props.options = [
        { label: "A", behavior_id: behaviorID },
        { label: "B", behavior_id: behaviorID }
      ];
    }
  }
  return {
    id: nextNodeID(),
    capability: capability.id,
    props
  };
}

function nextNodeID(): string {
  return `view-${Math.random().toString(36).slice(2, 8)}`;
}

function makeScaffold(
  kind: "blank" | "text" | "space" | "workspace" | "composite",
  world: OpenWorldState,
  behaviors: WorldBehavior[],
  zh: boolean
): WorldExperience {
  const title: WorldViewNode = {
    id: "title",
    capability: "core.text",
    props: { text: "{{world.title}}", size: 34, weight: "bold", tone: "default", align: "left" }
  };
  const premise: WorldViewNode = {
    id: "premise",
    capability: "core.text",
    props: {
      text: zh ? "{{world.premise}}\n\n这是你自己组合的体验。任何部分都可以修改。" : "{{world.premise}}\n\nThis experience is yours to recompose. Change any part.",
      size: 17,
      weight: "regular",
      tone: "muted",
      align: "left"
    }
  };
  const root: WorldViewNode = {
    id: "root",
    capability: "core.stack",
    props: { direction: "vertical", gap: 18, padding: 24, align: "stretch", tone: "panel" },
    children: [title]
  };
  if (kind === "text") {
    root.children = [title, premise];
  } else if (kind === "space") {
    const space = makeNode({ id: "core.space", kind: "presentation", version: 1, title: { en: "Space", zh: "空间" }, description: { en: "", zh: "" } }, world, behaviors);
    root.children = [title, { ...space, id: "space" }];
  } else if (kind === "workspace" || kind === "composite") {
    root.children = [title, premise];
    if (world.kinds[0]) {
      const table = makeNode({ id: "core.table", kind: "presentation", version: 1, title: { en: "Table", zh: "表" }, description: { en: "", zh: "" } }, world, behaviors);
      root.children.push({ ...table, id: "table" });
    }
    if (kind === "composite" && world.kinds[0]) {
      const space = makeNode({ id: "core.space", kind: "presentation", version: 1, title: { en: "Space", zh: "空间" }, description: { en: "", zh: "" } }, world, behaviors);
      root.children.push({ ...space, id: "space" });
    }
  }
  return { protocol: 1, root, behaviors };
}

function localized(label: LocalizedLabel, lang: "en" | "zh"): string {
  return lang === "zh" ? label.zh : label.en;
}

function canUseCapability(id: string, world: OpenWorldState, behaviors: WorldBehavior[]): boolean {
  if (id === "core.collection" || id === "core.table" || id === "core.space") {
    return world.kinds.length > 0;
  }
  if (id === "core.field") {
    return world.nodes.length > 0;
  }
  if (id === "core.meter") {
    return world.nodes.some((node) => {
      const kind = world.kinds.find((entry) => entry.id === node.kind_id);
      return Boolean(kind?.fields.some((field) => field.type === "number"));
    });
  }
  if (id === "core.button" || id === "core.choice") {
    return behaviors.length > 0;
  }
  return true;
}

function unavailableReason(id: string, world: OpenWorldState, behaviors: WorldBehavior[], lang: "en" | "zh"): string {
  if (id === "core.field" && world.nodes.length === 0) {
    return lang === "zh" ? "先添加一个存在。" : "Add a thing first.";
  }
  if (id === "core.meter" && world.nodes.length === 0) {
    return lang === "zh" ? "先添加一个带数字字段的存在。" : "Add a thing with a number field first.";
  }
  if ((id === "core.button" || id === "core.choice") && behaviors.length === 0) {
    return lang === "zh" ? "先在下方添加一个行为。" : "Add a behavior below first.";
  }
  return lang === "zh" ? "先添加一种存在。" : "Add a kind first.";
}

function findNode(root: WorldViewNode, id: string): WorldViewNode | undefined {
  if (root.id === id) {
    return root;
  }
  for (const child of root.children ?? []) {
    const found = findNode(child, id);
    if (found) {
      return found;
    }
  }
  return undefined;
}

function findParent(root: WorldViewNode, id: string): WorldViewNode | undefined {
  for (const child of root.children ?? []) {
    if (child.id === id) {
      return root;
    }
    const found = findParent(child, id);
    if (found) {
      return found;
    }
  }
  return undefined;
}

function updateNode(root: WorldViewNode, id: string, transform: (node: WorldViewNode) => WorldViewNode): WorldViewNode {
  if (root.id === id) {
    return transform(root);
  }
  return {
    ...root,
    children: (root.children ?? []).map((child) => updateNode(child, id, transform))
  };
}

function removeNode(root: WorldViewNode, id: string): WorldViewNode | undefined {
  if (root.id === id) {
    return undefined;
  }
  const children = (root.children ?? [])
    .filter((child) => child.id !== id)
    .map((child) => removeNode(child, id))
    .filter((child): child is WorldViewNode => Boolean(child));
  return { ...root, children };
}

function moveNode(root: WorldViewNode, id: string, delta: number): WorldViewNode {
  const siblings = root.children ?? [];
  const index = siblings.findIndex((child) => child.id === id);
  if (index >= 0) {
    const target = index + delta;
    if (target < 0 || target >= siblings.length) {
      return root;
    }
    const next = [...siblings];
    const left = next[index];
    const right = next[target];
    if (left && right) {
      next[index] = right;
      next[target] = left;
    }
    return { ...root, children: next };
  }
  return { ...root, children: siblings.map((child) => moveNode(child, id, delta)) };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}
