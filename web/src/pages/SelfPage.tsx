// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router";
import {
  approveConfirmation,
  createDelegation,
  denyConfirmation,
  listConfirmations,
  listDelegations,
  listObjects,
  revokeDelegation,
  type Confirmation,
  type Delegation,
  type ObjectListItem,
  type ObjectView
} from "../api";
import { ErrorNotice, StatusNotice } from "../errors";
import { permissionKey } from "../humanize";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";

type AIPermission = "read" | "contribute" | "derive";

function constraintsOf(delegation: Delegation): { maxActions: number; requireConfirmation: string[] } {
  const constraints = delegation.constraints ?? {};
  const maxActions =
    typeof constraints.max_ai_actions_per_hour === "number" ? constraints.max_ai_actions_per_hour : 20;
  const requireConfirmation = Array.isArray(constraints.require_confirmation)
    ? (constraints.require_confirmation as string[])
    : [];
  return { maxActions, requireConfirmation };
}

export default function SelfPage() {
  const { t, lang } = useI18n();
  const { status, user, logout, logoutError } = useAuth();
  const [objects, setObjects] = useState<ObjectListItem[]>([]);
  const [delegations, setDelegations] = useState<Delegation[]>([]);
  const [confirmations, setConfirmations] = useState<Confirmation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [notice, setNotice] = useState("");
  const [createdChild, setCreatedChild] = useState<ObjectView | null>(null);
  const [busy, setBusy] = useState("");
  const [scopeObjectID, setScopeObjectID] = useState("");
  const [scopePermissions, setScopePermissions] = useState<AIPermission[]>(["read", "contribute"]);
  const [scopeExpiry, setScopeExpiry] = useState("30");
  const [scopeMaxActions, setScopeMaxActions] = useState(20);

  const timeFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(lang === "zh" ? "zh-CN" : "en", {
        dateStyle: "medium",
        timeStyle: "short"
      }),
    [lang]
  );

  const objectsByID = useMemo(() => {
    const map = new Map<string, ObjectListItem>();
    for (const item of objects) {
      map.set(item.id, item);
    }
    return map;
  }, [objects]);

  const load = useCallback(async () => {
    setError(null);
    try {
      const [objectPage, delegationPage, confirmationPage] = await Promise.all([
        listObjects("all"),
        listDelegations(),
        listConfirmations("pending")
      ]);
      setObjects(objectPage.items);
      setDelegations(delegationPage.items);
      setConfirmations(confirmationPage.items);
      setScopeObjectID((previous) => previous || objectPage.items[0]?.id || "");
    } catch (failure) {
      setError(failure);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (status === "authenticated") {
      void load();
    }
  }, [status, load]);

  async function revoke(item: Delegation) {
    if (!window.confirm(t("self.revokeDelegationConfirm"))) {
      return;
    }
    setBusy(`delegation:${item.id}`);
    setError(null);
    try {
      await revokeDelegation(item.id);
      setNotice(t("workspace.saved"));
      await load();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function approve(item: Confirmation) {
    if (!window.confirm(t("workspace.confirmApprove"))) {
      return;
    }
    setBusy(`confirmation:${item.id}`);
    setError(null);
    setCreatedChild(null);
    try {
      const created = await approveConfirmation(item.id);
      setCreatedChild(created);
      setNotice(t("self.approved"));
      await load();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  async function deny(item: Confirmation) {
    setBusy(`confirmation:${item.id}`);
    setError(null);
    try {
      await denyConfirmation(item.id);
      setNotice(t("self.denied"));
      await load();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  function toggleScopePermission(permission: AIPermission) {
    if (permission === "read") {
      return;
    }
    setScopePermissions((previous) =>
      previous.includes(permission)
        ? previous.filter((value) => value !== permission)
        : [...previous, permission]
    );
  }

  async function grant() {
    if (!scopeObjectID) {
      return;
    }
    setBusy("grant");
    setError(null);
    setNotice("");
    try {
      await createDelegation({
        object_id: scopeObjectID,
        permissions: scopePermissions,
        constraints: {
          max_ai_actions_per_hour: scopeMaxActions,
          require_confirmation: ["derive.create", "capability.invoke"]
        },
        expires_in_days: scopeExpiry === "" ? undefined : Number(scopeExpiry)
      });
      setNotice(t("self.delegationCreated"));
      await load();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy("");
    }
  }

  if (status === "loading" || loading) {
    return <p className="page">{t("common.loading")}</p>;
  }
  if (status === "anonymous" || !user) {
    return (
      <div className="page">
        <h1>{t("self.title")}</h1>
        <p className="notice">{t("auth.required")}</p>
        <Link className="button" to="/login?next=%2Fself">
          {t("nav.login")}
        </Link>
      </div>
    );
  }

  const owned = objects.filter((item) => item.role === "owner").length;
  const shared = objects.length - owned;

  return (
    <div className="page page-wide self-page">
      <div className="page-heading">
        <p className="eyebrow">{t("app.name")}</p>
        <h1>{t("self.title")}</h1>
        <p className="lead">{t("self.lead")}</p>
      </div>

      {notice ? <StatusNotice>{notice}</StatusNotice> : null}
      {createdChild ? (
        <p className="status-notice status-success">
          {t("workspace.approvalCreated", {
            title:
              typeof createdChild.state.title === "string" ? createdChild.state.title : createdChild.id,
            version: createdChild.version
          })}{" "}
          <Link to={`/objects/${createdChild.id}?tab=work`}>{t("workspace.openCreated")} →</Link>
        </p>
      ) : null}
      {error ? <ErrorNotice error={error} onRetry={() => void load()} /> : null}

      <div className="self-grid">
        <section className="card identity-card">
          <div className="avatar avatar-large" aria-hidden="true">
            {user.display_name.slice(0, 1).toUpperCase()}
          </div>
          <h2>{t("self.identity")}</h2>
          <p className="identity-name">{user.display_name}</p>
          <p className="mono">@{user.handle}</p>
          <p>{user.email}</p>
          <p className="muted">
            {t("self.memberSince")}: {timeFormatter.format(new Date(user.created_at))}
          </p>
        </section>

        <section className="card">
          <h2>{t("self.presence")}</h2>
          <div className="mini-stats">
            <div>
              <strong>{owned}</strong>
              <span>{t("self.owned")}</span>
            </div>
            <div>
              <strong>{shared}</strong>
              <span>{t("self.shared")}</span>
            </div>
            <div>
              <strong>{confirmations.length}</strong>
              <span>{t("self.approvals")}</span>
            </div>
          </div>
          <Link className="text-link" to="/objects">
            {t("world.quick.objects")} →
          </Link>
        </section>
      </div>

      <section className="card">
        <h2>{t("self.consent")}</h2>
        <p className="muted">{t("self.consentLead")}</p>
        {delegations.length === 0 ? <p className="muted">{t("self.noDelegations")}</p> : null}
        <ul className="delegation-list">
          {delegations.map((item) => {
            const constraints = constraintsOf(item);
            const objectTitle = item.object_id ? objectsByID.get(item.object_id)?.title : null;
            return (
              <li key={item.id}>
                <div>
                  <strong>
                    {item.object_id
                      ? objectTitle
                        ? t("self.delegationObjectNamed", { title: objectTitle })
                        : t("self.delegationObject")
                      : t("self.delegationAll")}
                  </strong>
                  <small>
                    {item.permissions.map((permission) => t(permissionKey(permission))).join(" · ")} ·{" "}
                    {t("self.delegationMaxActions", { count: constraints.maxActions })}
                  </small>
                  <small>
                    {t("self.delegationExpiry")}:{" "}
                    {item.expires_at ? timeFormatter.format(new Date(item.expires_at)) : t("self.delegationNever")}
                  </small>
                  {constraints.requireConfirmation.length > 0 ? (
                    <small>
                      {t("self.delegationRequire", {
                        actions: constraints.requireConfirmation.join(", ")
                      })}
                    </small>
                  ) : null}
                  <span className={`status-chip status-${item.status}`}>
                    {t(`self.delegationStatus.${item.status}`)}
                  </span>
                </div>
                {item.status === "active" ? (
                  <button
                    className="text-button"
                    type="button"
                    disabled={busy !== ""}
                    onClick={() => void revoke(item)}
                  >
                    {t("self.revokeDelegation")}
                  </button>
                ) : null}
              </li>
            );
          })}
        </ul>

        <details className="advanced-panel" open={objects.length > 0}>
          <summary>{t("self.addDelegation")}</summary>
          <p className="form-hint">{t("self.addDelegationHint")}</p>
          {objects.length === 0 ? (
            <p className="muted">{t("self.noObjectsForDelegation")}</p>
          ) : (
            <div className="field-stack">
              <label>
                <span>{t("self.delegationObjectField")}</span>
                <select value={scopeObjectID} onChange={(event) => setScopeObjectID(event.target.value)}>
                  {objects.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.title}
                    </option>
                  ))}
                </select>
              </label>
              <fieldset className="permission-grid">
                <legend>{t("self.delegationPermissionsField")}</legend>
                {(["read", "contribute", "derive"] as AIPermission[]).map((permission) => (
                  <label key={permission} className="check-row">
                    <input
                      type="checkbox"
                      checked={scopePermissions.includes(permission)}
                      disabled={permission === "read"}
                      onChange={() => toggleScopePermission(permission)}
                    />
                    <span>{t(permissionKey(permission))}</span>
                  </label>
                ))}
              </fieldset>
              <div className="inline-field">
                <label>
                  <span>{t("self.delegationExpiryField")}</span>
                  <select value={scopeExpiry} onChange={(event) => setScopeExpiry(event.target.value)}>
                    <option value="">{t("self.delegationNever")}</option>
                    <option value="7">7</option>
                    <option value="30">30</option>
                    <option value="90">90</option>
                    <option value="365">365</option>
                  </select>
                </label>
                <label>
                  <span>{t("self.delegationMaxActionsField")}</span>
                  <input
                    type="number"
                    min={1}
                    max={200}
                    value={scopeMaxActions}
                    onChange={(event) => setScopeMaxActions(Number(event.target.value))}
                  />
                </label>
              </div>
              <button
                className="button button-small"
                type="button"
                disabled={busy !== "" || !scopePermissions.includes("read")}
                onClick={() => void grant()}
              >
                {t("self.delegationCreate")}
              </button>
            </div>
          )}
        </details>
      </section>

      <section className="card">
        <h2>{t("self.approvals")}</h2>
        <p className="muted">{t("self.approvalsHint")}</p>
        {confirmations.length === 0 ? <p className="muted">{t("self.noApprovals")}</p> : null}
        <ul className="confirmation-list">
          {confirmations.map((item) => {
            const summary =
              item.action === "derive.create"
                ? t("confirmation.deriveSummary")
                : item.action === "capability.invoke"
                  ? t("confirmation.capabilitySummary")
                  : t("confirmation.invitationSummary");
            const stale =
              typeof item.bound_version === "number" &&
              item.target_object_version > 0 &&
              item.bound_version < item.target_object_version;
            return (
            <li key={item.id}>
              <div>
                <strong>{t(`confirmation.action.${item.action}`)}</strong>
                <small>
                  {item.target_object_title || t("common.unknown")}
                  {item.target_object_version ? ` · v${item.target_object_version}` : ""} ·{" "}
                  {t("self.approvalExpires", {
                    time: timeFormatter.format(new Date(item.expires_at))
                  })}
                </small>
                <p>{summary}</p>
                {item.effect?.title ? (
                  <small>
                    {t("confirmation.childTitle")}: {item.effect.title}
                    {item.effect.inheritance_fields?.length
                      ? ` · ${t("confirmation.fields")}: ${item.effect.inheritance_fields.join(", ")}`
                      : ""}
                    {typeof item.effect.source_version === "number"
                      ? ` · ${t("confirmation.sourceVersion")}: v${item.effect.source_version}`
                      : ""}
                  </small>
                ) : null}
                {typeof item.bound_version === "number" ? (
                  <small className="mono">
                    {t("confirmation.boundVersion", { version: item.bound_version })}
                  </small>
                ) : null}
                {stale ? (
                  <p className="form-error" role="alert">
                    {t("confirmation.stale")}
                  </p>
                ) : null}
                {item.target_object_id ? (
                  <Link className="text-link" to={`/objects/${item.target_object_id}`}>
                    {t("self.approvalObject")} →
                  </Link>
                ) : null}
              </div>
              <div className="form-actions">
                <button
                  className="button button-small"
                  type="button"
                  disabled={busy !== ""}
                  onClick={() => void approve(item)}
                >
                  {t("self.approvalApprove")}
                </button>
                <button
                  className="button button-small button-ghost"
                  type="button"
                  disabled={busy !== ""}
                  onClick={() => void deny(item)}
                >
                  {t("self.approvalDeny")}
                </button>
              </div>
            </li>
            );
          })}
        </ul>
      </section>

      <div className="self-grid">
        <section className="card">
          <h2>{t("self.sessionTitle")}</h2>
          <p className="muted">{t("self.sessionNote")}</p>
          {logoutError ? (
            <p className="form-error" role="alert">
              {t("auth.logoutFailed")}
            </p>
          ) : null}
          <button className="button button-ghost" type="button" onClick={() => void logout()}>
            {t("self.signOut")}
          </button>
        </section>

        <section className="card">
          <h2>{t("self.dataTitle")}</h2>
          <p className="muted">{t("self.dataBody")}</p>
          <div className="form-actions">
            <Link className="button button-small button-ghost" to="/settings">
              {t("self.manageTokens")}
            </Link>
            <Link className="button button-small button-ghost" to="/settings">
              {t("self.exportLink")}
            </Link>
          </div>
        </section>
      </div>

      <section className="card">
        <h2>{t("self.securityTitle")}</h2>
        <p>{t("self.securityBody")}</p>
        <p className="muted">{t("self.laterGates")}</p>
      </section>
    </div>
  );
}
