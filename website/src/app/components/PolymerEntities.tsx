import type { ReactNode } from "react";

import type { Entity, PolymerEntityUniProtMapping } from "@/lib/api/entries";
import { isPreviewable } from "@/lib/entities";
import {
  type PolymerEntityView,
  taxonomyURL,
  uniProtSourceLabel,
  uniProtURL,
} from "@/lib/polymer-entities";
import EntityChains from "./EntityChains";

import styles from "./PolymerEntities.module.css";

const COLUMNS = 6;

/**
 * The entry's polymer entities, one framed table each.
 *
 * RCSB's shape: a band naming the entity, the column header under it, one row
 * of data. Each frame stands on its own, so an entity reads -- and can be
 * linked to -- without the ones above it.
 *
 * The columns are fixed and identical in every frame, which is the one thing
 * RCSB does not do: their tables size themselves, so four entities put their
 * residue counts in four different places and cannot be read down the page.
 * That is also why an empty field prints a dash instead of collapsing -- a
 * shrunk column would break the alignment the frames are here to keep.
 */
export default function PolymerEntities({
  views,
  artifacts,
}: {
  views: PolymerEntityView[];
  /** The entry's artifacts by id: a chain opens the FASTA it was read out of,
   *  and this is where that artifact comes from. */
  artifacts: Map<string, Entity>;
}) {
  if (views.length === 0) {
    return null;
  }

  return (
    <div className={styles.list}>
      {views.map((view) => (
        <EntityTable key={view.key} view={view} artifacts={artifacts} />
      ))}
    </div>
  );
}

function EntityTable({
  view,
  artifacts,
}: {
  view: PolymerEntityView;
  artifacts: Map<string, Entity>;
}) {
  return (
    <div className={styles.box}>
      <div className={styles.scroll}>
        <table className={styles.table}>
          <colgroup>
            <col className={styles.colMolecule} />
            <col className={styles.colChains} />
            <col className={styles.colLength} />
            <col className={styles.colOrganism} />
            <col className={styles.colUniProt} />
            <col />
          </colgroup>

          <thead>
            <tr className={styles.band}>
              <th colSpan={COLUMNS} scope="colgroup">
                {view.entityId ? `Entity ${view.entityId}` : "Entity"}
              </th>
            </tr>
            <tr className={styles.head}>
              <th scope="col">Molecule</th>
              <th scope="col">Chains</th>
              <th scope="col">Length</th>
              <th scope="col">Organism</th>
              <th scope="col">UniProt</th>
              <th scope="col">Details</th>
            </tr>
          </thead>

          <tbody>
            <tr>
              <td className={styles.molecule}>{view.name}</td>
              <td>{chainsCell(view, artifacts)}</td>
              <td className={styles.number}>{view.residues ?? <Absent />}</td>
              <td>{organismCell(view)}</td>
              <td>{uniProtCell(view.uniprotMappings)}</td>
              <td>{detailsCell(view)}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  );
}

function chainsCell(
  view: PolymerEntityView,
  artifacts: Map<string, Entity>,
): ReactNode {
  if (view.chains.length === 0) {
    return <Absent />;
  }

  return (
    <EntityChains chains={view.chains} entity={fastaArtifact(view, artifacts)} />
  );
}

/**
 * The artifact a chain came from.
 *
 * Chains exist in the view only because a FASTA record was matched to the
 * entity, so that record's artifact must be one of the entry's, and must carry
 * the parsed records the preview draws. If either is untrue the sync wrote a
 * sequence the entry cannot show -- a broken join, not a missing optional
 * field -- and the page says so instead of drawing a chain that does nothing.
 */
function fastaArtifact(
  view: PolymerEntityView,
  artifacts: Map<string, Entity>,
): Entity {
  const id = view.sequenceArtifactId;
  const artifact = id ? artifacts.get(id) : undefined;
  const which = `entity ${view.entityId ?? view.key}, chain(s) ${view.chains.join(", ")}`;

  if (!artifact) {
    throw new Error(
      `Polymer ${which}: the FASTA they were read from is not among the ` +
        `entry's artifacts (artifact ${id ?? "unknown"}).`,
    );
  }
  if (!isPreviewable(artifact)) {
    throw new Error(
      `Polymer ${which}: artifact ${artifact.id} (${artifact.name}) carries no ` +
        `parsed FASTA to open.`,
    );
  }
  return artifact;
}

function organismCell(view: PolymerEntityView): ReactNode {
  if (view.organisms.length === 0) {
    return <Absent />;
  }

  return (
    <ul className={styles.stack}>
      {view.organisms.map((organism) => (
        <li key={`${organism.scientific_name}-${organism.ncbi_taxonomy_id}`}>
          <span className={styles.italic}>{organism.scientific_name}</span>
          {organism.ncbi_taxonomy_id != null ? (
            <a
              className={styles.reference}
              href={taxonomyURL(organism.ncbi_taxonomy_id)}
              target="_blank"
              rel="noreferrer"
            >
              {organism.ncbi_taxonomy_id}
            </a>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

function uniProtCell(mappings: PolymerEntityUniProtMapping[]): ReactNode {
  if (mappings.length === 0) {
    return <Absent />;
  }

  return (
    <ul className={styles.stack}>
      {mappings.map((mapping) => (
        <li key={`${mapping.accession}-${mapping.source}`}>
          <a
            className={styles.link}
            href={uniProtURL(mapping.accession)}
            target="_blank"
            rel="noreferrer"
          >
            {mapping.accession}
          </a>
          {/* Where the accession came from, and which release it was resolved
              against: true, but secondary to the accession itself. */}
          <span className={styles.caption}>
            {uniProtSourceLabel(mapping.source)}
            {mapping.unp_release ? ` · ${mapping.unp_release}` : ""}
          </span>
        </li>
      ))}
    </ul>
  );
}

// Two unlike things share this column, so both are labelled: without the labels
// `P13S R15K` and `UNP residues 24-333` read as the same kind of value.
function detailsCell(view: PolymerEntityView): ReactNode {
  const details: ReactNode[] = [];

  if (view.construct) {
    details.push(
      <li key="construct">
        <span className={styles.detailLabel}>Construct</span>
        {view.construct}
      </li>,
    );
  }

  if (view.mutationsText) {
    details.push(
      <li key="mutations">
        <span className={styles.detailLabel}>
          {view.mutations?.length === 1 ? "Mutation" : "Mutations"}
        </span>
        {view.mutations ? (
          <span className={styles.mono}>{view.mutations.join(" ")}</span>
        ) : (
          view.mutationsText
        )}
      </li>,
    );
  }

  if (details.length === 0) {
    return <Absent />;
  }

  return <ul className={styles.details}>{details}</ul>;
}

function Absent() {
  return <span className={styles.absent}>—</span>;
}
