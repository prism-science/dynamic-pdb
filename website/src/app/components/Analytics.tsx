"use client";

import Script from "next/script";
import { useEffect } from "react";

import {
  flushPending,
  productionHost,
  track,
  umamiScriptURL,
  umamiWebsiteID,
} from "@/lib/analytics";

/**
 * Page views and outbound link clicks, via Umami Cloud.
 *
 * Cookieless, so there is no consent banner. `data-domains` keeps staging,
 * localhost, and ngrok out of the numbers: the script loads there but sends
 * nothing.
 *
 * Events raised before the script arrives are queued in lib/analytics and sent
 * from onLoad.
 *
 * Outbound clicks are caught once here, at the document, rather than on every
 * external link. File links are skipped: they point at the files host too, but
 * ResolvedFileLink already reports them as downloads.
 */
export default function Analytics() {
  useEffect(() => {
    function onClick(event: MouseEvent) {
      const anchor = (event.target as Element | null)?.closest?.("a[href]");
      if (!(anchor instanceof HTMLAnchorElement) || anchor.hasAttribute("download")) {
        return;
      }
      if (anchor.protocol === "mailto:") {
        track("outbound-click", { url: anchor.href });
        return;
      }
      if (!anchor.protocol.startsWith("http") || isOwnHost(anchor.hostname)) {
        return;
      }
      track("outbound-click", { url: anchor.href });
    }
    document.addEventListener("click", onClick, true);
    document.addEventListener("auxclick", onClick, true);
    return () => {
      document.removeEventListener("click", onClick, true);
      document.removeEventListener("auxclick", onClick, true);
    };
  }, []);

  if (!umamiWebsiteID) {
    return null;
  }
  return (
    <Script
      src={umamiScriptURL}
      strategy="afterInteractive"
      data-website-id={umamiWebsiteID}
      data-domains={productionHost}
      onLoad={flushPending}
    />
  );
}

function isOwnHost(hostname: string): boolean {
  return (
    hostname === window.location.hostname ||
    hostname === productionHost ||
    hostname.endsWith(`.${productionHost}`)
  );
}
