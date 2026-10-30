// SPDX-License-Identifier: Apache-2.0
import type { ReactNode } from "react";

export type IconName =
  | "world"
  | "together"
  | "explore"
  | "spark"
  | "branch"
  | "plus"
  | "close"
  | "search"
  | "lock"
  | "globe"
  | "help"
  | "gear"
  | "arrow"
  | "check"
  | "edit"
  | "trash"
  | "message"
  | "send"
  | "link"
  | "shield"
  | "book"
  | "idea"
  | "layers"
  | "reset"
  | "eye"
  | "user"
  | "chevron"
  | "clock"
  | "move"
  | "logout"
  | "compass";

const paths: Record<IconName, ReactNode> = {
  world: (
    <>
      <circle cx="12" cy="12" r="8" />
      <path d="M4 12h16M12 4c4 4 4 12 0 16M12 4c-4 4-4 12 0 16" />
    </>
  ),
  together: (
    <>
      <circle cx="9" cy="8" r="3" />
      <path d="M3 21v-3a6 6 0 0 1 12 0v3M17 7a3 3 0 0 1 0 6m1 3a5 5 0 0 1 3 5" />
    </>
  ),
  explore: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="m15.5 8.5-2.2 4.8-4.8 2.2 2.2-4.8 4.8-2.2Z" />
    </>
  ),
  spark: <path d="m12 2 1.7 6.3L20 10l-6.3 1.7L12 18l-1.7-6.3L4 10l6.3-1.7L12 2ZM19 16l.7 2.3L22 19l-2.3.7L19 22l-.7-2.3L16 19l2.3-.7L19 16Z" />,
  branch: <path d="M6 3v8a5 5 0 0 0 5 5h7M6 8h8M18 12v8" />,
  plus: <path d="M12 5v14M5 12h14" />,
  close: <path d="M18 6 6 18M6 6l12 12" />,
  search: (
    <>
      <circle cx="10.7" cy="10.7" r="6.2" />
      <path d="m15.2 15.2 5 5" />
    </>
  ),
  lock: (
    <>
      <path d="M8 10V7a4 4 0 0 1 8 0v3" />
      <rect x="5" y="10" width="14" height="11" rx="2" />
    </>
  ),
  globe: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18M12 3c5 5 5 13 0 18M12 3c-5 5-5 13 0 18" />
    </>
  ),
  help: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M9.5 9a2.5 2.5 0 1 1 4.1 1.9c-1.1.9-1.6 1.4-1.6 3.1M12 18h.01" />
    </>
  ),
  gear: (
    <>
      <circle cx="12" cy="12" r="3" />
      <path d="M12 2v3M12 19v3M2 12h3M19 12h3M4.9 4.9 7 7M17 17l2.1 2.1M19.1 4.9 17 7M7 17l-2.1 2.1" />
    </>
  ),
  arrow: <path d="M5 12h14M13 5l7 7-7 7" />,
  check: <path d="M4 12l5 5L20 6" />,
  edit: <path d="M12 5h7M5 19l3.5-.7L19 7.8 16.2 5 5.7 15.5 5 19Z" />,
  trash: <path d="M4 7h16M9 7V4h6v3M7 7l1 13h8l1-13M10 11v5M14 11v5" />,
  message: <path d="M4 4h16v12H8l-4 4V4Z" />,
  send: <path d="m21 3-7.4 18-3.7-7.9L2 9.4 21 3ZM9.9 13.1 21 3" />,
  link: <path d="M10 13a5 5 0 0 0 7.5.5l2-2a5 5 0 0 0-7-7l-1.1 1M14 11a5 5 0 0 0-7.5-.5l-2 2a5 5 0 0 0 7 7l1.1-1" />,
  shield: <path d="M12 2 4 6v7c0 5 3 8 8 9 5-1 8-4 8-9V6l-8-4Z" />,
  book: <path d="M4 4h7a4 4 0 0 1 4 4v12H8a4 4 0 0 0-4 1V4Zm16 0h-7a4 4 0 0 0-4 4v12h7a4 4 0 0 1 4 1V4Z" />,
  idea: <path d="M9 18h6M10 21h4M7 14a7 7 0 1 1 10 0c-2 2-2 3-2 4H9c0-1 0-2-2-4Z" />,
  layers: <path d="M12 2 2 8l10 6 10-6-10-6ZM2 13l10 6 10-6" />,
  reset: <path d="M3 12a9 9 0 1 0 3-6M3 3v6h6" />,
  eye: (
    <>
      <path d="M2 12s4-6 10-6 10 6 10 6-4 6-10 6-10-6-10-6Z" />
      <circle cx="12" cy="12" r="3" />
    </>
  ),
  user: (
    <>
      <circle cx="12" cy="8" r="3.5" />
      <path d="M4 21v-2a7 7 0 0 1 14 0v2" />
    </>
  ),
  chevron: <path d="m9 6 6 6-6 6" />,
  clock: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 2" />
    </>
  ),
  move: <path d="M12 2v20M2 12h20M12 2l-3 3m3-3 3 3M2 12l3-3m-3 3 3 3m17-3-3-3m3 3-3 3M12 22l-3-3m3 3 3-3" />,
  logout: <path d="M9 4H5v16h4M14 8l4 4-4 4M8 12h10" />,
  compass: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="m15.5 8.5-2.2 4.8-4.8 2.2 2.2-4.8 4.8-2.2Z" />
    </>
  )
};

export default function Icon({
  name,
  size = 20,
  className = ""
}: {
  name: IconName;
  size?: number;
  className?: string;
}) {
  return (
    <svg
      className={`dw-icon ${className}`.trim()}
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {paths[name]}
    </svg>
  );
}
