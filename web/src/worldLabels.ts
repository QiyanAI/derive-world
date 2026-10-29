// SPDX-License-Identifier: Apache-2.0

// Shared, user-facing names for the seven universal experience surfaces. The
// same vocabulary is used by the proposal preview, the world tabs and the
// advanced editor so no surface is ever shown as a capability id.
export function surfaceLabel(surface: string, zh: boolean): string {
  switch (surface) {
    case "live":
      return zh ? "可游玩 / 可观看的实时世界" : "playable or playing realtime world";
    case "story":
      return zh ? "可阅读的互动故事" : "readable interactive story";
    case "workspace":
      return zh ? "可使用的工作台" : "usable workbench";
    case "space":
      return zh ? "可视空间地图" : "visual space map";
    case "scene3d":
      return zh ? "可走动的三维场景" : "walkable 3D scene";
    case "media":
      return zh ? "图像 / 视频" : "image or video";
    case "custom":
      return zh ? "自定义交互表面" : "custom interactive surface";
    case "audio":
      return zh ? "程序化声音层" : "procedural sound layer";
    default:
      return zh ? "未知形式" : "unknown surface";
  }
}

// A branch title must never compound. Strip any previous branch suffix, then
// add one fresh suffix. Users can edit the result before confirming.
export function branchTitle(source: string, zh: boolean): string {
  const trimmed = source.trim();
  const stem = trimmed
    .replace(/[\s·—\-–]*(?:新?分支|new\s+branch|branch)(?:\s*\d+)?\s*$/iu, "")
    .trim();
  const base = stem || (zh ? "未命名世界" : "Untitled world");
  return `${base} · ${zh ? "新分支" : "new branch"}`;
}

// Defensive UI guard: a provider document must never be rendered as chat even
// if an older message was persisted before the server-side fix.
export function looksLikeJSONContent(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed) {
    return false;
  }
  if (trimmed.startsWith("{") || trimmed.startsWith("[")) {
    return true;
  }
  if (trimmed.startsWith("```")) {
    const body = trimmed.slice(3);
    const newline = body.indexOf("\n");
    const content = (newline >= 0 ? body.slice(newline + 1) : body).trim();
    return content.startsWith("{") || content.startsWith("[");
  }
  return trimmed.includes('"proposed_actions"');
}
