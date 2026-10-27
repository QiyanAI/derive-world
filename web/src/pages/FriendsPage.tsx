// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Link } from "react-router";
import {
  blockUser,
  listBlocks,
  listDirectMessages,
  listFriendRequests,
  listFriends,
  searchDiscoverableFriends,
  getDiscoveryPreference,
  setDiscoveryPreference,
  markDirectMessagesRead,
  newClientMessageID,
  removeFriend,
  respondFriendRequest,
  sendDirectMessage,
  sendFriendRequest,
  unblockUser,
  type Block,
  type DirectMessage,
  type DiscoveryCandidate,
  type Friend,
  type FriendRequest
} from "../api";
import { ErrorNotice, StatusNotice } from "../errors";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";

export default function FriendsPage() {
  const { t, lang } = useI18n();
  const { status, user } = useAuth();
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
  const [manageOpen, setManageOpen] = useState(false);
  const [discoveryResults, setDiscoveryResults] = useState<DiscoveryCandidate[]>([]);
  const [searching, setSearching] = useState(false);
  const [discoverable, setDiscoverable] = useState(false);
  const [updatingPreference, setUpdatingPreference] = useState(false);

  useEffect(() => {
    if (status !== "authenticated") return;
    const controller = new AbortController();
    void getDiscoveryPreference(controller.signal)
      .then((value) => { if (!controller.signal.aborted) setDiscoverable(value.discoverable); })
      .catch((failure) => { if (!controller.signal.aborted) setError(failure); });
    return () => controller.abort();
  }, [status]);

  useEffect(() => {
    if (!manageOpen || status !== "authenticated" || handleDraft.trim().length < 2) {
      setDiscoveryResults([]);
      setSearching(false);
      return;
    }
    const controller = new AbortController();
    setSearching(true);
    const timer = window.setTimeout(() => {
      void searchDiscoverableFriends(handleDraft.trim(), controller.signal)
        .then((result) => { if (!controller.signal.aborted) setDiscoveryResults(result.items); })
        .catch((failure) => { if (!controller.signal.aborted) setError(failure); })
        .finally(() => { if (!controller.signal.aborted) setSearching(false); });
    }, 400);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [manageOpen, handleDraft, status]);


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
      await sendFriendRequest(handleDraft.trim().replace(/^@/, ""));
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
        <button className="button button-small dw-find-friend-button" type="button" onClick={() => { setManageOpen(true); window.requestAnimationFrame(() => friendHandleRef.current?.focus()); }}>
          {lang === "zh" ? "查找好友" : "Find a friend"}
        </button>
      </div>

      {notice ? <StatusNotice>{notice}</StatusNotice> : null}
      {error ? <ErrorNotice error={error} onRetry={() => void loadSocial()} /> : null}

      <div className="friends-layout dw-together-world">
        <aside className="friends-side dw-people-panel">
          <section className="dw-friend-management" hidden={!manageOpen}><button type="button" className="button button-ghost button-small" onClick={() => setManageOpen(false)}>×</button>
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
            <p className="form-hint">{lang === "zh"
              ? "输入昵称或 @账号查找主动允许被发现的人。也可直接向已知的准确 @账号发送申请。"
              : "Search by name or @handle among people who opted in. You can also request a known exact @handle."}</p>
            <label className="check-row">
              <input type="checkbox" checked={discoverable} disabled={updatingPreference}
                onChange={(event) => {
                  const next = event.target.checked;
                  setUpdatingPreference(true);
                  void setDiscoveryPreference(next)
                    .then((result) => setDiscoverable(result.discoverable))
                    .catch(setError)
                    .finally(() => setUpdatingPreference(false));
                }} />
              <span>{lang === "zh" ? "允许别人按昵称或账号找到我（默认关闭）"
                : "Allow people to find me by name or handle (off by default)"}</span>
            </label>
            {handleDraft.trim().length >= 2 ? (
              <section className="dw-friend-discovery" aria-live="polite">
                <h3>{lang === "zh" ? "查找结果" : "Find people"}</h3>
                {searching ? <p className="muted">{t("common.loading")}</p> : null}
                {!searching && discoveryResults.length === 0 ?
                  <p className="muted">{lang === "zh" ? "没有匹配的公开账号；对方可能未开启可发现。" : "No opt-in matches found."}</p> : null}
                <ul className="friend-list">
                  {discoveryResults.map((candidate) => {
                    const friend = friends.some((item) => item.user_id === candidate.user_id);
                    const pending = incoming.some((item) => item.user_id === candidate.user_id) ||
                      outgoing.some((item) => item.user_id === candidate.user_id);
                    return (
                      <li key={candidate.user_id} className="friend-row">
                        <span className="avatar avatar-small" aria-hidden="true">{candidate.display_name.slice(0, 1)}</span>
                        <span><strong>{candidate.display_name}</strong><small>@{candidate.handle}</small></span>
                        <button type="button" className="button button-small"
                          disabled={busy !== "" || friend || pending}
                          onClick={() => void act("request", async () => {
                            await sendFriendRequest(candidate.handle);
                            setNotice(t("friends.requestSent"));
                          })}>
                          {friend ? (lang === "zh" ? "已是好友" : "Friends") :
                            pending ? (lang === "zh" ? "申请处理中" : "Pending") :
                            (lang === "zh" ? "添加好友" : "Add friend")}
                        </button>
                      </li>
                    );
                  })}
                </ul>
              </section>
            ) : null}
          </section>

          <section className="dw-friend-requests" hidden={incoming.length === 0 && outgoing.length === 0 && !manageOpen}>
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

          <section className="dw-friend-list">
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

          <details className="dw-friend-blocks"><summary>{t("friends.blocks")}</summary>
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
          </details>
        </aside>

        <section className="friends-thread dw-together-stage">
          <div className="dw-together-live-map" data-testid="dw-together-live-map">
            <p className="eyebrow">{lang === "zh" ? "真实连接中的共创世界" : "REAL PEOPLE · SHARED WORLDS"}</p>
            <div className="dw-together-live-scene">
              <div className="dw-together-live-core">
                <span aria-hidden="true">✧</span>
                <h2>{lang === "zh" ? "一起，创造新的可能" : "Create what's next together"}</h2>
                <p>{lang === "zh" ? "好友不会自动获得私人世界访问权限。" : "Friendship never grants access to private worlds."}</p>
                <Link className="button button-small" to="/objects?scope=shared">
                  {lang === "zh" ? "进入共同世界" : "Enter shared worlds"}
                </Link>
              </div>
              <div className="dw-together-live-people">
                <div className="dw-together-person dw-together-me">
                  <span className="avatar" aria-hidden="true">{(user?.display_name || "?").slice(0, 1)}</span>
                  <strong>{lang === "zh" ? "我" : "Me"}</strong>
                </div>
                {friends.slice(0, 8).map((friend) => (
                  <button className="dw-together-person" key={friend.user_id} type="button"
                    aria-pressed={selected?.user_id === friend.user_id}
                    onClick={() => setSelected(friend)}>
                    <span className="avatar" aria-hidden="true">{friend.display_name.slice(0, 1)}</span>
                    <strong>{friend.display_name}</strong>
                    <small>@{friend.handle}</small>
                  </button>
                ))}
              </div>
            </div>
            <p className="form-hint">{friends.length === 0
              ? (lang === "zh" ? "添加真实好友后，他们会出现在这里。"
                : "Your accepted friends will appear here.")
              : (lang === "zh" ? "点选好友直接交流；在共同世界中进行有权限的创造。"
                : "Select a person to message; collaborate only in worlds you may access.")}</p>
          </div>
          {!selected ? (
            <div className="dw-together-empty"><div className="dw-together-orbit" aria-hidden="true"><span>✧</span><span>◎</span><span>✦</span></div><h2>{lang === "zh" ? "一起，让世界有更多可能。" : "Create something that grows together."}</h2><p>{friends.length === 0 ? (lang === "zh" ? "先通过 @账号添加好友，同意后即可交流和共同创造。" : "Add a friend with an exact @handle to talk and create together.") : t("friends.threadEmpty")}</p><button className="button button-small" type="button" onClick={() => { setManageOpen(true); window.requestAnimationFrame(() => friendHandleRef.current?.focus()); }}>{lang === "zh" ? "邀请朋友" : "Invite a friend"}</button></div>
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
