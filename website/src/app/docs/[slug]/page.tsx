import type { Metadata } from "next";
import { notFound } from "next/navigation";

import AppFooter from "../../components/AppFooter";

import ApiReference from "../ApiReference";
import DocsBar from "../DocsBar";
import DocsNav from "../DocsNav";
import Markdown from "../Markdown";
import OnThisPage, { OnThisPageMenu } from "../OnThisPage";
import PrevNext from "../PrevNext";
import { markdownFor } from "../content";
import { DOCS_PAGES, docsPageFor, extractHeadings } from "../pages";

import styles from "../docs.module.css";

type Props = { params: Promise<{ slug: string }> };

export const dynamicParams = false;

export function generateStaticParams() {
  return DOCS_PAGES.map((page) => ({ slug: page.slug }));
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const page = docsPageFor((await params).slug);
  return page ? { title: `${page.title} · Dynamic PDB`, description: page.description } : {};
}

export default async function DocsPage({ params }: Props) {
  const page = docsPageFor((await params).slug);
  if (!page) notFound();

  if (page.kind === "api") {
    return (
      <>
        <main className={styles.page} aria-label="Docs">
          <DocsBar page={page} menuAlways />
          <h1 className={styles.srOnly}>{page.title}</h1>
          <ApiReference />
          <div className={styles.apiPrevNext}>
            <PrevNext slug={page.slug} />
          </div>
        </main>
        <AppFooter />
      </>
    );
  }

  const markdown = markdownFor(page.slug) ?? "";
  const sections = extractHeadings(markdown);

  return (
    <>
      <main className={styles.page} aria-label="Docs">
        <DocsBar page={page} />
        <div className={styles.shell}>
          <aside className={styles.rail}>
            <nav className={styles.railInner} aria-label="Docs pages">
              <DocsNav current={page.slug} />
            </nav>
          </aside>

          <article className={styles.body}>
            <div className={styles.prose}>
              <p className={styles.eyebrow}>{page.group}</p>
              <h1 className={styles.title}>{page.title}</h1>
              <OnThisPageMenu sections={sections} />
              <Markdown source={markdown} />
              <PrevNext slug={page.slug} />
            </div>
          </article>

          <OnThisPage sections={sections} />
        </div>
      </main>
      <AppFooter />
    </>
  );
}
