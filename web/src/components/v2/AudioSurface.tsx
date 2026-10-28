// SPDX-License-Identifier: Apache-2.0
import { useEffect, useMemo, useRef, useState } from "react";
import type { OpenWorldState } from "../../api";
import { useI18n } from "../../i18n";
import { assetURL } from "./MediaSurface";

type Tone = {
  frequency?: number;
  wave?: string;
  volume?: number;
  pulse?: number;
};

type Cue = {
  variable: string;
  mode: string;
  asset_id?: string;
  tone?: Tone;
  volume?: number;
};

type AudioProps = {
  title: string;
  ambient_asset_id: string;
  tone?: Tone;
  loop: boolean;
  volume: number;
  muted: boolean;
  cues: Cue[];
};

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function tone(value: unknown): Tone | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return undefined;
  }
  const input = value as Record<string, unknown>;
  return {
    frequency: typeof input.frequency === "number" ? input.frequency : undefined,
    wave: typeof input.wave === "string" ? input.wave : undefined,
    volume: typeof input.volume === "number" ? input.volume : undefined,
    pulse: typeof input.pulse === "number" ? input.pulse : undefined
  };
}

function asAudioProps(value: Record<string, unknown>): AudioProps {
  const cues = Array.isArray(value.cues)
    ? value.cues.map(record).map((cue) => ({
        variable: typeof cue.variable === "string" ? cue.variable : "",
        mode: typeof cue.mode === "string" ? cue.mode : "change",
        asset_id: typeof cue.asset_id === "string" ? cue.asset_id : "",
        tone: tone(cue.tone),
        volume: typeof cue.volume === "number" ? cue.volume : undefined
      }))
    : [];
  return {
    title: typeof value.title === "string" ? value.title : "",
    ambient_asset_id:
      typeof value.ambient_asset_id === "string" ? value.ambient_asset_id : "",
    tone: tone(value.tone),
    loop: value.loop !== false,
    volume: typeof value.volume === "number" ? value.volume : 0.5,
    muted: value.muted === true,
    cues
  };
}

const EMPTY_VARIABLES: Record<string, string | number | boolean> = {};

let sharedContext: AudioContext | null = null;

function audioContext(): AudioContext | null {
  if (typeof window === "undefined") {
    return null;
  }
  const Context =
    window.AudioContext ??
    (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
  if (!Context) {
    return null;
  }
  if (!sharedContext) {
    sharedContext = new Context();
  }
  return sharedContext;
}

function wave(value: string | undefined): OscillatorType {
  switch (value) {
    case "square":
    case "triangle":
    case "sawtooth":
      return value;
    default:
      return "sine";
  }
}

function startAmbientTone(context: AudioContext, toneValue: Tone, volume: number): () => void {
  const oscillator = context.createOscillator();
  const gain = context.createGain();
  oscillator.type = wave(toneValue.wave);
  oscillator.frequency.value = Math.max(40, Math.min(8000, toneValue.frequency ?? 280));
  const toneVolume = Math.max(0, Math.min(0.5, toneValue.volume ?? 0.08));
  gain.gain.value = toneVolume * Math.max(0, Math.min(1, volume));
  oscillator.connect(gain);
  gain.connect(context.destination);
  oscillator.start();
  const pulse = Math.max(0, Math.min(4, toneValue.pulse ?? 0));
  let lfo: OscillatorNode | null = null;
  if (pulse > 0) {
    const depth = context.createGain();
    depth.gain.value = Math.min(0.02, toneVolume * 0.35);
    lfo = context.createOscillator();
    lfo.frequency.value = pulse;
    lfo.connect(depth);
    depth.connect(gain.gain);
    lfo.start();
  }
  return () => {
    try {
      oscillator.stop();
      lfo?.stop();
    } catch {
      // Already stopped.
    }
    oscillator.disconnect();
    gain.disconnect();
  };
}

function playCueTone(context: AudioContext, toneValue: Tone, volume: number): void {
  const oscillator = context.createOscillator();
  const gain = context.createGain();
  oscillator.type = wave(toneValue.wave);
  oscillator.frequency.value = Math.max(40, Math.min(8000, toneValue.frequency ?? 440));
  const peak = Math.max(
    0.001,
    Math.min(0.5, (toneValue.volume ?? 0.12) * Math.max(0, Math.min(1, volume)))
  );
  const now = context.currentTime;
  gain.gain.setValueAtTime(peak, now);
  gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.35);
  oscillator.connect(gain);
  gain.connect(context.destination);
  oscillator.start(now);
  oscillator.stop(now + 0.38);
  oscillator.addEventListener("ended", () => {
    oscillator.disconnect();
    gain.disconnect();
  });
}

function cueFires(mode: string, current: unknown, previous: unknown): boolean {
  switch (mode) {
    case "truthy":
      return current === true && previous !== true;
    case "falsy":
      return current !== true && previous === true;
    case "increase":
      return (
        typeof current === "number" &&
        typeof previous === "number" &&
        current > previous
      );
    case "decrease":
      return (
        typeof current === "number" &&
        typeof previous === "number" &&
        current < previous
      );
    case "change":
    default:
      return current !== previous;
  }
}

