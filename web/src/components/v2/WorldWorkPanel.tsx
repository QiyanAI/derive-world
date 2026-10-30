// SPDX-License-Identifier: Apache-2.0
import { useEffect, useState, type FormEvent } from "react";
import {
  applyAction,
  errorMessage,
  type ObjectView,
  type SceneElement
} from "../../api";
import { useI18n } from "../../i18n";
import Icon from "./Icon";

type ScenePurpose = "project" | "story";

function sceneString(object: ObjectView, key: string): string {
  const value = object.state[key];
  return typeof value === "string" ? value : "";
}

function sceneElements(object: ObjectView): SceneElement[] {
  const value = object.state.elements;
  return Array.isArray(value) ? (value as SceneElement[]) : [];
}

function sceneTags(object: ObjectView): string[] {
  const value = object.state.tags;
  return Array.isArray(value) ? (value as string[]) : [];
}

export default function WorldWorkPanel({
  object,
  canContribute,
  sourceTitle,
  onUpdated,
  onReload,
  onBranch,
  onAI
}: {
  object: ObjectView;
  canContribute: boolean;
  sourceTitle: string;
  onUpdated: (updated: ObjectView) => Promise<void> | void;
  onReload: () => Promise<void> | void;
  onBranch: () => void;
  onAI: () => void;
}) {
  const { lang } = useI18n();
  const zh = lang === "zh";
  const purpose: ScenePurpose = sceneString(object, "purpose") === "story" ? "story" : "project";
  const elements = sceneElements(object);
  const done = elements.filter((element) => element.status === "done").length;
  const [editingTitle, setEditingTitle] = useState(false);
  const [titleDraft, setTitleDraft] = useState(sceneString(object, "title"));
  const [editingPremise, setEditingPremise] = useState(false);
  const [premiseDraft, setPremiseDraft] = useState(sceneString(object, "premise"));
  const [editingElement, setEditingElement] = useState("");
  const [elementDraft, setElementDraft] = useState("");
  const [newText, setNewText] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    setTitleDraft(sceneString(object, "title"));
    setPremiseDraft(sceneString(object, "premise"));
    setEditingTitle(false);
    setEditingPremise(false);
    setEditingElement("");
    setNewText("");
    setError("");
    setNotice("");
  }, [object.id]);

  async function run(action: string, input: Record<string, unknown>) {
    setBusy(action);
    setError("");
    setNotice("");
    try {
      const updated = await applyAction(object.id, action, input, object.version);
      await onUpdated(updated);
      setNotice(zh ? "已保存。" : "Saved.");
      return updated;
    } catch (failure) {
      setError(errorMessage(failure));
      await onReload();
      return null;
    } finally {
      setBusy("");
    }
  }

  async function saveTitle(event: FormEvent) {
    event.preventDefault();
    const value = titleDraft.trim();
    if (!value || value === sceneString(object, "title")) {
      setEditingTitle(false);
      return;
    }
    const updated = await run("set_title", { title: value });
    if (updated) {
      setEditingTitle(false);
    }
  }

  async function savePremise() {
    if (premiseDraft === sceneString(object, "premise")) {
      setEditingPremise(false);
      return;
    }
    const updated = await run("set_premise", { premise: premiseDraft });
    if (updated) {
      setEditingPremise(false);
    }
  }

  async function addElement(event: FormEvent) {
    event.preventDefault();
    const text = newText.trim();
    if (!text || elements.length >= 32) {
      return;
    }
    const updated = await run("add_element", {
      kind: purpose === "story" ? "scene" : "idea",
      text
    });
    if (updated) {
      setNewText("");
    }
  }

  async function saveElement(element: SceneElement) {
    const text = elementDraft.trim();
    if (!text || text === element.text) {
      setEditingElement("");
      return;
    }
    const updated = await run("update_element", { element_id: element.id, text });
    if (updated) {
      setEditingElement("");
    }
  }

  const source = object.lineage?.parent.object_id
    ? zh
      ? `源自「${sourceTitle || "另一条世界线"}」的独立分支`
      : `An independent branch from “${sourceTitle || "another worldline"}”`
    : zh
      ? "这是一段由你开启的创造。它可以继续改变，也可以衍生出不同方向。"
      : "A creation you began. It can keep changing, or grow into a different direction.";

  return (
    <div className="dw-world-work" data-testid="dw-world-work">
      <div className="dw-inspect-header">
        <div>
          <p className="dw-inspect-kicker">
            {zh ? "YOU ARE HERE" : "YOU ARE HERE"} ·{" "}
            {purpose === "story" ? (zh ? "自由创造" : "Story") : zh ? "实用创造" : "Project"}
          </p>
          {editingTitle ? (
            <form className="dw-inline-title" onSubmit={saveTitle}>
              <input
                className="dw-input"
                maxLength={200}
                value={titleDraft}
                onChange={(event) => setTitleDraft(event.target.value)}
                autoFocus
                aria-label={zh ? "世界名称" : "World title"}
              />
              <button className="dw-icon-button" type="submit" disabled={busy !== ""} aria-label={zh ? "保存名称" : "Save title"}>
                <Icon name="check" size={16} />
              </button>
              <button className="dw-icon-button" type="button" onClick={() => setEditingTitle(false)} aria-label={zh ? "取消" : "Cancel"}>
                <Icon name="close" size={16} />
              </button>
            </form>
          ) : (
            <h2 className="dw-inspect-title">
              {sceneString(object, "title") || (zh ? "未命名世界" : "Untitled world")}
              {canContribute ? (
                <button
                  className="dw-inspect-title-edit"
                  type="button"
                  onClick={() => {
                    setTitleDraft(sceneString(object, "title"));
                    setEditingTitle(true);
                  }}
                  aria-label={zh ? "调整名称" : "Rename world"}
                >
                  <Icon name="edit" size={14} />
                </button>
              ) : null}
            </h2>
          )}
          <p className="dw-inspect-sub">
            {source}
            {" · "}
            {zh ? `${done}/${elements.length} 个已完成` : `${done}/${elements.length} complete`}
          </p>
        </div>
      </div>

      <div className="dw-inspect-premise">
        {editingPremise ? (
          <>
            <textarea
              className="dw-input dw-textarea"
              rows={3}
              maxLength={2000}
              value={premiseDraft}
              onChange={(event) => setPremiseDraft(event.target.value)}
              aria-label={zh ? "创造起点" : "Premise"}
              autoFocus
            />
            <div className="dw-inline-actions">
              <button className="dw-button dw-button-small dw-button-primary" type="button" onClick={() => void savePremise()} disabled={busy !== ""}>
                {zh ? "保存起点" : "Save premise"}
              </button>
              <button className="dw-button dw-button-small dw-button-ghost" type="button" onClick={() => setEditingPremise(false)}>
                {zh ? "取消" : "Cancel"}
              </button>
            </div>
          </>
        ) : (
          <>
            <p>{sceneString(object, "premise") || (zh ? "你的世界从第一步开始。" : "Your world begins with the first step.")}</p>
            {canContribute ? (
              <button className="dw-text-button" type="button" onClick={() => setEditingPremise(true)}>
                {zh ? "编辑起点" : "Edit premise"}
              </button>
            ) : null}
          </>
        )}
      </div>

      <div className="dw-panel-section-title">{zh ? "触碰就能改变的内容" : "Touch it to change it"}</div>
      <ul className="dw-live-list">
        {elements.map((element) => {
          const editing = editingElement === element.id;
          const finished = element.status === "done";
          return (
            <li key={element.id} className={`dw-live-item${finished ? " is-done" : ""}`}>
              <button
                type="button"
                className="dw-live-check"
                disabled={!canContribute || busy !== ""}
                aria-label={
                  finished
                    ? zh
                      ? `标记为未完成：${element.text}`
                      : `Mark incomplete: ${element.text}`
                    : zh
                      ? `标记已完成：${element.text}`
                      : `Mark complete: ${element.text}`
                }
                onClick={() =>
                  void run("update_element", {
                    element_id: element.id,
                    status: finished ? "open" : "done"
                  })
                }
              >
                {finished ? <Icon name="check" size={12} /> : <Icon name="layers" size={12} />}
              </button>
              {editing ? (
                <div className="dw-live-edit">
                  <input
                    className="dw-input"
                    maxLength={2000}
                    value={elementDraft}
                    onChange={(event) => setElementDraft(event.target.value)}
                    aria-label={zh ? "编辑内容" : "Edit content"}
                    autoFocus
                    onKeyDown={(event) => {
                      if (event.key === "Enter") {
                        event.preventDefault();
                        void saveElement(element);
                      }
                      if (event.key === "Escape") {
                        setEditingElement("");
                      }
                    }}
                  />
                  <button className="dw-icon-button" type="button" onClick={() => void saveElement(element)} disabled={busy !== ""} aria-label={zh ? "保存" : "Save"}>
                    <Icon name="check" size={15} />
                  </button>
                  <button className="dw-icon-button" type="button" onClick={() => setEditingElement("")} aria-label={zh ? "取消" : "Cancel"}>
                    <Icon name="close" size={15} />
                  </button>
                </div>
              ) : (
                <>
                  <button
                    type="button"
                    className="dw-live-text"
                    disabled={!canContribute}
                    onClick={() => {
                      setElementDraft(element.text);
                      setEditingElement(element.id);
                    }}
                  >
                    {element.text}
                  </button>
                  {canContribute ? (
                    <span className="dw-live-actions">
                      <button
                        className="dw-icon-button"
                        type="button"
                        aria-label={zh ? "编辑内容" : "Edit content"}
                        onClick={() => {
                          setElementDraft(element.text);
                          setEditingElement(element.id);
                        }}
                      >
                        <Icon name="edit" size={13} />
                      </button>
                      <button
                        className="dw-icon-button"
                        type="button"
                        aria-label={zh ? "移除" : "Remove"}
                        disabled={busy !== ""}
                        onClick={() => void run("remove_element", { element_id: element.id })}
                      >
                        <Icon name="trash" size={13} />
                      </button>
                    </span>
                  ) : null}
                </>
              )}
            </li>
          );
        })}
        {elements.length === 0 ? (
          <li className="dw-live-empty">
            {zh ? "这里还没有内容。从第一步开始，或让 AI 提议一个方向。" : "Nothing here yet. Start with one step, or ask your AI for a direction."}
          </li>
        ) : null}
      </ul>

      {canContribute ? (
        <form className="dw-inline-add" onSubmit={addElement}>
          <input
            className="dw-input"
            maxLength={2000}
            value={newText}
            onChange={(event) => setNewText(event.target.value)}
            placeholder={zh ? "添加一个新想法 / 下一步…" : "Add a new idea or next step…"}
            aria-label={zh ? "添加新的想法或步骤" : "Add a new idea or step"}
            disabled={elements.length >= 32}
          />
          <button className="dw-inline-add-button" type="submit" disabled={!newText.trim() || busy !== "" || elements.length >= 32} aria-label={zh ? "添加" : "Add"}>
            <Icon name="plus" size={17} />
          </button>
        </form>
      ) : null}

      <div className="dw-inspector-tools">
        {object.permissions.includes("derive") ? (
          <button className="dw-button dw-button-primary dw-button-small" type="button" onClick={onBranch}>
            <Icon name="branch" size={15} />
            {zh ? "衍生新方向" : "Derive a new direction"}
          </button>
        ) : null}
        <button className="dw-button dw-button-small dw-button-ghost" type="button" onClick={onAI}>
          <Icon name="spark" size={15} />
          {zh ? "让 AI 帮我想" : "Ask my AI"}
        </button>
      </div>

      {notice ? <p className="dw-form-success" role="status">{notice}</p> : null}
      {error ? <p className="dw-form-error" role="alert">{error}</p> : null}

      <div className="dw-panel-section-title">{zh ? "这条世界线的来源" : "Where this worldline came from"}</div>
      <p className="dw-inspect-source">{source}</p>
      {sceneTags(object).length > 0 ? (
        <div className="dw-tag-list">
          {sceneTags(object).map((tag) => (
            <span className="dw-tag" key={tag}>{tag}</span>
          ))}
        </div>
      ) : null}
      <p className="dw-fineprint">
        <Icon name="shield" size={13} />
        {zh
          ? "这里的改变会真实写入对象并保留版本历史。私人 AI 上下文不会随世界共享。"
          : "Changes here are written to the real object with version history. Private AI context is never shared with the world."}
      </p>
    </div>
  );
}
