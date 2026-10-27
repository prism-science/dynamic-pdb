import type { components } from "@/lib/api/schema.gen";

export type ArchiveStatus = components["schemas"]["EntryArchiveStatus"];

/** One entry that replaced an obsoleted one, linked to the registry's own entry for it when the
 *  registry has one, otherwise to its RCSB page. */
export type Successor = {
  pdbId: string;
  href: string;
  external: boolean;
};

export type ObsoleteNotice = {
  /** When the PDB obsoleted the entry, for example "Jan 17, 2024"; null when it did not say. */
  date: string | null;
  successors: Successor[];
};

const dateFormatter = new Intl.DateTimeFormat("en-US", {
  year: "numeric",
  month: "short",
  day: "numeric",
  timeZone: "UTC",
});

/** What the entry page says about an entry the PDB obsoleted, or null while the PDB still
 *  serves it. */
export function obsoleteNotice(status: ArchiveStatus | undefined): ObsoleteNotice | null {
  if (status?.state !== "obsolete") {
    return null;
  }
  const obsoletedAt = status.obsoleted_at ? new Date(status.obsoleted_at) : null;
  return {
    date:
      obsoletedAt && !Number.isNaN(obsoletedAt.getTime())
        ? dateFormatter.format(obsoletedAt)
        : null,
    successors: status.superseded_by.map((successor) =>
      successor.entry_id
        ? {
            pdbId: successor.pdb_id,
            href: `/entries/${encodeURIComponent(successor.entry_id)}`,
            external: false,
          }
        : {
            pdbId: successor.pdb_id,
            href: `https://www.rcsb.org/structure/${encodeURIComponent(successor.pdb_id)}`,
            external: true,
          },
    ),
  };
}
