// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  approveConfirmation,
  createConversation,
  denyConfirmation,
  getAIProfile,
  getConversation,
  listConfirmations,
  listConversations,
  sendAIMessage,
  type AIMessage,
  type Confirmation,
  type ObjectView,
  type PersonalAIProfile
} from "../../api";
import { ErrorNotice } from "../../errors";
import { useI18n } from "../../i18n";
import { useMeta } from "../../meta";
import Icon from "./Icon";
import Modal from "./Modal";

type Trace = { mode: "fixture" | "real"; at: string };

function profileName(profile: PersonalAIProfile | null, fallback: string): string {
  const name = profile?.name?.trim();
  return name && name.length > 0 ? name : fallback;
}

export default function PersonalAIDialog({
  open,
  onClose,
  onWorldCreated
}: {
  open: boolean;
  onClose: () => void;
  onWorldCreated: (object: ObjectView) => void;
}) {
  const { t, lang } = useI18n();
  const { mode } = useMeta();
  const zh = lang === "zh";
  const [profile, setProfile] = useState<PersonalAIProfile | null>(null);
  const [conversationID, setConversationID] = useState<string | null>(null);
  const [messages, setMessages] = useState<AIMessage[]>([]);
  const [confirmations, setConfirmations] = useState<Confirmation[]>([]);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [lastMode, setLastMode] = useState<"fixture" | "real" | null>(null);

  const worldProposals = useMemo(
    () => confirmations.filter((item) => item.action === "world.create"),
    [confirmations]
  );

  const loadPending = useCallback(async () => {
    try {
      const response = await listConfirmations("pending");
      setConfirmations(response.items);
    } catch (failure) {
      setError(failure);
    }
  }, []);

  const loadConversation = useCallback(async () => {
    const response = await listConversations();
    const general = response.items.find((item) => item.object_id === null) ?? null;
    if (!general) {
      setConversationID(null);
      setMessages([]);
      return;
    }
    setConversationID(general.id);
    const detail = await getConversation(general.id);
    setMessages(detail.messages);
  }, []);

  useEffect(() => {
    if (!open) {
      return;
    }
    let cancelled = false;
    setError(null);
    setNotice("");
    setBusy("");
    setLastMode(null);
    try {
      const raw = window.sessionStorage.getItem("dw:last-ai-inference");
      if (raw) {
        const parsed = JSON.parse(raw) as Trace;
        if (parsed.mode === "fixture" || parsed.mode === "real") {
          setLastMode(parsed.mode);
        }
      }
    } catch {
      // Session storage is optional.
    }
    void getAIProfile()
      .then((value) => {
        if (!cancelled) {
          setProfile(value);
        }
      })
      .catch((failure) => {
        if (!cancelled) {
          setError(failure);
        }
      });
    void loadConversation().catch((failure) => {
      if (!cancelled) {
        setError(failure);
      }
    });
    void loadPending();
    return () => {
      cancelled = true;
    };
  }, [open, loadConversation, loadPending]);

  async function send(contentOverride?: string) {
    const content = (contentOverride ?? draft).trim();
    if (!content || busy) {
      return;
    }
    setBusy("send");
    setError(null);
    setNotice("");
    try {
      let active = conversationID;
      if (!active) {
        const created = await createConversation({ title: t("ai.universal.title").slice(0, 120) });
        active = created.id;
        setConversationID(created.id);
      }
      const result = await sendAIMessage(active, content);
      setMessages((previous) => [...previous, result.user_message, result.assistant_message]);
      setDraft("");
      setLastMode(result.model_mode);
      const trace = { mode: result.model_mode, at: new Date().toISOString() };
      try {
        window.sessionStorage.setItem("dw:last-ai-inference", JSON.stringify(trace));
      } catch {
        // Session storage is optional.
      }
      window.dispatchEvent(new CustomEvent("dw:ai-inference", { detail: trace }));
      if (result.proposed_actions.some((action) => action.action === "world.create")) {
        setNotice(t("ai.universal.proposalHint"));
      } else if ((result.rejected_actions ?? 0) > 0) {
        setNotice(t("ai.universal.rejected"));
      } else if (result.parse_status === "structured") {
        setNotice(t("ai.universal.noProposal"));
      }
      await loadPending();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function createProposed(item: Confirmation) {
    setBusy(`approve:${item.id}`);
    setError(null);
    try {
      const object = await approveConfirmation(item.id);
      setNotice(t("ai.universal.approved"));
      setConfirmations((current) => current.filter((entry) => entry.id !== item.id));
      onWorldCreated(object);
      onClose();
    } catch (failure) {
      setError(failure);
      await loadPending();
    } finally {
      setBusy("");
    }
  }

  async function dismissProposal(item: Confirmation) {
    setBusy(`deny:${item.id}`);
    setError(null);
    try {
      await denyConfirmation(item.id);
      setConfirmations((current) => current.filter((entry) => entry.id !== item.id));
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function newConversation() {
    if (busy) {
      return;
    }
    setBusy("new");
    setError(null);
    setNotice("");
    try {
      const created = await createConversation({ title: t("ai.universal.title").slice(0, 120) });
      setConversationID(created.id);
      setMessages([]);
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  const activeMode = lastMode ?? mode;

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={t("ai.universal.title")}
      kicker={t("ai.universal.kicker")}
      wide
      className="dw-personal-ai-modal"
      actions={
        <>
          <button className="dw-button dw-button-ghost" type="button" onClick={() => void newConversation()} disabled={busy !== ""}>
            {t("ai.universal.newConversation")}
          </button>
          <button className="dw-button dw-button-ghost" type="button" onClick={onClose}>
            {t("common.close")}
          </button>
        </>
      }
    >
      <div className="dw-personal-ai" data-testid="personal-ai-dialog">
        <div className="dw-personal-ai-identity">
          <span className="dw-personal-ai-avatar" aria-hidden="true">
            {(profile ? profileName(profile, t("ai.universal.title")) : t("ai.universal.title"))
              .slice(0, 1)
              .toUpperCase()}
          </span>
          <div>
            <strong>{profileName(profile, t("ai.universal.title"))}</strong>
            <p>{t("ai.universal.lead")}</p>
            <small>{t("ai.universal.identity")}</small>
          </div>
          <span className={`dw-personal-ai-mode is-${activeMode}`}>
            <Icon name={activeMode === "fixture" ? "layers" : "spark"} size={13} />
            {activeMode === "fixture" ? t("mode.fixture") : activeMode === "real" ? t("mode.real") : t("mode.unknown")}
          </span>
        </div>

        <div className="dw-personal-ai-context">
          <span className="dw-personal-ai-general">
            <Icon name="world" size={14} />
            {t("ai.universal.general")}
          </span>
          <span className="dw-personal-ai-note">
            {activeMode === "fixture" ? t("ai.universal.fixtureNote") : t("ai.universal.realNote")}
          </span>
        </div>

        {worldProposals.length > 0 ? (
          <section className="dw-personal-ai-proposals" aria-label={t("ai.universal.worldProposal")}>
            <h3>{t("ai.universal.worldProposal")}</h3>
            {worldProposals.map((item) => (
              <article key={item.id} className="dw-personal-ai-proposal" data-testid="world-proposal">
                <div>
                  <p className="dw-inspect-kicker">{t("ai.universal.pending")}</p>
                  <strong>{item.effect?.title || item.target_object_title || t("ai.universal.worldProposal")}</strong>
                  {item.effect?.summary ? <p>{item.effect.summary}</p> : null}
                  {typeof item.effect?.node_count === "number" ? (
                    <small>
                      {t("openWorld.summary", {
                        kinds: item.effect.kind_count ?? 0,
                        nodes: item.effect.node_count ?? 0,
                        rules: item.effect.rule_count ?? 0
                      })}
                    </small>
                  ) : null}
                  <small>{item.permission_summary}</small>
                </div>
                <div className="dw-personal-ai-proposal-actions">
                  <button
                    className="dw-button dw-button-primary dw-button-small"
                    type="button"
                    disabled={busy !== ""}
                    onClick={() => void createProposed(item)}
                  >
                    {busy === `approve:${item.id}` ? t("ai.universal.creating") : t("ai.universal.createWorld")}
                  </button>
                  <button
                    className="dw-button dw-button-ghost dw-button-small"
                    type="button"
                    disabled={busy !== ""}
                    onClick={() => void dismissProposal(item)}
                  >
                    {t("ai.universal.deny")}
                  </button>
                </div>
              </article>
            ))}
          </section>
        ) : null}

        {notice ? (
          <p className="dw-form-success" role="status">
            {notice}
          </p>
        ) : null}
        {error ? <ErrorNotice error={error} compact /> : null}

        <div className="dw-personal-ai-messages" aria-live="polite" data-testid="personal-ai-messages">
          {messages.length === 0 ? <p className="muted">{t("ai.universal.empty")}</p> : null}
          {messages.map((message) => (
            <div
              key={message.id}
              className={`dw-personal-ai-message is-${message.role}`}
              data-testid={`personal-ai-${message.role}`}
            >
              <small>
                {message.role === "user" ? (zh ? "你" : "You") : profileName(profile, t("ai.universal.title"))}
                {message.model_mode === "fixture" ? ` · ${t("mode.fixture")}` : ""}
              </small>
              <p>{message.content}</p>
            </div>
          ))}
        </div>

        {messages.length === 0 && worldProposals.length === 0 ? (
          <div className="dw-personal-ai-suggestions">
            <button type="button" className="dw-chip" onClick={() => void send(t("ai.universal.quickFirst"))} disabled={busy !== ""}>
              <Icon name="spark" size={14} />
              {t("ai.universal.quickFirst")}
            </button>
            <button type="button" className="dw-chip" onClick={() => void send(t("ai.universal.quickCity"))} disabled={busy !== ""}>
              <Icon name="world" size={14} />
              {t("ai.universal.quickCity")}
            </button>
          </div>
        ) : null}

        <form
          className="dw-personal-ai-compose"
          onSubmit={(event) => {
            event.preventDefault();
            void send();
          }}
        >
          <label className="visually-hidden" htmlFor="dw-personal-ai-draft">
            {t("ai.universal.placeholder")}
          </label>
          <textarea
            id="dw-personal-ai-draft"
            rows={3}
            maxLength={16384}
            value={draft}
            placeholder={t("ai.universal.placeholder")}
            onChange={(event) => setDraft(event.target.value)}
          />
          <button
            className="dw-button dw-button-primary"
            type="submit"
            disabled={!draft.trim() || busy !== ""}
          >
            {busy === "send" ? t("ai.universal.sending") : t("ai.universal.send")}
            <Icon name="send" size={15} />
          </button>
        </form>
        <p className="dw-fineprint">
          <Icon name="shield" size={13} />
          {zh
            ? "AI 只能提出方案。世界创建、提议应用与衍生都必须由你确认；服务商密钥永不进入浏览器。"
            : "The AI only proposes. World creation, proposal application, and derivation all require your confirmation; provider keys never reach the browser."}
        </p>
      </div>
    </Modal>
  );
}
