"use client";

import { useRef, useState, type KeyboardEvent } from "react";
import dynamic from "next/dynamic";

import ChevronIcon from "./ChevronIcon";
import { LINEAGE_EXAMPLES } from "./lineage-examples";

import styles from "./docs.module.css";

const LineageFlow = dynamic(() => import("../components/LineageFlow"), {
  ssr: false,
  loading: () => <div className={styles.examplesLoading}>Loading diagram…</div>,
});

export default function LineageExamples() {
  const [index, setIndex] = useState(0);
  const tabs = useRef<(HTMLButtonElement | null)[]>([]);
  const count = LINEAGE_EXAMPLES.length;
  const example = LINEAGE_EXAMPLES[index];

  const select = (next: number, focus = false) => {
    const wrapped = (next + count) % count;
    setIndex(wrapped);
    if (focus) tabs.current[wrapped]?.focus();
  };

  const onKeyDown = (event: KeyboardEvent) => {
    const moves: Record<string, number> = {
      ArrowRight: index + 1,
      ArrowLeft: index - 1,
      Home: 0,
      End: count - 1,
    };
    if (!(event.key in moves)) return;
    event.preventDefault();
    select(moves[event.key], true);
  };

  return (
    <figure className={styles.examples} aria-label="Provenance examples">
      <div className={styles.examplesTabs} role="tablist" onKeyDown={onKeyDown}>
        {LINEAGE_EXAMPLES.map((item, position) => (
          <button
            key={item.id}
            ref={(element) => {
              tabs.current[position] = element;
            }}
            type="button"
            role="tab"
            id={`example-tab-${item.id}`}
            aria-selected={position === index}
            aria-controls="example-panel"
            tabIndex={position === index ? 0 : -1}
            className={`${styles.examplesTab} ${
              position === index ? styles.examplesTabOn : ""
            }`}
            onClick={() => select(position)}
          >
            {item.label}
          </button>
        ))}
      </div>

      <div
        id="example-panel"
        role="tabpanel"
        aria-labelledby={`example-tab-${example.id}`}
        className={styles.examplesCanvas}
      >
        <LineageFlow key={example.id} lineage={example.lineage} interactive={false} />
      </div>

      <figcaption className={styles.examplesFooter}>
        <p className={styles.examplesCaption}>{example.caption}</p>
        <div className={styles.examplesStepper}>
          <button
            type="button"
            className={styles.examplesArrow}
            aria-label="Previous example"
            onClick={() => select(index - 1)}
          >
            <ChevronIcon direction="left" />
          </button>
          <span className={styles.examplesCount}>
            {index + 1} / {count}
          </span>
          <button
            type="button"
            className={styles.examplesArrow}
            aria-label="Next example"
            onClick={() => select(index + 1)}
          >
            <ChevronIcon direction="right" />
          </button>
        </div>
      </figcaption>
    </figure>
  );
}
