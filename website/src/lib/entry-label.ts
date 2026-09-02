type EntryLabelSource = {
  id: string;
  name: string;
  metadata?: Record<string, unknown> | null;
};

export function formatEntryLabel(entry: EntryLabelSource): string {
  const externalRefs = entry.metadata?.external_refs;
  if (isRecord(externalRefs)) {
    const pdbID = stringValue(externalRefs.pdb);
    if (pdbID) {
      return `PDB ${pdbID.toUpperCase()} | ${entry.id}`;
    }
  }

  return `${entry.name} | ${entry.id}`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function stringValue(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}
