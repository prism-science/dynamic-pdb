require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const { obsoleteNotice } = require("../src/lib/archive-status.ts");

test("should say nothing when the PDB still serves the entry", () => {
  // when
  const notice = obsoleteNotice(undefined);

  // then
  assert.equal(notice, null);
});

test("should link a successor to its registry entry when the registry has one", () => {
  // given
  const status = {
    state: "obsolete",
    obsoleted_at: "2024-01-17T00:00:00Z",
    superseded_by: [{ pdb_id: "8R76", entry_id: "dpdb_abcdefgh" }],
  };

  // when
  const notice = obsoleteNotice(status);

  // then
  assert.deepEqual(notice, {
    date: "Jan 17, 2024",
    successors: [{ pdbId: "8R76", href: "/entries/dpdb_abcdefgh", external: false }],
  });
});

test("should link a successor to RCSB when the registry has no entry for it", () => {
  // given
  const status = { state: "obsolete", superseded_by: [{ pdb_id: "8R78" }] };

  // when
  const notice = obsoleteNotice(status);

  // then
  assert.deepEqual(notice, {
    date: null,
    successors: [
      { pdbId: "8R78", href: "https://www.rcsb.org/structure/8R78", external: true },
    ],
  });
});

test("should keep the date in UTC when the obsolete date is at midnight", () => {
  // given
  const status = {
    state: "obsolete",
    obsoleted_at: "2018-03-07T00:00:00Z",
    superseded_by: [],
  };

  // when
  const notice = obsoleteNotice(status);

  // then
  assert.equal(notice.date, "Mar 7, 2018");
  assert.deepEqual(notice.successors, []);
});
