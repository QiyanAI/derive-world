// SPDX-License-Identifier: Apache-2.0
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { applyAction, type ObjectView, type SceneElement } from "../api";
import { ErrorNotice, StatusNotice } from "../errors";
import { useI18n, type MessageKey } from "../i18n";

type SceneKind = SceneElement["kind"];

const kinds: SceneKind[] = ["idea", "step", "scene", "choice", "outcome"];

function sceneElements(object: ObjectView): SceneElement[] {
  const value = object.state.elements;
  return Array.isArray(value) ? (value as SceneElement[]) : [];
}

function sceneString(object: ObjectView, key: string): string {
  const value = object.state[key];
  return typeof value === "string" ? value : "";
}

function sceneTags(object: ObjectView): string[] {
  const value = object.state.tags;
  return Array.isArray(value) ? (value as string[]) : [];
}

export default function SceneWorkTab({
  object,
  canContribute,
  canManage,
  onUpdated,
  onReload
}: {
  object: ObjectView;
  canContribute: boolean;
  canManage: boolean;
  onUpdated: (updated: ObjectView) => Promise<void> | void;
  onReload: () => Promise<void> | void;
}) {
  const { t } = useI18n();
  const [titleDraft, setTitleDraft] = useState(sceneString(object, "title"));
  const [premiseDraft, setPremiseDraft] = useState(sceneString(object, "premise"));
  const [editingText, setEditingText] = useState("");
  const [textDraft, setTextDraft] = useState("");
  const [newKind, setNewKind] = useState<SceneKind>("idea");
  const [newText, setNewText] = useState("");
  const [tagDraft, setTagDraft] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [notice, setNotice] = useState("");

  useEffect(() => {
    setTitleDraft(sceneString(object, "title"));
    setPremiseDraft(sceneString(object, "premise"));
  }, [object]);

  useEffect(() => {
    setNewKind(sceneString(object, "purpose") === "story" ? "scene" : "idea");
  }, [object.id]);

  async function run(action: string, input: Record<string, unknown>) {
    setBusy(action);
    setError(null);
    setNotice("");
    try {
      const updated = await applyAction(object.id, action, input, object.version);
      await onUpdated(updated);
      setNotice(t("scene.saved"));
      if (action === "add_element") {
        setNewText("");
      }
      if (action === "add_tag") {
        setTagDraft("");
      }
      return true;
    } catch (failure) {
      setError(failure);
      await onReload();
      return false;
    } finally {
      setBusy("");
    }
  }

  async function saveHeader() {
    let version = object.version;
    if (titleDraft.trim() && titleDraft !== sceneString(object, "title")) {
      const updated = await applyAction(object.id, "set_title", { title: titleDraft }, version);
      version = updated.version;
      await onUpdated(updated);
    }
    if (premiseDraft !== sceneString(object, "premise")) {
      const updated = await applyAction(object.id, "set_premise", { premise: premiseDraft }, version);
      await onUpdated(updated);
    }
    setNotice(t("scene.saved"));
  }

  const elements = sceneElements(object);
  const purpose = sceneString(object, "purpose") || "project";
  const done = elements.filter((element) => element.status === "done").length;

  function exportScene() {
    const payload = {
      id: object.id,
      version: object.version,
      title: sceneString(object, "title"),
      purpose,
      premise: sceneString(object, "premise"),
      elements,
      tags: sceneTags(object)
    };
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `${sceneString(object, "title") || "scene"}.json`;
    anchor.click();
    URL.revokeObjectURL(url);
  }

  return (
    <section id="object-work-panel" className="card object-panel scene-panel" data-testid="scene-work-panel">
      <div className="section-heading">
        <div>
          <h2>{t("scene.title")}</h2>
          <p className="muted">
            {t(purpose === "story" ? "scene.progressStory" : "scene.progress", {
              done,
              total: elements.length
            })}{" "}
            · {t(`scene.kind.${purpose}` as MessageKey)}
          </p>
        </div>
        {canContribute ? (
          <button
            type="button"
            className="button button-small"
            disabled={busy !== ""}
            onClick={() => void saveHeader()}
          >
            {busy ? t("workspace.saving") : t("scene.saveHeader")}
          </button>
        ) : null}
        <button type="button" className="button button-ghost button-small" onClick={exportScene}>
          {t("scene.export")}
        </button>
      </div>

      {notice ? <StatusNotice>{notice}</StatusNotice> : null}
      {error ? <ErrorNotice error={error} /> : null}

      <div className="scene-header">
        <label>
          <span>{t("scene.titleField")}</span>
          <input
            maxLength={200}
            value={titleDraft}
            disabled={!canContribute || busy !== ""}
            onChange={(event) => setTitleDraft(event.target.value)}
          />
        </label>
        <label>
          <span>{t("scene.premise")}</span>
          <textarea
            rows={3}
            maxLength={2000}
            value={premiseDraft}
            disabled={!canContribute || busy !== ""}
            onChange={(event) => setPremiseDraft(event.target.value)}
          />
        </label>
        {canManage ? (
          <label>
            <span>{t("scene.purpose")}</span>
            <select
              value={purpose}
              disabled={busy !== ""}
              onChange={(event) => void run("set_purpose", { purpose: event.target.value })}
            >
              <option value="project">{t("scene.purpose.project")}</option>
              <option value="story">{t("scene.purpose.story")}</option>
            </select>
          </label>
        ) : null}
      </div>

      <h3>{t("scene.elements")}</h3>
      <ol className="scene-elements">
        {elements.map((element, index) => (
          <li key={element.id} className={`scene-element scene-kind-${element.kind} ${element.status === "done" ? "scene-element-done" : ""}`}>
            <div className="scene-element-head">
              <span className={`scene-kind scene-kind-badge scene-kind-${element.kind}`}>
                {t(`scene.kind.${element.kind}`)}
              </span>
              {canContribute ? (
                <div className="scene-element-actions">
                  <button
                    type="button"
                    className="text-button"
                    disabled={busy !== "" || index === 0}
                    onClick={() => void run("move_element", { element_id: element.id, position: index - 1 })}
                    aria-label={t("scene.moveUp")}
                  >
                    ↑
                  </button>
                  <button
                    type="button"
                    className="text-button"
                    disabled={busy !== "" || index === elements.length - 1}
                    onClick={() => void run("move_element", { element_id: element.id, position: index + 1 })}
                    aria-label={t("scene.moveDown")}
                  >
                    ↓
                  </button>
                  <button
                    type="button"
                    className="text-button"
                    disabled={busy !== ""}
                    onClick={() => {
                      if (editingText === element.id) {
                        setEditingText("");
                        return;
                      }
                      setTextDraft(element.text);
                      setEditingText(element.id);
                    }}
                  >
                    {t("scene.edit")}
                  </button>
                  <button
                    type="button"
                    className="text-button"
                    disabled={busy !== ""}
                    onClick={() => void run("remove_element", { element_id: element.id })}
                  >
                    {t("common.remove")}
                  </button>
                </div>
              ) : null}
            </div>
            {editingText === element.id ? (
              <div className="inline-field">
                <input
                  maxLength={2000}
                  value={textDraft}
                  onChange={(event) => setTextDraft(event.target.value)}
                />
                <button
                  type="button"
                  className="button button-small"
                  disabled={busy !== "" || !textDraft.trim()}
                  onClick={async () => {
                    const ok = await run("update_element", {
                      element_id: element.id,
                      text: textDraft
                    });
                    if (ok) {
                      setEditingText("");
                    }
                  }}
                >
                  {t("common.save")}
                </button>
              </div>
            ) : (
              <p className="scene-element-text">{element.text}</p>
            )}
            <label className="check-row scene-step-check">
              <input
                type="checkbox"
                checked={element.status === "done"}
                disabled={!canContribute || busy !== ""}
                onChange={() =>
                  void run("update_element", {
                    element_id: element.id,
                    status: element.status === "done" ? "open" : "done"
                  })
                }
              />
              <span>{t(purpose === "story" ? "scene.markResolved" : "scene.stepDone")}</span>
            </label>
          </li>
        ))}
        {elements.length === 0 ? (
          <li className="muted">{t(purpose === "story" ? "scene.emptyStory" : "scene.empty")}</li>
        ) : null}
      </ol>

      {canContribute ? (
        <div className="scene-add">
          <select value={newKind} onChange={(event) => setNewKind(event.target.value as SceneKind)}>
            {kinds.map((kind) => (
              <option key={kind} value={kind}>
                {t(`scene.kind.${kind}`)}
              </option>
            ))}
          </select>
          <input
            placeholder={t("scene.addPlaceholder")}
            maxLength={2000}
            value={newText}
            onChange={(event) => setNewText(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && newText.trim() && busy === "") {
                event.preventDefault();
                void run("add_element", { kind: newKind, text: newText });
              }
            }}
          />
          <button
            type="button"
            className="button button-small"
            disabled={busy !== "" || !newText.trim() || elements.length >= 32}
            onClick={() => void run("add_element", { kind: newKind, text: newText })}
          >
            {t("scene.add")}
          </button>
        </div>
      ) : null}

      <h3>{t("workspace.tags")}</h3>
      <div className="tag-list">
        {sceneTags(object).map((tag) => (
          <span className="tag" key={tag}>
            {tag}
            {canContribute ? (
              <button
                type="button"
                className="tag-remove"
                aria-label={`${t("common.remove")} ${tag}`}
                disabled={busy !== ""}
                onClick={() => void run("remove_tag", { tag })}
              >
                ×
              </button>
            ) : null}
          </span>
        ))}
        {sceneTags(object).length === 0 ? <span className="muted">{t("workspace.tagsEmpty")}</span> : null}
      </div>
      {canContribute ? (
        <div className="inline-field">
          <input
            placeholder={t("workspace.tagPlaceholder")}
            maxLength={50}
            value={tagDraft}
            onChange={(event) => setTagDraft(event.target.value)}
          />
          <button
            type="button"
            className="button button-small"
            disabled={busy !== "" || !tagDraft.trim()}
            onClick={() => void run("add_tag", { tag: tagDraft })}
          >
            {t("workspace.addTag")}
          </button>
        </div>
      ) : null}

      {object.permissions.includes("derive") ? (
        <div className="scene-derive">
          <p className="muted">{t("scene.deriveHint")}</p>
          <Link className="button button-small" to={`/objects/${object.id}/derive`}>
            {t("scene.derive")}
          </Link>
        </div>
      ) : null}
    </section>
  );
}
