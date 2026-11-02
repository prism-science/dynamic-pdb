import path from "node:path";

// The browser tests read the same e2e/stack/stack.env that `make browser-stack`
// used, so the servers Playwright starts, the database and the tests agree on
// ports, user ids and the JWT secret. A variable already set in the environment wins.
process.loadEnvFile(path.join(__dirname, "../stack/stack.env"));

function required(name: string): string {
  const value = process.env[name];
  if (!value) {
    throw new Error(`${name} is not set; it is defined in e2e/stack/stack.env`);
  }
  return value;
}

const backendPort = required("BROWSER_BACKEND_PORT");
const websitePort = required("BROWSER_WEBSITE_PORT");

export const stack = {
  backendPort,
  websitePort,
  backendURL: `http://127.0.0.1:${backendPort}`,
  websiteURL: `http://127.0.0.1:${websitePort}`,
  s3URL: `http://127.0.0.1:${required("BROWSER_S3_PORT")}`,
  s3Bucket: required("BROWSER_S3_BUCKET"),
  dbPort: required("BROWSER_DB_PORT"),
  dbName: required("BROWSER_DB_NAME"),
  dbUser: required("BROWSER_DB_USER"),
  dbPassword: required("BROWSER_DB_PASSWORD"),
  jwtSecret: required("BROWSER_JWT_SECRET"),
  jwtIssuer: required("BROWSER_JWT_ISSUER"),
  submitterId: required("BROWSER_SUBMITTER_ID"),
  reviewerId: required("BROWSER_REVIEWER_ID"),
} as const;

// Nothing the test backend calls may leave this machine: RCSB and SIFTS point at
// a closed port. Entries in these tests have no PDB ID, so nothing should call them.
const unreachable = "http://127.0.0.1:9";

/**
 * Overrides for backend/config/local.yml (DYNAMIC_PDB_* variables, read by
 * backend/internal/config/config.go). They exist only in the backend process
 * that Playwright starts.
 */
export function backendEnvironment(): Record<string, string> {
  return {
    DYNAMIC_PDB_ENV: "local",
    DYNAMIC_PDB_SERVER_ADDR: `127.0.0.1:${stack.backendPort}`,
    DYNAMIC_PDB_SERVER_PUBLIC_BASE_URL: stack.backendURL,
    // Every request comes from 127.0.0.1, so the per-IP limit would throttle the suite.
    DYNAMIC_PDB_SERVER_RATE_LIMIT_REQUESTS: "1000000",
    DYNAMIC_PDB_DB_HOST: "127.0.0.1",
    DYNAMIC_PDB_DB_PORT: stack.dbPort,
    DYNAMIC_PDB_DB_NAME: stack.dbName,
    DYNAMIC_PDB_DB_USERNAME: stack.dbUser,
    DYNAMIC_PDB_DB_PASSWORD: stack.dbPassword,
    DYNAMIC_PDB_AUTH_JWT_SECRET: stack.jwtSecret,
    DYNAMIC_PDB_AUTH_JWT_ISSUER: stack.jwtIssuer,
    // Stated here as well as in local.yml: these tests exist to prove the forms
    // save under enforce, and a reviewer must approve another user's revisions, as on production.
    DYNAMIC_PDB_VALIDATION_MODE: "enforce",
    DYNAMIC_PDB_REVISIONS_SELF_APPROVAL_ENABLED: "false",
    DYNAMIC_PDB_CDN_S3_ENDPOINT: stack.s3URL,
    DYNAMIC_PDB_CDN_S3_BUCKET: stack.s3Bucket,
    DYNAMIC_PDB_CDN_S3_REGION: "us-east-1",
    DYNAMIC_PDB_CDN_S3_ACCESS_KEY_ID: "browser-tests",
    DYNAMIC_PDB_CDN_S3_SECRET_ACCESS_KEY: "browser-tests",
    AWS_EC2_METADATA_DISABLED: "true",
    DYNAMIC_PDB_RCSB_DATA_URL: unreachable,
    DYNAMIC_PDB_RCSB_FILES_URL: unreachable,
    DYNAMIC_PDB_RCSB_WWW_URL: unreachable,
    DYNAMIC_PDB_RCSB_CDN_URL: unreachable,
    DYNAMIC_PDB_SIFTS_URL: unreachable,
  };
}

/** Runtime settings for `next start`. NEXT_PUBLIC_API_BASE_URL is baked in by `make browser-stack`. */
export function websiteEnvironment(): Record<string, string> {
  return {
    API_INTERNAL_BASE_URL: stack.backendURL,
    APP_BASE_URL: stack.websiteURL,
    NEXT_TELEMETRY_DISABLED: "1",
  };
}
