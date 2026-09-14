import type { DisagreementRegion } from "@/lib/model-agreement";

import styles from "./DisagreementRegions.module.css";

/**
 * The runs of chain the models place differently, one card each.
 *
 * The row on the board says where and how much; this says what to do about
 * it. A profile of a hundred and sixty columns does not tell anyone where to
 * look -- "residues 60 to 72, these two models are the ones that disagree,
 * this one split them in two" does.
 */
export default function DisagreementRegions({
  regions,
}: {
  regions: DisagreementRegion[];
}) {
  if (regions.length === 0) {
    return null;
  }
  return (
    <div className={styles.regions}>
      {regions.map((region) => (
        <article key={`${region.start}`} className={styles.region}>
          <header className={styles.regionHead}>
            <h4 className={styles.regionTitle}>{name(region)}</h4>
            <span className={styles.regionRange}>
              residues {region.start}–{region.end}
            </span>
          </header>
          <p className={styles.regionText}>{sentence(region)}</p>
        </article>
      ))}
    </div>
  );
}

// Named after the decade it starts in, the way a crystallographer talks about
// one: "the 60s loop". The range is printed beside it, so the name only has to
// be something to say out loud.
function name(region: DisagreementRegion): string {
  const decade = Math.floor(region.start / 10) * 10;
  return `${decade}s ${region.loop ? "loop" : "region"}`;
}

function sentence(region: DisagreementRegion): string {
  const parts = [
    `The models place this ${region.loop ? "loop" : "stretch"} up to ${region.peak.value.toFixed(
      2,
    )} Å apart, at residue ${region.peak.seq}.`,
  ];
  // Only worth saying with three models or more: with two, each of them is
  // exactly half the gap from the middle, so "furthest from the rest" names
  // one of a pair arbitrarily.
  const [furthest] = region.standouts;
  if (furthest && region.standouts.length > 2) {
    parts.push(
      `${capital(furthest.title)} sits furthest from the rest of them here, ${furthest.value.toFixed(
        2,
      )} Å from where they average out.`,
    );
  }
  if (region.split.length > 0) {
    parts.push(
      `${capital(list(region.split.map((model) => model.title)))} modelled more than one conformation in it.`,
    );
  }
  return parts.join(" ");
}

function list(titles: string[]): string {
  if (titles.length <= 1) {
    return titles[0] ?? "";
  }
  return `${titles.slice(0, -1).join(", ")} and ${titles[titles.length - 1]}`;
}

function capital(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

// A round number to put the axis at, so the height of a bar can be read off
// it rather than guessed from the tallest one.
function niceCeiling(value: number): number {
  const steps = [0.5, 1, 2, 3, 5, 10, 20, 50];
  return steps.find((step) => value <= step) ?? Math.ceil(value);
}

function format(value: number): string {
  return value >= 1 ? value.toFixed(1) : value.toFixed(2);
}
