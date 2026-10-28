// SPDX-License-Identifier: Apache-2.0
import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";
import {
  deriveObject,
  getLens,
  openWorldState,
  type DeriveLens,
  type ObjectView
} from "../api";
import { ErrorNotice, StatusNotice } from "../errors";
import { useI18n } from "../i18n";
import AIPanel from "./AIPanel";
import LensPanel from "./LensPanel";
import ObjectWorkTab from "./ObjectWorkTab";
import WorldWorkPanel from "./v2/WorldWorkPanel";
import Icon from "./v2/Icon";

export type StageInspectorTab = "work" | "branch" | "lens" | "ai";

function objectTitle(object: ObjectView): string {
  const value = object.state.title;
  return typeof value === "string" ? value : "";
}

function formatInheritedValue(value: unknown, emptyLabel: string): string {
  if (value === undefined || value === null || value === "") {
    return emptyLabel;
  }
  if (Array.isArray(value)) {
    if (value.length === 0) {
      return emptyLabel;
    }
    return value
      .map((item) => {
        if (item && typeof item === "object" && "text" in item) {
          const text = (item as { text?: unknown }).text;
          return typeof text === "string" ? text : JSON.stringify(item);
        }
        return typeof item === "string" ? item : JSON.stringify(item);
      })
      .join(" · ");
  }
  if (typeof value === "object") {
    return JSON.stringify(value);
  }
  return String(value);
}

