// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  applyProposal,
  approveConfirmation,
  createConversation,
  denyConfirmation,
  denyProposal,
  getConfirmation,
  getConversation,
  getObject,
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
import { proposalPreview } from "../humanize";
import { useI18n, type MessageKey } from "../i18n";
import { useMeta } from "../meta";
import {
  approvalIntent,
  isExpired,
  isPendingConfirmation,
  promptForMessage,
  resultObjectHref,
  sanitizeAssistantText
} from "../approval";
import { looksLikeJSONContent } from "../worldLabels";
import Icon from "./v2/Icon";
import InlineApproval, { type ApprovalPhase } from "./v2/InlineApproval";

interface Props {
  object: ObjectView;
  canContribute: boolean;
  onChanged: () => void;
}

type TurnOutcome = {
  prompt: string;
  accepted: number;
  rejected: number;
  parseStatus: "structured" | "fallback";
  notPrepared: boolean;
};

type Resolution = {
  status: "applied" | "created" | "failed";
  errorText?: string;
  object?: ObjectView;
  versionConflict?: boolean;
};

// AIPanel is the single world-scoped Personal AI surface. Approvals are
// rendered directly under the assistant reply that produced them; there is no
// separate confirmation section, and there is no "switch to general AI" escape
// that would silently abandon the world context.
export default function AIPanel({ object, canContribute, onChanged }: Props) {
  const objectID = object.id;
  const objectTitle = typeof object.state.title === "string" ? object.state.title : "";
  const objectVersion = object.version;
  const canDerive = object.permissions.includes("derive");
  const { t, lang } = useI18n();
  const { mode } = useMeta();
  const zh = lang === "zh";
  const [conversationID, setConversationID] = useState<string | null>(null);
  const [messages, setMessages] = useState<AIMessage[]>([]);
  const [proposals, setProposals] = useState<Proposal[]>([]);
  const [confirmations, setConfirmations] = useState<Confirmation[]>([]);
  const [resolved, setResolved] = useState<Record<string, Resolution>>({});
  const [turnOutcomes, setTurnOutcomes] = useState<Record<string, TurnOutcome>>({});
  const [dismissed, setDismissed] = useState<Record<string, true>>({});
  const [focusRequest, setFocusRequest] = useState<{ id: string; nonce: number } | null>(null);
  const [revealID, setRevealID] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState<unknown>(null);
  const transcriptRef = useRef<HTMLDivElement | null>(null);
  const focusNonce = useRef(0);
  const previousMessages = useRef(0);

  const draftBytes = useMemo(() => new TextEncoder().encode(draft).length, [draft]);

  const refreshApprovals = useCallback(async () => {
    const [proposalPage, confirmationPage] = await Promise.all([
      canContribute ? listProposals(objectID) : Promise.resolve({ items: [] as Proposal[] }),
      listConfirmations("all")
    ]);
    setProposals(proposalPage.items);
    setConfirmations(
      confirmationPage.items.filter((item) =>
        item.target_object_id
          ? item.target_object_id === objectID
          : item.resource_type === "living_object" && item.resource_id === objectID
      )
    );
    const pending = [
      ...proposalPage.items.filter(
        (item) => item.status === "pending" && !proposalExpired(item)
      ),
      ...confirmationPage.items.filter(
        (item) =>
          isPendingConfirmation(item) &&
          (item.target_object_id === objectID ||
            (item.resource_type === "living_object" && item.resource_id === objectID))
      )
    ];
    return pending;
  }, [canContribute, objectID]);

  const load = useCallback(async () => {
    const conversationPage = await listConversations();
    const bound = conversationPage.items.find((item) => item.object_id === objectID);
    if (bound) {
      setConversationID(bound.id);
      try {
        const detail = await getConversation(bound.id);
        previousMessages.current = 0;
        setMessages(detail.messages);
      } catch {
        setMessages([]);
      }
    } else {
      setConversationID(null);
      previousMessages.current = 0;
      setMessages([]);
    }
    const pending = await refreshApprovals();
    const first =
      pending
        .slice()
        .sort((left, right) => Date.parse(left.created_at) - Date.parse(right.created_at))[0] ??
      null;
    if (first) {
      setRevealID(first.id);
    }
  }, [objectID, refreshApprovals]);

  useEffect(() => {
    let cancelled = false;
    setResolved({});
    setTurnOutcomes({});
    setDismissed({});
    setNotice("");
    setError(null);
    setBusy("");
    void load().catch((failure) => {
      if (!cancelled) {
        setError(failure);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [load]);

  // Opening the world panel must leave the composer reachable. The panel has
  // its own scroll area, so focusing or tapping the AI entry alone can leave
  // the input just below a 768px viewport.
  useEffect(() => {
    const timer = window.setTimeout(() => {
      document.getElementById("dw-ai-compose")?.scrollIntoView({ block: "nearest" });
    }, 90);
    return () => window.clearTimeout(timer);
  }, [objectID]);

  useEffect(() => {
    if (!canContribute) {
      return;
    }
    const timer = window.setInterval(() => setNow(Date.now()), 15_000);
    return () => window.clearInterval(timer);
  }, [canContribute]);

  useEffect(() => {
    const reconcile = () => {
      void refreshApprovals().catch(() => undefined);
    };
    window.addEventListener("focus", reconcile);
    document.addEventListener("visibilitychange", reconcile);
    return () => {
      window.removeEventListener("focus", reconcile);
      document.removeEventListener("visibilitychange", reconcile);
    };
  }, [refreshApprovals]);

  // A newly arrived plan moves the user to its real button.
  useEffect(() => {
    if (!focusRequest) {
      return;
    }
    const timer = window.setTimeout(() => {
      const node = document.querySelector<HTMLElement>(
        `[data-approval-primary="${focusRequest.id}"]`
      );
      if (!node) {
        return;
      }
      const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;
      node.scrollIntoView({ block: "nearest", behavior: reduced ? "auto" : "smooth" });
      node.focus({ preventScroll: true });
      setFocusRequest((current) => (current?.nonce === focusRequest.nonce ? null : current));
    }, 140);
    return () => window.clearTimeout(timer);
  }, [focusRequest, proposals, confirmations, messages]);

  // Reopening the panel reveals a waiting plan without stealing focus.
  useEffect(() => {
    if (!revealID) {
      return;
    }
    const timer = window.setTimeout(() => {
      const node =
        document.querySelector<HTMLElement>(`[data-approval-primary="${revealID}"]`) ??
        document.querySelector<HTMLElement>(`[data-approval-id="${revealID}"]`);
      if (node) {
        const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;
        node.scrollIntoView({ block: "nearest", behavior: reduced ? "auto" : "smooth" });
      }
      setRevealID(null);
    }, 160);
    return () => window.clearTimeout(timer);
  }, [revealID, proposals, confirmations]);

  useEffect(() => {
    const node = transcriptRef.current;
    const increased = messages.length > previousMessages.current;
    previousMessages.current = messages.length;
    if (!node || focusRequest || revealID) {
      return;
    }
    // A refresh or a cleared reveal must not jump away from the pending card.
    if (increased) {
      node.scrollTop = node.scrollHeight;
    }
  }, [messages.length, proposals.length, confirmations.length, busy, focusRequest, revealID]);

  const lastAssistantID = useMemo(() => {
    for (let index = messages.length - 1; index >= 0; index -= 1) {
      if (messages[index]?.role === "assistant") {
        return messages[index]?.id ?? null;
      }
    }
    return null;
  }, [messages]);

  const approvalsByMessage = useMemo(() => {
    const map = new Map<string, Array<{ kind: "proposal"; item: Proposal } | { kind: "confirmation"; item: Confirmation }>>();
    for (const item of proposals) {
      if (!item.source_message_id || dismissed[item.id]) {
        continue;
      }
      const resolution = resolved[item.id];
      if (item.status === "denied") {
        continue;
      }
      if (item.status !== "pending" && !resolution) {
        continue;
      }
      const list = map.get(item.source_message_id) ?? [];
      list.push({ kind: "proposal", item });
      map.set(item.source_message_id, list);
    }
    for (const item of confirmations) {
      if (!item.message_id || dismissed[item.id]) {
        continue;
      }
      if (item.status === "denied") {
        continue;
      }
      const resolution = resolved[item.id];
      const show =
        item.status === "pending" ||
        (item.status === "approved" && Boolean(item.result_object_id)) ||
        Boolean(resolution) ||
        (item.status === "expired" && recentlyCreated(item));
      if (!show) {
        continue;
      }
      const list = map.get(item.message_id) ?? [];
      list.push({ kind: "confirmation", item });
      map.set(item.message_id, list);
    }
    for (const list of map.values()) {
      list.sort((left, right) => Date.parse(left.item.created_at) - Date.parse(right.item.created_at));
    }
    return map;
  }, [proposals, confirmations, resolved, dismissed]);

  const unlinked = useMemo(() => {
    const items = [
      ...proposals
        .filter(
          (item) =>
            item.status === "pending" &&
            !proposalExpired(item) &&
            !item.source_message_id &&
            !dismissed[item.id]
        )
        .map((item) => ({ kind: "proposal" as const, item })),
      ...confirmations
        .filter(
          (item) =>
            !item.message_id &&
            !dismissed[item.id] &&
            (isPendingConfirmation(item) ||
              Boolean(resolved[item.id]) ||
              (item.status === "approved" && Boolean(item.result_object_id)))
        )
        .map((item) => ({ kind: "confirmation" as const, item }))
    ];
    return items.sort((left, right) => Date.parse(left.item.created_at) - Date.parse(right.item.created_at));
  }, [proposals, confirmations, resolved, dismissed, now]);

  const pendingCount =
    proposals.filter((item) => item.status === "pending" && !proposalExpired(item)).length +
    confirmations.filter((item) => isPendingConfirmation(item, now)).length;

  // While a world plan is live, a "try again" card for a world_pending
  // rejection cannot succeed and would compete with the real confirmation.
  const hasLivePlan = confirmations.some(
    (item) => item.action === "world.create" && isPendingConfirmation(item, now)
  );

  function requestFocus(itemID: string) {
    focusNonce.current += 1;
    setFocusRequest({ id: itemID, nonce: focusNonce.current });
  }

  async function send(contentOverride?: string) {
    const content = (contentOverride ?? draft).trim();
    if (!content || busy || !canContribute) {
      return;
    }

    // "批准通过" must not become a new change request while a real proposal is
    // waiting. Bring the user to the inline button instead.
    const intentKind = approvalIntent(content);
    if (intentKind) {
      const pending = await refreshApprovals().catch(() => null);
      if (pending && pending.length > 0) {
        const first = pending
          .slice()
          .sort((left, right) => Date.parse(left.created_at) - Date.parse(right.created_at))[0];
        if (first) {
          setDraft("");
          setNotice(t("ai.approval.useButton"));
          requestFocus(first.id);
          return;
        }
      }
      if (intentKind === "strong") {
        setDraft("");
        setNotice(t("ai.approval.nonePending"));
        return;
      }
    }

    setBusy("send");
    setNotice("");
    setError(null);
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
      const result = await sendAIMessage(activeConversation, content, "change");
      setMessages((previous) => [...previous, result.user_message, result.assistant_message]);
      setDraft("");
      const trace = { mode: result.model_mode, at: new Date().toISOString() };
      try {
        window.sessionStorage.setItem("dw:last-ai-inference", JSON.stringify(trace));
      } catch {
        // Session storage is optional; the event still updates an open dialog.
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
      if (result.context_stale) {
        setNotice(t("workspace.aiStale"));
      } else if (result.parse_status === "structured" && result.proposed_actions.length === 0) {
        setNotice(t("workspace.aiNoAction"));
      }
      await refreshApprovals();
      const firstAction = result.proposed_actions[0];
      const firstID = firstAction?.proposal_id ?? firstAction?.confirmation_id;
      if (firstID) {
        setRevealID(firstID);
      } else {
        document.getElementById("dw-ai-compose")?.scrollIntoView({ block: "nearest" });
      }
    } catch (failure) {
      setError(failure);
      if (contentOverride) {
        setDraft(content);
      }
    } finally {
      setBusy("");
    }
  }

  async function apply(item: Proposal) {
    setBusy(`apply:${item.id}`);
    setNotice("");
    setError(null);
    try {
      await applyProposal(objectID, item.id, objectVersion);
      setResolved((previous) => ({
        ...previous,
        [item.id]: { status: "applied" }
      }));
      setNotice(t("workspace.applied"));
      onChanged();
      await refreshApprovals();
    } catch (failure) {
      const described = describeError(failure);
      setError(failure);
      setResolved((previous) => ({
        ...previous,
        [item.id]: {
          status: "failed",
          errorText: t(described.messageKey),
          versionConflict: described.code === "version_conflict"
        }
      }));
    } finally {
      setBusy("");
    }
  }

  async function deny(item: Proposal) {
    setBusy(`deny:${item.id}`);
    setError(null);
    try {
      await denyProposal(objectID, item.id);
      setDismissed((previous) => ({ ...previous, [item.id]: true }));
      setNotice(t("workspace.denied"));
      await refreshApprovals();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function approve(item: Confirmation) {
    setBusy(`approve:${item.id}`);
    setNotice("");
    setError(null);
    try {
      const result = await approveConfirmation(item.id);
      const created = asObjectView(result);
      if (item.action === "derive.create" && created) {
        setResolved((previous) => ({
          ...previous,
          [item.id]: { status: "created", object: created }
        }));
        setNotice(t("workspace.approved"));
      } else {
        setResolved((previous) => ({
          ...previous,
          [item.id]: { status: "applied" }
        }));
        setNotice(t("workspace.approved"));
      }
      onChanged();
      await refreshApprovals();
    } catch (failure) {
      const described = describeError(failure);
      if (described.code === "conflict") {
        const reconciled = await reconcileConfirmation(item);
        if (reconciled) {
          return;
        }
      }
      setError(failure);
      setResolved((previous) => ({
        ...previous,
        [item.id]: {
          status: "failed",
          errorText: t(described.messageKey),
          versionConflict: described.code === "version_conflict"
        }
      }));
    } finally {
      setBusy("");
    }
  }

  // Another tab may have approved this confirmation first. Reconcile to the
  // server's decision instead of leaving a live approve button on a conflict.
  async function reconcileConfirmation(item: Confirmation): Promise<boolean> {
    try {
      const current = await getConfirmation(item.id);
      if (current.status === "approved") {
        const created = current.result_object_id
          ? await getObject(current.result_object_id).catch(() => null)
          : null;
        setConfirmations((entries) =>
          entries.map((entry) => (entry.id === item.id ? { ...entry, ...current } : entry))
        );
        setResolved((previous) => ({
          ...previous,
          [item.id]: created
            ? { status: "created", object: created }
            : { status: "applied" }
        }));
        onChanged();
        await refreshApprovals();
        return true;
      }
      if (current.status === "denied" || current.status === "expired") {
        setDismissed((previous) => ({ ...previous, [item.id]: true }));
        await refreshApprovals();
        return true;
      }
    } catch (reconcileError) {
      if (describeError(reconcileError).code === "confirmation_expired") {
        setDismissed((previous) => ({ ...previous, [item.id]: true }));
        await refreshApprovals();
        return true;
      }
    }
    return false;
  }

  async function denyConfirmationItem(item: Confirmation) {
    setBusy(`deny-confirmation:${item.id}`);
    setError(null);
    try {
      await denyConfirmation(item.id);
      setDismissed((previous) => ({ ...previous, [item.id]: true }));
      setNotice(t("workspace.denied"));
      await refreshApprovals();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  function dismiss(itemID: string) {
    setDismissed((previous) => ({ ...previous, [itemID]: true }));
  }

  function prepareAgain(itemID: string, messageID: string | undefined) {
    const prompt = messageID ? promptForMessage(messages, messageID) : "";
    if (!prompt) {
      setNotice(t("ai.approval.nonePending"));
      return;
    }
    setDismissed((previous) => ({ ...previous, [itemID]: true }));
    void send(prompt);
  }

  function renderApproval(
    entry: { kind: "proposal"; item: Proposal } | { kind: "confirmation"; item: Confirmation }
  ) {
    return entry.kind === "proposal"
      ? renderProposal(entry.item)
      : renderConfirmation(entry.item);
  }

  function renderProposal(item: Proposal) {
    const resolution = resolved[item.id];
    const expired = proposalExpired(item, now);
    const working = busy === `apply:${item.id}`;
    let phase: ApprovalPhase;
    if (resolution?.status === "applied") {
      phase = "applied";
    } else if (resolution?.status === "failed") {
      phase = expired ? "expired" : "failed";
    } else if (working) {
      phase = "working";
    } else if (item.status === "expired" || expired) {
      if (item.status === "expired" && !recentlyCreated(item)) {
        return null;
      }
      phase = "expired";
    } else if (item.status === "applied") {
      phase = "applied";
    } else if (item.status === "pending") {
      phase = "pending";
    } else {
      return null;
    }

    const preview = humanProposalPreview(item, object, zh, t);
    const versionConflict = Boolean(resolution?.versionConflict);
    return (
      <InlineApproval
        key={item.id}
        id={item.id}
        kind="proposal"
        phase={phase}
        title={preview.title}
        summary={preview.summary}
        fields={preview.fields}
        errorText={resolution?.errorText}
        onConfirm={() => void apply(item)}
        onDismiss={phase === "expired" ? () => dismiss(item.id) : () => void deny(item)}
        onRetry={versionConflict ? undefined : () => void apply(item)}
        onPrepareAgain={
          versionConflict ? () => prepareAgain(item.id, item.source_message_id) : undefined
        }
      />
    );
  }

  function renderConfirmation(item: Confirmation) {
    const resolution = resolved[item.id];
    const expired = isExpired(item.expires_at, now);
    const working = busy === `approve:${item.id}`;
    const createdObject = resolution?.object ?? null;
    const href =
      (createdObject ? objectHref(createdObject) : null) ?? resultObjectHref(item);
    let phase: ApprovalPhase;
    if (resolution?.status === "created") {
      phase = "created";
    } else if (resolution?.status === "applied") {
      phase = "applied";
    } else if (resolution?.status === "failed") {
      phase = expired ? "expired" : "failed";
    } else if (working) {
      phase = "working";
    } else if (item.status === "approved") {
      if (!item.result_object_id && item.action !== "capability.invoke") {
        return null;
      }
      phase = item.result_object_id ? "created" : "applied";
    } else if (item.status === "expired" || expired) {
      if (item.status === "expired" && !recentlyCreated(item)) {
        return null;
      }
      phase = "expired";
    } else if (item.status === "pending") {
      phase = "pending";
    } else {
      return null;
    }

    const title =
      item.effect?.title?.trim() ||
      (item.action === "derive.create"
        ? zh
          ? "创建一个独立分支"
          : "Create an independent branch"
        : item.action === "capability.invoke"
          ? zh
            ? "执行一个能力"
            : "Run a capability"
          : t(`confirmation.action.${item.action}`));
    const summary =
      item.action === "derive.create"
        ? t("confirmation.deriveSummary")
        : item.action === "capability.invoke"
          ? t("confirmation.capabilitySummary")
          : t("confirmation.worldSummary");
    const versionConflict = Boolean(resolution?.versionConflict);
    const permissionBlocked = item.action === "derive.create" && !canDerive;
    const boundVersion = item.bound_version ?? item.effect?.source_version;
    const versionNote =
      boundVersion && boundVersion > 0
        ? t("confirmation.boundVersion", { version: boundVersion })
        : undefined;
    return (
      <InlineApproval
        key={item.id}
        id={item.id}
        kind={item.action === "derive.create" ? "derive" : "capability"}
        phase={permissionBlocked && (phase === "pending" || phase === "working") ? "failed" : phase}
        title={title}
        summary={summary}
        versionNote={versionNote}
        expiresAt={item.expires_at}
        errorText={
          permissionBlocked && (phase === "pending" || phase === "working")
            ? zh
              ? "你当前没有衍生权限，所以不能创建这个分支。"
              : "You do not have derive permission, so this branch cannot be created."
            : resolution?.errorText
        }
        createdHref={href}
        createdTitle={
          createdObject && typeof createdObject.state.title === "string"
            ? createdObject.state.title
            : item.result_object_title
        }
        onConfirm={() => void approve(item)}
        onDismiss={
          phase === "expired" ? () => dismiss(item.id) : () => void denyConfirmationItem(item)
        }
        onRetry={
          versionConflict || permissionBlocked
            ? undefined
            : () => void approve(item)
        }
        onPrepareAgain={
          versionConflict ? () => prepareAgain(item.id, item.message_id) : undefined
        }
      />
    );
  }

  return (
    <section className="card ai-panel" data-testid="ai-panel" aria-busy={busy !== ""}>
      <div className="section-heading">
        <h2>{zh ? "和我的 AI 一起创造" : "Create with my AI"}</h2>
        <small>{canContribute ? t("workspace.permission.contribute") : t("workspace.permission.read")}</small>
      </div>
      <p className="muted">{t("workspace.aiIntro")}</p>
      <div className="dw-ai-context-badge" data-testid="ai-world-context">
        <span>
          <strong>{t("ai.universal.context", { title: objectTitle || t("common.untitled") })}</strong>
          <small>{zh ? "只作用于当前世界；每次修改都需要你确认。" : "Bound to this world only; every change needs your approval."}</small>
        </span>
      </div>
      {mode === "fixture" ? <p className="fixture-inline">{t("workspace.aiFixture")}</p> : null}
      {notice ? <StatusNotice>{notice}</StatusNotice> : null}
      {error ? <ErrorNotice error={error} compact /> : null}
      {describeError(error).code === "payload_too_large" ? (
        <p className="form-error" role="alert">
          {t("ai.handoffTooLarge")}
        </p>
      ) : null}

      <div className="ai-messages" aria-live="polite" ref={transcriptRef}>
        {messages.length === 0 && busy === "" ? <p className="muted">{t("workspace.aiEmpty")}</p> : null}
        {messages.map((message) => {
          const entries = message.role === "assistant" ? approvalsByMessage.get(message.id) ?? [] : [];
          const hasResult = entries.some((entry) => {
            if (entry.kind === "confirmation") {
              const resolution = resolved[entry.item.id];
              return (
                entry.item.status === "approved" ||
                resolution?.status === "created" ||
                resolution?.status === "applied"
              );
            }
            return resolved[entry.item.id]?.status === "applied";
          });
          const hasPending = entries.some((entry) =>
            entry.kind === "confirmation"
              ? isPendingConfirmation(entry.item, now)
              : entry.item.status === "pending"
          );
          const sanitized =
            message.role === "assistant"
              ? sanitizeAssistantText(message.content, zh, { hasResult, hasPending })
              : null;
          const isLastAssistant = message.role === "assistant" && message.id === lastAssistantID;
          const outcome = turnOutcomes[message.id];
          const showUnprepared =
            isLastAssistant &&
            entries.length === 0 &&
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
              <div className={`ai-message ai-${message.role}`}>
                <small>{message.role === "user" ? t("workspace.you") : t("workspace.ai")}</small>
                {looksLikeJSONContent(message.content) ? (
                  <p className="dw-ai-unsafe">
                    {zh
                      ? "这条旧回复不是可读的对话内容，已隐藏。请重新提出你的要求。"
                      : "This old reply is not readable conversation; it has been hidden. Please ask again."}
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
              {entries.map((entry) => renderApproval(entry))}
              {showUnprepared ? (
                <div className="dw-ai-unprepared" role="status">
                  <strong>{t("ai.approval.notPrepared")}</strong>
                  <p>{t("ai.approval.notPreparedBody")}</p>
                  <button
                    className="dw-button dw-button-ghost dw-button-small"
                    type="button"
                    disabled={busy !== "" || !canContribute}
                    onClick={() => void send(promptForMessage(messages, message.id))}
                  >
                    <Icon name="reset" size={14} />
                    {t("ai.approval.retry")}
                  </button>
                </div>
              ) : null}
            </div>
          );
        })}
        {unlinked.length > 0 ? (
          <div className="dw-ai-turn">
            <p className="dw-approval-fallback-label">{t("ai.approval.pendingShort")}</p>
            {unlinked.map((entry) => renderApproval(entry))}
          </div>
        ) : null}
        {busy === "send" ? (
          <div className="ai-message ai-assistant is-pending" role="status">
            <small>{t("workspace.ai")}</small>
            <p>
              <span className="dw-thinking-dots" aria-hidden="true">
                <i />
                <i />
                <i />
              </span>
              {zh ? "正在准备修改方案…" : "Preparing a change…"}
            </p>
          </div>
        ) : null}
      </div>

      {!canContribute ? (
        <p className="form-hint">
          {zh
            ? "你是这个世界的读者，可以体验内容，但不能让 AI 修改它。"
            : "You can experience this world, but only members with contribute permission can ask the AI to change it."}
        </p>
      ) : null}
      <label className="ai-compose">
        <span className="visually-hidden">{t("workspace.aiPlaceholder")}</span>
        <textarea
          id="dw-ai-compose"
          rows={3}
          maxLength={16384}
          value={draft}
          placeholder={zh ? "例如：把这里变成下雨的夜晚；再加两栋建筑；给这个世界加上声音。" : "For example: make this a rainy night; add two more buildings; give this world sound."}
          onChange={(event) => setDraft(event.target.value)}
          disabled={!canContribute}
        />
      </label>
      <div className="form-actions">
        <button
          className="button button-small"
          type="button"
          disabled={!canContribute || !draft.trim() || busy !== "" || draftBytes > 16384}
          onClick={() => void send()}
        >
          {busy === "send" ? t("workspace.aiSending") : t("workspace.aiSend")}
        </button>
        <small className={draftBytes > 16384 ? "form-error" : "muted"}>
          {t("ai.messageBytes", { bytes: draftBytes })}
        </small>
        {pendingCount > 0 ? (
          <small className="dw-approval-count" role="status">
            {pendingCount} · {t("ai.approval.pendingShort")}
          </small>
        ) : null}
        {mode === "fixture" && canContribute ? (
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
    </section>
  );
}

function proposalExpired(item: Proposal, now = Date.now()): boolean {
  const expires = Date.parse(item.expires_at);
  return Number.isFinite(expires) && expires <= now;
}

function recentlyCreated(item: { created_at: string }): boolean {
  const created = Date.parse(item.created_at);
  return Number.isFinite(created) && Date.now() - created < 30 * 60 * 1000;
}

function objectHref(object: ObjectView): string {
  return object.object_type === "open_world"
    ? `/worlds/${object.id}`
    : `/objects/${object.id}?tab=work`;
}

function asObjectView(value: unknown): ObjectView | null {
  if (
    value &&
    typeof value === "object" &&
    typeof (value as { id?: unknown }).id === "string" &&
    typeof (value as { object_type?: unknown }).object_type === "string" &&
    Boolean((value as { state?: unknown }).state)
  ) {
    return value as ObjectView;
  }
  return null;
}

function humanProposalPreview(
  item: Proposal,
  object: ObjectView,
  zh: boolean,
  t: (key: MessageKey) => string
): { title: string; summary?: string; fields: Array<{ label: string; value: string }> } {
  const input = item.input ?? {};
  if (item.action === "reshape_world") {
    const note =
      typeof input.note === "string" && input.note.trim()
        ? input.note.trim()
        : zh
          ? "AI 准备了一次世界重做。"
          : "The AI prepared a world reshape.";
    return {
      title: zh ? "重做这个世界" : "Reshape this world",
      summary: note,
      fields: []
    };
  }
  const preview = proposalPreview(item, object);
  const known = preview.action === item.action;
  const fields = known
    ? preview.fields
        .slice(0, 3)
        .filter((field) => field.value.trim().length > 0)
        .map((field) => ({ label: t(field.labelKey), value: field.value }))
    : [];
  return {
    title:
      ACTION_LABELS[item.action]?.[zh ? "zh" : "en"] ??
      (zh ? "对世界的修改" : "A change to this world"),
    fields
  };
}

const ACTION_LABELS: Record<string, { en: string; zh: string }> = {
  set_title: { en: "Rename this world", zh: "重命名这个世界" },
  set_premise: { en: "Change the premise", zh: "改变世界的前提" },
  set_summary: { en: "Change the summary", zh: "修改摘要" },
  set_body: { en: "Change the content", zh: "修改内容" },
  add_element: { en: "Add something", zh: "添加一个存在" },
  update_element: { en: "Update something", zh: "更新一个存在" },
  remove_element: { en: "Remove something", zh: "移除一个存在" },
  move_element: { en: "Move something", zh: "移动一个存在" },
  add_kind: { en: "Add a kind", zh: "添加一种存在" },
  update_kind: { en: "Update a kind", zh: "更新一种存在" },
  remove_kind: { en: "Remove a kind", zh: "移除一种存在" },
  add_node: { en: "Add a thing", zh: "添加一个存在" },
  update_node: { en: "Update a thing", zh: "更新一个存在" },
  remove_node: { en: "Remove a thing", zh: "移除一个存在" },
  move_node: { en: "Move a thing", zh: "移动一个存在" },
  set_node_field: { en: "Change a value", zh: "修改一个值" },
  set_node_status: { en: "Change a status", zh: "修改状态" },
  touch_node: { en: "Act on a thing", zh: "操作一个存在" },
  add_link: { en: "Add a relation", zh: "添加关系" },
  remove_link: { en: "Remove a relation", zh: "移除关系" },
  add_rule: { en: "Add a rule", zh: "添加规则" },
  remove_rule: { en: "Remove a rule", zh: "移除规则" },
  set_views: { en: "Change the visible views", zh: "修改可见视图" },
  add_checklist_item: { en: "Add a step", zh: "添加一个步骤" },
  toggle_checklist_item: { en: "Tick a step", zh: "勾选一个步骤" },
  remove_checklist_item: { en: "Remove a step", zh: "移除一个步骤" },
  add_tag: { en: "Add a tag", zh: "添加标签" },
  remove_tag: { en: "Remove a tag", zh: "移除标签" },
  cast_vote: { en: "Cast a vote", zh: "投票" }
};
