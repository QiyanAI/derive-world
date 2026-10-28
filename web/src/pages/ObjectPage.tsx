// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router";
import {
  APIError,
  deleteObject,
  getLineage,
  getObject,
  listEvents,
  listMembers,
  type EventView,
  type Lineage,
  type Member,
  type ObjectView
} from "../api";
import AIPanel from "../components/AIPanel";
import DiscussionsPanel from "../components/DiscussionsPanel";
import FirstLoopGuide from "../components/FirstLoopGuide";
import LensPanel from "../components/LensPanel";
import ObjectAccessTab from "../components/ObjectAccessTab";
import ObjectHistoryTab from "../components/ObjectHistoryTab";
import ObjectWorkTab from "../components/ObjectWorkTab";
import PublicationPanel from "../components/PublicationPanel";
import SceneWorkTab from "../components/SceneWorkTab";
import SectionNav, { type SectionTab } from "../components/SectionNav";
import { ErrorNotice, StatusNotice } from "../errors";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";

const tabs: SectionTab[] = [
  { id: "work", labelKey: "object.tab.work" },
  { id: "lens", labelKey: "object.tab.lens" },
  { id: "discuss", labelKey: "object.tab.discuss" },
  { id: "publish", labelKey: "object.tab.publish" },
  { id: "ai", labelKey: "object.tab.ai" },
  { id: "access", labelKey: "object.tab.access" },
  { id: "history", labelKey: "object.tab.history" }
];

function normalizeTab(value: string | null): string {
  return tabs.some((tab) => tab.id === value) ? (value as string) : "work";
}

function stateString(object: ObjectView, key: string): string {
  const value = object.state[key];
  return typeof value === "string" ? value : "";
}

function recordedVoteOf(events: EventView[], userID: string | undefined): string {
  if (!userID) {
    return "";
  }
  let vote = "";
  for (const event of events) {
    if (
      event.event_type === "object.state_changed" &&
      event.payload.action === "cast_vote" &&
      event.actor.user_id === userID
    ) {
      const input = event.payload.input;
      if (input && typeof input === "object") {
        const option = (input as Record<string, unknown>).option_id;
        if (typeof option === "string") {
          vote = option;
        }
      }
    }
  }
  return vote;
}

