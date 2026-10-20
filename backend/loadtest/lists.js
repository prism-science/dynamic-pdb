// Fires concurrent list requests at increasing offsets and checks them against a p95 budget.
// Each endpoint/offset/concurrency combination runs as its own scenario, one after another.
// The offset "cursor" makes each VU walk CURSOR_PAGES pages deep by meta.next_cursor instead.
import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'http://host.docker.internal:8080';
const ENDPOINTS = listFromEnv('ENDPOINTS', 'entries,models');
const OFFSETS = listFromEnv('OFFSETS', '0,1000,5000,cursor');
const CURSOR_OFFSET = 'cursor';
const CURSOR_PAGES = Number(__ENV.CURSOR_PAGES || 50);
const CONCURRENCY = listFromEnv('CONCURRENCY', '8,16,32').map(Number);
const PAGE_LIMIT = Number(__ENV.PAGE_LIMIT || 100);
const DURATION_SECONDS = Number(__ENV.DURATION_SECONDS || 15);
const GAP_SECONDS = 10;
const P95_BUDGET_MS = Number(__ENV.P95_BUDGET_MS || 1000);
const REQUEST_TIMEOUT = __ENV.REQUEST_TIMEOUT || '30s';

const ENDPOINT_PATHS = {
  entries: '/v1/entries',
  models: '/v1/models',
};

const scenarioNames = [];
const scenarios = {};
const thresholds = {};
let startSeconds = 0;
for (const endpoint of ENDPOINTS) {
  if (!(endpoint in ENDPOINT_PATHS)) {
    throw new Error(`unknown endpoint: ${endpoint}`);
  }
  for (const offset of OFFSETS) {
    if (offset !== CURSOR_OFFSET && !Number.isInteger(Number(offset))) {
      throw new Error(`unknown offset: ${offset}`);
    }
    for (const vus of CONCURRENCY) {
      const name = `${endpoint}_offset${offset}_vus${vus}`;
      scenarioNames.push(name);
      scenarios[name] = {
        executor: 'constant-vus',
        exec: 'listPage',
        vus,
        duration: `${DURATION_SECONDS}s`,
        startTime: `${startSeconds}s`,
        gracefulStop: `${GAP_SECONDS}s`,
        env: { ENDPOINT: endpoint, OFFSET: offset },
      };
      thresholds[`http_req_duration{scenario:${name}}`] = [`p(95)<${P95_BUDGET_MS}`];
      thresholds[`http_req_failed{scenario:${name}}`] = ['rate==0'];
      thresholds[`http_reqs{scenario:${name}}`] = ['count>0'];
      startSeconds += DURATION_SECONDS + GAP_SECONDS;
    }
  }
}

export const options = {
  scenarios,
  thresholds,
  summaryTrendStats: ['med', 'p(95)', 'max'],
};

// Per-VU walk state for the cursor scenarios. k6 reuses VUs across scenarios, so the
// walk records its endpoint and restarts instead of sending one list's cursor to another.
let walk = { endpoint: null, cursor: null, pages: 0 };

export function listPage() {
  const firstPageURL = `${BASE_URL}${ENDPOINT_PATHS[__ENV.ENDPOINT]}?limit=${PAGE_LIMIT}`;
  if (__ENV.OFFSET !== CURSOR_OFFSET) {
    getPage(`${firstPageURL}&offset=${__ENV.OFFSET}`);
    return;
  }
  if (walk.endpoint !== __ENV.ENDPOINT || walk.cursor === null || walk.pages >= CURSOR_PAGES) {
    walk = { endpoint: __ENV.ENDPOINT, cursor: null, pages: 0 };
  }
  const url = walk.cursor === null ? firstPageURL : `${firstPageURL}&cursor=${encodeURIComponent(walk.cursor)}`;
  const response = getPage(url);
  walk.pages += 1;
  walk.cursor = response.status === 200 ? response.json('meta.next_cursor') || null : null;
}

function getPage(url) {
  const response = http.get(url, {
    headers: { Accept: 'application/vnd.api+json' },
    timeout: REQUEST_TIMEOUT,
  });
  check(response, { 'status is 200': (r) => r.status === 200 });
  return response;
}

export function handleSummary(data) {
  const metricValues = (metric, name) => (data.metrics[`${metric}{scenario:${name}}`] || { values: {} }).values;
  const rows = scenarioNames.map((name) => {
    const duration = metricValues('http_req_duration', name);
    const requests = metricValues('http_reqs', name);
    const failed = metricValues('http_req_failed', name);
    return {
      scenario: name,
      requests: requests.count || 0,
      rps: (requests.count || 0) / DURATION_SECONDS,
      p50_ms: duration.med || 0,
      p95_ms: duration['p(95)'] || 0,
      max_ms: duration.max || 0,
      failed_rate: failed.rate || 0,
    };
  });
  const outputs = { stdout: summaryTable(rows) };
  if (__ENV.SUMMARY_JSON) {
    outputs[__ENV.SUMMARY_JSON] = JSON.stringify(rows, null, 2);
  }
  return outputs;
}

function summaryTable(rows) {
  const header = ['scenario', 'requests', 'req/s', 'p50 ms', 'p95 ms', 'max ms', 'failed', 'budget'];
  const lines = rows.map((row) => [
    row.scenario,
    String(row.requests),
    row.rps.toFixed(1),
    row.p50_ms.toFixed(0),
    row.p95_ms.toFixed(0),
    row.max_ms.toFixed(0),
    `${(row.failed_rate * 100).toFixed(1)}%`,
    row.p95_ms < P95_BUDGET_MS && row.failed_rate === 0 ? 'ok' : 'FAIL',
  ]);
  const widths = header.map((title, column) => Math.max(title.length, ...lines.map((line) => line[column].length)));
  const format = (cells) => cells.map((cell, column) => cell.padEnd(widths[column])).join('  ');
  return `\np95 budget: ${P95_BUDGET_MS} ms, page limit: ${PAGE_LIMIT}, cursor walk: ${CURSOR_PAGES} pages\n\n${[format(header), ...lines.map(format)].join('\n')}\n`;
}

function listFromEnv(name, fallback) {
  return (__ENV[name] || fallback).split(',').map((value) => value.trim()).filter(Boolean);
}
