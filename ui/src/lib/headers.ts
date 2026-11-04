// Response-header helpers for src/hooks.server.ts (GK-021).

/** Adds origins to the policy's connect-src directive (creating it if absent). */
export function withConnectSources(policy: string, origins: (string | undefined)[]): string {
	const add = origins.filter((o): o is string => Boolean(o));
	if (add.length === 0) return policy;
	const directives = policy
		.split(';')
		.map((d) => d.trim())
		.filter(Boolean);
	const i = directives.findIndex((d) => d.split(/\s+/)[0] === 'connect-src');
	if (i < 0) {
		directives.push(['connect-src', "'self'", ...add].join(' '));
	} else {
		const sources = directives[i].split(/\s+/);
		for (const o of add) if (!sources.includes(o)) sources.push(o);
		directives[i] = sources.join(' ');
	}
	return directives.join('; ');
}

/**
 * Whether a route is private or carries a one-time code. Decided by the
 * route SvelteKit matched (not the raw path, which may be percent-encoded).
 */
export function isPrivateRoute(routeId: string | null): boolean {
	return routeId !== null && /^\/(account|admin|sign-in|auth)(\/|$)/.test(routeId);
}

/** Names of the sign-in settings that are missing when only some are set. */
export function partialSignInSettings(values: Record<string, string | undefined>): string[] {
	const missing = Object.keys(values).filter((k) => !values[k]);
	return missing.length > 0 && missing.length < Object.keys(values).length ? missing : [];
}
