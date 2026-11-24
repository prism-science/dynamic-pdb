import type { Metadata } from "next";
import Link from "next/link";

import { getApiBaseUrl } from "@/lib/api/baseUrl";
import { bulkDownloadAvailable } from "@/lib/api/downloads";
import {
  downloadLimits,
  scriptExamples,
  SCRIPT_FILE_NAME,
  SCRIPT_PATH,
  selectionFromQuery,
} from "@/lib/download-instructions";
import { FILE_CHOICES, fileChoiceText, REUSE_LICENSE_URL, REUSE_TERMS } from "@/lib/download-selection";

import AppFooter from "../components/AppFooter";

import styles from "./download.module.css";

export const metadata: Metadata = {
  title: "Download · Dynamic PDB",
};

type SearchParamValue = string | string[] | undefined;
type Props = { searchParams?: Promise<Record<string, SearchParamValue>> };

/**
 * How to download many models at once (B4): the panel, its limits, the reuse
 * terms, and the script for any size. Opened from the panel with
 * ?selection=<id>, the examples are written out for that selection.
 *
 * While the server has bulk download off, the page says so in two sentences:
 * there is no bulk download yet, and how to get data until there is. It
 * promises nothing about a mechanism that is not on, and offers no service
 * over the address: that line is for questions, not for requesting a copy by
 * mail.
 */
export default async function Download({ searchParams }: Props) {
  const available = await bulkDownloadAvailable();
  if (!available) {
    return (
      <>
        <main className={styles.page}>
          <h1 className={styles.title}>Download</h1>

          <div className={styles.prose}>
            <p className={styles.lead}>Bulk download is not available yet.</p>
            {/* No full stop after the address: a period against a mailto link
                reads as part of it, and the sentence is the last one anyway. */}
            <p className={styles.note}>
              Individual files can be downloaded from any entry page. For
              questions, contact{" "}
              <a className={styles.email} href="mailto:prism-core@astera.org">
                prism-core@astera.org
              </a>
            </p>
          </div>
        </main>

        <AppFooter />
      </>
    );
  }

  const params = (await searchParams) ?? {};
  const selectionId = selectionFromQuery(params.selection);
  const examples = scriptExamples({ selectionId, apiBaseUrl: getApiBaseUrl() });

  return (
    <>
      <main className={styles.page}>
        <h1 className={styles.title}>Download</h1>

        <div className={styles.prose}>
          <p className={styles.lead}>
            Select models, check what the archive will hold, then download it as one zip. For more than one
            archive holds, the script downloads the same files one by one.
          </p>

          <section className={styles.section} aria-labelledby="download-how">
            <h2 id="download-how" className={styles.heading}>
              How it works
            </h2>
            <ol className={styles.steps}>
              <li>
                Tick models in <Link href="/browse">search results</Link> or on entry pages, or select all results
                of a search. The selection stays as you move between pages and searches.
              </li>
              <li>Open the download panel at the bottom of the page and choose the files:</li>
            </ol>
            <ul className={styles.choices}>
              {FILE_CHOICES.map((choice) => {
                const text = fileChoiceText(choice);
                return (
                  <li key={choice}>
                    <span className={styles.choiceName}>{text.label}</span>{" "}
                    <code className={styles.code}>{choice}</code>: {text.description}
                  </li>
                );
              })}
            </ul>
            <ol className={styles.steps} start={3}>
              <li>
                Check the archive: it shows the models, the files and their sizes, and anything left out and why,
                before anything is built.
              </li>
              <li>
                Build the archive, then download the zip. Its <code className={styles.code}>manifest.tsv</code>{" "}
                lists every file with its status and SHA-256.
              </li>
            </ol>
          </section>

          <section className={styles.section} aria-labelledby="download-limits">
            <h2 id="download-limits" className={styles.heading}>
              Limits
            </h2>
            <ul className={styles.list}>
              {downloadLimits().map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
          </section>

          <section className={styles.section} aria-labelledby="download-reuse">
            <h2 id="download-reuse" className={styles.heading}>
              Reuse
            </h2>
            <p className={styles.text} data-reuse-terms="">
              {REUSE_TERMS}
            </p>
            <p className={styles.note}>
              License of Dynamic PDB and PDB archive files:{" "}
              <a href={REUSE_LICENSE_URL} target="_blank" rel="noreferrer">
                CC0 1.0
              </a>
              . Related datasets and files held by other repositories follow their repository&rsquo;s terms; the
              manifest links each one.
            </p>
          </section>

          <section className={styles.section} id="script" aria-labelledby="download-script">
            <h2 id="download-script" className={styles.heading}>
              Download with a script
            </h2>
            <p className={styles.text} data-script-selection={selectionId ?? ""}>
              {selectionId === null ? (
                <>
                  Replace <code className={styles.code}>&lt;selection id&gt;</code> with the selection ID the download
                  panel shows.
                </>
              ) : (
                <>
                  The examples are for selection <code className={styles.code}>{selectionId}</code>.
                </>
              )}
            </p>

            <h3 className={styles.subheading}>Any size: the Python script</h3>
            <p className={styles.text}>
              It downloads the selection file by file with the archive&rsquo;s rules: the same files, folders and{" "}
              <code className={styles.code}>manifest.tsv</code>, each file checked against its SHA-256. There is no size
              limit, and running it again after a break finishes the rest. It needs Python 3.8 or newer and nothing
              else, on macOS or Linux. On Windows, run it in WSL (Windows Subsystem for Linux), or use the archive.
            </p>
            <pre className={styles.pre} data-example="script">
              <code>{examples.script}</code>
            </pre>
            <p className={styles.note}>
              <a href={SCRIPT_PATH} download={SCRIPT_FILE_NAME}>
                Download {SCRIPT_FILE_NAME}
              </a>
              . <code className={styles.code}>--files</code> takes{" "}
              {FILE_CHOICES.map((choice, index) => (
                <span key={choice}>
                  {index === 0 ? "" : index === FILE_CHOICES.length - 1 ? " or " : ", "}
                  <code className={styles.code}>{choice}</code>
                </span>
              ))}
              . It exits with 1 when a file Dynamic PDB holds could not be read or differs from its record; the file is
              listed in <code className={styles.code}>manifest.tsv</code>, never replaced.
            </p>

            <h3 className={styles.subheading}>One archive with curl</h3>
            <pre className={styles.pre} data-example="archive">
              <code>{examples.archive}</code>
            </pre>

            <h3 className={styles.subheading}>Saving a selection without the panel</h3>
            <p className={styles.text}>
              Up to 10,000 model IDs. The panel saves whole searches too: every result of a search, as it was when
              saved.
            </p>
            <pre className={styles.pre} data-example="save-selection">
              <code>{examples.saveSelection}</code>
            </pre>
          </section>

          <p className={styles.note}>
            For questions, contact{" "}
            <a className={styles.email} href="mailto:prism-core@astera.org">
              prism-core@astera.org
            </a>
          </p>
        </div>
      </main>

      <AppFooter />
    </>
  );
}
