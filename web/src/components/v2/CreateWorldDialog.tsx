// SPDX-License-Identifier: Apache-2.0
import { useLayoutEffect, useRef, useState, type FormEvent } from "react";
import { createObject, errorMessage, type ObjectView } from "../../api";
import { useI18n } from "../../i18n";
import Icon from "./Icon";
import Modal from "./Modal";

export default function CreateWorldDialog({
  open,
  onClose,
  onCreated,
  onAskAI
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (object: ObjectView) => void;
  onAskAI?: (prompt: string) => void;
}) {
  const { lang } = useI18n();
  const zh = lang === "zh";
  const [busy, setBusy] = useState<"ai" | "blank" | "">("");
  const [error, setError] = useState("");
  const [blankKind, setBlankKind] = useState<"scene" | "open">("scene");
  const inputRef = useRef<HTMLTextAreaElement>(null);

  useLayoutEffect(() => {
    if (open) {
      window.setTimeout(() => inputRef.current?.focus(), 60);
    }
  }, [open]);

  function promptValue(): string {
    return (inputRef.current?.value ?? "").trim();
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    const prompt = promptValue();
    if (!prompt) {
      setError(zh ? "先说出你想创造的世界。" : "Describe the world you want to create.");
      inputRef.current?.focus();
      return;
    }
    if (onAskAI) {
      setBusy("ai");
      setError("");
      onAskAI(prompt);
      onClose();
      return;
    }
    await createBlank(prompt);
  }

  async function createBlank(titleOverride?: string) {
    const title = (titleOverride ?? promptValue()) || (zh ? "未命名世界" : "Untitled world");
    setBusy("blank");
    setError("");
    try {
      const object =
        blankKind === "open"
          ? await createObject({
              object_type: "open_world",
              title: title.slice(0, 200),
              content: {
                premise: "",
                kinds: [],
                nodes: [],
                links: [],
                rules: [],
                views: ["scene", "stage", "cards", "timeline"],
                tags: []
              },
              shared_context: {}
            })
          : await createObject({
              object_type: "living_scene",
              title: title.slice(0, 200),
              content: {
                purpose: "project",
                premise: "",
                elements: [
                  {
                    kind: "idea",
                    text: zh
                      ? "第一步：让这个想法开始生长"
                      : "First step: let this idea take shape"
                  }
                ],
                tags: []
              },
              shared_context: {}
            });
      onCreated(object);
      onClose();
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
      title={zh ? "你想创造什么？" : "What do you want to create?"}
      kicker={zh ? "说出一个世界" : "DESCRIBE A WORLD"}
      wide
      actions={
        <>
          <button
            className="dw-button dw-button-ghost"
            type="button"
            onClick={() => void createBlank()}
            disabled={busy !== ""}
          >
            {busy === "blank"
              ? zh ? "正在创建…" : "Creating…"
              : zh ? "让它生长" : "Let it grow"}
          </button>
          <button
            className="dw-button dw-button-primary"
            type="submit"
            form="dw-create-world-form"
            disabled={busy !== ""}
          >
            {busy === "ai"
              ? zh ? "正在交给 Personal AI…" : "Asking your Personal AI…"
              : zh ? "让 Personal AI 创造" : "Let my Personal AI create it"}
            <Icon name="spark" size={16} />
          </button>
        </>
      }
    >
      <form id="dw-create-world-form" onSubmit={submit} className="dw-form">
        <p className="dw-modal-lead">
          {zh
            ? "不需要选择类型、模板或参数。说出你想让它成为什么，Personal AI 会选择并组合真正能运行的形式。"
            : "No type, template or parameter to choose. Say what it should become; your Personal AI will choose and combine forms that actually run."}
        </p>
        <label className="dw-field-label" htmlFor="dw-new-world-prompt">
          {zh ? "你想让什么开始？" : "What do you want to begin?"}
        </label>
        <textarea
          ref={inputRef}
          id="dw-new-world-prompt"
          className="dw-input dw-create-world-prompt"
          rows={5}
          maxLength={4000}
          defaultValue=""
          placeholder={
            zh
              ? "例如：一个可以弹奏的星图；一部关于潮汐图书馆的互动小说；一个收集光点的游戏；一个管理写作计划的工作台……"
              : "For example: a playable star map; an interactive novel about a tide library; a game about gathering light; a workbench for a writing plan…"
          }
        />
        <details className="dw-create-advanced">
          <summary>{zh ? "高级：从空白开始" : "Advanced: start blank"}</summary>
          <div className="dw-create-advanced-choices">
          <button
            type="button"
            className={`dw-chip${blankKind === "scene" ? " is-active" : ""}`}
            aria-pressed={blankKind === "scene"}
            onClick={() => setBlankKind("scene")}
          >
            {zh ? "想法场景" : "Idea scene"}
          </button>
          <button
            type="button"
            className={`dw-chip${blankKind === "open" ? " is-active" : ""}`}
            aria-pressed={blankKind === "open"}
            onClick={() => setBlankKind("open")}
          >
            {zh ? "定义自由世界" : "Define a free world"}
          </button>
          </div>
        </details>
        <p className="dw-fineprint">
          <Icon name="shield" size={13} />
          {zh
            ? "Personal AI 只会提出方案；批准前不会创建任何东西。新世界默认仅自己可见。"
            : "Your Personal AI only proposes; nothing is created until you approve it. New worlds are private by default."}
        </p>
        {error ? (
          <p className="dw-form-error" role="alert">
            {error}
          </p>
        ) : null}
      </form>
    </Modal>
  );
}
