"use client";

import { useEffect, useState } from "react";

import { sectionIds, type DocsSection, type DocsTab } from "./tabs";

import styles from "./docs.module.css";

export default function DocsRail({ current }: { current: DocsTab }) {
  const active = useActiveSection(sectionIds(current.sections));

  return (
    <aside className={styles.rail}>
      <nav className={styles.railInner} aria-label={`${current.label} sections`}>
        {current.sections.map((section: DocsSection) => (
          <Item key={section.id} section={section} active={active} />
        ))}
      </nav>
    </aside>
  );
}

function Item({
  section,
  active,
}: {
  section: DocsSection;
  active: string | null;
}) {
  return (
    <div className={styles.item}>
      <a
        href={`#${section.id}`}
        aria-current={section.id === active ? "true" : undefined}
        className={`${styles.navLink} ${
          section.id === active ? styles.navLinkOn : ""
        }`}
      >
        {section.label}
      </a>

      {section.children?.length ? (
        <div className={styles.children}>
          {section.children.map((child) => (
            <a
              key={child.id}
              href={`#${child.id}`}
              aria-current={child.id === active ? "true" : undefined}
              className={`${styles.navLink} ${styles.navChild} ${
                child.id === active ? styles.navLinkOn : ""
              }`}
            >
              {child.label}
            </a>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function readingLine(): number {
  const bar = document.querySelector("[data-docs-bar]");
  const bottom = bar ? bar.getBoundingClientRect().bottom : 56;
  return Math.round(bottom) + 24;
}

function useActiveSection(ids: string[]): string | null {
  const [active, setActive] = useState<string | null>(ids[0] ?? null);
  const key = ids.join("|");

  useEffect(() => {
    const sections = key
      .split("|")
      .map((id) => document.getElementById(id))
      .filter((el): el is HTMLElement => el !== null);
    if (sections.length === 0) return;

    const pick = () => {
      const atBottom =
        window.innerHeight + window.scrollY >=
        document.documentElement.scrollHeight - 2;
      if (atBottom) {
        const started = sections.filter(
          (section) => section.getBoundingClientRect().top < window.innerHeight,
        );
        setActive((started[started.length - 1] ?? sections[0]).id);
        return;
      }

      const line = readingLine();
      let current = sections[0];
      for (const section of sections) {
        if (section.getBoundingClientRect().top - line <= 0) current = section;
      }
      setActive(current.id);
    };

    pick();
    const observer = new IntersectionObserver(pick, {
      rootMargin: `-${readingLine()}px 0px -60% 0px`,
      threshold: [0, 1],
    });
    sections.forEach((section) => observer.observe(section));
    window.addEventListener("scroll", pick, { passive: true });
    window.addEventListener("resize", pick);

    return () => {
      observer.disconnect();
      window.removeEventListener("scroll", pick);
      window.removeEventListener("resize", pick);
    };
  }, [key]);

  return active;
}
