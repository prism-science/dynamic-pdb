import ScopePage from "../../scope-page";

export const dynamic = "force-dynamic";

// The same page as the entry, with the model named in the URL instead of
// chosen for the reader.
export default async function ModelPage({
  params,
  searchParams,
}: {
  params: Promise<{ entryId: string; modelId: string }>;
  searchParams: Promise<{ tab?: string; sort?: string; dir?: string }>;
}) {
  const { entryId, modelId } = await params;
  const { tab, sort, dir } = await searchParams;
  return (
    <ScopePage
      entryId={entryId}
      modelId={modelId}
      requestedTab={tab}
      requestedSort={sort}
      requestedDirection={dir}
    />
  );
}
