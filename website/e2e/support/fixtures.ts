import { randomUUID } from "node:crypto";

import { test as base } from "@playwright/test";

import { BackendClient } from "./api";
import { reviewer, submitter } from "./auth";

type Fixtures = {
  /** The backend as the submitter, for setup and for checking what a form saved. */
  submitterApi: BackendClient;
  /** The backend as the reviewer, who approves the submitter's revisions. */
  reviewerApi: BackendClient;
  /** A short id unique to this test run, for titles and file contents. */
  runTag: string;
  /** Keeps the browser on this machine; see below. */
  localNetworkOnly: void;
};

const localHosts = new Set(["127.0.0.1", "localhost"]);

export const test = base.extend<Fixtures>({
  submitterApi: async ({}, use) => {
    const client = await BackendClient.signedInAs(submitter);
    await use(client);
    await client.dispose();
  },

  reviewerApi: async ({}, use) => {
    const client = await BackendClient.signedInAs(reviewer);
    await use(client);
    await client.dispose();
  },

  runTag: async ({}, use) => {
    await use(randomUUID().slice(0, 8));
  },

  // The browser may only reach the local stack, so a test can never write to the
  // dev or production site. A blocked request is aborted and listed on the test.
  localNetworkOnly: [
    async ({ context }, use, testInfo) => {
      const blocked: string[] = [];
      await context.route(
        (url) => !localHosts.has(url.hostname),
        async (route) => {
          blocked.push(route.request().url());
          await route.abort("blockedbyclient");
        },
      );
      await use();
      for (const url of blocked) {
        testInfo.annotations.push({ type: "blocked request", description: url });
      }
    },
    { auto: true },
  ],
});

export { expect } from "@playwright/test";
