// SPDX-License-Identifier: Apache-2.0
import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router";
import { deriveObject, getObject, type ObjectView } from "../api";
import { ErrorNotice } from "../errors";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";

type Field = "summary" | "body" | "checklist" | "tags" | "prompt" | "options" | "premise" | "elements";

const collaborativeFields: Field[] = ["summary", "body", "checklist", "tags"];
const interactiveFields: Field[] = ["prompt", "options"];
const sceneFields: Field[] = ["premise", "elements", "tags"];

export default function DerivePage() {
  const { id } = useParams();
  const { t } = useI18n();
  const { status } = useAuth();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [object, setObject] = useState<ObjectView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [title, setTitle] = useState("");
  const [sourceVersion, setSourceVersion] = useState(searchParams.get("source") ?? "");
  const [fields, setFields] = useState<Field[]>([]);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!id || status !== "authenticated") {
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    getObject(id, controller.signal)
      .then((loaded) => {
        if (controller.signal.aborted) {
          return;
        }
        setObject(loaded);
        const baseTitle = String(loaded.state.title ?? "");
        setTitle(`${baseTitle} ${t("derive.defaultSuffix")}`.trim().slice(0, 200));
        setFields(
          loaded.object_type === "interactive"
            ? ["prompt", "options"]
            : loaded.object_type === "living_scene"
              ? ["premise", "elements"]
              : ["summary", "body"]
        );
      })
      .catch((failure) => {
        if (!controller.signal.aborted) {
          setError(failure);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setLoading(false);
        }
      });
    return () => controller.abort();
  }, [id, status, t]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!object) {
      return;
    }
    setError(null);
    setBusy(true);
    try {
      const child = await deriveObject(object.id, {
        title,
        source_version: sourceVersion === "" ? undefined : Number(sourceVersion),
        inheritance_fields: fields
      });
      navigate(`/objects/${child.id}?derived=1&tab=work`);
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy(false);
    }
  }

  if (status === "loading" || loading) {
    return <p className="page">{t("common.loading")}</p>;
  }
  if (status === "anonymous") {
    return (
      <div className="page">
        <p className="notice">{t("auth.required")}</p>
        <Link className="button" to={`/login?next=${encodeURIComponent(`/objects/${id ?? ""}`)}`}>
          {t("nav.login")}
        </Link>
      </div>
    );
  }
  if (!object) {
    return (
      <div className="page">
        <p className="eyebrow">404</p>
        <p className="notice">{t("object.notFound")}</p>
        {error ? <ErrorNotice error={error} /> : null}
        <Link className="button" to="/objects">
          {t("common.backToObjects")}
        </Link>
      </div>
    );
  }
  if (!object.permissions.includes("derive")) {
    return (
      <div className="page">
        <h1>{t("derive.title")}</h1>
        <p className="notice">{t("derive.noDerive")}</p>
        <Link className="button" to={`/objects/${object.id}`}>
          {t("derive.backToParent")}
        </Link>
      </div>
    );
  }

  const available: Field[] =
    object.object_type === "interactive"
      ? interactiveFields
      : object.object_type === "living_scene"
        ? sceneFields
        : collaborativeFields;
  const selectedSource = sourceVersion === "" ? object.version : Number(sourceVersion);
  const historical = selectedSource !== object.version;

  function fieldPreview(field: Field): string {
    if (field === "checklist") {
      return Array.isArray(object?.state.checklist) ? `${object.state.checklist.length}` : "0";
    }
    if (field === "options") {
      return Array.isArray(object?.state.options) ? `${object.state.options.length}` : "0";
    }
    if (field === "elements") {
      return Array.isArray(object?.state.elements) ? `${object.state.elements.length}` : "0";
    }
    if (field === "tags") {
      return Array.isArray(object?.state.tags) ? (object.state.tags as string[]).join(", ") || "—" : "—";
    }
    return String(object?.state[field] ?? "—").slice(0, 200);
  }

  return (
    <div className="page">
      <p className="eyebrow">{t("workspace.derive")}</p>
      <h1>{t("derive.title")}</h1>
      <p className="lead">{t("derive.lead")}</p>

      <div className="branch-visual" aria-hidden="true">
        <div className="branch-node branch-parent">
          <span className="map-node-label">{t("derive.branchParent")}</span>
          <strong>{String(object.state.title ?? "")}</strong>
          <span className="mono">v{object.version}</span>
        </div>
        <span className="branch-arrow">→</span>
        <div className="branch-node branch-child">
          <span className="map-node-label">{t("derive.branchChild")}</span>
          <strong>{title || t("derive.titleField")}</strong>
          <span className="mono">v1</span>
        </div>
      </div>

      <form className="card form" onSubmit={submit}>
        <label>
          <span>{t("derive.titleField")}</span>
          <input required minLength={1} maxLength={200} value={title} onChange={(event) => setTitle(event.target.value)} />
        </label>
        <label>
          <span>{t("derive.sourceVersion")}</span>
          <select value={sourceVersion} onChange={(event) => setSourceVersion(event.target.value)}>
            <option value="">{t("derive.current", { version: object.version })}</option>
            {Array.from({ length: Math.max(0, object.version - 1) }, (_, index) => index + 1).map((version) => (
              <option key={version} value={String(version)}>
                {t("derive.historical", { version })}
              </option>
            ))}
          </select>
          <small>{t("derive.sourceHint")}</small>
        </label>

        <fieldset className="field-group">
          <legend>{t("derive.fields")}</legend>
          {available.map((field) => {
            const requiredField = field === "options";
            return (
              <label className="check-row" key={field}>
                <input
                  type="checkbox"
                  checked={fields.includes(field)}
                  disabled={requiredField}
                  onChange={(event) => {
                    setFields((previous) =>
                      event.target.checked ? [...previous, field] : previous.filter((value) => value !== field)
                    );
                  }}
                />
                <span>{t(`derive.field.${field}`)}</span>
              </label>
            );
          })}
          <small>{t("derive.fieldsHint")}</small>
        </fieldset>

        <div className="preview-box">
          <h2>{t("derive.preview")}</h2>
          {historical ? (
            <p>{t("derive.previewHistorical")}</p>
          ) : (
            <dl className="preview-grid">
              {fields.map((field) => (
                <div key={field}>
                  <dt>{t(`derive.field.${field}`)}</dt>
                  <dd>{fieldPreview(field)}</dd>
                </div>
              ))}
            </dl>
          )}
          <small>{t("derive.previewHint")}</small>
        </div>

        <p className="muted">{t("derive.independent")}</p>
        {error ? <ErrorNotice error={error} /> : null}
        <div className="form-actions">
          <button className="button" type="submit" disabled={busy || fields.length === 0}>
            {busy ? t("derive.creating") : t("derive.submit")}
          </button>
          <Link className="button button-ghost" to={`/objects/${object.id}`}>
            {t("derive.backToParent")}
          </Link>
        </div>
      </form>
    </div>
  );
}
