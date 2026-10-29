// SPDX-License-Identifier: Apache-2.0
import type { ReactNode } from "react";
import { useI18n } from "../../i18n";
import { surfaceLabel } from "../../worldLabels";
import Icon from "./Icon";

export type ApprovalPhase = "pending" | "working" | "created" | "applied" | "failed" | "expired";
export type ApprovalKind = "world" | "derive" | "capability" | "proposal";

export interface InlineApprovalProps {
  id: string;
  kind: ApprovalKind;
  phase: ApprovalPhase;
  title: string;
  summary?: string;
  versionNote?: string;
  surfaces?: string[];
  kindNames?: string[];
  thingNames?: string[];
  fields?: Array<{ label: string; value: string }>;
  expiresAt?: string;
  errorText?: string;
  createdHref?: string | null;
  createdTitle?: string;
  onConfirm: () => void;
  onDismiss?: () => void;
  onRetry?: () => void;
  onPrepareAgain?: () => void;
}

const testIds: Record<ApprovalKind, string> = {
  world: "world-proposal",
  derive: "confirmation-card",
  capability: "confirmation-card",
  proposal: "proposal-card"
};

// InlineApproval is the single approval surface. It is rendered immediately
// after the assistant message that produced it, never in a separate panel.
// It only exists because the server returned a real confirmation or proposal;
// there is no client-side path that invents an approve button.
export default function InlineApproval(props: InlineApprovalProps) {
  const { t, lang } = useI18n();
  const zh = lang === "zh";
  const {
    id,
    kind,
    phase,
    title,
    summary,
    versionNote,
    surfaces = [],
    kindNames = [],
    thingNames = [],
    fields = [],
    expiresAt,
    errorText,
    createdHref,
    createdTitle,
    onConfirm,
    onDismiss,
    onRetry,
    onPrepareAgain
  } = props;

  const status = approvalStatus(kind, phase, zh);
  const primaryLabel = approvalPrimaryLabel(kind, zh);
  const workingLabel = approvalWorkingLabel(kind, zh);
  const dismissable = phase === "pending" || phase === "failed";
  const permission = permissionNote(kind, zh);

  return (
    <article
      className={`dw-approval is-${phase} is-${kind}`}
      data-testid={testIds[kind]}
      data-approval-id={id}
      data-state={phase}
      data-inline="true"
      aria-label={title}
    >
      <header className="dw-approval-head">
        <span className="dw-approval-status" role="status">
          <Icon
            name={
              phase === "created" || phase === "applied"
                ? "check"
                : phase === "failed" || phase === "expired"
                  ? "shield"
                  : "spark"
            }
            size={13}
          />
          {status}
        </span>
        <h4>{title}</h4>
        {summary ? <p className="dw-approval-summary">{summary}</p> : null}
        {versionNote ? <p className="dw-approval-version">{versionNote}</p> : null}
      </header>

      {surfaces.length > 0 ? (
        <div className="dw-approval-section">
          <small>{zh ? "它将真正以这些方式运行" : "It will really run as"}</small>
          <div className="dw-approval-chips">
            {surfaces.map((surface) => (
              <span key={surface} className="dw-approval-chip">
                {surfaceLabel(surface, zh)}
              </span>
            ))}
          </div>
        </div>
      ) : null}

      {kindNames.length > 0 ? (
        <div className="dw-approval-section">
          <small>{zh ? "世界中会有" : "The world will contain"}</small>
          <div className="dw-approval-chips">
            {kindNames.map((name) => (
              <span key={`kind-${name}`} className="dw-approval-chip is-kind">
                {name}
              </span>
            ))}
          </div>
        </div>
      ) : null}

      {thingNames.length > 0 ? (
        <div className="dw-approval-section is-things">
          <small>{zh ? "具体的存在" : "Concrete things"}</small>
          <p className="dw-approval-things">{thingNames.join(zh ? "、" : ", ")}</p>
        </div>
      ) : null}

      {fields.length > 0 ? (
        <dl className="dw-approval-fields">
          {fields.map((field) => (
            <div key={`${field.label}-${field.value.slice(0, 24)}`}>
              <dt>{field.label}</dt>
              <dd>{field.value}</dd>
            </div>
          ))}
        </dl>
      ) : null}

      {phase === "failed" ? (
        <p className="dw-approval-error" role="alert">
          {errorText || (zh ? "这次操作没有完成。没有内容被改变。" : "This action did not complete. Nothing was changed.")}
        </p>
      ) : null}

      {phase === "created" || phase === "applied" ? (
        <p className="dw-approval-result" role="status">
          <Icon name="check" size={13} />
          {createdTitle
            ? zh
              ? `《${createdTitle}》已经存在。`
              : `“${createdTitle}” exists now.`
            : phase === "applied"
              ? zh
                ? "改动已经写入这个世界。"
                : "The change is now part of this world."
              : zh
                ? "它已经存在。"
                : "It now exists."}
        </p>
      ) : null}

      {phase === "pending" && expiresAt ? (
        <p className="dw-approval-meta">
          {zh
            ? `请在 ${new Date(expiresAt).toLocaleTimeString()} 前确认`
            : `Confirm before ${new Date(expiresAt).toLocaleTimeString()}`}
        </p>
      ) : null}

      {phase === "expired" ? (
        <p className="dw-approval-meta">
          {zh ? "这份方案已过期，没有创建或改变任何内容。" : "This plan expired; nothing was created or changed."}
        </p>
      ) : null}

      {phase === "pending" ? (
        <p className="dw-approval-permission">
          <Icon name="shield" size={13} />
          {permission}
        </p>
      ) : null}

      <div className="dw-approval-actions">
        {phase === "pending" ? (
          <>
            <button
              className="dw-button dw-button-primary dw-button-small dw-approval-primary"
              type="button"
              onClick={onConfirm}
              data-approval-primary={id}
            >
              {primaryLabel}
              <Icon name="spark" size={14} />
            </button>
            {onDismiss ? (
              <button className="dw-button dw-button-ghost dw-button-small" type="button" onClick={onDismiss}>
                {t("ai.approval.dismiss")}
              </button>
            ) : null}
          </>
        ) : null}

        {phase === "working" ? (
          <button className="dw-button dw-button-primary dw-button-small" type="button" disabled aria-busy="true">
            <span className="dw-thinking-dots" aria-hidden="true">
              <i />
              <i />
              <i />
            </span>
            {workingLabel}
          </button>
        ) : null}

        {phase === "failed" ? (
          <>
            {onRetry ? (
              <button
                className="dw-button dw-button-primary dw-button-small"
                type="button"
                onClick={onRetry}
                data-approval-primary={id}
              >
                {t("ai.approval.retry")}
              </button>
            ) : onPrepareAgain ? (
              <button
                className="dw-button dw-button-primary dw-button-small"
                type="button"
                onClick={onPrepareAgain}
                data-approval-primary={id}
              >
                {t("ai.approval.prepareAgain")}
              </button>
            ) : null}
            {dismissable && onDismiss ? (
              <button className="dw-button dw-button-ghost dw-button-small" type="button" onClick={onDismiss}>
                {t("ai.approval.dismiss")}
              </button>
            ) : null}
          </>
        ) : null}

        {phase === "expired" ? (
          <>
            {onPrepareAgain ? (
              <button
                className="dw-button dw-button-primary dw-button-small"
                type="button"
                onClick={onPrepareAgain}
                data-approval-primary={id}
              >
                {t("ai.approval.prepareAgain")}
              </button>
            ) : null}
            {onDismiss ? (
              <button className="dw-button dw-button-ghost dw-button-small" type="button" onClick={onDismiss}>
                {t("ai.approval.dismiss")}
              </button>
            ) : null}
          </>
        ) : null}

        {phase === "created" && createdHref ? (
          <a
            className="dw-button dw-button-primary dw-button-small"
            href={createdHref}
            data-approval-primary={id}
          >
            {kind === "world" ? t("ai.approval.enter") : t("ai.approval.enterBranch")}
            <Icon name="arrow" size={14} />
          </a>
        ) : null}
      </div>
    </article>
  );
}

