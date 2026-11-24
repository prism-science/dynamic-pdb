import type { RelatedDataset } from "@/lib/api/entries";

/** Where a DOI resolves. The API stores bare DOIs such as 10.15785/SBGRID/1303. */
export function doiUrl(doi: string): string {
  return `https://doi.org/${doi}`;
}

/** Whether text is a landing page the API accepts: an absolute http or https
 *  URL with a host, without spaces or a user name and password
 *  (backend/internal/validation/related_datasets.go). */
export function isLandingUrl(text: string): boolean {
  // The API reads "https:example.org" as having no host; URL() would add the
  // slashes, so the prefix is checked on the text itself.
  if (!/^https?:\/\//i.test(text) || /\s/.test(text)) {
    return false;
  }
  let url: URL;
  try {
    url = new URL(text);
  } catch {
    return false;
  }
  return url.hostname !== "" && url.username === "" && url.password === "";
}

/** The dataset's landing page, or null when it has none the page may link to. */
export function landingUrl(dataset: RelatedDataset): string | null {
  return dataset.landing_url && isLandingUrl(dataset.landing_url)
    ? dataset.landing_url
    : null;
}

/** What names the dataset and where that name links: its DOI to the DOI
 *  resolver; without a DOI, its accession to its landing page, or to nothing
 *  when it has none. */
export function relatedDatasetName(dataset: RelatedDataset): {
  label: string;
  href: string | null;
} {
  if (dataset.doi) {
    return { label: dataset.doi, href: doiUrl(dataset.doi) };
  }
  return { label: dataset.accession ?? "", href: landingUrl(dataset) };
}

/** One key per dataset, as the API tells duplicates apart: DOIs are
 *  case-insensitive, and an accession is compared case-insensitively within
 *  its repository. */
export function relatedDatasetKey(dataset: RelatedDataset): string {
  if (dataset.doi) {
    return `doi:${dataset.doi.toLowerCase()}`;
  }
  const repository = (dataset.repository ?? "").toLowerCase();
  return `accession:${repository}\u0000${(dataset.accession ?? "").toLowerCase()}`;
}

/** "diffraction image data · SBGrid Data Bank": what the dataset holds and
 *  where it is kept, and its accession when a DOI already names it. */
export function relatedDatasetSummary(dataset: RelatedDataset): string {
  return [dataset.data_type, dataset.repository, dataset.doi ? dataset.accession : undefined]
    .filter((part): part is string => Boolean(part?.trim()))
    .join(" · ");
}

/** Labels for how a dataset relates to the experiment the entry describes
 *  (RelatedDatasetRelation in backend/api/openapi.yaml). */
const RELATION_LABELS: Record<string, string> = {
  source_data: "Source data",
  similar_sample: "Similar sample",
  other: "Other relation",
};

/** Labels for what shows the link (RelatedDatasetLinkEvidence). */
const LINK_EVIDENCE_LABELS: Record<string, string> = {
  pdb_record: "cited by the PDB entry",
  repository_record: "named in the repository's record",
  repository_text: "named on the repository's page",
  publication: "named in a publication",
  depositor_statement: "stated by the depositor",
};

/** A value's label, or the value itself: the API may add values, and an
 *  older page shows a new one as text rather than hiding it. */
function labelOf(labels: Record<string, string>, value: string | undefined): string | null {
  if (!value?.trim()) {
    return null;
  }
  return Object.hasOwn(labels, value) ? labels[value] : value;
}

/** "Similar sample · named on the repository's page": the stated relation and
 *  what shows it, or null when the dataset states neither. */
export function relatedDatasetRelationSummary(dataset: RelatedDataset): string | null {
  const relation = labelOf(RELATION_LABELS, dataset.relation);
  const evidence = labelOf(LINK_EVIDENCE_LABELS, dataset.link_evidence);
  if (!relation && !evidence) {
    return null;
  }
  return [relation, evidence].filter(Boolean).join(" · ");
}
