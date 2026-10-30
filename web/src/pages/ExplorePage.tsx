// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router";
import {
  derivePublicWorld,
  errorMessage,
  getPublicLineage,
  getPublicWorld,
  listPublicWorlds,
  reportPublicWorld,
  type PublicLineage,
  type PublicWorld
} from "../api";
import { ErrorNotice } from "../errors";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";
import Icon from "../components/v2/Icon";
import Modal from "../components/v2/Modal";
import { useToast } from "../components/v2/Toast";
import { branchTitle } from "../worldLabels";

type Tone = "mint" | "violet" | "peach" | "blue";

function toneFor(value: string): Tone {
  let hash = 0;
  for (let index = 0; index < value.length; index += 1) {
    hash = (hash * 31 + value.charCodeAt(index)) >>> 0;
  }
  return (["violet", "blue", "mint", "peach"] as Tone[])[hash % 4] ?? "violet";
}

function publicPreview(world: PublicWorld): {
  title: string;
  body: string;
  elements: string[];
  purpose: string;
} {
  const snapshot = (world.snapshot?.state ?? {}) as Record<string, unknown>;
  const title = String(snapshot.title ?? world.title);
  let body = "";
  if (typeof snapshot.premise === "string") {
    body = snapshot.premise;
  } else if (typeof snapshot.summary === "string") {
    body = snapshot.summary;
  } else if (typeof snapshot.prompt === "string") {
    body = snapshot.prompt;
  } else if (typeof snapshot.body === "string") {
    body = snapshot.body;
  }
  if (!body && typeof world.summary === "string") {
    body = world.summary;
  }
  const elements = Array.isArray(snapshot.elements)
    ? (snapshot.elements as Array<Record<string, unknown>>)
        .map((element) => String(element.text ?? ""))
        .filter(Boolean)
    : Array.isArray(snapshot.nodes)
      ? (snapshot.nodes as Array<Record<string, unknown>>)
          .map((node) => String(node.name ?? node.text ?? ""))
          .filter(Boolean)
      : Array.isArray(snapshot.checklist)
        ? (snapshot.checklist as Array<Record<string, unknown>>)
            .map((item) => String(item.text ?? ""))
            .filter(Boolean)
        : [];
  const purpose = typeof snapshot.purpose === "string" ? snapshot.purpose : "";
  return { title, body, elements, purpose };
}

function fieldsFor(world: PublicWorld): string[] {
  if (world.object_type === "interactive") {
    return ["prompt", "options"];
  }
  if (world.object_type === "living_scene") {
    return ["premise", "elements", "tags"];
  }
  if (world.object_type === "open_world") {
    return ["premise", "structure", "views", "tags"];
  }
  return ["summary", "body", "checklist", "tags"];
}

function typeLabel(world: PublicWorld, zh: boolean): string {
  if (world.object_type === "open_world") {
    return zh ? "自由世界" : "Free world";
  }
  if (world.object_type === "living_scene") {
    return zh ? "活场景" : "Living scene";
  }
  if (world.object_type === "interactive") {
    return zh ? "互动选择" : "Interactive";
  }
  return zh ? "共同创造" : "Collaborative";
}

