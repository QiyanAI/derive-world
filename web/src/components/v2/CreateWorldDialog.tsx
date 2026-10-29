// SPDX-License-Identifier: Apache-2.0
import { useLayoutEffect, useRef, useState, type FormEvent } from "react";
import { createObject, errorMessage, type ObjectView } from "../../api";
import { useI18n } from "../../i18n";
import Icon from "./Icon";
import Modal from "./Modal";

// CreateWorldDialog asks one question and hands the answer to the Personal AI.
// The default path never creates an empty object: a world exists only after the
// AI has prepared a real plan and the human approves it. A blank world remains
// available as an explicit advanced choice with a required title.
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
  const [blankKind, setBlankKind] = useState<"scene" | "open">("open");
  const inputRef = useRef<HTMLTextAreaElement>(null);

  useLayoutEffect(() => {
    if (open) {
      window.setTimeout(() => inputRef.current?.focus(), 60);
      setBusy("");
      setError("");
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
    setError(zh ? "Personal AI 暂时不可用。" : "Personal AI is unavailable right now.");
  }

  async function createBlank() {
    const title = promptValue();
    if (!title) {
      setError(zh ? "先给这个空白世界一个名字。" : "Give this blank world a name first.");
      inputRef.current?.focus();
      return;
    }
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
      kicker={zh ? "创造一个世界" : "CREATE A WORLD"}
      wide
      actions={
        <>
          <button className="dw-button dw-button-ghost" type="button" onClick={onClose}>
            {zh ? "取消" : "Cancel"}
          </button>
          <button
            className="dw-button dw-button-primary"
            type="submit"
            form="dw-create-world-form"
            disabled={busy !== ""}
            data-testid="create-world-ai"
          >
            {busy === "ai"
              ? zh ? "正在交给 Personal AI…" : "Asking your Personal AI…"
              : zh ? "和我的 AI 一起创造" : "Create with my AI"}
            <Icon name="spark" size={16} />
          </button>
        </>
      }
    >
      <form id="dw-create-world-form" onSubmit={submit} className="dw-form">
        <p className="dw-modal-lead">
          {zh
            ? "不需要选择类型、模板或参数。说出你想让它成为什么，Personal AI 会准备一份可以真正运行的方案；你确认之后，世界才会出现。"
            : "No type, template or parameter to choose. Say what it should become; your Personal AI will prepare a plan that can really run. The world appears only after you confirm."}
        </p>
        <label className="dw-field-label" htmlFor="dw-new-world-prompt">
          {zh ? "你想让什么开始？" : "What do you want to begin?"}
        </label>
        <textarea
          ref={inputRef}
          id="dw-new-world-prompt"
          data-testid="create-world-prompt"
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
          <summary>{zh ? "高级：从空白世界开始" : "Advanced: start from a blank world"}</summary>
          <p className="dw-fineprint">
            {zh
              ? "只有当你明确想自己搭建时才使用。空白世界不会经过 AI 规划；上面的输入会成为它的标题。"
              : "Use this only when you explicitly want to build it yourself. A blank world skips AI planning; the text above becomes its title."}
          </p>
          <div className="dw-create-advanced-choices">
            <button
              type="button"
              className={`dw-chip${blankKind === "open" ? " is-active" : ""}`}
              aria-pressed={blankKind === "open"}
              onClick={() => setBlankKind("open")}
            >
              {zh ? "自由世界" : "Free world"}
            </button>
            <button
              type="button"
              className={`dw-chip${blankKind === "scene" ? " is-active" : ""}`}
              aria-pressed={blankKind === "scene"}
              onClick={() => setBlankKind("scene")}
            >
              {zh ? "想法场景" : "Idea scene"}
            </button>
            <button
              className="dw-button dw-button-ghost dw-button-small"
              type="button"
              onClick={() => void createBlank()}
              disabled={busy !== ""}
            >
              {busy === "blank" ? (zh ? "正在创建…" : "Creating…") : zh ? "创建空白世界" : "Create blank world"}
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
