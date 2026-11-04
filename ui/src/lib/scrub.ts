// Removes sign-in secrets from a browser URL (GK-021, contracts K00: no
// tokens in URLs). Used once when the app starts (src/hooks.client.ts) so a
// one-time code, token hash, provider error or implicit-flow token never
// stays in the address bar or history, wherever the provider sent it. Only
// /auth/callback reads its own code or token hash, and removes them itself.

// Always removed outside the callback. `code` and `type` are removed only in
// their sign-in shapes (a provider code is a UUID; `type` goes with a token
// hash), so future pages may use those names for other things.
const SECRET_PARAMS = [
	'token_hash',
	'error',
	'error_code',
	'error_description',
	'access_token',
	'refresh_token',
	'provider_token',
	'provider_refresh_token'
];
const PROVIDER_CODE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * Returns the URL (path, query and fragment) with sign-in secrets removed,
 * or null when nothing needs removing. Implicit-flow fragments are never
 * accepted, so a fragment carrying a token or error is dropped everywhere.
 */
export function scrubbedUrl(href: string): string | null {
	const url = new URL(href);
	let changed = false;
	if (
		/(^#|&)(access_token|refresh_token|provider_token|error|error_code|error_description)=/.test(
			url.hash
		)
	) {
		url.hash = '';
		changed = true;
	}
	if (url.pathname !== '/auth/callback') {
		const remove = [...SECRET_PARAMS];
		if (PROVIDER_CODE.test(url.searchParams.get('code') ?? '')) remove.push('code');
		if (url.searchParams.has('token_hash')) remove.push('type');
		for (const name of remove) {
			if (url.searchParams.has(name)) {
				url.searchParams.delete(name);
				changed = true;
			}
		}
	}
	return changed ? url.pathname + url.search + url.hash : null;
}
