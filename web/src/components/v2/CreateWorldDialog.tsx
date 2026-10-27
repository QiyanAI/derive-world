// SPDX-License-Identifier: Apache-2.0
import { useLayoutEffect, useRef, useState, type FormEvent } from "react";
import { createObject, errorMessage, type ObjectView } from "../../api";
import { useI18n } from "../../i18n";
import Icon from "./Icon";
import Modal from "./Modal";

export default function CreateWorldDialog({
  open,
  onClose,
  onCreated
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (object: ObjectView) => void;
}) {
  const { lang } = useI18n();
  const zh = lang === "zh";
  const [title, setTitle] = useState("");
  const [kind, setKind] = useState<"project" | "story">("project");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  useLayoutEffect(() => {
    if (open) {
      setTitle("");
      setKind("project");
      setError("");
      setBusy(false);
      window.setTimeout(() => inputRef.current?.focus(), 60);
    }
  }, [open]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    const clean = (title || inputRef.current?.value || "").trim();
    if (!clean) {
      setError(zh ? "先写下一个想开始的念头。" : "Write the idea you want to begin with.");
      inputRef.current?.focus();
      return;
    }
    setBusy(true);
    setError("");
    try {
      const object = await createObject({
        object_type: "living_scene",
        title: clean,
        content: {
          purpose: kind,
          premise: "",
          elements: [
            {
              kind: kind === "story" ? "scene" : "idea",
              text:
                kind === "story"
                  ? zh
                    ? "开场：让这个世界开始呼吸"
                    : "Opening: let this world begin to breathe"
                  : zh
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
      setBusy(false);
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={zh ? "从一个念头开始" : "Begin with one idea"}
      kicker={zh ? "YOUR FIRST SPARK" : "YOUR FIRST SPARK"}
      actions={
        <>
          <button className="dw-button dw-button-ghost" type="button" onClick={onClose}>
            {zh ? "返回" : "Back"}
          </button>
          <button
            className="dw-button dw-button-primary"
            type="submit"
            form="dw-create-world-form"
            disabled={busy}
          >
            {busy ? (zh ? "正在生长…" : "Growing…") : zh ? "让它开始生长" : "Let it grow"}
            <Icon name="arrow" size={16} />
          </button>
        </>
      }
    >
      <form id="dw-create-world-form" onSubmit={submit} className="dw-form">
        <p className="dw-modal-lead">
          {zh
            ? "不是创建一份空白文档，而是给一个想法一个可以继续生长的开始。"
            : "Not an empty document — a beginning that can keep growing."}
        </p>
        <label className="dw-field-label" htmlFor="dw-new-world-title">
          {zh ? "你想让什么开始发生？" : "What do you want to begin?"}
        </label>
        <input
          ref={inputRef}
          id="dw-new-world-title"
          className="dw-input"
          maxLength={200}
          value={title}
          onChange={(event) => setTitle(event.target.value)}
          placeholder={zh ? "例如：一座能听见风的城市…" : "For example: a city that can hear the wind…"}
          autoComplete="off"
        />
        <p className="dw-field-label">{zh ? "它更像哪一种创造？" : "What kind of creation is it?"}</p>
        <div className="dw-seg-choices">
          <button
            type="button"
            className={`dw-seg-choice${kind === "project" ? " is-active" : ""}`}
            aria-pressed={kind === "project"}
            onClick={() => setKind("project")}
          >
            <Icon name="idea" size={20} />
            <strong>{zh ? "把想法变成成果" : "Turn an idea into outcomes"}</strong>
            <small>{zh ? "计划、学习、项目、生活" : "Plans, learning, projects, life"}</small>
          </button>
          <button
            type="button"
            className={`dw-seg-choice${kind === "story" ? " is-active" : ""}`}
            aria-pressed={kind === "story"}
            onClick={() => setKind("story")}
          >
            <Icon name="book" size={20} />
            <strong>{zh ? "创造一个故事世界" : "Create a story world"}</strong>
            <small>{zh ? "设定、剧情、选择与探索" : "Settings, scenes, choices, exploration"}</small>
          </button>
        </div>
        <p className="dw-fineprint">
          <Icon name="lock" size={13} />
          {zh
            ? "新世界默认仅自己可见。邀请好友或公开，都必须由你另行选择。"
            : "A new world is private by default. Inviting someone or publishing requires a separate choice."}
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
