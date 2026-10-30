// SPDX-License-Identifier: Apache-2.0
import { useState, type FormEvent } from "react";
import type {
  OpenWorldAppearance,
  OpenWorldBinding,
  OpenWorldBindingTarget,
  OpenWorldKind,
  OpenWorldLinkStyle,
  OpenWorldPresentation,
  OpenWorldState
} from "../../api";
import { useI18n } from "../../i18n";

const shapeOptions = ["orb", "beacon", "wave", "shard", "ring", "glyph", "bloom", "thread"] as const;
const motionOptions = ["none", "pulse", "float", "sway", "flicker", "breathe", "orbit"] as const;
const labelOptions = ["always", "hover", "hidden"] as const;
const layerOptions = ["back", "mid", "front"] as const;
const backdropOptions = ["horizon", "void", "loom"] as const;
const ambientOptions = ["none", "stars", "motes", "rain", "embers"] as const;
const linkOptions = ["thread", "arc", "orbit", "hidden"] as const;
const bindingTargets = ["scale", "glow", "opacity", "hue_shift", "speed"] as const;

function targetDefaults(target: OpenWorldBindingTarget): { min: number; max: number } {
  switch (target) {
    case "scale":
      return { min: 0.8, max: 1.6 };
    case "glow":
      return { min: 15, max: 95 };
    case "opacity":
      return { min: 0.35, max: 1 };
    case "hue_shift":
      return { min: -30, max: 30 };
    case "speed":
      return { min: 0.6, max: 1.8 };
  }
}

