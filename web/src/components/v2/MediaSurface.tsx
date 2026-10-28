// SPDX-License-Identifier: Apache-2.0
import { useEffect, useRef, useState } from "react";
import type { OpenWorldState } from "../../api";
import { useI18n } from "../../i18n";

type MediaProps = {
  kind: "image" | "video";
  asset_id: string;
  caption: string;
  fit: "contain" | "cover";
  controls: boolean;
  loop: boolean;
  muted: boolean;
  autoplay: boolean;
  play_variable: string;
  show_variable: string;
};

const EMPTY_VARIABLES: Record<string, string | number | boolean> = {};

function asMediaProps(value: Record<string, unknown>): MediaProps {
  return {
    kind: value.kind === "video" ? "video" : "image",
    asset_id: typeof value.asset_id === "string" ? value.asset_id : "",
    caption: typeof value.caption === "string" ? value.caption : "",
    fit: value.fit === "cover" ? "cover" : "contain",
    controls: value.controls !== false,
    loop: value.loop === true,
    muted: value.muted !== false,
    autoplay: value.autoplay === true,
    play_variable: typeof value.play_variable === "string" ? value.play_variable : "",
    show_variable: typeof value.show_variable === "string" ? value.show_variable : ""
  };
}

export function assetURL(assetBase: string, assetID: string): string {
  if (!assetBase || !assetID) {
    return "";
  }
  return `${assetBase}/${encodeURIComponent(assetID)}`;
}

export default function MediaSurface({
  props,
  world,
  assetBase,
  nodeID
}: {
  props: Record<string, unknown>;
  world: OpenWorldState;
  assetBase: string;
  nodeID: string;
}) {
  const { t } = useI18n();
  const media = asMediaProps(props);
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const [failed, setFailed] = useState(false);
  const [soundOn, setSoundOn] = useState(false);
  const variables = world.variables ?? EMPTY_VARIABLES;
  const hidden = media.show_variable ? !variables[media.show_variable] : false;

  useEffect(() => {
    setFailed(false);
  }, [media.asset_id, media.kind]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !media.play_variable) {
      return;
    }
    const playing = Boolean(variables[media.play_variable]);
    if (playing) {
      void video.play().catch(() => {
        // Autoplay policy can still refuse; the native controls stay usable.
      });
    } else {
      video.pause();
    }
  }, [media.play_variable, variables]);

  if (hidden) {
    return null;
  }
  const url = assetURL(assetBase, media.asset_id);
  if (!url) {
    return (
      <div className="dw-media dw-media-empty" data-testid="world-media-empty" data-capability-node={nodeID}>
        <p>{t("openWorld.mediaEmpty")}</p>
      </div>
    );
  }
  if (failed) {
    return (
      <div className="dw-media dw-media-empty" data-testid="world-media-unavailable" data-capability-node={nodeID}>
        <p>{t("openWorld.mediaUnavailable")}</p>
      </div>
    );
  }
  return (
    <figure
      className={`dw-media dw-media-${media.kind}`}
      data-testid="world-media"
      data-media-kind={media.kind}
      data-asset-id={media.asset_id}
      data-capability-node={nodeID}
    >
      {media.kind === "video" ? (
        <video
          ref={videoRef}
          className={`dw-media-content is-${media.fit}`}
          src={url}
          controls={media.controls}
          loop={media.loop}
          muted={media.muted && !soundOn}
          autoPlay={media.autoplay && media.muted}
          playsInline
          preload="metadata"
          onError={() => setFailed(true)}
          aria-label={media.caption || media.kind}
        />
      ) : (
        <img
          className={`dw-media-content is-${media.fit}`}
          src={url}
          alt={media.caption}
          loading="lazy"
          decoding="async"
          onError={() => setFailed(true)}
          data-testid="world-media-image"
        />
      )}
      {media.caption ? <figcaption>{media.caption}</figcaption> : null}
      {media.kind === "video" && media.muted && !soundOn ? (
        <button
          type="button"
          className="dw-button dw-button-small dw-button-ghost dw-media-sound"
          onClick={() => {
            setSoundOn(true);
            void videoRef.current?.play().catch(() => undefined);
          }}
        >
          {t("openWorld.enableSound")}
        </button>
      ) : null}
    </figure>
  );
}
