"use client";

import { Fragment, useId, useState } from "react";

import { plainSequence, sequenceLines } from "@/lib/fasta";
import {
  chainsLabel,
  hasEntityDetails,
  organismLabel,
  type PolymerEntityView,
  taxonomyURL,
  uniProtSourceLabel,
  uniProtURL,
} from "@/lib/polymer-entities";

import styles from "./PolymerEntities.module.css";

/**
 * The entry's polymer entities as one row each, opening onto the construct,
 * mutations, references and sequence.
 *
 * A table on the same frame as the files below it: what an entity records is a
 * column, and the columns are what anyone compares between rows. The details
 * are collapsed because the row count is unbounded -- a crystal structure has
 * two or three entities, a ribosome has fifty -- and an always-open panel per
 * entity would push the models table off the page.
 */
export default function PolymerEntities({
  views,
}: {
  views: PolymerEntityView[];
}) {
  const panelPrefix = useId();
  const [openKeys, setOpenKeys] = useState<ReadonlySet<string>>(new Set());

  if (views.length === 0) {
    return null;
  }

  function toggle(key: string) {
    setOpenKeys((current) => {
      const next = new Set(current);
      if (!next.delete(key)) {
        next.add(key);
      }
      return next;
    });
  }

  return (
    <div className={styles.wrap}>
      {/* Columns are kept and scrolled sideways on a narrow window rather than
          stacked, the same way the file table handles its own. */}
      <div className={styles.scroll}>
        <table className={styles.table}>
          <thead>
            <tr>
              <th scope="col">
                <span className={styles.entityHead}>Entity</span>
              </th>
              <th scope="col">Molecule</th>
              <th scope="col">Chains</th>
              <th scope="col" className={styles.numHead}>
                Residues
              </th>
              <th scope="col">Organism</th>
              <th scope="col">
                <span className={styles.srOnly}>Details</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {views.map((view) => {
              const expandable = hasEntityDetails(view);
              const open = expandable && openKeys.has(view.key);
              const panelId = `${panelPrefix}-${view.key}`;

              return (
                <Fragment key={view.key}>
                  <EntityRow
                    view={view}
                    open={open}
                    expandable={expandable}
                    panelId={panelId}
                    onToggle={() => toggle(view.key)}
                  />
                  {open ? <DetailsRow view={view} panelId={panelId} /> : null}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// The row carries what the entities are compared on: which molecule it is, how
// much of it there is, and where it came from. Everything else waits behind the
// disclosure.
function EntityRow({
  view,
  open,
  expandable,
  panelId,
  onToggle,
}: {
  view: PolymerEntityView;
  open: boolean;
  expandable: boolean;
  panelId: string;
  onToggle: () => void;
}) {
  const chains = chainsLabel(view.chains);
  const organism = organismLabel(view.organisms);

  return (
    <tr
      className={styles.row}
      data-open={open ? "true" : undefined}
      // The whole row is the target, but the molecule name is a real button so
      // the row is reachable by keyboard, as in the file table.
      onClick={expandable ? onToggle : undefined}
    >
      <td>
        <span className={styles.entityId}>{view.entityId ?? "—"}</span>
      </td>

      <td className={styles.nameCell}>
        {expandable ? (
          <button
            type="button"
            className={styles.nameBtn}
            aria-expanded={open}
            aria-controls={panelId}
            onClick={(event) => {
              event.stopPropagation();
              onToggle();
            }}
            title={open ? "Hide entity details" : "Show entity details"}
          >
            <span className={styles.name}>{view.name}</span>
          </button>
        ) : (
          <span className={styles.name}>{view.name}</span>
        )}
      </td>

      <td className={`${styles.valueCell} ${styles.chains}`}>
        {chains ?? <span className={styles.absent}>—</span>}
      </td>

      <td className={`${styles.valueCell} ${styles.residues}`}>
        {view.residues !== null ? (
          `${view.residues} aa`
        ) : (
          <span className={styles.absent}>—</span>
        )}
      </td>

      <td className={`${styles.valueCell} ${styles.organism}`}>
        {organism ?? <span className={styles.absent}>—</span>}
      </td>

      <td className={styles.actionCell}>
        {expandable ? <ChevronIcon /> : null}
      </td>
    </tr>
  );
}

function DetailsRow({
  view,
  panelId,
}: {
  view: PolymerEntityView;
  panelId: string;
}) {
  const taxa = view.organisms.filter(
    (organism) => organism.ncbi_taxonomy_id != null,
  );

  return (
    <tr>
      <td className={styles.detailsCell} colSpan={6}>
        <div id={panelId} className={styles.details}>
          <dl className={styles.facts}>
            {view.construct ? (
              <>
                <dt>Construct</dt>
                <dd>{view.construct}</dd>
              </>
            ) : null}

            {view.mutationsText ? (
              <>
                <dt>{view.mutations?.length === 1 ? "Mutation" : "Mutations"}</dt>
                <dd>
                  {/* Split into chips only when the field really is a list of
                      substitutions; it is free text, and often is not. */}
                  {view.mutations ? (
                    <span className={styles.mutations}>
                      {view.mutations.map((mutation) => (
                        <span key={mutation} className={styles.mutation}>
                          {mutation}
                        </span>
                      ))}
                    </span>
                  ) : (
                    view.mutationsText
                  )}
                </dd>
              </>
            ) : null}

            {view.uniprotMappings.length > 0 ? (
              <>
                <dt>UniProt</dt>
                <dd>
                  <ul className={styles.references}>
                    {view.uniprotMappings.map((mapping) => (
                      <li key={`${mapping.accession}-${mapping.source}`}>
                        <ExternalLink
                          href={uniProtURL(mapping.accession)}
                          label={mapping.accession}
                        />
                        <span className={styles.caption}>
                          {uniProtSourceLabel(mapping.source)}
                          {mapping.unp_release
                            ? ` · release ${mapping.unp_release}`
                            : ""}
                        </span>
                      </li>
                    ))}
                  </ul>
                </dd>
              </>
            ) : null}

            {taxa.length > 0 ? (
              <>
                <dt>Taxonomy</dt>
                <dd>
                  <ul className={styles.references}>
                    {taxa.map((organism) => (
                      <li key={organism.ncbi_taxonomy_id}>
                        <ExternalLink
                          href={taxonomyURL(organism.ncbi_taxonomy_id as number)}
                          label={`NCBI ${organism.ncbi_taxonomy_id}`}
                        />
                        <span className={styles.caption}>
                          {organism.scientific_name}
                        </span>
                      </li>
                    ))}
                  </ul>
                </dd>
              </>
            ) : null}
            {view.sequence ? (
              <>
                <dt>Sequence</dt>
                <dd>
                  <div className={styles.sequence}>
                    <div className={styles.sequenceBody}>
                      {sequenceLines(view.sequence).map((line, index) => (
                        <div key={index} className={styles.sequenceLine}>
                          {line}
                        </div>
                      ))}
                    </div>
                    <CopySequenceButton sequence={view.sequence} />
                  </div>
                </dd>
              </>
            ) : null}
          </dl>
        </div>
      </td>
    </tr>
  );
}

// The residues without the reading spaces: what a search box or an alignment
// tool expects, not what the panel prints.
function CopySequenceButton({ sequence }: { sequence: string }) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(plainSequence(sequence));
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  }

  const label = copied ? "Copied" : "Copy the sequence";

  return (
    <button
      type="button"
      className={styles.copy}
      data-copied={copied ? "true" : undefined}
      title={label}
      aria-label={label}
      onClick={(event) => {
        event.stopPropagation();
        void copy();
      }}
    >
      {copied ? <CheckIcon /> : <CopyIcon />}
    </button>
  );
}

function ExternalLink({ href, label }: { href: string; label: string }) {
  return (
    <a
      className={styles.link}
      href={href}
      target="_blank"
      rel="noreferrer"
      onClick={(event) => event.stopPropagation()}
    >
      {label}
      <svg
        width="11"
        height="11"
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

function CopyIcon() {
  return (
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
      <rect x="9" y="9" width="12" height="12" rx="2" />
      <path d="M5 15V5a2 2 0 0 1 2-2h10" />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="m4 12.5 5.5 5.5L20 7" />
    </svg>
  );
}

function ChevronIcon() {
  return (
    <svg
      className={styles.chevron}
      width="10"
      height="10"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="3"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="m6 9 6 6 6-6" />
    </svg>
  );
}
