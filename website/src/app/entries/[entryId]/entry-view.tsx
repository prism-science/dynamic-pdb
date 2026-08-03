import type { ReactNode } from "react";

import type {
  DataPayload,
  Entity,
  EntityRelation,
  MetricsPayload,
  Model,
  ProgramPayload,
} from "@/lib/api/entries";
import { getEntityFileURL, getFilePayload } from "@/lib/entities";
import StructureViewerModal from "@/app/components/StructureViewerModal";
import { detectStructureKind, type StructureMap } from "@/lib/structureKind";

import styles from "./entry-page.module.css";

export type Provenance = {
  index: Map<string, Entity>;
  programOf: (entityId: string) => Entity | null;
  inputsOf: (entityId: string) => Entity[];
  metricsOf: (entityId: string) => Entity[];
};

export type MetadataFact = {
  label: string;
  value: string;
};

export function buildProvenance(
  entities: Entity[],
  relations: EntityRelation[],
): Provenance {
  const index = new Map(entities.map((entity) => [entity.id, entity]));

  const outputProgramsOf = (entityId: string) =>
    relations
      .filter(
        (relation) =>
          relation.source_entity_id === entityId &&
          relation.relation_type === "output_of",
      )
      .map((relation) => index.get(relation.target_entity_id))
      .filter((entity): entity is Entity => entity?.type === "program");

  const programOf = (entityId: string) => {
    return outputProgramsOf(entityId)[0] ?? null;
  };

  const inputsOf = (entityId: string) => {
    const programs = outputProgramsOf(entityId);
    const programIds = new Set(programs.map((program) => program.id));
    const inputs: Entity[] = [];
    for (const relation of relations) {
      if (
        relation.relation_type === "input_to" &&
        programIds.has(relation.target_entity_id)
      ) {
        const source = index.get(relation.source_entity_id);
        if (source && source.type !== "program" && source.id !== entityId) {
          inputs.push(source);
        }
      }
    }
    return inputs;
  };

  const metricsOf = (entityId: string) => {
    const metrics: Entity[] = [];
    for (const relation of relations) {
      if (
        relation.relation_type === "metrics_for" &&
        relation.target_entity_id === entityId
      ) {
        const source = index.get(relation.source_entity_id);
        if (source?.type === "metrics") {
          metrics.push(source);
        }
      }
    }
    return metrics;
  };

  return { index, programOf, inputsOf, metricsOf };
}

export function ImagePlaceholderIcon({ size = 26 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="2.4" fill="currentColor" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" transform="rotate(60 12 12)" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" transform="rotate(120 12 12)" />
    </svg>
  );
}

export function EntryHero({
  title,
  eyebrow,
  description,
  meta = [],
  action,
}: {
  title: string;
  eyebrow?: string;
  description?: string | null;
  meta?: string[];
  action?: ReactNode;
}) {
  return (
    <header className={styles.hero}>
      {eyebrow ? <p className={styles.eyebrow}>{eyebrow}</p> : null}
      <div className={styles.heroTitleRow}>
        <h1>{title}</h1>
        {action ? <div className={styles.heroAction}>{action}</div> : null}
      </div>
      {description?.trim() ? (
        <p className={styles.description}>{description}</p>
      ) : null}
      {meta.length > 0 ? (
        <div className={styles.heroMeta} aria-label="Summary">
          {meta.map((item) => (
            <span key={item}>{item}</span>
          ))}
        </div>
      ) : null}
    </header>
  );
}

export function SectionHeader({
  title,
  detail,
}: {
  title: string;
  detail?: ReactNode;
}) {
  return (
    <div className={styles.sectionHeader}>
      <h2>{title}</h2>
      {detail ? <span>{detail}</span> : null}
    </div>
  );
}

