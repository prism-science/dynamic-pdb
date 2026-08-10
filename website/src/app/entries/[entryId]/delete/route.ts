import { NextResponse } from "next/server";

import { ApiRequestError, deleteEntry } from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";

type DeleteEntryRouteParams = {
  params: Promise<{ entryId: string }>;
};

export async function DELETE(_request: Request, { params }: DeleteEntryRouteParams) {
  const session = await getAuthSession();
  if (session == null) {
    return NextResponse.json(
      { error: "You must be signed in to delete an entry." },
      { status: 401 },
    );
  }

  const { entryId } = await params;
  try {
    await deleteEntry(session.token, entryId);
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return NextResponse.json(
        { error: "Failed to delete the entry." },
        { status: error.status ?? 502 },
      );
    }
    throw error;
  }

  return new Response(null, { status: 204 });
}
