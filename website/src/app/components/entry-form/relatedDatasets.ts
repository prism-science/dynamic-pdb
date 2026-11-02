import type { RelatedDataset } from "@/lib/api/entries";

import type { RelatedDatasetDraft } from "./types";

// Datasets kept elsewhere that relate to the entry, by DOI. The rules repeat
// the API's (backend/internal/validation/related_datasets.go), so a mistake
// shows beside its row: the API refuses a malformed dataset in every
// validation mode.

export const MAX_RELATED_DATASETS = 20;
export const MAX_RELATED_DATASET_LABEL_CHARS = 256;
export const MAX_RELATED_DATASET_DETAILS_CHARS = 2000;

/** A bare DOI. The registrant code has no minimum length: RCSB's own example
 *  is 10.000/10002/image_data/cif. */
export const DOI_PATTERN = /^10\.[0-9]+(\.[0-9]+)*\/\S+$/;

/** RCSB's examples of mmCIF _pdbx_related_exp_data_set.data_set_type. Any
 *  text is accepted. */
export const DATA_TYPE_SUGGESTIONS = [
  "diffraction image data",
  "NMR free induction decay data",
] as const;

/** The repositories RCSB lists for _pdbx_related_exp_data_set.db_source. Any
 *  text is accepted. */
export const REPOSITORY_SUGGESTIONS = [
  "Apollo - University of Cambridge Repository",
  "CXIDB",
  "DRYAD Database",
  "EMPIAR",
  "ESRF Data Portal",
  "ETH Zurich Research Collection",
  "Etsin",
  "Integrated Resource for Reproducibility in Macromolecular Crystallography",
  "International Union of Crystallography",
  "KAUST Repository",
  "Keele Data Repository",
  "MAXIV Public Data Repository",
  "MX-RDR (Macromolecular Xtallography Raw Data Repository)",
  "Manchester eScholar Services",
  "Mendeley Data",
  "PSI Public Data Repository",
  "ProteinDiffraction",
  "RMIT University Figshare",
  "SBGrid Data Bank",
  "Xtal Raw Data Archive",
  "Zenodo",
] as const;

const DOI_PREFIXES = [
  "https://doi.org/",
  "http://doi.org/",
  "https://dx.doi.org/",
  "http://dx.doi.org/",
  "doi:",
];

/** Strips a resolver URL or "doi:" in front of a DOI, as the API does, so
 *  "https://doi.org/10.15785/SBGRID/1303" becomes "10.15785/SBGRID/1303". */
export function normalizeDoi(text: string): string {
  const doi = text.trim();
  const prefix = DOI_PREFIXES.find((candidate) =>
    doi.toLowerCase().startsWith(candidate),
  );
  return prefix ? doi.slice(prefix.length).trim() : doi;
}

export function emptyRelatedDataset(): RelatedDatasetDraft {
  return {
    key: crypto.randomUUID(),
    doi: "",
    dataType: "",
    repository: "",
    details: "",
  };
}

export function relatedDatasetDraftsFromStored(
  datasets: RelatedDataset[] | undefined,
): RelatedDatasetDraft[] {
  return (datasets ?? []).map((dataset) => ({
    key: crypto.randomUUID(),
    doi: dataset.doi,
    dataType: dataset.data_type,
    repository: dataset.repository ?? "",
    details: dataset.details ?? "",
  }));
}

/** The datasets as the request carries them. Undefined when there are none,
 *  so a create request leaves the field out. */
export function relatedDatasetsRequest(
  drafts: RelatedDatasetDraft[],
): RelatedDataset[] | undefined {
  if (drafts.length === 0) {
    return undefined;
  }
  return drafts.map((draft) => ({
    doi: normalizeDoi(draft.doi),
    data_type: draft.dataType.trim(),
    repository: draft.repository.trim() || undefined,
    details: draft.details.trim() || undefined,
  }));
}

/** The id a row's messages carry: its position, counted from 1. */
export function relatedDatasetItem(index: number): string {
  return String(index + 1);
}
