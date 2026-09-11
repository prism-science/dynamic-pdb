import ScopePage from "./scope-page";

export const dynamic = "force-dynamic";

// The entry with no model named: the page picks the one to start on and renders
// exactly what /models/{id} renders.
export default async function EntryPage({
  params,
  searchParams,
}: {
  params: Promise<{ entryId: string }>;
  searchParams: Promise<{ tab?: string; sort?: string; dir?: string }>;
}) {
  const { entryId } = await params;
  const { tab, sort, dir } = await searchParams;
  return (
    <ScopePage
      entryId={entryId}
      modelId={null}
      requestedTab={tab}
      requestedSort={sort}
      requestedDirection={dir}
    />
  );
}
