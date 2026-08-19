import path from "node:path";

import type { NextConfig } from "next";

const config: NextConfig = {
  reactStrictMode: true,
  devIndicators: false,
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
