"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

import { legacyHashTarget } from "./pages";

export default function LegacyHashRedirect() {
  const router = useRouter();
  useEffect(() => {
    const target = legacyHashTarget(window.location.hash);
    if (target) router.replace(target);
  }, [router]);
  return null;
}
