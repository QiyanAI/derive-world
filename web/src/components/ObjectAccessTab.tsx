// SPDX-License-Identifier: Apache-2.0
import { useEffect, useState } from "react";
import { applyAction, removeMember, type Member, type ObjectView } from "../api";
import { ErrorNotice, StatusNotice } from "../errors";
import { useI18n } from "../i18n";
import HandoffPanel from "./HandoffPanel";
import InvitationsPanel from "./InvitationsPanel";
import PermissionList from "./PermissionList";

function initials(name: string): string {
  return name.trim().slice(0, 1).toUpperCase() || "?";
}

export default function ObjectAccessTab({
  object,
  members,
  canManage,
  canInvite,
  onUpdated,
  onReload
}: {
  object: ObjectView;
  members: Member[];
  canManage: boolean;
  canInvite: boolean;
  onUpdated: (updated: ObjectView) => Promise<void> | void;
  onReload: () => Promise<void> | void;
}) {
  const { t } = useI18n();
  const [sharedDraft, setSharedDraft] = useState(JSON.stringify(object.shared_context, null, 2));
  const sharedBytes = new TextEncoder().encode(sharedDraft).length;
  const [sharedDirty, setSharedDirty] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [formError, setFormError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    if (!sharedDirty) {
      setSharedDraft(JSON.stringify(object.shared_context, null, 2));
    }
  }, [object, sharedDirty]);

  async function saveShared() {
    setNotice("");
    setError(null);
    setFormError("");
    let parsed: unknown;
    try {
      parsed = JSON.parse(sharedDraft || "{}");
      if (parsed === null || Array.isArray(parsed) || typeof parsed !== "object") {
        throw new Error("shared");
      }
    } catch {
      setFormError(t("object.create.sharedInvalid"));
      return;
    }
    setBusy("shared");
    try {
      const updated = await applyAction(
        object.id,
        "set_shared_context",
        { shared_context: parsed as Record<string, unknown> },
        object.version
      );
      setSharedDirty(false);
      await onUpdated(updated);
      setNotice(t("workspace.saved"));
    } catch (failure) {
      setError(failure);
      await onReload();
    } finally {
      setBusy("");
    }
  }

  async function remove(userID: string) {
    if (!window.confirm(t("workspace.removeMemberConfirm"))) {
      return;
    }
    setBusy(`member:${userID}`);
    setError(null);
    try {
      await removeMember(object.id, userID);
      await onReload();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  return (
    <div className="stack">
      <section className="card">
        <h2>{t("workspace.members")}</h2>
        <p className="muted">{t("workspace.accessIntro")}</p>
        <ul className="member-list member-list-rich" data-testid="member-list">
          {members.map((member) => (
            <li key={member.user_id}>
              <span className="avatar" aria-hidden="true">
                {initials(member.display_name)}
              </span>
              <div className="member-main">
                <strong>{member.display_name}</strong>
                <small>{t(`role.${member.role}`)}</small>
              </div>
              <PermissionList permissions={member.permissions} />
              {canManage && member.user_id !== object.owner_id ? (
                <button
                  type="button"
                  className="text-button"
                  disabled={busy !== ""}
                  onClick={() => void remove(member.user_id)}
                >
                  {t("workspace.removeMember")}
                </button>
              ) : null}
            </li>
          ))}
        </ul>
      </section>

      <InvitationsPanel objectID={object.id} canInvite={canInvite} canManage={canManage} />

      <HandoffPanel objectID={object.id} />

      <section className="card">
        <h2>{t("workspace.sharedContext")}</h2>
        <p className="muted">{t("workspace.sharedHint")}</p>
        <pre className="code-block">{JSON.stringify(object.shared_context, null, 2)}</pre>
        {canManage ? (
          <>
            <textarea
              className="mono"
              rows={5}
              aria-label={t("workspace.sharedContext")}
              value={sharedDraft}
              onChange={(event) => {
                setSharedDraft(event.target.value);
                setSharedDirty(true);
              }}
            />
            <small className={sharedBytes > 8192 ? "form-error" : "muted"}>
              {t("workspace.sharedBytes", { bytes: sharedBytes })}
            </small>
            <div className="form-actions">
              <button
                className="button button-small"
                type="button"
                disabled={busy !== "" || !sharedDirty || sharedBytes > 8192}
                onClick={() => void saveShared()}
              >
                {busy === "shared" ? t("common.saving") : t("workspace.saveShared")}
              </button>
              {sharedDirty ? (
                <button
                  className="button button-small button-ghost"
                  type="button"
                  disabled={busy !== ""}
                  onClick={() => {
                    setSharedDraft(JSON.stringify(object.shared_context, null, 2));
                    setSharedDirty(false);
                  }}
                >
                  {t("common.cancel")}
                </button>
              ) : null}
            </div>
          </>
        ) : (
          <p className="form-hint">{t("workspace.sharedContextOwner")}</p>
        )}
        {formError ? (
          <p className="form-error" role="alert">
            {formError}
          </p>
        ) : null}
        {notice ? <StatusNotice>{notice}</StatusNotice> : null}
        {error ? <ErrorNotice error={error} compact /> : null}
      </section>
    </div>
  );
}
