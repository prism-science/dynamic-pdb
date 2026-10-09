"use client";

import type { AnchorHTMLAttributes, ReactNode } from "react";

import { track } from "@/lib/analytics";
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
  // Every download button on the site is one of these, so this one handler
  // counts them all. The catalogue href names the file; the resolved storage
  // URL does not.
  const { onClick, download } = props;
  return (
    <a
      className={className}
      href={resolved.url}
      {...props}
      onClick={(event) => {
        if (download) {
          track("download-click", { file: href });
        }
        onClick?.(event);
      }}
    >
      {children}
    </a>
  );
}
