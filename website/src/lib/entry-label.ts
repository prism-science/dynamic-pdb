type EntryLabelSource = {
  id: string;
  title: string | null;
  external_refs?: Record<string, string> | null;
};

export type EntryIdentity = {
  /** What a reader recognises the entry by: its PDB code, or its own title
   *  when it has no code. Null for an entry that carries neither. */
  name: string | null;
  /** The Dynamic PDB id, which every entry has. */
  id: string;
};

/**
 * The two halves of an entry's label, for places that set them differently --
 * the code is what a reader scans for, the id is what a link or an API call
 * needs, and in one run of monospace they read as one opaque string.
 */
export function entryIdentity(entry: EntryLabelSource): EntryIdentity {
  const pdbID = stringValue(entry.external_refs?.pdb);
  if (pdbID) {
    return { name: `PDB ${pdbID.toUpperCase()}`, id: entry.id };
  }
  return { name: stringValue(entry.title), id: entry.id };
}

export function formatEntryLabel(entry: EntryLabelSource): string {
  const { name, id } = entryIdentity(entry);
  return name ? `${name} | ${id}` : id;
}

function stringValue(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}
