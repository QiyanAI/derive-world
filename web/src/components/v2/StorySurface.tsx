// SPDX-License-Identifier: Apache-2.0
import { useMemo } from "react";
import type { OpenWorldNode, OpenWorldState, WorldViewNode } from "../../api";
import { useI18n } from "../../i18n";

export default function StorySurface({
  node,
  world,
  canContribute,
  busy,
  onInvoke
}: {
  node: WorldViewNode;
  world: OpenWorldState;
  canContribute: boolean;
  busy: boolean;
  onInvoke: (behaviorID: string, entityID?: string, value?: unknown) => void;
}) {
  const { t, lang } = useI18n();
  const zh = lang === "zh";
  const props = node.props ?? {};
  const chapterKindID = typeof props.chapter_kind_id === "string" ? props.chapter_kind_id : "";
  const progressVariable =
    typeof props.progress_variable === "string" ? props.progress_variable : "story.chapter";
  const title = typeof props.title === "string" ? props.title : world.title;
  const restartBehavior =
    typeof props.restart_behavior_id === "string" ? props.restart_behavior_id : "";
  const jumpByEntity = useMemo(() => {
    const map = new Map<string, string>();
    const list = Array.isArray(props.jump_behaviors) ? props.jump_behaviors : [];
    for (const raw of list) {
      if (!raw || typeof raw !== "object") {
        continue;
      }
      const entry = raw as Record<string, unknown>;
      const entityID = typeof entry.entity_id === "string" ? entry.entity_id : "";
      const behaviorID = typeof entry.behavior_id === "string" ? entry.behavior_id : "";
      if (entityID && behaviorID) {
        map.set(entityID, behaviorID);
      }
    }
    return map;
  }, [props.jump_behaviors]);
  const chapters = useMemo(
    () => world.nodes.filter((candidate) => candidate.kind_id === chapterKindID),
    [world.nodes, chapterKindID]
  );
  const rawProgress = world.variables?.[progressVariable];
  const progress =
    typeof rawProgress === "number" && Number.isFinite(rawProgress)
      ? Math.max(0, Math.min(chapters.length - 1, Math.floor(rawProgress)))
      : 0;
  const current = chapters[progress];
  const chapterByID = useMemo(() => {
    const map = new Map<string, OpenWorldNode>();
    for (const chapter of chapters) {
      map.set(chapter.id, chapter);
    }
    return map;
  }, [chapters]);
  const choices = useMemo(() => {
    if (!current) {
      return [];
    }
    return world.links.filter(
      (link) => link.from === current.id && chapterByID.has(link.to)
    );
  }, [world.links, current, chapterByID]);

  function jump(targetID: string) {
    if (!canContribute || busy) {
      return;
    }
    const behaviorID = jumpByEntity.get(targetID);
    const targetIndex = chapters.findIndex((chapter) => chapter.id === targetID);
    if (behaviorID && targetIndex >= 0) {
      onInvoke(behaviorID, targetID, targetIndex);
      return;
    }
    // Legacy worlds may have chapter links without generated jump behaviors.
    // They remain readable; only persistence requires a generated behavior.
  }

  if (!chapterKindID || chapters.length === 0) {
    return (
      <section className="dw-story-surface" data-testid="world-story">
        <p className="muted">
          {zh ? "这部作品还没有篇章。" : "This work has no chapters yet."}
        </p>
      </section>
    );
  }

  return (
    <section className="dw-story-surface" data-testid="world-story">
      <header className="dw-story-head">
        <span className="dw-story-kicker">{zh ? "可读的世界" : "A living book"}</span>
        <h2>{title}</h2>
        <div className="dw-story-progress">
          <span>
            {zh
              ? `第 ${progress + 1} / ${chapters.length} 章`
              : `Chapter ${progress + 1} / ${chapters.length}`}
          </span>
          <span className="dw-story-progress-track">
            <span style={{ width: `${((progress + 1) / chapters.length) * 100}%` }} />
          </span>
        </div>
      </header>
      <div className="dw-story-layout">
        <article className="dw-story-page" data-testid="story-chapter">
          <h3>{current?.name}</h3>
          <div className="dw-story-text">
            {(current?.text ?? "").split(/\n{2,}/).map((paragraph, index) => (
              <p key={`${current?.id}:${index}`}>{paragraph}</p>
            ))}
          </div>
          <div className="dw-story-choices">
            {choices.map((link) => (
              <button
                key={link.id}
                type="button"
                className="dw-button dw-button-primary dw-story-choice"
                data-testid="story-choice"
                disabled={!canContribute || busy}
                onClick={() => jump(link.to)}
              >
                {link.label || (zh ? "继续" : "Continue")}
              </button>
            ))}
            {choices.length === 0 ? (
              <p className="dw-story-end">
                {zh ? "这一章在这里结束。" : "This chapter ends here."}
              </p>
            ) : null}
          </div>
        </article>
        <aside className="dw-story-toc" aria-label={zh ? "目录" : "Contents"}>
          <h4>{zh ? "目录" : "Contents"}</h4>
          <ol>
            {chapters.map((chapter, index) => (
              <li key={chapter.id}>
                <button
                  type="button"
                  className={`dw-story-toc-item${index === progress ? " is-current" : ""}`}
                  data-testid="story-toc-item"
                  disabled={!canContribute || busy || index === progress}
                  onClick={() => jump(chapter.id)}
                >
                  <span>{String(index + 1).padStart(2, "0")}</span>
                  {chapter.name}
                </button>
              </li>
            ))}
          </ol>
          {restartBehavior ? (
            <button
              type="button"
              className="dw-button dw-button-ghost dw-button-small"
              disabled={!canContribute || busy}
              onClick={() => onInvoke(restartBehavior)}
            >
              {zh ? "从第一章重新读" : "Read from the beginning"}
            </button>
          ) : null}
          <p className="dw-story-note">
            {t("openWorld.saved")} · {zh ? "阅读位置会随世界保存" : "Your place is saved with the world"}
          </p>
        </aside>
      </div>
    </section>
  );
}
