import type { RelatedDataset } from "@/lib/api/entries";

/** Where a DOI resolves. The API stores bare DOIs such as 10.15785/SBGRID/1303. */
export function doiUrl(doi: string): string {
  return `https://doi.org/${doi}`;
}

/** "diffraction image data · SBGrid Data Bank": what the dataset holds and
 *  where it is kept. */
export function relatedDatasetSummary(dataset: RelatedDataset): string {
  return [dataset.data_type, dataset.repository]
    .filter((part): part is string => Boolean(part?.trim()))
    .join(" · ");
}
