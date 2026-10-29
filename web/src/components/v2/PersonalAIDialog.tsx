// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  approveConfirmation,
  createConversation,
  denyConfirmation,
  getAIProfile,
  getConfirmation,
  getConversation,
  getObject,
  listConfirmations,
  listConversations,
  sendAIMessage,
  type AIMessage,
  type Confirmation,
  type ObjectView,
  type PersonalAIProfile
} from "../../api";
import { describeError, ErrorNotice } from "../../errors";
import { useI18n } from "../../i18n";
import { useMeta } from "../../meta";
import {
  approvalIntent,
  isExpired,
  isPendingConfirmation,
  promptForMessage,
  resultObjectHref,
  sanitizeAssistantText
} from "../../approval";
import { looksLikeJSONContent } from "../../worldLabels";
import Icon from "./Icon";
import InlineApproval, { type ApprovalPhase } from "./InlineApproval";
import Modal from "./Modal";

type Trace = { mode: "fixture" | "real"; at: string };
type TurnOutcome = {
  prompt: string;
  accepted: number;
  rejected: number;
  parseStatus: "structured" | "fallback";
  notPrepared: boolean;
};

type FailureState = { status: "failed"; errorText: string };

function profileName(profile: PersonalAIProfile | null, fallback: string): string {
  const name = profile?.name?.trim();
  return name && name.length > 0 ? name : fallback;
}

function isWorldConfirmation(item: Confirmation): boolean {
  return item.action === "world.create";
}

