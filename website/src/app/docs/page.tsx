import Link from "next/link";
import { redirect } from "next/navigation";

import AppFooter from "../components/AppFooter";

import DocsBar from "./DocsBar";
import LegacyHashRedirect from "./LegacyHashRedirect";
import { docsGroups, docsHref } from "./pages";

import styles from "./docs.module.css";

const QUICK_START = [
  {
    title: "Browse public entries",
    body: "Search by PDB ID or protein name. No account needed.",
    href: "/browse",
  },
  {
    title: "Inspect a model",
    body: "View the structure, its files, processing history, and quality metrics.",
    href: "/docs/find-and-compare",
  },
  {
    title: "Download the data",
    body: "Download individual files, or retrieve records through the API.",
    href: "/docs/access-data",
  },
];

const API_EXAMPLE = `curl -H 'Accept: application/vnd.api+json' \\
  'https://dynamicpdb.com/api/v1/entries?pdb_id=7C24'`;

const CLI_EXAMPLE = "curl -fsSL https://dynamicpdb.com/install.sh | bash";

type SearchParamValue = string | string[] | undefined;
type Props = { searchParams?: Promise<Record<string, SearchParamValue>> };

export default async function Docs({ searchParams }: Props) {
  const params = (await searchParams) ?? {};
  const tab = Array.isArray(params.tab) ? params.tab[0] : params.tab;
  if (tab === "api") redirect("/docs/api");
  if (tab === "upload") redirect("/docs/cli");

  return (
    <>
      <main className={styles.page} aria-label="Docs">
        <LegacyHashRedirect />
        <DocsBar page={null} menuAlways />
        <div className={styles.landing}>
          <h1 className={styles.title}>Documentation</h1>
          <p className={styles.lead}>
            How The Dynamic PDB organizes structural models and their evidence,
            and how to find, compare, download, and contribute data.
          </p>

          <section className={styles.featured} aria-label="Start here">
            <div className={`${styles.feature} ${styles.featureWide}`}>
              <p className={styles.railLabel}>Quick start</p>
              <h2 className={styles.featureTitle}>Explore a model in three steps</h2>
              <ol className={styles.steps}>
                {QUICK_START.map((step) => (
                  <li key={step.href}>
                    <Link href={step.href} className={styles.stepLink}>
                      {step.title}
                    </Link>
                    <span className={styles.stepBody}>{step.body}</span>
                  </li>
                ))}
              </ol>
            </div>

            <Link href="/docs/api" className={styles.feature}>
              <p className={styles.railLabel}>API</p>
              <h2 className={styles.featureTitle}>Retrieve records programmatically</h2>
              <p className={styles.cardBody}>
                Public JSON records, no authentication. Suited to scripts and LLM agents.
              </p>
              <pre className={styles.featureCode}>
                <code>{API_EXAMPLE}</code>
              </pre>
              <span className={styles.featureMore}>API reference</span>
            </Link>

            <Link href="/docs/cli" className={styles.feature}>
              <p className={styles.railLabel}>CLI</p>
              <h2 className={styles.featureTitle}>Upload in batches</h2>
              <p className={styles.cardBody}>
                Scan a folder, review the generated manifest, and upload. For
                approved depositors.
              </p>
              <pre className={styles.featureCode}>
                <code>{CLI_EXAMPLE}</code>
              </pre>
              <span className={styles.featureMore}>CLI guide</span>
            </Link>
          </section>

          {docsGroups().map(({ group, pages }) => (
            <section key={group} className={styles.landingGroup}>
              <h2 className={styles.railLabel}>{group}</h2>
              <div className={styles.cards}>
                {pages.map((page) => (
                  <Link key={page.slug} href={docsHref(page)} className={styles.card}>
                    <span className={styles.cardTitle}>{page.title}</span>
                    <span className={styles.cardBody}>{page.description}</span>
                  </Link>
                ))}
              </div>
            </section>
          ))}
        </div>
      </main>
      <AppFooter />
    </>
  );
}
