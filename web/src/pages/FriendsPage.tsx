// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router";
import {
  applyAction,
  errorMessage,
  getLens,
  getObject,
  getWorldStage,
  listFriendRequests,
  listFriends,
  listRecentDirectMessages,
  listMembers,
  markDirectMessagesRead,
  newClientMessageID,
  respondFriendRequest,
  sendDirectMessage,
  type DeriveLens,
  type DirectMessage,
  type Friend,
  type FriendRequest,
  type Member,
  type ObjectView,
  type StageObject
} from "../api";
import AIPanel from "../components/AIPanel";
import { ErrorNotice } from "../errors";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";
import FriendSearchDialog from "../components/v2/FriendSearchDialog";
import Icon from "../components/v2/Icon";
import InviteDialog from "../components/v2/InviteDialog";
import Modal from "../components/v2/Modal";
import { useToast } from "../components/v2/Toast";

function pickCentral(
  objects: StageObject[],
  currentID: string | null
): StageObject | null {
  const current = currentID ? objects.find((object) => object.id === currentID) : null;
  if (current) {
    return current;
  }
  const shared = objects
    .filter((object) => object.member_count > 1)
    .sort((left, right) => right.updated_at.localeCompare(left.updated_at));
  if (shared[0]) {
    return shared[0];
  }
  const writable = objects.filter(
    (object) => object.permissions.includes("contribute") || object.permissions.includes("manage")
  );
  return writable[0] ?? objects[0] ?? null;
}

