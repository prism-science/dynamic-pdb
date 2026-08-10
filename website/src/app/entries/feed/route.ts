import { NextResponse } from "next/server";

import { ApiRequestError, listEntries } from "@/lib/api/entries";

const defaultLimit = 50;
const maxLimit = 100;

export async function GET(request: Request) {
  const url = new URL(request.url);
  const limit = boundedPositiveInteger(url.searchParams.get("limit"), defaultLimit, maxLimit);
  const offset = positiveInteger(url.searchParams.get("offset"), 0);
  const query = url.searchParams.get("query")?.trim() ?? "";

  try {
    const items = await listEntries(undefined, {
      query,
      limit,
      offset,
    });
    return NextResponse.json({ items });
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return NextResponse.json({ items: [] }, { status: error.status ?? 502 });
    }
    throw error;
  }
}

function boundedPositiveInteger(value: string | null, fallback: number, max: number) {
  const parsed = positiveInteger(value, fallback);
  return Math.min(parsed, max);
}

function positiveInteger(value: string | null, fallback: number) {
  if (value == null) {
    return fallback;
  }
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}
