const defaultApiBaseUrl =
  process.env.NODE_ENV === "production"
    ? "https://dynamicpdb.com/api"
    : "http://localhost:8080";

export function getApiBaseUrl(): string {
  return process.env.NEXT_PUBLIC_API_BASE_URL ?? defaultApiBaseUrl;
}
