// SPDX-License-Identifier: Apache-2.0
import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { APIError, acceptInvitation, previewInvitation, type InvitationPreview } from "../api";
import { ErrorNotice } from "../errors";
import { permissionBodyKey, permissionKey } from "../humanize";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";

export default function InvitePage() {
  const { token } = useParams();
  const { t, lang } = useI18n();
  const { status } = useAuth();
  const navigate = useNavigate();
  const [preview, setPreview] = useState<InvitationPreview | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [invalid, setInvalid] = useState(false);
  const [reloadKey, setReloadKey] = useState(0);

  const nextPath = `/invite/${token ?? ""}`;
  const timeFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(lang === "zh" ? "zh-CN" : "en", {
        dateStyle: "medium",
        timeStyle: "short"
      }),
    [lang]
  );

  useEffect(() => {
    if (status !== "authenticated" || !token) {
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    setInvalid(false);
    previewInvitation(token, controller.signal)
      .then(setPreview)
      .catch((failure) => {
        if (controller.signal.aborted) {
          return;
        }
        if (failure instanceof APIError && (failure.status === 404 || failure.code === "invitation_invalid")) {
          setInvalid(true);
        } else {
          setError(failure);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setLoading(false);
        }
      });
    return () => controller.abort();
  }, [status, token, reloadKey]);

  async function accept() {
    if (!token) {
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const result = await acceptInvitation(token);
      navigate(`/objects/${result.object.id}?tab=work&joined=1`);
    } catch (failure) {
      if (failure instanceof APIError && (failure.status === 404 || failure.code === "invitation_invalid")) {
        setInvalid(true);
      } else {
        setError(failure);
      }
    } finally {
      setBusy(false);
    }
  }

  if (status === "loading") {
    return <p className="page">{t("common.loading")}</p>;
  }
  if (status === "anonymous") {
    return (
      <div className="page">
        <p className="eyebrow">{t("invite.preview")}</p>
        <h1>{t("invite.preview")}</h1>
        <p className="lead">{t("invite.loginRequired")}</p>
        <div className="form-actions">
          <Link className="button" to={`/login?next=${encodeURIComponent(nextPath)}`}>
            {t("invite.signIn")}
          </Link>
          <Link className="button button-ghost" to={`/register?next=${encodeURIComponent(nextPath)}`}>
            {t("invite.createAccount")}
          </Link>
        </div>
      </div>
    );
  }
  if (loading) {
    return <p className="page">{t("common.loading")}</p>;
  }
  if (!preview && error && !invalid) {
    return (
      <div className="page">
        <p className="eyebrow">{t("invite.preview")}</p>
        <h1>{t("error.requestFailed")}</h1>
        <ErrorNotice error={error} onRetry={() => setReloadKey((value) => value + 1)} />
        <Link className="button" to="/objects">
          {t("invite.back")}
        </Link>
      </div>
    );
  }
  if (invalid || !preview) {
    return (
      <div className="page">
        <p className="eyebrow">{t("invite.preview")}</p>
        <h1>{t("invite.invalid")}</h1>
        <p className="lead">{t("invite.invalidBody")}</p>
        {error ? <ErrorNotice error={error} /> : null}
        <Link className="button" to="/objects">
          {t("invite.back")}
        </Link>
      </div>
    );
  }

  return (
    <div className="page invite-page">
      <p className="eyebrow">{t("invite.preview")}</p>
      <h1>{preview.title}</h1>
      <p className="lead">
        {t("invite.inviter")}: {preview.inviter_name}
      </p>
      <p>
        <span className={`template-badge template-${preview.object_type}`}>
          {preview.object_type === "collaborative"
            ? t("invite.type.collaborative")
            : t("invite.type.interactive")}
        </span>
      </p>

      <div className="invite-grid">
        <section className="card">
          <h2>{t("invite.permissions")}</h2>
          <ul className="permission-list">
            {preview.permissions.map((permission) => (
              <li key={permission}>
                <strong>{t(permissionKey(permission))}</strong>
                <span>{t(permissionBodyKey(permission))}</span>
              </li>
            ))}
          </ul>
          <dl className="detail-list">
            <div>
              <dt>{t("invite.expires")}</dt>
              <dd>{timeFormatter.format(new Date(preview.expires_at))}</dd>
            </div>
            <div>
              <dt>{t("invite.remaining")}</dt>
              <dd className="mono">
                {preview.remaining_uses} / {preview.max_uses}
              </dd>
            </div>
          </dl>
        </section>
        <section className="card">
          <h2>{t("invite.whatNext")}</h2>
          <p>{t("invite.whatNextBody")}</p>
          <p className="muted">{t("invite.permissionNote")}</p>
          {preview.already_member ? (
            <p className="notice notice-compact">{t("invite.alreadyMember")}</p>
          ) : null}
          {error ? <ErrorNotice error={error} compact /> : null}
          <div className="form-actions">
            {preview.already_member ? (
              <Link className="button" to={`/objects/${preview.object_id}`}>
                {t("invite.alreadyMemberOpen")}
              </Link>
            ) : (
              <button className="button" type="button" disabled={busy} onClick={() => void accept()}>
                {busy ? t("invite.joining") : t("invite.accept")}
              </button>
            )}
            <Link className="button button-ghost" to="/objects">
              {t("invite.back")}
            </Link>
          </div>
        </section>
      </div>
    </div>
  );
}
