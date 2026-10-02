import { permanentRedirect } from "next/navigation";

export default function About() {
  permanentRedirect("/docs/about");
}
