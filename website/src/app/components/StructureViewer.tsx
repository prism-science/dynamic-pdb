"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";

import { createPluginUI } from "molstar/lib/mol-plugin-ui";
import { DefaultPluginUISpec } from "molstar/lib/mol-plugin-ui/spec";
import type { PluginUIContext } from "molstar/lib/mol-plugin-ui/context";
import { PluginConfig } from "molstar/lib/mol-plugin/config";
import { setSubtreeVisibility } from "molstar/lib/mol-plugin/behavior/static/state";
import { StateTransforms } from "molstar/lib/mol-plugin-state/transforms";
import { VolumeRepresentation3DHelpers } from "molstar/lib/mol-plugin-state/transforms/representation";
import { Volume } from "molstar/lib/mol-model/volume";
import { Vec3, Mat4, Tensor } from "molstar/lib/mol-math/linear-algebra";
import { Color } from "molstar/lib/mol-util/color";

import { fetchFileURL } from "@/lib/api/ext";
import type { StructureKind, StructureMap } from "@/lib/structureKind";

import styles from "./StructureViewer.module.css";

const Empty = () => null;

// How far (Å) from a model atom density is kept — like PyMOL's `carve`.
const CARVE_RADIUS = 2;
// Extra padding (Å) around the model's bounding box for the resampled grid.
const BOX_MARGIN = 3;
// Cap on resampled grid points, to bound the cost of the resample loop.
const MAX_POINTS = 2_600_000;

// Trilinear sample of a periodic grid at fractional grid coordinates (which may
// fall outside [0,dim); indices wrap around because the map is periodic).
function sampleWrapped(
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  data: any,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  space: any,
  ox: number,
  oy: number,
  oz: number,
  gx: number,
  gy: number,
  gz: number,
): number {
  const x0 = Math.floor(gx);
  const y0 = Math.floor(gy);
  const z0 = Math.floor(gz);
  const dx = gx - x0;
  const dy = gy - y0;
  const dz = gz - z0;
  const xa = ((x0 % ox) + ox) % ox;
  const ya = ((y0 % oy) + oy) % oy;
  const za = ((z0 % oz) + oz) % oz;
  const xb = (xa + 1) % ox;
  const yb = (ya + 1) % oy;
  const zb = (za + 1) % oz;
  const g = space.get;
  const c000 = g(data, xa, ya, za);
  const c100 = g(data, xb, ya, za);
  const c010 = g(data, xa, yb, za);
  const c110 = g(data, xb, yb, za);
  const c001 = g(data, xa, ya, zb);
  const c101 = g(data, xb, ya, zb);
  const c011 = g(data, xa, yb, zb);
  const c111 = g(data, xb, yb, zb);
  const c00 = c000 * (1 - dx) + c100 * dx;
  const c10 = c010 * (1 - dx) + c110 * dx;
  const c01 = c001 * (1 - dx) + c101 * dx;
  const c11 = c011 * (1 - dx) + c111 * dx;
  const c0 = c00 * (1 - dy) + c10 * dy;
  const c1 = c01 * (1 - dy) + c11 * dy;
  return c0 * (1 - dz) + c1 * dz;
}

