// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useRef, useState, type FormEvent, type PointerEvent as ReactPointerEvent } from "react";
import { Link, useLocation, useNavigate, useParams, useSearchParams } from "react-router";
import {
  applyAction,
  deriveObject,
  errorMessage,
  getObject,
  generateObjectAsset,
  getWorldCapabilities,
  listObjectAssets,
  openWorldState,
  removeObjectAsset,
  uploadObjectAsset,
  type AssetKind,
  type ObjectView,
  type OpenWorldField,
  type OpenWorldKind,
  type OpenWorldNode,
  type OpenWorldPresentation,
  type OpenWorldState,
  type OpenWorldView,
  type WorldAsset,
  type WorldCapabilityDescriptor,
  type WorldExperience as WorldExperienceDocument
} from "../api";
import AIPanel from "../components/AIPanel";
import Icon from "../components/v2/Icon";
import Modal from "../components/v2/Modal";
import WorldExperience from "../components/v2/WorldExperience";
import WorldExperienceEditor from "../components/v2/WorldExperienceEditor";
import WorldLookEditor from "../components/v2/WorldLookEditor";
import WorldScene from "../components/v2/WorldScene";
import { useI18n } from "../i18n";
import { branchTitle } from "../worldLabels";

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

type ViewMode = "structure" | "experience" | "ai";
type WorldViewMode = OpenWorldView | "experience";

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
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const { t, lang } = useI18n();
  const zh = lang === "zh";
  const [object, setObject] = useState<ObjectView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [notice, setNotice] = useState("");
  const location = useLocation();
  const [busy, setBusy] = useState("");
  const [view, setView] = useState<WorldViewMode | "">("");
  // A world opens on its experience, not on the technical structure editor.
  // Panels are opened deliberately from the header; none is open by default.
  const [panel, setPanel] = useState<ViewMode | null>(
    searchParams.get("build") === "1"
      ? "experience"
      : searchParams.get("ai") === "1"
        ? "ai"
        : null
  );
  const [experienceOpened, setExperienceOpened] = useState(searchParams.get("build") === "1");
  const [titleEditing, setTitleEditing] = useState(false);
  const [titleDraft, setTitleDraft] = useState("");
  const [branchOpen, setBranchOpen] = useState(false);
  const [branchDraft, setBranchDraft] = useState("");
  const [branchBusy, setBranchBusy] = useState(false);
  const [capabilities, setCapabilities] = useState<WorldCapabilityDescriptor[]>([]);
  const [capabilityError, setCapabilityError] = useState("");
  const [assets, setAssets] = useState<WorldAsset[]>([]);
  const [assetError, setAssetError] = useState("");
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
      setTitleDraft(typeof value.state.title === "string" ? value.state.title : "");
    } catch (failure) {
      setError(failure);
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    // A route change to another world must never leave the previous object
    // interactive. Clear it and show loading until the new snapshot arrives;
    // otherwise the first click after Branch can write to the parent world.
    setObject(null);
    setLoading(true);
    setView("");
    setError(null);
    setNotice("");
    setBusy("");
    setDrag(null);
    setDragging(false);
    setPremiseEditing(false);
    setTitleEditing(false);
    setBranchOpen(false);
    setPanel(
      searchParams.get("build") === "1"
        ? "experience"
        : searchParams.get("ai") === "1"
          ? "ai"
          : null
    );
    setExperienceOpened(searchParams.get("build") === "1");
  }, [id]);

  // A link from the approval ledger opens the world's own AI panel, where the
  // pending action is rendered under its reply.
  useEffect(() => {
    if (searchParams.get("ai") === "1") {
      setPanel("ai");
    }
  }, [searchParams]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!panel) {
      return;
    }
    if (!window.matchMedia("(max-width: 900px)").matches) {
      return;
    }
    // On a narrow screen the panel renders below the world; opening it must
    // bring the panel itself into view instead of changing something offscreen.
    const timer = window.setTimeout(() => {
      document.querySelector(".dw-open-panel")?.scrollIntoView({ behavior: "smooth", block: "start" });
    }, 80);
    return () => window.clearTimeout(timer);
  }, [panel]);

  useEffect(() => {
    // `build=1` is a one-time entry into the experience builder. Removing it
    // from the URL keeps a reload on the classic structure panel instead of
    // surprising the user (or an existing test) by reopening the builder.
    if (searchParams.get("build") === "1") {
      navigate(`/worlds/${id}`, { replace: true });
    }
  }, [id, navigate, searchParams]);

  useEffect(() => {
    let active = true;
    setAssets([]);
    setAssetError("");
    listObjectAssets(id)
      .then((items) => {
        if (active) {
          setAssets(items);
        }
      })
      .catch(() => {
        if (active) {
          setAssetError(zh ? "无法加载世界文件。" : "The world files could not be loaded.");
        }
      });
    return () => {
      active = false;
    };
  }, [id, zh]);

  useEffect(() => {
    let active = true;
    getWorldCapabilities()
      .then((items) => {
        if (active) {
          setCapabilities(items);
          setCapabilityError("");
        }
      })
      .catch(() => {
        if (active) {
          setCapabilityError(zh ? "无法加载能力目录。" : "The capability catalog could not be loaded.");
        }
      });
    return () => {
      active = false;
    };
  }, [zh]);

  const world = object ? openWorldState(object) : null;
  const preferredView: WorldViewMode = world
    ? view || (world.experience ? "experience" : world.views[0] || "scene")
    : "scene";
  const activeView: WorldViewMode =
    preferredView === "experience" && !world?.experience
      ? world?.views[0] ?? "scene"
      : preferredView;
  const canContribute = object?.permissions.includes("contribute") ?? false;
  const hasExperience = Boolean(world?.experience);
  const navState = (location.state ?? {}) as { created?: boolean; derivedFrom?: string };

  useEffect(() => {
    if (!world) {
      return;
    }
    if (view === "experience") {
      if (!world.experience) {
        setView(world.views[0] ?? "scene");
      }
      return;
    }
    if (view && !world.views.includes(view)) {
      setView(world.experience ? "experience" : world.views[0] ?? "scene");
    }
  }, [world, view]);

  useEffect(() => {
    if (world?.experience && view === "") {
      setView("experience");
    }
  }, [world?.experience, view]);

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

  function openBranchDialog() {
    if (!object) {
      return;
    }
    const source = String(openWorldState(object).title || t("common.untitled"));
    setBranchDraft(branchTitle(source, zh).slice(0, 200));
    setBranchOpen(true);
    setError(null);
  }

  async function deriveBranch() {
    if (!object || branchBusy || busy !== "") {
      return;
    }
    const title = branchDraft.trim();
    if (!title) {
      return;
    }
    setBranchBusy(true);
    setError(null);
    try {
      const sourceTitle = String(openWorldState(object).title || t("common.untitled"));
      const child = await deriveObject(object.id, {
        title: title.slice(0, 200),
        source_version: object.version,
        inheritance_fields: ["premise", "structure", "views"]
      });
      setBranchOpen(false);
      navigate(`/worlds/${child.id}`, { state: { derivedFrom: sourceTitle } });
    } catch (failure) {
      setError(failure);
    } finally {
      setBranchBusy(false);
    }
  }

  async function saveTitle() {
    if (!object || busy !== "") {
      return;
    }
    const title = titleDraft.trim();
    if (!title || title === String(openWorldState(object).title || "")) {
      setTitleEditing(false);
      return;
    }
    const updated = await run("set_title", { title: title.slice(0, 200) });
    if (updated) {
      setTitleEditing(false);
    }
  }

  async function run(action: string, input: Record<string, unknown>) {
    if (!object || busy) {
      return null;
    }
    setBusy(action);
    setError(null);
    setNotice("");
    const before = openWorldState(object);
    try {
      const updated = await applyAction(object.id, action, input, object.version);
      setObject(updated);
      const changes = describeWorldDiff(before, openWorldState(updated), zh);
      setNotice(
        changes.length > 0
          ? zh
            ? `世界回应：${changes.join("；")}`
            : `The world answered: ${changes.join("; ")}`
          : action === "touch_node"
            ? t("openWorld.touched")
            : t("openWorld.saved")
      );
      return updated;
    } catch (failure) {
      setError(failure);
      await load();
      return null;
    } finally {
      setBusy("");
    }
  }

  function invokeBehavior(behaviorID: string, entityID?: string, value?: unknown) {
    const input: Record<string, unknown> = { behavior_id: behaviorID };
    if (entityID) {
      input.entity_id = entityID;
    }
    if (value !== undefined) {
      input.value = value;
    }
    void run("invoke_behavior", input);
  }

  async function saveExperience(experience: WorldExperienceDocument | null): Promise<boolean> {
    const updated = await run("set_experience", { experience });
    return Boolean(updated);
  }

  async function uploadAsset(kind: AssetKind, file: File): Promise<WorldAsset | null> {
    if (!object || busy) {
      return null;
    }
    setBusy(`asset:${kind}`);
    setError(null);
    setNotice("");
    try {
      const created = await uploadObjectAsset(object.id, kind, file);
      setAssets((current) => [created, ...current]);
      setNotice(t("openWorld.saved"));
      return created;
    } catch (failure) {
      setError(failure);
      return null;
    } finally {
      setBusy("");
    }
  }

  async function generateAsset(): Promise<WorldAsset | null> {
    if (!object || busy) {
      return null;
    }
    setBusy("asset:generate");
    setError(null);
    setNotice("");
    try {
      const generated = await generateObjectAsset(object.id, {
        hue: world?.presentation.scene.sky_hue ?? 222,
        hue2: world?.presentation.scene.ground_hue ?? 265,
        lightness: 14
      });
      setAssets((current) => [generated, ...current]);
      setNotice(t("openWorld.saved"));
      return generated;
    } catch (failure) {
      setError(failure);
      return null;
    } finally {
      setBusy("");
    }
  }

  async function removeAsset(assetID: string): Promise<void> {
    if (!object || busy) {
      return;
    }
    setBusy("asset:remove");
    setError(null);
    try {
      await removeObjectAsset(object.id, assetID);
      setAssets((current) => current.filter((asset) => asset.id !== assetID));
      setNotice(t("openWorld.saved"));
    } catch (failure) {
      setError(failure);
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
          {titleEditing ? (
            <div className="dw-open-title-edit">
              <input
                className="dw-input"
                value={titleDraft}
                maxLength={200}
                autoFocus
                aria-label={zh ? "世界名称" : "World title"}
                onChange={(event) => setTitleDraft(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.preventDefault();
                    void saveTitle();
                  } else if (event.key === "Escape") {
                    setTitleEditing(false);
                  }
                }}
              />
              <button className="dw-button dw-button-small dw-button-primary" type="button" onClick={() => void saveTitle()} disabled={busy !== ""}>
                {zh ? "保存" : "Save"}
              </button>
              <button className="dw-button dw-button-small dw-button-ghost" type="button" onClick={() => setTitleEditing(false)}>
                {zh ? "取消" : "Cancel"}
              </button>
            </div>
          ) : (
            <h1 className="dw-open-title">
              {world.title || t("common.untitled")}
              {canContribute ? (
                <button
                  className="dw-open-title-edit-button"
                  type="button"
                  aria-label={zh ? "重命名这个世界" : "Rename this world"}
                  title={zh ? "重命名这个世界" : "Rename this world"}
                  onClick={() => {
                    setTitleDraft(String(world.title || ""));
                    setTitleEditing(true);
                  }}
                >
                  <Icon name="edit" size={15} />
                </button>
              ) : null}
            </h1>
          )}
          <p className="dw-open-summary">
            {t("openWorld.summary", {
              kinds: world.kinds.length,
              nodes: world.nodes.length,
              rules: world.rules.length
            })}
          </p>
        </div>
        <div className="dw-open-head-actions">
          {canContribute ? (
            <button
              className="dw-button dw-button-small dw-button-primary"
              type="button"
              aria-pressed={panel === "ai"}
              data-testid="ai-entry"
              onClick={() => {
                setPanel("ai");
                window.setTimeout(() => {
                  document.getElementById("dw-ai-compose")?.focus();
                }, 60);
              }}
            >
              <Icon name="spark" size={15} />
              {zh ? "和我的 AI 一起创造" : "Create with my AI"}
            </button>
          ) : null}
          <details className="dw-open-advanced">
            <summary className="dw-button dw-button-small dw-button-ghost">
              {zh ? "高级" : "Advanced"}
              <Icon name="chevron" size={13} />
            </summary>
            <div className="dw-open-advanced-menu">
              <button type="button" onClick={() => setPanel("structure")}>
                {zh ? "世界结构" : "World structure"}
              </button>
              <button
                type="button"
                onClick={() => {
                  setExperienceOpened(true);
                  setPanel("experience");
                }}
              >
                {zh ? "体验编辑" : "Experience editor"}
              </button>
              {canContribute ? (
                <button
                  type="button"
                  onClick={() => {
                    setTitleDraft(String(world.title || ""));
                    setTitleEditing(true);
                  }}
                >
                  {zh ? "重命名世界" : "Rename world"}
                </button>
              ) : null}
              {object.permissions.includes("derive") ? (
                <button type="button" onClick={openBranchDialog}>
                  {zh ? "衍生一个新世界" : "Derive a new world"}
                </button>
              ) : null}
            </div>
          </details>
        </div>
      </header>

      {navState.derivedFrom ? (
        <div className="dw-open-origin-banner" role="status">
          <Icon name="branch" size={15} />
          <span>
            {zh
              ? `这是从《${navState.derivedFrom}》衍生的独立世界；原世界没有被修改。`
              : `This is an independent world derived from “${navState.derivedFrom}”; the original was not modified.`}
          </span>
          <Link to={`/objects/${object.id}/lineage`}>{zh ? "查看来源与分支" : "View origin and branches"}</Link>
        </div>
      ) : null}
      {navState.created ? (
        <div className="dw-open-origin-banner is-success" role="status">
          <Icon name="check" size={15} />
          <span>{zh ? "世界已创建。你可以直接开始体验，也可以继续和 AI 一起改变它。" : "Your world is ready. Start experiencing it, or keep changing it with your AI."}</span>
        </div>
      ) : null}

      {panel ? (
        <button
          className="dw-open-panel-close"
          type="button"
          onClick={() => setPanel(null)}
        >
          <Icon name="close" size={14} />
          {zh ? "关闭面板" : "Close panel"}
        </button>
      ) : null}

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

      <div className={`dw-open-layout${panel ? " has-panel" : ""}`}>
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
            {world.experience ? (
              <button
                type="button"
                className={`dw-open-view${activeView === "experience" ? " is-active" : ""}`}
                aria-pressed={activeView === "experience"}
                onClick={() => setView("experience")}
              >
                {t("openWorld.experience")}
              </button>
            ) : null}
            {world.views.map((mode) => (
              <button
                key={mode}
                type="button"
                className={`dw-open-view${activeView === mode ? " is-active" : ""}`}
                aria-pressed={activeView === mode}
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

          {activeView === "experience" && world.experience ? (
            <WorldExperience
              world={world}
              experience={world.experience}
              assetBase={`/api/v1/objects/${object.id}/assets`}
              canContribute={canContribute}
              busy={busy !== ""}
              onInvoke={invokeBehavior}
              onSetField={(entityID, field, value) =>
                void run("set_node_field", { node_id: entityID, field, value })
              }
              onCustom={(surfaceID, event, payload) =>
                void run("custom_action", {
                  surface_id: surfaceID,
                  event,
                  node_id: payload.nodeID,
                  key: payload.key,
                  value: payload.value
                })
              }
            />
          ) : world.nodes.length === 0 ? (
            <div className="dw-open-empty">
              <Icon name="world" size={26} />
              <p>{t("openWorld.empty")}</p>
              {canContribute ? (
                <div className="dw-open-empty-actions">
                  <button
                    className="dw-button dw-button-primary dw-button-small"
                    type="button"
                    onClick={() => {
                      setPanel("ai");
                      window.setTimeout(() => document.getElementById("dw-ai-compose")?.focus(), 60);
                    }}
                  >
                    <Icon name="spark" size={15} />
                    {zh ? "和我的 AI 一起创造" : "Create with my AI"}
                  </button>
                  <button
                    className="dw-button dw-button-ghost dw-button-small"
                    type="button"
                    onClick={() => setPanel("structure")}
                  >
                    {zh ? "手动定义（高级）" : "Define manually (advanced)"}
                  </button>
                </div>
              ) : null}
            </div>
          ) : activeView === "scene" ? (
            <WorldScene
              world={world}
              worldID={object.id}
              canContribute={canContribute}
              busy={busy !== ""}
              onTouch={(nodeID) => void run("touch_node", { node_id: nodeID })}
            />
          ) : activeView === "stage" ? (
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
          ) : activeView === "cards" ? (
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

        {panel ? (
        <aside className="dw-open-panel">
          {panel === "ai" ? (
            <AIPanel object={object} canContribute={canContribute} onChanged={() => void load()} />
          ) : panel === "structure" ? (
            <StructurePanel
              key={object.id}
              world={world}
              busy={busy !== ""}
              canContribute={canContribute}
              hasExperience={hasExperience}
              onAddKind={(input) => void run("add_kind", input)}
              onUpdateKind={(input) => void run("update_kind", input)}
              onRemoveKind={(kindID) => void run("remove_kind", { kind_id: kindID })}
              onAddNode={(input) => void run("add_node", input)}
              onRemoveNode={(nodeID) => void run("remove_node", { node_id: nodeID })}
              onAddLink={(input) => void run("add_link", input)}
              onRemoveLink={(linkID) => void run("remove_link", { link_id: linkID })}
              onAddRule={(input) => void run("add_rule", input)}
              onRemoveRule={(ruleID) => void run("remove_rule", { rule_id: ruleID })}
              onSetPresentation={(presentation) => void run("set_presentation", { presentation })}
            />
          ) : null}
          {experienceOpened ? (
            <div hidden={panel !== "experience"} data-testid="world-experience-panel">
              {assetError ? (
                <p className="dw-form-error" role="alert">
                  {assetError}
                </p>
              ) : null}
              {capabilityError ? (
                <p className="dw-form-error" role="alert">
                  {capabilityError}
                </p>
              ) : capabilities.length === 0 ? (
                <p className="dw-page-loading" role="status">
                  {t("common.loading")}
                </p>
              ) : (
                <WorldExperienceEditor
                  key={`${object.id}:${object.version}`}
                  world={world}
                  objectID={object.id}
                  assets={assets}
                  canManage={object.permissions.includes("manage")}
                  catalog={capabilities}
                  busy={busy !== ""}
                  canContribute={canContribute}
                  onSave={saveExperience}
                  onUploadAsset={uploadAsset}
                  onGenerateAsset={generateAsset}
                  onRemoveAsset={removeAsset}
                />
              )}
            </div>
          ) : null}
        </aside>
        ) : null}
      </div>

      <Modal
        open={branchOpen}
        onClose={() => setBranchOpen(false)}
        title={zh ? "衍生一个新世界" : "Derive a new world"}
        kicker={zh ? "独立分支" : "INDEPENDENT BRANCH"}
        actions={
          <>
            <button className="dw-button dw-button-ghost" type="button" onClick={() => setBranchOpen(false)} disabled={branchBusy}>
              {zh ? "取消" : "Cancel"}
            </button>
            <button
              className="dw-button dw-button-primary"
              type="button"
              onClick={() => void deriveBranch()}
              disabled={branchBusy || !branchDraft.trim() || busy !== ""}
            >
              <Icon name="branch" size={15} />
              {branchBusy ? (zh ? "正在衍生…" : "Deriving…") : zh ? "创建这个分支" : "Create this branch"}
            </button>
          </>
        }
      >
        <div className="dw-form">
          <p className="dw-modal-lead">
            {zh
              ? `会从《${String(world.title || t("common.untitled"))}》的当前版本创建一个独立的新世界。原世界不会被修改。`
              : `This creates an independent new world from the current version of “${String(world.title || t("common.untitled"))}”. The original is not modified.`}
          </p>
          <label className="dw-field-label" htmlFor="dw-branch-title">
            {zh ? "新世界的名字" : "Name of the new world"}
          </label>
          <input
            id="dw-branch-title"
            className="dw-input"
            value={branchDraft}
            maxLength={200}
            onChange={(event) => setBranchDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                void deriveBranch();
              }
            }}
          />
          <p className="dw-fineprint">
            <Icon name="branch" size={13} />
            {zh
              ? "它会继承当前的前提、结构与视图；之后两者各自独立演化。"
              : "It inherits the current premise, structure and views; from then on both worlds evolve independently."}
          </p>
          <Link className="dw-text-button" to={`/objects/${object.id}/derive`} onClick={() => setBranchOpen(false)}>
            {zh ? "需要更多选项？打开完整衍生页" : "Need more options? Open the full derive page"}
          </Link>
        </div>
      </Modal>
    </div>
  );
}

function describeWorldDiff(before: OpenWorldState, after: OpenWorldState, zh: boolean): string[] {
  const previous = new Map(before.nodes.map((node) => [node.id, node]));
  const kinds = new Map(after.kinds.map((kind) => [kind.id, kind]));
  const changes: string[] = [];
  for (const node of after.nodes) {
    const old = previous.get(node.id);
    const kind = kinds.get(node.kind_id);
    if (!old) {
      changes.push(zh ? `${node.name} 出现` : `${node.name} appeared`);
      continue;
    }
    if (old.status !== node.status && node.status === "done") {
      changes.push(zh ? `${node.name} 被唤醒` : `${node.name} woke`);
    }
    for (const [key, value] of Object.entries(node.values)) {
      if (old.values[key] !== value) {
        const field = kind?.fields.find((entry) => entry.key === key);
        changes.push(
          `${node.name} · ${field?.label || key}: ${valueLabel(old.values[key])} → ${valueLabel(value)}`
        );
      }
    }
  }
  return changes.slice(0, 5);
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
          {value === true ? t("openWorld.on") : t("openWorld.off")}
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
  hasExperience,
  onAddKind,
  onUpdateKind,
  onRemoveKind,
  onAddNode,
  onRemoveNode,
  onAddLink,
  onRemoveLink,
  onAddRule,
  onRemoveRule,
  onSetPresentation
}: {
  world: OpenWorldState;
  busy: boolean;
  canContribute: boolean;
  hasExperience: boolean;
  onAddKind: (input: Record<string, unknown>) => void;
  onUpdateKind: (input: Record<string, unknown>) => void;
  onRemoveKind: (kindID: string) => void;
  onAddNode: (input: Record<string, unknown>) => void;
  onRemoveNode: (nodeID: string) => void;
  onAddLink: (input: Record<string, unknown>) => void;
  onRemoveLink: (linkID: string) => void;
  onAddRule: (input: Record<string, unknown>) => void;
  onRemoveRule: (ruleID: string) => void;
  onSetPresentation: (presentation: OpenWorldPresentation) => void;
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
  const [nodeX, setNodeX] = useState("");
  const [nodeY, setNodeY] = useState("");
  const [kindShape, setKindShape] = useState("orb");
  const [kindMotion, setKindMotion] = useState("float");
  const [linkFrom, setLinkFrom] = useState("");
  const [linkTo, setLinkTo] = useState("");
  const [linkLabel, setLinkLabel] = useState("");
  const [ruleTrigger, setRuleTrigger] = useState("");
  const [conditionNode, setConditionNode] = useState("");
  const [conditionField, setConditionField] = useState("status");
  const [conditionOp, setConditionOp] = useState("eq");
  const [conditionValue, setConditionValue] = useState("");
  const [effectTarget, setEffectTarget] = useState("");
  const [effectField, setEffectField] = useState("status");
  const [effectOp, setEffectOp] = useState<"set" | "add" | "toggle">("set");
  const [effectValue, setEffectValue] = useState("done");
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
    onAddKind({
      name: kindName.trim(),
      hue: kindHue,
      fields,
      appearance: {
        shape: kindShape,
        size: 48,
        motion: kindMotion,
        intensity: 50,
        glow: 40,
        label: "always",
        layer: "mid"
      }
    });
    setKindName("");
    setKindHue(168);
    setFields([]);
  }

  function submitNode(event: FormEvent) {
    event.preventDefault();
    if (!nodeKind || !nodeName.trim()) {
      return;
    }
    const input: Record<string, unknown> = {
      kind_id: nodeKind,
      name: nodeName.trim(),
      text: nodeText
    };
    if (nodeX.trim() !== "") {
      const x = Number(nodeX);
      if (Number.isFinite(x)) {
        input.x = x;
      }
    }
    if (nodeY.trim() !== "") {
      const y = Number(nodeY);
      if (Number.isFinite(y)) {
        input.y = y;
      }
    }
    onAddNode(input);
    setNodeName("");
    setNodeText("");
    setNodeX("");
    setNodeY("");
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
        value = effectValue === "open" ? "open" : "done";
      }
    } else if (targetField?.type === "number") {
      const parsed = Number(effectValue);
      if (!Number.isFinite(parsed)) {
        return;
      }
      value = parsed;
    } else if (targetField?.type === "flag") {
      if (effectOp === "set") {
        value = effectValue !== "false";
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
    const payload: Record<string, unknown> = { trigger_id: ruleTrigger, effects: [effect] };
    if (conditionNode) {
      const condition: Record<string, unknown> = {
        node_id: conditionNode,
        field: conditionField,
        op: conditionOp
      };
      if (conditionOp !== "truthy" && conditionOp !== "falsy") {
        if (conditionField === "status") {
          condition.value = conditionValue === "done" ? "done" : "open";
        } else if (conditionFieldDef?.type === "number") {
          const parsed = Number(conditionValue);
          if (!Number.isFinite(parsed)) {
            return;
          }
          condition.value = parsed;
        } else if (conditionFieldDef?.type === "flag") {
          condition.value = conditionValue === "true";
        } else {
          condition.value = conditionValue;
        }
      }
      payload.condition = condition;
    }
    onAddRule(payload);
  }

  const effectFields = targetKind?.fields ?? [];
  const conditionTarget = world.nodes.find((node) => node.id === conditionNode);
  const conditionKind = conditionTarget ? kindFor(world, conditionTarget.kind_id) : undefined;
  const conditionFieldDef = conditionField === "status"
    ? undefined
    : conditionKind?.fields.find((field) => field.key === conditionField);
  const conditionOps = conditionField === "status"
    ? ["eq", "ne"]
    : conditionFieldDef?.type === "number"
      ? ["eq", "ne", "gt", "gte", "lt", "lte"]
      : conditionFieldDef?.type === "flag"
        ? ["eq", "ne", "truthy", "falsy"]
        : ["eq", "ne"];
  const conditionNeedsValue = conditionOp !== "truthy" && conditionOp !== "falsy";

  return (
    <div className="dw-open-structure" data-testid="open-world-structure">
      {hasExperience ? (
        <p className="dw-open-hint">{t("openWorld.classicLookHidden")}</p>
      ) : (
        <WorldLookEditor
          world={world}
          busy={busy}
          canContribute={canContribute}
          onSetPresentation={onSetPresentation}
          onUpdateKind={(kindID, appearance, bindings) =>
            onUpdateKind({ kind_id: kindID, appearance, bindings })
          }
        />
      )}
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
              <div className="dw-open-inline-form">
                <select
                  id="ow-kind-shape"
                  className="dw-input"
                  aria-label={t("openWorld.shape")}
                  value={kindShape}
                  onChange={(event) => setKindShape(event.target.value)}
                >
                  {(["orb", "beacon", "wave", "shard", "ring", "glyph", "bloom", "thread"] as const).map((shape) => (
                    <option key={shape} value={shape}>
                      {t(`openWorld.shape.${shape}` as never)}
                    </option>
                  ))}
                </select>
                <select
                  id="ow-kind-motion"
                  className="dw-input"
                  aria-label={t("openWorld.motion")}
                  value={kindMotion}
                  onChange={(event) => setKindMotion(event.target.value)}
                >
                  {(["none", "pulse", "float", "sway", "flicker", "breathe", "orbit"] as const).map((motion) => (
                    <option key={motion} value={motion}>
                      {t(`openWorld.motion.${motion}` as never)}
                    </option>
                  ))}
                </select>
              </div>
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
              <div className="dw-open-inline-form">
                <input
                  id="ow-node-x"
                  className="dw-input"
                  type="number"
                  placeholder={t("openWorld.positionX")}
                  aria-label={t("openWorld.positionX")}
                  value={nodeX}
                  onChange={(event) => setNodeX(event.target.value)}
                />
                <input
                  id="ow-node-y"
                  className="dw-input"
                  type="number"
                  placeholder={t("openWorld.positionY")}
                  aria-label={t("openWorld.positionY")}
                  value={nodeY}
                  onChange={(event) => setNodeY(event.target.value)}
                />
              </div>
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
              <label className="dw-field-label" htmlFor="ow-rule-condition">{t("openWorld.condition")}</label>
              <select
                id="ow-rule-condition"
                className="dw-input"
                value={conditionNode}
                onChange={(event) => {
                  setConditionNode(event.target.value);
                  setConditionField("status");
                  setConditionOp("eq");
                  setConditionValue("done");
                }}
              >
                <option value="">{t("openWorld.conditionAlways")}</option>
                {world.nodes.map((node) => (
                  <option key={node.id} value={node.id}>{node.name}</option>
                ))}
              </select>
              {conditionNode ? (
                <>
                  <label className="dw-field-label" htmlFor="ow-condition-field">{t("openWorld.conditionField")}</label>
                  <select
                    id="ow-condition-field"
                    className="dw-input"
                    value={conditionField}
                    onChange={(event) => {
                      const next = event.target.value;
                      setConditionField(next);
                      setConditionOp("eq");
                      const field = conditionKind?.fields.find((entry) => entry.key === next);
                      setConditionValue(next === "status" ? "done" : field?.type === "flag" ? "false" : "");
                    }}
                  >
                    <option value="status">{t("openWorld.statusField")}</option>
                    {conditionKind?.fields.map((field) => (
                      <option key={field.key} value={field.key}>{field.label || field.key}</option>
                    ))}
                  </select>
                  <label className="dw-field-label" htmlFor="ow-condition-op">{t("openWorld.conditionOp")}</label>
                  <select
                    id="ow-condition-op"
                    className="dw-input"
                    value={conditionOp}
                    onChange={(event) => setConditionOp(event.target.value)}
                  >
                    {conditionOps.map((op) => (
                      <option key={op} value={op}>{t(`openWorld.cond.${op}` as never)}</option>
                    ))}
                  </select>
                  {conditionNeedsValue ? (
                    conditionField === "status" ? (
                      <select
                        id="ow-condition-value"
                        className="dw-input"
                        value={conditionValue === "done" ? "done" : "open"}
                        onChange={(event) => setConditionValue(event.target.value)}
                      >
                        <option value="done">{t("openWorld.status.done")}</option>
                        <option value="open">{t("openWorld.status.open")}</option>
                      </select>
                    ) : conditionFieldDef?.type === "flag" ? (
                      <select
                        id="ow-condition-value"
                        className="dw-input"
                        value={conditionValue === "true" ? "true" : "false"}
                        onChange={(event) => setConditionValue(event.target.value)}
                      >
                        <option value="true">{t("openWorld.on")}</option>
                        <option value="false">{t("openWorld.off")}</option>
                      </select>
                    ) : (
                      <input
                        id="ow-condition-value"
                        className="dw-input"
                        type={conditionFieldDef?.type === "number" ? "number" : "text"}
                        value={conditionValue}
                        onChange={(event) => setConditionValue(event.target.value)}
                      />
                    )
                  ) : null}
                </>
              ) : null}
              <label className="dw-field-label" htmlFor="ow-effect-target">{t("openWorld.effectTarget")}</label>
              <select
                id="ow-effect-target"
                className="dw-input"
                value={effectTarget}
                onChange={(event) => {
                  setEffectTarget(event.target.value);
                  setEffectField("status");
                  setEffectValue("done");
                }}
              >
                {world.nodes.map((node) => (
                  <option key={node.id} value={node.id}>{node.name}</option>
                ))}
              </select>
              <label className="dw-field-label" htmlFor="ow-effect-field">{t("openWorld.effectField")}</label>
              <select
                id="ow-effect-field"
                className="dw-input"
                value={effectField}
                onChange={(event) => {
                  const next = event.target.value;
                  setEffectField(next);
                  const nextField = effectFields.find((field) => field.key === next);
                  setEffectValue(next === "status" ? "done" : nextField?.type === "flag" ? "true" : "");
                }}
              >
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
                      <option value="true">{t("openWorld.on")}</option>
                      <option value="false">{t("openWorld.off")}</option>
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
