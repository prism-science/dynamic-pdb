import { redirect } from "next/navigation";

// The review queue lives at /review. This route is kept only so old links land
// somewhere sensible.
export default function ReviewsIndex() {
  redirect("/review");
}