export default function PersonalAIDialog({
  open,
  onClose,
  onWorldCreated,
  initialPrompt,
  initialConversationID
}: {
  open: boolean;
  onClose: () => void;
  onWorldCreated: (object: ObjectView) => void;
  initialPrompt?: string;
  initialConversationID?: string;
}) {
  const { t, lang } = useI18n();
  const { mode } = useMeta();
  const zh = lang === "zh";
  const [profile, setProfile] = useState<PersonalAIProfile | null>(null);
  const [conversationID, setConversationID] = useState<string | null>(null);
  const [messages, setMessages] = useState<AIMessage[]>([]);
  const [approvals, setApprovals] = useState<Confirmation[]>([]);
  const [pendingElsewhere, setPendingElsewhere] = useState<Confirmation[]>([]);
  const [legacyCreated, setLegacyCreated] = useState<Confirmation[]>([]);
  const [turnOutcomes, setTurnOutcomes] = useState<Record<string, TurnOutcome>>({});
  const [failures, setFailures] = useState<Record<string, FailureState>>({});
  const [dismissed, setDismissed] = useState<Record<string, true>>({});
  const [focusRequest, setFocusRequest] = useState<{ id: string; nonce: number } | null>(null);
  const [revealID, setRevealID] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [lastMode, setLastMode] = useState<"fixture" | "real" | null>(null);
  const [ready, setReady] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const sentInitial = useRef("");
  const focusNonce = useRef(0);
  const transcriptRef = useRef<HTMLDivElement | null>(null);
  const previousMessages = useRef(0);
  const previousLegacyCreated = useRef(0);

  const approvalsByMessage = useMemo(() => {
    const map = new Map<string, Confirmation[]>();
    for (const item of approvals) {
      if (!isWorldConfirmation(item) || !item.message_id || dismissed[item.id]) {
        continue;
      }
      const list = map.get(item.message_id) ?? [];
      list.push(item);
      map.set(item.message_id, list);
    }
    return map;
  }, [approvals, dismissed]);

  const unlinkedApprovals = useMemo(
    () =>
      approvals.filter(
        (item) =>
          isWorldConfirmation(item) &&
          !item.message_id &&
          !dismissed[item.id] &&
          (isPendingConfirmation(item, now) ||
            (item.status === "approved" && Boolean(item.result_object_id)) ||
            (item.status === "expired" && recentlyCreated(item)))
      ),
    [approvals, dismissed, now]
  );

  // While a real plan waits, the decision is the only thing that matters in
  // this dialog. Collapsing the identity/marketing blocks gives the card and
  // its button the room they need on a 768px laptop.
  // A live world plan is the one decision this conversation needs. While it
  // waits, a "try again" card for a world_pending rejection cannot succeed and
  // would compete with the real confirm button.
  const hasLivePlan = useMemo(
    () =>
      approvals.some(
        (item) => isWorldConfirmation(item) && isPendingConfirmation(item, now)
      ),
    [approvals, now]
  );

  const hasApproval = useMemo(
    () =>
      legacyCreated.length > 0 ||
      approvals.some(
        (item) =>
          isWorldConfirmation(item) &&
          !dismissed[item.id] &&
          (item.status === "pending" ||
            (item.status === "approved" && Boolean(item.result_object_id)))
      ),
    [approvals, dismissed, legacyCreated.length]
  );

  const lastAssistantID = useMemo(() => {
    for (let index = messages.length - 1; index >= 0; index -= 1) {
      if (messages[index]?.role === "assistant") {
        return messages[index]?.id ?? null;
      }
    }
    return null;
  }, [messages]);

  const refreshApprovals = useCallback(async (activeConversationID: string | null) => {
    const [all, pendingPage] = await Promise.all([
      listConfirmations("all"),
      listConfirmations("pending")
    ]);
    const worldItems = all.items.filter(isWorldConfirmation);
    // The active conversation owns its linked rows. Legacy rows with no
    // conversation link remain actionable only while pending; already-approved
    // legacy rows become the "created worlds" group below.
    const scoped = worldItems.filter((item) =>
      item.conversation_id
        ? item.conversation_id === activeConversationID
        : item.status === "pending" && isPendingConfirmation(item)
    );
    const createdLegacy = worldItems
      .filter(
        (item) =>
          !item.conversation_id &&
          !item.message_id &&
          item.status === "approved" &&
          Boolean(item.result_object_id)
      )
      .sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at))
      .slice(0, 3);
    setApprovals(scoped);
    setLegacyCreated(createdLegacy);
    const pending = pendingPage.items.filter(
      (item) =>
        isWorldConfirmation(item) &&
        isPendingConfirmation(item) &&
        Boolean(item.conversation_id)
    );
    setPendingElsewhere(
      pending.filter((item) => item.conversation_id !== activeConversationID)
    );
    return { scoped, created: createdLegacy };
  }, []);

  const loadConversation = useCallback(
    async (preferredID?: string | null) => {
      const conversationPage = await listConversations();
      const generals = conversationPage.items.filter((item) => item.object_id === null);
      setPendingElsewhere([]);
      if (generals.length === 0) {
        setConversationID(null);
        setMessages([]);
        setApprovals([]);
        setLegacyCreated([]);
        return null;
      }
      const pendingPage = await listConfirmations("pending");
      const pending = pendingPage.items.filter(
        (item) => isWorldConfirmation(item) && isPendingConfirmation(item)
      );
      const preferred = generals.find((item) => item.id === preferredID);
      let target = preferred;
      if (!target && preferredID) {
        // The requested conversation may be older than the first page or bound
        // to an object. Fetch it directly instead of silently switching to a
        // different conversation and leaving its pending plan unreachable.
        try {
          const requested = await getConversation(preferredID);
          target = requested.conversation;
        } catch {
          target = undefined;
        }
      }
      target =
        target ??
        generals.find((item) => pending.some((entry) => entry.conversation_id === item.id)) ??
        generals[0];
      if (!target) {
        setConversationID(null);
        setMessages([]);
        setApprovals([]);
        return null;
      }
      setConversationID(target.id);
      const detail = await getConversation(target.id);
      previousMessages.current = 0;
      previousLegacyCreated.current = 0;
      setMessages(detail.messages);
      const loaded = await refreshApprovals(target.id);
      const firstPending = loaded?.scoped.find((item) => isPendingConfirmation(item));
      const newestCreated = loaded?.scoped.find(
        (item) => item.status === "approved" && Boolean(item.result_object_id)
      );
      // A created world is an entry point, not a below-the-fold artifact.
      const reveal = firstPending ?? newestCreated ?? loaded?.created[0];
      if (reveal) {
        setRevealID(reveal.id);
      }
      return target.id;
    },
    [refreshApprovals]
  );

  useEffect(() => {
    if (!open) {
      return;
    }
    let cancelled = false;
    setError(null);
    setNotice("");
    setBusy("");
    setLastMode(null);
    setReady(false);
    setFailures({});
    setDismissed({});
    setTurnOutcomes({});
    setLegacyCreated([]);
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
    void loadConversation(initialConversationID)
      .catch((failure) => {
        if (!cancelled) {
          setError(failure);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setReady(true);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [open, initialConversationID, loadConversation]);

  // Wait until the conversation list has loaded before auto-sending. Sending
  // first creates a second conversation and can overwrite the newest turn.
  useEffect(() => {
    if (!open || !ready) {
      return;
    }
    if (!initialPrompt || sentInitial.current === initialPrompt) {
      return;
    }
    sentInitial.current = initialPrompt;
    void send(initialPrompt, "create");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, ready, initialPrompt]);

  useEffect(() => {
    if (!open) {
      sentInitial.current = "";
    }
  }, [open]);

  // Re-evaluate expiry while the modal stays open, so a stale plan can never
  // keep offering a button the server will refuse.
  useEffect(() => {
    if (!open) {
      return;
    }
    const timer = window.setInterval(() => setNow(Date.now()), 15_000);
    return () => window.clearInterval(timer);
  }, [open]);

  // Reconcile with the server when the tab regains focus: an approval may have
  // been executed in another tab, or a new plan may have arrived.
  useEffect(() => {
    if (!open || !conversationID) {
      return;
    }
    const reconcile = () => {
      void refreshApprovals(conversationID).catch(() => undefined);
    };
    window.addEventListener("focus", reconcile);
    document.addEventListener("visibilitychange", reconcile);
    return () => {
      window.removeEventListener("focus", reconcile);
      document.removeEventListener("visibilitychange", reconcile);
    };
  }, [open, conversationID, refreshApprovals]);

  // Move the user to the real button when a plan arrives or when they ask to
  // approve in text. Scrolling alone is not enough for keyboard users.
  useEffect(() => {
    if (!open || !focusRequest) {
      return;
    }
    const timer = window.setTimeout(() => {
      const selector = `[data-approval-primary="${focusRequest.id}"]`;
      const node = document.querySelector<HTMLElement>(selector);
      if (!node) {
        return;
      }
      const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;
      node.scrollIntoView({ block: "nearest", behavior: reduced ? "auto" : "smooth" });
      node.focus({ preventScroll: true });
      setFocusRequest((current) => (current?.nonce === focusRequest.nonce ? null : current));
    }, 140);
    return () => window.clearTimeout(timer);
  }, [open, focusRequest, approvals, messages]);

  // On load, reveal a waiting plan without stealing focus from the composer.
  useEffect(() => {
    if (!open || !revealID) {
      return;
    }
    const timer = window.setTimeout(() => {
      const node =
        document.querySelector<HTMLElement>(`[data-approval-primary="${revealID}"]`) ??
        document.querySelector<HTMLElement>(`[data-approval-id="${revealID}"]`);
      if (node) {
        const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;
        // nearest keeps the plan and its button together when the card fits,
        // and at minimum guarantees the real button is visible when it does not.
        node.scrollIntoView({ block: "nearest", behavior: reduced ? "auto" : "smooth" });
      }
      setRevealID(null);
    }, 160);
    return () => window.clearTimeout(timer);
  }, [open, revealID, approvals]);

  useEffect(() => {
    const node = transcriptRef.current;
    const messagesAdded = messages.length > previousMessages.current;
    const legacyAdded = legacyCreated.length > previousLegacyCreated.current;
    previousMessages.current = messages.length;
    previousLegacyCreated.current = legacyCreated.length;
    if (!node || focusRequest || revealID) {
      return;
    }
    // Pin to the newest turn or a newly arrived legacy entry only when content
    // was actually added. Clearing a reveal/focus request must never undo the
    // scroll that just showed the real button.
    if (messagesAdded || legacyAdded) {
      node.scrollTop = node.scrollHeight;
    }
  }, [
    messages.length,
    approvals.length,
    legacyCreated.length,
    busy,
    focusRequest,
    revealID
  ]);

  function requestFocus(confirmationID: string) {
    focusNonce.current += 1;
    setFocusRequest({ id: confirmationID, nonce: focusNonce.current });
  }

  async function pendingWorldConfirmations(): Promise<Confirmation[]> {
    const page = await listConfirmations("pending");
    const pending = page.items.filter(
      (item) => isWorldConfirmation(item) && isPendingConfirmation(item)
    );
    setPendingElsewhere(
      pending.filter(
        (item) => Boolean(item.conversation_id) && item.conversation_id !== conversationID
      )
    );
    return pending;
  }

  async function send(contentOverride?: string, intent: "create" | "change" = "create") {
    const content = (contentOverride ?? draft).trim();
    if (!content || busy) {
      return;
    }

    // "批准通过" is not a new request. If a real confirmation waits, bring the
    // user to its button; if none waits, say so without spending a model turn.
    const intentKind = approvalIntent(content);
    if (intentKind) {
      try {
        const pending = await pendingWorldConfirmations();
        const target = pending[0];
        if (target) {
          if (target.conversation_id && target.conversation_id !== conversationID) {
            await switchConversation(target.conversation_id);
          }
          setDraft("");
          setNotice(t("ai.approval.useButton"));
          requestFocus(target.id);
          return;
        }
      } catch {
        // Falling through to a normal turn is safer than blocking on a
        // transient read failure only for a weak acknowledgement.
      }
      if (intentKind === "strong") {
        setDraft("");
        setNotice(t("ai.approval.nonePending"));
        return;
      }
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
      const result = await sendAIMessage(active, content, intent);
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
      setTurnOutcomes((previous) => ({
        ...previous,
        [result.assistant_message.id]: {
          prompt: content,
          accepted: result.proposed_actions.length,
          rejected: result.rejected_actions ?? 0,
          parseStatus: result.parse_status,
          notPrepared:
            result.proposed_actions.length === 0 &&
            (result.parse_status === "fallback" || (result.rejections?.length ?? 0) > 0)
        }
      }));
      if (result.proposed_actions.some((action) => action.action === "world.create")) {
        setNotice(t("ai.universal.proposalHint"));
      } else if (result.parse_status === "structured" && result.proposed_actions.length === 0) {
        setNotice(t("ai.universal.noProposal"));
      }
      await refreshApprovals(active);
      const firstConfirmation = result.proposed_actions.find(
        (action) => action.kind === "confirmation" && action.confirmation_id
      );
      if (firstConfirmation?.confirmation_id) {
        // Scroll the new plan into view, but do not steal Enter from the
        // composer; focus moves only when the user explicitly approves in text.
        setRevealID(firstConfirmation.confirmation_id);
      }
    } catch (failure) {
      setError(failure);
      // Keep the user's idea so a failure never costs them the sentence.
      if (contentOverride) {
        setDraft(content);
      }
    } finally {
      setBusy("");
    }
  }

  async function switchConversation(targetID: string) {
    if (busy) {
      return;
    }
    setBusy("switch");
    setError(null);
    try {
      setConversationID(targetID);
      const detail = await getConversation(targetID);
      previousMessages.current = 0;
      previousLegacyCreated.current = 0;
      setMessages(detail.messages);
      setFailures({});
      setTurnOutcomes({});
      const loaded = await refreshApprovals(targetID);
      const firstPending = loaded.scoped.find((item) => isPendingConfirmation(item));
      const newestCreated = loaded.scoped.find(
        (item) => item.status === "approved" && Boolean(item.result_object_id)
      );
      const reveal = firstPending ?? newestCreated ?? loaded.created[0];
      if (reveal) {
        setRevealID(reveal.id);
      }
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function createProposed(item: Confirmation) {
    setBusy(`approve:${item.id}`);
    setError(null);
    setNotice("");
    setFailures((previous) => {
      const next = { ...previous };
      delete next[item.id];
      return next;
    });
    try {
      const object = await approveConfirmation(item.id);
      setNotice(t("ai.universal.approved"));
      setApprovals((current) =>
        current.map((entry) =>
          entry.id === item.id
            ? {
                ...entry,
                status: "approved",
                result_object_id: object.id,
                result_object_title:
                  typeof object.state.title === "string" ? object.state.title : entry.target_object_title,
                result_object_type: object.object_type
              }
            : entry
        )
      );
      onWorldCreated(object);
      onClose();
    } catch (failure) {
      const described = describeError(failure);
      // Another tab may have approved this exact confirmation. That is a
      // created world, not a failure: reconcile and show the real entry.
      if (described.code === "conflict") {
        const reconciled = await reconcileApproval(item);
        if (reconciled) {
          return;
        }
      }
      setError(failure);
      setFailures((previous) => ({
        ...previous,
        [item.id]: { status: "failed", errorText: t(described.messageKey) }
      }));
      try {
        await refreshApprovals(conversationID);
      } catch {
        // The inline failure state already explains the outcome.
      }
    } finally {
      setBusy("");
    }
  }

  async function reconcileApproval(item: Confirmation): Promise<boolean> {
    try {
      const current = await getConfirmation(item.id);
      if (current.status === "approved") {
        setApprovals((entries) =>
          entries.map((entry) => (entry.id === item.id ? { ...entry, ...current } : entry))
        );
        if (current.result_object_id) {
          try {
            const object = await getObject(current.result_object_id);
            onWorldCreated(object);
            onClose();
          } catch {
            // The world exists but is no longer readable; the created card
            // still tells the truth without a dead link.
          }
        }
        return true;
      }
      if (current.status === "denied" || current.status === "expired") {
        setApprovals((entries) => entries.filter((entry) => entry.id !== item.id));
        return true;
      }
    } catch (reconcileError) {
      if (describeError(reconcileError).code === "confirmation_expired") {
        setApprovals((entries) =>
          entries.map((entry) =>
            entry.id === item.id ? { ...entry, status: "expired" } : entry
          )
        );
        return true;
      }
    }
    return false;
  }

  async function dismissProposal(item: Confirmation) {
    if (item.status === "expired" || isExpired(item.expires_at, now)) {
      setDismissed((previous) => ({ ...previous, [item.id]: true }));
      return;
    }
    setBusy(`deny:${item.id}`);
    setError(null);
    try {
      await denyConfirmation(item.id);
      setApprovals((current) => current.filter((entry) => entry.id !== item.id));
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function prepareAgain(item: Confirmation) {
    const prompt = item.message_id ? promptForMessage(messages, item.message_id) : "";
    if (!prompt) {
      setNotice(t("ai.approval.nonePending"));
      return;
    }
    setDismissed((previous) => ({ ...previous, [item.id]: true }));
    await send(prompt, "create");
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
      setApprovals([]);
      setLegacyCreated([]);
      setFailures({});
      setTurnOutcomes({});
      setPendingElsewhere([]);
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
      title={zh ? "和我的 AI 一起创造" : "Create with my AI"}
      kicker={zh ? "世界创造" : "WORLD CREATION"}
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
      <div
        className={`dw-personal-ai${hasApproval ? " has-approval" : ""}`}
        data-testid="personal-ai-dialog"
      >
        <div className="dw-personal-ai-identity">
          <span className="dw-personal-ai-avatar" aria-hidden="true">
            {(profile ? profileName(profile, t("ai.universal.title")) : t("ai.universal.title"))
              .slice(0, 1)
              .toUpperCase()}
          </span>
          <div>
            <strong>{profileName(profile, t("ai.universal.title"))}</strong>
            <p>{zh ? "说出你想创造的世界。方案和确认按钮会直接出现在回复下方。" : "Describe the world you want. The plan and its confirm button appear directly under the reply."}</p>
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
            {zh ? "创造模式 · 每次创建都由你确认" : "Creation mode · you approve every world"}
          </span>
          <span className="dw-personal-ai-note">
            {activeMode === "fixture" ? t("ai.universal.fixtureNote") : t("ai.universal.realNote")}
          </span>
        </div>

        {pendingElsewhere.length > 0 ? (
          <div className="dw-approval-elsewhere" role="status">
            <span>
              <Icon name="clock" size={14} />
              {t("ai.approval.pendingElsewhere", { count: pendingElsewhere.length })}
            </span>
            <button
              className="dw-button dw-button-ghost dw-button-small"
              type="button"
              onClick={() =>
                void switchConversation(
                  pendingElsewhere.find((item) => item.conversation_id)?.conversation_id ?? ""
                )
              }
            >
              {t("ai.approval.openElsewhere")}
            </button>
          </div>
        ) : null}

        {notice ? (
          <p className="dw-form-success" role="status">
            {notice}
          </p>
        ) : null}
        {error ? <ErrorNotice error={error} compact /> : null}

        <div className="dw-personal-ai-messages" aria-live="polite" ref={transcriptRef} data-testid="personal-ai-messages">
          {messages.length === 0 && busy === "" ? <p className="muted">{t("ai.universal.empty")}</p> : null}
          {messages.map((message) => {
            const messageApprovals = message.role === "assistant" ? approvalsByMessage.get(message.id) ?? [] : [];
            const hasResult = messageApprovals.some(
              (item) => item.status === "approved" && Boolean(item.result_object_id)
            );
            const hasPending = messageApprovals.some((item) => isPendingConfirmation(item, now));
            const sanitized =
              message.role === "assistant"
                ? sanitizeAssistantText(message.content, zh, { hasResult, hasPending })
                : null;
            const isLastAssistant = message.role === "assistant" && message.id === lastAssistantID;
            const outcome = turnOutcomes[message.id];
            const hasCards = messageApprovals.length > 0;
            const showUnprepared =
              isLastAssistant &&
              !hasCards &&
              !hasLivePlan &&
              (Boolean(message.notice) ||
                message.parse_status === "fallback" ||
                Boolean(outcome?.notPrepared));
            return (
              <div
                key={message.id}
                className={`dw-ai-turn is-${message.role}`}
                data-message-id={message.id}
                data-testid="ai-turn"
              >
                <div
                  className={`dw-personal-ai-message is-${message.role}`}
                  data-testid={`personal-ai-${message.role}`}
                >
                  <small>
                    {message.role === "user" ? (zh ? "你" : "You") : profileName(profile, t("ai.universal.title"))}
                  </small>
                  {looksLikeJSONContent(message.content) ? (
                    <p className="dw-ai-unsafe">
                      {zh ? "这条旧消息不是可读的对话内容，已隐藏。请重新提出你的想法。" : "This old message is not readable conversation; it has been hidden. Please ask again."}
                    </p>
                  ) : (
                    <p>{sanitized ? sanitized.text : message.content}</p>
                  )}
                  {sanitized?.legacyGuidance ? (
                    <p className="dw-ai-notice" role="status">
                      <Icon name="shield" size={13} />
                      {t("ai.approval.legacyGuidance")}
                    </p>
                  ) : null}
                  {message.notice ? (
                    <p className="dw-ai-notice" role="status">
                      <Icon name="shield" size={13} />
                      {message.notice}
                    </p>
                  ) : null}
                </div>
                {messageApprovals.map((item) => renderApproval(item))}
                {showUnprepared ? (
                  <div className="dw-ai-unprepared" role="status">
                    <strong>{t("ai.approval.notPrepared")}</strong>
                    <p>{t("ai.approval.notPreparedBody")}</p>
                    <button
                      className="dw-button dw-button-ghost dw-button-small"
                      type="button"
                      disabled={busy !== ""}
                      onClick={() => void send(promptForMessage(messages, message.id), "create")}
                    >
                      <Icon name="reset" size={14} />
                      {t("ai.approval.retry")}
                    </button>
                  </div>
                ) : null}
              </div>
            );
          })}

          {unlinkedApprovals.length > 0 || legacyCreated.length > 0 ? (
            <div className="dw-ai-turn">
              <p className="dw-approval-fallback-label">
                {unlinkedApprovals.length > 0
                  ? t("ai.approval.pendingShort")
                  : t("ai.approval.createdWorlds")}
              </p>
              {unlinkedApprovals.map((item) => renderApproval(item))}
              {legacyCreated.map((item) => renderApproval(item))}
            </div>
          ) : null}

          {busy === "send" ? (
            <div className="dw-personal-ai-message is-assistant is-pending" role="status">
              <small>{profileName(profile, t("ai.universal.title"))}</small>
              <p>
                <span className="dw-thinking-dots" aria-hidden="true">
                  <i />
                  <i />
                  <i />
                </span>
                {zh ? "正在准备方案…" : "Preparing a plan…"}
              </p>
            </div>
          ) : null}
        </div>

        {messages.length === 0 && approvals.length === 0 && busy === "" ? (
          <div className="dw-personal-ai-suggestions">
            <button type="button" className="dw-chip" onClick={() => void send(t("ai.universal.quickFirst"), "create")} disabled={busy !== ""}>
              <Icon name="spark" size={14} />
              {t("ai.universal.quickFirst")}
            </button>
            <button type="button" className="dw-chip" onClick={() => void send(t("ai.universal.quickCity"), "create")} disabled={busy !== ""}>
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
            placeholder={zh ? "例如：一座会下雨、可以自由走动的城市。" : "For example: a city where it rains and I can walk freely."}
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
        <p className="dw-fineprint dw-ai-limits">
          <Icon name="layers" size={13} />
          {t("ai.universal.limits")}
        </p>
      </div>
    </Modal>
  );

  function renderApproval(item: Confirmation) {
    if (item.status === "denied") {
      return null;
    }
    const failure = failures[item.id];
    const working = busy === `approve:${item.id}`;
    let phase: ApprovalPhase;
    if (item.status === "approved") {
      // A created world is an entry point, never another approval request.
      if (!item.result_object_id) {
        return null;
      }
      phase = "created";
    } else if (failure) {
      phase = isExpired(item.expires_at, now) ? "expired" : "failed";
    } else if (working) {
      phase = "working";
    } else if (item.status === "expired" || isExpired(item.expires_at, now)) {
      if (item.status === "expired" && !recentlyCreated(item)) {
        return null;
      }
      phase = "expired";
    } else {
      phase = "pending";
    }
    const title =
      item.effect?.title?.trim() ||
      item.result_object_title?.trim() ||
      item.target_object_title?.trim() ||
      (zh ? "世界方案" : "World plan");
    return (
      <InlineApproval
        key={item.id}
        id={item.id}
        kind="world"
        phase={phase}
        title={title}
        summary={item.effect?.summary?.trim()}
        surfaces={item.effect?.surfaces}
        kindNames={item.effect?.kind_names}
        thingNames={item.effect?.thing_names}
        expiresAt={item.expires_at}
        errorText={failure?.errorText}
        createdHref={resultObjectHref(item)}
        createdTitle={item.result_object_title || item.effect?.title}
        onConfirm={() => void createProposed(item)}
        onDismiss={() => void dismissProposal(item)}
        onRetry={() => void createProposed(item)}
        onPrepareAgain={() => void prepareAgain(item)}
      />
    );
  }
}

// An approved result is worth showing across a refresh, but an expired plan
// from months ago is history, not a decision. Keep the card for a short window.
function recentlyCreated(item: Confirmation): boolean {
  const created = Date.parse(item.created_at);
  return Number.isFinite(created) && Date.now() - created < 30 * 60 * 1000;
}
