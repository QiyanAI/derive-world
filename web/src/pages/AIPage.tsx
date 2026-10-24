// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router";
import {
  createConversation,
  deleteConversation,
  getAIProfile,
  getConversation,
  getPrivateContext,
  listConversations,
  listObjects,
  sendAIMessage,
  updateAIProfile,
  updatePrivateContext,
  type AIMessage,
  type Conversation,
  type ObjectListItem,
  type PersonalAIProfile,
  type PrivateContext
} from "../api";
import HandoffPanel from "../components/HandoffPanel";
import { ErrorNotice, StatusNotice } from "../errors";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";
import { useMeta } from "../meta";

const CONTEXT_LIMIT = 8192;

export default function AIPage() {
  const { t, lang } = useI18n();
  const { status, refresh: refreshAuth } = useAuth();
  const { mode } = useMeta();
  const [profile, setProfile] = useState<PersonalAIProfile | null>(null);
  const [name, setName] = useState("");
  const [persona, setPersona] = useState("");
  const [privateContext, setPrivateContext] = useState<PrivateContext | null>(null);
  const [contextDraft, setContextDraft] = useState("");
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [objects, setObjects] = useState<ObjectListItem[]>([]);
  const [generalID, setGeneralID] = useState<string | null>(null);
  const [generalMessages, setGeneralMessages] = useState<AIMessage[]>([]);
  const [generalDraft, setGeneralDraft] = useState("");
  const [handoffObjectID, setHandoffObjectID] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState<unknown>(null);

  const timeFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(lang === "zh" ? "zh-CN" : "en", {
        dateStyle: "medium",
        timeStyle: "short"
      }),
    [lang]
  );

  const contextBytes = useMemo(() => new TextEncoder().encode(contextDraft).length, [contextDraft]);
  const generalBytes = useMemo(() => new TextEncoder().encode(generalDraft).length, [generalDraft]);

  const objectsByID = useMemo(() => {
    const map = new Map<string, ObjectListItem>();
    for (const item of objects) {
      map.set(item.id, item);
    }
    return map;
  }, [objects]);

  const loadGeneralConversation = useCallback(async (items: Conversation[]) => {
    const general = items.find((item) => item.object_id === null);
    if (!general) {
      setGeneralID(null);
      setGeneralMessages([]);
      return;
    }
    setGeneralID(general.id);
    try {
      const detail = await getConversation(general.id);
      setGeneralMessages(detail.messages);
    } catch {
      setGeneralMessages([]);
    }
  }, []);

  const load = useCallback(async () => {
    setError(null);
    try {
      const [loadedProfile, loadedContext, conversationPage, objectPage] = await Promise.all([
        getAIProfile(),
        getPrivateContext(),
        listConversations(),
        listObjects("all")
      ]);
      setProfile(loadedProfile);
      setName(loadedProfile.name);
      setPersona(loadedProfile.persona);
      setPrivateContext(loadedContext);
      setContextDraft(loadedContext.content);
      setConversations(conversationPage.items);
      setObjects(objectPage.items);
      setHandoffObjectID((previous) => previous || objectPage.items[0]?.id || "");
      await loadGeneralConversation(conversationPage.items);
    } catch (failure) {
      setError(failure);
    } finally {
      setLoading(false);
    }
  }, [loadGeneralConversation]);

  useEffect(() => {
    if (status === "authenticated") {
      void load();
    }
  }, [status, load]);

  async function saveProfile() {
    setBusy("profile");
    setError(null);
    setNotice("");
    try {
      const updated = await updateAIProfile({ name, persona });
      setProfile((previous) => (previous ? { ...previous, ...updated } : updated));
      setNotice(t("ai.profileSaved"));
      await refreshAuth();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function saveContext() {
    setBusy("context");
    setError(null);
    setNotice("");
    try {
      const updated = await updatePrivateContext(contextDraft);
      setPrivateContext(updated);
      setNotice(t("ai.contextSaved"));
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function sendGeneral() {
    const content = generalDraft.trim();
    if (!content || busy) {
      return;
    }
    setBusy("send");
    setError(null);
    setNotice("");
    try {
      let conversationID = generalID;
      if (!conversationID) {
        const created = await createConversation({ title: t("ai.conversationGeneral") });
        conversationID = created.id;
        setGeneralID(created.id);
      }
      const result = await sendAIMessage(conversationID, content);
      setGeneralMessages((previous) => [...previous, result.user_message, result.assistant_message]);
      setGeneralDraft("");
      const conversationPage = await listConversations();
      setConversations(conversationPage.items);
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function removeConversation(item: Conversation) {
    if (!window.confirm(t("ai.deleteConversationConfirm"))) {
      return;
    }
    setBusy(`delete:${item.id}`);
    setError(null);
    try {
      await deleteConversation(item.id);
      if (generalID === item.id) {
        setGeneralID(null);
        setGeneralMessages([]);
      }
      const conversationPage = await listConversations();
      setConversations(conversationPage.items);
      setNotice(t("ai.conversationDeleted"));
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  if (status === "loading" || loading) {
    return <p className="page">{t("common.loading")}</p>;
  }
  if (status === "anonymous") {
    return (
      <div className="page">
        <h1>{t("ai.title")}</h1>
        <p className="notice">{t("auth.required")}</p>
        <Link className="button" to="/login?next=%2Fai">
          {t("nav.login")}
        </Link>
      </div>
    );
  }

  return (
    <div className="page page-wide ai-page">
      <div className="page-heading">
        <p className="eyebrow">{t("app.name")}</p>
        <h1>{t("ai.title")}</h1>
        <p className="lead">{t("ai.lead")}</p>
      </div>

      {notice ? <StatusNotice>{notice}</StatusNotice> : null}
      {error ? <ErrorNotice error={error} onRetry={() => void load()} /> : null}

      <div className="ai-layout">
        <div className="stack">
          <section className="card">
            <h2>{t("ai.profile")}</h2>
            <div className="field-stack">
              <label>
                <span>{t("ai.name")}</span>
                <input maxLength={80} value={name} onChange={(event) => setName(event.target.value)} />
              </label>
              <label>
                <span>{t("ai.persona")}</span>
                <textarea
                  rows={5}
                  maxLength={2000}
                  value={persona}
                  onChange={(event) => setPersona(event.target.value)}
                />
              </label>
              <button
                className="button button-small"
                type="button"
                disabled={busy !== ""}
                onClick={() => void saveProfile()}
              >
                {t("ai.saveProfile")}
              </button>
            </div>
          </section>

          <section className="card">
            <h2>{t("ai.context")}</h2>
            <p className="muted">{t("ai.contextHint")}</p>
            <textarea
              rows={7}
              maxLength={CONTEXT_LIMIT}
              value={contextDraft}
              aria-label={t("ai.context")}
              onChange={(event) => setContextDraft(event.target.value)}
            />
            <div className="section-heading">
              <small className={contextBytes > CONTEXT_LIMIT ? "form-error" : "muted"}>
                {t("ai.contextBytes", { bytes: contextBytes })}
              </small>
              <button
                className="button button-small"
                type="button"
                disabled={busy !== "" || contextBytes > CONTEXT_LIMIT}
                onClick={() => void saveContext()}
              >
                {t("ai.saveContext")}
              </button>
            </div>
            <p className="form-hint">{t("ai.contextDisclosure")}</p>
            {privateContext ? (
              <small className="muted">{timeFormatter.format(new Date(privateContext.updated_at))}</small>
            ) : null}
          </section>

          <section className="card">
            <div className="section-heading">
              <h2>{t("ai.generalTitle")}</h2>
              <small>{t("ai.generalHint")}</small>
            </div>
            <div className="ai-messages" aria-live="polite">
              {generalMessages.length === 0 ? <p className="muted">{t("ai.generalEmpty")}</p> : null}
              {generalMessages.map((message) => (
                <div key={message.id} className={`ai-message ai-${message.role}`}>
                  <small>
                    {message.role === "user" ? t("workspace.you") : t("workspace.ai")}
                    {message.model_mode === "fixture" ? ` · ${t("mode.fixture")}` : ""}
                  </small>
                  <p>{message.content}</p>
                </div>
              ))}
            </div>
            <label className="ai-compose">
              <span className="visually-hidden">{t("ai.generalPlaceholder")}</span>
              <textarea
                rows={3}
                maxLength={16384}
                value={generalDraft}
                placeholder={t("ai.generalPlaceholder")}
                onChange={(event) => setGeneralDraft(event.target.value)}
              />
            </label>
            <div className="form-actions">
              <button
                className="button button-small"
                type="button"
                disabled={!generalDraft.trim() || busy !== "" || generalBytes > 16384}
                onClick={() => void sendGeneral()}
              >
                {busy === "send" ? t("ai.sending") : t("ai.send")}
              </button>
              <small className={generalBytes > 16384 ? "form-error" : "muted"}>
                {t("ai.messageBytes", { bytes: generalBytes })}
              </small>
              {mode === "fixture" ? (
                <button
                  type="button"
                  className="chip-button"
                  disabled={busy !== ""}
                  onClick={() => setGeneralDraft("hello")}
                >
                  {t("workspace.aiQuickHello")}
                </button>
              ) : null}
            </div>
            {mode === "fixture" ? <p className="fixture-inline">{t("ai.fixtureNote")}</p> : null}
          </section>
        </div>

        <aside className="ai-side">
          <section className="card">
            <h2>{t("ai.usage")}</h2>
            {profile?.usage ? (
              <dl className="detail-list">
                <div>
                  <dt>{t("ai.usageCalls")}</dt>
                  <dd className="mono">
                    {profile.usage.calls} / {profile.usage.daily_calls_limit}
                  </dd>
                </div>
                <div>
                  <dt>{t("ai.usageChars")}</dt>
                  <dd className="mono">
                    {profile.usage.input_chars} / {profile.usage.daily_input_chars_limit}
                  </dd>
                </div>
                <div>
                  <dt>{t("ai.model")}</dt>
                  <dd className="mono">{profile.usage.model_name}</dd>
                </div>
                <div>
                  <dt>{t("ai.mode")}</dt>
                  <dd>{profile.usage.model_mode}</dd>
                </div>
              </dl>
            ) : null}
            {mode === "fixture" ? <p className="fixture-inline">{t("ai.fixtureNote")}</p> : null}
          </section>

          <section className="card">
            <h2>{t("ai.authorizationsLink")}</h2>
            <p className="muted">{t("ai.authorizationsBody")}</p>
            <Link className="button button-small button-ghost" to="/self">
              {t("nav.self")}
            </Link>
          </section>

          <section className="card">
            <h2>{t("ai.conversations")}</h2>
            {conversations.length === 0 ? <p className="muted">{t("ai.noConversations")}</p> : null}
            <ul className="conversation-list">
              {conversations.map((item) => {
                const boundTitle = item.object_id ? objectsByID.get(item.object_id)?.title : null;
                return (
                  <li key={item.id}>
                    <div>
                      <strong>
                        {item.object_id
                          ? boundTitle
                            ? t("ai.conversationObject", { title: boundTitle })
                            : t("ai.conversationGeneral")
                          : t("ai.conversationGeneral")}
                      </strong>
                      <small>{item.title}</small>
                    </div>
                    <div className="conversation-actions">
                      {item.object_id ? (
                        <Link className="text-link" to={`/objects/${item.object_id}?tab=ai`}>
                          {t("ai.openConversation")}
                        </Link>
                      ) : null}
                      <button
                        type="button"
                        className="text-button"
                        disabled={busy !== ""}
                        onClick={() => void removeConversation(item)}
                      >
                        {t("ai.deleteConversation")}
                      </button>
                    </div>
                  </li>
                );
              })}
            </ul>
          </section>

          <section className="card">
            <h2>{t("ai.handoffTitle")}</h2>
            <p className="muted">{t("ai.handoffBody")}</p>
            {objects.length === 0 ? (
              <p className="muted">{t("ai.handoffNoObjects")}</p>
            ) : (
              <label>
                <span>{t("ai.handoffSelect")}</span>
                <select value={handoffObjectID} onChange={(event) => setHandoffObjectID(event.target.value)}>
                  {objects.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.title}
                    </option>
                  ))}
                </select>
              </label>
            )}
          </section>
          {handoffObjectID ? <HandoffPanel key={handoffObjectID} objectID={handoffObjectID} /> : null}
        </aside>
      </div>
    </div>
  );
}
