"use client";

import { Fragment, useState } from "react";

import type { Entity } from "@/lib/api/entries";
import FilePreviewModal from "./FilePreviewModal";

import styles from "./PolymerEntities.module.css";

/** A chain the depositor renamed: `C [auth D]` is asym C, called D in the
 *  coordinates. The FASTA header carries both, glued together. */
const AUTH_CHAIN = /^(.*?)\s*\[\s*auth\s+(.+?)\s*\]$/i;

/**
 * An entity's chains, as one control that opens the FASTA they were read from.
 *
 * The preview rather than the file: the artifact's parsed records are what the
 * files tab shows too, and they are already on the page -- the object behind
 * `payload.file_url` needs a signed URL the browser does not have.
 */
export default function EntityChains({
  chains,
  entity,
}: {
  chains: string[];
  entity: Entity;
}) {
  const [open, setOpen] = useState(false);

  return (
    <>
      {/* One control for the whole cell, not one per letter: the chains of an
          entity are one FASTA record, so `A, C` is a single thing to click.
          Splitting them into tokens still happens in here -- it is a detail of
          how the value is drawn, not something to press. */}
      <button
        type="button"
        className={styles.chains}
        onClick={() => setOpen(true)}
        title={`Open ${entity.name}`}
      >
        {chains.map((chain, index) => (
          <Fragment key={chain}>
            {index > 0 ? ", " : null}
            <ChainToken chain={chain} />
          </Fragment>
        ))}
      </button>

      <FilePreviewModal
        entity={open ? entity : null}
        onClose={() => setOpen(false)}
      />
    </>
  );
}

function ChainToken({ chain }: { chain: string }) {
  const auth = AUTH_CHAIN.exec(chain);
  if (!auth) {
    return <span className={styles.mono}>{chain}</span>;
  }

  return (
    <>
      <span className={styles.mono}>{auth[1]}</span>
      <span className={styles.auth}>auth {auth[2]}</span>
    </>
  );
}
