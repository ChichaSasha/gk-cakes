// Browser sign-in (GK-021) with the identity provider's SDK (Supabase).
// PKCE flow: an email link returns a one-time code to /auth/callback, which
// is exchanged using a verifier held only in this browser, then removed from
// the URL. The SDK keeps the session in memory and persists it in this
// origin's localStorage (the provider-supported store); a strict Content
// Security Policy limits script to this site. Sign-out ends the provider
// session and clears the stored one.
import { createClient, type Session, type SupabaseClient } from '@supabase/supabase-js';
import { browser } from '$app/env';
import { PUBLIC_API_URL, PUBLIC_AUTH_PUBLISHABLE_KEY, PUBLIC_AUTH_URL } from '$app/env/public';
import { apiGet, safeReturnPath, type ReturnPath } from './api';

/** Whether sign-in is configured for this environment. */
export const signInConfigured = Boolean(
	PUBLIC_API_URL && PUBLIC_AUTH_URL && PUBLIC_AUTH_PUBLISHABLE_KEY
);

const RETURN_KEY = 'gk-return-path';

let client: SupabaseClient | undefined;

/** The provider client (browser only). */
export function auth(): SupabaseClient {
	if (!browser || !signInConfigured) throw new Error('sign-in is not available here');
	client ??= createClient(PUBLIC_AUTH_URL as string, PUBLIC_AUTH_PUBLISHABLE_KEY as string, {
		auth: {
			flowType: 'pkce',
			persistSession: true,
			autoRefreshToken: true,
			detectSessionInUrl: false,
			storageKey: 'gk-auth'
		}
	});
	return client;
}

/** The current session, refreshed by the SDK when needed. */
export async function currentSession(): Promise<Session | null> {
	const { data } = await auth().auth.getSession();
	return data.session;
}

/** GETs an API path with the current session's access token. */
export async function authedGet<T>(path: string): Promise<T> {
	const session = await currentSession();
	return apiGet<T>(path, { baseUrl: PUBLIC_API_URL as string, token: session?.access_token });
}

/** Remembers where to go after an email-link sign-in (never in the URL). */
export function rememberReturnPath(path: string): void {
	sessionStorage.setItem(RETURN_KEY, safeReturnPath(path));
}

/** The remembered return page, once. */
export function takeReturnPath(): ReturnPath {
	const path = sessionStorage.getItem(RETURN_KEY);
	sessionStorage.removeItem(RETURN_KEY);
	return safeReturnPath(path);
}

/** The exact email-link callback URL for this site (allowlisted at the provider). */
export function callbackUrl(): string {
	return `${location.origin}/auth/callback`;
}

/** Ends the provider session and clears local state. */
export async function signOut(): Promise<void> {
	try {
		await auth().auth.signOut({ scope: 'local' });
	} finally {
		sessionStorage.removeItem(RETURN_KEY);
	}
}

export interface Me {
	account_id: string;
	capabilities: string[];
	session_expires_at: string;
}

export interface StaffMember {
	account_id: string;
	capability: string;
	granted_at: string;
}
