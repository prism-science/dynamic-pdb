"use client";

import { useState, type FormEvent } from "react";

import { lookupArchiveModelAction } from "@/app/entries/[entryId]/models/new/archiveActions";
import { createModelAction } from "@/app/entries/[entryId]/models/new/actions";

import {
  buildArchiveModelInput,
  reRefinementTargets,
  suggestedModelType,
  type ArchiveModelLookup,
} from "./archiveModel";
import { MODEL_PURPOSES, MODEL_TYPES } from "./types";
import styles from "./form.module.css";

/**
 * Adds a deposition that belongs to this entry's experiment, such as a
 * re-refinement of its data, as a model of the entry. The values come from the
 * archive and the deposited file; only what they cannot say is asked for.
 */
export default function ArchiveModelForm({
  entryId,
  entryPdbId,
}: {
  entryId: string;
  entryPdbId: string | null;
}) {
  const [pdbId, setPdbId] = useState("");
  const [lookup, setLookup] = useState<ArchiveModelLookup | null>(null);
  const [modelType, setModelType] = useState("");
  const [purpose, setPurpose] = useState("Refinement");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const search = async () => {
    setBusy(true);
    setError(null);
    setLookup(null);
    const result = await lookupArchiveModelAction(pdbId);
    setBusy(false);
    if ("error" in result) {
      setError(result.error);
      return;
    }
    setLookup(result);
    setModelType(suggestedModelType(result));
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!lookup) {
      return;
    }
    setBusy(true);
    setError(null);
    const result = await createModelAction(
      entryId,
      buildArchiveModelInput(lookup, { modelType, purpose }),
    );
    if (result) {
      setError(result.error);
      setBusy(false);
    }
  };

  const targets = lookup ? reRefinementTargets(lookup.relatedEntries) : [];
  const entryPdb = entryPdbId?.trim().toUpperCase() ?? null;
  const unrelated = lookup !== null && (entryPdb === null || !targets.includes(entryPdb));

  return (
    <form className={styles.form} onSubmit={submit} noValidate>
      <section className={styles.field}>
        <label className={styles.label} htmlFor="archive-model-pdb">
          PDB ID
        </label>
        <div className={styles.sourceRow}>
          <input
            id="archive-model-pdb"
            className={styles.input}
            value={pdbId}
            onChange={(event) => setPdbId(event.target.value)}
            placeholder="44PS"
            spellCheck={false}
          />
          <button
            type="button"
            className={styles.secondary}
            disabled={busy || pdbId.trim() === ""}
            onClick={() => void search()}
          >
            Look up
          </button>
        </div>
      </section>

      {lookup ? (
        <>
          <section className={styles.field}>
            <span className={styles.label}>Title</span>
            <span>{lookup.title}</span>
          </section>
          {targets.length > 0 ? (
            <section className={styles.field}>
              <span className={styles.label}>Re-refinement of</span>
              <span>{targets.join(", ")}</span>
            </section>
          ) : null}
          {unrelated ? (
            <div className={styles.missingWarning}>
              The PDB does not record {lookup.pdbId} as a re-refinement of this entry
              {entryPdb ? ` (${entryPdb})` : ""}. Add it here only if it comes from this entry&apos;s
              experimental data.
            </div>
          ) : null}
          <section className={styles.field}>
            <label className={styles.label} htmlFor="archive-model-type">
              Model type
            </label>
            <select
              id="archive-model-type"
              className={styles.select}
              value={modelType}
              onChange={(event) => setModelType(event.target.value)}
            >
              {MODEL_TYPES.map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
          </section>
          <section className={styles.field}>
            <label className={styles.label} htmlFor="archive-model-purpose">
              Purpose
            </label>
            <select
              id="archive-model-purpose"
              className={styles.select}
              value={purpose}
              onChange={(event) => setPurpose(event.target.value)}
            >
              {MODEL_PURPOSES.map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
          </section>
        </>
      ) : null}

      {error ? <p className={styles.error}>{error}</p> : null}

      {lookup ? (
        <div className={styles.actions}>
          <div className={styles.actionsRight}>
            <button type="submit" className={styles.primary} disabled={busy}>
              Add this deposition
            </button>
          </div>
        </div>
      ) : null}
    </form>
  );
}
