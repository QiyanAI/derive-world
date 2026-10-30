// SPDX-License-Identifier: Apache-2.0
import { useEffect, useRef, useState } from "react";
import { applyAction, type ObjectView } from "../api";
import { ErrorNotice, StatusNotice } from "../errors";
import { useI18n } from "../i18n";

interface ChecklistItem {
  id: string;
  text: string;
  done: boolean;
}

interface Option {
  id: string;
  label: string;
}

function stateString(object: ObjectView, key: string): string {
  const value = object.state[key];
  return typeof value === "string" ? value : "";
}

function checklistOf(object: ObjectView): ChecklistItem[] {
  const value = object.state.checklist;
  return Array.isArray(value) ? (value as ChecklistItem[]) : [];
}

function tagsOf(object: ObjectView): string[] {
  const value = object.state.tags;
  return Array.isArray(value) ? (value as string[]) : [];
}

function optionsOf(object: ObjectView): Option[] {
  const value = object.state.options;
  return Array.isArray(value) ? (value as Option[]) : [];
}

function votesOf(object: ObjectView): Record<string, number> {
  const value = object.state.votes;
  return value && typeof value === "object" ? (value as Record<string, number>) : {};
}

export default function ObjectWorkTab({
  object,
  canContribute,
  canManage,
  recordedVote,
  onUpdated,
  onReload
}: {
  object: ObjectView;
  canContribute: boolean;
  canManage: boolean;
  recordedVote: string;
  onUpdated: (updated: ObjectView) => Promise<void> | void;
  onReload: () => Promise<void> | void;
}) {
  const { t } = useI18n();
  const collaborative = object.object_type === "collaborative";
  const [editing, setEditing] = useState(false);
  const [titleDraft, setTitleDraft] = useState(stateString(object, "title"));
  const [summaryDraft, setSummaryDraft] = useState(stateString(object, "summary"));
  const [bodyDraft, setBodyDraft] = useState(stateString(object, "body"));
  const [itemDraft, setItemDraft] = useState("");
  const [tagDraft, setTagDraft] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [notice, setNotice] = useState("");
  const [myVote, setMyVote] = useState(recordedVote);
  const versionAtEdit = useRef(object.version);
  const staleEdit = editing && versionAtEdit.current !== object.version;

  useEffect(() => {
    if (!editing) {
      setTitleDraft(stateString(object, "title"));
      setSummaryDraft(stateString(object, "summary"));
      setBodyDraft(stateString(object, "body"));
    }
  }, [object, editing]);

  useEffect(() => {
    if (recordedVote) {
      setMyVote(recordedVote);
    }
  }, [recordedVote]);

  function beginEdit() {
    versionAtEdit.current = object.version;
    setEditing(true);
  }

  function cancelEdit() {
    setTitleDraft(stateString(object, "title"));
    setSummaryDraft(stateString(object, "summary"));
    setBodyDraft(stateString(object, "body"));
    setEditing(false);
    setError(null);
  }

  async function runSingle(action: string, input: Record<string, unknown>) {
    setBusy(action);
    setError(null);
    setNotice("");
    try {
      const updated = await applyAction(object.id, action, input, object.version);
      if (action === "cast_vote" && typeof input.option_id === "string") {
        setMyVote(input.option_id);
      }
      await onUpdated(updated);
      setNotice(t("workspace.saved"));
      if (action === "add_checklist_item") {
        setItemDraft("");
      }
      if (action === "add_tag") {
        setTagDraft("");
      }
    } catch (failure) {
      setError(failure);
      await onReload();
    } finally {
      setBusy("");
    }
  }

  async function saveDocument() {
    const changes: Array<{ action: string; input: Record<string, unknown> }> = [];
    if (titleDraft !== stateString(object, "title")) {
      changes.push({ action: "set_title", input: { title: titleDraft } });
    }
    if (summaryDraft !== stateString(object, "summary")) {
      changes.push({ action: "set_summary", input: { summary: summaryDraft } });
    }
    if (bodyDraft !== stateString(object, "body")) {
      changes.push({ action: "set_body", input: { body: bodyDraft } });
    }
    if (changes.length === 0) {
      setNotice(t("workspace.noChanges"));
      return;
    }
    setBusy("document");
    setError(null);
    setNotice("");
    let version = object.version;
    let latest: ObjectView | null = null;
    let applied = 0;
    try {
      for (const change of changes) {
        latest = await applyAction(object.id, change.action, change.input, version);
        version = latest.version;
        applied += 1;
      }
      await onUpdated(latest as ObjectView);
      setEditing(false);
      setNotice(t("workspace.saved"));
    } catch (failure) {
      setError(failure);
      if (applied > 0) {
        setNotice(t("workspace.partialSaved"));
      }
      await onReload();
    } finally {
      setBusy("");
    }
  }

  return (
    <section id="object-work-panel" className="card object-panel" data-testid="object-work-panel">
      <div className="section-heading">
        <h2>{t("workspace.sectionWork")}</h2>
        {collaborative && canContribute ? (
          editing ? (
            <div className="form-actions">
              <button
                type="button"
                className="button button-small"
                disabled={busy !== ""}
                onClick={() => void saveDocument()}
              >
                {busy === "document" ? t("workspace.saving") : t("workspace.saveChanges")}
              </button>
              <button type="button" className="button button-small button-ghost" disabled={busy !== ""} onClick={cancelEdit}>
                {t("workspace.cancelEdit")}
              </button>
            </div>
          ) : (
            <button type="button" className="button button-small button-ghost" onClick={beginEdit}>
              {t("workspace.edit")}
            </button>
          )
        ) : null}
      </div>

      {notice ? <StatusNotice>{notice}</StatusNotice> : null}
      {error ? <ErrorNotice error={error} /> : null}
      {staleEdit ? <p className="notice notice-compact">{t("workspace.conflict")}</p> : null}

      {collaborative ? (
        <>
          {editing ? (
            <div className="field-stack document-editor">
              <label>
                <span>{t("workspace.actionTitle")}</span>
                <input
                  maxLength={200}
                  value={titleDraft}
                  onChange={(event) => setTitleDraft(event.target.value)}
                />
              </label>
              <label>
                <span>{t("workspace.actionSummary")}</span>
                <textarea
                  rows={3}
                  maxLength={2000}
                  value={summaryDraft}
                  onChange={(event) => setSummaryDraft(event.target.value)}
                />
              </label>
              <label>
                <span>{t("workspace.actionBody")}</span>
                <textarea
                  rows={12}
                  maxLength={20000}
                  value={bodyDraft}
                  onChange={(event) => setBodyDraft(event.target.value)}
                />
              </label>
            </div>
          ) : (
            <article className="document-view">
              {stateString(object, "summary") ? (
                <p className="document-summary">{stateString(object, "summary")}</p>
              ) : null}
              {stateString(object, "body") ? (
                <div className="document-body">{stateString(object, "body")}</div>
              ) : null}
              {!stateString(object, "summary") && !stateString(object, "body") ? (
                <p className="muted">{t("workspace.documentEmpty")}</p>
              ) : null}
            </article>
          )}

          <h3>{t("workspace.checklist")}</h3>
          <ul className="checklist">
            {checklistOf(object).map((item) => (
              <li key={item.id}>
                <label className="check-row">
                  <input
                    type="checkbox"
                    checked={item.done}
                    disabled={!canContribute || busy !== ""}
                    onChange={() =>
                      void runSingle("toggle_checklist_item", {
                        item_id: item.id,
                        done: !item.done
                      })
                    }
                  />
                  <span className={item.done ? "done" : ""}>{item.text}</span>
                </label>
                {canContribute ? (
                  <button
                    type="button"
                    className="text-button"
                    disabled={busy !== ""}
                    onClick={() => void runSingle("remove_checklist_item", { item_id: item.id })}
                  >
                    {t("common.remove")}
                  </button>
                ) : null}
              </li>
            ))}
            {checklistOf(object).length === 0 ? <li className="muted">{t("workspace.checklistEmpty")}</li> : null}
          </ul>
          {canContribute ? (
            <div className="inline-field">
              <input
                placeholder={t("workspace.itemPlaceholder")}
                maxLength={500}
                value={itemDraft}
                onChange={(event) => setItemDraft(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" && itemDraft.trim() && busy === "") {
                    event.preventDefault();
                    void runSingle("add_checklist_item", { text: itemDraft });
                  }
                }}
              />
              <button
                className="button button-small"
                type="button"
                disabled={busy !== "" || itemDraft.trim() === ""}
                onClick={() => void runSingle("add_checklist_item", { text: itemDraft })}
              >
                {t("workspace.addItem")}
              </button>
            </div>
          ) : null}

          <h3>{t("workspace.tags")}</h3>
          <div className="tag-list">
            {tagsOf(object).map((tag) => (
              <span className="tag" key={tag}>
                {tag}
                {canContribute ? (
                  <button
                    type="button"
                    className="tag-remove"
                    aria-label={`${t("common.remove")} ${tag}`}
                    disabled={busy !== ""}
                    onClick={() => void runSingle("remove_tag", { tag })}
                  >
                    ×
                  </button>
                ) : null}
              </span>
            ))}
            {tagsOf(object).length === 0 ? <span className="muted">{t("workspace.tagsEmpty")}</span> : null}
          </div>
          {canContribute ? (
            <div className="inline-field">
              <input
                placeholder={t("workspace.tagPlaceholder")}
                maxLength={50}
                value={tagDraft}
                onChange={(event) => setTagDraft(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" && tagDraft.trim() && busy === "") {
                    event.preventDefault();
                    void runSingle("add_tag", { tag: tagDraft });
                  }
                }}
              />
              <button
                className="button button-small"
                type="button"
                disabled={busy !== "" || tagDraft.trim() === ""}
                onClick={() => void runSingle("add_tag", { tag: tagDraft })}
              >
                {t("workspace.addTag")}
              </button>
            </div>
          ) : null}
        </>
      ) : (
        <>
          <p className="poll-prompt">{stateString(object, "prompt") || "—"}</p>
          <p className="poll-status">
            <span className={stateString(object, "status") === "open" ? "status-open" : "status-closed"}>
              {stateString(object, "status") === "open" ? t("workspace.open") : t("workspace.closed")}
            </span>
            <span className="muted">
              {" · "}
              {t("workspace.totalVotes", {
                count: Object.values(votesOf(object)).reduce((sum, value) => sum + value, 0)
              })}
            </span>
          </p>
          <ul className="option-list">
            {optionsOf(object).map((option) => {
              const votes = votesOf(object);
              const count = votes[option.id] ?? 0;
              const total = Object.values(votes).reduce((sum, value) => sum + value, 0);
              const percent = total === 0 ? 0 : Math.round((count / total) * 100);
              const selected = myVote === option.id;
              return (
                <li key={option.id}>
                  <button
                    type="button"
                    className={selected ? "option-button option-selected" : "option-button"}
                    disabled={!canContribute || stateString(object, "status") !== "open" || busy !== ""}
                    aria-pressed={selected}
                    onClick={() => void runSingle("cast_vote", { option_id: option.id })}
                  >
                    <span className="option-label">
                      <span>{option.label}</span>
                      {selected ? <small>{t("common.you")}</small> : null}
                    </span>
                    <span className="option-tally">
                      <span className="option-bar" aria-hidden="true">
                        <span className="option-bar-fill" style={{ width: `${percent}%` }} />
                      </span>
                      <strong>{count}</strong>
                      <small>{percent}%</small>
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
          <p className="form-hint">
            {myVote
              ? t("workspace.yourVote", {
                  option:
                    optionsOf(object).find((option) => option.id === myVote)?.label ?? myVote
                })
              : t("workspace.oneVote")}
          </p>
          {canManage && stateString(object, "status") === "open" ? (
            <button
              className="button button-ghost button-small"
              type="button"
              disabled={busy !== ""}
              onClick={() => void runSingle("close_poll", {})}
            >
              {t("workspace.closePoll")}
            </button>
          ) : null}
          {stateString(object, "status") !== "open" ? (
            <p className="muted">{t("workspace.pollClosed")}</p>
          ) : null}
        </>
      )}
    </section>
  );
}
