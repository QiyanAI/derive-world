// SPDX-License-Identifier: Apache-2.0
// Shared approval helpers. One vocabulary for the general Personal AI and the
// in-world Personal AI, so both surfaces place, expire and explain approvals
// in exactly the same way.

import type { AIMessage, Confirmation } from "./api";

export type ApprovalIntent = "strong" | "weak" | null;

// A short approval phrase is intercepted before it reaches the model. The
// model can only propose; treating "批准通过" as a fresh request would create a
// second plan and leave the real confirmation ignored.
const STRONG_APPROVALS = new Set([
  "批准",
  "批准通过",
  "批准这个",
  "批准它",
  "确认",
  "确认创建",
  "确认通过",
  "确认并创建",
  "同意",
  "同意创建",
  "approve",
  "approved",
  "approval",
  "confirm",
  "confirmed",
  "confirmcreation",
  "confirmthecreation",
  "confirmtheplan",
  "approvetheplan",
  "createtheworld",
  "createitnow"
]);

const WEAK_APPROVALS = new Set([
  "通过",
  "可以",
  "好的",
  "好",
  "行",
  "就这样",
  "就这样吧",
  "就这么办",
  "创建",
  "创建吧",
  "创建它",
  "创建这个世界",
  "yes",
  "yep",
  "ok",
  "okay",
  "sure",
  "proceed",
  "goahead",
  "doit",
  "apply",
  "applyit",
  "create",
  "createit",
  "confirmit"
]);