export function MetadataFacts({ facts }: { facts: MetadataFact[] }) {
  if (facts.length === 0) {
    return null;
  }
  return (
    <dl className={styles.modelProvenance}>
      {facts.map((fact) => (
        <div key={fact.label} className={styles.modelProvenanceRow}>
          <dt>{fact.label}</dt>
          <dd>{fact.value}</dd>
        </div>
      ))}
    </dl>
  );
}

type MetricStatus = "good" | "warn" | "bad";

type MetricSpec = {
  key: keyof MetricsPayload;
  label: string;
  direction: "lower" | "higher";
  scaleMax: number;
  status: (value: number) => MetricStatus;
};

// Direction, scale, and quality thresholds are fixed properties of each
// crystallographic metric (R-factors: lower is better; correlation
// coefficients: higher is better), not stored in the payload.
const modelMetricTiles: MetricSpec[] = [
  {
    key: "r_work",
    label: "R-work",
    direction: "lower",
    scaleMax: 0.4,
    status: (value) => (value < 0.25 ? "good" : value < 0.3 ? "warn" : "bad"),
  },
  {
    key: "r_free",
    label: "R-free",
    direction: "lower",
    scaleMax: 0.4,
    status: (value) => (value < 0.25 ? "good" : value < 0.3 ? "warn" : "bad"),
  },
  {
    key: "rscc",
    label: "RSCC",
    direction: "higher",
    scaleMax: 1,
    status: (value) => (value >= 0.9 ? "good" : value >= 0.8 ? "warn" : "bad"),
  },
  {
    key: "cc",
    label: "CC",
    direction: "higher",
    scaleMax: 1,
    status: (value) => (value >= 0.9 ? "good" : value >= 0.8 ? "warn" : "bad"),
  },
];

export function ModelCard({
  entity,
  provenance,
  thumbnailImageURL,
  metadata,
  maps,
}: {
  entity: Entity;
  provenance: Provenance;
  thumbnailImageURL?: string | null;
  metadata?: Model["metadata"];
  maps?: StructureMap[];
}) {
  const fileURL = getEntityFileURL(entity);
  const payload = getFilePayload(entity);
  const meta = (payload?.metadata ?? {}) as Record<string, unknown>;
  const previewURL = thumbnailImageURL?.trim() || getModelPreviewURL(meta);
  const structureKind = detectStructureKind(fileURL ?? undefined);

  const merged: MetricsPayload = {};
  for (const metric of provenance.metricsOf(entity.id)) {
    Object.assign(merged, metric.payload as MetricsPayload);
  }
  const tiles = modelMetricTiles.filter(
    (tile) => typeof merged[tile.key] === "number",
  );
  const program = provenance.programOf(entity.id);
  const modelFacts = modelMetadataFacts(metadata ?? payload ?? {});

  return (
    <article className={styles.modelCard}>
      <div className={styles.modelLeft}>
        <div className={styles.modelPreview} data-empty={previewURL ? undefined : "true"}>
          {previewURL ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img src={previewURL} alt="" loading="lazy" />
          ) : (
            <ImagePlaceholderIcon size={34} />
          )}
        </div>
        {structureKind && fileURL ? (
          <StructureViewerModal
            url={fileURL}
            kind={structureKind}
            name={entity.name}
            maps={maps}
          />
        ) : null}
      </div>

      <div className={styles.modelBody}>
        <MetadataFacts
          facts={[
            ...(program
              ? [{ label: "Made with", value: formatProgram(program) }]
              : []),
            ...modelFacts,
          ]}
        />

        {tiles.length > 0 ? (
          <section className={styles.validation}>
            <div className={styles.metricGrid}>
              {tiles.map((tile) => {
                const value = merged[tile.key] as number;
                const fill = Math.max(0, Math.min(1, value / tile.scaleMax));
                return (
                  <div key={tile.key} className={styles.metricTile}>
                    <div className={styles.metricLabel}>{tile.label}</div>
                    <div className={styles.metricValue}>
                      {metricFormatter.format(value)}
                    </div>
                    <div className={styles.metricBar}>
                      <span
                        className={styles.metricBarFill}
                        data-status={tile.status(value)}
                        style={{ width: `${fill * 100}%` }}
                      />
                    </div>
                    <div className={styles.metricHint}>
                      {tile.direction === "lower" ? "↓ better" : "↑ better"}
                    </div>
                  </div>
                );
              })}
            </div>
          </section>
        ) : null}
      </div>
    </article>
  );
}

