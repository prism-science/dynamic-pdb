"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import type { ModelSearchResult } from "@/lib/api/entries";
import type { SearchFailure } from "@/lib/model-search";

import ModelResultRow from "./ModelResultRow";
import SearchErrorNotice from "./SearchErrorNotice";
import styles from "./model-search.module.css";

type FeedPage = { items: ModelSearchResult[]; nextCursor: string | null };
type FeedFailure = { error: SearchFailure };

/**
 * The model search pages after the first, loaded as the reader scrolls, from
 * /browse/model-feed with the same filters. A page that fails says why and
 * offers Try again; it never reads as the end of the results.
 */
export default function ModelResultsScroll({
  searchQuery,
  initialCursor,
}: {
  /** The search's query string, as modelSearchQuery writes it. */
  searchQuery: string;
  /** Cursor of the page after the server-rendered one; null when there is none. */
  initialCursor: string | null;
}) {
  const [results, setResults] = useState<ModelSearchResult[]>([]);
  const [cursor, setCursor] = useState(initialCursor);
  const [loading, setLoading] = useState(false);
  const [failure, setFailure] = useState<SearchFailure | null>(null);
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  // The page request in flight, so a new search can stop it: its answer
  // belongs to the old search and must not reach this one's list.
  const requestRef = useRef<AbortController | null>(null);
  const hasMore = cursor !== null;

  useEffect(() => {
    requestRef.current?.abort();
    requestRef.current = null;
    setResults([]);
    setCursor(initialCursor);
    setLoading(false);
    setFailure(null);
  }, [initialCursor, searchQuery]);

  useEffect(() => () => requestRef.current?.abort(), []);

  const loadMore = useCallback(async () => {
    if (loading || cursor === null || failure !== null) {
      return;
    }
    const request = new AbortController();
    requestRef.current = request;
    setLoading(true);
    try {
      const params = new URLSearchParams(searchQuery);
      params.set("cursor", cursor);
      let response: Response;
      let body: FeedPage | FeedFailure | null;
      try {
        response = await fetch(`/browse/model-feed?${params.toString()}`, {
          cache: "no-store",
          signal: request.signal,
        });
        body = (await response.json().catch(() => null)) as FeedPage | FeedFailure | null;
      } catch {
        if (!request.signal.aborted) {
          setFailure({ status: null });
        }
        return;
      }
      if (request.signal.aborted) {
        return;
      }
      if (!response.ok || body === null || "error" in body) {
        setFailure(body !== null && "error" in body ? body.error : { status: response.status });
        return;
      }
      const page = body;
      setResults((current) => [...current, ...page.items]);
      setCursor(page.nextCursor);
    } finally {
      if (requestRef.current === request) {
        requestRef.current = null;
        setLoading(false);
      }
    }
  }, [cursor, failure, loading, searchQuery]);

  useEffect(() => {
    const sentinel = sentinelRef.current;
    if (sentinel == null || !hasMore || failure !== null) {
      return;
    }
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry?.isIntersecting) {
          void loadMore();
        }
      },
      { rootMargin: "600px 0px" },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [failure, hasMore, loadMore]);

  return (
    <>
      {results.length > 0 ? (
        <ol className={styles.rows}>
          {results.map((result) => (
            <ModelResultRow key={result.id} result={result} />
          ))}
        </ol>
      ) : null}
      {failure !== null ? (
        <SearchErrorNotice failure={failure}>
          <button type="button" className={styles.retryButton} onClick={() => setFailure(null)}>
            Try again
          </button>
        </SearchErrorNotice>
      ) : null}
      {hasMore && failure === null ? (
        <div ref={sentinelRef} className={styles.loadSentinel} aria-hidden="true" />
      ) : null}
      {loading ? (
        <p className={styles.loading} role="status">
          Loading more models…
        </p>
      ) : null}
    </>
  );
}
