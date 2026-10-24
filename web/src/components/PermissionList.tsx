// SPDX-License-Identifier: Apache-2.0
import { permissionBodyKey, permissionKey } from "../humanize";
import { useI18n } from "../i18n";

export default function PermissionList({
  permissions,
  withDescription = false
}: {
  permissions: string[];
  withDescription?: boolean;
}) {
  const { t } = useI18n();
  if (permissions.length === 0) {
    return <p className="muted">{t("common.none")}</p>;
  }
  if (!withDescription) {
    return (
      <span className="permission-chips">
        {permissions.map((permission) => (
          <span className="permission-chip" key={permission}>
            {t(permissionKey(permission))}
          </span>
        ))}
      </span>
    );
  }
  return (
    <ul className="permission-list">
      {permissions.map((permission) => (
        <li key={permission}>
          <strong>{t(permissionKey(permission))}</strong>
          <span>{t(permissionBodyKey(permission))}</span>
        </li>
      ))}
    </ul>
  );
}
