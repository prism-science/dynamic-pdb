import type { EntryCrystal, EntryCrystallography } from "@/lib/api/entries";

import type { CrystalDraft, DiffractionDraft } from "./types";

export function emptyCrystal(): CrystalDraft {
  return {
    key: crypto.randomUUID(),
    ph: "",
    growthTemperature: "",
    diffractions: [],
  };
}

export function emptyDiffraction(): DiffractionDraft {
  return { key: crypto.randomUUID(), temperature: "" };
}

/**
 * The ids the request gives each crystal and dataset. Crystals are numbered
 * from 1, and datasets from 1 across the whole entry, the way the archive
 * numbers exptl_crystal and diffrn rows. Validation messages and API errors
 * name items by these ids, so both use this one function.
 */
export function crystalIds(
  crystals: CrystalDraft[],
): { crystalId: string; diffractionIds: string[] }[] {
  let nextDiffraction = 1;
  return crystals.map((crystal, index) => ({
    crystalId: String(index + 1),
    diffractionIds: crystal.diffractions.map(() => String(nextDiffraction++)),
  }));
}

/** A blank value is left out; it is never sent as 0. */
export function optionalNumber(text: string): number | undefined {
  const trimmed = text.trim();
  if (!trimmed) {
    return undefined;
  }
  const value = Number(trimmed);
  return Number.isFinite(value) ? value : undefined;
}

export function crystalIsEmpty(crystal: CrystalDraft): boolean {
  return (
    !crystal.ph.trim() &&
    !crystal.growthTemperature.trim() &&
    crystal.diffractions.length === 0
  );
}

export function crystallographyRequest(
  crystals: CrystalDraft[],
): EntryCrystallography | undefined {
  if (crystals.length === 0) {
    return undefined;
  }
  const ids = crystalIds(crystals);
  return {
    crystals: crystals.map((crystal, index): EntryCrystal => {
      const ph = optionalNumber(crystal.ph);
      const temperatureKelvin = optionalNumber(crystal.growthTemperature);
      const growth = {
        ...(ph !== undefined ? { ph } : {}),
        ...(temperatureKelvin !== undefined
          ? { temperature_kelvin: temperatureKelvin }
          : {}),
      };
      const diffractions = crystal.diffractions.map((diffraction, position) => {
        const temperature = optionalNumber(diffraction.temperature);
        return {
          id: ids[index].diffractionIds[position],
          ...(temperature !== undefined
            ? { temperature_kelvin: temperature }
            : {}),
        };
      });
      return {
        id: ids[index].crystalId,
        ...(Object.keys(growth).length > 0 ? { growth } : {}),
        ...(diffractions.length > 0 ? { diffractions } : {}),
      };
    }),
  };
}
