import { redirect } from "next/navigation";

import { getAuthSession } from "@/lib/auth/session";
import Breadcrumbs from "@/app/components/Breadcrumbs";
import NewStructureForm from "@/app/components/NewStructureForm";

import styles from "./new-structure.module.css";

export const dynamic = "force-dynamic";

type NewStructurePageProps = {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
};

export default async function NewStructurePage({ searchParams }: NewStructurePageProps) {
  const params = await searchParams;
  const rawExperimentID = params.ext_experiment_id;
  const experimentID =
    typeof rawExperimentID === "string" ? rawExperimentID.trim() : "";
  const returnToParams = new URLSearchParams();
  if (experimentID) {
    returnToParams.set("ext_experiment_id", experimentID);
  }
  const returnTo = `/structures/new${returnToParams.size > 0 ? `?${returnToParams}` : ""}`;

  const session = await getAuthSession();
  if (!session) {
    redirect(`/auth/github/login?return_to=${encodeURIComponent(returnTo)}`);
  }

  return (
    <main className={styles.page}>
      <div className={styles.shell}>
        <Breadcrumbs
          items={[{ label: "Proteins", href: "/" }, { label: "New structure" }]}
        />
        <h1 className={styles.title}>New structure</h1>
        <NewStructureForm extExperimentId={experimentID || null} />
      </div>
    </main>
  );
}
