require("./register.cjs");

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");

const {
  DOCS_PAGES,
  extractHeadings,
  legacyHashTarget,
  neighbors,
  sectionIds,
  slugify,
} = require("../src/app/docs/pages.ts");
const { LINEAGE_EXAMPLES } = require("../src/app/docs/lineage-examples.ts");

const contentRoot = path.join(__dirname, "..", "content", "docs");
const markdownPages = DOCS_PAGES.filter((page) => page.kind === "markdown");

test("should link the first page forward only and the last page back only", () => {
  // when
  const first = neighbors(DOCS_PAGES[0].slug);
  const last = neighbors(DOCS_PAGES[DOCS_PAGES.length - 1].slug);

  // then
  assert.equal(first.previous, null);
  assert.equal(first.next.slug, DOCS_PAGES[1].slug);
  assert.equal(last.previous.slug, DOCS_PAGES[DOCS_PAGES.length - 2].slug);
  assert.equal(last.next, null);
});

test("should return no neighbors when the slug is unknown", () => {
  assert.deepEqual(neighbors("missing"), { previous: null, next: null });
});

test("should nest h3 headings under h2 and ignore headings inside code fences", () => {
  // given
  const markdown = [
    "## Use the **API**",
    "### 1. Find an entry",
    "```bash",
    "## not a heading",
    "```",
    "## [Linked](/docs) heading",
  ].join("\n");

  // when
  const sections = extractHeadings(markdown);

  // then
  assert.deepEqual(sections, [
    {
      id: "use-the-api",
      label: "Use the API",
      children: [{ id: "1-find-an-entry", label: "1. Find an entry" }],
    },
    { id: "linked-heading", label: "Linked heading" },
  ]);
});

test("should slugify punctuation and parentheses away", () => {
  assert.equal(
    slugify("Interpreting comparison metrics and evaluations (L3)"),
    "interpreting-comparison-metrics-and-evaluations-l3",
  );
});

test("should have a content file with unique heading ids for every markdown page", () => {
  for (const page of markdownPages) {
    // given
    const markdown = fs.readFileSync(path.join(contentRoot, `${page.slug}.md`), "utf8");

    // when
    const ids = sectionIds(extractHeadings(markdown));

    // then
    assert.ok(ids.length > 0, `${page.slug} has no headings`);
    assert.equal(new Set(ids).size, ids.length, `${page.slug} repeats a heading`);
  }
});

test("should not leave a top-level heading in markdown, since the page renders the title", () => {
  for (const page of markdownPages) {
    const markdown = fs.readFileSync(path.join(contentRoot, `${page.slug}.md`), "utf8");
    assert.doesNotMatch(markdown, /^# /m, page.slug);
  }
});

test("should send old single-page CLI anchors to the CLI page", () => {
  assert.equal(legacyHashTarget("#installation"), "/docs/cli#installation");
  assert.equal(legacyHashTarget("#manifest-artifacts"), "/docs/cli#manifest-artifacts");
  assert.equal(legacyHashTarget("#unknown"), null);
  assert.equal(legacyHashTarget(""), null);
});

test("should place every lineage example node below the nodes that feed it", () => {
  for (const example of LINEAGE_EXAMPLES) {
    const generation = new Map(
      example.lineage.nodes.map((node) => [node.id, node.generation]),
    );
    for (const edge of example.lineage.edges) {
      assert.ok(
        generation.get(edge.target) > generation.get(edge.source),
        `${example.id}: ${edge.id}`,
      );
    }
  }
});
