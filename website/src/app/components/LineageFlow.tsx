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
const ARTIFACT_WIDTH = 246;
const ARTIFACT_HEIGHT = 74;
const RUN_WIDTH = 214;
const RUN_HEIGHT = 38;
const COLUMN_GAP = 20;
export const ROW_PITCH = 120;

const TRUNK_STROKE = "#8f8a86";
const BRANCH_STROKE = "#cfcbc8";

type FlowNodeData = {
  node: LineageNode;
  onOpen?: (node: LineageNode) => void;
};

export default function LineageFlow({
  lineage,
  onSelect,
}: {
  lineage: Lineage;
  onSelect?: (node: LineageNode) => void;
}) {
  const { nodes, edges } = useMemo(
    () => layout(lineage, onSelect),
    [lineage, onSelect],
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
    },
    [onSelect],
  );

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      nodeTypes={NODE_TYPES}
      defaultEdgeOptions={DEFAULT_EDGE_OPTIONS}
      onNodeClick={onSelect ? handleNodeClick : undefined}
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
  onSelect?: (node: LineageNode) => void,
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
      trunk.reduce((sum, node) => sum + sizeOf(node).width, 0) +
      COLUMN_GAP * Math.max(0, trunk.length - 1);

    let x = -trunkWidth / 2;
    for (const node of ordered) {
      const { width, height } = sizeOf(node);
      nodes.push({
        id: node.id,
        type: node.kind,
        position: {
          x,
          // Rows share a pitch, so a short run pill sits centred in the band
          // rather than hugging the top of it.
          y: generation * ROW_PITCH + (ARTIFACT_HEIGHT - height) / 2,
        },
        width,
        height,
        data: { node, onOpen: onSelect },
        draggable: false,
      });
      x += width + COLUMN_GAP;
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

function sizeOf(node: LineageNode): { width: number; height: number } {
  return node.kind === "run"
    ? { width: RUN_WIDTH, height: RUN_HEIGHT }
    : { width: ARTIFACT_WIDTH, height: ARTIFACT_HEIGHT };
}

function ArtifactNode({ data }: NodeProps) {
  const { node, onOpen } = data as FlowNodeData;
  return (
    <div
      className={styles.artifact}
      data-level={node.level ?? undefined}
      data-current={node.current ? "true" : undefined}
      data-off-path={node.onPath ? undefined : "true"}
      data-interactive={onOpen ? "true" : undefined}
      style={{ width: ARTIFACT_WIDTH, height: ARTIFACT_HEIGHT }}
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
        <span className={styles.type} data-type={node.entity.type}>
          {node.entity.type}
        </span>
      </span>
      <span className={styles.name}>{node.name}</span>
      <Handle type="source" position={Position.Bottom} className={styles.handle} />
    </div>
  );
}

function RunNode({ data }: NodeProps) {
  const { node } = data as FlowNodeData;
  return (
    <div
      className={styles.run}
      style={{ width: RUN_WIDTH, height: RUN_HEIGHT }}
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

const NODE_TYPES = { artifact: ArtifactNode, run: RunNode };
