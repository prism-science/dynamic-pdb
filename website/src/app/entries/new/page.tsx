import { redirect } from "next/navigation";

import { getAuthSession } from "@/lib/auth/session";
import Breadcrumbs from "@/app/components/Breadcrumbs";
import NewEntryForm from "@/app/components/NewEntryForm";

import styles from "./new-entry.module.css";

export const dynamic = "force-dynamic";

export default async function NewEntryPage() {
  const session = await getAuthSession();
  if (!session) {
    redirect("/auth/github/login?return_to=/entries/new");
  }

  return (
    <main className={styles.page}>
      <div className={styles.shell}>
        <Breadcrumbs
          items={[{ label: "Proteins", href: "/" }, { label: "New entry" }]}
        />
        <h1 className={styles.title}>New entry</h1>
        <NewEntryForm />
      </div>
    </main>
  );
}
