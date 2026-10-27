// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router";
import {
  applyProposal,
  approveConfirmation,
  createConversation,
  denyConfirmation,
  denyProposal,
  getConversation,
  listConfirmations,
  listConversations,
  listProposals,
  sendAIMessage,
  type AIMessage,
  type Confirmation,
  type ObjectView,
  type Proposal
} from "../api";
import { describeError, ErrorNotice, StatusNotice } from "../errors";
import { actionKey, proposalPreview } from "../humanize";
import { useI18n } from "../i18n";
import { useMeta } from "../meta";

interface Props {
  object: ObjectView;
  canContribute: boolean;
  onChanged: () => void;
}

export default function AIPanel({ object, canContribute, onChanged }: Props) {
  const objectID = object.id;
  const objectTitle = typeof object.state.title === "string" ? object.state.title : "";
  const objectVersion = object.version;
  const { t, lang } = useI18n();
  const { mode } = useMeta();
  const [conversationID, setConversationID] = useState<string | null>(null);
  const [messages, setMessages] = useState<AIMessage[]>([]);
  const [proposals, setProposals] = useState<Proposal[]>([]);
  const [confirmations, setConfirmations] = useState<Confirmation[]>([]);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [createdChild, setCreatedChild] = useState<ObjectView | null>(null);

  const draftBytes = useMemo(() => new TextEncoder().encode(draft).length, [draft]);
  const timeFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(lang === "zh" ? "zh-CN" : "en", {
        dateStyle: "medium",
        timeStyle: "short"
      }),
    [lang]
  );

  const refreshProposals = useCallback(async () => {
    if (!canContribute) {
      setProposals([]);
      return;
    }
    try {
      const response = await listProposals(objectID);
      setProposals(response.items.filter((item) => item.status === "pending"));
    } catch (failure) {
      setError(failure);
    }
  }, [canContribute, objectID]);

  const refreshConfirmations = useCallback(async () => {
    try {
      const response = await listConfirmations("pending");
      setConfirmations(
        response.items.filter((item) =>
          item.target_object_id
            ? item.target_object_id === objectID
            : item.resource_type === "living_object" && item.resource_id === objectID
        )
      );
    } catch (failure) {
      setError(failure);
    }
  }, [objectID]);

  useEffect(() => {
    let cancelled = false;
    setConversationID(null);
    setMessages([]);
    setNotice("");
    setError(null);
    void listConversations().then(async (response) => {
      if (cancelled) {
        return;
      }
      const bound = response.items.find((item) => item.object_id === objectID);
      if (!bound) {
        return;
      }
      setConversationID(bound.id);
      try {
        const detail = await getConversation(bound.id);
        if (!cancelled) {
          setMessages(detail.messages);
        }
      } catch {
        // A stale binding is not fatal; a new conversation is created on send.
      }
    });
    void refreshProposals();
    void refreshConfirmations();
    return () => {
      cancelled = true;
    };
  }, [objectID, refreshConfirmations, refreshProposals]);

  async function send(contentOverride?: string) {
    const content = (contentOverride ?? draft).trim();
    if (!content || busy) {
      return;
    }
    setBusy("send");
    setNotice("");
    setError(null);
    setCreatedChild(null);
    try {
      let activeConversation = conversationID;
      if (!activeConversation) {
        const created = await createConversation({
          title: `AI · ${objectTitle}`.slice(0, 120),
          object_id: objectID
        });
        activeConversation = created.id;
        setConversationID(created.id);
      }
      const result = await sendAIMessage(activeConversation, content);
      setMessages((previous) => [...previous, result.user_message, result.assistant_message]);
      setDraft("");
      const trace = { mode: result.model_mode, at: new Date().toISOString() };
      try {
        window.sessionStorage.setItem("dw:last-ai-inference", JSON.stringify(trace));
      } catch {
        // Session storage is optional; the event still updates an open dialog.
      }
      window.dispatchEvent(new CustomEvent("dw:ai-inference", { detail: trace }));
      if (result.context_stale) {
        setNotice(t("workspace.aiStale"));
      } else if (result.parse_status === "structured" && result.proposed_actions.length === 0) {
        setNotice(t("workspace.aiNoAction"));
      }
      await refreshProposals();
      await refreshConfirmations();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function apply(item: Proposal) {
    if (!window.confirm(t("workspace.applyConfirm"))) {
      return;
    }
    setBusy(`apply:${item.id}`);
    setNotice("");
    setError(null);
    try {
      await applyProposal(objectID, item.id, objectVersion);
      setNotice(t("workspace.applied"));
      onChanged();
      await refreshProposals();
      await refreshConfirmations();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function deny(item: Proposal) {
    setBusy(`deny:${item.id}`);
    setError(null);
    try {
      await denyProposal(objectID, item.id);
      setNotice(t("workspace.denied"));
      await refreshProposals();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function approve(item: Confirmation) {
    if (!window.confirm(t("workspace.confirmApprove"))) {
      return;
    }
    setBusy(`approve:${item.id}`);
    setNotice("");
    setError(null);
    try {
      const created = await approveConfirmation(item.id);
      setCreatedChild(created);
      setNotice(t("workspace.approved"));
      onChanged();
      await refreshConfirmations();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function denyConfirmationItem(item: Confirmation) {
    setBusy(`deny-confirmation:${item.id}`);
    setError(null);
    try {
      await denyConfirmation(item.id);
      setNotice(t("workspace.denied"));
      await refreshConfirmations();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  return (
    <section className="card ai-panel" data-testid="ai-panel">
      <div className="section-heading">
        <h2>{t("workspace.ai")}</h2>
        <small>{canContribute ? t("workspace.permission.contribute") : t("workspace.permission.read")}</small>
      </div>
      <p className="muted">{t("workspace.aiIntro")}</p>
      {mode === "fixture" ? <p className="fixture-inline">{t("workspace.aiFixture")}</p> : null}
      {notice ? <StatusNotice>{notice}</StatusNotice> : null}
      {createdChild ? (
        <p className="status-notice status-success">
          {t("workspace.approvalCreated", {
            title:
              typeof createdChild.state.title === "string" ? createdChild.state.title : createdChild.id,
            version: createdChild.version
          })}{" "}
          <Link to={`/objects/${createdChild.id}?tab=work`}>{t("workspace.openCreated")} →</Link>
        </p>
      ) : null}
      {error ? (
        <ErrorNotice error={error} compact />
      ) : null}
      {describeError(error).code === "payload_too_large" ? (
        <p className="form-error" role="alert">
          {t("ai.handoffTooLarge")}
        </p>
      ) : null}

      <div className="ai-messages" aria-live="polite">
        {messages.length === 0 ? <p className="muted">{t("workspace.aiEmpty")}</p> : null}
        {messages.map((message) => (
          <div key={message.id} className={`ai-message ai-${message.role}`}>
            <small>
              {message.role === "user" ? t("workspace.you") : t("workspace.ai")}
              {message.model_mode === "fixture" ? ` · ${t("mode.fixture")}` : ""}
              {message.parse_status ? ` · ${message.parse_status}` : ""}
            </small>
            <p>{message.content}</p>
          </div>
        ))}
      </div>

      <label className="ai-compose">
        <span className="visually-hidden">{t("workspace.aiPlaceholder")}</span>
        <textarea
          rows={3}
          maxLength={16384}
          value={draft}
          placeholder={t("workspace.aiPlaceholder")}
          onChange={(event) => setDraft(event.target.value)}
        />
      </label>
      <div className="form-actions">
        <button
          className="button button-small"
          type="button"
          disabled={!draft.trim() || busy !== "" || draftBytes > 16384}
          onClick={() => void send()}
        >
          {busy === "send" ? t("workspace.aiSending") : t("workspace.aiSend")}
        </button>
        <small className={draftBytes > 16384 ? "form-error" : "muted"}>
          {t("ai.messageBytes", { bytes: draftBytes })}
        </small>
        {mode === "fixture" ? (
          <>
            <button
              className="chip-button"
              type="button"
              disabled={busy !== ""}
              onClick={() => void send(t("workspace.aiQuickPropose"))}
            >
              {t("workspace.aiQuickPropose")}
            </button>
            <button
              className="chip-button"
              type="button"
              disabled={busy !== ""}
              onClick={() => void send(t("workspace.aiQuickDerive"))}
            >
              {t("workspace.aiQuickDerive")}
            </button>
            <button
              type="button"
              className="chip-button"
              disabled={busy !== ""}
              onClick={() => void send(t("workspace.aiQuickHello"))}
            >
              {t("workspace.aiQuickHello")}
            </button>
          </>
        ) : null}
      </div>

      {proposals.length > 0 ? (
        <>
          <h3>{t("workspace.proposal")}</h3>
          <p className="form-hint">{t("workspace.proposalHint")}</p>
          {proposals.map((item) => {
            const preview = proposalPreview(item, object);
            return (
              <article key={item.id} className="proposal-card">
                <div className="proposal-head">
                  <strong>{t(actionKey(item.action))}</strong>
                  <small>{t("workspace.baseVersion", { version: item.base_version })}</small>
                </div>
                <dl className="proposal-fields">
                  {preview.fields.map((field, index) => (
                    <div key={`${field.labelKey}-${index}`}>
                      <dt>{t(field.labelKey)}</dt>
                      <dd>{field.value.length > 400 ? `${field.value.slice(0, 400)}…` : field.value}</dd>
                    </div>
                  ))}
                </dl>
                {canContribute ? (
                  <div className="form-actions">
                    <button
                      className="button button-small"
                      type="button"
                      disabled={busy !== ""}
                      onClick={() => void apply(item)}
                    >
                      {t("workspace.apply")}
                    </button>
                    <button
                      className="button button-small button-ghost"
                      type="button"
                      disabled={busy !== ""}
                      onClick={() => void deny(item)}
                    >
                      {t("workspace.deny")}
                    </button>
                  </div>
                ) : null}
              </article>
            );
          })}
        </>
      ) : null}

      <h3>{t("workspace.confirmations")}</h3>
      <p className="form-hint">{t("workspace.confirmationHint")}</p>
      {confirmations.length === 0 ? <p className="muted">{t("workspace.noConfirmations")}</p> : null}
      {confirmations.map((item) => {
        const summary =
          item.action === "derive.create"
            ? t("confirmation.deriveSummary")
            : item.action === "capability.invoke"
              ? t("confirmation.capabilitySummary")
              : t("confirmation.invitationSummary");
        const stale =
          typeof item.bound_version === "number" &&
          item.target_object_version > 0 &&
          item.bound_version < item.target_object_version;
        return (
          <article key={item.id} className="proposal-card confirmation-card" data-testid="confirmation-card">
            <strong>{t("workspace.confirmation")}</strong>
            <span className="mono">{t(`confirmation.action.${item.action}`)}</span>
            <p>{summary}</p>
            {item.effect ? (
              <dl className="proposal-fields">
                {item.effect.title ? (
                  <div>
                    <dt>{t("confirmation.childTitle")}</dt>
                    <dd>{item.effect.title}</dd>
                  </div>
                ) : null}
                {item.effect.inheritance_fields && item.effect.inheritance_fields.length > 0 ? (
                  <div>
                    <dt>{t("confirmation.fields")}</dt>
                    <dd>{item.effect.inheritance_fields.join(", ")}</dd>
                  </div>
                ) : null}
                {typeof item.effect.source_version === "number" ? (
                  <div>
                    <dt>{t("confirmation.sourceVersion")}</dt>
                    <dd className="mono">v{item.effect.source_version}</dd>
                  </div>
                ) : null}
              </dl>
            ) : null}
            <small>
              {item.target_object_title || t("common.unknown")}
              {item.target_object_version ? ` · v${item.target_object_version}` : ""} ·{" "}
              {timeFormatter.format(new Date(item.expires_at))}
            </small>
            {typeof item.bound_version === "number" ? (
              <small className="mono">{t("confirmation.boundVersion", { version: item.bound_version })}</small>
            ) : null}
            {stale ? (
              <p className="form-error" role="alert">
                {t("confirmation.stale")}
              </p>
            ) : null}
            <div className="form-actions">
              <button
                className="button button-small"
                type="button"
                disabled={busy !== ""}
                onClick={() => void approve(item)}
              >
                {t("workspace.confirmApprove")}
              </button>
              <button
                className="button button-small button-ghost"
                type="button"
                disabled={busy !== ""}
                onClick={() => void denyConfirmationItem(item)}
              >
                {t("workspace.confirmDeny")}
              </button>
            </div>
          </article>
        );
      })}
    </section>
  );
}