export default function ExplorePage() {
  const { id } = useParams();
  const [searchParams] = useSearchParams();
  const { t, lang } = useI18n();
  const { status } = useAuth();
  const navigate = useNavigate();
  const zh = lang === "zh";
  const { view: toastView } = useToast();
  const [worlds, setWorlds] = useState<PublicWorld[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [featuredID, setFeaturedID] = useState("");
  const [selected, setSelected] = useState<PublicWorld | null>(null);
  const [lineage, setLineage] = useState<PublicLineage | null>(null);
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [rulesOpen, setRulesOpen] = useState(false);
  const [deriveOpen, setDeriveOpen] = useState(false);
  const [deriveTitle, setDeriveTitle] = useState("");
  const [deriveFields, setDeriveFields] = useState<string[]>([]);
  const [deriveBusy, setDeriveBusy] = useState(false);
  const [deriveError, setDeriveError] = useState("");
  const [reportNotice, setReportNotice] = useState("");

  const load = useCallback(async (cursor?: string, signal?: AbortSignal) => {
    setLoading(true);
    setError(null);
    try {
      const page = await listPublicWorlds(cursor, signal);
      if (signal?.aborted) {
        return;
      }
      setWorlds((previous) => (cursor ? [...previous, ...page.items] : page.items));
      setNextCursor(page.next_cursor);
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
    void load(undefined, controller.signal);
    return () => controller.abort();
  }, [load]);

  useEffect(() => {
    if (worlds.length === 0) {
      setFeaturedID("");
      return;
    }
    setFeaturedID((current) =>
      current && worlds.some((world) => world.id === current) ? current : worlds[0]?.id ?? ""
    );
  }, [worlds]);

  useEffect(() => {
    if (!id) {
      setSelected(null);
      setLineage(null);
      setDeriveOpen(false);
      return;
    }
    const controller = new AbortController();
    setDetailLoading(true);
    setError(null);
    setReportNotice("");
    Promise.all([
      getPublicWorld(id, controller.signal),
      getPublicLineage(id, controller.signal)
    ])
      .then(([world, family]) => {
        if (controller.signal.aborted) {
          return;
        }
        setSelected(world);
        setLineage(family);
        const preview = publicPreview(world);
        setDeriveTitle(branchTitle(preview.title, zh).slice(0, 200));
        setDeriveFields(fieldsFor(world));
        if (searchParams.get("derive") === "1" && world.derive_allowed) {
          setDeriveOpen(true);
        }
      })
      .catch((failure) => {
        if (!controller.signal.aborted) {
          setError(failure);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setDetailLoading(false);
        }
      });
    return () => controller.abort();
  }, [id, zh]);

  const featured = useMemo(
    () => worlds.find((world) => world.id === featuredID) ?? worlds[0] ?? null,
    [featuredID, worlds]
  );
  const featuredPreview = featured ? publicPreview(featured) : null;

  async function report() {
    if (!id) {
      return;
    }
    const reason = window.prompt(t("explore.reportPrompt"));
    if (!reason || !reason.trim()) {
      return;
    }
    setError(null);
    try {
      await reportPublicWorld(id, reason.trim());
      setReportNotice(t("explore.reportThanks"));
    } catch (failure) {
      setError(failure);
    }
  }

  async function derive(event: FormEvent) {
    event.preventDefault();
    if (!selected || !id) {
      return;
    }
    if (!deriveTitle.trim() || deriveFields.length === 0) {
      setDeriveError(zh ? "请填写新分支名称，并至少继承一个字段。" : "Name the branch and inherit at least one field.");
      return;
    }
    setDeriveBusy(true);
    setDeriveError("");
    try {
      const child = await derivePublicWorld(id, {
        title: deriveTitle.trim(),
        inheritance_fields: deriveFields
      });
      try {
        window.sessionStorage.setItem("dw:focus-object", child.id);
      } catch {
        // The world still loads without the focus hint.
      }
      // Only an open world has a world-page experience. Other published forms
      // keep the accepted V2 landing on the stage with the child focused.
      if (child.object_type === "open_world") {
        navigate(`/worlds/${child.id}`, {
          state: { derivedFrom: publicPreview(selected).title }
        });
      } else {
        navigate("/");
      }
    } catch (failure) {
      setDeriveError(errorMessage(failure));
    } finally {
      setDeriveBusy(false);
    }
  }

  function toggleField(field: string) {
    setDeriveFields((current) =>
      current.includes(field) ? current.filter((item) => item !== field) : [...current, field]
    );
  }

  if (id) {
    const preview = selected ? publicPreview(selected) : null;
    return (
      <div className={`dw-page dw-explore-page tone-${selected ? toneFor(selected.id) : "violet"}`} data-testid="explore-detail">
        <header className="dw-page-head">
          <p className="dw-eyebrow">
            <span className="dw-eyebrow-line" />
            FROM SOMEONE ELSE&apos;S IMAGINATION
          </p>
          <div className="dw-heading-row">
            <div className="dw-head-left">
              <h1>
                {detailLoading
                  ? zh
                    ? "正在进入世界…"
                    : "Entering the world…"
                  : zh
                    ? "进入一个公开世界。"
                    : "Enter a public world."}
              </h1>
              {selected ? (
                <p className="dw-lead">
                  {zh
                    ? "作者主动公开了这个版本。你可以阅读它，但私人内容与 AI 上下文不会被公开。"
                    : "The author chose to publish this version. You can read it; private content and AI context are not exposed."}
                </p>
              ) : null}
              {selected ? (
                <p className="dw-public-meta">
                  <span>{typeLabel(selected, zh)}</span>
                  <span>v{selected.source_version}</span>
                  <span>{t("explore.by", { name: selected.publisher_name })}</span>
                  <span>{selected.license}</span>
                  <span className={selected.derive_allowed ? "is-open" : ""}>
                    {selected.derive_allowed ? (zh ? "允许衍生" : "Derivation allowed") : zh ? "仅浏览" : "View only"}
                  </span>
                </p>
              ) : null}
            </div>
            <div className="dw-head-actions">
              <Link className="dw-button dw-button-ghost" to="/explore">
                <Icon name="arrow" size={15} />
                {t("explore.back")}
              </Link>
            </div>
          </div>
        </header>

        {error ? <ErrorNotice error={error} onRetry={() => void load()} /> : null}
        {reportNotice ? <p className="dw-form-success" role="status">{reportNotice}</p> : null}

        {selected && preview ? (
          <div className="dw-public-detail-grid">
            <article className="dw-public-stage" aria-label={preview.title}>
              <div className="dw-public-art" aria-hidden="true">
                <span className="dw-public-orb" />
                <span className="dw-public-ring" />
                <span className="dw-public-glow" />
              </div>
              <div className="dw-public-stage-body">
                <p className="dw-eyebrow">A PUBLIC VERSION · CHOSEN BY ITS AUTHOR</p>
                <h2>{preview.title}</h2>
                {preview.body ? <p className="dw-public-premise">{preview.body}</p> : null}
                {preview.elements.length > 0 ? (
                  <ol className="dw-public-elements">
                    {preview.elements.slice(0, 8).map((element, index) => (
                      <li key={`${index}-${element}`}>
                        <span aria-hidden="true">{String(index + 1).padStart(2, "0")}</span>
                        {element}
                      </li>
                    ))}
                  </ol>
                ) : (
                  <p className="dw-empty-note">
                    {zh ? "这个公开版本没有可见的文字内容。" : "This published version has no visible text content."}
                  </p>
                )}
              </div>
              <div className="dw-public-stage-foot">
                <span>
                  <Icon name="shield" size={14} />
                  {zh ? "这是作者主动选择的公开版本" : "A version its author chose to publish"}
                </span>
                <button type="button" className="dw-text-button" onClick={() => void report()}>
                  {zh ? "报告这个世界" : "Report this world"}
                </button>
              </div>
            </article>

            <aside className="dw-public-aside">
              <section className="dw-public-aside-card">
                <h2>{t("explore.lineageTitle")}</h2>
                {lineage?.source ? (
                  <p className="dw-muted-line">
                    {zh
                      ? `源自「${lineage.source.title}」v${lineage.source.source_version}`
                      : `From “${lineage.source.title}” v${lineage.source.source_version}`}
                  </p>
                ) : (
                  <p className="dw-muted-line">
                    {zh ? "这是一个公开版本，没有更早的公开来源。" : "This public version has no earlier public source."}
                  </p>
                )}
                {lineage?.derived.length ? (
                  <div className="dw-public-lineage-list">
                    <p className="dw-field-label">{zh ? "公开衍生" : "Public branches"}</p>
                    {lineage.derived.map((child) => (
                      <Link key={child.publication_id} to={`/explore/${child.publication_id}`}>
                        <Icon name="branch" size={14} />
                        {child.title}
                      </Link>
                    ))}
                  </div>
                ) : null}
              </section>

              <section className="dw-public-aside-card">
                <h2>{zh ? "创造你的独立版本" : "Create your own version"}</h2>
                <p className="dw-muted-line">
                  {zh
                    ? "新分支默认私人，拥有独立的所有者、边界和未来。它不会继承原作者的成员、私人内容或 AI 上下文。"
                    : "A new branch is private by default, with its own owner, boundary, and future. It inherits no members, private content, or AI context from the author."}
                </p>
                {selected.derive_allowed ? (
                  status === "authenticated" ? (
                    <button className="dw-button dw-button-primary dw-full-button" type="button" onClick={() => setDeriveOpen(true)}>
                      <Icon name="branch" size={16} />
                      {zh ? "创造我的分支" : "Create my branch"}
                    </button>
                  ) : (
                    <Link className="dw-button dw-button-primary dw-full-button" to={`/login?next=${encodeURIComponent(`/explore/${id}`)}`}>
                      {zh ? "登录后创造分支" : "Sign in to derive"}
                    </Link>
                  )
                ) : (
                  <p className="dw-fineprint">
                    <Icon name="eye" size={13} />
                    {zh ? "作者只允许浏览，未允许衍生。" : "The author allows viewing only, not derivation."}
                  </p>
                )}
              </section>

              <section className="dw-public-aside-card dw-public-rules-card">
                <h2>{zh ? "公开与衍生规则" : "Publication rules"}</h2>
                <p>
                  <Icon name="lock" size={14} />
                  {zh ? "私人是默认，公开必须是主动选择。" : "Private is the default; publishing is always a deliberate choice."}
                </p>
                <p>
                  <Icon name="eye" size={14} />
                  {zh ? "浏览与衍生分开授权。" : "Viewing and derivation are separate permissions."}
                </p>
                <p>
                  <Icon name="branch" size={14} />
                  {zh ? "每条衍生都是独立世界。" : "Every derived branch is an independent world."}
                </p>
              </section>
            </aside>
          </div>
        ) : detailLoading ? (
          <div className="dw-page-loading">{zh ? "正在进入这个世界…" : "Entering this world…"}</div>
        ) : null}

        <Modal
          open={deriveOpen}
          onClose={() => setDeriveOpen(false)}
          title={zh ? "从这个公开版本创造你的分支" : "Derive your branch from this public version"}
          kicker={zh ? "YOUR INDEPENDENT BRANCH" : "YOUR INDEPENDENT BRANCH"}
          wide
          actions={
            <>
              <button className="dw-button dw-button-ghost" type="button" onClick={() => setDeriveOpen(false)}>
                {zh ? "返回" : "Back"}
              </button>
              <button className="dw-button dw-button-primary" type="submit" form="dw-derive-form" disabled={deriveBusy}>
                {deriveBusy ? (zh ? "正在创造…" : "Creating…") : zh ? "创造我的分支" : "Create my branch"}
              </button>
            </>
          }
        >
          <form id="dw-derive-form" className="dw-form" onSubmit={derive}>
            <p className="dw-modal-lead">
              {zh
                ? "新分支会保留你选择的公开内容作为来源，但默认属于你的私人空间。原作者不会获得你的私人内容或 AI 上下文。"
                : "The new branch keeps the public content you choose as its source, but belongs to your private space by default. The author receives none of your private content or AI context."}
            </p>
            <label className="dw-field-label" htmlFor="dw-derive-title">
              {zh ? "新分支名称" : "Branch title"}
            </label>
            <input
              id="dw-derive-title"
              className="dw-input"
              maxLength={200}
              value={deriveTitle}
              onChange={(event) => setDeriveTitle(event.target.value)}
              autoFocus
            />
            <fieldset className="dw-derive-fields">
              <legend>{zh ? "继承哪些公开内容" : "Which public fields to inherit"}</legend>
              {selected ? fieldsFor(selected).map((field) => (
                <label key={field} className="dw-check-row">
                  <input
                    type="checkbox"
                    checked={deriveFields.includes(field)}
                    onChange={() => toggleField(field)}
                  />
                  <span>{t(`derive.field.${field}` as never)}</span>
                </label>
              )) : null}
            </fieldset>
            {deriveError ? <p className="dw-form-error" role="alert">{deriveError}</p> : null}
          </form>
        </Modal>
        {toastView}
      </div>
    );
  }

  return (
    <div className="dw-page dw-explore-page" data-testid="explore-page">
      <header className="dw-page-head">
        <p className="dw-eyebrow">
          <span className="dw-eyebrow-line" />
          OPEN WORLDS, ENDLESS FUTURES
        </p>
        <div className="dw-heading-row">
          <div className="dw-head-left">
            <h1>
              {zh ? (
                <>
                  别人的世界，<em>也可以是你的起点。</em>
                </>
              ) : (
                <>
                  Someone else&apos;s world <em>can be your beginning.</em>
                </>
              )}
            </h1>
            <p className="dw-lead">
              {zh
                ? "每个人决定是否公开。被允许的作品可以探索，并衍生出属于你的新方向。"
                : "Each person decides what becomes public. Where derivation is allowed, a public world can become your own new direction."}
            </p>
          </div>
          <div className="dw-head-actions">
            <button className="dw-button dw-button-ghost" type="button" onClick={() => setRulesOpen(true)}>
              <Icon name="help" size={16} />
              {zh ? "公开与衍生规则" : "Publication rules"}
            </button>
          </div>
        </div>
      </header>

      {error ? <ErrorNotice error={error} onRetry={() => void load()} /> : null}

      {featured && featuredPreview ? (
        <>
          <div className="dw-explore-grid">
          <div className={`dw-explore-hero tone-${toneFor(featured.id)}`}>
            <div className="dw-explore-hero-art" aria-hidden="true">
              <span className="dw-explore-moon" />
              <span className="dw-explore-ring" />
              <span className="dw-explore-island" />
            </div>
            <div className="dw-explore-hero-content">
              <p className="dw-eyebrow">FROM SOMEONE ELSE&apos;S IMAGINATION</p>
              <h2>{featuredPreview.title}</h2>
              <p>
                {featuredPreview.body ||
                  (zh
                    ? "作者主动公开了这个世界的一个版本。你可以进入查看，或在允许时创造自己的分支。"
                    : "The author published one version of this world. Enter to view it, or derive your own when allowed.")}
              </p>
              <div className="dw-explore-meta">
                <span>{t("explore.by", { name: featured.publisher_name })}</span>
                <span>{featured.derive_allowed ? (zh ? "允许衍生" : "Derivable") : zh ? "仅浏览" : "View only"}</span>
                <span>v{featured.source_version}</span>
              </div>
              <div className="dw-explore-hero-actions">
                <Link className="dw-button dw-button-primary" to={`/explore/${featured.id}`}>
                  {zh ? "进入这个世界" : "Enter this world"}
                  <Icon name="arrow" size={15} />
                </Link>
                {featured.derive_allowed && status === "authenticated" ? (
                  <Link className="dw-button dw-button-ghost" to={`/explore/${featured.id}?derive=1`}>
                    <Icon name="branch" size={15} />
                    {zh ? "创造我的分支" : "Create my branch"}
                  </Link>
                ) : null}
              </div>
            </div>
          </div>

          <div className="dw-explore-aside">
            <div className="dw-explore-aside-head">
              <h2>{zh ? "看见别人的可能性" : "See other possibilities"}</h2>
              <span>{zh ? "主动公开 · 真实数据" : "Chosen public · real data"}</span>
            </div>
            <div className="dw-explore-tiles">
              {worlds
                .filter((world) => world.id !== featured.id)
                .slice(0, 3)
                .map((world) => {
                  const preview = publicPreview(world);
                  return (
                    <button
                      key={world.id}
                      type="button"
                      className={`dw-explore-tile tone-${toneFor(world.id)}`}
                      onClick={() => setFeaturedID(world.id)}
                      aria-label={zh ? `探索 ${preview.title}` : `Explore ${preview.title}`}
                    >
                      <Icon name="branch" size={16} />
                      <strong>{preview.title}</strong>
                      <small>
                        {t("explore.by", { name: world.publisher_name })} ·{" "}
                        {world.derive_allowed ? (zh ? "允许衍生" : "Derivable") : zh ? "仅浏览" : "View only"}
                      </small>
                    </button>
                  );
                })}
            </div>
          </div>
          </div>

          {worlds.length > 1 ? (
            <div className="dw-explore-more">
              <span className="dw-activity-label">{zh ? "更多公开世界" : "More public worlds"}</span>
              {worlds.map((world) => (
                <Link key={world.id} className="dw-activity-chip" to={`/explore/${world.id}`}>
                  <Icon name="globe" size={13} />
                  {publicPreview(world).title}
                </Link>
              ))}
              {nextCursor ? (
                <button className="dw-text-button" type="button" onClick={() => void load(nextCursor)} disabled={loading}>
                  {loading ? (zh ? "正在加载…" : "Loading…") : zh ? "加载更多" : "Load more"}
                </button>
              ) : null}
            </div>
          ) : null}
        </>
      ) : loading ? (
        <div className="dw-page-loading">{zh ? "正在寻找公开世界…" : "Finding public worlds…"}</div>
      ) : (
        <div className="dw-explore-empty">
          <span className="dw-explore-empty-symbol" aria-hidden="true">
            <Icon name="globe" size={26} />
          </span>
          <h2>{zh ? "还没有人主动公开世界。" : "No one has published a world yet."}</h2>
          <p>
            {zh
              ? "公开从来不是默认。你可以在自己的世界里选择一个版本，主动把它带到探索世界。"
              : "Publishing is never automatic. In your own world, choose a version and deliberately bring it to Explore."}
          </p>
          <Link className="dw-button dw-button-primary" to="/">
            {zh ? "回到我的世界" : "Return to my world"}
          </Link>
        </div>
      )}

      <div className="dw-explore-footer">
        <span className="dw-activity-label">{zh ? "一颗灵感可以被很多人续写" : "One spark can be continued by many"}</span>
        <span className="dw-activity-chip">
          <Icon name="shield" size={13} />
          {zh ? "所有公开都需要作者主动选择" : "Every publication is a deliberate author choice"}
        </span>
        <span className="dw-activity-chip">
          <Icon name="lock" size={13} />
          {zh ? "衍生后的作品默认属于你的私人空间" : "A derived branch is private to its creator by default"}
        </span>
      </div>

      <Modal
        open={rulesOpen}
        onClose={() => setRulesOpen(false)}
        title={zh ? "从自己的世界，向外延伸" : "Reach outward from your own world"}
        kicker={zh ? "OPEN, BY CHOICE" : "OPEN, BY CHOICE"}
        actions={
          <button className="dw-button dw-button-primary" type="button" onClick={() => setRulesOpen(false)}>
            {zh ? "知道了" : "Understood"}
          </button>
        }
      >
        <p className="dw-modal-lead">
          {zh ? "公开应当是一种主动选择，而不是产品默认行为。" : "Public should be a deliberate choice, never a product default."}
        </p>
        <div className="dw-choice-list">
          <div className="dw-choice">
            <Icon name="lock" size={19} />
            <div>
              <strong>{zh ? "私人是默认" : "Private by default"}</strong>
              <small>{zh ? "未授权时不公开；好友身份不会自动授予权限。" : "Nothing is public without authorization; friendship grants no access by itself."}</small>
            </div>
          </div>
          <div className="dw-choice">
            <Icon name="eye" size={19} />
            <div>
              <strong>{zh ? "浏览与衍生分开授权" : "Viewing and derivation are separate"}</strong>
              <small>{zh ? "别人能看见，不代表一定能编辑或复制衍生。" : "Being able to see a world does not mean being able to edit or derive from it."}</small>
            </div>
          </div>
          <div className="dw-choice">
            <Icon name="branch" size={19} />
            <div>
              <strong>{zh ? "每条衍生独立" : "Every branch is independent"}</strong>
              <small>{zh ? "新世界拥有自己的所有者、边界与继续发展的路线。" : "A new world has its own owner, boundary, and path forward."}</small>
            </div>
          </div>
        </div>
        <p className="dw-fineprint">
          <Icon name="shield" size={13} />
          {zh ? "公开只投影作者选择的版本，不会公开私人 AI 上下文或成员内容。" : "Publishing projects only the version the author chooses; it never exposes private AI context or member content."}
        </p>
      </Modal>
      {toastView}
    </div>
  );
}
