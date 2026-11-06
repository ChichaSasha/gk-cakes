// The account API as the signed-in customer (browser only; account pages
// render a shell on the server and load everything here, K00).
import { PUBLIC_API_URL } from '$app/env/public';
import { currentSession, signInConfigured } from '../auth';
import type { AccountApi } from './client';

/**
 * The account API with this browser's session, or null when nobody is signed
 * in. The token is read again for every request (the SDK refreshes it), so a
 * page left open for hours never sends an expired one.
 */
export async function accountApi(): Promise<AccountApi | null> {
	if (!signInConfigured || !PUBLIC_API_URL) return null;
	if (!(await currentSession())) return null;
	return {
		baseUrl: PUBLIC_API_URL,
		token: async () => (await currentSession())?.access_token
	};
}

/** The signed-in email address (from the session), for "Signed in as …". */
export async function signedInEmail(): Promise<string | undefined> {
	if (!signInConfigured) return undefined;
	return (await currentSession())?.user?.email ?? undefined;
}
