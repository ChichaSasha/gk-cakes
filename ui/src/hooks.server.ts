// Response headers for every page (GK-021). SvelteKit builds the Content
// Security Policy with per-response nonces (vite.config.ts); the API and the
// identity provider are added to connect-src here because their origins are
// runtime settings (one UI build serves every environment).
import type { Handle, ServerInit } from '@sveltejs/kit/hooks';
import { PUBLIC_API_URL, PUBLIC_AUTH_PUBLISHABLE_KEY, PUBLIC_AUTH_URL } from '$app/env/public';
import { isPrivateRoute, partialSignInSettings, withConnectSources } from './lib/headers';

export const init: ServerInit = () => {
	const missing = partialSignInSettings({
		PUBLIC_API_URL,
		PUBLIC_AUTH_URL,
		PUBLIC_AUTH_PUBLISHABLE_KEY
	});
	if (missing.length > 0) {
		console.warn(`sign-in is not configured: missing ${missing.join(', ')}`);
	}
};

export const handle: Handle = async ({ event, resolve }) => {
	const response = await resolve(event);
	const h = response.headers;
	const policy = h.get('content-security-policy');
	if (policy) {
		h.set('content-security-policy', withConnectSources(policy, [PUBLIC_API_URL, PUBLIC_AUTH_URL]));
	}
	h.set('x-content-type-options', 'nosniff');
	h.set('referrer-policy', 'strict-origin-when-cross-origin');
	h.set('cross-origin-opener-policy', 'same-origin');
	h.set('permissions-policy', 'camera=(), microphone=(), geolocation=()');
	if (isPrivateRoute(event.route.id)) {
		h.set('cache-control', 'no-store');
		h.set('x-robots-tag', 'noindex, nofollow');
		// The callback URL carries a one-time code until the page removes it.
		h.set('referrer-policy', 'no-referrer');
	}
	return response;
};
