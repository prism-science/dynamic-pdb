"use client";

import { useEffect } from "react";

import { track } from "@/lib/analytics";

/**
 * Reports a search once its results are on screen.
 *
 * The header field, the landing field, and the landing's example terms all land
 * on /browse?query=..., so this one spot counts every search. The result count
 * rides along so that searches finding nothing stand out.
 */
export default function SearchTracker({
  query,
  results,
}: {
  query: string;
  results: number;
}) {
  useEffect(() => {
    if (query) {
      track("search", { query, results });
    }
  }, [query, results]);

  return null;
}
