"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";

import { createPluginUI } from "molstar/lib/mol-plugin-ui";
import { DefaultPluginUISpec } from "molstar/lib/mol-plugin-ui/spec";
import type { PluginUIContext } from "molstar/lib/mol-plugin-ui/context";
import { PluginConfig } from "molstar/lib/mol-plugin/config";
import { Color } from "molstar/lib/mol-util/color";

import type { StructureKind } from "@/lib/structureKind";

import styles from "./StructureViewer.module.css";

const Empty = () => null;

export default function StructureViewer({
  url,
  kind,
}: {
  url: string;
  kind: StructureKind;
}) {
  const viewerRef = useRef<HTMLDivElement>(null);
  const [pluginElement, setPluginElement] = useState<ReactNode>(null);
  const [plugin, setPlugin] = useState<PluginUIContext | null>(null);
  const [spinning, setSpinning] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    let created: PluginUIContext | undefined;
    const controller = new AbortController();

    setPluginElement(null);
    setPlugin(null);
    setSpinning(false);
    setError(null);
    setLoading(true);

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

      const response = await fetch(url, { signal: controller.signal });
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

      if (!cancelled) {
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
      created?.dispose();
    };
  }, [url, kind]);

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

  const ready = plugin != null && !loading && !error;

  return (
    <div className={styles.structureViewer}>
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
