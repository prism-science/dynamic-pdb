import { mkdirSync } from "node:fs";
import path from "node:path";

import { defineConfig, devices } from "@playwright/test";

import { backendEnvironment, stack, websiteEnvironment } from "./e2e/support/stack";

// The servers' logs go to files rather than into the test output. CI uploads them when a test fails.
const logDirectory = path.join(__dirname, "browser-test-logs");
mkdirSync(logDirectory, { recursive: true });
const logFile = (name: string) => `'${path.join(logDirectory, name)}'`;

// Browser tests for the deposition forms, run against the real stack that
// `make browser-stack` prepares (see the "Browser tests" section of README.md).
export default defineConfig({
  testDir: "./e2e",
  globalSetup: "./e2e/global-setup.ts",
  // The tests share one backend and one database; one at a time keeps them simple to read in a trace.
  workers: 1,
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  // A flaky test must fail the job rather than pass on a second try.
  retries: 0,
  timeout: 60_000,
  expect: { timeout: 15_000 },
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    baseURL: stack.websiteURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: [
    {
      // Stands in for RCSB and SIFTS; the backend below reads the archive from it.
      name: "fake-rcsb",
      command: `exec node e2e/stack/fake-rcsb.mjs > ${logFile("fake-rcsb.log")} 2>&1`,
      env: { BROWSER_RCSB_PORT: stack.rcsbPort },
      url: `${stack.rcsbURL}/healthz`,
      reuseExistingServer: !process.env.CI,
      timeout: 30_000,
    },
    {
      name: "backend",
      command: `exec ./bin/server > ${logFile("backend.log")} 2>&1`,
      cwd: path.join(__dirname, "../backend"),
      env: backendEnvironment(),
      url: `${stack.backendURL}/readyz`,
      reuseExistingServer: !process.env.CI,
      timeout: 60_000,
    },
    {
      name: "website",
      command: `exec npx next start -H 127.0.0.1 -p ${stack.websitePort} > ${logFile("website.log")} 2>&1`,
      env: websiteEnvironment(),
      url: stack.websiteURL,
      reuseExistingServer: !process.env.CI,
      timeout: 60_000,
    },
  ],
});
