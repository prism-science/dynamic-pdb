"use client";

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";

import type { ScopeRailModel } from "@/lib/scope-rail";
import {
  bestMetricValues,
  formatMetric,
  metricSpec,
  metricSpecs,
  sortModelsByMetric,
  type MetricSpec,
} from "@/lib/model-metrics";

import styles from "./ScopeRail.module.css";

/** Kept in step with the transition the rows are given below. */
const ROW_SLIDE_MS = 320;

/**
 * How long the pointer has to mean it.
 *
 * Opening waits a moment so that crossing the column on the way somewhere else
 * does nothing at all; closing waits longer, because leaving by a few pixels --
 * rounding a corner, overshooting a chip -- is not leaving. Either wait is
 * cancelled if the pointer comes back, so a hand moving in and out never starts
 * the animation, let alone reverses it half way.
 */
const OPEN_AFTER_MS = 110;
const CLOSE_AFTER_MS = 260;

const dateFormatter = new Intl.DateTimeFormat("en-US", {
  year: "numeric",
  month: "short",
  day: "numeric",
});

/**
 * The models of one entry, and which one the page is currently about.
 *
 * There is no row for the entry itself. Every tab on this page is a tab of some
 * model -- the coordinates, the metrics, the files are all one model's -- so a
 * scope with no model selected would be a page with nothing to show. Landing on
 * the entry lands on a model.
 *
 * Closed it is the list it has always been. Open -- on hover, or as soon as
 * anything inside it takes focus -- it widens over the record and every row
 * unfolds the five figures the models are told apart by, with a row of chips
 * that orders the list by any one of them. The reader compares without leaving
 * the model they are on.
 */
