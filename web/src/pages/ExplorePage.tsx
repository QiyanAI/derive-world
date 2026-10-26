// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import {
  derivePublicWorld,
  getPublicLineage,
  getPublicWorld,
  listPublicWorlds,
  reportPublicWorld,
  type PublicLineage,
  type PublicWorld
} from "../api";
import { ErrorNotice, StatusNotice } from "../errors";
import { useAuth } from "../auth";
import { useI18n, type MessageKey } from "../i18n";

function publicPreview(world: PublicWorld): { title: string; body: string; elements: string[] } {
  const snapshot = (world.snapshot?.state ?? {}) as Record<string, unknown>;
  const title = String(snapshot.title ?? world.title);
  let body = "";
  if (typeof snapshot.summary === "string") {
    body = snapshot.summary;
  } else if (typeof snapshot.prompt === "string") {
    body = snapshot.prompt;
  } else if (typeof snapshot.premise === "string") {
    body = snapshot.premise;
  } else if (typeof snapshot.body === "string") {
    body = snapshot.body;
  }
  const elements = Array.isArray(snapshot.elements)
    ? (snapshot.elements as Array<Record<string, unknown>>)
        .map((element) => String(element.text ?? ""))
        .filter(Boolean)
    : Array.isArray(snapshot.checklist)
      ? (snapshot.checklist as Array<Record<string, unknown>>)
          .map((item) => String(item.text ?? ""))
          .filter(Boolean)
      : [];
  return { title, body, elements };
}

