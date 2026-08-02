import Link from "next/link";

import type { Structure } from "@/lib/api/structures";
import { deleteStructureAction } from "@/app/actions/delete";

import DeleteButton from "./DeleteButton";
import StructuresSearchForm from "./StructuresSearchForm";
import styles from "./StructuresBrowser.module.css";

const dateFormatter = new Intl.DateTimeFormat("en-US", {
  year: "numeric",
  month: "short",
  day: "numeric",
});

function formatDate(value: string): string | null {
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? null : dateFormatter.format(parsed);
}

export default function StructuresBrowser({
  structures,
  canCreate = false,
  query = "",
  currentUserId = null,
}: {
  structures: Structure[];
  canCreate?: boolean;
  query?: string;
  currentUserId?: string | null;
}) {
  return (
    <div className={styles.wrap}>
      <header className={styles.head}>
        <h1 className={styles.title}>Proteins</h1>
        {canCreate ? (
          <Link className={styles.addButton} href="/structures/new">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
              <path d="M12 5v14M5 12h14" />
            </svg>
            New structure
          </Link>
        ) : null}
      </header>

      <StructuresSearchForm query={query} />

      {structures.length > 0 ? (
        <ul className={styles.grid}>
          {structures.map((structure) => {
            const updated = formatDate(structure.updated_at);
            const canDelete =
              currentUserId != null && structure.created_by === currentUserId;
            return (
              <li
                key={structure.id}
                className={styles.cardItem}
                data-has-delete={canDelete ? "true" : undefined}
              >
                <Link className={styles.card} href={`/structures/${structure.id}`}>
                  <span
                    className={styles.thumb}
                    data-empty={structure.thumbnail_image_url ? undefined : "true"}
                  >
                    {structure.thumbnail_image_url ? (
                      // eslint-disable-next-line @next/next/no-img-element
                      <img src={structure.thumbnail_image_url} alt="" loading="lazy" />
                    ) : (
                      <MoleculeIcon />
                    )}
                  </span>
                  <span className={styles.cardBody}>
                    <span className={styles.cardName}>{structure.name}</span>
                    <span className={styles.cardDesc}>
                      {structure.description?.trim()
                        ? structure.description
                        : "No description yet."}
                    </span>
                    {updated ? (
                      <span className={styles.cardMeta}>
                        <span className={styles.cardDate}>Updated {updated}</span>
                      </span>
                    ) : null}
                  </span>
                </Link>
                {canDelete ? (
                  <div className={styles.cardDelete}>
                    <DeleteButton
                      action={deleteStructureAction.bind(null, structure.id)}
                      itemName={structure.name}
                      itemKind="structure"
                    />
                  </div>
                ) : null}
              </li>
            );
          })}
        </ul>
      ) : (
        <p className={styles.empty}>
          {query ? `No structures match "${query}".` : "No structures found."}
        </p>
      )}
    </div>
  );
}

function MoleculeIcon() {
  return (
    <svg width="38" height="38" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="2.4" fill="currentColor" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" transform="rotate(60 12 12)" />
      <ellipse cx="12" cy="12" rx="10" ry="4.4" stroke="currentColor" strokeWidth="1.3" transform="rotate(120 12 12)" />
    </svg>
  );
}
