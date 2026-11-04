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

/** Pages whose content is private or carries a one-time code. */
export function isPrivatePath(pathname: string): boolean {
	return /^\/(account|admin|sign-in|auth)(\/|$)/.test(pathname);
}
