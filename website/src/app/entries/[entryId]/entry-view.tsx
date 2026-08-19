import type {
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
  // Set when the value identifies the record in an external database; rendered
  // as the only coloured value in the block so it reads as clickable.
  href?: string;
  // Identifiers and symmetry symbols are glyph-sensitive (P 21 21 21, 5GY3);
  // organism names are conventionally italicised.
  format?: "mono" | "italic";
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

function FactValue({ fact }: { fact: MetadataFact }) {
  if (!fact.href) {
    return <>{fact.value}</>;
  }
  return (
    <a
      className={styles.factLink}
      href={fact.href}
      target="_blank"
      rel="noreferrer"
    >
      {fact.value}
      <svg
        width="12"
        height="12"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M14 4h6v6M20 4l-8.5 8.5M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5" />
      </svg>
    </a>
  );
}

// Two columns of label/value rows on a fixed label track, so values start at
// the same x on both sides instead of being flung to the far edge.
//
// The halves are split here and rendered as two independent lists rather than
// as one grid flowing across both. A single grid shares its rows between the
// columns, so a three-line affiliation on the right stretches the row and
// punches a hole into the left column; separate lists let each side pack
// tight. Splitting at ceil(n / 2) also keeps the reading order down the left
// column and stays balanced whichever facts happen to be missing.
export function InfoGrid({ facts }: { facts: MetadataFact[] }) {
  if (facts.length === 0) {
    return null;
  }
  const half = Math.ceil(facts.length / 2);
  const columns = [facts.slice(0, half), facts.slice(half)];

  return (
    <div className={styles.infoColumns}>
      {columns.map((column, index) =>
        column.length > 0 ? (
          <dl key={index} className={styles.infoColumn}>
            {column.map((fact) => (
              <div key={fact.label} className={styles.infoRow}>
                <dt>{fact.label}</dt>
                <dd data-format={fact.format}>
                  <FactValue fact={fact} />
                </dd>
              </div>
            ))}
          </dl>
        ) : null,
      )}
    </div>
  );
}

type MetricStatus = "good" | "warn" | "bad";

type MetricSpec = {
  key: keyof MetricsPayload;
  label: string;
  /**
   * Whether the value falls inside the accepted range, used to colour it.
   *
   * This is all that is left of the chart these tiles used to draw. That bar
   * filled with `value / scaleMax`, so a worse R-factor drew a longer bar --
   * the opposite of how a filled bar reads -- and needed a "lower is better"
   * caption under it to be understood at all. The figure alone carries the
   * reading; the threshold only decides whether it is worth colouring.
   */
  status: (value: number) => MetricStatus;
};

// Quality thresholds are fixed properties of each crystallographic metric
// (R-factors: lower is better; correlation coefficients: higher is better),
// not stored in the payload.
const modelMetricTiles: MetricSpec[] = [
  {
    key: "r_work",
    label: "R-work",
    status: (value) => (value < 0.25 ? "good" : value < 0.3 ? "warn" : "bad"),
  },
  {
    key: "r_free",
    label: "R-free",
    status: (value) => (value < 0.25 ? "good" : value < 0.3 ? "warn" : "bad"),
  },
  {
    key: "rscc",
    label: "RSCC",
    status: (value) => (value >= 0.9 ? "good" : value >= 0.8 ? "warn" : "bad"),
  },
  {
    key: "cc",
    label: "CC",
    status: (value) => (value >= 0.9 ? "good" : value >= 0.8 ? "warn" : "bad"),
  },
];

export function ModelEvaluations({
  entity,
  provenance,
}: {
  entity: Entity;
  provenance: Provenance;
}) {
  const merged: MetricsPayload = {};
  for (const metric of provenance.metricsOf(entity.id)) {
    Object.assign(merged, metric.payload as MetricsPayload);
  }
  const tiles = modelMetricTiles.filter(
    (tile) => typeof merged[tile.key] === "number",
  );
  if (tiles.length === 0) {
    return null;
  }

  return (
    <div className={styles.metricGrid}>
      {tiles.map((tile) => {
        const value = merged[tile.key] as number;
        const status = tile.status(value);
        return (
          <div key={tile.key} className={styles.metricTile}>
            <div className={styles.metricLabel}>{tile.label}</div>
            {/* Only a value outside its range is coloured. A page where every
                number is green says nothing; one amber figure is the whole
                report. */}
            <div
              className={styles.metricValue}
              data-status={status === "good" ? undefined : status}
            >
              {metricFormatter.format(value)}
            </div>
          </div>
        );
      })}
    </div>
  );
}

export function hasModelEvaluations(entity: Entity, provenance: Provenance) {
  const merged: MetricsPayload = {};
  for (const metric of provenance.metricsOf(entity.id)) {
    Object.assign(merged, metric.payload as MetricsPayload);
  }
  return modelMetricTiles.some((tile) => typeof merged[tile.key] === "number");
}

export function modelMetadata(
  entity: Entity | null,
  metadata?: Model["metadata"],
): Record<string, unknown> {
  const payload = entity ? getFilePayload(entity) : null;
  return (metadata ?? payload ?? {}) as Record<string, unknown>;
}

export function modelPreviewURL(
  entity: Entity | null,
  thumbnailImageURL?: string | null,
  entryThumbnailImageURL?: string | null,
): string | null {
  const explicit = thumbnailImageURL?.trim();
  if (explicit) {
    return explicit;
  }
  const payload = entity ? getFilePayload(entity) : null;
  const meta = (payload?.metadata ?? {}) as Record<string, unknown>;
  return getModelPreviewURL(meta) ?? entryThumbnailImageURL?.trim() ?? null;
}

export function ModelViewerButton({
  entity,
  maps,
}: {
  entity: Entity;
  maps?: StructureMap[];
}) {
  const fileURL = getEntityFileURL(entity);
  const structureKind = detectStructureKind(fileURL ?? undefined);
  if (!structureKind || !fileURL) {
    return null;
  }
  return (
    <StructureViewerModal
      url={fileURL}
      kind={structureKind}
      name={entity.name}
      maps={maps}
    />
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
    facts.push({
      label: "PDB",
      value: pdb,
      href: rcsbStructureURL(pdb),
      format: "mono",
    });
  }
  if (resolution != null) {
    facts.push({
      label: "Resolution",
      value: `${numberFormatter.format(resolution)} Å`,
    });
  }
  if (method) {
    facts.push({ label: "Method", value: method });
  }
  if (spaceGroup) {
    facts.push({ label: "Space group", value: spaceGroup, format: "mono" });
  }
  if (organism) {
    facts.push({ label: "Organism", value: organism, format: "italic" });
  }

  return facts;
}

// A PDB ID is four alphanumerics starting with a digit; anything else is not
// addressable on rcsb.org and stays plain text rather than a broken link.
function rcsbStructureURL(pdb: string): string | undefined {
  const id = pdb.trim().toUpperCase();
  return /^[1-9][A-Z0-9]{3}$/.test(id)
    ? `https://www.rcsb.org/structure/${id}`
    : undefined;
}

// What the model contains: bare counts, which belong next to the thumbnail as
// vitals — the same role "310 residues" plays on the entry — rather than as
// tiles competing with the evaluation metrics.
// Counts only. Ligand codes are a named fact, not a measurement, so they go in
// the Info block with the rest of the labelled values instead of borrowing a
// "Label: value" shape no other line in the rail uses.
export function modelVitals(metadata: Record<string, unknown>): string[] {
  const parts: string[] = [];
  const atomCount = numberValue(metadata.atom_count);
  const modeledResidues = numberValue(metadata.modeled_residues);
  const proteinChains = numberValue(metadata.unique_protein_chains);

  if (atomCount != null) {
    parts.push(`${numberFormatter.format(atomCount)} atoms`);
  }
  if (modeledResidues != null) {
    parts.push(`${numberFormatter.format(modeledResidues)} residues`);
  }
  if (proteinChains != null) {
    parts.push(
      `${numberFormatter.format(proteinChains)} ${
        proteinChains === 1 ? "chain" : "chains"
      }`,
    );
  }

  return parts.length > 0 ? [parts.join(" · ")] : [];
}

// Where the model came from: prose-length values that need a label beside them.
export function modelInfoFacts(
  metadata: Record<string, unknown>,
  program: Entity | null,
): MetadataFact[] {
  const facts: MetadataFact[] = [];
  const purpose = stringValue(metadata.purpose);
  const modelType = stringValue(metadata.model_type);
  const ligands = stringArrayValue(metadata.ligands);
  const authors = stringArrayValue(metadata.authors);
  const affiliation = stringValue(metadata.affiliation);

  if (program) {
    facts.push({ label: "Made with", value: formatProgram(program) });
  }
  if (purpose) {
    facts.push({ label: "Purpose", value: purpose });
  }
  if (modelType) {
    facts.push({ label: "Model type", value: modelType });
  }
  if (ligands.length > 0) {
    facts.push({
      label: ligands.length === 1 ? "Ligand" : "Ligands",
      value: ligands.join(", "),
      format: "mono",
    });
  }
  if (authors.length > 0) {
    facts.push({ label: "Authors", value: authors.join(", ") });
  }
  if (affiliation) {
    facts.push({ label: "Affiliation", value: affiliation });
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
