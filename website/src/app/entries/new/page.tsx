import { redirect } from "next/navigation";

import { getAuthSession } from "@/lib/auth/session";
import NewEntryForm from "@/app/components/NewEntryForm";

import styles from "./new-entry.module.css";

export const dynamic = "force-dynamic";

export default async function NewEntryPage() {
  const returnTo = "/entries/new";

  const session = await getAuthSession();
  if (!session) {
    redirect(`/auth/github/login?return_to=${encodeURIComponent(returnTo)}`);
  }

  return (
    <main className={styles.page}>
      <div className={styles.shell}>
        <h1 className={styles.title}>New entry</h1>
        <NewEntryForm />
      </div>
    </main>
  );
}