export default function WorldLookEditor({
  world,
  busy,
  canContribute,
  onSetPresentation,
  onUpdateKind
}: {
  world: OpenWorldState;
  busy: boolean;
  canContribute: boolean;
  onSetPresentation: (presentation: OpenWorldPresentation) => void;
  onUpdateKind: (kindID: string, appearance: OpenWorldAppearance, bindings: OpenWorldBinding[]) => void;
}) {
  const { t } = useI18n();
  const [scene, setScene] = useState(world.presentation.scene);
  const [linkStyle, setLinkStyle] = useState<OpenWorldLinkStyle>(world.presentation.link_style);

  function submitScene(event: FormEvent) {
    event.preventDefault();
    onSetPresentation({ scene, link_style: linkStyle });
  }

  return (
    <section className="dw-open-look" data-testid="open-world-look">
      <h2>{t("openWorld.look")}</h2>
      <p className="dw-open-hint">{t("openWorld.lookHint")}</p>
      <form className="dw-open-look-form" onSubmit={submitScene}>
        <div className="dw-open-look-grid">
          <label>
            <span>{t("openWorld.backdrop")}</span>
            <select
              className="dw-input"
              value={scene.backdrop}
              disabled={!canContribute || busy}
              onChange={(event) =>
                setScene((current) => ({ ...current, backdrop: event.target.value as typeof current.backdrop }))
              }
            >
              {backdropOptions.map((option) => (
                <option key={option} value={option}>
                  {t(`openWorld.backdrop.${option}` as never)}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>{t("openWorld.linkStyle")}</span>
            <select
              className="dw-input"
              value={linkStyle}
              disabled={!canContribute || busy}
              onChange={(event) => setLinkStyle(event.target.value as OpenWorldLinkStyle)}
            >
              {linkOptions.map((option) => (
                <option key={option} value={option}>
                  {t(`openWorld.link.${option}` as never)}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>
              {t("openWorld.skyHue")} · {scene.sky_hue}
            </span>
            <input
              type="range"
              min={0}
              max={359}
              value={scene.sky_hue}
              disabled={!canContribute || busy}
              onChange={(event) => setScene((current) => ({ ...current, sky_hue: Number(event.target.value) }))}
            />
          </label>
          <label>
            <span>
              {t("openWorld.groundHue")} · {scene.ground_hue}
            </span>
            <input
              type="range"
              min={0}
              max={359}
              value={scene.ground_hue}
              disabled={!canContribute || busy}
              onChange={(event) => setScene((current) => ({ ...current, ground_hue: Number(event.target.value) }))}
            />
          </label>
          <label>
            <span>
              {t("openWorld.accentHue")} · {scene.accent_hue}
            </span>
            <input
              type="range"
              min={0}
              max={359}
              value={scene.accent_hue}
              disabled={!canContribute || busy}
              onChange={(event) => setScene((current) => ({ ...current, accent_hue: Number(event.target.value) }))}
            />
          </label>
          <label>
            <span>
              {t("openWorld.horizon")} · {scene.horizon}
            </span>
            <input
              type="range"
              min={0}
              max={100}
              value={scene.horizon}
              disabled={!canContribute || busy}
              onChange={(event) => setScene((current) => ({ ...current, horizon: Number(event.target.value) }))}
            />
          </label>
          <label>
            <span>{t("openWorld.ambient")}</span>
            <select
              className="dw-input"
              value={scene.ambient}
              disabled={!canContribute || busy}
              onChange={(event) =>
                setScene((current) => ({ ...current, ambient: event.target.value as typeof current.ambient }))
              }
            >
              {ambientOptions.map((option) => (
                <option key={option} value={option}>
                  {t(`openWorld.ambient.${option}` as never)}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>
              {t("openWorld.ambientDensity")} · {scene.ambient_density}
            </span>
            <input
              type="range"
              min={0}
              max={100}
              value={scene.ambient_density}
              disabled={!canContribute || busy}
              onChange={(event) =>
                setScene((current) => ({ ...current, ambient_density: Number(event.target.value) }))
              }
            />
          </label>
        </div>
        {canContribute ? (
          <button className="dw-button dw-button-small dw-button-primary" type="submit" disabled={busy}>
            {t("openWorld.saveLook")}
          </button>
        ) : null}
      </form>

      <div className="dw-open-kind-looks">
        <h3>{t("openWorld.kindLooks")}</h3>
        {world.kinds.map((kind) => (
          <KindLook key={kind.id} kind={kind} busy={busy} canContribute={canContribute} onUpdateKind={onUpdateKind} />
        ))}
      </div>
    </section>
  );
}

function KindLook({
  kind,
  busy,
  canContribute,
  onUpdateKind
}: {
  kind: OpenWorldKind;
  busy: boolean;
  canContribute: boolean;
  onUpdateKind: (kindID: string, appearance: OpenWorldAppearance, bindings: OpenWorldBinding[]) => void;
}) {
  const { t } = useI18n();
  const [appearance, setAppearance] = useState<OpenWorldAppearance>(kind.appearance);
  const [bindings, setBindings] = useState<OpenWorldBinding[]>(kind.bindings);
  const [bindingField, setBindingField] = useState("");
  const [bindingTarget, setBindingTarget] = useState<OpenWorldBindingTarget>("glow");
  const [inMin, setInMin] = useState("0");
  const [inMax, setInMax] = useState("10");
  const [outMin, setOutMin] = useState("15");
  const [outMax, setOutMax] = useState("95");

  const bindableFields = kind.fields.filter((field) => field.type !== "text");

  function chooseTarget(target: OpenWorldBindingTarget) {
    setBindingTarget(target);
    const defaults = targetDefaults(target);
    setOutMin(String(defaults.min));
    setOutMax(String(defaults.max));
  }

  function addBinding() {
    if (!bindingField || bindings.some((binding) => binding.target === bindingTarget)) {
      return;
    }
    const next: OpenWorldBinding = {
      field: bindingField,
      target: bindingTarget,
      in_min: Number(inMin),
      in_max: Number(inMax),
      out_min: Number(outMin),
      out_max: Number(outMax)
    };
    if (
      !Number.isFinite(next.in_min) ||
      !Number.isFinite(next.in_max) ||
      !Number.isFinite(next.out_min) ||
      !Number.isFinite(next.out_max) ||
      next.in_max <= next.in_min
    ) {
      return;
    }
    setBindings((current) => [...current, next]);
    setBindingField("");
  }

  return (
    <details className="dw-open-kind-look" data-testid={`world-kind-look-${kind.id}`}>
      <summary>
        <i style={{ background: `hsl(${kind.hue} 62% 58%)` }} />
        {kind.name}
      </summary>
      <div className="dw-open-look-grid">
        <label>
          <span>{t("openWorld.shape")}</span>
          <select
            className="dw-input"
            value={appearance.shape}
            disabled={!canContribute || busy}
            onChange={(event) =>
              setAppearance((current) => ({ ...current, shape: event.target.value as typeof current.shape }))
            }
          >
            {shapeOptions.map((option) => (
              <option key={option} value={option}>
                {t(`openWorld.shape.${option}` as never)}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>{t("openWorld.motion")}</span>
          <select
            className="dw-input"
            value={appearance.motion}
            disabled={!canContribute || busy}
            onChange={(event) =>
              setAppearance((current) => ({ ...current, motion: event.target.value as typeof current.motion }))
            }
          >
            {motionOptions.map((option) => (
              <option key={option} value={option}>
                {t(`openWorld.motion.${option}` as never)}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>
            {t("openWorld.size")} · {Math.round(appearance.size)}
          </span>
          <input
            type="range"
            min={12}
            max={200}
            value={appearance.size}
            disabled={!canContribute || busy}
            onChange={(event) => setAppearance((current) => ({ ...current, size: Number(event.target.value) }))}
          />
        </label>
        <label>
          <span>
            {t("openWorld.glow")} · {appearance.glow}
          </span>
          <input
            type="range"
            min={0}
            max={100}
            value={appearance.glow}
            disabled={!canContribute || busy}
            onChange={(event) => setAppearance((current) => ({ ...current, glow: Number(event.target.value) }))}
          />
        </label>
        <label>
          <span>
            {t("openWorld.intensity")} · {appearance.intensity}
          </span>
          <input
            type="range"
            min={0}
            max={100}
            value={appearance.intensity}
            disabled={!canContribute || busy}
            onChange={(event) =>
              setAppearance((current) => ({ ...current, intensity: Number(event.target.value) }))
            }
          />
        </label>
        <label>
          <span>{t("openWorld.labelMode")}</span>
          <select
            className="dw-input"
            value={appearance.label}
            disabled={!canContribute || busy}
            onChange={(event) =>
              setAppearance((current) => ({ ...current, label: event.target.value as typeof current.label }))
            }
          >
            {labelOptions.map((option) => (
              <option key={option} value={option}>
                {t(`openWorld.labelMode.${option}` as never)}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>{t("openWorld.layer")}</span>
          <select
            className="dw-input"
            value={appearance.layer}
            disabled={!canContribute || busy}
            onChange={(event) =>
              setAppearance((current) => ({ ...current, layer: event.target.value as typeof current.layer }))
            }
          >
            {layerOptions.map((option) => (
              <option key={option} value={option}>
                {t(`openWorld.layer.${option}` as never)}
              </option>
            ))}
          </select>
        </label>
      </div>

      <div className="dw-open-bindings">
        <p className="dw-open-hint">{t("openWorld.bindingsHint")}</p>
        <ul className="dw-open-binding-list">
          {bindings.map((binding) => (
            <li key={binding.target}>
              <code>
                {binding.field} → {binding.target} [{binding.in_min}..{binding.in_max}] → [{binding.out_min}..
                {binding.out_max}]
              </code>
              {canContribute ? (
                <button
                  type="button"
                  aria-label={t("openWorld.remove")}
                  disabled={busy}
                  onClick={() => setBindings((current) => current.filter((entry) => entry.target !== binding.target))}
                >
                  ×
                </button>
              ) : null}
            </li>
          ))}
        </ul>
        {canContribute && bindableFields.length > 0 ? (
          <div className="dw-open-binding-add">
            <select className="dw-input" value={bindingField} onChange={(event) => setBindingField(event.target.value)}>
              <option value="">{t("openWorld.bindingField")}</option>
              {bindableFields.map((field) => (
                <option key={field.key} value={field.key}>
                  {field.label || field.key}
                </option>
              ))}
            </select>
            <select
              className="dw-input"
              value={bindingTarget}
              onChange={(event) => chooseTarget(event.target.value as OpenWorldBindingTarget)}
            >
              {bindingTargets.map((target) => (
                <option key={target} value={target}>
                  {t(`openWorld.target.${target}` as never)}
                </option>
              ))}
            </select>
            <input
              className="dw-input"
              type="number"
              aria-label={t("openWorld.inMin")}
              value={inMin}
              onChange={(event) => setInMin(event.target.value)}
            />
            <input
              className="dw-input"
              type="number"
              aria-label={t("openWorld.inMax")}
              value={inMax}
              onChange={(event) => setInMax(event.target.value)}
            />
            <input
              className="dw-input"
              type="number"
              aria-label={t("openWorld.outMin")}
              value={outMin}
              onChange={(event) => setOutMin(event.target.value)}
            />
            <input
              className="dw-input"
              type="number"
              aria-label={t("openWorld.outMax")}
              value={outMax}
              onChange={(event) => setOutMax(event.target.value)}
            />
            <button
              className="dw-button dw-button-small dw-button-ghost"
              type="button"
              disabled={!bindingField || bindings.some((binding) => binding.target === bindingTarget)}
              onClick={addBinding}
            >
              {t("openWorld.addBinding")}
            </button>
          </div>
        ) : null}
      </div>

      {canContribute ? (
        <button
          className="dw-button dw-button-small dw-button-primary"
          type="button"
          disabled={busy}
          onClick={() => onUpdateKind(kind.id, appearance, bindings)}
        >
          {t("openWorld.saveLook")}
        </button>
      ) : null}
    </details>
  );
}
