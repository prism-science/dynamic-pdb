import { redirect } from "next/navigation";

import { getAuthSession } from "@/lib/auth/session";
import NewEntryForm from "@/app/components/NewEntryForm";

import styles from "./new-entry.module.css";

export const dynamic = "force-dynamic";

type NewEntryPageProps = {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
};

export default async function NewEntryPage({ searchParams }: NewEntryPageProps) {
  const params = await searchParams;
  const rawExperimentID = params.ext_experiment_id;
  const experimentID =
    typeof rawExperimentID === "string" ? rawExperimentID.trim() : "";
  const returnToParams = new URLSearchParams();
  if (experimentID) {
    returnToParams.set("ext_experiment_id", experimentID);
  }
  const returnTo = `/entries/new${returnToParams.size > 0 ? `?${returnToParams}` : ""}`;

  const session = await getAuthSession();
  if (!session) {
    redirect(`/auth/github/login?return_to=${encodeURIComponent(returnTo)}`);
  }

  return (
    <main className={styles.page}>
      <div className={styles.shell}>
        <h1 className={styles.title}>New entry</h1>
        <NewEntryForm extExperimentId={experimentID || null} />
      </div>
    </main>
  );
}