function approvalStatus(kind: ApprovalKind, phase: ApprovalPhase, zh: boolean): string {
  switch (phase) {
    case "pending":
      return zh ? "待确认" : "Waiting for you";
    case "working":
      return kind === "proposal"
        ? zh ? "正在应用" : "Applying"
        : zh ? "正在创建" : "Creating";
    case "created":
      return zh ? "已创建" : "Created";
    case "applied":
      return zh ? "已应用" : "Applied";
    case "failed":
      return zh ? "失败" : "Failed";
    case "expired":
      return zh ? "已过期" : "Expired";
  }
}

function approvalPrimaryLabel(kind: ApprovalKind, zh: boolean): string {
  switch (kind) {
    case "world":
      return zh ? "确认创建世界" : "Confirm and create world";
    case "derive":
      return zh ? "确认并创建分支" : "Confirm and create branch";
    case "capability":
      return zh ? "确认并执行" : "Confirm and run";
    case "proposal":
      return zh ? "确认并应用" : "Confirm and apply";
  }
}

function approvalWorkingLabel(kind: ApprovalKind, zh: boolean): string {
  switch (kind) {
    case "world":
      return zh ? "正在创建世界…" : "Creating your world…";
    case "derive":
      return zh ? "正在创建分支…" : "Creating the branch…";
    case "capability":
      return zh ? "正在执行…" : "Running…";
    case "proposal":
      return zh ? "正在应用改动…" : "Applying the change…";
  }
}

function permissionNote(kind: ApprovalKind, zh: boolean): string {
  switch (kind) {
    case "world":
      return zh
        ? "只会创建一个仅你可见的新世界；在你确认之前不会写入任何内容。"
        : "Only a new private world visible to you is created; nothing is written until you confirm.";
    case "derive":
      return zh
        ? "只会创建一个独立的新世界；当前世界不会被修改。"
        : "Only one independent new world is created; the current world is not modified.";
    case "capability":
      return zh ? "确认后会在目标对象上执行一次操作。" : "One operation runs on the target once you confirm.";
    case "proposal":
      return zh
        ? "只有你确认后，这个世界才会改变。"
        : "This world changes only after you confirm.";
  }
}

export function ApprovalNote({ children }: { children: ReactNode }) {
  return (
    <p className="dw-approval-note" role="status">
      <Icon name="shield" size={13} />
      {children}
    </p>
  );
}
