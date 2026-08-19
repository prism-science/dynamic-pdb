"use client";

import { useCallback, useMemo } from "react";
import {
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  MarkerType,
  Position,
  ReactFlow,
  type Edge,
  type Node,
  type NodeMouseHandler,
  type NodeProps,
} from "@xyflow/react";

import type { Lineage, LineageNode } from "@/lib/lineage";

import "@xyflow/react/dist/style.css";
import styles from "./LineageFlow.module.css";

// Top to bottom, not left to right. A four-run chain is eight columns wide;
// fitView would shrink the labels to nothing. Depth costs height, which the
// overlay has, and the width goes to a run's fan-in and fan-out instead.
// Sized so that a two-line artifact name fits without clipping: 18px meta row
// + 5px gap + two 17px lines + 16px padding.
//
// The compact set is for the new-entry form, where the graph lives in a
// 372px column beside the fields: two nodes side by side still fit without
// fitView shrinking the labels away.
type Metrics = {
  artifactWidth: number;
  artifactHeight: number;
  runWidth: number;
  runHeight: number;
  columnGap: number;
  rowPitch: number;
};

const FULL: Metrics = {
  artifactWidth: 246,
  artifactHeight: 74,
  runWidth: 214,
  runHeight: 38,
  columnGap: 20,
  rowPitch: 120,
};

const COMPACT: Metrics = {
  artifactWidth: 168,
  artifactHeight: 62,
  runWidth: 150,
  runHeight: 34,
  columnGap: 14,
  rowPitch: 100,
};

export const ROW_PITCH = FULL.rowPitch;

const TRUNK_STROKE = "#8a8d9b";
const BRANCH_STROKE = "#d5d7e4";

type FlowNodeData = {
  node: LineageNode;
  onOpen?: (node: LineageNode) => void;
  onSupply?: (node: LineageNode) => void;
  metrics: Metrics;
};

export default function LineageFlow({
  lineage,
  onSelect,
  onSupply,
  compact = false,
}: {
  lineage: Lineage;
  onSelect?: (node: LineageNode) => void;
  /** Called for a node the graph expects but nobody has uploaded, so the
   *  placeholder can act as the upload button for exactly that slot. */
  onSupply?: (node: LineageNode) => void;
  compact?: boolean;
}) {
  const metrics = compact ? COMPACT : FULL;
  const { nodes, edges } = useMemo(
    () => layout(lineage, metrics, onSelect, onSupply),
    [lineage, metrics, onSelect, onSupply],
  );

  // Clicks are handled here rather than inside the node component on purpose.
  // React Flow marks a node wrapper `pointer-events: none` unless it is
  // selectable, draggable, or the graph has an onNodeClick — with all three
  // off, a handler on the node's own markup never receives a real cursor
  // event (a synthetic .click() still fires, which makes it easy to miss).
  const handleNodeClick: NodeMouseHandler<Node<FlowNodeData>> = useCallback(
    (_event, flowNode) => {
      const { node } = flowNode.data;
      if (node.kind === "artifact") {
        onSelect?.(node);
      }
      if (node.kind === "expected") {
        onSupply?.(node);
      }
    },
    [onSelect, onSupply],
  );

  // fitView only runs on mount. The form's preview changes shape with every
  // link the depositor adds, so the canvas is remounted when the node set
  // itself changes — keyed on ids, not on names, so typing a program's name
  // does not throw the view away mid-edit.
  const shape = nodes.map((node) => node.id).join("|");

  return (
    <ReactFlow
      key={shape}
      nodes={nodes}
      edges={edges}
      nodeTypes={NODE_TYPES}
      defaultEdgeOptions={DEFAULT_EDGE_OPTIONS}
      onNodeClick={onSelect || onSupply ? handleNodeClick : undefined}
      fitView
      fitViewOptions={{ padding: 0.12 }}
      minZoom={0.35}
      maxZoom={1.4}
      // Read-only: the graph is a record of what happened, not a canvas to edit.
      nodesDraggable={false}
      nodesConnectable={false}
      elementsSelectable={false}
      zoomOnDoubleClick={false}
    >
      <Background variant={BackgroundVariant.Dots} gap={20} size={1} />
      <Controls showInteractive={false} />
    </ReactFlow>
  );
}

// Bezier, not smoothstep. Two inputs feeding one run produce two smoothstep
// paths whose horizontal segments land on the same y and overlap into what
// looks like a stray rectangle; bezier curves converge on the target cleanly.
const DEFAULT_EDGE_OPTIONS = {
  type: "default",
  animated: false,
} as const;

