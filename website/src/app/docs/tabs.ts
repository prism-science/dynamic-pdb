
export type DocsSection = {
  id: string;
  label: string;
    children?: DocsSection[];
};

const UPLOAD_SECTIONS: DocsSection[] = [
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

export const DOCS_TABS = [
  { key: "upload", label: "Uploading data", sections: UPLOAD_SECTIONS },
  { key: "api", label: "API", sections: [] as DocsSection[] },
] as const;

export type DocsTab = (typeof DOCS_TABS)[number];

export function docsTabFor(key: string | undefined): DocsTab {
  return DOCS_TABS.find((tab) => tab.key === key) ?? DOCS_TABS[0];
}

export function docsTabHref(tab: { key: string }): string {
  return tab.key === DOCS_TABS[0].key ? "/docs" : `/docs?tab=${tab.key}`;
}

export function sectionIds(sections: readonly DocsSection[]): string[] {
  return sections.flatMap((section) => [
    section.id,
    ...sectionIds(section.children ?? []),
  ]);
}
