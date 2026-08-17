import { redirect } from "next/navigation";

// Reviews used to be addressed by an opaque "entry:<uuid>" / "model:<uuid>" id.
// The queue is keyed on the entry now, so send old links to the inbox.
export default async function LegacyReviewPage({
  params,
}: {
  params: Promise<{ reviewId: string }>;
}) {
  const { reviewId } = await params;
  const [, rawEntryId] = decodeURIComponent(reviewId).split(":");
  redirect(rawEntryId ? `/review?sel=${encodeURIComponent(rawEntryId)}` : "/review");
}
