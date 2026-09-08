import type { EntryCrystallography } from "@/lib/api/entries";

/** One diffraction dataset, carrying the conditions of the crystal it came
 *  from. A crystal recorded without any dataset still gets a row, so its growth
 *  conditions are not dropped. */
export type DiffractionDataset = {
  key: string;
  datasetId: string | null;
  crystalId: string;
  ph: number | null;
  growthKelvin: number | null;
  collectionKelvin: number | null;
};

export type CrystallographyView = {
  datasets: DiffractionDataset[];
};

/**
 * The entry's crystals and the datasets collected from them, flattened to one
 * row each.
 *
 * Crystal-level conditions repeat down their crystal's rows rather than sitting
 * in a caption above the table: a caption beside a table reads as a stray
 * label, and a column can be scanned.
 *
 * Null when nothing was measured. An old deposit like 4HHB still declares a
 * crystal and a diffraction and records no value for either; a table of dashes
 * tells the reader less than no section at all.
 */
export function crystallographyView(
  crystallography: EntryCrystallography | undefined,
): CrystallographyView | null {
  const datasets: DiffractionDataset[] = [];

  for (const [index, crystal] of (crystallography?.crystals ?? []).entries()) {
    const crystalId = trimmed(crystal.id) ?? String(index + 1);
    const ph = finiteNumber(crystal.growth?.ph);
    const growthKelvin = finiteNumber(crystal.growth?.temperature_kelvin);
    const diffractions = crystal.diffractions ?? [];

    if (diffractions.length === 0) {
      datasets.push({
        key: crystalId,
        datasetId: null,
        crystalId,
        ph,
        growthKelvin,
        collectionKelvin: null,
      });
      continue;
    }

    for (const [position, diffraction] of diffractions.entries()) {
      const datasetId = trimmed(diffraction.id) ?? String(position + 1);
      datasets.push({
        key: `${crystalId}/${datasetId}`,
        datasetId,
        crystalId,
        ph,
        growthKelvin,
        collectionKelvin: finiteNumber(diffraction.temperature_kelvin),
      });
    }
  }

  const measured = datasets.some(
    (dataset) =>
      dataset.ph !== null ||
      dataset.growthKelvin !== null ||
      dataset.collectionKelvin !== null,
  );

  return measured ? { datasets } : null;
}

export function formatKelvin(value: number): string {
  return `${numberFormatter.format(value)} K`;
}

export function formatPH(value: number): string {
  return numberFormatter.format(value);
}

function finiteNumber(value: number | undefined): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function trimmed(value: string | undefined): string | null {
  const text = value?.trim();
  return text ? text : null;
}

const numberFormatter = new Intl.NumberFormat("en-US", {
  maximumFractionDigits: 1,
});