export default function AudioSurface({
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
  const audio = useMemo(() => asAudioProps(props), [props]);
  const [enabled, setEnabled] = useState(false);
  const [muted, setMuted] = useState(audio.muted);
  const [cuesFired, setCuesFired] = useState(0);
  const [playing, setPlaying] = useState(false);
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const stopToneRef = useRef<(() => void) | null>(null);
  const contextRef = useRef<AudioContext | null>(null);
  const variables = world.variables ?? EMPTY_VARIABLES;
  const previousRef = useRef<Record<string, unknown>>({ ...variables });
  const ambientURL = assetURL(assetBase, audio.ambient_asset_id);
  const ambientKind = ambientURL ? "file" : audio.tone ? "tone" : "none";

  useEffect(() => {
    setMuted(audio.muted);
  }, [audio.muted, nodeID]);

  async function enable(): Promise<void> {
    const context = audioContext();
    contextRef.current = context;
    if (context) {
      try {
        await context.resume();
      } catch {
        // A browser may still refuse; the controls remain visible.
      }
    }
    setEnabled(true);
    setMuted(false);
  }

  // Ambient file playback follows the enabled/muted/volume state.
  useEffect(() => {
    const element = audioRef.current;
    if (!element || !ambientURL || !enabled || muted) {
      element?.pause();
      return;
    }
    element.volume = Math.max(0, Math.min(1, audio.volume));
    element.loop = audio.loop !== false;
    void element.play().catch(() => {
      // The browser can refuse a programmatic play; the enable control stays.
    });
  }, [ambientURL, audio.loop, audio.volume, enabled, muted]);

  useEffect(() => {
    if (!ambientURL) {
      return;
    }
    const element = new Audio(ambientURL);
    element.preload = "auto";
    element.loop = audio.loop !== false;
    element.addEventListener("play", () => setPlaying(true));
    element.addEventListener("pause", () => setPlaying(false));
    audioRef.current = element;
    return () => {
      element.pause();
      element.removeAttribute("src");
      element.load();
      audioRef.current = null;
    };
  }, [ambientURL, audio.loop]);

  // Procedural ambient tone.
  useEffect(() => {
    if (!enabled || muted || ambientURL || !audio.tone) {
      return;
    }
    const context = contextRef.current ?? audioContext();
    contextRef.current = context;
    if (!context) {
      return;
    }
    stopToneRef.current = startAmbientTone(context, audio.tone, audio.volume);
    return () => {
      stopToneRef.current?.();
      stopToneRef.current = null;
    };
  }, [enabled, muted, ambientURL, audio.tone, audio.volume]);

  // Cues fire on real world-variable transitions, never on initial mount.
  useEffect(() => {
    const previous = previousRef.current;
    let fired = false;
    if (enabled && !muted) {
      for (const cue of audio.cues) {
        if (!cue.variable) {
          continue;
        }
        const current = variables[cue.variable];
        if (!cueFires(cue.mode, current, previous[cue.variable])) {
          continue;
        }
        fired = true;
        const cueVolume = Math.max(0, Math.min(1, cue.volume ?? 0.7));
        if (cue.asset_id) {
          const url = assetURL(assetBase, cue.asset_id);
          if (url) {
            const element = new Audio(url);
            element.volume = cueVolume;
            void element.play().catch(() => undefined);
            element.addEventListener("ended", () => {
              element.removeAttribute("src");
              element.load();
            });
          }
        } else if (cue.tone) {
          const context = contextRef.current ?? audioContext();
          if (context) {
            playCueTone(context, cue.tone, cueVolume);
          }
        }
      }
    }
    previousRef.current = { ...variables };
    if (fired) {
      setCuesFired((value) => value + 1);
    }
  }, [variables, enabled, muted, assetBase, audio.cues]);

  return (
    <div
      className="dw-audio"
      data-testid="world-audio"
      data-capability-node={nodeID}
      data-enabled={enabled}
      data-muted={muted}
      data-ambient={ambientKind}
      data-cues={audio.cues.length}
      data-cues-fired={cuesFired}
      data-playing={playing}
    >
      <span className="dw-audio-title">{audio.title || t("openWorld.sound")}</span>
      {!enabled ? (
        <button
          type="button"
          className="dw-button dw-button-small dw-button-ghost"
          onClick={() => void enable()}
          data-testid="world-audio-enable"
        >
          {t("openWorld.enableSound")}
        </button>
      ) : (
        <button
          type="button"
          className="dw-button dw-button-small dw-button-ghost"
          aria-pressed={!muted}
          onClick={() => setMuted((value) => !value)}
          data-testid="world-audio-mute"
        >
          {muted ? t("openWorld.soundOff") : t("openWorld.soundOn")}
        </button>
      )}
    </div>
  );
}
