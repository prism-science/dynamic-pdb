import type { CompoundGroup, ModeledCompoundsView } from "@/lib/modeled-compounds";

import styles from "./ModeledCompoundList.module.css";

/**
 * The compounds a model's file models, for the entry page and the review
 * preview, under the heading each page gives it. One row per compound with its
 * copy count and tags; a row opens to the positions of its copies. Without a
 * read file (the typed lists stand) it renders nothing.
 *
 * Plain <details> rather than a client toggle: the list is a record to read,
 * and it opens without any script.
 */
export default function ModeledCompoundList({ view }: { view: ModeledCompoundsView }) {
  switch (view.state) {
    case "typed":
      return null;
    case "reading":
      return view.lastError ? (
        <p className={styles.status} data-tone="warning">
          The last attempt to read the model file failed ({view.lastError}); it is tried again
          shortly.
        </p>
      ) : (
        <p className={styles.status}>
          Reading the model file. Its compounds appear here a few minutes after it was uploaded.
        </p>
      );
    case "failed":
      return (
        <p className={styles.status} data-tone="warning">
          The model file could not be read: {view.reason}
        </p>
      );
    case "read":
      return <ReadCompounds groups={view.groups} complete={view.complete} warnings={view.warnings} />;
    default:
      throw new Error(`unknown modeled compounds view: ${String((view satisfies never) as unknown)}`);
  }
}

function ReadCompounds({
  groups,
  complete,
  warnings,
}: {
  groups: CompoundGroup[];
  complete: boolean;
  warnings: string[];
}) {
  return (
    <div>
      {!complete ? (
        <p className={styles.status} data-tone="warning">
          Partial list: {warnings.join("; ")}
        </p>
      ) : null}
      {groups.length === 0 ? (
        <p className={styles.status}>The file models no compounds outside its polymer chains.</p>
      ) : (
        <ul className={styles.list}>
          {groups.map((group) => (
            <li key={group.ccdId}>
              <details className={styles.group}>
                <summary className={styles.summary}>
                  <code className={styles.id}>{group.ccdId}</code>
                  <span className={styles.count}>
                    {group.copies.length === 1 ? "1 copy" : `${group.copies.length} copies`}
                  </span>
                  {group.tags.map((tag) => (
                    <span key={tag} className={styles.tag} data-tag={tag}>
                      {tag}
                    </span>
                  ))}
                  {group.reviewReasons.length > 0 ? (
                    <span className={styles.review}>Needs review</span>
                  ) : null}
                </summary>
                {group.reviewReasons.map((reason) => (
                  <p key={reason} className={styles.reason}>
                    {reason}
                  </p>
                ))}
                <ul className={styles.copies}>
                  {group.copies.map((copy, index) => (
                    <li key={`${copy.position}-${index}`} data-review={copy.reviewReason ? "true" : undefined}>
                      {copy.position}
                    </li>
                  ))}
                </ul>
              </details>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
