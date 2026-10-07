import { redirect } from "next/navigation";

// Archived while external submissions are closed. A temporary redirect, so
// the page can come back at the same address.
export default function Archived() {
  redirect("/docs");
}
