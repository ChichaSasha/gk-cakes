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

/**
 * The signed-in email address and marker hash from ONE session read, so the
 * address a link goes to and the account it is marked for always match.
 */
export async function sessionIdentity(): Promise<{ email?: string; who?: string }> {
	if (!signInConfigured) return {};
	const session = await currentSession();
	return { email: session?.user?.email ?? undefined, who: await markerIdOf(session?.user?.id) };
}

async function markerIdOf(sub: string | undefined): Promise<string | undefined> {
	try {
		const subtle = globalThis.crypto?.subtle;
		if (!sub || !subtle) return undefined;
		const digest = await subtle.digest('SHA-256', new TextEncoder().encode(sub));
		return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('');
	} catch {
		// No hash (for example an insecure context): no marker, so no shortcut.
		return undefined;
	}
}

/** The signed-in email address (from the session), for "Signed in as …". */
export async function signedInEmail(): Promise<string | undefined> {
	if (!signInConfigured) return undefined;
	return (await currentSession())?.user?.email ?? undefined;
}

/**
 * A one-way identifier of the signed-in account for the deletion marker:
 * SHA-256 of the session's subject (hex). Not personal data in itself, and
 * it never leaves this browser.
 */
export async function accountMarkerId(): Promise<string | undefined> {
	if (!signInConfigured) return undefined;
	return markerIdOf((await currentSession())?.user?.id);
}

/**
 * The session's latest email-link or Google sign-in instant (ms), or null:
 * whether the deletion's fresh sign-in is likely done (the API decides).
 */
export async function sessionSignInAt(): Promise<number | null> {
	if (!signInConfigured) return null;
	return signInAt((await currentSession())?.access_token);
}
