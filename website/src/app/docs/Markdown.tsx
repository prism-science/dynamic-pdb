import Link from "next/link";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";

import LineageExamples from "./LineageExamples";
import TourButton from "./TourButton";
import { slugify } from "./pages";

import styles from "./docs.module.css";

type HastNode = {
  type: string;
  value?: string;
  tagName?: string;
  properties?: { className?: unknown };
  children?: HastNode[];
};

function textOf(node: HastNode | undefined): string {
  if (!node) return "";
  if (node.type === "text") return node.value ?? "";
  return (node.children ?? []).map(textOf).join("");
}

function languageOf(node: HastNode | undefined): string | null {
  const code = node?.children?.find((child) => child.tagName === "code");
  const classes = code?.properties?.className;
  const language = Array.isArray(classes)
    ? classes.find((name) => String(name).startsWith("language-"))
    : undefined;
  return language ? String(language).slice("language-".length) : null;
}

const COMPONENTS: Components = {
  h2: ({ node, children }) => (
    <h2 id={slugify(textOf(node as HastNode))} className={styles.heading}>
      {children}
    </h2>
  ),
  h3: ({ node, children }) => (
    <h3 id={slugify(textOf(node as HastNode))} className={styles.subheading}>
      {children}
    </h3>
  ),
  p: ({ children }) => <p className={styles.bodyText}>{children}</p>,
  ul: ({ children }) => <ul className={styles.list}>{children}</ul>,
  ol: ({ children }) => <ol className={styles.list}>{children}</ol>,
  a: ({ href = "", children }) => {
    if (href.startsWith("/")) {
      return (
        <Link href={href} className={styles.link}>
          {children}
        </Link>
      );
    }
    const external = /^https?:/.test(href);
    return (
      <a
        href={href}
        className={styles.link}
        target={external ? "_blank" : undefined}
        rel={external ? "noreferrer" : undefined}
      >
        {children}
      </a>
    );
  },
  code: ({ children }) => <code className={styles.code}>{children}</code>,
  pre: ({ node }) => {
    const language = languageOf(node as HastNode);
    if (language === "lineage-examples") return <LineageExamples />;
    if (language === "tour-button") return <TourButton />;
    return (
      <pre className={styles.pre}>
        <code>{textOf(node as HastNode).replace(/\n$/, "")}</code>
      </pre>
    );
  },
  table: ({ children }) => (
    <div className={styles.tableWrap}>
      <table className={styles.table}>{children}</table>
    </div>
  ),
  blockquote: ({ children }) => (
    <aside className={styles.callout}>
      <p className={styles.calloutLabel}>Note</p>
      {children}
    </aside>
  ),
};

export default function Markdown({ source }: { source: string }) {
  return (
    <ReactMarkdown remarkPlugins={[remarkGfm]} components={COMPONENTS}>
      {source}
    </ReactMarkdown>
  );
}
