// SPDX-License-Identifier: Apache-2.0
import { useMemo, useState } from "react";
import type { OpenWorldState, WorldViewNode } from "../../api";
import { useI18n } from "../../i18n";

type Column = { field: string; label: string; type: string };
type Action = { label: string; behavior_id: string; scope: string };
type Summary = { type: string; field?: string; label: string };

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function numberValue(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

export default function WorkspaceSurface({
  node,
  world,
  canContribute,
  busy,
  onInvoke,
  onSetField
}: {
  node: WorldViewNode;
  world: OpenWorldState;
  canContribute: boolean;
  busy: boolean;
  onInvoke: (behaviorID: string, entityID?: string, value?: unknown) => void;
  onSetField: (entityID: string, field: string, value: string | number | boolean) => void;
}) {
  const { lang } = useI18n();
  const zh = lang === "zh";
  const props = node.props ?? {};
  const kindID = typeof props.kind_id === "string" ? props.kind_id : "";
  const title = typeof props.title === "string" ? props.title : "";
  const columns = useMemo(
    () => (Array.isArray(props.columns) ? props.columns.map((raw) => asRecord(raw) as unknown as Column) : []),
    [props.columns]
  );
  const actions = useMemo(
    () => (Array.isArray(props.actions) ? props.actions.map((raw) => asRecord(raw) as unknown as Action) : []),
    [props.actions]
  );
  const summary = useMemo(
    () => (Array.isArray(props.summary) ? props.summary.map((raw) => asRecord(raw) as unknown as Summary) : []),
    [props.summary]
  );
  const rows = useMemo(
    () => world.nodes.filter((candidate) => candidate.kind_id === kindID),
    [world.nodes, kindID]
  );
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [notice, setNotice] = useState("");
  const rowActions = actions.filter((action) => action.scope !== "global");
  const globalActions = actions.filter((action) => action.scope === "global");

  function draftKey(entityID: string, field: string): string {
    return `${entityID}:${field}`;
  }

  function cellValue(entityID: string, field: string, type: string): string {
    const key = draftKey(entityID, field);
    if (drafts[key] !== undefined) {
      return drafts[key];
    }
    const row = rows.find((candidate) => candidate.id === entityID);
    const value = row?.values?.[field];
    if (type === "flag") {
      return value === true ? "true" : "false";
    }
    return value === undefined || value === null ? "" : String(value);
  }

  function commitCell(entityID: string, field: string, type: string) {
    const key = draftKey(entityID, field);
    const raw = drafts[key];
    if (raw === undefined || !canContribute || busy) {
      return;
    }
    let value: string | number | boolean = raw;
    if (type === "number") {
      const parsed = Number(raw);
      if (!Number.isFinite(parsed)) {
        setNotice(zh ? "请输入有效数字。" : "Enter a valid number.");
        return;
      }
      value = parsed;
    } else if (type === "flag") {
      value = raw === "true";
    }
    setNotice("");
    onSetField(entityID, field, value);
  }

  const summaryValues = summary.map((entry) => {
    switch (entry.type) {
      case "count":
        return { label: entry.label, value: rows.length };
      case "done":
        return {
          label: entry.label,
          value: rows.filter((row) => row.status === "done").length
        };
      case "sum":
        return {
          label: entry.label,
          value: rows.reduce((total, row) => total + numberValue(row.values?.[entry.field ?? ""]), 0)
        };
      default:
        return { label: entry.label, value: 0 };
    }
  });

  return (
    <section className="dw-workspace-surface" data-testid="world-workspace">
      <header className="dw-workspace-head">
        <div>
          <span className="dw-workspace-kicker">{zh ? "可操作的世界" : "A working world"}</span>
          <h2>{title || world.title}</h2>
        </div>
        <div className="dw-workspace-summary">
          {summaryValues.map((entry) => (
            <span key={entry.label} className="dw-workspace-stat">
              <strong>{entry.value}</strong>
              <small>{entry.label}</small>
            </span>
          ))}
        </div>
      </header>
      {globalActions.length > 0 ? (
        <div className="dw-workspace-global-actions">
          {globalActions.map((action) => (
            <button
              key={action.behavior_id}
              type="button"
              className="dw-button dw-button-small dw-button-primary"
              data-testid="workspace-global-action"
              disabled={!canContribute || busy}
              onClick={() => onInvoke(action.behavior_id)}
            >
              {action.label}
            </button>
          ))}
        </div>
      ) : null}
      <div className="dw-workspace-table-wrap">
        <table className="dw-workspace-table" data-testid="workspace-table">
          <thead>
            <tr>
              {columns.map((column) => (
                <th key={column.field}>{column.label}</th>
              ))}
              {rowActions.length > 0 ? <th>{zh ? "操作" : "Actions"}</th> : null}
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.id} data-testid="workspace-row" data-entity-id={row.id}>
                {columns.map((column) => {
                  if (column.field === "status") {
                    return (
                      <td key={column.field}>
                        <span className={`dw-workspace-status is-${row.status}`}>
                          {row.status === "done"
                            ? (zh ? "完成" : "Done")
                            : (zh ? "进行中" : "Open")}
                        </span>
                      </td>
                    );
                  }
                  if (column.type === "flag") {
                    const checked = cellValue(row.id, column.field, "flag") === "true";
                    return (
                      <td key={column.field}>
                        <input
                          type="checkbox"
                          checked={checked}
                          disabled={!canContribute || busy}
                          data-testid={`workspace-flag-${column.field}`}
                          onChange={(event) => {
                            const next = event.target.checked ? "true" : "false";
                            setDrafts((current) => ({
                              ...current,
                              [draftKey(row.id, column.field)]: next
                            }));
                            if (canContribute && !busy) {
                              onSetField(row.id, column.field, event.target.checked);
                            }
                          }}
                        />
                      </td>
                    );
                  }
                  return (
                    <td key={column.field}>
                      <input
                        className="dw-workspace-input"
                        type={column.type === "number" ? "number" : "text"}
                        value={cellValue(row.id, column.field, column.type)}
                        disabled={!canContribute || busy}
                        data-testid={`workspace-cell-${column.field}`}
                        onChange={(event) => {
                          const value = event.target.value;
                          setDrafts((current) => ({
                            ...current,
                            [draftKey(row.id, column.field)]: value
                          }));
                        }}
                        onBlur={() => commitCell(row.id, column.field, column.type)}
                        onKeyDown={(event) => {
                          if (event.key === "Enter") {
                            event.currentTarget.blur();
                          }
                        }}
                      />
                      {column.type === "number" ? (
                        <span className="dw-workspace-bar" aria-hidden="true">
                          <span
                            style={{
                              width: `${Math.max(2, Math.min(100, numberValue(rows.find((candidate) => candidate.id === row.id)?.values?.[column.field]) * 5))}%`
                            }}
                          />
                        </span>
                      ) : null}
                    </td>
                  );
                })}
                {rowActions.length > 0 ? (
                  <td className="dw-workspace-row-actions">
                    {rowActions.map((action) => (
                      <button
                        key={action.behavior_id}
                        type="button"
                        className="dw-button dw-button-small dw-button-ghost"
                        data-testid="workspace-row-action"
                        disabled={!canContribute || busy}
                        onClick={() => onInvoke(action.behavior_id, row.id)}
                      >
                        {action.label}
                      </button>
                    ))}
                  </td>
                ) : null}
              </tr>
            ))}
            {rows.length === 0 ? (
              <tr>
                <td colSpan={Math.max(1, columns.length + (rowActions.length > 0 ? 1 : 0))}>
                  <p className="muted">
                    {zh ? "还没有条目。用上面的按钮添加第一行。" : "No rows yet. Add the first one above."}
                  </p>
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
      {notice ? <p className="dw-form-error">{notice}</p> : null}
      <p className="dw-workspace-note">
        {zh
          ? "每次编辑、操作和新增都会真实保存；刷新后仍然存在。"
          : "Every edit, action and new row is saved and survives a refresh."}
      </p>
    </section>
  );
}
