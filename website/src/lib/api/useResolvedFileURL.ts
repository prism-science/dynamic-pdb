"use client";

import { useEffect, useState } from "react";

import { parseExtFileReference, resolveFileURL } from "./ext";

type Resolution = {
  source: string | null;
  url: string | null;
  error: string | null;
};

export function useResolvedFileURL(source: string | null): {
  url: string | null;
  loading: boolean;
  error: string | null;
} {
  const isExtFile = source ? parseExtFileReference(source) !== null : false;
  const [resolution, setResolution] = useState<Resolution>({
    source: null,
    url: null,
    error: null,
  });

  useEffect(() => {
    if (!source || !isExtFile) {
      return;
    }
    let cancelled = false;
    void resolveFileURL(source)
      .then((url) => {
        if (!cancelled) {
          setResolution({ source, url, error: null });
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setResolution({
            source,
            url: null,
            error:
              error instanceof Error
                ? error.message
                : "Could not resolve the Ext file.",
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [isExtFile, source]);

  if (!isExtFile) {
    return { url: source, loading: false, error: null };
  }
  if (resolution.source !== source) {
    return { url: null, loading: true, error: null };
  }
  return {
    url: resolution.url,
    loading: false,
    error: resolution.error,
  };
}
