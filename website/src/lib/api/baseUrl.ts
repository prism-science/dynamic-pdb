const defaultApiBaseUrl =
  process.env.NODE_ENV === "production"
    ? "https://dynamicpdb.com/api"
    : "http://localhost:8080";

export const jsonApiMediaType = "application/vnd.api+json";

export function getApiBaseUrl(): string {
  return process.env.NEXT_PUBLIC_API_BASE_URL ?? defaultApiBaseUrl;
}

/** Base URL for requests the website server sends to the backend. In the cluster it is the
 *  backend Service, so these requests skip the load balancer and the backend counts them
 *  against the website pod's own rate-limit bucket. Never hand it to the browser; use
 *  getApiBaseUrl for that. */
export function getServerApiBaseUrl(): string {
  return process.env.API_INTERNAL_BASE_URL || getApiBaseUrl();
}
