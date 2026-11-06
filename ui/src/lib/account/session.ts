// The account API as the signed-in customer (browser only; account pages
// render a shell on the server and load everything here, K00).
import { PUBLIC_API_URL } from '$app/env/public';
import { currentSession, signInConfigured } from '../auth';
import type { AccountApi } from './client';
import { signInAt } from './privacy';

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

/**
 * The session's latest email-link or Google sign-in instant (ms), or null:
 * whether the deletion's fresh sign-in is likely done (the API decides).
 */
/**
 * A one-way identifier of the signed-in account for the deletion marker:
 * SHA-256 of the session's subject (hex). Not personal data in itself, and
 * it never leaves this browser.
 */
export async function accountMarkerId(): Promise<string | undefined> {
	if (!signInConfigured) return undefined;
	const sub = (await currentSession())?.user?.id;
	if (!sub) return undefined;
	const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(sub));
	return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('');
}

export async function sessionSignInAt(): Promise<number | null> {
	if (!signInConfigured) return null;
	return signInAt((await currentSession())?.access_token);
}
