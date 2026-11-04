import { describe, expect, it } from 'vitest';
import { isPrivateRoute, partialSignInSettings, withConnectSources } from './headers';
import { origin, publishableKey } from './settings';

describe('withConnectSources', () => {
	const policy =
		"default-src 'self'; script-src 'self' 'nonce-abc'; connect-src 'self'; frame-ancestors 'none'";

	it('adds the runtime origins to connect-src only', () => {
		const got = withConnectSources(policy, ['https://api.example', 'https://p.supabase.co']);
		expect(got).toBe(
			"default-src 'self'; script-src 'self' 'nonce-abc'; connect-src 'self' https://api.example https://p.supabase.co; frame-ancestors 'none'"
		);
	});

	it('creates connect-src when absent, skips unset values and duplicates', () => {
		expect(withConnectSources("default-src 'self'", ['https://api.example'])).toBe(
			"default-src 'self'; connect-src 'self' https://api.example"
		);
		expect(withConnectSources(policy, [undefined, ''])).toBe(policy);
		expect(withConnectSources(policy, ["'self'"])).toBe(policy);
	});
});

describe('isPrivateRoute', () => {
	it('marks sign-in and private routes by route ID', () => {
		for (const r of ['/account', '/admin', '/sign-in', '/auth/callback'])
			expect(isPrivateRoute(r)).toBe(true);
		for (const r of [null, '/', '/healthz', '/accounting', '/administer'])
			expect(isPrivateRoute(r)).toBe(false);
	});
});

describe('partialSignInSettings', () => {
	it('names what is missing only when some settings are set', () => {
		expect(partialSignInSettings({ A: 'x', B: undefined, C: '' })).toEqual(['B', 'C']);
		expect(partialSignInSettings({ A: undefined, B: undefined })).toEqual([]);
		expect(partialSignInSettings({ A: 'x', B: 'y' })).toEqual([]);
	});
});

describe('settings validators', () => {
	it('accepts exact https origins (http only on loopback) and unset values', () => {
		const v = origin('X');
		expect(v('https://staging-api.gkcake.com')).toBe('https://staging-api.gkcake.com');
		expect(v('http://localhost:8080')).toBe('http://localhost:8080');
		expect(v(undefined)).toBeUndefined();
		expect(v('')).toBeUndefined();
		for (const bad of [
			'http://api.example.com',
			'https://api.example.com/',
			'https://api.example.com/v1',
			'not a url',
			'https://u:p@api.example.com'
		]) {
			expect(() => v(bad)).toThrow('X must be an exact');
		}
	});

	it('accepts only publishable keys', () => {
		const v = publishableKey('K');
		expect(v('sb_publishable_2Ykzv4WThHGbG1LJlAksWg_5sbxfkfc')).toBe(
			'sb_publishable_2Ykzv4WThHGbG1LJlAksWg_5sbxfkfc'
		);
		expect(v(undefined)).toBeUndefined();
		for (const bad of [
			'sb_secret_0123456789abcdefgh',
			'eyJhbGciOiJIUzI1NiJ9.x.y',
			'sb_publishable_short'
		]) {
			expect(() => v(bad)).toThrow('K must be a publishable key');
		}
	});
});
