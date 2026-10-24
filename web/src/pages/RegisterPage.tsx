// SPDX-License-Identifier: Apache-2.0
import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { APIError } from "../api";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";

function safeNext(value: string | null): string {
  if (!value || !value.startsWith("/") || value.startsWith("//")) {
    return "";
  }
  return value;
}

export default function RegisterPage() {
  const { t } = useI18n();
  const { register, status } = useAuth();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const next = safeNext(searchParams.get("next"));
  const destination = next || "/welcome";
  const [email, setEmail] = useState("");
  const [handle, setHandle] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (status === "authenticated") {
      navigate(destination, { replace: true });
    }
  }, [status, destination, navigate]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setError("");
    setBusy(true);
    try {
      await register({ email, handle, display_name: displayName, password });
      navigate(destination, { replace: true });
    } catch (failure) {
      if (failure instanceof APIError && failure.status === 409) {
        setError(t("auth.register.exists"));
      } else if (failure instanceof APIError && failure.status === 429) {
        setError(t("error.code.rate_limited"));
      } else if (failure instanceof APIError) {
        setError(t("auth.register.invalid"));
      } else {
        setError(t("error.networkBody"));
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="page auth-page">
      <div className="page-heading">
        <p className="eyebrow">{t("app.name")}</p>
        <h1>{t("auth.register.title")}</h1>
        <p className="lead">{t("auth.register.subtitle")}</p>
      </div>
      <form className="card form" onSubmit={submit}>
        <label>
          <span>{t("auth.email")}</span>
          <input
            type="email"
            autoComplete="email"
            required
            maxLength={254}
            value={email}
            onChange={(event) => setEmail(event.target.value)}
          />
        </label>
        <label>
          <span>{t("auth.handle")}</span>
          <input
            autoComplete="username"
            required
            minLength={3}
            maxLength={32}
            pattern="[a-z0-9][a-z0-9_\-]{2,31}"
            value={handle}
            onChange={(event) => setHandle(event.target.value.toLowerCase())}
          />
          <small>{t("auth.handleHint")}</small>
        </label>
        <label>
          <span>{t("auth.displayName")}</span>
          <input
            autoComplete="name"
            required
            minLength={1}
            maxLength={80}
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
          />
        </label>
        <label>
          <span>{t("auth.password")}</span>
          <input
            type="password"
            autoComplete="new-password"
            required
            minLength={12}
            maxLength={128}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
          <small>{t("auth.passwordHint")}</small>
        </label>
        {error ? (
          <p className="form-error" role="alert">
            {error}
          </p>
        ) : null}
        <button className="button" type="submit" disabled={busy}>
          {busy ? t("auth.register.loading") : t("auth.register.submit")}
        </button>
        <p className="form-hint">
          {t("auth.register.haveAccount")}{" "}
          <Link to={`/login${next ? `?next=${encodeURIComponent(next)}` : ""}`}>{t("nav.login")}</Link>
        </p>
      </form>
    </div>
  );
}
