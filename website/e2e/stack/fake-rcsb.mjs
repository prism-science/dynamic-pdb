// A stand-in for RCSB and SIFTS in the browser tests, so the PDB lookup and the
// archive sources of a new entry can be tested without leaving this machine.
// The backend reads every RCSB and SIFTS URL from here (e2e/support/stack.ts).
//
// It serves one archive entry for any PDB ID, from e2e/fixtures/rcsb-archive.json
// with the ID filled in, and 404 for everything else: SIFTS then reports no
// release at once, and the sync's file downloads find nothing. Tests change what
// one PDB ID returns through the /__fake routes (e2e/support/fakeRcsb.ts):
//   PUT    /__fake/entries/{ID}                        fields merged over the entry
//   PUT    /__fake/entries/{ID}/polymer_entities/{N}   fields merged over entity N
//   POST   /__fake/entries/{ID}/fail?status=N           answer N for that entry
//   POST   /__fake/entries/{ID}/fail?mode=malformed     answer 200 with a body
//                                                        that is not JSON
//   DELETE /__fake/entries/{ID}/fail                    answer normally again
// Every test uses a fresh PDB ID, so no test sees another's changes.

import { readFileSync } from "node:fs";
import { createServer } from "node:http";

const port = Number(process.env.BROWSER_RCSB_PORT);
if (!Number.isInteger(port) || port <= 0) {
  throw new Error("BROWSER_RCSB_PORT is not set; it is defined in e2e/stack/stack.env");
}

const template = readFileSync(new URL("../fixtures/rcsb-archive.json", import.meta.url), "utf8");

/** PDB ID → fields merged over the entry. */
const entryFields = new Map();
/** "PDB ID/entity ID" → fields merged over that polymer entity. */
const entityFields = new Map();
/**
 * PDB ID → how every request for that entry fails: an HTTP status, or
 * MALFORMED for a 200 whose body is cut off mid-JSON.
 */
const failures = new Map();
const MALFORMED = "malformed";

function archive(pdbId) {
  return JSON.parse(template.replaceAll("__PDB_ID__", pdbId));
}

function send(response, status, body) {
  response.writeHead(status, { "Content-Type": "application/json" });
  response.end(JSON.stringify(body));
}

/** Answers a request for pdbId as set by /fail; false when it is not failing. */
function sendFailure(response, pdbId) {
  if (!failures.has(pdbId)) {
    return false;
  }
  const failure = failures.get(pdbId);
  if (failure === MALFORMED) {
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end('{"rcsb_id": "');
    return true;
  }
  send(response, failure, { message: "failed by the browser test" });
  return true;
}

function noContent(response) {
  response.writeHead(204);
  response.end();
}

async function readJSON(request) {
  const chunks = [];
  for await (const chunk of request) {
    chunks.push(chunk);
  }
  return JSON.parse(Buffer.concat(chunks).toString("utf8") || "{}");
}

const routes = [
  {
    method: "GET",
    pattern: /^\/healthz$/,
    handle: (_match, _request, response) => send(response, 200, { ok: true }),
  },
  {
    method: "PUT",
    pattern: /^\/__fake\/entries\/([0-9A-Z]{4})$/,
    handle: async ([, pdbId], request, response) => {
      entryFields.set(pdbId, await readJSON(request));
      noContent(response);
    },
  },
  {
    method: "PUT",
    pattern: /^\/__fake\/entries\/([0-9A-Z]{4})\/polymer_entities\/([0-9]+)$/,
    handle: async ([, pdbId, entityId], request, response) => {
      entityFields.set(`${pdbId}/${entityId}`, await readJSON(request));
      noContent(response);
    },
  },
  {
    method: "POST",
    pattern: /^\/__fake\/entries\/([0-9A-Z]{4})\/fail$/,
    handle: ([, pdbId], _request, response, url) => {
      const mode = url.searchParams.get("mode");
      if (mode === MALFORMED) {
        failures.set(pdbId, MALFORMED);
      } else if (mode === null) {
        failures.set(pdbId, Number(url.searchParams.get("status") ?? 403));
      } else {
        send(response, 400, { message: `unknown failure mode: ${mode}` });
        return;
      }
      noContent(response);
    },
  },
  {
    method: "DELETE",
    pattern: /^\/__fake\/entries\/([0-9A-Z]{4})\/fail$/,
    handle: ([, pdbId], _request, response) => {
      failures.delete(pdbId);
      noContent(response);
    },
  },
  {
    method: "GET",
    pattern: /^\/rest\/v1\/core\/entry\/([0-9A-Z]{4})$/,
    handle: ([, pdbId], _request, response) => {
      if (sendFailure(response, pdbId)) {
        return;
      }
      send(response, 200, { ...archive(pdbId).entry, ...(entryFields.get(pdbId) ?? {}) });
    },
  },
  {
    method: "GET",
    pattern: /^\/rest\/v1\/core\/polymer_entity\/([0-9A-Z]{4})\/([0-9]+)$/,
    handle: ([, pdbId, entityId], _request, response) => {
      if (sendFailure(response, pdbId)) {
        return;
      }
      const entity = archive(pdbId).polymer_entities[entityId];
      if (entity) {
        send(response, 200, { ...entity, ...(entityFields.get(`${pdbId}/${entityId}`) ?? {}) });
      } else {
        send(response, 404, { message: `no polymer entity ${entityId}` });
      }
    },
  },
];

const server = createServer((request, response) => {
  const url = new URL(request.url ?? "/", `http://${request.headers.host}`);
  console.log(`${request.method} ${url.pathname}${url.search}`);
  for (const route of routes) {
    const match = route.method === request.method ? route.pattern.exec(url.pathname) : null;
    if (match) {
      Promise.resolve(route.handle(match, request, response, url)).catch((error) => {
        console.error(error);
        send(response, 500, { message: String(error) });
      });
      return;
    }
  }
  send(response, 404, { message: "not served by the fake RCSB" });
});

server.listen(port, "127.0.0.1", () => {
  console.log(`fake RCSB listening on http://127.0.0.1:${port}`);
});