function layout(
  lineage: Lineage,
  metrics: Metrics,
  onSelect?: (node: LineageNode) => void,
  onSupply?: (node: LineageNode) => void,
): { nodes: Node<FlowNodeData>[]; edges: Edge[] } {
  const rows = new Map<number, LineageNode[]>();
  for (const node of lineage.nodes) {
    const row = rows.get(node.generation) ?? [];
    row.push(node);
    rows.set(node.generation, row);
  }

  const nodes: Node<FlowNodeData>[] = [];
  for (const [generation, row] of [...rows.entries()].sort((a, b) => a[0] - b[0])) {
    // The chain itself is centred and the side outputs hang off to the right,
    // so the trunk reads as one straight line down the middle instead of
    // shifting a column left or right depending on how many extras a run
    // happened to emit.
    const trunk = row.filter((node) => node.onPath);
    const branches = row.filter((node) => !node.onPath);
    const ordered = [...trunk, ...branches];
    const trunkWidth =
      trunk.reduce((sum, node) => sum + sizeOf(node, metrics).width, 0) +
      metrics.columnGap * Math.max(0, trunk.length - 1);

    let x = -trunkWidth / 2;
    for (const node of ordered) {
      const { width, height } = sizeOf(node, metrics);
      nodes.push({
        id: node.id,
        type: node.kind,
        position: {
          x,
          // Rows share a pitch, so a short run pill sits centred in the band
          // rather than hugging the top of it.
          y: generation * metrics.rowPitch + (metrics.artifactHeight - height) / 2,
        },
        width,
        height,
        data: { node, onOpen: onSelect, onSupply, metrics },
        draggable: false,
      });
      x += width + metrics.columnGap;
    }
  }

  const edges: Edge[] = lineage.edges.map((edge) => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    markerEnd: {
      type: MarkerType.ArrowClosed,
      width: 12,
      height: 12,
      color: edge.onPath ? TRUNK_STROKE : BRANCH_STROKE,
    },
    style: {
      stroke: edge.onPath ? TRUNK_STROKE : BRANCH_STROKE,
      strokeWidth: edge.onPath ? 1.6 : 1.2,
    },
  }));

  return { nodes, edges };
}

function sizeOf(
  node: LineageNode,
  metrics: Metrics,
): { width: number; height: number } {
  return node.kind === "run"
    ? { width: metrics.runWidth, height: metrics.runHeight }
    : { width: metrics.artifactWidth, height: metrics.artifactHeight };
}

/**
 * A file a run declared as input but that nobody uploaded. Drawn rather than
 * omitted so the gap is visible, and clickable so the graph doubles as the
 * place to close it.
 */
function ExpectedNode({ data }: NodeProps) {
  const { node, onSupply, metrics } = data as FlowNodeData;
  return (
    <div
      className={styles.expected}
      data-compact={metrics === COMPACT ? "true" : undefined}
      data-interactive={onSupply ? "true" : undefined}
      style={{ width: metrics.artifactWidth, height: metrics.artifactHeight }}
      role={onSupply ? "button" : undefined}
      tabIndex={onSupply ? 0 : undefined}
      onKeyDown={
        onSupply
          ? (event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                onSupply(node);
              }
            }
          : undefined
      }
      title={`${node.name} — named by ${node.version ?? "a run log"}, not uploaded`}
    >
      <Handle type="target" position={Position.Top} className={styles.handle} />
      <span className={styles.meta}>
        <span className={styles.expectedTag}>expected</span>
        {onSupply ? <span className={styles.expectedAdd}>+ upload</span> : null}
      </span>
      <span className={styles.name}>{node.name}</span>
      <Handle type="source" position={Position.Bottom} className={styles.handle} />
    </div>
  );
}

function ArtifactNode({ data }: NodeProps) {
  const { node, onOpen, metrics } = data as FlowNodeData;
  return (
    <div
      className={styles.artifact}
      data-compact={metrics === COMPACT ? "true" : undefined}
      data-level={node.level ?? undefined}
      data-current={node.current ? "true" : undefined}
      data-off-path={node.onPath ? undefined : "true"}
      data-interactive={onOpen ? "true" : undefined}
      style={{ width: metrics.artifactWidth, height: metrics.artifactHeight }}
      role={onOpen ? "button" : undefined}
      tabIndex={onOpen ? 0 : undefined}
      onKeyDown={
        onOpen
          ? (event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                onOpen(node);
              }
            }
          : undefined
      }
      title={
        node.onPath
          ? node.name
          : `${node.name} — produced alongside the chain, not used by this model`
      }
    >
      <Handle type="target" position={Position.Top} className={styles.handle} />
      <span className={styles.meta}>
        {node.level ? <span className={styles.level}>{node.level}</span> : null}
        {node.typeLabel ? (
          <span className={styles.type} data-type={node.typeLabel}>
            {node.typeLabel}
          </span>
        ) : null}
      </span>
      <span className={styles.name}>{node.name}</span>
      <Handle type="source" position={Position.Bottom} className={styles.handle} />
    </div>
  );
}

/**
 * Stands in for the siblings a wide row could not show. Deliberately plain:
 * it is a count, not an artifact, and must not be mistaken for one.
 */
function OverflowNode({ data }: NodeProps) {
  const { node, metrics } = data as FlowNodeData;
  return (
    <div
      className={styles.overflow}
      data-compact={metrics === COMPACT ? "true" : undefined}
      style={{ width: metrics.artifactWidth, height: metrics.artifactHeight }}
      title={`${node.hiddenCount ?? 0} further files, hidden to keep the row readable`}
    >
      <Handle type="target" position={Position.Top} className={styles.handle} />
      <span className={styles.overflowCount}>+{node.hiddenCount ?? 0}</span>
      <span className={styles.overflowLabel}>more files</span>
      <Handle type="source" position={Position.Bottom} className={styles.handle} />
    </div>
  );
}

function RunNode({ data }: NodeProps) {
  const { node, metrics } = data as FlowNodeData;
  return (
    <div
      className={styles.run}
      data-compact={metrics === COMPACT ? "true" : undefined}
      style={{ width: metrics.runWidth, height: metrics.runHeight }}
      title={node.version ? `${node.name} ${node.version}` : node.name}
    >
      <Handle type="target" position={Position.Top} className={styles.handle} />
      <span className={styles.runName}>{node.name}</span>
      {node.version ? (
        <span className={styles.runVersion}>{node.version}</span>
      ) : null}
      <Handle type="source" position={Position.Bottom} className={styles.handle} />
    </div>
  );
}

const NODE_TYPES = {
  artifact: ArtifactNode,
  run: RunNode,
  expected: ExpectedNode,
  overflow: OverflowNode,
};
