import Link from "next/link";

import AppFooter from "../components/AppFooter";

import ApiReference from "./ApiReference";
import DocsRail from "./DocsRail";
import UploadDocs from "./upload-docs";
import { DOCS_TABS, docsTabFor, docsTabHref, type DocsTab } from "./tabs";

import styles from "./docs.module.css";

type SearchParamValue = string | string[] | undefined;
type Props = { searchParams?: Promise<Record<string, SearchParamValue>> };

export default async function Docs({ searchParams }: Props) {
  const params = (await searchParams) ?? {};
  const tab = docsTabFor(firstValue(params.tab));

  return (
    <>
      <main className={styles.page} aria-label="Docs">
        <h1 className={styles.srOnly}>Docs</h1>

        <div className={styles.barOuter} data-docs-bar>
          <div className={styles.bar}>
            <TabSwitch current={tab} />
          </div>
        </div>

        {tab.key === "api" ? (
          <ApiReference />
        ) : (
          <div className={styles.shell}>
            <DocsRail current={tab} />
            <div className={styles.body}>
              <UploadDocs />
            </div>
          </div>
        )}
      </main>

      <AppFooter />
    </>
  );
}

function TabSwitch({ current }: { current: DocsTab }) {
  return (
    <div className={styles.tabs} role="tablist">
      {DOCS_TABS.map((tab) => (
        <Link
          key={tab.key}
          href={docsTabHref(tab)}
          role="tab"
          aria-selected={tab.key === current.key}
          className={`${styles.tab} ${
            tab.key === current.key ? styles.tabOn : ""
          }`}
        >
          {tab.label}
        </Link>
      ))}
    </div>
  );
}

function firstValue(value: SearchParamValue): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}
