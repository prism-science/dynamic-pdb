import about from "../../../content/docs/about.md";
import accessData from "../../../content/docs/access-data.md";
import contributing from "../../../content/docs/contributing.md";
import dataOrganization from "../../../content/docs/data-organization.md";
import findAndCompare from "../../../content/docs/find-and-compare.md";
import reuse from "../../../content/docs/reuse.md";

const MARKDOWN: Record<string, string> = {
  about,
  "data-organization": dataOrganization,
  "find-and-compare": findAndCompare,
  "access-data": accessData,
  contributing,
  reuse,
};

export function markdownFor(slug: string): string | undefined {
  return MARKDOWN[slug];
}
