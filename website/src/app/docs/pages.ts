export type DocsSection = {
  id: string;
  label: string;
  children?: DocsSection[];
};

export type DocsPage = {
  slug: string;
  title: string;
  description: string;
  group: string;
  kind: "markdown" | "cli" | "api";
};

/** Reading order. Previous and next links follow it, and so does the nav. */
export const DOCS_PAGES: DocsPage[] = [
  {
    slug: "about",
    title: "About The Dynamic PDB",
    description: "What the database is, what you can do with it, and its current scope.",
    group: "Overview",
    kind: "markdown",
  },
  {
    slug: "data-organization",
    title: "How data are organized",
    description: "Entries, models, files, runs, metrics, and the L0 to L3 data levels.",
    group: "Overview",
    kind: "markdown",
  },
  {
    slug: "find-and-compare",
    title: "Find and compare models",
    description: "Search the catalog, inspect a model, and compare alternatives.",
    group: "Use the data",
    kind: "markdown",
  },
  {
    slug: "access-data",
    title: "Access and download data",
    description: "Download files from the website or retrieve records through the API.",
    group: "Use the data",
    kind: "markdown",
  },
  {
    slug: "api",
    title: "API reference",
    description: "Endpoints, parameters, and response formats.",
    group: "Use the data",
    kind: "api",
  },
  {
    slug: "contributing",
    title: "Contribute data",
    description: "Prepare a submission, choose a method, and follow it through review.",
    group: "Contribute",
    kind: "markdown",
  },
  {
    slug: "cli",
    title: "Uploading with the CLI",
    description: "Install the command-line client, write a manifest, and upload in batches.",
    group: "Contribute",
    kind: "cli",
  },
  {
    slug: "reuse",
    title: "Reuse, acknowledgment, and feedback",
    description: "Licensing, how to acknowledge the resource, and how to reach us.",
    group: "Community",
    kind: "markdown",
  },
];

export const CLI_SECTIONS: DocsSection[] = [
  { id: "installation", label: "Installation" },
  { id: "cli-authentication", label: "Authentication" },
  {
    id: "create-a-manifest",
    label: "Create a manifest",
    children: [
      { id: "which-entries", label: "Which entries get uploaded" },
      { id: "manifest-artifacts", label: "Artifacts" },
      { id: "metadata-and-metrics", label: "Metadata and metrics" },
    ],
  },
  { id: "upload-files", label: "Upload files" },
];

export function docsPageFor(slug: string): DocsPage | undefined {
  return DOCS_PAGES.find((page) => page.slug === slug);
}

export function docsHref(page: DocsPage): string {
  return `/docs/${page.slug}`;
}

export function neighbors(slug: string): {
  previous: DocsPage | null;
  next: DocsPage | null;
} {
  const index = DOCS_PAGES.findIndex((page) => page.slug === slug);
  if (index === -1) return { previous: null, next: null };
  return {
    previous: DOCS_PAGES[index - 1] ?? null,
    next: DOCS_PAGES[index + 1] ?? null,
  };
}

export function docsGroups(): { group: string; pages: DocsPage[] }[] {
  const groups: { group: string; pages: DocsPage[] }[] = [];
  for (const page of DOCS_PAGES) {
    const last = groups[groups.length - 1];
    if (last?.group === page.group) last.pages.push(page);
    else groups.push({ group: page.group, pages: [page] });
  }
  return groups;
}

export function slugify(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9\s-]/g, "")
    .trim()
    .replace(/\s+/g, "-")
    .replace(/-+/g, "-");
}

export function plainText(markdown: string): string {
  return markdown
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/[*_`]/g, "")
    .trim();
}

/** The h2 and h3 headings of a page, nested, ignoring anything inside code fences. */
export function extractHeadings(markdown: string): DocsSection[] {
  const sections: DocsSection[] = [];
  let fence: string | null = null;
  for (const line of markdown.split("\n")) {
    const marker = line.match(/^\s*(```|~~~)/)?.[1];
    if (marker) {
      fence = fence === null ? marker : fence === marker ? null : fence;
      continue;
    }
    if (fence) continue;

    const heading = line.match(/^(##|###)\s+(.+?)\s*#*\s*$/);
    if (!heading) continue;
    const label = plainText(heading[2]);
    const section = { id: slugify(label), label };
    const parent = sections[sections.length - 1];
    if (heading[1] === "###" && parent) {
      parent.children = [...(parent.children ?? []), section];
    } else {
      sections.push(section);
    }
  }
  return sections;
}

export function sectionIds(sections: readonly DocsSection[]): string[] {
  return sections.flatMap((section) => [
    section.id,
    ...sectionIds(section.children ?? []),
  ]);
}

/** Hashes the single-page docs used, so old links land on the CLI page. */
export function legacyHashTarget(hash: string): string | null {
  const id = hash.replace(/^#/, "");
  if (!id) return null;
  return sectionIds(CLI_SECTIONS).includes(id) ? `/docs/cli#${id}` : null;
}
