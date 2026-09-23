require("./register.cjs");

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  MAX_COORDINATE_FILE_BYTES,
  loadCoordinateBytes,
  loadCoordinateFile,
} = require("../src/lib/coordinate-file-cache.ts");

test("should retain only two coordinate files when more files are loaded", async () => {
  // given
  const originalFetch = global.fetch;
  const requests = [];
  global.fetch = async (url, init) => {
    const text = String(url);
    requests.push({ url: text, cache: init?.cache });
    return new Response(text, {
      headers: { "content-length": String(text.length) },
    });
  };

  try {
    const first = "https://files.example/first.cif";
    const second = "https://files.example/second.cif";
    const third = "https://files.example/third.cif";

    // when
    await loadCoordinateFile(first);
    await loadCoordinateFile(second);
    await loadCoordinateFile(first);
    await loadCoordinateFile(third);
    await loadCoordinateFile(second);

    // then
    assert.deepEqual(
      requests.map((request) => request.url),
      [first, second, third, second],
    );
    assert.ok(requests.every((request) => request.cache === "no-store"));
  } finally {
    global.fetch = originalFetch;
  }
});

test("should proxy Dynamic PDB coordinate files through the site origin", async () => {
  // given
  const originalFetch = global.fetch;
  let requestedURL;
  global.fetch = async (url) => {
    requestedURL = String(url);
    return new Response("ATOM", { headers: { "content-length": "4" } });
  };

  try {
    const url = "https://files.dynamicpdb.com/models/rerefined.pdb";

    // when
    await loadCoordinateFile(url);

    // then
    assert.equal(requestedURL, `/dpdb-file?u=${encodeURIComponent(url)}`);
  } finally {
    global.fetch = originalFetch;
  }
});

test("should share an in-flight request when the same file is loaded twice", async () => {
  // given
  const originalFetch = global.fetch;
  let finishRequest;
  let requestCount = 0;
  global.fetch = () => {
    requestCount += 1;
    return new Promise((resolve) => {
      finishRequest = resolve;
    });
  };

  try {
    const url = "https://files.example/pending.cif";

    // when
    const first = loadCoordinateFile(url);
    const second = loadCoordinateFile(url);
    finishRequest(
      new Response("data_PENDING", {
        headers: { "content-length": "12" },
      }),
    );

    // then
    assert.equal(first, second);
    assert.equal(await first, "data_PENDING");
    assert.equal(requestCount, 1);
  } finally {
    global.fetch = originalFetch;
  }
});

test("should reject oversized files before retaining them", async () => {
  // given
  const originalFetch = global.fetch;
  let requestCount = 0;
  global.fetch = async () => {
    requestCount += 1;
    return new Response("oversized", {
      headers: { "content-length": String(MAX_COORDINATE_FILE_BYTES + 1) },
    });
  };

  try {
    const url = "https://files.example/oversized.cif";

    // when
    const first = await loadCoordinateFile(url);
    const second = await loadCoordinateFile(url);

    // then
    assert.equal(first, null);
    assert.equal(second, null);
    assert.equal(requestCount, 2);
  } finally {
    global.fetch = originalFetch;
  }
});

test("should reject oversized binary structure files", async () => {
  // given
  const originalFetch = global.fetch;
  let requestInit;
  global.fetch = async (_url, init) => {
    requestInit = init;
    return new Response(Uint8Array.from([1, 2, 3]), {
      headers: { "content-length": String(MAX_COORDINATE_FILE_BYTES + 1) },
    });
  };

  try {
    // when
    const bytes = await loadCoordinateBytes(
      "https://files.example/oversized.ccp4",
    );

    // then
    assert.equal(bytes, null);
    assert.equal(requestInit?.cache, "no-store");
  } finally {
    global.fetch = originalFetch;
  }
});

test("should keep the shared download alive when one reader cancels", async () => {
  // given a reader that attaches, detaches and immediately attaches again --
  // what React's Strict Mode does to every effect in development.
  const originalFetch = global.fetch;
  let requestCount = 0;
  global.fetch = async (_url, init) => {
    requestCount += 1;
    await new Promise((resolve, reject) => {
      const timer = setTimeout(resolve, 10);
      init?.signal?.addEventListener("abort", () => {
        clearTimeout(timer);
        reject(Object.assign(new Error("aborted"), { name: "AbortError" }));
      });
    });
    return new Response("data_SHARED", {
      headers: { "content-length": "11" },
    });
  };

  try {
    const url = "https://files.example/shared.cif";
    const first = new AbortController();
    const second = new AbortController();

    // when
    const dropped = loadCoordinateFile(url, first.signal);
    first.abort();
    const kept = loadCoordinateFile(url, second.signal);

    // then
    assert.equal(await dropped, null);
    assert.equal(await kept, "data_SHARED");
    assert.equal(requestCount, 1);
  } finally {
    global.fetch = originalFetch;
  }
});

test("should cancel the download once the last reader has gone", async () => {
  // given
  const originalFetch = global.fetch;
  let requestSignal;
  global.fetch = async (_url, init) => {
    requestSignal = init?.signal;
    await new Promise((resolve, reject) => {
      const timer = setTimeout(resolve, 50);
      init?.signal?.addEventListener("abort", () => {
        clearTimeout(timer);
        reject(Object.assign(new Error("aborted"), { name: "AbortError" }));
      });
    });
    return new Response("data_ABANDONED", {
      headers: { "content-length": "14" },
    });
  };

  try {
    const url = "https://files.example/abandoned.cif";
    const controller = new AbortController();

    // when
    const abandoned = loadCoordinateFile(url, controller.signal);
    controller.abort();

    // then
    assert.equal(await abandoned, null);
    assert.notEqual(requestSignal, controller.signal);
    await new Promise((resolve) => setTimeout(resolve, 5));
    assert.equal(requestSignal?.aborted, true);
  } finally {
    global.fetch = originalFetch;
  }
});

test("should read a coordinate file for a reader that never cancels", async () => {
  // given
  const originalFetch = global.fetch;
  global.fetch = async () =>
    new Response("coordinates", { headers: { "content-length": "11" } });
  const controller = new AbortController();

  try {
    // when
    const text = await loadCoordinateFile(
      "https://files.example/cancellable.cif",
      controller.signal,
    );

    // then
    assert.equal(text, "coordinates");
  } finally {
    global.fetch = originalFetch;
  }
});
