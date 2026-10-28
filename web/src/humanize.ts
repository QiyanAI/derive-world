// SPDX-License-Identifier: Apache-2.0
import type { EventView, ObjectView, Proposal } from "./api";
import type { MessageKey } from "./i18n/en";

const actionKeys: Record<string, MessageKey> = {
  set_title: "action.set_title",
  set_summary: "action.set_summary",
  set_body: "action.set_body",
  add_checklist_item: "action.add_checklist_item",
  toggle_checklist_item: "action.toggle_checklist_item",
  remove_checklist_item: "action.remove_checklist_item",
  add_tag: "action.add_tag",
  remove_tag: "action.remove_tag",
  cast_vote: "action.cast_vote",
  close_poll: "action.close_poll",
  set_shared_context: "action.set_shared_context",
  set_premise: "action.set_premise",
  set_purpose: "action.set_purpose",
  add_element: "action.add_element",
  update_element: "action.update_element",
  remove_element: "action.remove_element",
  move_element: "action.move_element",
  add_kind: "action.add_kind",
  update_kind: "action.update_kind",
  remove_kind: "action.remove_kind",
  add_node: "action.add_node",
  update_node: "action.update_node",
  remove_node: "action.remove_node",
  move_node: "action.move_node",
  set_node_field: "action.set_node_field",
  set_node_status: "action.set_node_status",
  touch_node: "action.touch_node",
  add_link: "action.add_link",
  remove_link: "action.remove_link",
  add_rule: "action.add_rule",
  remove_rule: "action.remove_rule",
  set_views: "action.set_views",
  reshape_world: "action.reshape_world"
};

const eventKeys: Record<string, MessageKey> = {
  "object.created": "event.object.created",
  "object.state_changed": "event.object.state_changed",
  "object.member_added": "event.object.member_added",
  "object.member_removed": "event.object.member_removed",
  "object.invitation_created": "event.object.invitation_created",
  "object.invitation_revoked": "event.object.invitation_revoked",
  "object.derived": "event.object.derived",
  "object.deleted": "event.object.deleted",
  "ai.proposal_created": "event.ai.proposal_created",
  "ai.proposal_applied": "event.ai.proposal_applied"
};

export function actionKey(action: string): MessageKey {
  return actionKeys[action] ?? "action.unknown";
}

export function eventKey(eventType: string): MessageKey {
  return eventKeys[eventType] ?? "event.other";
}

export function permissionKey(permission: string): MessageKey {
  switch (permission) {
    case "read":
      return "workspace.permission.read";
    case "contribute":
      return "workspace.permission.contribute";
    case "derive":
      return "workspace.permission.derive";
    case "invite":
      return "workspace.permission.invite";
    case "manage":
      return "workspace.permission.manage";
    default:
      return "common.unknown";
  }
}

export function permissionBodyKey(permission: string): MessageKey {
  switch (permission) {
    case "read":
      return "workspace.permission.read.body";
    case "contribute":
      return "workspace.permission.contribute.body";
    case "derive":
      return "workspace.permission.derive.body";
    case "invite":
      return "workspace.permission.invite.body";
    case "manage":
      return "workspace.permission.manage.body";
    default:
      return "common.unknown";
  }
}

export function actorLabel(
  event: EventView,
  currentUserId: string | undefined,
  t: (key: MessageKey, vars?: Record<string, string | number>) => string
): string {
  if (event.actor.type === "personal_ai") {
    return event.actor.display_name || t("actor.ai");
  }
  if (event.actor.type === "system") {
    return t("actor.system");
  }
  if (currentUserId && event.actor.user_id === currentUserId) {
    return t("actor.you");
  }
  return event.actor.display_name || t("actor.member");
}

export interface ProposalPreview {
  action: string;
  fields: Array<{ labelKey: MessageKey; value: string }>;
}

const fieldKeys = {
  title: "workspace.actionTitle",
  summary: "workspace.actionSummary",
  body: "workspace.actionBody",
  text: "workspace.checklist",
  item: "workspace.checklist",
  done: "workspace.checklist",
  tag: "workspace.tags",
  option: "object.create.options",
  shared_context: "workspace.sharedContext",
  input: "workspace.eventPayload",
  premise: "scene.premise",
  kind: "scene.elements",
  element: "scene.elements",
  node_name: "openWorld.nodeName",
  field: "openWorld.fieldKey",
  status: "openWorld.statusField",
  value: "openWorld.effectValue",
  position: "scene.moveDown"
} satisfies Record<string, MessageKey>;

function field(label: keyof typeof fieldKeys, value: string) {
  return { labelKey: fieldKeys[label], value };
}

function optionLabel(object: ObjectView | null, optionID: unknown): string {
  if (!object || typeof optionID !== "string" || !Array.isArray(object.state.options)) {
    return typeof optionID === "string" ? optionID : "";
  }
  const options = object.state.options as Array<{ id?: string; label?: string }>;
  const found = options.find((option) => option.id === optionID);
  return found?.label ?? optionID;
}

function checklistLabel(object: ObjectView | null, itemID: unknown): string {
  if (!object || typeof itemID !== "string" || !Array.isArray(object.state.checklist)) {
    return typeof itemID === "string" ? itemID : "";
  }
  const items = object.state.checklist as Array<{ id?: string; text?: string }>;
  const found = items.find((item) => item.id === itemID);
  return found?.text ?? itemID;
}

