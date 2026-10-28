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

const KELVIN_AT_ZERO_CELSIUS = 273.15;

export type TemperatureRange = { min: number; max: number };

/** The wwPDB's warning ranges, in kelvin, both ends included. The form warns
 *  outside them and never blocks: unusual conditions do occur. */
export const GROWTH_TEMPERATURE_RANGE: TemperatureRange = { min: 277, max: 316 };
export const COLLECTION_TEMPERATURE_RANGE: TemperatureRange = { min: 80, max: 300 };

export function inTemperatureRange(
  kelvin: number,
  range: TemperatureRange,
): boolean {
  return kelvin >= range.min && kelvin <= range.max;
}

export function celsiusToKelvin(celsius: number): number {
  return celsius + KELVIN_AT_ZERO_CELSIUS;
}

/** At most two decimals and no trailing zeros: 19.85, 20, -173.15. */
export function formatTemperature(value: number): string {
  return String(Math.round(value * 100) / 100);
}

/** The °C shown under a kelvin box as the depositor types: "293.15" →
 *  "20 °C". Null while the box holds no number. */
export function celsiusEcho(kelvinText: string): string | null {
  const kelvin = optionalNumber(kelvinText);
  if (kelvin === undefined) {
    return null;
  }
  return `${formatTemperature(kelvin - KELVIN_AT_ZERO_CELSIUS)} °C`;
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
