// Browser sign-in (GK-021) with the identity provider's SDK (Supabase).
// PKCE flow: an email link returns a one-time code to /auth/callback, which
// is exchanged using a verifier held only in this browser, then removed from
// the URL. Invitation emails link to /auth/callback with a token hash instead,
// verified once and only when nobody is signed in in this browser. The SDK
// keeps the session in memory and persists it, with its access and refresh
// tokens, in this origin's localStorage (the provider-supported store; a
// GK-021 decision for Creator review, behind a strict Content Security
// Policy). Sign-out ends the provider session when it is reachable and always
// clears the stored one; private pages in other tabs follow (leaveOnSignOut).
import { createClient, type Session, type SupabaseClient } from '@supabase/supabase-js';
import { browser } from '$app/env';
import { PUBLIC_API_URL, PUBLIC_AUTH_PUBLISHABLE_KEY, PUBLIC_AUTH_URL } from '$app/env/public';
import { apiGet, safeReturnPath, type ApiOptions, type ReturnPath } from './api';

/** Whether sign-in is configured for this environment. */
export const signInConfigured = Boolean(
	PUBLIC_API_URL && PUBLIC_AUTH_URL && PUBLIC_AUTH_PUBLISHABLE_KEY
);

const RETURN_KEY = 'gk-return-path';
const RETURN_TTL_MS = 30 * 60 * 1000; // as long as an email link is valid

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

/** Whether the storefront can reach the API (PUBLIC_API_URL is set). */
export const shopConfigured = Boolean(PUBLIC_API_URL);

/**
 * API options for a storefront call: the signed-in customer's access token
 * when there is a session, otherwise none (a guest, GK-030).
 */
export async function customerApi(): Promise<ApiOptions> {
	const session = signInConfigured ? await currentSession() : null;
	return { baseUrl: PUBLIC_API_URL as string, token: session?.access_token };
}

/**
 * Remembers where to go after an email-link sign-in (never in the URL). It
 * is kept in localStorage for 30 minutes, because the link usually opens in
 * a new tab.
 */
export function rememberReturnPath(path: string): void {
	localStorage.setItem(
		RETURN_KEY,
		JSON.stringify({ path: safeReturnPath(path), expires: Date.now() + RETURN_TTL_MS })
	);
}

/** The remembered return page, once (an allowed page, or /account). */
export function takeReturnPath(): ReturnPath {
	const raw = localStorage.getItem(RETURN_KEY);
	localStorage.removeItem(RETURN_KEY);
	try {
		const saved = JSON.parse(raw ?? 'null') as { path?: string; expires?: number } | null;
		return saved && typeof saved.expires === 'number' && saved.expires > Date.now()
			? safeReturnPath(saved.path)
			: '/account';
	} catch {
		return '/account';
	}
}

/**
 * Signs in with an invitation's token hash, once. The callback decides first
 * (tokenHashDecision) that it is an invitation and nobody is signed in.
 */
export async function verifyInvitation(tokenHash: string): Promise<boolean> {
	const { error } = await auth().auth.verifyOtp({ token_hash: tokenHash, type: 'invite' });
	return !error;
}

/**
 * Sends the current private page to sign-in when the session ends without
 * this tab asking (signed out in another tab, or the refresh failed).
 * Returns an unsubscribe, for onMount.
 */
export function leaveOnSignOut(leave: () => void): () => void {
	return signInConfigured ? onSignedOut(leave) : () => {};
}

/** The exact email-link callback URL for this site (allowlisted at the provider). */
export function callbackUrl(): string {
	return `${location.origin}/auth/callback`;
}

/**
 * Ends the provider session and clears local state. Returns false when the
 * provider could not be reached: this device is signed out regardless, but
 * the session may stay valid at the provider until it expires.
 */
export async function signOut(): Promise<boolean> {
	localStorage.removeItem(RETURN_KEY);
	signingOutHere = true;
	try {
		const { error } = await auth().auth.signOut({ scope: 'local' });
		return !error;
	} finally {
		signingOutHere = false;
	}
}

// This tab's own sign-out is handled by the page that started it.
let signingOutHere = false;

/**
 * Calls fn when the session ends without this tab asking (another tab signed
 * out, or the session could not be refreshed). Returns an unsubscribe.
 */
export function onSignedOut(fn: () => void): () => void {
	const { data } = auth().auth.onAuthStateChange((event) => {
		if (event === 'SIGNED_OUT' && !signingOutHere) fn();
	});
	return () => data.subscription.unsubscribe();
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
