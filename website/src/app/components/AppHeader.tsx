import { getAuthSession } from "@/lib/auth/session";

import HeaderBar, { type HeaderUser } from "./HeaderBar";

export default async function AppHeader() {
  const session = await getAuthSession();

  let user: HeaderUser | null = null;
  if (session) {
    const handle = session.login ?? null;
    user = {
      displayName: session.name || (handle ? `@${handle}` : "Account"),
      subLabel: session.name && handle ? `@${handle}` : null,
      initial: (session.name || handle || "?").charAt(0).toUpperCase(),
    };
  }

  return <HeaderBar user={user} />;
}
