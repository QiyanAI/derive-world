// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useState } from "react";
import {
  createPublication,
  listPublications,
  revokePublication,
  type Publication
} from "../api";
import { ErrorNotice, StatusNotice } from "../errors";
import { useI18n } from "../i18n";

export default function PublicationPanel({
  objectID,
  currentVersion,
  canManage
}: {
  objectID: string;
  currentVersion: number;
  canManage: boolean;
}) {
  const { t, lang } = useI18n();
  const [items, setItems] = useState<Publication[]>([]);
  const [version, setVersion] = useState("");
  const [deriveAllowed, setDeriveAllowed] = useState(false);
  const [license, setLicense] = useState<Publication["license"]>("all_rights_reserved");
  const [error, setError] = useState<unknown>(null);
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState("");
  const [loading, setLoading] = useState(true);

  const timeFormatter = new Intl.DateTimeFormat(lang === "zh" ? "zh-CN" : "en", {
    dateStyle: "medium",
    timeStyle: "short"
  });

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true);
      setError(null);
      try {
        const response = await listPublications(objectID, signal);
        if (!signal?.aborted) {
          setItems(response.items);
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
    },
    [objectID]
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  async function publish() {
    setBusy("publish");
    setError(null);
    setNotice("");
    try {
      await createPublication(objectID, {
        source_version: version === "" ? undefined : Number(version),
        derive_allowed: deriveAllowed,
        license
      });
      setNotice(t("publish.created"));
      await load();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function revoke(id: string) {
    setBusy(id);
    setError(null);
    setNotice("");
    try {
      await revokePublication(id);
      setNotice(t("publish.revoked"));
      await load();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  return (
    <section className="card publication-panel" data-testid="publication-panel">
      <div className="section-heading">
        <h2>{t("publish.title")}</h2>
        <button className="button button-ghost button-small" type="button" onClick={() => void load()}>
          {t("common.refresh")}
        </button>
      </div>
      <p className="muted">{t("publish.lead")}</p>
      {notice ? <StatusNotice>{notice}</StatusNotice> : null}
      {error ? <ErrorNotice error={error} /> : null}
      {loading && items.length === 0 ? <p className="muted">{t("common.loading")}</p> : null}

      {canManage ? (
        <div className="publish-form">
          <label>
            <span>{t("publish.version")}</span>
            <select value={version} onChange={(event) => setVersion(event.target.value)}>
              <option value="">{t("publish.currentVersion", { version: currentVersion })}</option>
              {Array.from({ length: Math.max(0, currentVersion - 1) }, (_, index) => index + 1).map((value) => (
                <option key={value} value={String(value)}>
                  v{value}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>{t("publish.license")}</span>
            <select value={license} onChange={(event) => setLicense(event.target.value as Publication["license"])}>
              <option value="all_rights_reserved">{t("publish.license.allRights")}</option>
              <option value="cc_by_4.0">CC BY 4.0</option>
              <option value="cc_by_sa_4.0">CC BY-SA 4.0</option>
            </select>
          </label>
          <label className="check-row">
            <input
              type="checkbox"
              checked={deriveAllowed}
              onChange={(event) => setDeriveAllowed(event.target.checked)}
            />
            <span>{t("publish.allowDerive")}</span>
          </label>
          <button className="button" type="button" disabled={busy !== ""} onClick={() => void publish()}>
            {busy === "publish" ? t("publish.publishing") : t("publish.publish")}
          </button>
          <p className="form-hint">{t("publish.consentHint")}</p>
        </div>
      ) : (
        <p className="muted">{t("publish.ownerOnly")}</p>
      )}

      <h3>{t("publish.records")}</h3>
      {items.length === 0 ? (
        <p className="muted">{t("publish.none")}</p>
      ) : (
        <ul className="publication-list">
          {items.map((item) => (
            <li key={item.id}>
              <div>
                <strong>
                  v{item.source_version} · {t(`publish.status.${item.status}`)}
                </strong>
                <small>
                  {timeFormatter.format(new Date(item.created_at))} · {item.license} ·{" "}
                  {item.derive_allowed ? t("publish.derivable") : t("publish.viewOnly")}
                </small>
              </div>
              <div className="pulse-actions">
                {item.status === "active" ? (
                  <a
                    className="text-link"
                    href={`/explore/${item.id}`}
                    target="_blank"
                    rel="noreferrer"
                  >
                    {t("publish.openPublic")}
                  </a>
                ) : null}
                {item.status !== "revoked" ? (
                  <button
                    className="button button-ghost button-small button-danger"
                    type="button"
                    disabled={busy !== ""}
                    onClick={() => void revoke(item.id)}
                  >
                    {t("publish.revoke")}
                  </button>
                ) : null}
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
