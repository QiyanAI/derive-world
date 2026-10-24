// SPDX-License-Identifier: Apache-2.0
import { useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { createObject } from "../api";
import { ErrorNotice } from "../errors";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";

export default function ObjectNewPage() {
  const { t } = useI18n();
  const { status } = useAuth();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const initialType = searchParams.get("template") === "interactive" ? "interactive" : "collaborative";
  const [objectType, setObjectType] = useState<"collaborative" | "interactive">(initialType);
  const [title, setTitle] = useState("");
  const [summary, setSummary] = useState("");
  const [body, setBody] = useState("");
  const [tags, setTags] = useState("");
  const [prompt, setPrompt] = useState("");
  const [options, setOptions] = useState<string[]>(["", ""]);
  const [sharedContext, setSharedContext] = useState("{}");
  const [formError, setFormError] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  function applyExample(example: "notes" | "poll") {
    if (example === "notes") {
      setObjectType("collaborative");
      setTitle(t("object.create.example.notesTitle"));
      setSummary(t("object.create.example.notesSummary"));
      setBody(t("object.create.example.notesBody"));
      setTags(t("object.create.example.tags"));
    } else {
      setObjectType("interactive");
      setTitle(t("object.create.example.pollTitle"));
      setPrompt(t("object.create.example.pollPrompt"));
      setOptions([
        t("object.create.example.pollOption1"),
        t("object.create.example.pollOption2"),
        t("object.create.example.pollOption3")
      ]);
    }
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setError(null);
    setFormError("");
    let shared: Record<string, unknown>;
    try {
      const parsed = JSON.parse(sharedContext || "{}");
      if (parsed === null || Array.isArray(parsed) || typeof parsed !== "object") {
        throw new Error("shared_context");
      }
      shared = parsed as Record<string, unknown>;
    } catch {
      setFormError(t("object.create.sharedInvalid"));
      return;
    }
    const labels = options.map((label) => label.trim()).filter(Boolean);
    if (objectType === "interactive" && (labels.length < 2 || labels.length > 10)) {
      setFormError(t("object.create.optionError"));
      return;
    }
    const content =
      objectType === "collaborative"
        ? {
            summary,
            body,
            checklist: [] as unknown[],
            tags: tags
              .split(",")
              .map((tag) => tag.trim())
              .filter(Boolean)
          }
        : {
            prompt,
            options: labels.map((label) => ({ label }))
          };
    setBusy(true);
    try {
      const created = await createObject({
        object_type: objectType,
        title,
        content,
        shared_context: shared
      });
      navigate(`/objects/${created.id}?tab=work&created=1`);
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy(false);
    }
  }

  if (status === "loading") {
    return <p className="page">{t("common.loading")}</p>;
  }
  if (status === "anonymous") {
    return (
      <div className="page">
        <p className="notice">{t("auth.required")}</p>
        <Link className="button" to="/login?next=%2Fobjects%2Fnew">
          {t("nav.login")}
        </Link>
      </div>
    );
  }

  return (
    <div className="page">
      <div className="page-heading">
        <p className="eyebrow">{t("app.name")}</p>
        <h1>{t("object.create.title")}</h1>
        <p className="lead">{t("object.create.lead")}</p>
      </div>
      <form className="card form" onSubmit={submit}>
        <fieldset className="template-choice">
          <legend>{t("object.create.type")}</legend>
          <label className={objectType === "collaborative" ? "choice choice-active" : "choice"}>
            <input
              type="radio"
              name="object_type"
              value="collaborative"
              checked={objectType === "collaborative"}
              onChange={() => setObjectType("collaborative")}
            />
            <strong>{t("object.create.collaborative")}</strong>
            <small>{t("object.create.collaborativeHint")}</small>
          </label>
          <label className={objectType === "interactive" ? "choice choice-active" : "choice"}>
            <input
              type="radio"
              name="object_type"
              value="interactive"
              checked={objectType === "interactive"}
              onChange={() => setObjectType("interactive")}
            />
            <strong>{t("object.create.interactive")}</strong>
            <small>{t("object.create.interactiveHint")}</small>
          </label>
        </fieldset>

        <div className="example-row">
          <span>{t("object.create.examples")}</span>
          <button type="button" className="chip-button" onClick={() => applyExample("notes")}>
            {t("object.create.example.notes")}
          </button>
          <button type="button" className="chip-button" onClick={() => applyExample("poll")}>
            {t("object.create.example.poll")}
          </button>
          <small>{t("object.create.examplesHint")}</small>
        </div>

        <label>
          <span>{t("object.create.titleField")}</span>
          <input
            required
            minLength={1}
            maxLength={200}
            value={title}
            onChange={(event) => setTitle(event.target.value)}
          />
        </label>

        {objectType === "collaborative" ? (
          <>
            <label>
              <span>{t("object.create.summary")}</span>
              <textarea
                rows={3}
                maxLength={2000}
                value={summary}
                onChange={(event) => setSummary(event.target.value)}
              />
            </label>
            <label>
              <span>{t("object.create.body")}</span>
              <textarea
                rows={8}
                maxLength={20000}
                value={body}
                onChange={(event) => setBody(event.target.value)}
              />
            </label>
            <label>
              <span>{t("object.create.tags")}</span>
              <input value={tags} onChange={(event) => setTags(event.target.value)} />
              <small>{t("object.create.tagsHint")}</small>
            </label>
          </>
        ) : (
          <>
            <label>
              <span>{t("object.create.prompt")}</span>
              <textarea
                rows={4}
                maxLength={2000}
                value={prompt}
                onChange={(event) => setPrompt(event.target.value)}
              />
            </label>
            <div className="field-group">
              <span>{t("object.create.options")}</span>
              {options.map((option, index) => (
                <div className="inline-field" key={index}>
                  <input
                    aria-label={t("object.create.optionPlaceholder", { index: index + 1 })}
                    placeholder={t("object.create.optionPlaceholder", { index: index + 1 })}
                    maxLength={200}
                    value={option}
                    onChange={(event) => {
                      const next = [...options];
                      next[index] = event.target.value;
                      setOptions(next);
                    }}
                  />
                  {options.length > 2 ? (
                    <button
                      type="button"
                      className="button button-ghost button-small"
                      onClick={() => setOptions(options.filter((_, position) => position !== index))}
                    >
                      {t("object.create.removeOption")}
                    </button>
                  ) : null}
                </div>
              ))}
              {options.length < 10 ? (
                <button
                  type="button"
                  className="button button-ghost button-small"
                  onClick={() => setOptions([...options, ""])}
                >
                  {t("object.create.addOption")}
                </button>
              ) : null}
            </div>
          </>
        )}

        <details className="advanced-panel">
          <summary>{t("object.create.advanced")}</summary>
          <label>
            <span>{t("object.create.sharedContext")}</span>
            <textarea
              className="mono"
              rows={4}
              value={sharedContext}
              onChange={(event) => setSharedContext(event.target.value)}
            />
            <small>{t("object.create.sharedHint")}</small>
          </label>
        </details>

        {formError ? (
          <p className="form-error" role="alert">
            {formError}
          </p>
        ) : null}
        {error ? <ErrorNotice error={error} compact /> : null}
        <div className="form-actions">
          <button className="button" type="submit" disabled={busy}>
            {busy ? t("object.create.creating") : t("object.create.submit")}
          </button>
          <Link className="button button-ghost" to="/objects">
            {t("common.cancel")}
          </Link>
        </div>
      </form>
    </div>
  );
}
