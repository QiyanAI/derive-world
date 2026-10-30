// SPDX-License-Identifier: Apache-2.0
import { useEffect, useMemo, useState } from "react";
import {
  createPublication,
  errorMessage,
  listPublications,
  revokePublication,
  type ObjectView,
  type Publication
} from "../../api";
import { useI18n } from "../../i18n";
import Icon from "./Icon";
import Modal from "./Modal";

type PrivacyMode = "private" | "invite" | "view" | "derive";

const choices: Array<{
  mode: PrivacyMode;
  icon: "lock" | "message" | "eye" | "branch";
  zh: [string, string];
  en: [string, string];
}> = [
  {
    mode: "private",
    icon: "lock",
    zh: ["仅自己可见", "所有内容保持私人，不会自动向其他人公开。"],
    en: ["Only me", "Everything stays private. Nothing is shown to anyone else."]
  },
  {
    mode: "invite",
    icon: "message",
    zh: ["与邀请的人一起", "只有明确授权的成员能参与。好友身份不会自动开放世界。"],
    en: ["People I invite", "Only explicitly authorized members can contribute. Friendship never grants access."]
  },
  {
    mode: "view",
    icon: "eye",
    zh: ["让陌生人看见", "别人可以浏览你选定的公开版本，但不能衍生。"],
    en: ["Anyone can view", "People may read the published version, but cannot derive from it."]
  },
  {
    mode: "derive",
    icon: "branch",
    zh: ["公开，并允许衍生", "别人可从公开版本创造独立分支；新分支默认属于对方且私人。"],
    en: ["Public and derivable", "Others may create independent branches; each branch is private to its creator by default."]
  }
];

export default function PrivacyDialog({
  open,
  object,
  onClose,
  onChanged,
  onInvite
}: {
  open: boolean;
  object: ObjectView | null;
  onClose: () => void;
  onChanged: () => Promise<void> | void;
  onInvite: () => void;
}) {
  const { lang } = useI18n();
  const zh = lang === "zh";
  const [publications, setPublications] = useState<Publication[]>([]);
  const [busy, setBusy] = useState<PrivacyMode | "">("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open || !object) {
      setPublications([]);
      setNotice("");
      setError("");
      return;
    }
    const controller = new AbortController();
    setNotice("");
    setError("");
    listPublications(object.id, controller.signal)
      .then((page) => {
        if (!controller.signal.aborted) {
          setPublications(page.items);
        }
      })
      .catch((failure) => {
        if (!controller.signal.aborted) {
          setError(errorMessage(failure));
        }
      });
    return () => controller.abort();
  }, [open, object]);

  const active = useMemo(
    () =>
      publications.find(
        (publication) =>
          publication.status === "active" || publication.status === "pending_consent"
      ) ?? null,
    [publications]
  );
  const current: PrivacyMode = !active
    ? "private"
    : active.derive_allowed
      ? "derive"
      : "view";

  async function choose(mode: PrivacyMode) {
    if (!object) {
      return;
    }
    if (mode === "invite") {
      onClose();
      onInvite();
      return;
    }
    setBusy(mode);
    setError("");
    setNotice("");
    try {
      if (active) {
        await revokePublication(active.id);
      } else if (mode === "private") {
        setNotice(zh ? "这个世界现在仅自己可见。" : "This world is private now.");
        setBusy("");
        await onChanged();
        return;
      }
      if (mode === "private") {
        setNotice(zh ? "已撤回公开版本。世界重新仅自己可见。" : "Publication revoked. The world is private again.");
      } else {
        const publication = await createPublication(object.id, {
          source_version: object.version,
          derive_allowed: mode === "derive",
          license: "all_rights_reserved"
        });
        setNotice(
          publication.status === "pending_consent"
            ? zh
              ? "公开请求已提交。共同创作者同意后，这个版本才会出现在探索世界。"
              : "The publication request is pending. It appears in Explore only after every contributor agrees."
            : zh
              ? "这个版本已主动公开。没有你的这次选择，它不会公开。"
              : "This version is published because you chose it. Nothing is public by default."
        );
      }
      const page = await listPublications(object.id);
      setPublications(page.items);
      await onChanged();
    } catch (failure) {
      setError(errorMessage(failure));
    } finally {
      setBusy("");
    }
  }

  const title = object
    ? (typeof object.state.title === "string" && object.state.title) || (zh ? "未命名世界" : "Untitled world")
    : zh
      ? "公开范围"
      : "Visibility";

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={zh ? "每个世界，都由你决定边界" : "You decide each world's boundary"}
      kicker={zh ? "YOUR SPACE, YOUR CHOICE" : "YOUR SPACE, YOUR CHOICE"}
      wide
    >
      <p className="dw-modal-lead">
        {object
          ? zh
            ? `“${title}”当前版本 v${object.version}。公开只投影你主动选择的版本，不会公开私人 AI 上下文或全部成员内容。`
            : `“${title}” version v${object.version}. Publishing projects only the version you choose; private AI context and all member content are never included.`
          : zh
            ? "先选择一个世界，再决定它的边界。私人是默认。"
            : "Select a world first. Private is the default."}
      </p>
      {object ? (
        <div className={`dw-privacy-list${busy ? " is-busy" : ""}`} role="radiogroup" aria-label={zh ? "公开范围" : "Visibility"}>
          {choices.map((choice) => {
            const selected = current === choice.mode;
            return (
              <button
                key={choice.mode}
                type="button"
                role="radio"
                aria-checked={selected}
                className={`dw-privacy-option${selected ? " is-active" : ""}`}
                disabled={busy !== ""}
                onClick={() => void choose(choice.mode)}
              >
                <Icon name={choice.icon} size={20} />
                <span>
                  <strong>{choice.zh[0] && (zh ? choice.zh[0] : choice.en[0])}</strong>
                  <small>{zh ? choice.zh[1] : choice.en[1]}</small>
                </span>
                {selected ? <span className="dw-privacy-current">{zh ? "当前" : "Current"}</span> : null}
              </button>
            );
          })}
        </div>
      ) : (
        <div className="dw-empty-note">
          {zh ? "从世界里选择一个节点，再调整它的公开范围。" : "Select a node in your world to change its visibility."}
        </div>
      )}
      {active ? (
        <p className="dw-fineprint">
          <Icon name="shield" size={13} />
          {active.status === "pending_consent"
            ? zh
              ? "这个公开版本正在等待共同创作者确认，因此尚未出现在探索世界。"
              : "This publication is waiting for contributor consent, so it is not in Explore yet."
            : zh
              ? "这个版本已公开。撤回后，新的访问将立即停止。"
              : "This version is public. Revoking stops new access immediately."}
        </p>
      ) : (
        <p className="dw-fineprint">
          <Icon name="lock" size={13} />
          {zh
            ? "私人是默认。好友关系本身不会产生私人世界访问权。"
            : "Private is the default. Friendship itself never grants access to a private world."}
        </p>
      )}
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
