// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import {
  errorMessage,
  getObject,
  getPulse,
  getWorldLayout,
  getWorldStage,
  listFriends,
  listPublications,
  respondPublicationConsent,
  saveWorldLayout,
  type Friend,
  type LivingPulse,
  type ObjectView,
  type Publication,
  type PulseItem,
  type WorldStage
} from "../api";
import { ErrorNotice } from "../errors";
import { actionKey, eventKey } from "../humanize";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";
import StageInspector, { type StageInspectorTab } from "../components/StageInspector";
import CreateWorldDialog from "../components/v2/CreateWorldDialog";
import Icon from "../components/v2/Icon";
import InviteDialog from "../components/v2/InviteDialog";
import Modal from "../components/v2/Modal";
import PrivacyDialog from "../components/v2/PrivacyDialog";
import { useToast } from "../components/v2/Toast";
import WorldCanvas, { type WorldPosition } from "../components/v2/WorldCanvas";

type PrivacyStatus = "private" | "pending" | "public" | "derivable";

function privacyFrom(publications: Publication[]): PrivacyStatus {
  const active = publications.find((publication) => publication.status === "active");
  if (active) {
    return active.derive_allowed ? "derivable" : "public";
  }
  if (publications.some((publication) => publication.status === "pending_consent")) {
    return "pending";
  }
  return "private";
}

