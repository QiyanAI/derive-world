// SPDX-License-Identifier: Apache-2.0
import type { Confirmation } from "../../api";
import { surfaceLabel } from "../../worldLabels";
import Icon from "./Icon";

// WorldProposalCard is the single approval surface for a pending world.create.
// It shows the real, server-validated shape of the world (premise, surfaces,
// kinds, things, counts) instead of a blind title, and it never renders raw
// model output or payload hashes.
export default function WorldProposalCard({
  item,
  busy,
  zh,
  onCreate,
  onDismiss
}: {
  item: Confirmation;
  busy: string;
  zh: boolean;
  onCreate: (item: Confirmation) => void;
  onDismiss: (item: Confirmation) => void;
}) {
  const effect = item.effect;
  const title = effect?.title?.trim() || item.target_object_title?.trim() || (zh ? "未命名世界" : "Untitled world");
  const summary = effect?.summary?.trim();
  const surfaces = effect?.surfaces ?? [];
  const kindNames = effect?.kind_names ?? [];
  const thingNames = effect?.thing_names ?? [];
  const interactionCount = effect?.interaction_count ?? 0;
  const expired = Date.parse(item.expires_at) <= Date.now();
  const busyCreate = busy === `approve:${item.id}`;
  const busyDismiss = busy === `deny:${item.id}`;

  return (
    <article className="dw-world-proposal-card" data-testid="world-proposal" data-approval-action="world.create">
      <div className="dw-world-proposal-head">
        <p className="dw-inspect-kicker">
          <Icon name="spark" size={13} />
          {expired ? (zh ? "方案已过期" : "This plan expired") : zh ? "方案已准备好，等待你确认" : "Ready for your approval"}
        </p>
        <h4>{title}</h4>
        {summary ? <p className="dw-world-proposal-summary">{summary}</p> : null}
      </div>

      {surfaces.length > 0 ? (
        <div className="dw-world-proposal-section">
          <small>{zh ? "它将真正以这些方式运行" : "It will really run as"}</small>
          <div className="dw-world-proposal-chips">
            {surfaces.map((surface) => (
              <span key={surface} className="dw-world-proposal-chip">
                {surfaceLabel(surface, zh)}
              </span>
            ))}
          </div>
        </div>
      ) : null}

      {kindNames.length > 0 ? (
        <div className="dw-world-proposal-section">
          <small>{zh ? "世界中会有" : "The world will contain"}</small>
          <div className="dw-world-proposal-chips">
            {kindNames.map((name) => (
              <span key={`kind-${name}`} className="dw-world-proposal-chip is-kind">
                {name}
              </span>
            ))}
          </div>
        </div>
      ) : null}

      {thingNames.length > 0 ? (
        <div className="dw-world-proposal-section">
          <small>{zh ? "具体的存在" : "Concrete things"}</small>
          <p className="dw-world-proposal-things">{thingNames.join(zh ? "、" : ", ")}</p>
        </div>
      ) : null}

      <div className="dw-world-proposal-meta">
        {typeof effect?.node_count === "number" ? (
          <span>
            {zh
              ? `${effect.kind_count ?? 0} 种存在 · ${effect.node_count} 个存在 · ${effect.rule_count ?? 0} 条规则`
              : `${effect.kind_count ?? 0} kinds · ${effect.node_count} things · ${effect.rule_count ?? 0} rules`}
          </span>
        ) : null}
        {interactionCount > 0 ? (
          <span>{zh ? `${interactionCount} 个互动` : `${interactionCount} interactions`}</span>
        ) : null}
        <span>
          {expired
            ? zh
              ? "请让 AI 重新准备一份方案"
              : "Ask the AI to prepare a new plan"
            : zh
              ? `请在 ${new Date(item.expires_at).toLocaleTimeString()} 前确认`
              : `Confirm before ${new Date(item.expires_at).toLocaleTimeString()}`}
        </span>
      </div>

      <p className="dw-world-proposal-permission">
        <Icon name="shield" size={13} />
        {zh
          ? "只会创建一个仅你可见的新世界；在你按下确认之前，不会写入任何内容。"
          : "Only a new private world visible to you will be created; nothing is written until you confirm."}
      </p>

      <div className="dw-world-proposal-actions">
        <button
          className="dw-button dw-button-primary dw-button-small"
          type="button"
          disabled={busy !== "" || expired}
          onClick={() => onCreate(item)}
        >
          {busyCreate ? (zh ? "正在创建…" : "Creating…") : zh ? "创建这个世界" : "Create this world"}
        </button>
        <button
          className="dw-button dw-button-ghost dw-button-small"
          type="button"
          disabled={busy !== ""}
          onClick={() => onDismiss(item)}
        >
          {busyDismiss ? (zh ? "正在取消…" : "Dismissing…") : zh ? "暂不创建" : "Not now"}
        </button>
      </div>
    </article>
  );
}
