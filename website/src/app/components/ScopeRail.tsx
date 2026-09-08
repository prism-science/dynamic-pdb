import Link from "next/link";

import type { ScopeRailModel } from "@/lib/scope-rail";

import styles from "./ScopeRail.module.css";

/**
 * The models of one entry, and which one the page is currently about.
 *
 * There is no row for the entry itself. Every tab on this page is a tab of some
 * model -- the coordinates, the metrics, the files are all one model's -- so a
 * scope with no model selected would be a page with nothing to show. Landing on
 * the entry lands on a model.
 */
export default function ScopeRail({
  entryId,
  models,
  activeModelId,
  tab,
  addModelHref,
}: {
  entryId: string;
  models: ScopeRailModel[];
  activeModelId: string | null;
  /** The tab the reader is on, carried across the switch. */
  tab: string | null;
  /** Omitted for signed-out visitors, who cannot add one. */
  addModelHref?: string | null;
}) {
  const base = `/entries/${encodeURIComponent(entryId)}`;
  const carried = tab ? `?tab=${tab}` : "";

  return (
    <div className={styles.column}>
      <aside className={styles.rail} aria-label="Models">
        <p className={styles.heading}>Models</p>

        <nav className={styles.list}>
          {models.map((model) => (
            <Link
              key={model.id}
              className={styles.row}
              data-current={activeModelId === model.id ? "true" : undefined}
              aria-current={activeModelId === model.id ? "page" : undefined}
              href={`${base}/models/${encodeURIComponent(model.id)}${carried}`}
              scroll={false}
            >
              <span
                className={styles.thumb}
                data-empty={model.thumbnailImageURL ? undefined : "true"}
              >
                {model.thumbnailImageURL ? (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img src={model.thumbnailImageURL} alt="" />
                ) : null}
              </span>
              <span className={styles.name} title={model.title}>
                {model.title}
              </span>
            </Link>
          ))}
        </nav>

        {addModelHref ? (
          <Link className={styles.add} href={addModelHref}>
            + Add model
          </Link>
          ) : null}
      </aside>
    </div>
  );
}