export function proposalPreview(proposal: Proposal, object: ObjectView | null): ProposalPreview {
  const input = proposal.input ?? {};
  switch (proposal.action) {
    case "set_title":
      return { action: proposal.action, fields: [field("title", String(input.title ?? ""))] };
    case "set_summary":
      return { action: proposal.action, fields: [field("summary", String(input.summary ?? ""))] };
    case "set_body":
      return { action: proposal.action, fields: [field("body", String(input.body ?? ""))] };
    case "add_checklist_item":
      return { action: proposal.action, fields: [field("text", String(input.text ?? ""))] };
    case "toggle_checklist_item":
      return {
        action: proposal.action,
        fields: [
          field("item", checklistLabel(object, input.item_id)),
          field("done", String(input.done ?? ""))
        ]
      };
    case "remove_checklist_item":
      return {
        action: proposal.action,
        fields: [field("item", checklistLabel(object, input.item_id))]
      };
    case "add_tag":
      return { action: proposal.action, fields: [field("tag", String(input.tag ?? ""))] };
    case "remove_tag":
      return { action: proposal.action, fields: [field("tag", String(input.tag ?? ""))] };
    case "cast_vote":
      return {
        action: proposal.action,
        fields: [field("option", optionLabel(object, input.option_id))]
      };
    case "close_poll":
      return { action: proposal.action, fields: [] };
    case "set_shared_context":
      return {
        action: proposal.action,
        fields: [field("shared_context", JSON.stringify(input.shared_context ?? {}))]
      };
    case "set_premise":
      return { action: proposal.action, fields: [field("premise", String(input.premise ?? ""))] };
    case "add_element":
      return {
        action: proposal.action,
        fields: [
          field("kind", String(input.kind ?? "")),
          field("text", String(input.text ?? ""))
        ]
      };
    case "update_element":
      return {
        action: proposal.action,
        fields: [
          field("element", String(input.element_id ?? "")),
          field("text", String(input.text ?? input.status ?? input.kind ?? ""))
        ]
      };
    case "remove_element":
      return {
        action: proposal.action,
        fields: [field("element", String(input.element_id ?? ""))]
      };
    case "move_element":
      return {
        action: proposal.action,
        fields: [
          field("element", String(input.element_id ?? "")),
          field("position", String(input.position ?? ""))
        ]
      };
    case "add_kind":
      return {
        action: proposal.action,
        fields: [
          field("node_name", String(input.name ?? "")),
          field("field", Array.isArray(input.fields) ? `${input.fields.length}` : "0")
        ]
      };
    case "update_kind":
    case "remove_kind":
      return { action: proposal.action, fields: [field("element", String(input.kind_id ?? ""))] };
    case "add_node":
      return {
        action: proposal.action,
        fields: [
          field("node_name", String(input.name ?? "")),
          field("premise", String(input.text ?? ""))
        ]
      };
    case "update_node":
    case "remove_node":
    case "move_node":
      return {
        action: proposal.action,
        fields: [
          field("element", String(input.node_id ?? "")),
          field("node_name", String(input.name ?? input.text ?? ""))
        ]
      };
    case "set_node_field":
      return {
        action: proposal.action,
        fields: [
          field("field", String(input.field ?? "")),
          field("value", String(input.value ?? ""))
        ]
      };
    case "set_node_status":
    case "touch_node":
      return {
        action: proposal.action,
        fields: [
          field("element", String(input.node_id ?? "")),
          field("status", String(input.status ?? ""))
        ]
      };
    case "add_link":
      return {
        action: proposal.action,
        fields: [
          field("premise", String(input.label ?? "")),
          field("element", `${String(input.from ?? "")} → ${String(input.to ?? "")}`)
        ]
      };
    case "remove_link":
    case "remove_rule":
      return { action: proposal.action, fields: [field("element", String(input.link_id ?? input.rule_id ?? ""))] };
    case "add_rule":
      return {
        action: proposal.action,
        fields: [
          field("element", String(input.trigger_id ?? "")),
          field("value", Array.isArray(input.effects) ? `${input.effects.length}` : "0")
        ]
      };
    case "set_views":
      return {
        action: proposal.action,
        fields: [field("value", Array.isArray(input.views) ? input.views.join(", ") : "")]
      };
    case "reshape_world": {
      const content =
        input.content && typeof input.content === "object"
          ? (input.content as Record<string, unknown>)
          : {};
      const kinds = Array.isArray(content.kinds) ? content.kinds.length : 0;
      const nodes = Array.isArray(content.nodes) ? content.nodes.length : 0;
      const variables =
        content.variables && typeof content.variables === "object"
          ? Object.keys(content.variables as Record<string, unknown>).length
          : 0;
      const experience = content.experience && typeof content.experience === "object";
      return {
        action: proposal.action,
        fields: [
          field("summary", String(input.note ?? "")),
          field(
            "input",
            `${kinds} kinds · ${nodes} things · ${variables} variables${experience ? " · experience" : ""}`
          )
        ]
      };
    }
    default:
      return {
        action: proposal.action,
        fields: [field("input", JSON.stringify(input))]
      };
  }
}

export function eventAction(event: EventView): string {
  return typeof event.payload.action === "string" ? event.payload.action : "";
}