export default function WorldStagePage() {
  const { t, lang } = useI18n();
  const { user } = useAuth();
  const navigate = useNavigate();
  const zh = lang === "zh";
  const { push: toast, view: toastView } = useToast();
  const [stage, setStage] = useState<WorldStage | null>(null);
  const [positions, setPositions] = useState<Record<string, WorldPosition>>({});
  const [pulse, setPulse] = useState<LivingPulse | null>(null);
  const [friends, setFriends] = useState<Friend[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [selected, setSelected] = useState<ObjectView | null>(null);
  const [selectedLoading, setSelectedLoading] = useState(false);
  const [selectedError, setSelectedError] = useState<unknown>(null);
  const [inspectorOpen, setInspectorOpen] = useState(false);
  const [inspectorTab, setInspectorTab] = useState<StageInspectorTab>("work");
  const [privacyStatus, setPrivacyStatus] = useState<PrivacyStatus>("private");
  const [createOpen, setCreateOpen] = useState(false);
  const [privacyOpen, setPrivacyOpen] = useState(false);
  const [inviteOpen, setInviteOpen] = useState(false);
  const [pulseOpen, setPulseOpen] = useState(false);
  const [consentBusy, setConsentBusy] = useState("");
  const [focusId, setFocusId] = useState<string | null>(null);
  const selectRequest = useRef(0);
  const branchPoint = useRef<WorldPosition | null>(null);

  const load = useCallback(async (signal?: AbortSignal) => {
    setError(null);
    try {
      const [stageData, layoutData, pulseData, friendData] = await Promise.all([
        getWorldStage(signal),
        getWorldLayout(signal).catch(() => null),
        getPulse(signal).catch(() => null),
        listFriends(signal).catch(() => ({ items: [] as Friend[] }))
      ]);
      if (signal?.aborted) {
        return;
      }
      setStage(stageData);
      if (layoutData) {
        setPositions(
          Object.fromEntries(layoutData.items.map((item) => [item.object_id, { x: item.x, y: item.y }]))
        );
      }
      if (pulseData) {
        setPulse(pulseData);
      }
      setFriends(friendData.items);
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
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  useEffect(() => {
    const onCreate = () => setCreateOpen(true);
    window.addEventListener("dw:create-world", onCreate);
    return () => window.removeEventListener("dw:create-world", onCreate);
  }, []);

  useEffect(() => {
    let source: EventSource | null = null;
    let fallback: number | null = null;
    const startPolling = () => {
      if (fallback !== null) {
        return;
      }
      fallback = window.setInterval(() => {
        if (!document.hidden) {
          void load();
        }
      }, 15000);
    };
    try {
      source = new EventSource("/api/v1/stream");
      source.addEventListener("refresh", () => void load());
      source.addEventListener("reconnect", () => {
        source?.close();
        startPolling();
      });
      source.onerror = () => {
        source?.close();
        source = null;
        startPolling();
      };
    } catch {
      startPolling();
    }
    const onVisible = () => {
      if (!document.hidden) {
        void load();
      }
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      source?.close();
      if (fallback !== null) {
        window.clearInterval(fallback);
      }
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [load]);

  const loadSelected = useCallback(async (objectID: string) => {
    const request = selectRequest.current + 1;
    selectRequest.current = request;
    setSelectedLoading(true);
    setSelectedError(null);
    try {
      const [object, publications] = await Promise.all([
        getObject(objectID),
        listPublications(objectID).catch(() => ({ items: [] as Publication[] }))
      ]);
      if (selectRequest.current === request) {
        setSelected(object);
        setSelectedId(objectID);
        setPrivacyStatus(privacyFrom(publications.items));
      }
    } catch (failure) {
      if (selectRequest.current === request) {
        setSelectedError(failure);
        setSelectedId(objectID);
      }
    } finally {
      if (selectRequest.current === request) {
        setSelectedLoading(false);
      }
    }
  }, []);

  const openInspector = useCallback(
    (objectID: string, tab: StageInspectorTab) => {
      setInspectorOpen(true);
      setInspectorTab(tab);
      setSelectedId(objectID);
      void loadSelected(objectID);
    },
    [loadSelected]
  );

  useEffect(() => {
    let focus = "";
    try {
      focus = window.sessionStorage.getItem("dw:focus-object") ?? "";
      if (focus) {
        window.sessionStorage.removeItem("dw:focus-object");
      }
    } catch {
      focus = "";
    }
    if (!focus) {
      return;
    }
    const timer = window.setTimeout(() => {
      setFocusId(focus);
      openInspector(focus, "work");
    }, 250);
    return () => window.clearTimeout(timer);
  }, [openInspector]);

  const closeInspector = useCallback(() => {
    const previousID = selectedId;
    selectRequest.current += 1;
    setInspectorOpen(false);
    setSelected(null);
    setSelectedId(null);
    if (previousID) {
      window.setTimeout(() => {
        document.querySelector<HTMLElement>(`[data-stage-object="${previousID}"]`)?.focus();
      }, 50);
    }
  }, [selectedId]);

  const handleObjectUpdated = useCallback(
    async (updated: ObjectView) => {
      setSelected(updated);
      try {
        const publications = await listPublications(updated.id);
        setPrivacyStatus(privacyFrom(publications.items));
      } catch {
        // The object update succeeded even if publication status is unavailable.
      }
      await load();
    },
    [load]
  );

  const handleObjectReload = useCallback(async () => {
    if (selectedId) {
      await loadSelected(selectedId);
    }
  }, [loadSelected, selectedId]);

  const handleDerived = useCallback(
    (child: ObjectView) => {
      const at = branchPoint.current;
      branchPoint.current = null;
      setSelected(child);
      setSelectedId(child.id);
      setInspectorTab("work");
      setInspectorOpen(true);
      setFocusId(child.id);
      setPositions((current) => (at ? { ...current, [child.id]: at } : current));
      toast(zh ? "新分支已经出现；原来的世界仍然完整。" : "A new branch exists; the original world is intact.");
      if (at) {
        void saveWorldLayout(child.id, at.x, at.y)
          .catch((failure) => {
            toast(errorMessage(failure), "error");
          })
          .finally(() => {
            void load();
          });
      } else {
        void load();
      }
    },
    [load, toast, zh]
  );

  useEffect(() => {
    if (!selectedId || selectedLoading || !selected || !stage) {
      return;
    }
    const stageObject = stage.objects.find((object) => object.id === selectedId);
    if (stageObject && stageObject.version !== selected.version) {
      void loadSelected(selectedId);
    }
  }, [stage, selectedId, selected, selectedLoading, loadSelected]);

  const sourceTitle = useMemo(() => {
    if (!selected) {
      return "";
    }
    if (selected.lineage?.publication_title) {
      return selected.lineage.publication_title;
    }
    const parentID = selected.lineage?.parent.object_id;
    if (!parentID) {
      return "";
    }
    return stage?.objects.find((object) => object.id === parentID)?.title ?? "";
  }, [selected, stage]);

  const privacyLabel = useMemo(() => {
    if (!selected) {
      return zh ? "默认私人" : "Private by default";
    }
    switch (privacyStatus) {
      case "derivable":
        return zh ? "公开 · 可衍生" : "Public · derivable";
      case "public":
        return zh ? "已公开" : "Published";
      case "pending":
        return zh ? "公开待确认" : "Publication pending";
      default:
        return zh ? "仅自己可见" : "Only me";
    }
  }, [privacyStatus, selected, zh]);

  const pulseItems = pulse?.items ?? [];
  const latestPulse = pulseItems[0] ?? null;

  function pulseLine(item: PulseItem): string {
    const label = item.action ? t(actionKey(item.action)) : t(eventKey(item.event_type ?? ""));
    return `${item.title || (zh ? "未命名世界" : "Untitled world")} · ${label}${
      item.actor_name ? ` · ${item.actor_name}` : ""
    }`;
  }

  function pulseIcon(item: PulseItem): "branch" | "check" | "spark" | "clock" {
    if (item.event_type === "object.derived") {
      return "branch";
    }
    if (item.action === "update_element" || item.action === "toggle_checklist_item") {
      return "check";
    }
    if (item.event_type?.startsWith("ai.")) {
      return "spark";
    }
    return "clock";
  }

  async function respondConsent(id: string, approve: boolean) {
    setConsentBusy(`${id}:${approve}`);
    try {
      await respondPublicationConsent(id, approve);
      toast(
        approve
          ? zh
            ? "你已同意这个公开版本。若所有共同创作者都同意，它会出现在探索世界。"
            : "You approved this public version. It appears in Explore once every contributor agrees."
          : zh
            ? "你拒绝了这个公开版本。它不会公开。"
            : "You declined this public version. It will not become public.",
        "ok"
      );
      await load();
      if (selectedId) {
        await loadSelected(selectedId);
      }
    } catch (failure) {
      toast(errorMessage(failure), "error");
    } finally {
      setConsentBusy("");
    }
  }

  function onPositionChange(id: string, position: WorldPosition) {
    setPositions((current) => ({ ...current, [id]: position }));
    void saveWorldLayout(id, position.x, position.y).catch((failure) => {
      toast(errorMessage(failure), "error");
    });
  }

  function createWorldButton() {
    setCreateOpen(true);
  }

  const emptyState = (
    <div className="dw-stage-onboarding" data-testid="world-onboarding">
      <p className="dw-eyebrow">
        <span className="dw-eyebrow-line" />
        {zh ? "A DIFFERENT BEGINNING" : "A DIFFERENT BEGINNING"}
      </p>
      <h2>{zh ? "不必先学会操作软件。" : "You don't need to learn the software first."}</h2>
      <p>
        {zh
          ? "试着给一个念头一个可以继续生长的开始。你可以直接改变它，也可以从旁边拉出一条新的未来。"
          : "Give one idea a beginning that can keep growing. Change it directly, or pull out a different future."}
      </p>
      <div className="dw-stage-onboarding-actions">
        <button className="dw-button dw-button-primary" type="button" onClick={createWorldButton}>
          <Icon name="plus" size={16} />
          {zh ? "创造一个世界" : "Create a world"}
        </button>
        <Link className="dw-button dw-button-ghost" to="/explore">
          {zh ? "先看看公开世界" : "Explore public worlds first"}
        </Link>
      </div>
    </div>
  );

  return (
    <div className="dw-page dw-world-page" data-testid="world-page">
      <header className="dw-page-head">
        <p className="dw-eyebrow">
          <span className="dw-eyebrow-line" />
          YOUR LIVING WORLD
        </p>
        <div className="dw-heading-row">
          <div className="dw-head-left">
            <h1>
              {zh ? (
                <>
                  让一个念头，<em>长出自己的未来。</em>
                </>
              ) : (
                <>
                  Let one idea <em>grow its own future.</em>
                </>
              )}
            </h1>
            <p className="dw-lead">
              {zh
                ? "触碰一个对象，改变它。拉出一条分支，看看不同选择会走向哪里。"
                : "Touch a creation to change it. Pull out a branch and see where a different choice leads."}
            </p>
            <p className="dw-world-hello">
              {zh ? `属于 ${user?.display_name ?? ""} 的持续世界` : `${user?.display_name ?? ""}'s living world`}
            </p>
          </div>
          <div className="dw-head-actions">
            <button className="dw-privacy-chip" type="button" onClick={() => setPrivacyOpen(true)}>
              <Icon name={privacyStatus === "private" || privacyStatus === "pending" ? "lock" : "globe"} size={15} />
              <span>{privacyLabel}</span>
            </button>
            {stage && stage.objects.length > 0 ? (
              <button className="dw-button dw-button-primary" type="button" onClick={createWorldButton}>
                <Icon name="plus" size={16} />
                {zh ? "创造一个世界" : "Create a world"}
              </button>
            ) : null}
          </div>
        </div>
      </header>

      {error ? <ErrorNotice error={error} onRetry={() => void load()} /> : null}

      <WorldCanvas
        stage={stage}
        layout={positions}
        selectedId={inspectorOpen ? selectedId : null}
        focusId={focusId}
        onSelect={(id) => openInspector(id, "work")}
        onBranch={(id, at) => {
          branchPoint.current = at;
          openInspector(id, "branch");
        }}
        onPositionChange={onPositionChange}
        onBackgroundClick={() => {
          if (inspectorOpen) {
            closeInspector();
          }
        }}
        emptyState={loading ? null : emptyState}
        pulse={
          <button className="dw-pulse-mini" type="button" onClick={() => setPulseOpen(true)}>
            <Icon name="spark" size={16} />
            <span>
              <strong>WORLD PULSE</strong>
              <small>
                {latestPulse
                  ? zh
                    ? `最近：${pulseLine(latestPulse)}`
                    : `Latest: ${pulseLine(latestPulse)}`
                  : zh
                    ? "这里的每次改变，都会留下痕迹。"
                    : "Every real change leaves a trace here."}
              </small>
            </span>
          </button>
        }
        aiOrb={
          <button
            className="dw-ai-orb"
            type="button"
            aria-label={zh ? "向 Personal AI 寻求创作建议" : "Ask your Personal AI for a direction"}
            onClick={() => {
              if (selectedId) {
                openInspector(selectedId, "ai");
              } else {
                window.dispatchEvent(new Event("dw:open-general-ai"));
              }
            }}
          >
            <Icon name="spark" size={23} />
            <small>{zh ? "AI 在此" : "AI here"}</small>
          </button>
        }
        inspector={
          inspectorOpen ? (
            <StageInspector
              object={selected}
              tab={inspectorTab}
              loading={selectedLoading}
              error={selectedError}
              sourceTitle={sourceTitle}
              onTab={setInspectorTab}
              onClose={closeInspector}
              onUpdated={handleObjectUpdated}
              onReload={handleObjectReload}
              onRefresh={() => load()}
              onDerived={handleDerived}
            />
          ) : undefined
        }
      />

      <div className="dw-activity-strip" aria-label={zh ? "世界动态" : "World activity"}>
        <span className="dw-activity-label">{zh ? "世界正在继续" : "The world continues"}</span>
        {pulseItems.slice(0, 5).map((item, index) => (
          <button
            key={`${item.object_id}-${item.occurred_at}-${index}`}
            className="dw-activity-chip"
            type="button"
            onClick={() => setPulseOpen(true)}
          >
            <Icon name={pulseIcon(item)} size={13} />
            <span>{pulseLine(item)}</span>
          </button>
        ))}
        {pulseItems.length === 0 ? (
          <span className="dw-activity-empty">
            {zh ? "一次真实的改变，就会让这里开始生长。" : "One real change will make this place begin to grow."}
          </span>
        ) : null}
      </div>

      <p className="dw-world-note">
        <Icon name="shield" size={13} />
        {zh
          ? "私人是默认。公开或邀请必须是你的主动选择；好友身份本身不会授予私人世界访问权。"
          : "Private is the default. Publishing or inviting is always a separate choice; friendship itself never grants access to a private world."}
      </p>

      <CreateWorldDialog
        key={createOpen ? "open" : "closed"}
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={(object) => {
          toast(zh ? "你的新世界开始生长了。" : "Your new world has begun to grow.");
          if (object.object_type === "open_world") {
            navigate(`/worlds/${object.id}`);
            return;
          }
          setFocusId(object.id);
          setSelected(object);
          setSelectedId(object.id);
          setInspectorOpen(false);
          void load().then(() => openInspector(object.id, "work"));
        }}
        onAskAI={(prompt) => {
          setCreateOpen(false);
          window.dispatchEvent(
            new CustomEvent("dw:ask-personal-ai", { detail: { prompt } })
          );
        }}
      />

      <PrivacyDialog
        open={privacyOpen}
        object={selected}
        onClose={() => setPrivacyOpen(false)}
        onChanged={async () => {
          await load();
          if (selectedId) {
            await loadSelected(selectedId);
          }
        }}
        onInvite={() => {
          if (!selected) {
            toast(zh ? "先选择一个世界，再邀请好友。" : "Select a world before inviting someone.");
            return;
          }
          setInviteOpen(true);
        }}
      />

      <InviteDialog
        open={inviteOpen}
        object={selected}
        friends={friends}
        onClose={() => setInviteOpen(false)}
        onFindFriend={() => {
          setInviteOpen(false);
          navigate("/friends");
        }}
      />

      <Modal
        open={pulseOpen}
        onClose={() => setPulseOpen(false)}
        title={zh ? "世界留下的每一个瞬间" : "Every moment the world left behind"}
        kicker="LIVING PULSE"
        wide
        actions={
          <Link className="dw-button dw-button-ghost" to="/self" onClick={() => setPulseOpen(false)}>
            {zh ? "查看待确认事项" : "Review pending approvals"}
          </Link>
        }
      >
        <p className="dw-modal-lead">
          {zh
            ? "这里只展示真实发生过的对象、邀请、公开与 AI 提议事件，不编造离线活动。"
            : "This shows only real object, invitation, publication, and AI-proposal events. It never invents offline activity."}
        </p>
        {pulse?.publication_consents.length ? (
          <section className="dw-consent-section">
            <h3>{zh ? "等待你确认的公开版本" : "Public versions waiting for your consent"}</h3>
            <ul className="dw-pulse-list">
              {pulse.publication_consents.map((consent) => (
                <li key={consent.id}>
                  <span className="dw-pulse-icon">
                    <Icon name="globe" size={16} />
                  </span>
                  <div>
                    <strong>{consent.title}</strong>
                    <small>
                      {zh ? `由 ${consent.publisher_name} 请求公开` : `Requested by ${consent.publisher_name}`}
                    </small>
                  </div>
                  <div className="dw-consent-actions">
                    <button
                      className="dw-friend-action"
                      type="button"
                      disabled={consentBusy !== ""}
                      onClick={() => void respondConsent(consent.id, true)}
                    >
                      {consentBusy === `${consent.id}:true` ? (zh ? "确认中…" : "Approving…") : zh ? "同意公开" : "Approve"}
                    </button>
                    <button
                      className="dw-friend-action dw-friend-action-quiet"
                      type="button"
                      disabled={consentBusy !== ""}
                      onClick={() => void respondConsent(consent.id, false)}
                    >
                      {zh ? "拒绝" : "Decline"}
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          </section>
        ) : null}
        {pulseItems.length === 0 ? (
          <p className="dw-empty-note">
            {zh ? "世界还没有留下改变。第一次触碰就会开始记录。" : "No changes yet. The first touch starts the record."}
          </p>
        ) : (
          <ul className="dw-pulse-list">
            {pulseItems.map((item, index) => (
              <li key={`${item.object_id}-${item.occurred_at}-${index}`}>
                <span className="dw-pulse-icon">
                  <Icon name={pulseIcon(item)} size={16} />
                </span>
                <div>
                  <strong>{pulseLine(item)}</strong>
                  <small>
                    {new Intl.DateTimeFormat(zh ? "zh-CN" : "en", {
                      dateStyle: "medium",
                      timeStyle: "short"
                    }).format(new Date(item.occurred_at))}
                  </small>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Modal>

      {loading && !stage ? (
        <div className="dw-page-loading" role="status">
          {zh ? "正在进入你的世界…" : "Entering your world…"}
        </div>
      ) : null}
      {toastView}
    </div>
  );
}
