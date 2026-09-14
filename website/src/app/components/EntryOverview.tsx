import type { Entity, Entry } from "@/lib/api/entries";
import { dataTableEntities } from "@/lib/entities";
import type { MetricScale } from "@/lib/model-comparison";
import type { PolymerEntityView } from "@/lib/polymer-entities";
import {
  ImagePlaceholderIcon,
  InfoGrid,
  type MetadataFact,
  type MetricColumn,
  MetricTiles,
} from "@/app/entries/[entryId]/entry-view";
import DataTable from "./DataTable";
import MetricScales from "./MetricScales";
import PolymerEntities from "./PolymerEntities";

import styles from "./EntryOverview.module.css";

/** The selected model, flattened for display. */
export type SummaryModel = {
  title: string;
  previewURL: string | null;
  /** The depositor's description of it; null on a model without one. */
  details: string | null;
  /** Program, model type, counts -- as ordinary labelled fields. */
  facts: MetadataFact[];
  metrics: MetricColumn[];
};

/**
 * The Overview tab: what this model is, how it stands against the entry's
 * other models, and the record both belong to.
 *
 * Three bands, narrowing outwards. The model itself comes first -- its
 * picture and its fields. Then where each of its figures stands on the scale
 * that figure is judged on, with the entry's other models on the same scale.
 * Then the record: the macromolecules, which are the same in every model, and
 * the files.
 *
 * The figures are tracks rather than tiles because "0.171" on its own says
 * nothing about whether 0.171 is any good. A block of deltas against each
 * other model used to sit under them and has gone: it re-stated in arithmetic
 * what the tracks already draw, and the other models' own values are on the
 * track under the pointer.
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
  scales,
}: {
  entry: Entry;
  entities: PolymerEntityView[];
  artifacts: Map<string, Entity>;
  /** The selected model's artifacts and the entry's own, for the Data
   *  section: the levels, and the files at each. */
  dataEntities: Entity[];
  /** Null on an entry that has no models yet. */
  model: SummaryModel | null;
  /** One track per figure. Empty when no model of the entry carries any. */
  scales: MetricScale[];
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
          {/* Two descriptions, in the order they narrow: the structure first,
              then the model of it. Both are prose from a depositor, so both are
              paragraphs. */}
          {details ? <p className={styles.details}>{details}</p> : null}
          {model?.details ? (
            <p className={styles.details}>{model.details}</p>
          ) : null}

          {/* Split down the middle rather than handed to InfoGrid whole: the
              two halves have to line up with the column measure of this page,
              not with InfoGrid's own narrower one. */}
          {model && model.facts.length > 0 ? (
            <div className={styles.facts}>
              <InfoGrid facts={firstHalf(model.facts)} columns={1} />
              <InfoGrid facts={secondHalf(model.facts)} columns={1} />
            </div>
          ) : null}
        </div>
      </div>

      {/* A section of its own, the full width of the page. The tracks are
          rulers: squeezed into the right-hand column they had a third of the
          room and every model on them landed on top of the next. */}
      {model && scales.length > 0 ? (
        <section className={styles.section}>
          <h2 className={styles.heading}>Evaluations</h2>
          <MetricScales scales={scales} />
        </section>
      ) : model && model.metrics.length > 0 ? (
        <section className={styles.section}>
          <h2 className={styles.heading}>Evaluations</h2>
          <MetricTiles metrics={model.metrics} />
        </section>
      ) : null}

      {entities.length > 0 ? (
        <section className={styles.section}>
          <h2 className={styles.heading}>Macromolecules</h2>
          {/* Said out loud because the page is otherwise entirely about one
              model, and this table is the one thing on it that is not. */}
          <p className={styles.sectionHint}>
            The same in every model of this entry.
          </p>
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