function normalizeApproval(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[\s。.!！?？,，、;；:：'"“”‘’()（）[\]【】-]/gu, "");
}

export function approvalIntent(content: string): ApprovalIntent {
  const normalized = normalizeApproval(content);
  if (!normalized || normalized.length > 24) {
    return null;
  }
  if (STRONG_APPROVALS.has(normalized)) {
    return "strong";
  }
  if (WEAK_APPROVALS.has(normalized)) {
    return "weak";
  }
  return null;
}

export function isExpired(expiresAt: string, now = Date.now()): boolean {
  const parsed = Date.parse(expiresAt);
  return Number.isFinite(parsed) && parsed <= now;
}

export function isPendingConfirmation(item: Confirmation, now = Date.now()): boolean {
  return item.status === "pending" && !isExpired(item.expires_at, now);
}

// Older replies sometimes pointed at a card that no longer exists. The
// server guards new turns; this helper keeps saved history honest without
// hiding the rest of what the AI said.
const CARD_GUIDANCE =
  /(提案卡片|提案卡|批准卡片|确认卡片|审批卡片|卡片上的(?:批准|确认)[^。.!?\n]{0,8}按钮|proposal\s+card|approval\s+card|confirmation\s+card|click[^.\n]{0,32}card)/i;
const SENTENCE = /[^。！？.!?\n]+[。！？.!?]?/g;

export interface SanitizedAssistantText {
  text: string;
  legacyGuidance: boolean;
}

// Legacy assistant rows can claim that something was already created or
// approved. When no real result is linked to that turn, the claim is false and
// must not be shown as fact. New turns are guarded server-side; this keeps the
// saved history honest without rewriting the database.
const LEGACY_CLAIMS: RegExp[] = [
  /(?:我|我们)(?:已经|已)(?:成功)?(?:创建|批准|应用|执行)(?:了)?(?:这个|该)?(?:世界|方案|改动|提案|分支)/u,
  /(?:世界|方案|改动|提案|分支)(?:已经|已)(?:被)?(?:创建|生效|上线|批准|应用|生成)/u,
  /(?:你|您)[^。！？\n]{0,8}(?:已经|已)(?:成功)?(?:批准|创建|应用)(?:了|过)?/u,
  /(?:创建|批准|应用|执行)成功了/u,
  /\bI(?:'ve| have| had)?\s+(?:now\s+)?(?:already\s+)?(?:successfully\s+)?(?:created|approved|applied|executed)\s+(?:the|your|this|a)\s+(?:world|plan|change|proposal|branch)\b/iu,
  /\b(?:the|your|this)\s+(?:world|plan|change|proposal|branch)\s+(?:is|was|has|have|had)\s+(?:(?:now|already|successfully)\s+)*(?:been\s+)?(?:(?:now|already|successfully)\s+)*(?:created|approved|applied|executed|live)\b(?!\s+(?:only|after|when|once|until|if)\b)/iu,
  /\b(?:world|plan|change|proposal|branch)\s+(?:has|have)\s+(?:now\s+|already\s+)?been\s+(?:successfully\s+)?(?:created|approved|applied|executed)\b/iu,
  /(?:已经|已)(?:成功)?(?:为|帮|替)(?:你|您|我)?[^。！？\n]{0,10}(?:创建|建|生成|批准|应用|执行)(?:好|成|了)/u,
  /(?:已经|已)(?:成功)?把[^。！？\n]{0,10}(?:创建|建|生成|批准|应用|执行)(?:好|成|了)/u,
  /(?:已经|已)(?:成功)?(?:创建|建)(?:好|成)了?(?:这个|该|它|这)?(?:世界|方案|改动|提案|分支)/u,
  /(?:已经|已)(?:为你|帮你|替你)(?:成功)?(?:创建|生成|批准|应用)(?:了)?/u
];

function legacyClaimReply(zh: boolean): string {
  return zh
    ? "这条旧回复声称某个结果已经完成，但服务器没有对应的批准记录。没有内容因为这句话而被创建或改变。"
    : "This older reply claimed a result was complete, but the server has no matching approval. Nothing was created or changed by that message.";
}

// A sentence can carry useful context before the obsolete instruction, as in
// "You already approved this plan — click the proposal card." Keep the useful
// clause and drop only the clause that points at a card.
function sanitizeSentence(sentence: string, zh: boolean): string {
  if (!CARD_GUIDANCE.test(sentence)) {
    return sentence;
  }
  const parts = sentence.split(/(?:——|—|；|;|：|:)/u);
  const kept = parts
    .map((part) => part.trim())
    .filter((part) => part.length > 0 && !CARD_GUIDANCE.test(part));
  if (kept.length === 0) {
    return "";
  }
  let result = kept.join(zh ? "；" : "; ");
  if (!/[。.!！?？]$/u.test(result)) {
    result += zh ? "。" : ".";
  }
  return result;
}

export function sanitizeAssistantText(
  value: string,
  zh: boolean,
  options: { hasResult?: boolean; hasPending?: boolean } = {}
): SanitizedAssistantText {
  if (
    !options.hasResult &&
    !options.hasPending &&
    LEGACY_CLAIMS.some((pattern) => pattern.test(value))
  ) {
    return { text: legacyClaimReply(zh), legacyGuidance: true };
  }
  if (!CARD_GUIDANCE.test(value)) {
    return { text: value, legacyGuidance: false };
  }
  const lines = value
    .split("\n")
    .map((line) => {
      const kept = line.replace(SENTENCE, (sentence) => sanitizeSentence(sentence, zh));
      return kept.replace(/[ \t]{2,}/g, " ").trim();
    })
    .filter((line) => line.length > 0);
  const text = lines.join("\n").trim();
  return {
    text:
      text ||
      (zh
        ? "这条旧回复提到了一种旧的确认方式。现在确认按钮会直接出现在对应回复的下方。"
        : "This older reply referred to an old confirmation method. Confirm buttons now appear directly under the reply."),
    legacyGuidance: true
  };
}

// The nearest user message before an assistant turn is the idea the user was
// asking about. It is used by "prepare this again" without asking the model to
// guess.
export function promptForMessage(messages: AIMessage[], assistantMessageID: string): string {
  const index = messages.findIndex((message) => message.id === assistantMessageID);
  for (let cursor = index - 1; cursor >= 0; cursor -= 1) {
    const message = messages[cursor];
    if (message?.role === "user") {
      return message.content;
    }
  }
  return "";
}

export function resultObjectHref(item: Confirmation): string | null {
  if (!item.result_object_id) {
    return null;
  }
  if (item.result_object_type === "open_world") {
    return `/worlds/${item.result_object_id}`;
  }
  return `/objects/${item.result_object_id}?tab=work`;
}