export default function ScopeRail({
  entryId,
  models,
  activeModelId,
  tab,
  sort,
  direction,
  addModelHref,
}: {
  entryId: string;
  models: ScopeRailModel[];
  activeModelId: string | null;
  /** The tab the reader is on, carried across the switch. */
  tab: string | null;
  /** The metric the list is ordered by, from the URL; null keeps deposit order. */
  sort: string | null;
  /** "asc" or "desc" from the URL; anything else means the metric's own. */
  direction: string | null;
  /** Omitted for signed-out visitors, who cannot add one. */
  addModelHref?: string | null;
}) {
  const [open, setOpen] = useState(false);
  const intent = useRef<number | null>(null);

  const cancelIntent = () => {
    if (intent.current !== null) {
      window.clearTimeout(intent.current);
      intent.current = null;
    }
  };
  const intendTo = (next: boolean) => {
    cancelIntent();
    intent.current = window.setTimeout(() => {
      intent.current = null;
      setOpen(next);
    }, next ? OPEN_AFTER_MS : CLOSE_AFTER_MS);
  };
  // Keyboard has no hesitation to model: focus lands, the panel is open.
  const settleTo = (next: boolean) => {
    cancelIntent();
    setOpen(next);
  };
  useEffect(() => cancelIntent, []);

  const [order, setOrder] = useState<{ spec: MetricSpec | null; ascending: boolean }>(
    () => {
      const spec = metricSpec(sort);
      return {
        spec,
        // Each metric knows which way is better, so the URL only has to carry a
        // direction when the reader turned it around.
        ascending:
          direction === "desc"
            ? false
            : direction === "asc"
              ? true
              : (spec?.lowerIsBetter ?? true),
      };
    },
  );

  const base = `/entries/${encodeURIComponent(entryId)}`;

  const query = (spec: MetricSpec | null, asc: boolean) => {
    const params = new URLSearchParams();
    if (tab) {
      params.set("tab", tab);
    }
    if (spec) {
      params.set("sort", String(spec.key));
      if (asc !== spec.lowerIsBetter) {
        params.set("dir", asc ? "asc" : "desc");
      }
    }
    const search = params.toString();
    return search ? `?${search}` : "";
  };

  const sorted = useMemo(
    () =>
      order.spec === null
        ? models
        : sortModelsByMetric(models, order.spec, order.ascending),
    [models, order],
  );
  const best = useMemo(() => bestMetricValues(models), [models]);

  // Slide the rows from where they were to where the new order puts them, so
  // the reorder is something the reader watches rather than a list that blinks
  // into a different arrangement.
  //
  // The "where they were" is taken in the click handler, in the same frame as
  // the click, and thrown away once it has been used. Keeping a running record
  // of every row's position instead would mean comparing against a measurement
  // made in some earlier state of the rail -- and the rail changes height
  // without React rendering anything, because it opens on hover in CSS. Rows
  // would then be flung by the difference between the closed rail and the open
  // one, which is most of the panel.
  const rowRefs = useRef<Map<string, HTMLAnchorElement>>(new Map());
  const topsBeforeSort = useRef<Map<string, number> | null>(null);
  const listRef = useRef<HTMLElement | null>(null);
  useLayoutEffect(() => {
    const before = topsBeforeSort.current;
    topsBeforeSort.current = null;
    if (before === null) {
      return;
    }

    let moved = false;
    rowRefs.current.forEach((node, id) => {
      const previous = before.get(id);
      if (previous === undefined) {
        return;
      }
      const delta = previous - node.getBoundingClientRect().top;
      if (delta === 0) {
        return;
      }
      moved = true;
      node.style.transition = "none";
      node.style.transform = `translateY(${delta}px)`;
      requestAnimationFrame(() => {
        node.style.transition = "transform 300ms cubic-bezier(0.2, 0.7, 0.3, 1)";
        node.style.transform = "";
      });
    });

    // A row in flight is not where it looks like it is, so a click during the
    // slide would open the model the reader was not pointing at. The list stops
    // taking clicks until everything has landed.
    const list = listRef.current;
    if (!moved || list === null) {
      return;
    }
    list.style.pointerEvents = "none";
    const settled = window.setTimeout(() => {
      list.style.pointerEvents = "";
    }, ROW_SLIDE_MS);
    return () => {
      window.clearTimeout(settled);
      list.style.pointerEvents = "";
    };
  }, [sorted]);

  const pick = (spec: MetricSpec) => {
    if (!window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      const tops = new Map<string, number>();
      rowRefs.current.forEach((node, id) => {
        tops.set(id, node.getBoundingClientRect().top);
      });
      topsBeforeSort.current = tops;
    }
    const next =
      order.spec?.key === spec.key
        ? { spec, ascending: !order.ascending }
        : { spec, ascending: spec.lowerIsBetter };
    setOrder(next);
    // The order belongs in the URL: it survives the move to another model, it
    // comes back on reload, and it can be sent to someone else. Written
    // straight to history rather than pushed through the router, because
    // nothing the server renders depends on it.
    if (typeof window !== "undefined") {
      window.history.replaceState(
        null,
        "",
        `${window.location.pathname}${query(next.spec, next.ascending)}`,
      );
    }
  };

  return (
    <div className={styles.column}>
      <aside
        className={styles.rail}
        aria-label="Models"
        data-open={open ? "true" : undefined}
        onPointerEnter={() => intendTo(true)}
        onPointerLeave={() => intendTo(false)}
        onFocus={() => settleTo(true)}
        onBlur={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
            settleTo(false);
          }
        }}
      >
        <p className={styles.heading}>
          Models
          <span className={styles.count}>{models.length}</span>
        </p>

        <div className={styles.toolbar} role="group" aria-label="Sort models">
          <span className={styles.toolbarLabel}>Sort by</span>
          {metricSpecs.map((spec) => {
            const active = order.spec?.key === spec.key;
            return (
              <button
                key={String(spec.key)}
                type="button"
                className={styles.chip}
                data-active={active ? "true" : undefined}
                aria-pressed={active}
                onClick={() => pick(spec)}
              >
                {spec.short}
                {active ? (
                  <span aria-hidden="true">{order.ascending ? " ↑" : " ↓"}</span>
                ) : null}
              </button>
            );
          })}
        </div>

        <nav className={styles.list} ref={listRef}>
          {sorted.map((model, index) => {
            const ranked =
              order.spec !== null &&
              typeof model.metrics[order.spec.key] === "number";
            return (
              <Link
                key={model.id}
                ref={(node) => {
                  if (node) {
                    rowRefs.current.set(model.id, node);
                  } else {
                    rowRefs.current.delete(model.id);
                  }
                }}
                className={styles.row}
                data-current={activeModelId === model.id ? "true" : undefined}
                aria-current={activeModelId === model.id ? "page" : undefined}
                href={`${base}/models/${encodeURIComponent(model.id)}${query(
                  order.spec,
                  order.ascending,
                )}`}
                scroll={false}
              >
                <span className={styles.rowGutter}>
                  <span className={styles.rank} aria-hidden="true">
                    {ranked ? index + 1 : ""}
                  </span>
                  <span
                    className={styles.thumb}
                    data-empty={model.thumbnailImageURL ? undefined : "true"}
                  >
                    {model.thumbnailImageURL ? (
                      // eslint-disable-next-line @next/next/no-img-element
                      <img src={model.thumbnailImageURL} alt="" />
                    ) : null}
                  </span>
                </span>

                <span className={styles.rowBody}>
                  <span className={styles.rowText}>
                    <span className={styles.name} title={model.title}>
                      {model.title}
                    </span>
                    <span className={styles.meta}>
                      {[model.software, formatDate(model.createdAt)]
                        .filter(Boolean)
                        .join(" · ")}
                    </span>
                  </span>

                  <span className={styles.strip}>
                  {metricSpecs.map((spec) => {
                    const value = model.metrics[spec.key];
                    const has = typeof value === "number";
                    return (
                      <span
                        key={String(spec.key)}
                        className={styles.stripCell}
                        data-status={has ? spec.status(value) : "none"}
                        data-sorted={
                          order.spec?.key === spec.key ? "true" : undefined
                        }
                        data-best={
                          has && value === best.get(spec.key) ? "true" : undefined
                        }
                      >
                        <span className={styles.stripLabel}>{spec.short}</span>
                        <span className={styles.stripValue}>
                          {has ? formatMetric(spec, value) : "—"}
                        </span>
                      </span>
                      );
                    })}
                  </span>
                </span>
              </Link>
            );
          })}
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

function formatDate(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "" : dateFormatter.format(date);
}
