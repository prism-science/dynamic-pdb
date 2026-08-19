import type { Metadata } from "next";
import type { ReactNode } from "react";

import "./molstar-skin.scss";
import "./globals.css";
import AppHeader from "./components/AppHeader";
import AppFooter from "./components/AppFooter";

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
        <div className="appContent">{children}</div>
        <AppFooter />
      </body>
    </html>
  );
}
