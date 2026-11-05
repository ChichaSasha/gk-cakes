// Validators for the public runtime settings declared in src/env.ts (GK-021).

/** An exact origin: https (http only for a loopback host), no path. */
export function origin(name: string) {
	return (value: string | undefined): string | undefined => {
		if (value === undefined || value === '') return undefined;
		let url: URL;
		try {
			url = new URL(value);
		} catch {
			throw new Error(`${name} must be an exact origin such as https://example.com`);
		}
		const loopback = ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname);
		if (
			url.origin !== value ||
			(url.protocol !== 'https:' && !(url.protocol === 'http:' && loopback))
		) {
			throw new Error(`${name} must be an exact https origin such as https://example.com`);
		}
		return value;
	};
}

/** A publishable key (sb_publishable_...), never a secret key. */
export function publishableKey(name: string) {
	return (value: string | undefined): string | undefined => {
		if (value === undefined || value === '') return undefined;
		if (!/^sb_publishable_[A-Za-z0-9_-]{16,}$/.test(value)) {
			throw new Error(`${name} must be a publishable key (sb_publishable_...)`);
		}
		return value;
	};
}