function BranchComposer({
  object,
  onDerived,
  onRefresh
}: {
  object: ObjectView;
  onDerived: (child: ObjectView) => void;
  onRefresh: () => Promise<void> | void;
}) {
  const { t } = useI18n();
  const [lens, setLens] = useState<DeriveLens | null>(null);
  const [title, setTitle] = useState("");
  const [fields, setFields] = useState<string[]>([]);
  const [phase, setPhase] = useState<"edit" | "preview">("edit");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [notice, setNotice] = useState("");
  const [lensError, setLensError] = useState<unknown>(null);

  const canDerive = object.permissions.includes("derive");
  const defaultTitle = useMemo(() => {
    const name = objectTitle(object) || t("common.untitled");
    return t("branch.defaultTitle", { title: name });
  }, [object, t]);

  useEffect(() => {
    let cancelled = false;
    setLensError(null);
    getLens(object.id)
      .then((data) => {
        if (cancelled) {
          return;
        }
        setLens(data);
        const available = data.inheritance_fields;
        setFields((current) => (current.length > 0 ? current : available));
      })
      .catch((failure) => {
        if (!cancelled) {
          setLensError(failure);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [object.id]);

  useEffect(() => {
    setTitle((current) => (current === "" ? defaultTitle : current));
  }, [defaultTitle]);

  const available = lens?.inheritance_fields ?? [];
  const previewEntries = fields.map((field) => ({
    field,
    value: formatInheritedValue(object.state[field], t("branch.emptyValue"))
  }));

  function toggleField(field: string) {
    setFields((current) =>
      current.includes(field) ? current.filter((item) => item !== field) : [...current, field]
    );
    setPhase("edit");
  }

  function enterPreview() {
    setError(null);
    setNotice("");
    if (title.trim() === "") {
      setError(new Error(t("branch.titleRequired")));
      return;
    }
    if (fields.length === 0) {
      setError(new Error(t("branch.fieldsRequired")));
      return;
    }
    if (object.object_type === "interactive" && !fields.includes("options")) {
      setError(new Error(t("branch.optionsRequired")));
      return;
    }
    setPhase("preview");
  }

  async function createBranch() {
    setBusy(true);
    setError(null);
    setNotice("");
    try {
      const child = await deriveObject(object.id, {
        title: title.trim(),
        source_version: object.version,
        inheritance_fields: fields
      });
      setNotice(t("branch.created"));
      setPhase("edit");
      setTitle(defaultTitle);
      onDerived(child);
      await onRefresh();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy(false);
    }
  }

  if (!canDerive) {
    return (
      <div className="stage-branch">
        <p className="muted">{t("branch.noPermission")}</p>
        <Link className="text-link" to={`/objects/${object.id}`}>
          {t("branch.openObject")}
        </Link>
      </div>
    );
  }

  return (
    <div className="stage-branch" data-testid="stage-branch-composer">
      <p className="muted">{t("branch.hint")}</p>
      {lensError ? <ErrorNotice error={lensError} compact /> : null}
      {notice ? <StatusNotice>{notice}</StatusNotice> : null}
      {error ? <ErrorNotice error={error} compact /> : null}

      {phase === "edit" ? (
        <form
          className="stack"
          onSubmit={(event) => {
            event.preventDefault();
            enterPreview();
          }}
        >
          <label>
            <span>{t("branch.title")}</span>
            <input
              maxLength={200}
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              placeholder={defaultTitle}
            />
          </label>
          <fieldset className="stage-branch-fields">
            <legend>{t("branch.inherit")}</legend>
            {available.length === 0 ? <p className="muted">{t("branch.noFields")}</p> : null}
            {available.map((field) => (
              <label key={field} className="check-row">
                <input
                  type="checkbox"
                  checked={fields.includes(field)}
                  onChange={() => toggleField(field)}
                />
                <span>{t(`branch.field.${field}` as never)}</span>
              </label>
            ))}
          </fieldset>
          <button className="button button-small" type="submit" disabled={busy}>
            {t("branch.preview")}
          </button>
        </form>
      ) : (
        <div className="stage-branch-preview" data-testid="stage-branch-preview">
          <h3>{t("branch.previewTitle")}</h3>
          <p>
            <strong>{title.trim()}</strong>
          </p>
          <p className="muted">
            {t("branch.source", {
              title: objectTitle(object) || t("common.untitled"),
              version: object.version
            })}
          </p>
          <h4>{t("branch.inheritedContent")}</h4>
          <dl className="stage-branch-values">
            {previewEntries.map((entry) => (
              <div key={entry.field}>
                <dt>{t(`branch.field.${entry.field}` as never)}</dt>
                <dd>{entry.value}</dd>
              </div>
            ))}
          </dl>
          <p className="muted">{t("branch.independent")}</p>
          <div className="stage-branch-actions">
            <button
              className="button button-small"
              type="button"
              disabled={busy}
              onClick={() => void createBranch()}
            >
              {busy ? t("common.loading") : t("branch.confirm")}
            </button>
            <button
              className="button button-ghost button-small"
              type="button"
              disabled={busy}
              onClick={() => setPhase("edit")}
            >
              {t("branch.back")}
            </button>
          </div>
        </div>
      )}
      <Link className="text-link" to={`/objects/${object.id}/derive`}>
        {t("branch.openFull")}
      </Link>
    </div>
  );
}

function OpenWorldLauncher({
  object,
  onBranch,
  onAI
}: {
  object: ObjectView;
  onBranch: () => void;
  onAI: () => void;
}) {
  const { t } = useI18n();
  const state = openWorldState(object);
  const canContribute = object.permissions.includes("contribute");
  return (
    <div className="dw-world-work dw-open-launcher" data-testid="open-world-launcher">
      <div className="dw-inspect-header">
        <div>
          <p className="dw-inspect-kicker">{t("openWorld.kicker")}</p>
          <h2 className="dw-inspect-title">{state.title || t("common.untitled")}</h2>
          <p className="dw-inspect-sub">
            {t("openWorld.summary", {
              kinds: state.kinds.length,
              nodes: state.nodes.length,
              rules: state.rules.length
            })}
          </p>
        </div>
      </div>
      <div className="dw-inspect-premise">
        <p>{state.premise || t("openWorld.premiseEmpty")}</p>
      </div>
      <div className="dw-open-launcher-actions">
        <Link className="dw-button dw-button-primary dw-button-small" to={`/worlds/${object.id}`}>
          <Icon name="arrow" size={15} />
          {t("openWorld.enter")}
        </Link>
        <button className="dw-button dw-button-small dw-button-ghost" type="button" onClick={onAI}>
          <Icon name="spark" size={15} />
          {t("openWorld.ai")}
        </button>
        {object.permissions.includes("derive") ? (
          <button className="dw-button dw-button-small dw-button-ghost" type="button" onClick={onBranch}>
            <Icon name="branch" size={15} />
            {t("stage.tab.branch")}
          </button>
        ) : null}
      </div>
      <p className="dw-inspect-source">{t("openWorld.unsupported")}</p>
      {canContribute ? null : <p className="muted">{t("workspace.permission.read")}</p>}
    </div>
  );
}

export default function StageInspector({
  object,
  tab,
  loading,
  error,
  sourceTitle = "",
  onTab,
  onClose,
  onUpdated,
  onReload,
  onRefresh,
  onDerived
}: {
  object: ObjectView | null;
  tab: StageInspectorTab;
  loading: boolean;
  error: unknown;
  sourceTitle?: string;
  onTab: (tab: StageInspectorTab) => void;
  onClose: () => void;
  onUpdated: (updated: ObjectView) => Promise<void> | void;
  onReload: () => Promise<void> | void;
  onRefresh: () => Promise<void> | void;
  onDerived: (child: ObjectView) => void;
}) {
  const { t } = useI18n();
  const headingRef = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    headingRef.current?.focus();
  }, [object?.id]);

  const canContribute = object?.permissions.includes("contribute") ?? false;
  const canManage = object?.permissions.includes("manage") ?? false;

  const tabs: Array<{ id: StageInspectorTab; label: string }> = [
    { id: "work", label: t("stage.tab.work") },
    { id: "branch", label: t("stage.tab.branch") },
    { id: "lens", label: t("stage.tab.lens") },
    { id: "ai", label: t("stage.tab.ai") }
  ];

  const livingScene = object?.object_type === "living_scene";
  const openWorld = object?.object_type === "open_world";
  const worldPanel = livingScene || openWorld;

  return (
    <section
      className={`card stage-inspector dw-inspector${worldPanel ? " dw-inspector-world" : ""}`}
      aria-label={t("stage.inspectorLabel")}
      data-testid="stage-inspector"
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.stopPropagation();
          onClose();
        }
      }}
    >
      {worldPanel ? (
        <>
          <button className="dw-inspector-close" type="button" onClick={onClose} aria-label={t("common.close")}>
            <Icon name="close" size={17} />
          </button>
          <h2 ref={headingRef} tabIndex={-1} className="dw-visually-hidden">
            {objectTitle(object ?? null) || t("common.untitled")}
          </h2>
        </>
      ) : (
        <>
          <div className="stage-inspector-head">
            <div>
              <p className="eyebrow">{t("stage.inspectorEyebrow")}</p>
              <h2 ref={headingRef} tabIndex={-1}>
                {object ? objectTitle(object) || t("common.untitled") : t("stage.inspectorTitle")}
              </h2>
            </div>
            <button className="text-button" type="button" onClick={onClose}>
              {t("common.close")}
            </button>
          </div>

          {object ? (
            <p className="muted stage-inspector-meta">
              {t(
                `object.create.${
                  object.object_type === "living_scene"
                    ? "scene"
                    : object.object_type === "open_world"
                      ? "open_world"
                      : object.object_type
                }` as never
              )}
              {" · "}
              {t("object.version")} v{object.version}
            </p>
          ) : null}
        </>
      )}

      {loading ? <p className="muted">{t("common.loading")}</p> : null}
      {error ? <ErrorNotice error={error} compact onRetry={() => void onReload()} /> : null}

      {object ? (
        <>
          <div
            className="stage-inspector-tabs dw-inspector-tabs"
            role="tablist"
            aria-label={t("stage.inspectorLabel")}
            onKeyDown={(event) => {
              const current = tabs.findIndex((entry) => entry.id === tab);
              let next = current;
              if (event.key === "ArrowRight") {
                next = (current + 1) % tabs.length;
              } else if (event.key === "ArrowLeft") {
                next = (current - 1 + tabs.length) % tabs.length;
              } else if (event.key === "Home") {
                next = 0;
              } else if (event.key === "End") {
                next = tabs.length - 1;
              } else {
                return;
              }
              event.preventDefault();
              const target = tabs[next];
              if (!target) {
                return;
              }
              onTab(target.id);
              window.requestAnimationFrame(() => {
                document.getElementById(`stage-tab-${target.id}`)?.focus();
              });
            }}
          >
            {tabs.map((entry) => (
              <button
                key={entry.id}
                id={`stage-tab-${entry.id}`}
                className="stage-inspector-tab"
                type="button"
                role="tab"
                aria-selected={tab === entry.id}
                aria-controls="stage-tabpanel"
                tabIndex={tab === entry.id ? 0 : -1}
                onClick={() => onTab(entry.id)}
              >
                {entry.label}
              </button>
            ))}
          </div>

          <div
            className="stage-inspector-body"
            id="stage-tabpanel"
            role="tabpanel"
            aria-labelledby={`stage-tab-${tab}`}
          >
            {tab === "work" ? (
              openWorld ? (
                <OpenWorldLauncher
                  object={object}
                  onBranch={() => onTab("branch")}
                  onAI={() => onTab("ai")}
                />
              ) : object.object_type === "living_scene" ? (
                <WorldWorkPanel
                  object={object}
                  canContribute={canContribute}
                  sourceTitle={sourceTitle}
                  onUpdated={onUpdated}
                  onReload={onReload}
                  onBranch={() => onTab("branch")}
                  onAI={() => onTab("ai")}
                />
              ) : (
                <ObjectWorkTab
                  object={object}
                  canContribute={canContribute}
                  canManage={canManage}
                  recordedVote=""
                  onUpdated={onUpdated}
                  onReload={onReload}
                />
              )
            ) : null}
            {tab === "branch" ? (
              <BranchComposer object={object} onDerived={onDerived} onRefresh={onRefresh} />
            ) : null}
            {tab === "lens" ? <LensPanel objectID={object.id} /> : null}
            {tab === "ai" ? (
              <AIPanel
                object={object}
                canContribute={canContribute}
                onChanged={() => {
                  void onReload();
                  void onRefresh();
                }}
              />
            ) : null}
          </div>

          <Link className="text-link stage-inspector-open" to={`/objects/${object.id}`}>
            {t("stage.openFull")}
          </Link>
        </>
      ) : null}
    </section>
  );
}
