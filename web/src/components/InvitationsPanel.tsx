// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  createInvitation,
  getSystemStatus,
  listInvitations,
  listFriends,
  sendDirectMessage,
  newClientMessageID,
  revokeInvitation,
  type Invitation,
  type Friend,
  type InvitationPermission
} from "../api";
import { ErrorNotice, StatusNotice } from "../errors";
import { permissionKey } from "../humanize";
import { useI18n } from "../i18n";

interface Props {
  objectID: string;
  canInvite: boolean;
  canManage: boolean;
}

const permissionOptions: InvitationPermission[] = ["read", "contribute", "derive", "invite"];

function statusOf(item: Invitation): "active" | "used" | "expired" | "revoked" {
  if (item.revoked_at) {
    return "revoked";
  }
  if (item.use_count >= item.max_uses) {
    return "used";
  }
  if (new Date(item.expires_at).getTime() <= Date.now()) {
    return "expired";
  }
  return "active";
}

export default function InvitationsPanel({ objectID, canInvite, canManage }: Props) {
  const { t, lang } = useI18n();
  const [items, setItems] = useState<Invitation[]>([]);
  const [selected, setSelected] = useState<InvitationPermission[]>(["read", "contribute"]);
  const [maxUses, setMaxUses] = useState(1);
  const [days, setDays] = useState(7);
  const [createdLink, setCreatedLink] = useState("");
  const [copied, setCopied] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [publicBaseURL, setPublicBaseURL] = useState("");
  const [acceptedFriends, setAcceptedFriends] = useState<Friend[]>([]);
  const [inviteFriendID, setInviteFriendID] = useState("");
  const [invitingFriend, setInvitingFriend] = useState(false);

  const timeFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(lang === "zh" ? "zh-CN" : "en", {
        dateStyle: "medium",
        timeStyle: "short"
      }),
    [lang]
  );

  const refresh = useCallback(async () => {
    try {
      const response = await listInvitations(objectID);
      setItems(response.items);
    } catch (failure) {
      setError(failure);
    }
  }, [objectID]);

  useEffect(() => {
    if (canInvite || canManage) {
      void refresh();
      getSystemStatus()
        .then((status) => setPublicBaseURL(status.public_base_url))
        .catch(() => setPublicBaseURL(""));
      listFriends().then((response) => setAcceptedFriends(response.items)).catch(setError);
    }
  }, [canInvite, canManage, refresh]);

  if (!canInvite && !canManage) {
    return null;
  }

  async function create() {
    setBusy(true);
    setNotice("");
    setError(null);
    setCreatedLink("");
    setCopied(false);
    try {
      const invitation = await createInvitation(objectID, {
        permissions: selected.length > 0 ? selected : undefined,
        max_uses: maxUses,
        expires_in_days: days
      });
      if (invitation.token) {
        const origin = publicBaseURL !== "" ? publicBaseURL.replace(/\/+$/, "") : window.location.origin;
        setCreatedLink(`${origin}/invite/${invitation.token}`);
        setNotice(t("workspace.inviteCreated"));
      }
      await refresh();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy(false);
    }
  }

  async function inviteAcceptedFriend() {
    if (!canInvite || !inviteFriendID || !acceptedFriends.some((f) => f.user_id === inviteFriendID)) return;
    setInvitingFriend(true);
    setError(null);
    setNotice("");
    let issuedID: string | null = null;
    try {
      const invitation = await createInvitation(objectID, {
        permissions: ["read", "contribute"], max_uses: 1, expires_in_days: 7
      });
      issuedID = invitation.id;
      if (!invitation.token) throw new Error("No invitation token was returned");
      const origin = new URL(publicBaseURL.trim() || window.location.origin, window.location.origin);
      if (origin.protocol !== "https:" && origin.hostname !== "localhost" && origin.hostname !== "127.0.0.1") {
        throw new Error("A secure instance URL is required for invitations");
      }
      const link = new URL("/invite/" + encodeURIComponent(invitation.token), origin).toString();
      await sendDirectMessage(inviteFriendID,
        (lang === "zh" ? "邀请你一起创造：" : "Join my world: ") + link, newClientMessageID());
      setNotice(lang === "zh" ? "已私信发出邀请；接受后才获得访问权限。" :
        "Invitation sent by private message; access requires acceptance.");
      await refresh();
    } catch (failure) {
      if (issuedID) { try { await revokeInvitation(objectID, issuedID); } catch { /* original error shown */ } }
      setError(failure);
    } finally { setInvitingFriend(false); }
  }
  async function revoke(id: string) {
    if (!window.confirm(t("workspace.revoke") + "?")) {
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await revokeInvitation(objectID, id);
      await refresh();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy(false);
    }
  }

  function toggle(permission: InvitationPermission) {
    setSelected((previous) =>
      previous.includes(permission)
        ? previous.filter((value) => value !== permission)
        : [...previous, permission]
    );
  }

  async function copyLink() {
    try {
      await navigator.clipboard?.writeText(createdLink);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }

  return (
    <section className="card">
      <h2>{t("workspace.invitations")}</h2>
      <p className="muted">{t("workspace.accessIntro")}</p>
      <div className="field-stack">
        <fieldset className="permission-grid">
          <legend>{t("workspace.invitePermissions")}</legend>
          {permissionOptions.map((permission) => (
            <label key={permission} className="check-row">
              <input type="checkbox" checked={selected.includes(permission)} onChange={() => toggle(permission)} />
              <span>{t(permissionKey(permission))}</span>
            </label>
          ))}
        </fieldset>
        {selected.length === 0 ? <p className="form-hint">{t("workspace.inviteDefault")}</p> : null}
        <div className="inline-field">
          <label>
            <span>{t("workspace.maxUses")}</span>
            <input
              type="number"
              min={1}
              max={100}
              value={maxUses}
              onChange={(event) => setMaxUses(Number(event.target.value))}
            />
          </label>
          <label>
            <span>{t("workspace.expiresDays")}</span>
            <input
              type="number"
              min={1}
              max={30}
              value={days}
              onChange={(event) => setDays(Number(event.target.value))}
            />
          </label>
        </div>
        <button
          className="button button-small"
          type="button"
          data-testid="invite-create"
          disabled={busy}
          onClick={() => void create()}
        >
          {t("workspace.createInvite")}
        </button>
      </div>
      {canInvite && acceptedFriends.length > 0 ? (
        <div className="dw-invite-friend">
          <h3>{lang === "zh" ? "邀请已添加的好友" : "Invite a friend to create together"}</h3>
          <p className="form-hint">{lang === "zh" ?
            "选择已同意的好友。一次性邀请通过私信发送，对方接受后才能访问。" :
            "Only accepted friends can be invited. A one-use private invitation requires acceptance."}</p>
          <div className="inline-field">
            <select aria-label={lang === "zh" ? "选择好友" : "Choose friend"}
              value={inviteFriendID} onChange={(event) => setInviteFriendID(event.target.value)}>
              <option value="">{lang === "zh" ? "选择好友" : "Choose a friend"}</option>
              {acceptedFriends.map((friend) => (
                <option key={friend.user_id} value={friend.user_id}>
                  {friend.display_name} (@{friend.handle})
                </option>
              ))}
            </select>
            <button type="button" className="button button-small"
              data-testid="invite-accepted-friend"
              disabled={!inviteFriendID || invitingFriend || busy}
              onClick={() => void inviteAcceptedFriend()}>
              {invitingFriend ? t("common.loading") :
                (lang === "zh" ? "邀请一起创造" : "Invite to collaborate")}
            </button>
          </div>
        </div>
      ) : null}
      {notice ? <StatusNotice>{notice}</StatusNotice> : null}
      {createdLink ? (
        <div className="link-once">
          <label>
            <span>{t("workspace.inviteLink")}</span>
            <input
              readOnly
              data-testid="invite-link"
              value={createdLink}
              onFocus={(event) => event.target.select()}
            />
          </label>
          <button className="button button-small button-ghost" type="button" onClick={() => void copyLink()}>
            {copied ? t("common.copied") : t("common.copy")}
          </button>
        </div>
      ) : null}
      {error ? <ErrorNotice error={error} compact /> : null}
      {items.length === 0 ? <p className="muted">{t("workspace.noInvites")}</p> : null}
      <ul className="invitation-list">
        {items.map((item) => {
          const status = statusOf(item);
          return (
            <li key={item.id}>
              <div>
                <strong>{item.permissions.map((permission) => t(permissionKey(permission))).join(" · ")}</strong>
                <small>
                  {t("workspace.inviteUsage", { used: item.use_count, max: item.max_uses })} ·{" "}
                  {timeFormatter.format(new Date(item.expires_at))} ·{" "}
                  <span className={`status-chip status-${status}`}>{t(`workspace.inviteStatus.${status}`)}</span>
                </small>
              </div>
              {status === "active" ? (
                <button className="text-button" type="button" disabled={busy} onClick={() => void revoke(item.id)}>
                  {t("workspace.revoke")}
                </button>
              ) : null}
            </li>
          );
        })}
      </ul>
    </section>
  );
}
