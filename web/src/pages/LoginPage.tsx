// SPDX-License-Identifier: Apache-2.0
import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { APIError } from "../api";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";

function safeNext(value: string | null): string {
  if (!value || !value.startsWith("/") || value.startsWith("//")) {
    return "/";
  }
  return value;
}

export default function LoginPage() {
  const { t } = useI18n();
  const { login, status } = useAuth();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const next = safeNext(searchParams.get("next"));
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (status === "authenticated") {
      navigate(next, { replace: true });
    }
  }, [status, next, navigate]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setError("");
    setBusy(true);
    try {
      await login(email, password);
      navigate(next, { replace: true });
    } catch (failure) {
      if (failure instanceof APIError && failure.status === 401) {
        setError(t("auth.login.failed"));
      } else if (failure instanceof APIError && failure.code === "rate_limited") {
        setError(t("error.code.rate_limited"));
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
        <h1>{t("auth.login.title")}</h1>
        <p className="lead">{t("auth.login.subtitle")}</p>
      </div>
      <form className="card form" onSubmit={submit}>
        <label>
          <span>{t("auth.email")}</span>
          <input
            type="email"
            autoComplete="email"
            required
            value={email}
            onChange={(event) => setEmail(event.target.value)}
          />
        </label>
        <label>
          <span>{t("auth.password")}</span>
          <input
            type="password"
            autoComplete="current-password"
            required
            minLength={12}
            maxLength={128}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </label>
        {error ? (
          <p className="form-error" role="alert">
            {error}
          </p>
        ) : null}
        <button className="button" type="submit" disabled={busy}>
          {busy ? t("auth.login.loading") : t("auth.login.submit")}
        </button>
        <p className="form-hint">
          {t("auth.login.noAccount")}{" "}
          <Link to={`/register?next=${encodeURIComponent(next)}`}>{t("nav.register")}</Link>
        </p>
      </form>
    </div>
  );
}
