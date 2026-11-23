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

/**
 * Reports a model search once its first page is on screen: which filters it
 * used (the URL Umami records already holds their values) and how many models
 * matched, so searches that find nothing stand out.
 */
export function ModelSearchTracker({
  filters,
  results,
  capped,
}: {
  /** The search's parameter names, comma-separated. */
  filters: string;
  results: number;
  /** Whether the count stopped at the cap ("10,000+"). */
  capped: boolean;
}) {
  useEffect(() => {
    track("model-search", { filters, results, capped });
  }, [filters, results, capped]);

  return null;
}
