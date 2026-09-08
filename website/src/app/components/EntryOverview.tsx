import type { Entity, Entry } from "@/lib/api/entries";
import { dataTableEntities } from "@/lib/entities";
import type { PolymerEntityView } from "@/lib/polymer-entities";
import {
  ImagePlaceholderIcon,
  InfoGrid,
  type MetadataFact,
  type MetricColumn,
  MetricTiles,
} from "@/app/entries/[entryId]/entry-view";
import DataTable from "./DataTable";
import PolymerEntities from "./PolymerEntities";

import styles from "./EntryOverview.module.css";

/** The selected model, flattened for display. */
export type SummaryModel = {
  title: string;
  previewURL: string | null;
  /** Program, model type, counts -- as ordinary labelled fields. */
  facts: MetadataFact[];
  metrics: MetricColumn[];
};

/**
 * The Summary tab: the model's picture, what the structure is, and the numbers.
 *
 * The picture is the selected model's, and so is everything in the right-hand
 * column below the description -- switch models in the rail and that column
 * changes while the left one does not. Macromolecules and Data sit under both
 * at full width, because neither table fits in half a page.
 *
 * The crystal is deliberately absent: pH and temperatures have their own tab,
 * and a fact printed in two places is a fact that will disagree with itself.
 */
export default function EntryOverview({
  entry,
  entities,
  artifacts,
  dataEntities,
  model,
}: {
  entry: Entry;
  entities: PolymerEntityView[];
  artifacts: Map<string, Entity>;
  /** The selected model's artifacts and the entry's own, for the Data
   *  section: the levels, and the files at each. */
  dataEntities: Entity[];
  /** Null on an entry that has no models yet. */
  model: SummaryModel | null;
}) {
  const title = entry.title?.trim() ?? null;
  const details = entry.details?.trim() ?? null;
  const heading = model?.title ?? title;

  return (
    <div className={styles.overview}>
      <div className={styles.head} data-solo={model ? undefined : "true"}>
        {model ? (
          <div
            className={styles.shot}
            data-empty={model.previewURL ? undefined : "true"}
          >
            {model.previewURL ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img src={model.previewURL} alt="" />
            ) : (
              <ImagePlaceholderIcon size={40} />
            )}
          </div>
        ) : null}

        <div className={styles.about}>
          {heading ? <h2 className={styles.title}>{heading}</h2> : null}
          {/* Which structure this model is of. Dropped when the heading is
              already the structure's own name, so it is never printed twice. */}
          {model && title ? <p className={styles.subtitle}>{title}</p> : null}
          {details ? <p className={styles.details}>{details}</p> : null}

          {/* Split down the middle rather than handed to InfoGrid whole: the
              two halves have to line up with the column measure of this page,
              not with InfoGrid's own narrower one. */}
          {model && model.facts.length > 0 ? (
            <div className={styles.facts}>
              <InfoGrid facts={firstHalf(model.facts)} columns={1} />
              <InfoGrid facts={secondHalf(model.facts)} columns={1} />
            </div>
          ) : null}

          {/* Inside this column, under the fields it belongs with: the numbers
              describe the same model those fields describe. Only Macromolecules
              is wide, because only Macromolecules needs the width. */}
          {model && model.metrics.length > 0 ? (
            <section className={styles.evaluations}>
              <h2 className={styles.heading}>Evaluations</h2>
              <MetricTiles metrics={model.metrics} />
            </section>
          ) : null}
        </div>
      </div>

      {entities.length > 0 ? (
        <section className={styles.section}>
          <h2 className={styles.heading}>Macromolecules</h2>
          <PolymerEntities views={entities} artifacts={artifacts} />
        </section>
      ) : null}

      {/* What this model was made from and what came out of it, by level. It
          had a tab of its own, which cost a click to answer "what files are
          here" -- a question a summary should already answer. */}
      {dataTableEntities(dataEntities).length > 0 ? (
        <section className={styles.section}>
          <h2 className={styles.heading}>Data</h2>
          <DataTable entities={dataEntities} />
        </section>
      ) : null}
    </div>
  );
}

function firstHalf(facts: MetadataFact[]): MetadataFact[] {
  return facts.slice(0, Math.ceil(facts.length / 2));
}

function secondHalf(facts: MetadataFact[]): MetadataFact[] {
  return facts.slice(Math.ceil(facts.length / 2));
}