export default function ObjectPage() {
  const { id } = useParams();
  const { t, lang } = useI18n();
  const { status: authStatus, user } = useAuth();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const activeTab = normalizeTab(searchParams.get("tab"));
  const firstUse = searchParams.get("created") === "1" && searchParams.get("first") === "1";
  const [guideDismissed, setGuideDismissed] = useState(false);

  const [object, setObject] = useState<ObjectView | null>(null);
  const [events, setEvents] = useState<EventView[]>([]);
  const [eventCursor, setEventCursor] = useState<string | null>(null);
  const maxSeqRef = useRef(0);
  const [members, setMembers] = useState<Member[]>([]);
  const [lineage, setLineage] = useState<Lineage | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [loadStatus, setLoadStatus] = useState(0);
  const [busy, setBusy] = useState("");
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadMoreError, setLoadMoreError] = useState<unknown>(null);
  const [accessLost, setAccessLost] = useState(false);
  const accessLostRef = useRef(false);
  const hadObjectRef = useRef(false);

  const timeFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(lang === "zh" ? "zh-CN" : "en", {
        dateStyle: "medium",
        timeStyle: "short"
      }),
    [lang]
  );

  const noticeFromQuery = useMemo(() => {
    if (searchParams.get("created") === "1") {
      return t("object.created");
    }
    if (searchParams.get("joined") === "1") {
      return t("object.joined");
    }
    if (searchParams.get("derived") === "1") {
      return t("object.derived");
    }
    return "";
  }, [searchParams, t]);

  const mergeEvents = useCallback((incoming: EventView[]) => {
    if (incoming.length === 0) {
      return;
    }
    setEvents((previous) => {
      const merged = new Map<number, EventView>();
      for (const item of [...previous, ...incoming]) {
        merged.set(item.seq, item);
      }
      const ordered = [...merged.values()].sort((left, right) => left.seq - right.seq);
      maxSeqRef.current = ordered.reduce((max, item) => Math.max(max, item.seq), 0);
      return ordered;
    });
  }, []);

  const refreshEvents = useCallback(async () => {
    if (!id) {
      return;
    }
    let cursor = maxSeqRef.current > 0 ? String(maxSeqRef.current) : "";
    let next: string | null = null;
    let pages = 0;
    do {
      const response = await listEvents(id, cursor || undefined);
      mergeEvents(response.items);
      next = response.next_cursor;
      if (next) {
        cursor = next;
      }
      pages++;
    } while (next && pages < 10);
    setEventCursor(next);
  }, [id, mergeEvents]);

  const refreshSideData = useCallback(async () => {
    if (!id) {
      return;
    }
    const [freshMembers, freshLineage] = await Promise.all([
      listMembers(id).catch(() => null),
      getLineage(id).catch(() => null)
    ]);
    if (freshMembers) {
      setMembers(freshMembers.items);
    }
    setLineage(freshLineage);
  }, [id]);

  const load = useCallback(
    async (signal?: AbortSignal, showLoading = true) => {
      if (!id) {
        return;
      }
      if (showLoading) {
        setLoading(true);
      }
      setError(null);
      try {
        const [loadedObject, loadedEvents, loadedMembers, loadedLineage] = await Promise.all([
          getObject(id, signal),
          listEvents(id, undefined, signal),
          listMembers(id, signal),
          getLineage(id, signal).catch(() => null)
        ]);
        if (signal?.aborted) {
          return;
        }
        setObject(loadedObject);
        setEvents(loadedEvents.items);
        maxSeqRef.current = loadedEvents.items.reduce((max, item) => Math.max(max, item.seq), 0);
        setEventCursor(loadedEvents.next_cursor);
        setMembers(loadedMembers.items);
        setLineage(loadedLineage);
      } catch (failure) {
        if (signal?.aborted) {
          return;
        }
        setLoadStatus(failure instanceof APIError ? failure.status : 0);
        setError(failure);
        if (
          hadObjectRef.current &&
          failure instanceof APIError &&
          (failure.status === 401 || failure.status === 403 || failure.status === 404)
        ) {
          accessLostRef.current = true;
          setAccessLost(true);
        }
      } finally {
        if (!signal?.aborted && showLoading) {
          setLoading(false);
        }
      }
    },
    [id]
  );

  useEffect(() => {
    if (!id || authStatus !== "authenticated") {
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [id, authStatus, load]);

  useEffect(() => {
    if (!id || authStatus !== "authenticated") {
      return;
    }
    let cancelled = false;
    const tick = async () => {
      if (document.hidden || accessLostRef.current) {
        return;
      }
      try {
        const fresh = await getObject(id);
        if (cancelled) {
          return;
        }
        setObject((previous) => (previous && previous.version === fresh.version ? previous : fresh));
        await refreshEvents();
        await refreshSideData();
      } catch (failure) {
        if (failure instanceof APIError && (failure.status === 401 || failure.status === 403 || failure.status === 404)) {
          accessLostRef.current = true;
          setAccessLost(true);
          return;
        }
        // Transient network/5xx failures keep the last authoritative snapshot;
        // the next explicit action surfaces the error.
      }
    };
    const timer = window.setInterval(() => void tick(), 10000);
    const onVisibility = () => {
      if (!document.hidden) {
        void tick();
      }
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [id, authStatus, refreshEvents, refreshSideData]);

  const handleUpdated = useCallback(
    async (updated: ObjectView) => {
      setObject(updated);
      await refreshEvents();
      await refreshSideData();
    },
    [refreshEvents, refreshSideData]
  );

  const handleReload = useCallback(async () => {
    await load(undefined, false);
  }, [load]);

  async function loadMoreEvents() {
    if (!id || !eventCursor) {
      return;
    }
    setLoadingMore(true);
    setLoadMoreError(null);
    try {
      const response = await listEvents(id, eventCursor);
      mergeEvents(response.items);
      setEventCursor(response.next_cursor);
    } catch (failure) {
      setLoadMoreError(failure);
    } finally {
      setLoadingMore(false);
    }
  }

  async function removeObject() {
    if (!object || !window.confirm(t("object.deleteConfirm"))) {
      return;
    }
    setBusy("delete");
    setError(null);
    try {
      await deleteObject(object.id);
      navigate("/objects");
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  if (authStatus === "loading" || loading) {
    return <p className="page">{t("common.loading")}</p>;
  }
  if (accessLost) {
    return (
      <div className="page">
        <p className="eyebrow">404</p>
        <h1>{t("object.accessLost")}</h1>
        <p className="lead">{t("object.accessLostBody")}</p>
        <Link className="button" to="/objects">
          {t("common.backToObjects")}
        </Link>
      </div>
    );
  }
  if (authStatus === "anonymous") {
    return (
      <div className="page">
        <p className="notice">{t("object.needSignIn")}</p>
        <Link className="button" to={`/login?next=${encodeURIComponent(`/objects/${id ?? ""}`)}`}>
          {t("nav.login")}
        </Link>
      </div>
    );
  }
  if (!object) {
    if (loadStatus === 404) {
      return (
        <div className="page">
          <p className="eyebrow">404</p>
          <h1>{t("object.noAccess")}</h1>
          <p className="lead">{t("object.noAccessBody")}</p>
          <Link className="button" to="/objects">
            {t("common.backToObjects")}
          </Link>
        </div>
      );
    }
    return (
      <div className="page">
        <h1>{t("object.loadFailed")}</h1>
        {error ? <ErrorNotice error={error} onRetry={() => void load()} /> : null}
        <Link className="button" to="/objects">
          {t("common.backToObjects")}
        </Link>
      </div>
    );
  }

  const canContribute = object.permissions.includes("contribute");
  const canManage = object.permissions.includes("manage");
  const canDerive = object.permissions.includes("derive");
  const canInvite = object.permissions.includes("invite");
  const recordedVote = recordedVoteOf(events, user?.id);
  const parentAncestor = lineage?.ancestors.find((ancestor) => !ancestor.redacted);
  const objectTypeLabel =
    object.object_type === "living_scene"
      ? t("object.create.scene")
      : object.object_type === "interactive"
        ? t("object.create.interactive")
        : t("object.create.collaborative");

  return (
    <div className="page page-wide object-page">
      <div className="object-breadcrumb">
        <Link to="/objects">{t("objects.title")}</Link>
        <span aria-hidden="true">/</span>
        <span>{stateString(object, "title")}</span>
      </div>

      <div className="page-heading with-actions object-heading">
        <div>
          <p className="eyebrow">
            <span className={`template-badge template-${object.object_type}`}>
              {objectTypeLabel}
            </span>
          </p>
          <h1>{stateString(object, "title")}</h1>
          <p className="object-meta-line">
            <span className="version-badge">{t("workspace.version", { version: object.version })}</span>
            <span>{t("workspace.role", { role: t(`role.${object.role}`) })}</span>
            <span>{t("object.membersCount", { count: members.length })}</span>
            <span>{t("object.lastUpdated", { time: timeFormatter.format(new Date(object.updated_at)) })}</span>
          </p>
        </div>
        <div className="heading-actions">
          <button
            className="button button-ghost button-small"
            type="button"
            data-testid="object-refresh"
            onClick={() => void load(undefined, false)}
          >
            {t("object.refresh")}
          </button>
          {canDerive ? (
            <Link className="button button-small" to={`/objects/${object.id}/derive`} data-testid="derive-link">
              {t("workspace.derive")}
            </Link>
          ) : null}
          {canInvite || canManage ? (
            <Link className="button button-small button-ghost" to={`/objects/${object.id}?tab=access`}>
              {t("workspace.invitations")}
            </Link>
          ) : null}
          {canManage ? (
            <button
              type="button"
              className="button button-small button-danger"
              onClick={() => void removeObject()}
              disabled={busy === "delete"}
            >
              {t("object.delete")}
            </button>
          ) : null}
        </div>
      </div>

      {noticeFromQuery ? <StatusNotice>{noticeFromQuery}</StatusNotice> : null}
      {error && object ? <ErrorNotice error={error} compact onRetry={() => void load()} /> : null}

      <SectionNav objectID={object.id} tabs={tabs} active={activeTab} />

      {firstUse && !guideDismissed ? (
        <FirstLoopGuide
          object={object}
          hasBranch={(lineage?.descendants.length ?? 0) > 0}
          canDerive={canDerive}
          onDismiss={() => setGuideDismissed(true)}
        />
      ) : null}

      <div className="object-layout">
        <div className="object-content">
          {activeTab === "work" ? (
            object.object_type === "open_world" ? (
              <section className="card object-panel" data-testid="open-world-object-panel">
                <p className="eyebrow">{t("openWorld.kicker")}</p>
                <h2>{String(object.state.title ?? t("common.untitled"))}</h2>
                <p className="muted">{String(object.state.premise ?? t("openWorld.premiseEmpty"))}</p>
                <p className="muted">
                  {t("openWorld.summary", {
                    kinds: Array.isArray(object.state.kinds) ? object.state.kinds.length : 0,
                    nodes: Array.isArray(object.state.nodes) ? object.state.nodes.length : 0,
                    rules: Array.isArray(object.state.rules) ? object.state.rules.length : 0
                  })}
                </p>
                <Link className="button" to={`/worlds/${object.id}`}>
                  {t("openWorld.enter")}
                </Link>
              </section>
            ) : object.object_type === "living_scene" ? (
              <SceneWorkTab
                object={object}
                canContribute={canContribute}
                canManage={canManage}
                onUpdated={handleUpdated}
                onReload={handleReload}
              />
            ) : (
              <ObjectWorkTab
                object={object}
                canContribute={canContribute}
                canManage={canManage}
                recordedVote={recordedVote}
                onUpdated={handleUpdated}
                onReload={handleReload}
              />
            )
          ) : null}
          {activeTab === "lens" ? <LensPanel objectID={object.id} /> : null}
          {activeTab === "discuss" ? <DiscussionsPanel objectID={object.id} /> : null}
          {activeTab === "publish" ? (
            <PublicationPanel objectID={object.id} currentVersion={object.version} canManage={canManage} />
          ) : null}
          {activeTab === "ai" ? <AIPanel object={object} canContribute={canContribute} onChanged={() => void handleReload()} /> : null}
          {activeTab === "access" ? (
            <ObjectAccessTab
              object={object}
              members={members}
              canManage={canManage}
              canInvite={canInvite}
              onUpdated={handleUpdated}
              onReload={handleReload}
            />
          ) : null}
          {activeTab === "history" ? (
            <ObjectHistoryTab
              object={object}
              events={events}
              eventCursor={eventCursor}
              lineage={lineage}
              currentUserId={user?.id}
              canDerive={canDerive}
              loadingMore={loadingMore}
              loadMoreError={loadMoreError}
              onLoadMore={loadMoreEvents}
            />
          ) : null}
        </div>

        <aside className="object-side">
          <section className="card">
            <h2>{t("object.atAGlance")}</h2>
            <dl className="detail-list">
              <div>
                <dt>{t("object.type")}</dt>
                <dd>{objectTypeLabel}</dd>
              </div>
              <div>
                <dt>{t("object.version")}</dt>
                <dd className="mono">{t("workspace.version", { version: object.version })}</dd>
              </div>
              <div>
                <dt>{t("object.createdAt")}</dt>
                <dd>{timeFormatter.format(new Date(object.created_at))}</dd>
              </div>
              <div>
                <dt>{t("object.updatedAt")}</dt>
                <dd>{timeFormatter.format(new Date(object.updated_at))}</dd>
              </div>
            </dl>
          </section>

          <section className="card">
            <div className="section-heading">
              <h2>{t("workspace.members")}</h2>
              <Link className="text-link" to={`/objects/${object.id}?tab=access`}>
                {t("common.view")}
              </Link>
            </div>
            <ul className="member-list member-list-compact">
              {members.slice(0, 5).map((member) => (
                <li key={member.user_id}>
                  <span className="avatar avatar-small" aria-hidden="true">
                    {member.display_name.slice(0, 1).toUpperCase()}
                  </span>
                  <div>
                    <strong>{member.display_name}</strong>
                    <small>{t(`role.${member.role}`)}</small>
                  </div>
                </li>
              ))}
            </ul>
          </section>

          <section className="card">
            <h2>{t("lineage.title")}</h2>
            {object.lineage ? (
              <p>
                {object.lineage.public && object.lineage.publication_title
                  ? t("object.derivedFromPublic", {
                      title: object.lineage.publication_title,
                      version: object.lineage.parent.version
                    })
                  : t("object.derivedFrom", {
                      title: parentAncestor?.title || t("common.unknown"),
                      version: object.lineage.parent.version
                    })}
              </p>
            ) : (
              <p className="muted">{t("lineage.none")}</p>
            )}
            <p className="muted">
              {t("object.children")}: {lineage ? lineage.descendants.length : 0}
            </p>
            <Link className="text-link" to={`/objects/${object.id}/lineage`}>
              {t("object.viewLineage")}
            </Link>
          </section>

          <section className="card object-side-note">
            <h2>{t("object.sharedContext")}</h2>
            <p className="muted">{t("object.personalContextNote")}</p>
          </section>
        </aside>
      </div>
    </div>
  );
}
