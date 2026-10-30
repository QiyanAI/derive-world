// SPDX-License-Identifier: Apache-2.0
import { useEffect, useMemo, useRef, useState } from "react";
import * as THREE from "three";
import { OrbitControls } from "three/examples/jsm/controls/OrbitControls.js";
import { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";
import type { OpenWorldNode, OpenWorldState } from "../../api";
import { useI18n } from "../../i18n";
import { assetURL } from "./MediaSurface";

const WORLD_WIDTH = 1600;
const WORLD_HEIGHT = 1000;
const PLAYER_RADIUS = 0.45;

type SceneView = {
  kind_id: string;
  primitive: string;
  model_asset_id?: string;
  scale: number;
  elevation: number;
  hue: number;
  opacity: number;
  collider: string;
  label: string;
  hide_done: boolean;
};

type EntityAction = { entity_id: string; behavior_id: string; label?: string };

type SceneProps = {
  title: string;
  sky_hue: number;
  ground_hue: number;
  fog: number;
  camera_mode: "orbit" | "follow";
  camera_distance: number;
  camera_height: number;
  fov: number;
  gravity: number;
  ambient_light: number;
  player_enabled: boolean;
  player_node_id: string;
  player_speed: number;
  player_height: number;
  views: SceneView[];
  show_links: boolean;
  activate_behavior_id: string;
  bump_behavior_id: string;
  entity_actions: EntityAction[];
};

type PlayerState = {
  id: string;
  x: number;
  y: number;
  z: number;
  vx: number;
  vy: number;
  vz: number;
  grounded: boolean;
};

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function numberValue(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function stringValue(value: unknown, fallback = ""): string {
  return typeof value === "string" ? value : fallback;
}

function asSceneProps(value: Record<string, unknown>): SceneProps {
  const views = Array.isArray(value.views) ? value.views.map(asRecord) : [];
  const entityActions = Array.isArray(value.entity_actions)
    ? value.entity_actions.map(asRecord).map((action) => ({
        entity_id: stringValue(action.entity_id),
        behavior_id: stringValue(action.behavior_id),
        label: stringValue(action.label)
      }))
    : [];
  return {
    title: stringValue(value.title),
    sky_hue: numberValue(value.sky_hue, 220),
    ground_hue: numberValue(value.ground_hue, 140),
    fog: numberValue(value.fog, 0.02),
    camera_mode: value.camera_mode === "orbit" ? "orbit" : "follow",
    camera_distance: numberValue(value.camera_distance, 14),
    camera_height: numberValue(value.camera_height, 8),
    fov: numberValue(value.fov, 55),
    gravity: numberValue(value.gravity, 20),
    ambient_light: numberValue(value.ambient_light, 0.8),
    player_enabled: value.player_enabled !== false,
    player_node_id: stringValue(value.player_node_id),
    player_speed: numberValue(value.player_speed, 4),
    player_height: numberValue(value.player_height, 1),
    views: views.map((view) => ({
      kind_id: stringValue(view.kind_id),
      primitive: stringValue(view.primitive, "sphere"),
      model_asset_id: stringValue(view.model_asset_id),
      scale: numberValue(view.scale, 1),
      elevation: numberValue(view.elevation, 0),
      hue: numberValue(view.hue, 200),
      opacity: numberValue(view.opacity, 0.95),
      collider: stringValue(view.collider, "none"),
      label: stringValue(view.label, "always"),
      hide_done: view.hide_done === true
    })),
    show_links: value.show_links !== false,
    activate_behavior_id: stringValue(value.activate_behavior_id),
    bump_behavior_id: stringValue(value.bump_behavior_id),
    entity_actions: entityActions.filter((action) => action.entity_id && action.behavior_id)
  };
}

function worldPosition(node: OpenWorldNode): { x: number; z: number } {
  return {
    x: (node.x - WORLD_WIDTH / 2) / 60,
    z: (node.y - WORLD_HEIGHT / 2) / 60
  };
}

function geometryFor(primitive: string): THREE.BufferGeometry {
  switch (primitive) {
    case "box":
      return new THREE.BoxGeometry(1.2, 1.2, 1.2);
    case "cylinder":
      return new THREE.CylinderGeometry(0.6, 0.6, 1.6, 20);
    case "cone":
      return new THREE.ConeGeometry(0.7, 1.6, 20);
    case "torus":
      return new THREE.TorusGeometry(0.62, 0.22, 12, 28);
    case "sphere":
    default:
      return new THREE.SphereGeometry(0.62, 24, 18);
  }
}

function textSprite(text: string, hue: number): THREE.Sprite {
  const canvas = document.createElement("canvas");
  canvas.width = 256;
  canvas.height = 64;
  const context = canvas.getContext("2d");
  if (context) {
    context.clearRect(0, 0, canvas.width, canvas.height);
    context.font = "600 26px system-ui, sans-serif";
    context.textAlign = "center";
    context.textBaseline = "middle";
    context.fillStyle = "rgba(255,255,255,0.94)";
    context.fillText(text.slice(0, 24), canvas.width / 2, canvas.height / 2);
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  const material = new THREE.SpriteMaterial({
    map: texture,
    transparent: true,
    depthWrite: false
  });
  const sprite = new THREE.Sprite(material);
  sprite.scale.set(3.2, 0.8, 1);
  sprite.userData.hue = hue;
  return sprite;
}

type Collider = {
  entityID: string;
  box: THREE.Box3;
};

export default function Scene3DSurface({
  props,
  world,
  assetBase,
  canContribute,
  busy,
  onInvoke,
  nodeID
}: {
  props: Record<string, unknown>;
  world: OpenWorldState;
  assetBase: string;
  canContribute: boolean;
  busy: boolean;
  onInvoke: (behaviorID: string, entityID?: string) => void;
  nodeID: string;
}) {
  const { t } = useI18n();
  const sceneProps = useMemo(() => asSceneProps(props), [props]);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const keysRef = useRef<Set<string>>(new Set());
  const jumpRef = useRef(false);
  const playerRef = useRef<PlayerState | null>(null);
  const canContributeRef = useRef(canContribute);
  const busyRef = useRef(busy);
  const invokeRef = useRef(onInvoke);
  const [webgl, setWebgl] = useState<"checking" | "ok" | "unavailable">("checking");

  canContributeRef.current = canContribute;
  busyRef.current = busy;
  invokeRef.current = onInvoke;

  const viewByKind = useMemo(() => {
    const map = new Map<string, SceneView>();
    for (const view of sceneProps.views) {
      map.set(view.kind_id, view);
    }
    return map;
  }, [sceneProps.views]);

  const visibleNodes = useMemo(
    () =>
      world.nodes.filter((node) => {
        const view = viewByKind.get(node.kind_id);
        if (!view) {
          return false;
        }
        return !(view.hide_done && node.status === "done");
      }),
    [world.nodes, viewByKind]
  );

  const entityBehaviors = useMemo(() => {
    const map = new Map<string, string>();
    for (const action of sceneProps.entity_actions) {
      map.set(action.entity_id, action.behavior_id);
    }
    return map;
  }, [sceneProps.entity_actions]);

  const structureKey = useMemo(
    () =>
      JSON.stringify({
        props,
        nodes: world.nodes.map((node) => ({
          id: node.id,
          kind: node.kind_id,
          status: node.status,
          x: node.x,
          y: node.y,
          name: node.name
        }))
      }),
    [props, world.nodes]
  );

  const playerStart = useMemo(() => {
    if (!sceneProps.player_enabled) {
      return null;
    }
    const candidate = sceneProps.player_node_id
      ? world.nodes.find((node) => node.id === sceneProps.player_node_id)
      : world.nodes[0];
    if (!candidate) {
      return null;
    }
    const position = worldPosition(candidate);
    return { id: candidate.id, ...position };
  }, [sceneProps.player_enabled, sceneProps.player_node_id, world.nodes]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return;
    }
    let renderer: THREE.WebGLRenderer;
    try {
      renderer = new THREE.WebGLRenderer({ antialias: true, alpha: false });
    } catch {
      setWebgl("unavailable");
      return;
    }
    setWebgl("ok");
    let disposed = false;
    let frame = 0;
    let modelsLoaded = 0;
    const width = Math.max(1, container.clientWidth);
    const height = Math.max(1, container.clientHeight || 420);
    renderer.setSize(width, height);
    renderer.setPixelRatio(Math.min(2, window.devicePixelRatio || 1));
    renderer.outputColorSpace = THREE.SRGBColorSpace;
    renderer.domElement.className = "dw-3d-canvas";
    renderer.domElement.setAttribute("data-testid", "world-3d-canvas");
    renderer.domElement.tabIndex = 0;
    container.appendChild(renderer.domElement);
    container!.dataset.modelsLoaded = "0";

    const scene = new THREE.Scene();
    const sky = new THREE.Color().setHSL(sceneProps.sky_hue / 360, 0.45, 0.12);
    const ground = new THREE.Color().setHSL(sceneProps.ground_hue / 360, 0.4, 0.2);
    scene.background = sky;
    if (sceneProps.fog > 0) {
      scene.fog = new THREE.FogExp2(sky.getHex(), sceneProps.fog);
    }
    const camera = new THREE.PerspectiveCamera(
      sceneProps.fov,
      width / height,
      0.1,
      300
    );
    camera.position.set(0, sceneProps.camera_height, sceneProps.camera_distance);
    const controls = new OrbitControls(camera, renderer.domElement);
    controls.enableDamping = true;
    controls.dampingFactor = 0.08;
    controls.maxPolarAngle = Math.PI * 0.49;
    controls.minDistance = 2;
    controls.maxDistance = 60;
    controls.target.set(0, 1, 0);

    scene.add(new THREE.HemisphereLight(0xffffff, 0x223344, sceneProps.ambient_light));
    const sun = new THREE.DirectionalLight(0xffffff, 1.2);
    sun.position.set(8, 14, 6);
    scene.add(sun);

    const floor = new THREE.Mesh(
      new THREE.PlaneGeometry(48, 32),
      new THREE.MeshStandardMaterial({ color: ground, roughness: 0.95, metalness: 0 })
    );
    floor.rotation.x = -Math.PI / 2;
    scene.add(floor);
    const grid = new THREE.GridHelper(48, 24, ground.getHex(), ground.getHex());
    grid.position.y = 0.01;
    (grid.material as THREE.Material).opacity = 0.18;
    (grid.material as THREE.Material).transparent = true;
    scene.add(grid);

    const nodeByID = new Map(world.nodes.map((node) => [node.id, node]));
    if (sceneProps.show_links) {
      const points: number[] = [];
      for (const link of world.links) {
        const from = nodeByID.get(link.from);
        const to = nodeByID.get(link.to);
        if (!from || !to) {
          continue;
        }
        const start = worldPosition(from);
        const end = worldPosition(to);
        points.push(start.x, 0.15, start.z, end.x, 0.15, end.z);
      }
      if (points.length > 0) {
        const geometry = new THREE.BufferGeometry();
        geometry.setAttribute("position", new THREE.Float32BufferAttribute(points, 3));
        const material = new THREE.LineBasicMaterial({
          color: new THREE.Color().setHSL(sceneProps.sky_hue / 360, 0.7, 0.55),
          transparent: true,
          opacity: 0.35
        });
        scene.add(new THREE.LineSegments(geometry, material));
      }
    }

    const colliders: Collider[] = [];
    const interactive: THREE.Object3D[] = [];
    const gltfLoader = new GLTFLoader();
    const disposables: Array<THREE.BufferGeometry | THREE.Material | THREE.Texture> = [];

    for (const node of visibleNodes) {
      const view = viewByKind.get(node.kind_id);
      if (!view) {
        continue;
      }
      const position = worldPosition(node);
      const group = new THREE.Group();
      group.position.set(position.x, view.elevation, position.z);
      group.userData.entityID = node.id;
      const color = new THREE.Color().setHSL(view.hue / 360, 0.62, 0.56);
      let mesh: THREE.Mesh | null = null;
      if (view.primitive === "model" && view.model_asset_id) {
        const url = assetURL(assetBase, view.model_asset_id);
        if (url) {
          gltfLoader.load(
            url,
            (gltf) => {
              if (disposed) {
                return;
              }
              const model = gltf.scene;
              const bounds = new THREE.Box3().setFromObject(model);
              const size = bounds.getSize(new THREE.Vector3());
              const largest = Math.max(size.x, size.y, size.z, 0.001);
              const scale = (1.6 * view.scale) / largest;
              model.scale.setScalar(scale);
              const center = bounds.getCenter(new THREE.Vector3());
              model.position.sub(center.multiplyScalar(scale));
              model.position.y += (size.y * scale) / 2;
              group.add(model);
              if (mesh) {
                group.remove(mesh);
              }
              interactive.push(model);
              modelsLoaded += 1;
              container!.dataset.modelsLoaded = String(modelsLoaded);
              if (view.collider !== "none") {
                const colliderBox = new THREE.Box3().setFromObject(model);
                colliders.push({ entityID: node.id, box: colliderBox });
              }
            },
            undefined,
            () => {
              // A broken model keeps the primitive placeholder; it never
              // blocks the rest of the world.
            }
          );
        }
      }
      const material = new THREE.MeshStandardMaterial({
        color,
        roughness: 0.55,
        metalness: 0.15,
        transparent: view.opacity < 1,
        opacity: view.opacity,
        emissive: color.clone().multiplyScalar(0.12)
      });
      mesh = new THREE.Mesh(geometryFor(view.primitive), material);
      mesh.scale.setScalar(view.scale);
      group.add(mesh);
      interactive.push(mesh);
      disposables.push(mesh.geometry, material);
      if (view.label !== "hidden") {
        const sprite = textSprite(node.name, view.hue);
        sprite.position.set(0, 1.5 * view.scale, 0);
        group.add(sprite);
        if (sprite.material.map) {
          disposables.push(sprite.material.map);
        }
        disposables.push(sprite.material);
      }
      scene.add(group);
      if (view.collider !== "none") {
        group.updateMatrixWorld(true);
        const bounds = new THREE.Box3().setFromObject(mesh);
        colliders.push({ entityID: node.id, box: bounds });
      }
    }

    let playerMesh: THREE.Mesh | null = null;
    let playerPosition = new THREE.Vector3(0, 0.5, 0);
    let playerVelocity = new THREE.Vector3();
    let grounded = true;
    let playerID = "";
    if (playerStart) {
      playerID = playerStart.id;
      const saved = playerRef.current;
      if (saved && saved.id === playerID) {
        playerPosition.set(saved.x, saved.y, saved.z);
        playerVelocity.set(saved.vx, saved.vy, saved.vz);
        grounded = saved.grounded;
      } else {
        playerPosition.set(playerStart.x, sceneProps.player_height / 2, playerStart.z);
      }
      const playerMaterial = new THREE.MeshStandardMaterial({
        color: new THREE.Color().setHSL(0.12, 0.9, 0.65),
        emissive: new THREE.Color().setHSL(0.12, 0.9, 0.25),
        roughness: 0.35
      });
      const playerGeometry = new THREE.SphereGeometry(PLAYER_RADIUS, 20, 16);
      playerMesh = new THREE.Mesh(playerGeometry, playerMaterial);
      playerMesh.position.copy(playerPosition);
      scene.add(playerMesh);
      disposables.push(playerGeometry, playerMaterial);
      controls.target.set(playerPosition.x, playerPosition.y + 1, playerPosition.z);
      camera.position.set(
        playerPosition.x,
        playerPosition.y + sceneProps.camera_height,
        playerPosition.z + sceneProps.camera_distance
      );
      controls.update();
    }

    let lastTarget = new THREE.Vector3().copy(controls.target);
    const bumped = new Set<string>();
    function invokeFor(entityID: string, behaviorID: string) {
      if (!canContributeRef.current || busyRef.current) {
        return;
      }
      invokeRef.current(behaviorID, entityID);
    }

    let pointerStart = { x: 0, y: 0 };
    const ray = new THREE.Raycaster();
    const pointer = new THREE.Vector2();
    function onPointerDown(event: PointerEvent) {
      pointerStart = { x: event.clientX, y: event.clientY };
      renderer.domElement.focus();
    }
    function onPointerUp(event: PointerEvent) {
      if (Math.abs(event.clientX - pointerStart.x) + Math.abs(event.clientY - pointerStart.y) > 6) {
        return;
      }
      const rect = renderer.domElement.getBoundingClientRect();
      pointer.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
      pointer.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;
      ray.setFromCamera(pointer, camera);
      const hits = ray.intersectObjects(interactive, true);
      for (const hit of hits) {
        let target: THREE.Object3D | null = hit.object;
        while (target && !target.userData.entityID) {
          target = target.parent;
        }
        const entityID = stringValue(target?.userData.entityID);
        if (!entityID) {
          continue;
        }
        const behaviorID =
          entityBehaviors.get(entityID) || sceneProps.activate_behavior_id;
        if (behaviorID) {
          invokeFor(entityID, behaviorID);
        }
        return;
      }
    }
    renderer.domElement.addEventListener("pointerdown", onPointerDown);
    renderer.domElement.addEventListener("pointerup", onPointerUp);

    function onKeyDown(event: KeyboardEvent) {
      if (!playerStart || !container!.contains(document.activeElement)) {
        return;
      }
      if (["KeyW", "KeyA", "KeyS", "KeyD", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "Space"].includes(event.code)) {
        event.preventDefault();
        keysRef.current.add(event.code);
      }
    }
    function onKeyUp(event: KeyboardEvent) {
      keysRef.current.delete(event.code);
    }
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("keyup", onKeyUp);

    const resize = new ResizeObserver(() => {
      if (disposed) {
        return;
      }
      const nextWidth = Math.max(1, container.clientWidth);
      const nextHeight = Math.max(1, container.clientHeight || 420);
      renderer.setSize(nextWidth, nextHeight);
      camera.aspect = nextWidth / nextHeight;
      camera.updateProjectionMatrix();
    });
    resize.observe(container);

    const clock = new THREE.Clock();
    let frameCount = 0;
    function animate() {
      if (disposed) {
        return;
      }
      frame = requestAnimationFrame(animate);
      const delta = Math.min(0.05, Math.max(0, clock.getDelta()));
      if (playerStart && playerMesh) {
        const forward = new THREE.Vector3();
        camera.getWorldDirection(forward);
        forward.y = 0;
        if (forward.lengthSq() < 0.0001) {
          forward.set(0, 0, -1);
        }
        forward.normalize();
        const right = new THREE.Vector3().crossVectors(forward, new THREE.Vector3(0, 1, 0)).normalize();
        const keys = keysRef.current;
        const direction = new THREE.Vector3();
        if (keys.has("KeyW") || keys.has("ArrowUp")) {
          direction.add(forward);
        }
        if (keys.has("KeyS") || keys.has("ArrowDown")) {
          direction.sub(forward);
        }
        if (keys.has("KeyD") || keys.has("ArrowRight")) {
          direction.add(right);
        }
        if (keys.has("KeyA") || keys.has("ArrowLeft")) {
          direction.sub(right);
        }
        if (direction.lengthSq() > 0) {
          direction.normalize();
        }
        const speed = sceneProps.player_speed;
        playerVelocity.x = direction.x * speed;
        playerVelocity.z = direction.z * speed;
        if ((keys.has("Space") || jumpRef.current) && grounded) {
          playerVelocity.y = 6.6;
          grounded = false;
        }
        jumpRef.current = false;
        playerVelocity.y -= sceneProps.gravity * delta;
        playerPosition.addScaledVector(playerVelocity, delta);
        const halfHeight = Math.max(0.25, sceneProps.player_height / 2);
        if (playerPosition.y <= halfHeight) {
          playerPosition.y = halfHeight;
          playerVelocity.y = 0;
          grounded = true;
        }
        playerPosition.x = Math.max(-18, Math.min(18, playerPosition.x));
        playerPosition.z = Math.max(-12, Math.min(12, playerPosition.z));
        for (const collider of colliders) {
          if (playerStart && collider.entityID === playerStart.id) {
            continue;
          }
          const closest = collider.box.clampPoint(
            playerPosition,
            new THREE.Vector3()
          );
          const offset = playerPosition.clone().sub(closest);
          const distance = offset.length();
          if (distance < PLAYER_RADIUS) {
            if (distance < 0.0001) {
              offset.set(1, 0, 0);
            } else {
              offset.normalize();
            }
            playerPosition.addScaledVector(offset, PLAYER_RADIUS - distance + 0.001);
            if (sceneProps.bump_behavior_id && !bumped.has(collider.entityID)) {
              bumped.add(collider.entityID);
              invokeFor(collider.entityID, sceneProps.bump_behavior_id);
            }
          }
        }
        playerMesh.position.copy(playerPosition);
        if (sceneProps.camera_mode === "follow") {
          const nextTarget = new THREE.Vector3(
            playerPosition.x,
            playerPosition.y + 1,
            playerPosition.z
          );
          const deltaTarget = nextTarget.clone().sub(lastTarget);
          camera.position.add(deltaTarget);
          controls.target.copy(nextTarget);
          lastTarget = nextTarget;
        }
        playerRef.current = {
          id: playerID,
          x: playerPosition.x,
          y: playerPosition.y,
          z: playerPosition.z,
          vx: playerVelocity.x,
          vy: playerVelocity.y,
          vz: playerVelocity.z,
          grounded
        };
        if (frameCount % 6 === 0) {
          container!.dataset.playerX = playerPosition.x.toFixed(2);
          container!.dataset.playerY = playerPosition.y.toFixed(2);
          container!.dataset.playerZ = playerPosition.z.toFixed(2);
          container!.dataset.playerGrounded = grounded ? "true" : "false";
        }
      }
      controls.update();
      renderer.render(scene, camera);
      frameCount += 1;
    }
    animate();

    return () => {
      disposed = true;
      cancelAnimationFrame(frame);
      resize.disconnect();
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("keyup", onKeyUp);
      renderer.domElement.removeEventListener("pointerdown", onPointerDown);
      renderer.domElement.removeEventListener("pointerup", onPointerUp);
      controls.dispose();
      for (const item of disposables) {
        item.dispose();
      }
      scene.traverse((object) => {
        if (object instanceof THREE.Mesh || object instanceof THREE.LineSegments) {
          object.geometry?.dispose();
          if (Array.isArray(object.material)) {
            object.material.forEach((material) => material.dispose());
          } else {
            object.material?.dispose();
          }
        }
      });
      renderer.dispose();
      renderer.domElement.remove();
    };
  }, [
    structureKey,
    assetBase,
    sceneProps.activate_behavior_id,
    sceneProps.bump_behavior_id,
    sceneProps.camera_distance,
    sceneProps.camera_height,
    sceneProps.camera_mode,
    sceneProps.fov,
    sceneProps.fog,
    sceneProps.gravity,
    sceneProps.ground_hue,
    sceneProps.ambient_light,
    sceneProps.player_enabled,
    sceneProps.player_height,
    sceneProps.player_node_id,
    sceneProps.player_speed,
    sceneProps.show_links,
    sceneProps.sky_hue,
    playerStart,
    visibleNodes,
    viewByKind,
    entityBehaviors,
    world.links,
    world.nodes
  ]);

  function pressKey(code: string, pressed: boolean) {
    if (pressed) {
      keysRef.current.add(code);
    } else {
      keysRef.current.delete(code);
    }
  }

  return (
    <section
      className="dw-3d"
      data-testid="world-3d"
      data-capability-node={nodeID}
      data-webgl={webgl}
      data-entities={visibleNodes.length}
      ref={containerRef}
      aria-label={sceneProps.title || t("openWorld.scene3d")}
    >
      {webgl === "unavailable" ? (
        <div className="dw-3d-fallback" data-testid="world-3d-unavailable">
          <p>{t("openWorld.scene3dUnavailable")}</p>
          <ul>
            {visibleNodes.map((node) => (
              <li key={node.id}>{node.name}</li>
            ))}
          </ul>
        </div>
      ) : null}
      {sceneProps.player_enabled ? (
        <div className="dw-3d-touch" data-testid="world-3d-touch">
          <button type="button" aria-label={t("openWorld.moveForward")} onPointerDown={() => pressKey("KeyW", true)} onPointerUp={() => pressKey("KeyW", false)} onPointerLeave={() => pressKey("KeyW", false)}>▲</button>
          <button type="button" aria-label={t("openWorld.moveLeft")} onPointerDown={() => pressKey("KeyA", true)} onPointerUp={() => pressKey("KeyA", false)} onPointerLeave={() => pressKey("KeyA", false)}>◀</button>
          <button type="button" aria-label={t("openWorld.jump")} onPointerDown={() => { jumpRef.current = true; }}>●</button>
          <button type="button" aria-label={t("openWorld.moveRight")} onPointerDown={() => pressKey("KeyD", true)} onPointerUp={() => pressKey("KeyD", false)} onPointerLeave={() => pressKey("KeyD", false)}>▶</button>
          <button type="button" aria-label={t("openWorld.moveBack")} onPointerDown={() => pressKey("KeyS", true)} onPointerUp={() => pressKey("KeyS", false)} onPointerLeave={() => pressKey("KeyS", false)}>▼</button>
        </div>
      ) : null}
      <ul className="dw-3d-entities dw-visually-hidden">
        {visibleNodes.map((node) => {
          const behaviorID =
            entityBehaviors.get(node.id) || sceneProps.activate_behavior_id;
          return (
            <li key={node.id}>
              <button
                type="button"
                data-testid="world-3d-entity"
                data-entity-id={node.id}
                disabled={!canContribute || busy || !behaviorID}
                onClick={() => {
                  if (behaviorID) {
                    onInvoke(behaviorID, node.id);
                  }
                }}
              >
                {node.name}
              </button>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
