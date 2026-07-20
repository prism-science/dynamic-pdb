import type { ReactNode } from "react";

import { ApiRequestError, listEntries } from "@/lib/api/entries";
import { getAuthSession } from "@/lib/auth/session";
import AuthBrand from "./components/AuthBrand";
import EntriesBrowser from "./components/EntriesBrowser";
import LoginButton from "./components/LoginButton";

import styles from "./page.module.css";

export const dynamic = "force-dynamic";

type SearchParamValue = string | string[] | undefined;

type HomeProps = {
  searchParams?: Promise<Record<string, SearchParamValue>>;
};

export default async function Home({ searchParams }: HomeProps) {
  const session = await getAuthSession();
  const resolvedSearchParams = (await searchParams) ?? {};
  const loginError = firstQueryValue(resolvedSearchParams.login_error);
  const loginErrorDetail = firstQueryValue(
    resolvedSearchParams.login_error_detail,
  );

  if (!session) {
    return (
      <div className={styles.signInWrap}>
        <section className={styles.signInCard}>
          <AuthBrand />

          <LoginButton className={styles.githubButton}>
            Continue with GitHub
          </LoginButton>

          {loginError ? (
            <Alert>
              {getLoginErrorMessage(loginError)}
              {loginErrorDetail ? ` Details: ${loginErrorDetail}` : ""}
            </Alert>
          ) : null}
        </section>
      </div>
    );
  }

  const entries = await loadEntries(session.token);

  return (
    <main className={styles.page} aria-label="dynamic-pdb entries">
      <section className={styles.entriesShell}>
        <EntriesBrowser entries={entries} />
      </section>
    </main>
  );
}

async function loadEntries(token: string) {
  try {
    return await listEntries(token);
  } catch (error) {
    if (error instanceof ApiRequestError) {
      return [];
    }
    throw error;
  }
}

function Alert({ children }: { children: ReactNode }) {
  return (
    <p className={styles.alert}>
      <svg
        className={styles.alertIcon}
        width="16"
        height="16"
        viewBox="0 0 16 16"
        fill="none"
        aria-hidden="true"
      >
        <circle cx="8" cy="8" r="7" stroke="currentColor" strokeWidth="1.4" />
        <path
          d="M8 4.5v4M8 11h.01"
          stroke="currentColor"
          strokeWidth="1.6"
          strokeLinecap="round"
        />
      </svg>
      <span>{children}</span>
    </p>
  );
}

function firstQueryValue(value: SearchParamValue): string | null {
  if (typeof value === "string") {
    return value;
  }
  if (Array.isArray(value)) {
    return value[0] ?? null;
  }
  return null;
}

function getLoginErrorMessage(error: string): string {
  switch (error) {
    case "access_denied":
      return "GitHub authorization was cancelled before the login could finish.";
    case "forbidden":
      return "Your GitHub account is signed in, but it is not in an allowed organization for dynamic-pdb yet.";
    case "invalid_state":
      return "The login session expired or no longer matches this browser tab. Please try again.";
    case "missing_code":
      return "GitHub did not return an authorization code. Please try again.";
    case "github_exchange_failed":
      return "GitHub sign-in completed, but token exchange failed.";
    case "backend_exchange_failed":
      return "GitHub sign-in worked, but the backend rejected or failed to exchange the token.";
    default:
      return "Login failed. Please try again.";
  }
}