export default function ExplorePage() {
  const { id } = useParams();
  const { t } = useI18n();
  const { status } = useAuth();
  const navigate = useNavigate();
  const [worlds, setWorlds] = useState<PublicWorld[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<PublicWorld | null>(null);
  const [lineage, setLineage] = useState<PublicLineage | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [title, setTitle] = useState("");
  const [fields, setFields] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [reportNotice, setReportNotice] = useState("");

  const load = useCallback(
    async (cursor?: string, signal?: AbortSignal) => {
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
    },
    []
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(undefined, controller.signal);
    return () => controller.abort();
  }, [load]);

  useEffect(() => {
    if (!id) {
      setSelected(null);
      setLineage(null);
      return;
    }
    const controller = new AbortController();
    setError(null);
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
        setTitle(`${preview.title} ${t("explore.deriveSuffix")}`.trim().slice(0, 200));
        const available =
          world.object_type === "interactive"
            ? ["prompt", "options"]
            : world.object_type === "living_scene"
              ? ["premise", "elements", "tags"]
              : ["summary", "body", "checklist", "tags"];
        setFields(available);
      })
      .catch((failure) => {
        if (!controller.signal.aborted) {
          setError(failure);
        }
      });
    return () => controller.abort();
  }, [id, t]);

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

  async function derive() {
    if (!selected || !id) {
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const child = await derivePublicWorld(id, { title, inheritance_fields: fields });
      navigate(`/objects/${child.id}?derived=1&tab=work`);
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy(false);
    }
  }

  if (id) {
    return (
      <div className="page page-wide explore-page">
        <p className="eyebrow">{t("explore.eyebrow")}</p>
        <div className="page-heading with-actions">
          <div>
            <h1>{selected ? publicPreview(selected).title : t("common.loading")}</h1>
            {selected ? (
              <p className="object-meta-line">
                <span className="template-badge">{selected.object_type}</span>
                <span>v{selected.source_version}</span>
                <span>{t("explore.by", { name: selected.publisher_name })}</span>
                <span>{selected.license}</span>
                <span>{selected.derive_allowed ? t("explore.derivable") : t("explore.viewOnly")}</span>
              </p>
            ) : null}
          </div>
          <Link className="button button-ghost" to="/explore">
            {t("explore.back")}
          </Link>
        </div>
        {error ? <ErrorNotice error={error} /> : null}
        {reportNotice ? <StatusNotice>{reportNotice}</StatusNotice> : null}
        {selected ? (
          <div className="explore-detail">
            <article className="card public-world">
              {publicPreview(selected).body ? (
                <p className="document-summary">{publicPreview(selected).body}</p>
              ) : null}
              {publicPreview(selected).elements.length > 0 ? (
                <ul className="public-elements">
                  {publicPreview(selected).elements.map((element, index) => (
                    <li key={index}>{element}</li>
                  ))}
                </ul>
              ) : null}
            </article>
            <aside className="card">
              <h2>{t("explore.lineageTitle")}</h2>
              {lineage?.source ? (
                <p>
                  {t("explore.sourceOf", { title: lineage.source.title })}{" "}
                  <Link to={`/explore/${lineage.source.publication_id}`}>{t("explore.openSource")}</Link>
                </p>
              ) : (
                <p className="muted">{t("explore.noSource")}</p>
              )}
              {lineage && lineage.derived.length > 0 ? (
                <>
                  <h3>{t("explore.branches")}</h3>
                  <ul className="lens-branch-list">
                    {lineage.derived.map((branch) => (
                      <li key={branch.publication_id}>
                        <Link to={`/explore/${branch.publication_id}`}>{branch.title}</Link>
                      </li>
                    ))}
                  </ul>
                </>
              ) : null}

              <h3>{t("explore.deriveTitle")}</h3>
              {status === "authenticated" ? (
                <button className="text-button" type="button" onClick={() => void report()}>
                  {t("explore.report")}
                </button>
              ) : null}
              {selected.derive_allowed ? (
                status === "authenticated" ? (
                  <div className="field-stack">
                    <label>
                      <span>{t("explore.deriveTitleField")}</span>
                      <input maxLength={200} value={title} onChange={(event) => setTitle(event.target.value)} />
                    </label>
                    <fieldset className="field-group">
                      <legend>{t("derive.fields")}</legend>
                      {[
                        ...(selected.object_type === "interactive"
                          ? ["prompt", "options"]
                          : selected.object_type === "living_scene"
                            ? ["premise", "elements", "tags"]
                            : ["summary", "body", "checklist", "tags"])
                      ].map((field) => (
                        <label className="check-row" key={field}>
                          <input
                            type="checkbox"
                            checked={fields.includes(field)}
                            onChange={(event) =>
                              setFields((previous) =>
                                event.target.checked
                                  ? [...previous, field]
                                  : previous.filter((value) => value !== field)
                              )
                            }
                          />
                          <span>{t(`derive.field.${field}` as MessageKey)}</span>
                        </label>
                      ))}
                    </fieldset>
                    <button
                      className="button"
                      type="button"
                      disabled={busy || fields.length === 0 || !title.trim()}
                      onClick={() => void derive()}
                    >
                      {busy ? t("derive.creating") : t("explore.deriveAction")}
                    </button>
                    <p className="form-hint">{t("explore.deriveHint")}</p>
                  </div>
                ) : (
                  <p>
                    <Link className="button button-small" to={`/login?next=${encodeURIComponent(`/explore/${id}`)}`}>
                      {t("nav.login")}
                    </Link>
                  </p>
                )
              ) : (
                <p className="muted">{t("explore.notDerivable")}</p>
              )}
            </aside>
          </div>
        ) : null}
      </div>
    );
  }

  return (
    <div className="page page-wide explore-page" data-testid="explore-page">
      <div className="page-heading">
        <p className="eyebrow">{t("explore.eyebrow")}</p>
        <h1>{t("explore.title")}</h1>
        <p className="lead">{t("explore.lead")}</p>
      </div>
      {error ? <ErrorNotice error={error} onRetry={() => void load()} /> : null}
      {loading && worlds.length === 0 ? <p className="muted">{t("common.loading")}</p> : null}
      {!loading && worlds.length === 0 ? (
        <div className="empty-state">
          <h2>{t("explore.emptyTitle")}</h2>
          <p>{t("explore.emptyBody")}</p>
        </div>
      ) : null}
      <ul className="public-grid">
        {worlds.map((world) => {
          const preview = publicPreview(world);
          return (
            <li key={world.id}>
              <Link className="card public-card" to={`/explore/${world.id}`}>
                <span className="template-badge">{world.object_type}</span>
                <h2>{preview.title}</h2>
                <p>{preview.body || t("explore.noSummary")}</p>
                <small>
                  {t("explore.by", { name: world.publisher_name })} · v{world.source_version} ·{" "}
                  {world.derive_allowed ? t("explore.derivable") : t("explore.viewOnly")}
                </small>
              </Link>
            </li>
          );
        })}
      </ul>
      {nextCursor ? (
        <button className="button button-ghost" type="button" onClick={() => void load(nextCursor)}>
          {t("common.loadMore")}
        </button>
      ) : null}
    </div>
  );
}
