import type { Entity, EntityLevel, EntityRelation } from "@/lib/api/entries";

// The pipeline that produced a model, walked backwards from its coordinates.
//
// Relation direction, as written by the API adapter: both `output_of` and
// `input_to` point *from the artifact to the run*. So "who made X" is the set
// of runs X is `output_of`, and "what X was made from" is the set of artifacts
// that are `input_to` those runs.
//
// The walk is upstream only. Downstream models built on top of this one are a
// different question and belong to the entry, not to this page.

export type LineageNodeKind = "artifact" | "run";

export type LineageNode = {
  id: string;
  kind: LineageNodeKind;
  name: string;
  version: string | null;
  level: EntityLevel | null;
  entity: Entity;
  // Longest path from a root, which is what fixes the row a node sits in.
  generation: number;
  // False for artifacts that a run on the chain also emitted but that this
  // model never consumed — shown, dimmed, because dropping them would make a
  // run look like it produced one thing when it produced three.
  onPath: boolean;
  current: boolean;
};

export type LineageEdge = {
  id: string;
  source: string;
  target: string;
  onPath: boolean;
};

export type Lineage = {
  nodes: LineageNode[];
  edges: LineageEdge[];
  // Runs on the chain, oldest first. The rail button is only worth showing
  // when there is at least one.
  runCount: number;
};

export function buildLineage(
  entities: Entity[],
  relations: EntityRelation[],
  targetArtifactId: string | null,
): Lineage {
  const empty: Lineage = { nodes: [], edges: [], runCount: 0 };
  if (!targetArtifactId) {
    return empty;
  }

  const index = new Map(entities.map((entity) => [entity.id, entity]));
  if (!index.has(targetArtifactId)) {
    return empty;
  }

  const runsThatProduced = (artifactId: string) =>
    relations
      .filter(
        (relation) =>
          relation.relation_type === "output_of" &&
          relation.source_entity_id === artifactId,
      )
      .map((relation) => index.get(relation.target_entity_id))
      .filter((entity): entity is Entity => entity?.type === "program");

  const artifactsRelatedTo = (runId: string, kind: EntityRelation["relation_type"]) =>
    relations
      .filter(
        (relation) =>
          relation.relation_type === kind && relation.target_entity_id === runId,
      )
      .map((relation) => index.get(relation.source_entity_id))
      .filter(
        (entity): entity is Entity =>
          entity !== undefined && entity.type !== "program",
      );

  const picked = new Map<string, { entity: Entity; onPath: boolean }>();
  const edges = new Map<string, LineageEdge>();

  const take = (entity: Entity, onPath: boolean) => {
    const existing = picked.get(entity.id);
    // onPath only ever gets promoted: a node reached first as a side output and
    // later as an ancestor is an ancestor.
    picked.set(entity.id, { entity, onPath: onPath || (existing?.onPath ?? false) });
  };

  const link = (source: string, target: string, onPath: boolean) => {
    const id = `${source}->${target}`;
    const existing = edges.get(id);
    edges.set(id, {
      id,
      source,
      target,
      onPath: onPath || (existing?.onPath ?? false),
    });
  };

  const seen = new Set<string>();
  const queue = [targetArtifactId];
  let runCount = 0;

  while (queue.length > 0) {
    const artifactId = queue.shift() as string;
    if (seen.has(artifactId)) {
      continue;
    }
    seen.add(artifactId);

    const artifact = index.get(artifactId);
    if (!artifact) {
      continue;
    }
    take(artifact, true);

    for (const run of runsThatProduced(artifactId)) {
      if (!picked.has(run.id)) {
        runCount += 1;
      }
      take(run, true);
      link(run.id, artifactId, true);

      for (const sibling of artifactsRelatedTo(run.id, "output_of")) {
        if (sibling.id === artifactId) {
          continue;
        }
        take(sibling, false);
        link(run.id, sibling.id, false);
      }

      for (const input of artifactsRelatedTo(run.id, "input_to")) {
        take(input, true);
        link(input.id, run.id, true);
        queue.push(input.id);
      }
    }
  }

  const generations = computeGenerations(
    [...picked.keys()],
    [...edges.values()],
  );

  const nodes: LineageNode[] = [...picked.values()].map(({ entity, onPath }) => ({
    id: entity.id,
    kind: entity.type === "program" ? "run" : "artifact",
    name: programName(entity) ?? entity.name,
    version: programVersion(entity),
    level: entity.level,
    entity,
    generation: generations.get(entity.id) ?? 0,
    onPath,
    current: entity.id === targetArtifactId,
  }));

  nodes.sort((a, b) => a.generation - b.generation || a.name.localeCompare(b.name));

  return { nodes, edges: [...edges.values()], runCount };
}

// Longest path from a root. Depth-first with an in-progress guard: the data is
// meant to be acyclic, but a bad relation should degrade to a flat row rather
// than hang the render.
function computeGenerations(
  ids: string[],
  edges: LineageEdge[],
): Map<string, number> {
  const parents = new Map<string, string[]>();
  for (const id of ids) {
    parents.set(id, []);
  }
  for (const edge of edges) {
    parents.get(edge.target)?.push(edge.source);
  }

  const depth = new Map<string, number>();
  const active = new Set<string>();

  const resolve = (id: string): number => {
    const cached = depth.get(id);
    if (cached !== undefined) {
      return cached;
    }
    if (active.has(id)) {
      return 0;
    }
    active.add(id);
    const own = parents.get(id) ?? [];
    const value =
      own.length === 0 ? 0 : Math.max(...own.map((parent) => resolve(parent) + 1));
    active.delete(id);
    depth.set(id, value);
    return value;
  };

  for (const id of ids) {
    resolve(id);
  }
  return depth;
}

function programName(entity: Entity): string | null {
  if (entity.type !== "program") {
    return null;
  }
  const payload = entity.payload as { name?: unknown };
  return typeof payload?.name === "string" && payload.name.trim()
    ? payload.name.trim()
    : null;
}

function programVersion(entity: Entity): string | null {
  if (entity.type !== "program") {
    return null;
  }
  const payload = entity.payload as { version?: unknown };
  return typeof payload?.version === "string" && payload.version.trim()
    ? payload.version.trim()
    : null;
}
