import { redirect } from "next/navigation";

import { listEntries } from "@/lib/api/entries";

// Production and dev have separate databases, so the tour entry is looked up
// rather than linked by ID: 7C24 where it exists, else the first public entry.
const PREFERRED_PDB_ID = "7C24";

export async function GET() {
  const entryId = await tourEntryId();
  redirect(entryId ? `/entries/${entryId}?tour=1` : "/browse");
}

async function tourEntryId(): Promise<string | null> {
  try {
    const [preferred] = await listEntries(undefined, {
      pdbIds: [PREFERRED_PDB_ID],
      limit: 1,
    });
    if (preferred) return preferred.id;
    const [first] = await listEntries(undefined, { limit: 1 });
    return first?.id ?? null;
  } catch {
    return null;
  }
}
