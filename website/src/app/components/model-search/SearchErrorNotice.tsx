import type { ReactNode } from "react";

import { searchFailureMessage, type SearchFailure } from "@/lib/model-search";

import styles from "./model-search.module.css";

/**
 * A search that did not answer, said as what to do next: the API's own reason
 * for a 400, when to try again for a 429 or 504.
 */
export default function SearchErrorNotice({
  failure,
  children,
}: {
  failure: SearchFailure;
  /** The way out: Try again, Clear filters. */
  children?: ReactNode;
}) {
  const message = searchFailureMessage(failure);
  return (
    <div className={styles.errorNotice} role="alert" data-status={failure.status ?? undefined}>
      <p className={styles.errorTitle}>{message.title}</p>
      <p className={styles.errorBody}>{message.body}</p>
      {children ? <div className={styles.errorActions}>{children}</div> : null}
    </div>
  );
}
