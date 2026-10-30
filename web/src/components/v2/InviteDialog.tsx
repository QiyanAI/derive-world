// SPDX-License-Identifier: Apache-2.0
import { useEffect, useState } from "react";
import {
  createInvitation,
  errorMessage,
  newClientMessageID,
  sendDirectMessage,
  type Friend,
  type Invitation,
  type ObjectView
} from "../../api";
import { useI18n } from "../../i18n";
import Icon from "./Icon";
import Modal from "./Modal";

function objectTitle(object: ObjectView | null, fallback: string): string {
  const title = object?.state.title;
  return typeof title === "string" && title ? title : fallback;
}

export default function InviteDialog({
  open,
  object,
  friends,
  onClose,
  onFindFriend
}: {
  open: boolean;
  object: ObjectView | null;
  friends: Friend[];
  onClose: () => void;
  onFindFriend: () => void;
}) {
  const { lang } = useI18n();
  const zh = lang === "zh";
  const [invitation, setInvitation] = useState<Invitation | null>(null);
  const [busy, setBusy] = useState("");
  const [sentTo, setSentTo] = useState<string[]>([]);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    if (open) {
      setInvitation(null);
      setBusy("");
      setSentTo([]);
      setNotice("");
      setError("");
    }
  }, [open, object?.id]);

  const link =
    invitation?.token && typeof window !== "undefined"
      ? `${window.location.origin}/invite/${encodeURIComponent(invitation.token)}`
      : "";

  async function createLink() {
    if (!object) {
      return;
    }
    setBusy("create");
    setError("");
    setNotice("");
    try {
      const created = await createInvitation(object.id, {
        permissions: ["read", "contribute"],
        max_uses: 1,
        expires_in_days: 14
      });
      setInvitation(created);
      setNotice(zh ? "邀请已创建。只有接受它的人会获得所列权限。" : "Invitation created. Only someone who accepts it receives the listed permissions.");
    } catch (failure) {
      setError(errorMessage(failure));
    } finally {
      setBusy("");
    }
  }

  async function copyLink() {
    if (!link) {
      return;
    }
    try {
      await navigator.clipboard.writeText(link);
      setNotice(zh ? "邀请链接已复制。" : "Invitation link copied.");
    } catch {
      setNotice(zh ? "无法自动复制，请手动选择链接。" : "Could not copy automatically. Select the link manually.");
    }
  }

  async function sendTo(friend: Friend) {
    if (!link || !object) {
      return;
    }
    setBusy(`send:${friend.user_id}`);
    setError("");
    try {
      const title = objectTitle(object, zh ? "这个世界" : "this world");
      await sendDirectMessage(
        friend.user_id,
        zh
          ? `我想邀请你一起继续「${title}」。这是单独的世界邀请，接受后你才会获得参与权限：${link}`
          : `I'd like to invite you to continue “${title}” with me. This is a separate world invitation; you receive access only after accepting: ${link}`,
        newClientMessageID()
      );
      setSentTo((current) => [...current, friend.user_id]);
      setNotice(zh ? `邀请链接已通过私信发送给 ${friend.display_name}。` : `The invitation link was sent to ${friend.display_name} by direct message.`);
    } catch (failure) {
      setError(errorMessage(failure));
    } finally {
      setBusy("");
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={zh ? "邀请好友进入这个世界" : "Invite someone into this world"}
      kicker={zh ? "INVITE TO CREATE" : "INVITE TO CREATE"}
      wide
      actions={
        <>
          <button className="dw-button dw-button-ghost" type="button" onClick={onFindFriend}>
            <Icon name="search" size={15} />
            {zh ? "查找好友" : "Find a friend"}
          </button>
          <button className="dw-button dw-button-ghost" type="button" onClick={onClose}>
            {zh ? "完成" : "Done"}
          </button>
        </>
      }
    >
      <p className="dw-modal-lead">
        {object
          ? zh
            ? `你正在邀请别人进入「${objectTitle(object, "未命名世界")}」。好友身份不会自动授权；接受邀请后，对方才会获得你选择的权限。`
            : `You are inviting someone into “${objectTitle(object, "Untitled world")}”. Friendship grants nothing by itself; the person receives access only after accepting.`
          : zh
            ? "先选择一个世界，再邀请好友。"
            : "Select a world before inviting someone."}
      </p>

      {!invitation ? (
        <div className="dw-invite-empty">
          <div className="dw-invite-permissions">
            <span><Icon name="eye" size={15} />{zh ? "查看世界内容" : "Read the world"}</span>
            <span><Icon name="edit" size={15} />{zh ? "添加与修改贡献" : "Contribute changes"}</span>
            <span><Icon name="shield" size={15} />{zh ? "不共享私人 AI 上下文" : "No private AI context"}</span>
          </div>
          <button
            className="dw-button dw-button-primary"
            type="button"
            disabled={!object || busy === "create"}
            onClick={() => void createLink()}
          >
            <Icon name="link" size={16} />
            {busy === "create" ? (zh ? "正在创建…" : "Creating…") : zh ? "创建一次性邀请链接" : "Create a one-use invitation"}
          </button>
          <p className="dw-fineprint">
            {zh
              ? "邀请默认 14 天有效、只能使用一次。你随时可以撤回尚未接受的邀请。"
              : "Invitations expire after 14 days, can be used once, and can be revoked before acceptance."}
          </p>
        </div>
      ) : (
        <div className="dw-invite-created">
          <label className="dw-field-label" htmlFor="dw-invite-link">
            {zh ? "邀请链接" : "Invitation link"}
          </label>
          <div className="dw-invite-link-row">
            <input id="dw-invite-link" className="dw-input" value={link} readOnly onFocus={(event) => event.currentTarget.select()} />
            <button className="dw-button dw-button-primary" type="button" onClick={() => void copyLink()}>
              <Icon name="link" size={15} />
              {zh ? "复制" : "Copy"}
            </button>
          </div>
          <h3>{zh ? "通过私信发给好友" : "Send to a friend by direct message"}</h3>
          {friends.length === 0 ? (
            <p className="dw-empty-note">
              {zh ? "还没有好友。先查找并添加好友，再单独发送世界邀请。" : "No friends yet. Find and add someone first, then send a separate world invitation."}
            </p>
          ) : (
            <div className="dw-invite-friends">
              {friends.map((friend) => {
                const sent = sentTo.includes(friend.user_id);
                return (
                  <div className="dw-invite-friend" key={friend.user_id}>
                    <span className="dw-friend-avatar" aria-hidden="true">{friend.display_name.slice(0, 1).toUpperCase()}</span>
                    <div>
                      <strong>{friend.display_name}</strong>
                      <small>@{friend.handle}</small>
                    </div>
                    <button
                      className="dw-friend-action"
                      type="button"
                      disabled={sent || busy === `send:${friend.user_id}`}
                      onClick={() => void sendTo(friend)}
                    >
                      <Icon name="send" size={14} />
                      {sent ? (zh ? "已发送" : "Sent") : zh ? "发送邀请" : "Send invite"}
                    </button>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      )}

      {notice ? <p className="dw-form-success" role="status">{notice}</p> : null}
      {error ? <p className="dw-form-error" role="alert">{error}</p> : null}
    </Modal>
  );
}