export default function FriendsPage() {
  const navigate = useNavigate();
  const { t, lang } = useI18n();
  const { status, user } = useAuth();
  const zh = lang === "zh";
  const { push: toast, view: toastView } = useToast();
  const [central, setCentral] = useState<StageObject | null>(null);
  const [centralObject, setCentralObject] = useState<ObjectView | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [friends, setFriends] = useState<Friend[]>([]);
  const [incoming, setIncoming] = useState<FriendRequest[]>([]);
  const [selectedFriend, setSelectedFriend] = useState<Friend | null>(null);
  const [messages, setMessages] = useState<DirectMessage[]>([]);
  const [messageDraft, setMessageDraft] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState("");
  const [notice, setNotice] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const [inviteOpen, setInviteOpen] = useState(false);
  const [contributeOpen, setContributeOpen] = useState(false);
  const [aiOpen, setAIOpen] = useState(false);
  const [lineageOpen, setLineageOpen] = useState(false);
  const [lineage, setLineage] = useState<DeriveLens | null>(null);
  const [contribution, setContribution] = useState("");
  const autoSelected = useRef(false);
  const messageEnd = useRef<HTMLDivElement>(null);

  const timeFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(zh ? "zh-CN" : "en", {
        dateStyle: "medium",
        timeStyle: "short"
      }),
    [zh]
  );

  const load = useCallback(async (signal?: AbortSignal) => {
    setError(null);
    try {
      const [stage, friendList, incomingList] = await Promise.all([
        getWorldStage(signal),
        listFriends(signal),
        listFriendRequests("incoming", signal).catch(() => ({ items: [] as FriendRequest[] }))
      ]);
      if (signal?.aborted) {
        return;
      }
      setFriends(friendList.items);
      setIncoming(incomingList.items);
      setCentral((current) => pickCentral(stage.objects, current?.id ?? null));
      setSelectedFriend((current) => {
        if (current) {
          return friendList.items.find((friend) => friend.user_id === current.user_id) ?? null;
        }
        if (!autoSelected.current && friendList.items.length > 0) {
          autoSelected.current = true;
          return (
            friendList.items.find((friend) => friend.unread > 0) ??
            friendList.items[0] ??
            null
          );
        }
        return null;
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

  useEffect(() => {
    if (status !== "authenticated") {
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [status, load]);

  useEffect(() => {
    if (!central) {
      setCentralObject(null);
      setMembers([]);
      return;
    }
    let cancelled = false;
    Promise.all([
      getObject(central.id),
      listMembers(central.id).catch(() => ({ items: [] as Member[] }))
    ])
      .then(([object, memberPage]) => {
        if (!cancelled) {
          setCentralObject(object);
          setMembers(memberPage.items);
        }
      })
      .catch((failure) => {
        if (!cancelled) {
          setError(failure);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [central?.id, central?.updated_at]);

  const loadMessages = useCallback(async (friend: Friend, signal?: AbortSignal) => {
    try {
      const page = await listRecentDirectMessages(friend.user_id, signal);
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
    if (!selectedFriend || status !== "authenticated") {
      setMessages([]);
      return;
    }
    const controller = new AbortController();
    void loadMessages(selectedFriend, controller.signal);
    void markDirectMessagesRead(selectedFriend.user_id).catch(() => undefined);
    return () => controller.abort();
  }, [selectedFriend, status, loadMessages]);

  useEffect(() => {
    if (status !== "authenticated") {
      return;
    }
    const timer = window.setInterval(() => {
      if (!document.hidden) {
        void load();
        if (selectedFriend) {
          void loadMessages(selectedFriend);
        }
      }
    }, 12000);
    return () => window.clearInterval(timer);
  }, [status, load, loadMessages, selectedFriend]);

  useEffect(() => {
    messageEnd.current?.scrollIntoView({ block: "end" });
  }, [messages.length]);

  async function refreshCentral() {
    if (!central) {
      return;
    }
    try {
      const [object, memberPage] = await Promise.all([
        getObject(central.id),
        listMembers(central.id).catch(() => ({ items: [] as Member[] }))
      ]);
      setCentralObject(object);
      setMembers(memberPage.items);
    } catch {
      // The stage refresh below still updates the surrounding world.
    }
    await load();
  }

  async function respondRequest(request: FriendRequest, accept: boolean) {
    setBusy(`request:${request.id}`);
    setError(null);
    try {
      await respondFriendRequest(request.id, accept);
      setNotice(
        accept
          ? zh
            ? `你和 ${request.display_name} 已成为好友。世界访问仍需单独邀请。`
            : `You and ${request.display_name} are friends now. World access still needs a separate invitation.`
          : zh
            ? "已拒绝好友申请。"
            : "Friend request declined."
      );
      await load();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function sendMessage(event: FormEvent) {
    event.preventDefault();
    if (!selectedFriend || !messageDraft.trim() || busy) {
      return;
    }
    const body = messageDraft;
    setMessageDraft("");
    setBusy("message");
    setError(null);
    try {
      await sendDirectMessage(selectedFriend.user_id, body, newClientMessageID());
      await loadMessages(selectedFriend);
      await load();
    } catch (failure) {
      setError(failure);
      setMessageDraft(body);
    } finally {
      setBusy("");
    }
  }

  async function contribute(event: FormEvent) {
    event.preventDefault();
    if (!centralObject || !contribution.trim() || busy) {
      return;
    }
    setBusy("contribute");
    setError(null);
    try {
      await applyAction(
        centralObject.id,
        "add_element",
        {
          kind: centralObject.state.purpose === "story" ? "scene" : "idea",
          text: contribution.trim()
        },
        centralObject.version
      );
      setContribution("");
      setContributeOpen(false);
      setNotice(zh ? "你的想法已经真实写进共同世界。" : "Your idea was written into the shared world.");
      toast(zh ? "共同世界向前生长了一步。" : "The shared world grew one step.");
      await refreshCentral();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function openLineage() {
    if (!central) {
      return;
    }
    setLineageOpen(true);
    setLineage(null);
    try {
      setLineage(await getLens(central.id));
    } catch (failure) {
      setError(failure);
    }
  }

  async function startChat(userID: string) {
    let friend = friends.find((entry) => entry.user_id === userID);
    if (!friend) {
      try {
        const refreshed = await listFriends();
        setFriends(refreshed.items);
        friend = refreshed.items.find((entry) => entry.user_id === userID);
      } catch {
        friend = undefined;
      }
    }
    if (friend) {
      setSelectedFriend(friend);
    }
  }

  const people = useMemo(() => {
    const result: Array<{ id: string; name: string; self: boolean; friend?: Friend }> = [];
    if (user) {
      result.push({
        id: user.id,
        name: user.display_name,
        self: true,
        friend: friends.find((friend) => friend.user_id === user.id)
      });
    }
    for (const member of members) {
      if (member.user_id === user?.id) {
        continue;
      }
      result.push({
        id: member.user_id,
        name: member.display_name,
        self: false,
        friend: friends.find((friend) => friend.user_id === member.user_id)
      });
    }
    return result.slice(0, 4);
  }, [friends, members, user]);

  if (status === "loading" || loading) {
    return <div className="dw-page dw-page-loading">{zh ? "正在进入共同世界…" : "Entering the shared world…"}</div>;
  }
  if (status === "anonymous") {
    return (
      <div className="dw-page">
        <div className="dw-empty-note">
          <p>{t("auth.required")}</p>
          <Link className="dw-button dw-button-primary" to={`/login?next=${encodeURIComponent("/friends")}`}>
            {t("nav.login")}
          </Link>
        </div>
      </div>
    );
  }

  const canContribute = Boolean(
    central?.permissions.includes("contribute") || central?.permissions.includes("manage")
  );
  const canInvite = Boolean(
    central?.permissions.includes("invite") || central?.permissions.includes("manage")
  );

  return (
    <div className="dw-page dw-together-page" data-testid="friends-page">
      <header className="dw-page-head">
        <p className="dw-eyebrow">
          <span className="dw-eyebrow-line" />
          SHARED POSSIBILITIES
        </p>
        <div className="dw-heading-row">
          <div className="dw-head-left">
            <h1>
              {zh ? (
                <>
                  有些未来，<em>一起创造更精彩。</em>
                </>
              ) : (
                <>
                  Some futures <em>are better made together.</em>
                </>
              )}
            </h1>
            <p className="dw-lead">
              {zh
                ? "你与好友直接交流，也能各自带着 AI，让同一个起点延伸出不同答案。"
                : "Talk directly with friends and bring your own AI. One beginning can grow into different answers."}
            </p>
          </div>
          <div className="dw-head-actions">
            <button className="dw-button dw-button-ghost" type="button" onClick={() => setSearchOpen(true)}>
              <Icon name="search" size={16} />
              {zh ? "查找好友" : "Find a friend"}
            </button>
            <button
              className="dw-button dw-button-primary"
              type="button"
              disabled={!centralObject || !canInvite}
              onClick={() => setInviteOpen(true)}
            >
              <Icon name="plus" size={16} />
              {zh ? "邀请好友" : "Invite a friend"}
            </button>
          </div>
        </div>
      </header>

      {error ? <ErrorNotice error={error} onRetry={() => void load()} /> : null}
      {notice ? <p className="dw-form-success" role="status">{notice}</p> : null}

      <div className="dw-together-grid">
        <section className="dw-together-stage" aria-label={zh ? "共同世界" : "Shared world"}>
          <div className="dw-together-mesh" aria-hidden="true" />
          <svg className="dw-collab-links" viewBox="0 0 800 620" preserveAspectRatio="none" aria-hidden="true">
            <path d="M150 120 Q340 150 390 305" />
            <path d="M650 170 Q460 175 410 300" />
            <path d="M650 500 Q500 400 410 315" />
            <path d="M150 500 Q300 400 380 330" />
          </svg>

          {people.map((person, index) => {
            const position = ["me", "friend-a", "friend-b", "friend-c"][index] ?? "friend-c";
            const content = (
              <>
                <span className="dw-collab-avatar-badge" aria-hidden="true">
                  {person.name.slice(0, 1).toUpperCase()}
                </span>
                <span>{person.self ? (zh ? "你" : "You") : person.name}</span>
              </>
            );
            if (person.friend) {
              return (
                <button
                  key={person.id}
                  type="button"
                  className={`dw-collab-avatar dw-collab-${position}`}
                  onClick={() => setSelectedFriend(person.friend ?? null)}
                  aria-label={zh ? `与 ${person.name} 聊天` : `Chat with ${person.name}`}
                >
                  {content}
                </button>
              );
            }
            return (
              <span key={person.id} className={`dw-collab-avatar dw-collab-${position}`} title={person.name}>
                {content}
              </span>
            );
          })}

          {central ? (
            <div className="dw-collab-center">
              <span className="dw-collab-world-symbol" aria-hidden="true">
                <Icon name="world" size={24} />
              </span>
              <p className="dw-eyebrow">
                {central.member_count > 1
                  ? zh
                    ? "A WORLD YOU CAN GROW TOGETHER"
                    : "A WORLD YOU CAN GROW TOGETHER"
                  : zh
                    ? "A WORLD WAITING FOR ANOTHER VOICE"
                    : "A WORLD WAITING FOR ANOTHER VOICE"}
              </p>
              <h2>{central.title || (zh ? "未命名世界" : "Untitled world")}</h2>
              <p>
                {central.note ||
                  (zh
                    ? "一个真实的共同世界。每个人和 AI 都只能在获得授权的范围内参与。"
                    : "A real shared world. People and AI participate only within granted permission.")}
              </p>
              <div className="dw-collab-center-actions">
                {canContribute ? (
                  <button className="dw-button dw-button-primary" type="button" onClick={() => setContributeOpen(true)}>
                    <Icon name="plus" size={15} />
                    {zh ? "给世界一个新想法" : "Give the world a new idea"}
                  </button>
                ) : (
                  <Link className="dw-button dw-button-ghost" to={`/objects/${central.id}`}>
                    {zh ? "查看这个世界" : "View this world"}
                  </Link>
                )}
              </div>
              <div className="dw-collab-links-row">
                <button className="dw-text-link-button" type="button" onClick={() => void openLineage()}>
                  {zh ? "看看大家的分支" : "Explore the branches"}
                </button>
                <button
                  className="dw-text-link-button"
                  type="button"
                  disabled={!centralObject}
                  onClick={() => setAIOpen(true)}
                >
                  {zh ? "让我的 AI 参与" : "Bring my AI"}
                </button>
                {canInvite ? (
                  <button className="dw-text-link-button" type="button" onClick={() => setInviteOpen(true)}>
                    {zh ? "邀请好友加入" : "Invite a friend"}
                  </button>
                ) : null}
              </div>
              <p className="dw-collab-members-count">
                {zh
                  ? `${central.member_count} 位真实参与者 · ${central.branch_count} 条分支`
                  : `${central.member_count} real participant${central.member_count === 1 ? "" : "s"} · ${central.branch_count} branch${central.branch_count === 1 ? "" : "es"}`}
              </p>
            </div>
          ) : (
            <div className="dw-collab-center dw-collab-center-empty">
              <span className="dw-collab-world-symbol" aria-hidden="true">
                <Icon name="together" size={24} />
              </span>
              <p className="dw-eyebrow">EVERY SHARED WORLD BEGINS SOMEWHERE</p>
              <h2>{zh ? "一起创造，从一个世界开始。" : "Together starts with one world."}</h2>
              <p>
                {zh
                  ? "先创造一个世界，再把单独的世界邀请发给好友。好友身份本身不会获得访问权。"
                  : "Create a world, then send a separate world invitation. Friendship never grants access by itself."}
              </p>
              <div className="dw-collab-center-actions">
                <button
                  className="dw-button dw-button-primary"
                  type="button"
                  onClick={() => {
                    navigate("/");
                    window.setTimeout(() => window.dispatchEvent(new Event("dw:create-world")), 120);
                  }}
                >
                  <Icon name="plus" size={15} />
                  {zh ? "创建一个世界" : "Create a world"}
                </button>
                <button className="dw-button dw-button-ghost" type="button" onClick={() => setSearchOpen(true)}>
                  {zh ? "先找好友" : "Find a friend first"}
                </button>
              </div>
            </div>
          )}

          <div className="dw-collab-action-row">
            <span className="dw-collab-hint">
              <Icon name="shield" size={13} />
              {zh ? "共同贡献真实写入；私人 AI 上下文不共享" : "Contributions are real; private AI context is never shared"}
            </span>
          </div>
        </section>

        <aside className="dw-conversation" aria-label={zh ? "与好友直接交流" : "Talk directly with friends"}>
          <p className="dw-eyebrow">A WORLD, MANY VOICES</p>
          <h2>{selectedFriend ? (zh ? `与 ${selectedFriend.display_name} 对话` : `Talking with ${selectedFriend.display_name}`) : zh ? "围绕世界，直接交流" : "Talk around the world"}</h2>
          <p className="dw-conversation-sub">
            {zh
              ? "真实好友私信。AI 上下文不随私信或共同世界共享。"
              : "Real direct messages between friends. AI context is never shared through messages or the shared world."}
          </p>

          {incoming.length > 0 ? (
            <div className="dw-incoming-requests" aria-labelledby="dw-incoming-heading">
              <h3 id="dw-incoming-heading">
                {zh ? `${incoming.length} 个好友申请等待你回应` : `${incoming.length} friend request${incoming.length === 1 ? "" : "s"} waiting`}
              </h3>
              {incoming.map((request) => (
                <div className="dw-incoming-request" key={request.id}>
                  <span className="dw-friend-chip-avatar" aria-hidden="true">
                    {request.display_name.slice(0, 1).toUpperCase()}
                  </span>
                  <div>
                    <strong>{request.display_name}</strong>
                    <small>@{request.handle}</small>
                  </div>
                  <div className="dw-incoming-actions">
                    <button
                      className="dw-friend-action"
                      type="button"
                      disabled={busy !== ""}
                      onClick={() => void respondRequest(request, true)}
                    >
                      {zh ? "接受" : "Accept"}
                    </button>
                    <button
                      className="dw-friend-action dw-friend-action-quiet"
                      type="button"
                      disabled={busy !== ""}
                      onClick={() => void respondRequest(request, false)}
                    >
                      {zh ? "拒绝" : "Decline"}
                    </button>
                  </div>
                </div>
              ))}
            </div>
          ) : null}

          {selectedFriend ? (
            <>
              <div className="dw-conversation-messages">
                {messages.length === 0 ? (
                  <p className="dw-empty-note">
                    {zh ? "还没有消息。说点什么，开始这段真实的对话。" : "No messages yet. Say something to begin this real conversation."}
                  </p>
                ) : (
                  messages.map((message) => {
                    const mine = message.sender_id === user?.id;
                    return (
                      <div key={message.id} className={`dw-message${mine ? " is-mine" : ""}`}>
                        <span className="dw-message-dot" aria-hidden="true">
                          {mine ? (zh ? "你" : "Y") : selectedFriend.display_name.slice(0, 1).toUpperCase()}
                        </span>
                        <div className="dw-message-body">
                          {message.body}
                          <small>{timeFormatter.format(new Date(message.created_at))}</small>
                        </div>
                      </div>
                    );
                  })
                )}
                <div ref={messageEnd} />
              </div>
              <form className="dw-chat-compose" onSubmit={sendMessage}>
                <input
                  className="dw-input"
                  maxLength={4000}
                  value={messageDraft}
                  onChange={(event) => setMessageDraft(event.target.value)}
                  placeholder={zh ? "直接对好友说点什么…" : "Say something directly to your friend…"}
                  aria-label={zh ? "发给好友的消息" : "Message to friend"}
                />
                <button type="submit" disabled={!messageDraft.trim() || busy !== ""} aria-label={zh ? "发送" : "Send"}>
                  <Icon name="send" size={17} />
                </button>
              </form>
            </>
          ) : (
            <div className="dw-conversation-empty">
              <p>
                {friends.length > 0
                  ? zh
                    ? "选择一位好友，开始真实的私信。"
                    : "Choose a friend to begin a real direct message."
                  : zh
                    ? "还没有好友。查找并添加好友后，你们才能直接交流。"
                    : "No friends yet. Find and add someone before you can talk directly."}
              </p>
              <button className="dw-button dw-button-primary" type="button" onClick={() => setSearchOpen(true)}>
                <Icon name="search" size={15} />
                {friends.length > 0 ? (zh ? "查找更多好友" : "Find more friends") : zh ? "查找好友" : "Find a friend"}
              </button>
            </div>
          )}

          <div className="dw-friends-strip" aria-label={zh ? "好友" : "Friends"}>
            {friends.map((friend) => (
              <button
                key={friend.user_id}
                type="button"
                className={`dw-friend-chip${selectedFriend?.user_id === friend.user_id ? " is-active" : ""}`}
                onClick={() => setSelectedFriend(friend)}
              >
                <span className="dw-friend-chip-avatar" aria-hidden="true">
                  {friend.display_name.slice(0, 1).toUpperCase()}
                </span>
                {friend.display_name}
                {friend.unread > 0 ? <span className="dw-friend-unread">{friend.unread}</span> : null}
              </button>
            ))}
            {friends.length === 0 ? (
              <span className="dw-muted-line">
                {zh ? "好友会出现在这里，并各自保留独立的 AI。" : "Friends appear here, each with a separate AI."}
              </span>
            ) : null}
          </div>
        </aside>
      </div>

      <FriendSearchDialog
        key={searchOpen ? "open" : "closed"}
        open={searchOpen}
        onClose={() => setSearchOpen(false)}
        onRefresh={load}
        onStartChat={(userID) => void startChat(userID)}
      />

      <InviteDialog
        open={inviteOpen}
        object={centralObject}
        friends={friends}
        onClose={() => setInviteOpen(false)}
        onFindFriend={() => {
          setInviteOpen(false);
          setSearchOpen(true);
        }}
      />

      <Modal
        open={contributeOpen}
        onClose={() => setContributeOpen(false)}
        title={zh ? "让你的想法成为世界的一部分" : "Let your idea become part of the world"}
        kicker="CO-CREATE"
        actions={
          <>
            <button className="dw-button dw-button-ghost" type="button" onClick={() => setContributeOpen(false)}>
              {zh ? "取消" : "Cancel"}
            </button>
            <button
              className="dw-button dw-button-primary"
              type="submit"
              form="dw-contribute-form"
              disabled={busy === "contribute" || !contribution.trim()}
            >
              {busy === "contribute" ? (zh ? "正在写入…" : "Writing…") : zh ? "留下我的贡献" : "Leave my contribution"}
            </button>
          </>
        }
      >
        <form id="dw-contribute-form" onSubmit={contribute} className="dw-form">
          <p className="dw-modal-lead">
            {zh
              ? "这条想法会真实写入共同世界并保留版本历史。它会以你的名义出现，其他参与者可以看到。"
              : "This idea is written into the real shared world with version history. It appears under your name and is visible to participants."}
          </p>
          <label className="dw-field-label" htmlFor="dw-contribution">
            {zh ? "你想添加什么新想法？" : "What idea do you want to add?"}
          </label>
          <textarea
            id="dw-contribution"
            className="dw-input dw-textarea"
            rows={4}
            maxLength={2000}
            value={contribution}
            onChange={(event) => setContribution(event.target.value)}
            placeholder={zh ? "比如：让一整座城市在雨停后用声音回答人们…" : "For example: let a whole city answer people with sound after the rain…"}
            autoFocus
          />
          {error ? <p className="dw-form-error" role="alert">{errorMessage(error)}</p> : null}
        </form>
      </Modal>

      <Modal
        open={lineageOpen}
        onClose={() => setLineageOpen(false)}
        title={zh ? "同一个起点，不同的未来" : "One beginning, different futures"}
        kicker="SHARED DERIVATIONS"
        wide
      >
        <p className="dw-modal-lead">
          {zh
            ? "不同参与者可以从共同起点出发，但每一条分支都有自己的所有者、边界与继续发展的路线。"
            : "Participants can start from the same beginning, but each branch has its own owner, boundary, and future."}
        </p>
        {lineage?.children.length ? (
          <ul className="dw-lineage-list">
            {lineage.children.map((child) => (
              <li key={child.object_id}>
                <Icon name="branch" size={17} />
                <div>
                  <strong>{child.title}</strong>
                  <small>
                    {zh ? `v${child.version} · 源自 v${child.source_version}` : `v${child.version} · from v${child.source_version}`}
                  </small>
                </div>
                <Link to={`/objects/${child.object_id}`} onClick={() => setLineageOpen(false)}>
                  {zh ? "打开" : "Open"}
                </Link>
              </li>
            ))}
          </ul>
        ) : (
          <p className="dw-empty-note">
            {zh ? "还没有人从这个世界衍生新分支。你可以成为第一个。" : "No one has derived a branch yet. You can be the first."}
          </p>
        )}
      </Modal>

      <Modal
        open={aiOpen}
        onClose={() => setAIOpen(false)}
        title={zh ? "让你的 Personal AI 参与" : "Bring your Personal AI in"}
        kicker="PERSONAL AI"
        wide
      >
        {centralObject ? (
          <AIPanel
            object={centralObject}
            canContribute={canContribute}
            onChanged={() => {
              void refreshCentral();
            }}
          />
        ) : (
          <p className="dw-empty-note">{zh ? "正在读取世界…" : "Reading the world…"}</p>
        )}
      </Modal>

      {toastView}
    </div>
  );
}
