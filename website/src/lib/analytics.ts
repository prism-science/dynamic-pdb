/**
 * Custom events for Umami Cloud, the cookieless analytics loaded by
 * app/components/Analytics.tsx.
 *
 * Umami records the page URL with every event, so the data here only carries
 * what the URL does not. The tracker only reports on the production host, and
 * may be blocked or still loading, so a missing `window.umami` is normal and
 * every call is a no-op then.
 */

export const umamiScriptURL = "https://cloud.umami.is/script.js";
// Public by design: it appears in every page's HTML. Empty turns tracking off.
export const umamiWebsiteID = "41e0ae39-0a08-45d0-bf1a-3a4f34bbc40c";
export const productionHost = "dynamicpdb.com";

export type AnalyticsEvent =
  | "download-click"
  | "viewer-open"
  | "search"
  | "outbound-click";

type EventData = Record<string, string | number | boolean>;

declare global {
  interface Window {
    umami?: { track: (name: string, data?: EventData) => void };
  }
}

// Events raised during hydration (a search landing on /browse, a deep link to
// the Structure tab) beat the script, which loads after the page is
// interactive. They wait here until it arrives. Bounded, since a blocked
// script never arrives.
const pending: [AnalyticsEvent, EventData | undefined][] = [];
const maxPending = 20;

export function track(name: AnalyticsEvent, data?: EventData): void {
  if (typeof window === "undefined") {
    return;
  }
  if (!window.umami) {
    if (pending.length < maxPending) {
      pending.push([name, data]);
    }
    return;
  }
  send(name, data);
}

/** Called once the Umami script has loaded. */
export function flushPending(): void {
  for (const [name, data] of pending.splice(0)) {
    send(name, data);
  }
}

function send(name: AnalyticsEvent, data?: EventData): void {
  try {
    window.umami?.track(name, data);
  } catch {
    // Analytics must never break the page.
  }
}
