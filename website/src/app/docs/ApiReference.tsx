"use client";

import { useEffect, useRef, useState } from "react";

import "@scalar/api-reference/style.css";
import styles from "./docs.module.css";

export default function ApiReference() {
  const host = useRef<HTMLDivElement>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const specUrl = "/api/openapi.yaml";

  useEffect(() => {
    let cancelled = false;
    let destroy: (() => void) | undefined;

    const loaded = import("@scalar/api-reference");
    loaded.catch(() => {});

    const fetched = fetch(specUrl, {
      headers: { Accept: "application/yaml" },
    }).then((response) => {
      if (!response.ok) throw new Error(`responded ${response.status}`);
    });

    void (async () => {
      try {
        await fetched;
      } catch (error) {
        if (!cancelled) {
          setFailure(error instanceof Error ? error.message : "request failed");
        }
        return;
      }
      if (cancelled) return;

      const { createApiReference } = await loaded;
      if (cancelled || !host.current) return;

      const instance = createApiReference(host.current, {
        url: specUrl,
        theme: "none",
        darkMode: false,
        hideDarkModeToggle: true,
        showDeveloperTools: "never",
        hideClientButton: true,
        hiddenClients: true,
        hideTestRequestButton: true,
        documentDownloadType: "none",
        hideModels: true,
        operationsSorter: orderApiOperations,
        hideSearch: true,
        mcp: { disabled: true },
        customCss: SCALAR_CSS,
      });

      const host_ = host.current;
      stripSchemaNames(host_);
      const watcher = new MutationObserver(() => stripSchemaNames(host_));
      watcher.observe(host_, { childList: true, subtree: true });
      host_.addEventListener("click", toggleFromName);

      destroy = () => {
        watcher.disconnect();
        host_.removeEventListener("click", toggleFromName);
        instance.destroy();
      };
    })();

    return () => {
      cancelled = true;
      destroy?.();
    };
  }, [specUrl]);

  if (failure) {
    return (
      <div className={styles.fallback}>
        <h2 className={styles.fallbackTitle}>The reference is not available</h2>
        <p className={styles.fallbackBody}>
          This page renders the API&rsquo;s OpenAPI description from{" "}
          <a className={styles.link} href={specUrl}>
            {specUrl}
          </a>
          . That request {failure}.
        </p>
        <p className={styles.fallbackBody}>
          If you are running this locally, that route reads{" "}
          <code className={styles.code}>backend/api/openapi.yaml</code>.
        </p>
      </div>
    );
  }

  return <div ref={host} className={styles.scalar} />;
}

function orderApiOperations(
  a: { method: string; path: string },
  b: { method: string; path: string },
) {
  return operationOrder(a) - operationOrder(b);
}

function operationOrder(operation: { method: string; path: string }) {
  const key = `${operation.method.toLowerCase()} ${operation.path}`;
  return API_OPERATION_ORDER[key] ?? Number.MAX_SAFE_INTEGER;
}

const API_OPERATION_ORDER: Record<string, number> = {
  "post /v1/entries": 0,
  "get /v1/entries/{entry_id}": 1,
  "get /v1/entries": 2,
  "get /v1/entries/{entry_id}/similar-entries": 3,
  "post /v1/entries/{entry_id}/models": 4,
  "get /v1/entries/{entry_id}/models/{model_id}": 5,
  "get /v1/entries/{entry_id}/models": 6,
  "post /v1/entries/{entry_id}/revisions": 7,
  "patch /v1/entries/{entry_id}/revisions/{revision_id}": 8,
  "get /v1/entries/{entry_id}/revisions/{revision_id}": 9,
  "get /v1/entries/{entry_id}/revisions": 10,
  "get /v1/entries/revisions": 11,
  "post /v1/entries/{entry_id}/models/{model_id}/revisions": 12,
  "patch /v1/entries/{entry_id}/models/{model_id}/revisions/{revision_id}": 13,
  "get /v1/entries/{entry_id}/models/{model_id}/revisions/{revision_id}": 14,
  "get /v1/entries/{entry_id}/models/{model_id}/revisions": 15,
  "get /v1/users/{user_id}/entries/{entry_id}/revisions/{revision_id}": 16,
  "get /v1/users/{user_id}/entries/{entry_id}/revisions": 17,
  "get /v1/users/{user_id}/entries/revisions": 18,
  "patch /v1/users/{user_id}/entries/{entry_id}/models/{model_id}/revisions/{revision_id}": 19,
  "get /v1/users/{user_id}/entries/{entry_id}/models/{model_id}/revisions/{revision_id}": 20,
  "get /v1/users/{user_id}/entries/{entry_id}/models/{model_id}/revisions": 21,
};

function toggleFromName(event: MouseEvent) {
  const target = event.target as Element | null;
  if (!target || target.closest("button, a")) return;

  const row = target.closest(".property-name")?.closest("li.property");
  const toggle = row?.querySelector<HTMLElement>(
    ":scope > .children > .schema-card > .schema-properties > .schema-card-title",
  );
  toggle?.click();
}

function stripSchemaNames(root: HTMLElement) {
  const isName = /^[A-Za-z][\w.-]*(\[\])?$/;

  for (const value of root.querySelectorAll(".property-detail-value")) {
    const text = [...value.childNodes].filter(
      (node): node is Text =>
        node.nodeType === Node.TEXT_NODE && node.textContent!.trim() !== "",
    );
    const name = text.at(-1);
    const separator = text.at(-2);
    if (!name || !separator) continue;
    if (separator.data.trim() !== "·") continue;
    if (!isName.test(name.data.trim())) continue;
    separator.data = "";
    name.data = "";
  }
}

const SCALAR_CSS = `
:root {
  --scalar-color-1: #1a1e2a;
  --scalar-color-2: #3d414f;
  --scalar-color-3: #5f616b;
  --scalar-color-accent: #663be4;
  --scalar-background-1: #ffffff;
  --scalar-background-2: #f2f3f8;
  --scalar-background-3: #eaebf4;
  --scalar-background-accent: #f1edfd;
  --scalar-border-color: #eaebf4;

  --scalar-sidebar-background-1: #ffffff;
  --scalar-sidebar-color-1: #1a1e2a;
  --scalar-sidebar-color-2: #3d414f;
  --scalar-sidebar-border-color: #eaebf4;
  --scalar-sidebar-item-hover-background: #f2f3f8;
  --scalar-sidebar-item-hover-color: #1a1e2a;
  --scalar-sidebar-item-active-background: #f1edfd;
  --scalar-sidebar-color-active: #663be4;
  --scalar-sidebar-search-background: #f2f3f8;
  --scalar-sidebar-search-border-color: #eaebf4;
  --scalar-sidebar-search-color: #5f616b;

  --scalar-button-1: #663be4;
  --scalar-button-1-hover: #5730c7;
  --scalar-button-1-color: #ffffff;

  --scalar-font: var(--font-sans);
  --scalar-font-code: var(--font-mono);
  --scalar-radius: 6px;
  --scalar-radius-lg: 8px;
  --scalar-radius-xl: 12px;

    --scalar-color-green: #0e7c57;
  --scalar-color-red: #b4231d;
  --scalar-color-orange: #8a6414;
}

`;