function formatProgram(program: Entity): string {
  const payload = program.payload as ProgramPayload;
  if (payload && typeof payload.name === "string") {
    return payload.version ? `${payload.name} ${payload.version}` : payload.name;
  }
  return program.name;
}

export function entryMetadataFacts(
  metadata: Record<string, unknown> | undefined,
): MetadataFact[] {
  const facts: MetadataFact[] = [];
  const externalRefs = recordValue(metadata?.external_refs);
  const pdb = stringValue(externalRefs?.pdb);
  const resolution = numberValue(metadata?.resolution);
  const organism = stringValue(metadata?.organism);
  const method = stringValue(metadata?.method);
  const spaceGroup = stringValue(metadata?.space_group);

  if (pdb) {
    facts.push({ label: "PDB", value: pdb });
  }
  if (resolution != null) {
    facts.push({
      label: "Resolution",
      value: `${numberFormatter.format(resolution)} Å`,
    });
  }
  if (organism) {
    facts.push({ label: "Organism", value: organism });
  }
  if (method) {
    facts.push({ label: "Method", value: method });
  }
  if (spaceGroup) {
    facts.push({ label: "Space group", value: spaceGroup });
  }

  return facts;
}

function modelMetadataFacts(metadata: Record<string, unknown>): MetadataFact[] {
  const facts: MetadataFact[] = [];
  const authors = stringArrayValue(metadata.authors);
  const affiliation = stringValue(metadata.affiliation);
  const purpose = stringValue(metadata.purpose);
  const modelType = stringValue(metadata.model_type);
  const atomCount = numberValue(metadata.atom_count);
  const modeledResidues = numberValue(metadata.modeled_residues);
  const proteinChains = numberValue(metadata.unique_protein_chains);
  const ligands = stringArrayValue(metadata.ligands);

  if (authors.length > 0) {
    facts.push({ label: "Authors", value: authors.join(", ") });
  }
  if (affiliation) {
    facts.push({ label: "Affiliation", value: affiliation });
  }
  if (purpose) {
    facts.push({ label: "Purpose", value: purpose });
  }
  if (modelType) {
    facts.push({ label: "Model type", value: modelType });
  }
  if (atomCount != null) {
    facts.push({ label: "Atoms", value: numberFormatter.format(atomCount) });
  }
  if (modeledResidues != null) {
    facts.push({
      label: "Residues",
      value: numberFormatter.format(modeledResidues),
    });
  }
  if (proteinChains != null) {
    facts.push({ label: "Chains", value: numberFormatter.format(proteinChains) });
  }
  if (ligands.length > 0) {
    facts.push({ label: "Ligands", value: ligands.join(", ") });
  }

  return facts;
}

function recordValue(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function stringValue(value: unknown): string | null {
  return typeof value === "string" && value.trim() !== ""
    ? value.trim()
    : null;
}

function numberValue(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function stringArrayValue(value: unknown): string[] {
  return Array.isArray(value)
    ? value
        .filter((item): item is string => typeof item === "string")
        .map((item) => item.trim())
        .filter(Boolean)
    : [];
}

function getModelPreviewURL(meta: Record<string, unknown>): string | null {
  const explicit =
    typeof meta.preview_image_url === "string"
      ? meta.preview_image_url.trim()
      : "";
  if (explicit) {
    return explicit;
  }
  return null;
}

const metricFormatter = new Intl.NumberFormat("en-US", {
  maximumFractionDigits: 3,
});

const numberFormatter = new Intl.NumberFormat("en-US");
