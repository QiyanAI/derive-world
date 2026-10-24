// SPDX-License-Identifier: Apache-2.0
import type { ReactNode } from "react";
import { APIError } from "./api";
import { useI18n } from "./i18n";
import type { MessageKey } from "./i18n/en";

const codeMessages: Record<string, MessageKey> = {
  invalid_request: "error.code.invalid_request",
  unauthenticated: "error.code.unauthenticated",
  forbidden: "error.code.forbidden",
  not_found: "error.code.not_found",
  conflict: "error.code.conflict",
  version_conflict: "error.code.version_conflict",
  idempotency_conflict: "error.code.idempotency_conflict",
  request_in_progress: "error.code.request_in_progress",
  confirmation_required: "error.code.confirmation_required",
  confirmation_expired: "error.code.confirmation_expired",
  invitation_invalid: "error.code.invitation_invalid",
  rate_limited: "error.code.rate_limited",
  budget_exceeded: "error.code.budget_exceeded",
  model_unavailable: "error.code.model_unavailable",
  model_timeout: "error.code.model_timeout",
  model_invalid_output: "error.code.model_invalid_output",
  capability_denied: "error.code.capability_denied",
  capability_failed: "error.code.capability_failed",
  capability_timeout: "error.code.capability_timeout",
  capability_unknown: "error.code.capability_unknown",
  payload_too_large: "error.code.payload_too_large",
  unsupported_media_type: "error.code.unsupported_media_type",
  internal: "error.code.internal"
};

export interface DescribedError {
  code: string;
  messageKey: MessageKey;
  requestId: string;
  serverMessage: string;
  status: number;
  isNetwork: boolean;
}

// describeError converts every failure into the closed set of product-visible
// messages. The server's own message is shown only inside "Technical details";
// it never becomes the primary explanation.
export function describeError(error: unknown): DescribedError {
  if (error instanceof APIError) {
    return {
      code: error.code,
      messageKey: codeMessages[error.code] ?? "error.code.unknown",
      requestId: error.requestId,
      serverMessage: error.message,
      status: error.status,
      isNetwork: false
    };
  }
  return {
    code: "network",
    messageKey: "error.networkBody",
    requestId: "",
    serverMessage: error instanceof Error ? error.message : "Unknown error",
    status: 0,
    isNetwork: true
  };
}

export function errorText(error: unknown): string {
  const described = describeError(error);
  return `${described.code}: ${described.serverMessage}`;
}

export function ErrorNotice({
  error,
  onRetry,
  compact = false
}: {
  error: unknown;
  onRetry?: () => void;
  compact?: boolean;
}) {
  const { t } = useI18n();
  const described = describeError(error);
  const title = described.isNetwork
    ? t("error.network")
    : described.status === 401
      ? t("error.unauthorized")
      : t("error.requestFailed");
  return (
    <div
      className={compact ? "error-panel error-panel-compact" : "error-panel"}
      role="alert"
      data-error-code={described.code}
    >
      <div className="error-panel-body">
        <strong>{title}</strong>
        <p>{t(described.messageKey)}</p>
        {described.requestId ? (
          <p className="mono error-request-id">
            {t("error.requestId")}: {described.requestId}
          </p>
        ) : null}
        <details className="error-details">
          <summary>{t("error.technical")}</summary>
          <p className="mono">{described.code}</p>
          {described.serverMessage ? (
            <p className="mono">
              {t("error.details")}: {described.serverMessage}
            </p>
          ) : null}
        </details>
      </div>
      {onRetry ? (
        <button type="button" className="button button-small button-ghost" onClick={onRetry}>
          {t("error.retry")}
        </button>
      ) : null}
    </div>
  );
}

export function StatusNotice({
  children,
  kind = "success"
}: {
  children: ReactNode;
  kind?: "success" | "info" | "warning";
}) {
  return (
    <p className={`status-notice status-${kind}`} role="status">
      {children}
    </p>
  );
}
