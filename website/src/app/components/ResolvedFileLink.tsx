"use client";

import type { AnchorHTMLAttributes, ReactNode } from "react";

import { useResolvedFileURL } from "@/lib/api/useResolvedFileURL";

type ResolvedFileLinkProps = Omit<
  AnchorHTMLAttributes<HTMLAnchorElement>,
  "href"
> & {
  href: string;
  children: ReactNode;
};

export default function ResolvedFileLink({
  href,
  children,
  className,
  ...props
}: ResolvedFileLinkProps) {
  const resolved = useResolvedFileURL(href);
  if (!resolved.url) {
    return (
      <span
        className={className}
        aria-busy={resolved.loading || undefined}
        title={resolved.error ?? undefined}
      >
        {children}
      </span>
    );
  }
  return (
    <a className={className} href={resolved.url} {...props}>
      {children}
    </a>
  );
}
