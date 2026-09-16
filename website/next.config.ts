import path from "node:path";

import type { NextConfig } from "next";

const config: NextConfig = {
  reactStrictMode: true,
  // hetstar and hetkit are vendored as a git submodule (website/vendor/hetstar)
  // and ship their `src/` as raw TypeScript rather than a built bundle, so Next
  // has to compile them the way it compiles our own source.
  transpilePackages: ["@dynamic-pdb/hetkit", "@dynamic-pdb/hetstar"],
  devIndicators: false,
  // The dev server is reached through an ngrok tunnel as well as localhost, and
  // Next blocks dev-time requests (assets, HMR) whose Origin it does not know:
  // through the tunnel the page renders but its client chunks come back empty,
  // so React never finishes hydrating and nothing on the page is interactive.
  // Dev only — it has no effect on a production build.
  allowedDevOrigins: ["*.ngrok-free.dev", "*.ngrok.io", "*.ngrok-free.app"],
  // Mol* ships its skin as Sass and expects to be loaded by package path
  // (`@use 'molstar/lib/mol-plugin-ui/skin/base/colors'`). Sass resolves loads
  // against these roots, so pointing it at node_modules lets src/app/molstar-
  // skin.scss configure the viewer the same way Mol*'s own dark and blue themes
  // do, instead of us shipping a hand-edited copy of their compiled CSS.
  sassOptions: {
    includePaths: [path.join(process.cwd(), "node_modules")],
  },
};

export default config;
