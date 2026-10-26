// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router";
import {
  checkModelConnection,
  createToken,
  exportMe,
  getSystemStatus,
  listTokens,
  revokeToken,
  type CreatedToken,
  type ModelCheck,
  type SystemStatus,
  type TokenMetadata
} from "../api";
import { ErrorNotice, StatusNotice } from "../errors";
import { useAuth } from "../auth";
import { useI18n, type Lang } from "../i18n";
import { useMeta } from "../meta";

const scopeOptions = ["objects:read", "objects:write", "derive:create", "capabilities:invoke"];

export default function SettingsPage() {
  const { t, lang, setLang } = useI18n();
  const { status, user, logout, logoutError } = useAuth();
  const { meta, mode } = useMeta();
  const [tokens, setTokens] = useState<TokenMetadata[]>([]);
  const [tokenName, setTokenName] = useState("");
  const [scopes, setScopes] = useState<string[]>(["objects:read"]);
  const [expiry, setExpiry] = useState("");
  const [createdToken, setCreatedToken] = useState<CreatedToken | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [notice, setNotice] = useState("");
  const [system, setSystem] = useState<SystemStatus | null>(null);
  const [modelCheck, setModelCheck] = useState<ModelCheck | null>(null);
  const [checkingModel, setCheckingModel] = useState(false);

  const loadTokens = useCallback(async () => {
    if (status !== "authenticated") {
      return;
    }
    try {
      const response = await listTokens();
      setTokens(response.items);
    } catch (failure) {
      setError(failure);
    }
  }, [status]);

  useEffect(() => {
    void loadTokens();
  }, [loadTokens]);

  useEffect(() => {
    if (status !== "authenticated") {
      return;
    }
    const controller = new AbortController();
    getSystemStatus(controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) {
          setSystem(value);
        }
      })
      .catch(() => undefined);
    return () => controller.abort();
  }, [status]);

  async function runModelCheck() {
    setCheckingModel(true);
    setModelCheck(null);
    try {
      setModelCheck(await checkModelConnection());
    } catch (failure) {
      setError(failure);
    } finally {
      setCheckingModel(false);
    }
  }

  function toggleScope(scope: string) {
    setScopes((previous) =>
      previous.includes(scope) ? previous.filter((value) => value !== scope) : [...previous, scope]
    );
  }

  async function submitToken() {
    if (!tokenName.trim() || scopes.length === 0) {
      return;
    }
    setBusy("token");
    setError(null);
    setNotice("");
    setCreatedToken(null);
    try {
      const created = await createToken({
        name: tokenName.trim(),
        scopes,
        expires_in_days: expiry ? Number(expiry) : undefined
      });
      setCreatedToken(created);
      setTokenName("");
      setExpiry("");
      await loadTokens();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function revoke(id: string) {
    setBusy(`revoke:${id}`);
    setError(null);
    try {
      await revokeToken(id);
      await loadTokens();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function downloadExport() {
    setBusy("export");
    setError(null);
    setNotice("");
    try {
      const data = await exportMe();
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = "derive-world-export.json";
      anchor.click();
      URL.revokeObjectURL(url);
      setNotice(t("settings.exportHint"));
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  return (
    <div className="page">
      <div className="page-heading">
        <p className="eyebrow">{t("app.name")}</p>
        <h1>{t("settings.title")}</h1>
        <p className="lead">{t("settings.lead")}</p>
      </div>

      {notice ? <StatusNotice>{notice}</StatusNotice> : null}
      {error ? <ErrorNotice error={error} onRetry={() => void loadTokens()} /> : null}

      <section className="card">
        <h2>{t("settings.language")}</h2>
        <div className="segmented" role="group" aria-label={t("settings.language")}>
          {(["en", "zh"] as Lang[]).map((value) => (
            <button
              key={value}
              type="button"
              className={lang === value ? "segment segment-active" : "segment"}
              onClick={() => setLang(value)}
              aria-pressed={lang === value}
            >
              {value === "en" ? "English" : "中文"}
            </button>
          ))}
        </div>
      </section>

      <section className="card system-status-card">
        <h2>{t("settings.system")}</h2>
        <p className="muted">{t("settings.systemHint")}</p>
        {system ? (
          <>
            <dl className="detail-list">
              <div>
                <dt>{t("system.modelMode")}</dt>
                <dd>
                  <span className={`mode-badge mode-${system.model_mode}`}>{system.model_mode.toUpperCase()}</span>
                  {system.model_name ? <span className="mono"> {system.model_name}</span> : null}
                </dd>
              </div>
              <div>
                <dt>{t("system.database")}</dt>
                <dd>{system.database_ok ? t("system.ok") : t("system.unavailable")}</dd>
              </div>
              <div>
                <dt>{t("system.capabilities")}</dt>
                <dd>{system.capabilities_ready ? t("system.ok") : t("system.unavailable")}</dd>
              </div>
              <div>
                <dt>{t("system.publicBaseURL")}</dt>
                <dd className="mono">{system.public_base_url}</dd>
              </div>
            </dl>
            <div className="model-check-row">
              <button
                className="button button-ghost button-small"
                type="button"
                disabled={checkingModel}
                onClick={() => void runModelCheck()}
              >
                {checkingModel ? t("system.checking") : t("system.checkModel")}
              </button>
              {modelCheck ? (
                <span className={`model-check model-check-${modelCheck.status}`}>
                  {t(`system.check.${modelCheck.status}`)} · {modelCheck.detail}
                </span>
              ) : null}
            </div>
            <h3>{t("system.quotas")}</h3>
            <p className="form-hint">{t("system.quotaNote")}</p>
            <dl className="detail-list">
              <div>
                <dt>{t("system.dailyCalls")}</dt>
                <dd>{system.quota.daily_calls}</dd>
              </div>
              <div>
                <dt>{t("system.dailyChars")}</dt>
                <dd>{system.quota.daily_input_chars}</dd>
              </div>
              <div>
                <dt>{t("system.concurrency")}</dt>
                <dd>{system.quota.concurrency}</dd>
              </div>
              <div>
                <dt>{t("system.hourly")}</dt>
                <dd>
                  {system.rate_limits.ai_per_hour} / {t("system.burst")} {system.rate_limits.ai_burst}
                </dd>
              </div>
              <div>
                <dt>{t("system.globalDaily")}</dt>
                <dd>{system.quota.global_daily_calls}</dd>
              </div>
              <div>
                <dt>{t("system.apiPerMinute")}</dt>
                <dd>{system.rate_limits.api_per_minute}</dd>
              </div>
              <div>
                <dt>{t("system.killSwitch")}</dt>
                <dd>{system.quota.kill_switch ? t("system.on") : t("system.off")}</dd>
              </div>
            </dl>
          </>
        ) : (
          <p className="muted">{t("system.unavailable")}</p>
        )}
      </section>

      <section className="card">
        <h2>{t("settings.account")}</h2>
        {status === "authenticated" && user ? (
          <>
            <p>
              <strong>{user.display_name}</strong> <span className="mono">@{user.handle}</span>
            </p>
            <p>{user.email}</p>
            <div className="form-actions">
              <Link className="button button-small button-ghost" to="/self">
                {t("nav.self")}
              </Link>
              <button className="button button-small button-ghost" type="button" onClick={() => void logout()}>
                {t("settings.signOut")}
              </button>
            </div>
            {logoutError ? (
              <p className="form-error" role="alert">
                {t("auth.logoutFailed")}
              </p>
            ) : null}
          </>
        ) : (
          <>
            <p className="muted">{t("auth.required")}</p>
            <Link className="button" to="/login?next=%2Fsettings">
              {t("nav.login")}
            </Link>
          </>
        )}
      </section>

      <section className="card">
        <h2>{t("settings.tokens")}</h2>
        <p className="muted">{t("settings.tokensHint")}</p>
        {status === "authenticated" ? (
          <>
            <div className="field-stack">
              <label>
                <span>{t("settings.tokenName")}</span>
                <input maxLength={64} value={tokenName} onChange={(event) => setTokenName(event.target.value)} />
              </label>
              <fieldset className="permission-grid">
                <legend>{t("settings.tokenScopes")}</legend>
                {scopeOptions.map((scope) => (
                  <label key={scope} className="check-row">
                    <input
                      type="checkbox"
                      checked={scopes.includes(scope)}
                      onChange={() => toggleScope(scope)}
                    />
                    <span className="mono">{scope}</span>
                  </label>
                ))}
              </fieldset>
              <label>
                <span>{t("settings.tokenExpiry")}</span>
                <input
                  type="number"
                  min={1}
                  max={365}
                  value={expiry}
                  onChange={(event) => setExpiry(event.target.value)}
                />
              </label>
              <button
                className="button button-small"
                type="button"
                disabled={busy !== "" || !tokenName.trim() || scopes.length === 0}
                onClick={() => void submitToken()}
              >
                {t("settings.createToken")}
              </button>
            </div>
            {createdToken ? (
              <div className="link-once">
                <p className="notice notice-compact">{t("settings.tokenOnce")}</p>
                <label>
                  <span>{createdToken.token_prefix}</span>
                  <input readOnly value={createdToken.plaintext} onFocus={(event) => event.target.select()} />
                </label>
                <button
                  className="button button-small button-ghost"
                  type="button"
                  onClick={() => void navigator.clipboard?.writeText(createdToken.plaintext)}
                >
                  {t("common.copy")}
                </button>
              </div>
            ) : null}
            {tokens.length === 0 ? <p className="muted">{t("settings.noTokens")}</p> : null}
            <ul className="token-list">
              {tokens.map((token) => (
                <li key={token.id}>
                  <div>
                    <strong>{token.name}</strong>
                    <small className="mono">
                      {token.token_prefix} · {token.scopes.join(", ")}
                      {token.revoked_at ? ` · ${t("settings.revoked")}` : ""}
                    </small>
                  </div>
                  {!token.revoked_at ? (
                    <button
                      className="text-button"
                      type="button"
                      disabled={busy !== ""}
                      onClick={() => void revoke(token.id)}
                    >
                      {t("workspace.revoke")}
                    </button>
                  ) : null}
                </li>
              ))}
            </ul>
            <button
              className="button button-ghost button-small"
              type="button"
              disabled={busy !== ""}
              onClick={() => void downloadExport()}
            >
              {t("settings.export")}
            </button>
            <p className="form-hint">{t("settings.exportHint")}</p>
          </>
        ) : (
          <p className="muted">{t("auth.required")}</p>
        )}
      </section>

      <section className="card">
        <h2>{t("settings.limitsTitle")}</h2>
        <dl className="meta-grid">
          <div>
            <dt>{t("settings.modelMode")}</dt>
            <dd>
              <span className={`mode-badge mode-${mode}`}>{mode}</span>
            </dd>
          </div>
          <div>
            <dt>{t("settings.version")}</dt>
            <dd>{meta?.version ?? "—"}</dd>
          </div>
          <div>
            <dt>{t("settings.commit")}</dt>
            <dd className="mono">{meta?.commit ?? "—"}</dd>
          </div>
          <div>
            <dt>
              {t("settings.limits")} · {t("settings.documentedDefaults")}
            </dt>
            <dd>{t("settings.limitsValue")}</dd>
          </div>
        </dl>
        {mode === "fixture" ? <p className="muted">{t("settings.fixtureNote")}</p> : null}
      </section>

      <section className="card">
        <h2>{t("settings.releaseStatus")}</h2>
        <p>{t("settings.g05Scope")}</p>
        <p className="muted">{t("footer.release")}</p>
      </section>

      <section className="card">
        <h2>{t("settings.security")}</h2>
        <p>{t("settings.securityBody")}</p>
      </section>
    </div>
  );
}