// The MTZ map is periodic over the crystallographic unit cell. The deposited
// model can sit anywhere relative to that cell (and often straddles cell
// boundaries), so a single [0,1] box never lines up. Like Coot/PyMOL, we
// resample the periodic map into a fresh cartesian grid that covers the model,
// keeping only points within `radius` Å of an atom (carve). The result hugs the
// model regardless of where it sits in the cell.
function expandMapAroundModel(
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  volume: any,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  structure: any,
  radius: number,
) {
  const grid = volume?.grid;
  const cell = grid?.transform?.cell;
  const lookup = structure?.lookup3d;
  const box = structure?.boundary?.box;
  if (!grid || !cell || !lookup || !box) {
    return;
  }

  const origData = grid.cells.data;
  const origSpace = grid.cells.space;
  const [ox, oy, oz] = origSpace.dimensions as number[];
  const mean = grid.stats.mean as number;
  const toFractional = cell.toFractional;
  const cellSize = cell.size as Vec3;

  const minX = box.min[0] - BOX_MARGIN;
  const minY = box.min[1] - BOX_MARGIN;
  const minZ = box.min[2] - BOX_MARGIN;
  const sizeX = box.max[0] - box.min[0] + 2 * BOX_MARGIN;
  const sizeY = box.max[1] - box.min[1] + 2 * BOX_MARGIN;
  const sizeZ = box.max[2] - box.min[2] + 2 * BOX_MARGIN;

  // Start from the map's own sampling, then coarsen if it would blow the cap.
  let spacing = Math.min(cellSize[0] / ox, cellSize[1] / oy, cellSize[2] / oz);
  if (!Number.isFinite(spacing) || spacing <= 0) {
    spacing = 0.5;
  }
  let nx = 0;
  let ny = 0;
  let nz = 0;
  for (let guard = 0; guard < 12; guard++) {
    nx = Math.max(2, Math.ceil(sizeX / spacing) + 1);
    ny = Math.max(2, Math.ceil(sizeY / spacing) + 1);
    nz = Math.max(2, Math.ceil(sizeZ / spacing) + 1);
    if (nx * ny * nz <= MAX_POINTS) {
      break;
    }
    spacing *= 1.26;
  }
  const stepX = sizeX / (nx - 1);
  const stepY = sizeY / (ny - 1);
  const stepZ = sizeZ / (nz - 1);

  const newData = new Float32Array(nx * ny * nz);
  newData.fill(mean);
  const newSpace = Tensor.Space([nx, ny, nz], [0, 1, 2], Float32Array);
  const F = Vec3();
  for (let i = 0; i < nx; i++) {
    const px = minX + i * stepX;
    for (let j = 0; j < ny; j++) {
      const py = minY + j * stepY;
      for (let k = 0; k < nz; k++) {
        const pz = minZ + k * stepZ;
        if (!lookup.check(px, py, pz, radius)) {
          continue; // stays at mean → no surface
        }
        Vec3.set(F, px, py, pz);
        Vec3.transformMat4(F, F, toFractional);
        const gx = (F[0] - Math.floor(F[0])) * ox;
        const gy = (F[1] - Math.floor(F[1])) * oy;
        const gz = (F[2] - Math.floor(F[2])) * oz;
        newSpace.set(
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          newData as any,
          i,
          j,
          k,
          sampleWrapped(origData, origSpace, ox, oy, oz, gx, gy, gz),
        );
      }
    }
  }

  const matrix = Mat4.identity();
  Mat4.setValue(matrix, 0, 0, stepX);
  Mat4.setValue(matrix, 1, 1, stepY);
  Mat4.setValue(matrix, 2, 2, stepZ);
  Mat4.setValue(matrix, 0, 3, minX);
  Mat4.setValue(matrix, 1, 3, minY);
  Mat4.setValue(matrix, 2, 3, minZ);

  volume.grid = {
    transform: { kind: "matrix", matrix },
    cells: Tensor.create(newSpace, Tensor.Data1(newData)),
    stats: grid.stats,
    periodicity: undefined,
  };
}

type MapLayerState = {
  url: string;
  name: string;
  status: "idle" | "loading" | "ready" | "error";
  visible: boolean;
  refs: string[];
  has2fofc: boolean;
  hasFofc: boolean;
  error?: string;
};

function initialLayers(maps: StructureMap[] | undefined): MapLayerState[] {
  return (maps ?? []).map((map) => ({
    url: map.url,
    name: map.name,
    status: "idle",
    visible: false,
    refs: [],
    has2fofc: false,
    hasFofc: false,
  }));
}

