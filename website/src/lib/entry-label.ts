type EntryLabelSource = {
  id: string;
  title: string | null;
  external_refs?: Record<string, string> | null;
};

export function formatEntryLabel(entry: EntryLabelSource): string {
  const pdbID = stringValue(entry.external_refs?.pdb);
  if (pdbID) {
    return `PDB ${pdbID.toUpperCase()} | ${entry.id}`;
  }

  const title = stringValue(entry.title);
  return title ? `${title} | ${entry.id}` : entry.id;
}

function stringValue(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}
