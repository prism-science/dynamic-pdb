import { notFound, redirect } from "next/navigation";

import {
  ApiRequestError,
  getStructurePageData,
  type Entity,
} from "@/lib/api/structures";
import { getAuthSession } from "@/lib/auth/session";
import Breadcrumbs from "@/app/components/Breadcrumbs";
import NewModelForm from "@/app/components/NewModelForm";

import styles from "@/app/structures/new/new-structure.module.css";

export const dynamic = "force-dynamic";

type NewModelPageProps = {
  params: Promise<{ structureId: string }>;
};

export default async function NewModelPage({ params }: NewModelPageProps) {
  const { structureId } = await params;
  const returnTo = `/structures/${encodeURIComponent(structureId)}/models/new`;

  const session = await getAuthSession();
  if (!session) {
    redirect(`/auth/github/login?return_to=${encodeURIComponent(returnTo)}`);
  }

  let data;
  try {
    data = await getStructurePageData(session.token, structureId);
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 404) {
      notFound();
    }
    throw error;
  }

  const structureHref = `/structures/${encodeURIComponent(structureId)}`;

  return (
    <main className={styles.page}>
      <div className={styles.shell}>
        <Breadcrumbs
          items={[
            { label: "Structures", href: "/" },
            { label: data.structure.name, href: structureHref },
            { label: "New model" },
          ]}
        />
        <h1 className={styles.title}>New model</h1>
        <NewModelForm
          structure={data.structure}
          structureDataEntities={structureDataEntities(data.entities)}
          modelCount={data.models.length}
        />
      </div>
    </main>
  );
}

// Files already deposited on the structure are the existing entities a new model may
// point at: they can be inputs to the program that produced it. Structure files
// count too — a .pdb/.cif is stored as a "model" entity, and a structure-level one
// is often the starting geometry.
function structureDataEntities(entities: Entity[]): Entity[] {
  return entities.filter(
    (entity) =>
      entity.model_id === null &&
      (entity.type === "data" || entity.type === "model"),
  );
}