export default function StructureViewer({
  url,
  kind,
  maps,
}: {
  url: string;
  kind: StructureKind;
  maps?: StructureMap[];
}) {
  const viewerRef = useRef<HTMLDivElement>(null);
  const pluginRef = useRef<PluginUIContext | null>(null);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const modelStructureRef = useRef<any>(null);
  const [pluginElement, setPluginElement] = useState<ReactNode>(null);
  const [plugin, setPlugin] = useState<PluginUIContext | null>(null);
  const [spinning, setSpinning] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [layers, setLayers] = useState<MapLayerState[]>(() =>
    initialLayers(maps),
  );
  const [menuOpen, setMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  const mapsKey = (maps ?? []).map((map) => map.url).join("|");

  // Close the maps dropdown when clicking outside it.
  useEffect(() => {
    if (!menuOpen) {
      return;
    }
    const onDown = (event: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        setMenuOpen(false);
      }
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [menuOpen]);

  useEffect(() => {
    let cancelled = false;
    let created: PluginUIContext | undefined;
    const controller = new AbortController();

    setPluginElement(null);
    setPlugin(null);
    setSpinning(false);
    setError(null);
    setLoading(true);
    setLayers(initialLayers(maps));

    (async () => {
      if (cancelled || !viewerRef.current) {
        return;
      }

      const showSequence = kind !== "ccp4";
      const defaultSpec = DefaultPluginUISpec();
      created = await createPluginUI({
        target: viewerRef.current,
        spec: {
          ...defaultSpec,
          config: [
            ...(defaultSpec.config ?? []),
            [PluginConfig.Viewport.ShowAnimation, false],
            [
              PluginConfig.Structure.DefaultRepresentationPreset,
              "preset-structure-representation-polymer-cartoon",
            ],
          ],
          layout: {
            initial: {
              isExpanded: false,
              showControls: true,
              controlsDisplay: "reactive",
              regionState: {
                top: showSequence ? "full" : "hidden",
                left: "hidden",
                right: "full",
                bottom: "hidden",
              },
            },
          },
          canvas3d: {
            ...defaultSpec.canvas3d,
            renderer: {
              ...defaultSpec.canvas3d?.renderer,
              backgroundColor: Color(0xffffff),
            },
          },
          components: {
            ...defaultSpec.components,
            remoteState: "none",
            controls: {
              ...(showSequence ? {} : { top: "none" }),
              left: "none",
              bottom: "none",
            },
            viewport: { controls: Empty },
            hideTaskOverlay: true,
          },
        },
        render: (component, container) => {
          if (container !== viewerRef.current || cancelled) {
            return;
          }
          setPluginElement(component);
        },
      });
      if (cancelled) {
        created.dispose();
        return;
      }

      created.canvas3d?.setProps((props) => {
        if (props.camera.helper.axes.name === "on") {
          props.camera.helper.axes.params.location = "bottom-right";
        }
      });

      const provider = created.dataFormats.get(kind);
      if (!provider) {
        throw new Error(`unsupported structure format: ${kind}`);
      }

      const response = await fetchFileURL(url, { signal: controller.signal });
      if (!response.ok) {
        throw new Error(`${response.status} ${response.statusText}`);
      }
      const content =
        kind === "ccp4"
          ? new Uint8Array(await response.arrayBuffer())
          : await response.text();
      if (cancelled) {
        return;
      }

      const data = await created.builders.data.rawData(
        { data: content, label: url },
        { state: { isGhost: true } },
      );
      const parsed = await provider.parse(created, data);
      if (provider.visuals) {
        await provider.visuals(created, parsed);
      }

      // Keep the loaded model structure so density can be carved to it and the
      // camera framed on it (rather than on the whole unit cell).
      const structures =
        created.managers.structure.hierarchy.current.structures;
      modelStructureRef.current = structures[0]?.cell.obj?.data ?? null;

      // Frame the model right away (the preset doesn't always focus on open).
      const initialSphere = modelStructureRef.current?.boundary?.sphere;
      if (initialSphere) {
        created.managers.camera.focusSphere(initialSphere);
      } else {
        created.canvas3d?.requestCameraReset();
      }

      if (!cancelled) {
        pluginRef.current = created;
        setPlugin(created);
        setLoading(false);
      }
    })().catch((err: unknown) => {
      if (!cancelled) {
        setError(structureErrorMessage(err));
        setLoading(false);
      }
    });

    return () => {
      cancelled = true;
      controller.abort();
      pluginRef.current = null;
      created?.dispose();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, kind, mapsKey]);

  const resetView = () => {
    plugin?.canvas3d?.requestCameraReset();
  };

  const toggleSpin = () => {
    const canvas = plugin?.canvas3d;
    if (!canvas) {
      return;
    }
    const next = !spinning;
    setSpinning(next);
    canvas.setProps({
      trackball: {
        animate: next
          ? { name: "spin", params: { speed: 0.6, axis: [0, -1, 0] } }
          : { name: "off", params: {} },
      },
    });
  };

  const setLayerAt = (index: number, patch: Partial<MapLayerState>) => {
    setLayers((prev) =>
      prev.map((layer, i) => (i === index ? { ...layer, ...patch } : layer)),
    );
  };

  const toggleLayer = async (index: number) => {
    const activePlugin = pluginRef.current;
    const layer = layers[index];
    if (!activePlugin || !layer || layer.status === "loading") {
      return;
    }

    // Already rendered: just flip visibility of its representations.
    if (layer.status === "ready") {
      const nextVisible = !layer.visible;
      for (const ref of layer.refs) {
        setSubtreeVisibility(activePlugin.state.data, ref, !nextVisible);
      }
      setLayerAt(index, { visible: nextVisible });
      return;
    }

    // First enable (idle / retry after error): fetch + parse + render lazily.
    setLayerAt(index, { status: "loading", error: undefined });
    try {
      const response = await fetchFileURL(layer.url);
      if (!response.ok) {
        throw new Error(`${response.status} ${response.statusText}`);
      }
      const bytes = new Uint8Array(await response.arrayBuffer());
      const provider = activePlugin.dataFormats.get("mtz");
      if (!provider) {
        throw new Error("MTZ maps are not supported by this viewer build.");
      }
      const data = await activePlugin.builders.data.rawData(
        { data: bytes, label: layer.name },
        { state: { isGhost: true } },
      );
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const parsed = (await provider.parse(activePlugin, data)) as {
        volumes?: { "2fofc"?: any[]; fofc?: any[] };
      };
      const twoFoFc = parsed.volumes?.["2fofc"] ?? [];
      const foFc = parsed.volumes?.fofc ?? [];
      const has2fofc = twoFoFc.length > 0;
      const hasFofc = foFc.length > 0;

      // Resample each periodic map into a grid that hugs the model.
      const structure = modelStructureRef.current;
      if (structure) {
        if (has2fofc) {
          expandMapAroundModel(twoFoFc[0]?.data, structure, CARVE_RADIUS);
        }
        if (hasFofc) {
          expandMapAroundModel(foFc[0]?.data, structure, CARVE_RADIUS);
        }
      }

      // Build the map surfaces ourselves (instead of provider.visuals) so we
      // can keep them semi-transparent and let the model show through.
      const tree = activePlugin.build();
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const selectors: any[] = [];

      if (has2fofc) {
        selectors.push(
          tree
            .to(twoFoFc[0])
            .apply(
              StateTransforms.Representation.VolumeRepresentation3D,
              VolumeRepresentation3DHelpers.getDefaultParamsStatic(
                activePlugin,
                "isosurface",
                { isoValue: Volume.IsoValue.relative(1.5), alpha: 0.5 },
                "uniform",
                { value: Color(0x3362b2) },
              ),
            ).selector,
        );
      }
      if (hasFofc) {
        selectors.push(
          tree
            .to(foFc[0])
            .apply(
              StateTransforms.Representation.VolumeRepresentation3D,
              VolumeRepresentation3DHelpers.getDefaultParamsStatic(
                activePlugin,
                "isosurface",
                { isoValue: Volume.IsoValue.relative(3), alpha: 0.6 },
                "uniform",
                { value: Color(0x33bb33) },
              ),
            ).selector,
          tree
            .to(foFc[0])
            .apply(
              StateTransforms.Representation.VolumeRepresentation3D,
              VolumeRepresentation3DHelpers.getDefaultParamsStatic(
                activePlugin,
                "isosurface",
                { isoValue: Volume.IsoValue.relative(-3), alpha: 0.6 },
                "uniform",
                { value: Color(0xbb3333) },
              ),
            ).selector,
        );
      }

      await tree.commit();

      const refs = selectors
        .map((selector) => selector?.ref)
        .filter((ref): ref is string => typeof ref === "string");

      // Frame the model (not the full unit cell) once density is on.
      const sphere = structure?.boundary?.sphere;
      if (sphere) {
        activePlugin.managers.camera.focusSphere(sphere);
      }

      setLayerAt(index, {
        status: "ready",
        visible: true,
        refs,
        has2fofc,
        hasFofc,
      });
    } catch (err) {
      setLayerAt(index, {
        status: "error",
        visible: false,
        error: structureErrorMessage(err),
      });
    }
  };

  const ready = plugin != null && !loading && !error;

  return (
    <div
      className={styles.structureViewer}
      data-has-bar={layers.length > 0 ? "true" : undefined}
    >
      {layers.length > 0 ? (
        <div className={styles.layerBar}>
          <span className={styles.layerBarLabel}>Density maps</span>
          <div className={styles.mapMenu} ref={menuRef}>
            <button
              type="button"
              className={styles.mapMenuTrigger}
              data-open={menuOpen ? "true" : undefined}
              disabled={!ready}
              onClick={() => setMenuOpen((open) => !open)}
              aria-haspopup="true"
              aria-expanded={menuOpen}
            >
              <span className={styles.mapMenuTriggerText}>
                {mapsSummary(layers)}
              </span>
              <ChevronIcon />
            </button>
            {menuOpen ? (
              <div className={styles.mapMenuList} role="menu">
                {layers.map((layer, index) => {
                  const on = layer.status === "ready" && layer.visible;
                  const label = layer.name.replace(/\.[^.]+$/, "");
                  return (
                    <button
                      key={layer.url}
                      type="button"
                      role="menuitemcheckbox"
                      aria-checked={on}
                      className={styles.mapMenuRow}
                      data-active={on ? "true" : undefined}
                      data-error={layer.status === "error" ? "true" : undefined}
                      disabled={layer.status === "loading"}
                      onClick={() => toggleLayer(index)}
                      title={layer.error ?? layer.name}
                    >
                      <span className={styles.mapMenuRowName}>{label}</span>
                      {layer.status === "loading" ? (
                        <span
                          className={styles.layerToggleSpinner}
                          aria-hidden="true"
                        />
                      ) : (
                        <EyeIcon on={on} />
                      )}
                    </button>
                  );
                })}
              </div>
            ) : null}
          </div>
        </div>
      ) : null}
      <div ref={viewerRef} className={styles.structureCanvas}>
        {pluginElement}
        {ready ? (
          <div className={styles.structureControls}>
            <button
              type="button"
              className={styles.structureControlButton}
              onClick={resetView}
              title="Reset view"
              aria-label="Reset view"
            >
              <ResetIcon />
            </button>
            <button
              type="button"
              className={`${styles.structureControlButton}${
                spinning ? ` ${styles.structureControlButtonActive}` : ""
              }`}
              onClick={toggleSpin}
              title={spinning ? "Stop spinning" : "Spin"}
              aria-label={spinning ? "Stop spinning" : "Spin"}
              aria-pressed={spinning}
            >
              <SpinIcon />
            </button>
          </div>
        ) : null}
        {loading && !error ? (
          <p className={styles.structurePlaceholder}>Loading structure...</p>
        ) : null}
        {error ? (
          <p className={styles.structurePlaceholder}>
            Failed to load structure: {error}
          </p>
        ) : null}
      </div>
    </div>
  );
}

function mapsSummary(layers: MapLayerState[]): string {
  if (layers.some((layer) => layer.status === "loading")) {
    return "Loading…";
  }
  const visible = layers.filter(
    (layer) => layer.status === "ready" && layer.visible,
  );
  if (visible.length === 0) {
    return "None shown";
  }
  if (visible.length === 1) {
    return visible[0].name.replace(/\.[^.]+$/, "");
  }
  return `${visible.length} shown`;
}

function ChevronIcon() {
  return (
    <svg
      className={styles.mapMenuChevron}
      width="12"
      height="12"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="m6 9 6 6 6-6" />
    </svg>
  );
}

function EyeIcon({ on }: { on: boolean }) {
  return on ? (
    <svg
      className={styles.layerEye}
      width="15"
      height="15"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M2 12s3.6-6.5 10-6.5S22 12 22 12s-3.6 6.5-10 6.5S2 12 2 12Z" />
      <circle cx="12" cy="12" r="2.6" />
    </svg>
  ) : (
    <svg
      className={styles.layerEye}
      width="15"
      height="15"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M3 3 21 21" />
      <path d="M10.6 6.1A9.8 9.8 0 0 1 12 6c6.4 0 10 6 10 6a16 16 0 0 1-3.1 3.8M6.3 7.8A15.8 15.8 0 0 0 2 12s3.6 6 10 6a9.9 9.9 0 0 0 4.1-.9" />
    </svg>
  );
}

function ResetIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M3 12a9 9 0 1 0 3-6.7L3 8" />
      <path d="M3 3v5h5" />
    </svg>
  );
}

function SpinIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <ellipse cx="12" cy="12" rx="9" ry="4" />
      <path d="M3 12a9 4 0 0 0 18 0" transform="rotate(60 12 12)" />
    </svg>
  );
}

function structureErrorMessage(err: unknown): string {
  const message = err instanceof Error ? err.message : String(err);
  if (/\b404\b|not found/i.test(message)) {
    return "This structure file is no longer available.";
  }
  return message;
}
