// SPDX-License-Identifier: Apache-2.0
import { useEffect, useRef, useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { createObject, type ObjectType } from "../api";
import { ErrorNotice } from "../errors";
import { useAuth } from "../auth";
import { useI18n, type MessageKey } from "../i18n";

export default function ObjectNewPage() {
  const { t } = useI18n();
  const { status } = useAuth();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const template = searchParams.get("template");
  const initialType: ObjectType =
    template === "interactive" ? "interactive" : template === "living_scene" ? "living_scene" : "collaborative";
  const [objectType, setObjectType] = useState<ObjectType>(initialType);
  const [title, setTitle] = useState("");
  const [summary, setSummary] = useState("");
  const [body, setBody] = useState("");
  const [tags, setTags] = useState("");
  const [prompt, setPrompt] = useState("");
  const [options, setOptions] = useState<string[]>(["", ""]);
  const [purpose, setPurpose] = useState<"project" | "story">(
    searchParams.get("purpose") === "story" ? "story" : "project"
  );
  const [premise, setPremise] = useState("");
  const [elements, setElements] = useState<Array<{ kind: string; text: string }>>([
    { kind: "idea", text: "" }
  ]);
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

  function applySceneStarter(kind: "project" | "story") {
    setObjectType("living_scene");
    setPurpose(kind);
    if (kind === "project") {
      setTitle(t("object.create.starter.projectTitle"));
      setPremise(t("object.create.starter.projectPremise"));
      setElements([
        { kind: "step", text: t("object.create.starter.projectStep") },
        { kind: "outcome", text: t("object.create.starter.projectOutcome") }
      ]);
    } else {
      setTitle(t("object.create.starter.storyTitle"));
      setPremise(t("object.create.starter.storyPremise"));
      setElements([
        { kind: "scene", text: t("object.create.starter.storyScene") },
        { kind: "choice", text: t("object.create.starter.storyChoice") }
      ]);
    }
  }

  const firstUse = searchParams.get("first") === "1";
  const starterApplied = useRef(false);

  useEffect(() => {
    if (!firstUse || starterApplied.current || initialType !== "living_scene") {
      return;
    }
    starterApplied.current = true;
    applySceneStarter(purpose);
    // The first-use entry pre-fills a real starter once; the user can edit it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

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
    const tagList = tags
      .split(",")
      .map((tag) => tag.trim())
      .filter(Boolean);
    const content =
      objectType === "collaborative"
        ? {
            summary,
            body,
            checklist: [] as unknown[],
            tags: tagList
          }
        : objectType === "interactive"
          ? {
              prompt,
              options: labels.map((label) => ({ label }))
            }
          : {
              purpose,
              premise,
              elements: elements
                .filter((element) => element.text.trim())
                .map((element) => ({ kind: element.kind, text: element.text })),
              tags: tagList
            };
    setBusy(true);
    try {
      const created = await createObject({
        object_type: objectType,
        title,
        content,
        shared_context: shared
      });
      navigate(`/objects/${created.id}?tab=work&created=1${firstUse ? "&first=1" : ""}`);
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
          <label className={objectType === "living_scene" ? "choice choice-active" : "choice"}>
            <input
              type="radio"
              name="object_type"
              value="living_scene"
              checked={objectType === "living_scene"}
              onChange={() => setObjectType("living_scene")}
            />
            <strong>{t("object.create.scene")}</strong>
            <small>{t("object.create.sceneHint")}</small>
          </label>
        </fieldset>

        <div className="example-row">
          <span>{t("object.create.examples")}</span>
          {objectType === "living_scene" ? (
            <>
              <button
                type="button"
                className="chip-button"
                onClick={() => applySceneStarter("project")}
              >
                {t("object.create.starter.project")}
              </button>
              <button
                type="button"
                className="chip-button"
                onClick={() => applySceneStarter("story")}
              >
                {t("object.create.starter.story")}
              </button>
            </>
          ) : (
            <>
              <button type="button" className="chip-button" onClick={() => applyExample("notes")}>
                {t("object.create.example.notes")}
              </button>
              <button type="button" className="chip-button" onClick={() => applyExample("poll")}>
                {t("object.create.example.poll")}
              </button>
            </>
          )}
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
        ) : objectType === "living_scene" ? (
          <>
            <label>
              <span>{t("scene.purpose")}</span>
              <select value={purpose} onChange={(event) => setPurpose(event.target.value as "project" | "story")}>
                <option value="project">{t("scene.purpose.project")}</option>
                <option value="story">{t("scene.purpose.story")}</option>
              </select>
            </label>
            <label>
              <span>{t("scene.premise")}</span>
              <textarea
                rows={3}
                maxLength={2000}
                value={premise}
                onChange={(event) => setPremise(event.target.value)}
              />
            </label>
            <div className="field-group">
              <span>{t("scene.elements")}</span>
              {elements.map((element, index) => (
                <div className="inline-field scene-new-element" key={index}>
                  <select
                    value={element.kind}
                    onChange={(event) => {
                      const next = [...elements];
                      const current = next[index] ?? { kind: "idea", text: "" };
                      next[index] = { kind: event.target.value, text: current.text };
                      setElements(next);
                    }}
                  >
                    {["idea", "step", "scene", "choice", "outcome"].map((kind) => (
                      <option key={kind} value={kind}>
                        {t(`scene.kind.${kind}` as MessageKey)}
                      </option>
                    ))}
                  </select>
                  <input
                    placeholder={t("scene.addPlaceholder")}
                    maxLength={2000}
                    value={element.text}
                    onChange={(event) => {
                      const next = [...elements];
                      const current = next[index] ?? { kind: "idea", text: "" };
                      next[index] = { kind: current.kind, text: event.target.value };
                      setElements(next);
                    }}
                  />
                  {elements.length > 1 ? (
                    <button
                      type="button"
                      className="button button-ghost button-small"
                      onClick={() => setElements(elements.filter((_, position) => position !== index))}
                    >
                      {t("common.remove")}
                    </button>
                  ) : null}
                </div>
              ))}
              {elements.length < 32 ? (
                <button
                  type="button"
                  className="button button-ghost button-small"
                  onClick={() => setElements([...elements, { kind: "idea", text: "" }])}
                >
                  {t("scene.add")}
                </button>
              ) : null}
            </div>
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
