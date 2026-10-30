// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type FormEvent } from "react";
import {
  errorMessage,
  getDiscoverability,
  listFriendRequests,
  respondFriendRequest,
  searchUsers,
  sendFriendRequest,
  setDiscoverability,
  type FriendRequest,
  type UserSearchResult
} from "../../api";
import { useI18n } from "../../i18n";
import Icon from "./Icon";
import Modal from "./Modal";

export default function FriendSearchDialog({
  open,
  onClose,
  onRefresh,
  onStartChat
}: {
  open: boolean;
  onClose: () => void;
  onRefresh: () => Promise<void> | void;
  onStartChat: (userID: string) => void;
}) {
  const { lang } = useI18n();
  const zh = lang === "zh";
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<UserSearchResult[]>([]);
  const [incoming, setIncoming] = useState<FriendRequest[]>([]);
  const [outgoing, setOutgoing] = useState<FriendRequest[]>([]);
  const [searching, setSearching] = useState(false);
  const [busy, setBusy] = useState("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [discoverable, setDiscoverableState] = useState<boolean | null>(null);
  const [discoverBusy, setDiscoverBusy] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const loadRequests = useCallback(async (signal?: AbortSignal) => {
    try {
      const [incomingList, outgoingList] = await Promise.all([
        listFriendRequests("incoming", signal),
        listFriendRequests("outgoing", signal)
      ]);
      if (!signal?.aborted) {
        setIncoming(incomingList.items);
        setOutgoing(outgoingList.items);
      }
    } catch (failure) {
      if (!signal?.aborted) {
        setError(errorMessage(failure));
      }
    }
  }, []);

  useLayoutEffect(() => {
    if (!open) {
      return;
    }
    const controller = new AbortController();
    void loadRequests(controller.signal);
    getDiscoverability(controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) {
          setDiscoverableState(value.discoverable);
        }
      })
      .catch(() => undefined);
    window.setTimeout(() => inputRef.current?.focus(), 80);
    return () => controller.abort();
  }, [open, loadRequests]);

  useEffect(() => {
    if (!open) {
      return;
    }
    const trimmed = query.trim();
    if (trimmed.replace(/^@/, "").length < 2) {
      setResults([]);
      setSearching(false);
      return;
    }
    const controller = new AbortController();
    setSearching(true);
    const timer = window.setTimeout(() => {
      searchUsers(trimmed, controller.signal)
        .then((page) => {
          if (!controller.signal.aborted) {
            setResults(page.items);
            setError("");
          }
        })
        .catch((failure) => {
          if (!controller.signal.aborted) {
            setError(errorMessage(failure));
          }
        })
        .finally(() => {
          if (!controller.signal.aborted) {
            setSearching(false);
          }
        });
    }, 260);
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [open, query]);

  async function refreshSearch() {
    const trimmed = query.trim();
    if (trimmed.replace(/^@/, "").length >= 2) {
      try {
        const page = await searchUsers(trimmed);
        setResults(page.items);
      } catch (failure) {
        setError(errorMessage(failure));
      }
    }
  }

  async function send(handle: string) {
    setBusy(`send:${handle}`);
    setError("");
    setNotice("");
    try {
      await sendFriendRequest(handle);
      setNotice(zh ? "好友申请已发出，等待对方同意。" : "Friend request sent. Waiting for them to accept.");
      await Promise.all([loadRequests(), refreshSearch()]);
      await onRefresh();
    } catch (failure) {
      setError(errorMessage(failure));
    } finally {
      setBusy("");
    }
  }

  async function accept(request: FriendRequest) {
    setBusy(`accept:${request.id}`);
    setError("");
    setNotice("");
    try {
      await respondFriendRequest(request.id, true);
      setNotice(zh ? `你和 ${request.display_name} 已成为好友。好友仍需单独邀请才能进入世界。` : `You and ${request.display_name} are friends now. A separate invitation is still required to enter a world.`);
      await Promise.all([loadRequests(), refreshSearch()]);
      await onRefresh();
    } catch (failure) {
      setError(errorMessage(failure));
    } finally {
      setBusy("");
    }
  }

  async function toggleDiscoverable() {
    if (discoverable === null) {
      return;
    }
    setDiscoverBusy(true);
    setError("");
    try {
      const saved = await setDiscoverability(!discoverable);
      setDiscoverableState(saved.discoverable);
      setNotice(
        saved.discoverable
          ? zh
            ? "其他已登录用户可以通过昵称或 @账号找到你。"
            : "Other signed-in people can find you by nickname or @handle."
          : zh
            ? "你已隐藏于账号搜索。已知你准确 @账号的人仍可发送好友申请。"
            : "You are hidden from account search. Someone who already knows your exact handle can still send a request."
      );
    } catch (failure) {
      setError(errorMessage(failure));
    } finally {
      setDiscoverBusy(false);
    }
  }

  function submit(event: FormEvent) {
    event.preventDefault();
  }

  const trimmed = query.trim();
  const canSearch = trimmed.replace(/^@/, "").length >= 2;

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={zh ? "找到想一起创造的人" : "Find someone to create with"}
      kicker={zh ? "FRIEND DISCOVERY" : "FRIEND DISCOVERY"}
      wide
      actions={
        <>
          <button className="dw-button dw-button-ghost" type="button" onClick={onClose}>
            {zh ? "完成" : "Done"}
          </button>
        </>
      }
    >
      <p className="dw-modal-lead">
        {zh
          ? "用昵称或 @账号查找已注册用户，发送好友申请。成为好友后仍需单独邀请，才能参与你的私人世界。"
          : "Search registered people by nickname or @handle, then send a friend request. Friendship never grants access to a private world; an invitation is always separate."}
      </p>
      <form onSubmit={submit} className="dw-friend-search-form">
        <label className="dw-field-label" htmlFor="dw-friend-search">
          {zh ? "查找好友" : "Find a friend"}
        </label>
        <div className="dw-search-field">
          <Icon name="search" size={18} />
          <input
            ref={inputRef}
            id="dw-friend-search"
            className="dw-input"
            maxLength={32}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={zh ? "试试：北枝 / @linche / 星海" : "Try a nickname or @handle"}
            autoComplete="off"
            spellCheck={false}
            aria-describedby="dw-friend-search-help"
          />
          {searching ? <span className="dw-search-spinner" aria-hidden="true" /> : null}
        </div>
        <div className="dw-search-meta" id="dw-friend-search-help">
          <span>
            {canSearch
              ? searching
                ? zh
                  ? "正在查找…"
                  : "Searching…"
                : zh
                  ? `${results.length} 个真实账号`
                  : `${results.length} real account${results.length === 1 ? "" : "s"}`
              : zh
                ? "至少输入 2 个字符。这里没有公开的用户目录。"
                : "Type at least 2 characters. There is no public user directory."}
          </span>
          {query ? (
            <button type="button" className="dw-text-button" onClick={() => setQuery("")}>
              {zh ? "清空搜索" : "Clear"}
            </button>
          ) : null}
        </div>
      </form>

      {results.length > 0 ? (
        <div className="dw-friend-results" role="region" aria-label={zh ? "好友搜索结果" : "Friend search results"}>
          {results.map((person) => {
            const request = outgoing.find((item) => item.user_id === person.user_id);
            const incomingRequest = incoming.find((item) => item.user_id === person.user_id);
            return (
              <div className="dw-friend-result" key={person.user_id}>
                <span className="dw-friend-avatar" aria-hidden="true">
                  {person.display_name.slice(0, 1).toUpperCase()}
                </span>
                <div className="dw-friend-result-meta">
                  <strong>
                    {person.display_name}
                    <span>@{person.handle}</span>
                  </strong>
                  <small>
                    {person.relation === "friend"
                      ? zh
                        ? "已是好友 · 仍需单独邀请才能进入世界"
                        : "Friend · a separate invitation is still needed to enter a world"
                      : person.relation === "outgoing"
                        ? zh
                          ? "申请已发出 · 等待对方同意"
                          : "Request sent · waiting for them"
                        : person.relation === "incoming"
                          ? zh
                            ? "对方已向你发出好友申请"
                            : "They already sent you a request"
                          : zh
                            ? "可以发送好友申请"
                            : "You can send a friend request"}
                  </small>
                </div>
                {person.relation === "friend" ? (
                  <button
                    type="button"
                    className="dw-friend-action dw-friend-action-quiet"
                    onClick={() => {
                      onStartChat(person.user_id);
                      onClose();
                    }}
                  >
                    <Icon name="message" size={14} />
                    {zh ? "聊天" : "Chat"}
                  </button>
                ) : person.relation === "incoming" && incomingRequest ? (
                  <button
                    type="button"
                    className="dw-friend-action"
                    disabled={busy !== ""}
                    onClick={() => void accept(incomingRequest)}
                  >
                    {zh ? "接受申请" : "Accept"}
                  </button>
                ) : person.relation === "outgoing" || request ? (
                  <span className="dw-friend-status">{zh ? "等待同意" : "Pending"}</span>
                ) : (
                  <button
                    type="button"
                    className="dw-friend-action"
                    disabled={busy !== ""}
                    onClick={() => void send(person.handle)}
                  >
                    {busy === `send:${person.handle}` ? (zh ? "发送中…" : "Sending…") : zh ? "+ 添加好友" : "+ Add friend"}
                  </button>
                )}
              </div>
            );
          })}
        </div>
      ) : null}

      {incoming.length > 0 ? (
        <section className="dw-friend-requests" aria-labelledby="dw-incoming-title">
          <h3 id="dw-incoming-title">{zh ? "等待你回应的申请" : "Requests waiting for you"}</h3>
          <div className="dw-friend-results">
            {incoming.map((request) => (
              <div className="dw-friend-result" key={request.id}>
                <span className="dw-friend-avatar" aria-hidden="true">
                  {request.display_name.slice(0, 1).toUpperCase()}
                </span>
                <div className="dw-friend-result-meta">
                  <strong>
                    {request.display_name}
                    <span>@{request.handle}</span>
                  </strong>
                  <small>{zh ? "想与你成为好友" : "Wants to be friends"}</small>
                </div>
                <button
                  type="button"
                  className="dw-friend-action"
                  disabled={busy !== ""}
                  onClick={() => void accept(request)}
                >
                  {zh ? "接受" : "Accept"}
                </button>
              </div>
            ))}
          </div>
        </section>
      ) : null}

      <p className="dw-fineprint">
        <Icon name="shield" size={13} />
        {zh
          ? "查找只对已登录用户开放，结果按查询返回且数量受限；不会暴露邮箱，也不会展示完整用户目录。"
          : "Discovery is available only to signed-in people, is query-scoped and capped, never exposes email, and never shows a full directory."}
      </p>

      <div className="dw-discover-toggle">
        <div>
          <strong>{zh ? "允许别人找到我" : "Let others find me"}</strong>
          <small>
            {zh
              ? "关闭后，你不会出现在昵称或 @账号搜索结果中；已知你准确账号的人仍可发送申请。"
              : "When off, you disappear from nickname and @handle search. Someone with your exact handle can still send a request."}
          </small>
        </div>
        <button
          type="button"
          className={`dw-switch${discoverable ? " is-on" : ""}`}
          role="switch"
          aria-checked={discoverable === true}
          aria-label={zh ? "允许别人找到我" : "Let others find me"}
          disabled={discoverable === null || discoverBusy}
          onClick={() => void toggleDiscoverable()}
        >
          <span />
        </button>
      </div>
      {notice ? (
        <p className="dw-form-success" role="status">
          {notice}
        </p>
      ) : null}
      {error ? (
        <p className="dw-form-error" role="alert">
          {error}
        </p>
      ) : null}
    </Modal>
  );
}
