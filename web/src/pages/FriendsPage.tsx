// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Link } from "react-router";
import {
  blockUser,
  listBlocks,
  listDirectMessages,
  listFriendRequests,
  listFriends,
  markDirectMessagesRead,
  newClientMessageID,
  removeFriend,
  respondFriendRequest,
  sendDirectMessage,
  sendFriendRequest,
  unblockUser,
  type Block,
  type DirectMessage,
  type Friend,
  type FriendRequest
} from "../api";
import { ErrorNotice, StatusNotice } from "../errors";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";

export default function FriendsPage() {
  const { t, lang } = useI18n();
  const { status } = useAuth();
  const [friends, setFriends] = useState<Friend[]>([]);
  const [incoming, setIncoming] = useState<FriendRequest[]>([]);
  const [outgoing, setOutgoing] = useState<FriendRequest[]>([]);
  const [blocks, setBlocks] = useState<Block[]>([]);
  const [selected, setSelected] = useState<Friend | null>(null);
  const [messages, setMessages] = useState<DirectMessage[]>([]);
  const [handleDraft, setHandleDraft] = useState("");
  const friendHandleRef = useRef<HTMLInputElement>(null);
  const [messageDraft, setMessageDraft] = useState("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState("");
  const [loading, setLoading] = useState(true);

  const timeFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(lang === "zh" ? "zh-CN" : "en", {
        dateStyle: "medium",
        timeStyle: "short"
      }),
    [lang]
  );

  const loadSocial = useCallback(async (signal?: AbortSignal) => {
    setError(null);
    try {
      const [friendList, incomingList, outgoingList, blockList] = await Promise.all([
        listFriends(signal),
        listFriendRequests("incoming", signal),
        listFriendRequests("outgoing", signal),
        listBlocks(signal)
      ]);
      if (signal?.aborted) {
        return;
      }
      setFriends(friendList.items);
      setIncoming(incomingList.items);
      setOutgoing(outgoingList.items);
      setBlocks(blockList.items);
      setSelected((current) => {
        if (!current) {
          return null;
        }
        const refreshed = friendList.items.find((friend) => friend.user_id === current.user_id);
        return refreshed ?? null;
      });
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

  const loadMessages = useCallback(async (friend: Friend, signal?: AbortSignal) => {
    try {
      const page = await listDirectMessages(friend.user_id, undefined, signal);
      if (!signal?.aborted) {
        setMessages(page.items);
      }
    } catch (failure) {
      if (!signal?.aborted) {
        setError(failure);
      }
    }
  }, []);

  useEffect(() => {
    if (status !== "authenticated") {
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    void loadSocial(controller.signal);
    return () => controller.abort();
  }, [status, loadSocial]);

  useEffect(() => {
    if (!selected || status !== "authenticated") {
      setMessages([]);
      return;
    }
    const controller = new AbortController();
    void loadMessages(selected, controller.signal);
    void markDirectMessagesRead(selected.user_id).catch(() => undefined);
    return () => controller.abort();
  }, [selected, status, loadMessages]);

  useEffect(() => {
    if (status !== "authenticated") {
      return;
    }
    const timer = window.setInterval(() => {
      if (!document.hidden) {
        void loadSocial();
        if (selected) {
          void loadMessages(selected);
        }
      }
    }, 12000);
    return () => window.clearInterval(timer);
  }, [status, loadSocial, loadMessages, selected]);

  async function act(key: string, action: () => Promise<unknown>) {
    setBusy(key);
    setError(null);
    setNotice("");
    try {
      await action();
      await loadSocial();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function submitRequest(event: FormEvent) {
    event.preventDefault();
    if (!handleDraft.trim()) {
      return;
    }
    await act("request", async () => {
      await sendFriendRequest(handleDraft.trim());
      setHandleDraft("");
      setNotice(t("friends.requestSent"));
    });
  }

  async function submitMessage(event: FormEvent) {
    event.preventDefault();
    if (!selected || !messageDraft.trim()) {
      return;
    }
    const body = messageDraft;
    setMessageDraft("");
    await act("message", async () => {
      await sendDirectMessage(selected.user_id, body, newClientMessageID());
      await loadMessages(selected);
      await loadSocial();
    });
  }

  if (status === "loading" || loading) {
    return <p className="page">{t("common.loading")}</p>;
  }
  if (status === "anonymous") {
    return (
      <div className="page">
        <p className="notice">{t("auth.required")}</p>
        <Link className="button" to={`/login?next=${encodeURIComponent("/friends")}`}>
          {t("nav.login")}
        </Link>
      </div>
    );
  }

  return (
    <div className="page page-wide friends-page" data-testid="friends-page">
      <div className="page-heading">
        <p className="eyebrow">{t("friends.eyebrow")}</p>
        <h1>{t("friends.title")}</h1>
        <p className="lead">{t("friends.lead")}</p>
        <button className="button button-small dw-find-friend-button" type="button" onClick={() => { friendHandleRef.current?.scrollIntoView({ block: "center", behavior: "smooth" }); friendHandleRef.current?.focus(); }}>
          {lang === "zh" ? "查找好友" : "Find a friend"}
        </button>
      </div>

      {notice ? <StatusNotice>{notice}</StatusNotice> : null}
      {error ? <ErrorNotice error={error} onRetry={() => void loadSocial()} /> : null}

      <div className="friends-layout">
        <aside className="friends-side">
          <section className="card">
            <h2>{t("friends.add")}</h2><p className="muted">{lang === "zh" ? "输入对方准确的 @账号发送好友申请。添加好友不会自动开放私人世界。" : "Enter an exact @handle to request friendship. Friends do not automatically see private worlds."}</p>
            <form className="inline-field" onSubmit={submitRequest} data-testid="dw-find-friend-form">
              <input
                ref={friendHandleRef}
                aria-label={lang === "zh" ? "好友的准确账号" : "Friend handle"}
                placeholder={t("friends.handlePlaceholder")}
                value={handleDraft}
                maxLength={32}
                onChange={(event) => setHandleDraft(event.target.value)}
              />
              <button className="button button-small" type="submit" disabled={busy !== "" || !handleDraft.trim()}>
                {t("friends.sendRequest")}
              </button>
            </form>
            <p className="form-hint">{t("friends.addHint")}</p>
          </section>

          <section className="card">
            <h2>{t("friends.requests")}</h2>
            {incoming.length === 0 && outgoing.length === 0 ? (
              <p className="muted">{t("friends.noRequests")}</p>
            ) : null}
            <ul className="friend-request-list">
              {incoming.map((request) => (
                <li key={request.id}>
                  <div>
                    <strong>{request.display_name}</strong>
                    <small>@{request.handle} · {t("friends.incoming")}</small>
                  </div>
                  <div className="pulse-actions">
                    <button
                      className="button button-small"
                      type="button"
                      disabled={busy !== ""}
                      onClick={() => void act(`accept-${request.id}`, () => respondFriendRequest(request.id, true))}
                    >
                      {t("friends.accept")}
                    </button>
                    <button
                      className="button button-ghost button-small"
                      type="button"
                      disabled={busy !== ""}
                      onClick={() => void act(`decline-${request.id}`, () => respondFriendRequest(request.id, false))}
                    >
                      {t("friends.decline")}
                    </button>
                  </div>
                </li>
              ))}
              {outgoing.map((request) => (
                <li key={request.id}>
                  <div>
                    <strong>{request.display_name}</strong>
                    <small>@{request.handle} · {t("friends.outgoing")}</small>
                  </div>
                  <span className="muted">{t("friends.pending")}</span>
                </li>
              ))}
            </ul>
          </section>

          <section className="card">
            <h2>{t("friends.friends")}</h2>
            {friends.length === 0 ? <p className="muted">{t("friends.noFriends")}</p> : null}
            <ul className="friend-list">
              {friends.map((friend) => (
                <li key={friend.user_id}>
                  <button
                    type="button"
                    className={selected?.user_id === friend.user_id ? "friend-row friend-row-active" : "friend-row"}
                    onClick={() => setSelected(friend)}
                  >
                    <span className="avatar avatar-small" aria-hidden="true">
                      {friend.display_name.slice(0, 1).toUpperCase()}
                    </span>
                    <span>
                      <strong>{friend.display_name}</strong>
                      <small>@{friend.handle}</small>
                    </span>
                    {friend.unread > 0 ? <span className="badge">{friend.unread}</span> : null}
                  </button>
                </li>
              ))}
            </ul>
          </section>

          <section className="card">
            <h2>{t("friends.blocks")}</h2>
            {blocks.length === 0 ? <p className="muted">{t("friends.noBlocks")}</p> : null}
            <ul className="friend-list">
              {blocks.map((block) => (
                <li key={block.user_id}>
                  <div className="friend-row">
                    <span>
                      <strong>{block.display_name}</strong>
                      <small>@{block.handle}</small>
                    </span>
                    <button
                      className="button button-ghost button-small"
                      type="button"
                      disabled={busy !== ""}
                      onClick={() => void act(`unblock-${block.user_id}`, () => unblockUser(block.user_id))}
                    >
                      {t("friends.unblock")}
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          </section>
        </aside>

        <section className="card friends-thread">
          {!selected ? (
            <div className="thread-empty">
              <h2>{t("friends.threadTitle")}</h2>
              <p className="muted">{t("friends.threadEmpty")}</p>
            </div>
          ) : (
            <>
              <div className="section-heading">
                <div>
                  <h2>{selected.display_name}</h2>
                  <p className="muted">@{selected.handle}</p>
                </div>
                <div className="pulse-actions">
                  <Link className="button button-ghost button-small" to={`/objects?scope=shared`}>
                    {t("friends.sharedWork")}
                  </Link>
                  <button
                    className="button button-ghost button-small"
                    type="button"
                    disabled={busy !== ""}
                    onClick={() => void act("block", () => blockUser(selected.user_id))}
                  >
                    {t("friends.block")}
                  </button>
                  <button
                    className="button button-ghost button-small button-danger"
                    type="button"
                    disabled={busy !== ""}
                    onClick={() => void act("remove", () => removeFriend(selected.user_id))}
                  >
                    {t("friends.remove")}
                  </button>
                </div>
              </div>
              <p className="form-hint">{t("friends.permissionNote")}</p>
              <ol className="message-thread">
                {messages.length === 0 ? <li className="muted">{t("friends.noMessages")}</li> : null}
                {messages.map((message) => (
                  <li
                    key={message.id}
                    className={message.sender_id === selected.user_id ? "message message-in" : "message message-out"}
                  >
                    <p>{message.body}</p>
                    <small>{timeFormatter.format(new Date(message.created_at))}</small>
                  </li>
                ))}
              </ol>
              <form className="inline-field message-compose" onSubmit={submitMessage}>
                <input
                  placeholder={t("friends.messagePlaceholder")}
                  maxLength={4000}
                  value={messageDraft}
                  onChange={(event) => setMessageDraft(event.target.value)}
                />
                <button className="button button-small" type="submit" disabled={busy !== "" || !messageDraft.trim()}>
                  {t("friends.send")}
                </button>
              </form>
            </>
          )}
        </section>
      </div>
    </div>
  );
}
