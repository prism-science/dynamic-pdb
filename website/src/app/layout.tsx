import type { Metadata } from "next";
import type { ReactNode } from "react";

import "molstar/build/viewer/molstar.css";
import "./globals.css";
import AppHeader from "./components/AppHeader";

export const metadata: Metadata = {
  title: "dynamic-pdb",
  description: "Dynamic PDB workflows.",
  icons: { icon: "/dynamic-pdb-mark.svg" },
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <body>
        <AppHeader />
        <div className="appContent">{children}</div>
      </body>
    </html>
  );
}
