import type { Metadata } from "next";
import type { ReactNode } from "react";

import "./molstar-skin.scss";
// Precompiled from Tailwind inside the package; every utility is scoped under
// the .hetstar wrapper the viewer renders, and preflight is off.
import "@dynamic-pdb/hetstar/styles.css";
import "./globals.css";
import Analytics from "./components/Analytics";
import AppHeader from "./components/AppHeader";
import DevBanner from "./components/DevBanner";

export const metadata: Metadata = {
  title: "dynamic-pdb",
  description: "Dynamic PDB workflows.",
  icons: {
    icon: [{ url: "/icon.png", type: "image/png", sizes: "512x512" }],
    apple: "/apple-touch-icon.png",
  },
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <body>
        <AppHeader />
        <DevBanner />
        <div className="appContent">{children}</div>
        <Analytics />
      </body>
    </html>
  );
}
