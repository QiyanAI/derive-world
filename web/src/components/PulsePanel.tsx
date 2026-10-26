// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router";
import {
  getPulse,
  respondFriendRequest,
  respondPublicationConsent,
  type LivingPulse
} from "../api";
import { ErrorNotice } from "../errors";
import { actionKey, eventKey } from "../humanize";
import { useI18n } from "../i18n";

function pulseTarget(item: { object_id: string; event_type?: string }): string {
  const base = `/objects/${item.object_id}`;
  switch (item.event_type) {
    case "object.state_changed":
      return `${base}?tab=work`;
    case "object.derived":
      return `${base}?tab=lens`;
    case "ai.proposal_created":
    case "ai.proposal_applied":
      return `${base}?tab=ai`;
    default:
      return base;
  }
}

export default function PulsePanel({
  compact = false,
  refreshKey = ""
}: {
  compact?: boolean;
  refreshKey?: string;
}) {
  const { t, lang } = useI18n();
  const [pulse, setPulse] = useState<LivingPulse | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState("");
  const [loading, setLoading] = useState(true);

  const timeFormatter = new Intl.DateTimeFormat(lang === "zh" ? "zh-CN" : "en", {
    dateStyle: "medium",
    timeStyle: "short"
  });

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    setError(null);
    try {
      const data = await getPulse(signal);
      if (!signal?.aborted) {
        setPulse(data);
      }
    } catch (failure) {
      if (!signal?.aborted) {
        setError(failure);
      }
    } finally {
      if (!signal?.aborted) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  useEffect(() => {
    if (refreshKey !== "") {
      void load();
    }
  }, [load, refreshKey]);

  async function act(key: string, action: () => Promise<unknown>) {
    setBusy(key);
    setError(null);
    try {
      await action();
      await load();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  if (loading && !pulse) {
    return <p className="muted">{t("common.loading")}</p>;
  }
  if (error && !pulse) {
    return <ErrorNotice error={error} onRetry={() => void load()} />;
  }
  if (!pulse) {
    return <p className="muted">{t("pulse.empty")}</p>;
  }

  const hasAnything =
    pulse.items.length > 0 ||
    pulse.publication_consents.length > 0 ||
    (pulse.social?.incoming_requests.length ?? 0) > 0 ||
    (pulse.social?.unread_messages ?? 0) > 0;

  return (
    <section className={compact ? "card pulse-panel pulse-compact" : "card pulse-panel"} data-testid="pulse-panel">
      <div className="section-heading">
        <h2>{t("pulse.title")}</h2>
        <button className="button button-ghost button-small" type="button" onClick={() => void load()}>
          {t("common.refresh")}
        </button>
      </div>
      {error ? <ErrorNotice error={error} compact /> : null}

      {!hasAnything ? <p className="muted">{t("pulse.empty")}</p> : null}

      {pulse.social && pulse.social.incoming_requests.length > 0 ? (
        <div className="pulse-section">
          <h3>{t("pulse.friendRequests")}</h3>
          <ul className="pulse-list">
            {pulse.social.incoming_requests.map((request) => (
              <li key={request.id}>
                <div>
                  <strong>{request.display_name}</strong>
                  <small>@{request.handle}</small>
                </div>
                <div className="pulse-actions">
                  <button
                    type="button"
                    className="button button-small"
                    disabled={busy !== ""}
                    onClick={() =>
                      void act(`accept-${request.id}`, () => respondFriendRequest(request.id, true))
                    }
                  >
                    {t("friends.accept")}
                  </button>
                  <button
                    type="button"
                    className="button button-ghost button-small"
                    disabled={busy !== ""}
                    onClick={() =>
                      void act(`decline-${request.id}`, () => respondFriendRequest(request.id, false))
                    }
                  >
                    {t("friends.decline")}
                  </button>
                </div>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      {pulse.publication_consents.length > 0 ? (
        <div className="pulse-section">
          <h3>{t("pulse.consents")}</h3>
          <ul className="pulse-list">
            {pulse.publication_consents.map((consent) => (
              <li key={consent.id}>
                <div>
                  <strong>{consent.title}</strong>
                  <small>{t("pulse.consentBy", { name: consent.publisher_name })}</small>
                </div>
                <div className="pulse-actions">
                  <button
                    type="button"
                    className="button button-small"
                    disabled={busy !== ""}
                    onClick={() =>
                      void act(`approve-${consent.id}`, () => respondPublicationConsent(consent.id, true))
                    }
                  >
                    {t("pulse.approve")}
                  </button>
                  <button
                    type="button"
                    className="button button-ghost button-small"
                    disabled={busy !== ""}
                    onClick={() =>
                      void act(`decline-${consent.id}`, () => respondPublicationConsent(consent.id, false))
                    }
                  >
                    {t("pulse.decline")}
                  </button>
                </div>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      <div className="pulse-section">
        <h3>{t("pulse.changes")}</h3>
        {pulse.items.length === 0 ? (
          <p className="muted">{t("pulse.noChanges")}</p>
        ) : (
          <ol className="pulse-list pulse-timeline">
            {pulse.items.slice(0, compact ? 6 : 30).map((item, index) => (
              <li key={`${item.object_id}-${item.occurred_at}-${index}`}>
                <div>
                  <Link to={pulseTarget(item)}>{item.title || t("common.untitled")}</Link>
                  <small>
                    {item.action ? t(actionKey(item.action)) : t(eventKey(item.event_type ?? ""))}
                    {item.actor_name ? ` · ${item.actor_name}` : ""}
                  </small>
                </div>
                <small className="muted">{timeFormatter.format(new Date(item.occurred_at))}</small>
              </li>
            ))}
          </ol>
        )}
      </div>

      {pulse.social && pulse.social.unread_messages > 0 ? (
        <p className="pulse-unread">
          <Link to="/friends">{t("pulse.unread", { count: pulse.social.unread_messages })}</Link>
        </p>
      ) : null}
    </section>
  );
}
