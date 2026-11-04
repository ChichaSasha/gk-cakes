import { describe, expect, it } from 'vitest';
import { scrubbedUrl } from './scrub';

const site = 'https://staging.gkcake.com';
const uuid = '123e4567-e89b-42d3-a456-426614174000';

describe('scrubbedUrl', () => {
	it('removes provider codes, token hashes and errors outside the callback', () => {
		expect(scrubbedUrl(`${site}/?code=${uuid}`)).toBe('/');
		expect(scrubbedUrl(`${site}/admin?token_hash=abc&type=invite`)).toBe('/admin');
		expect(
			scrubbedUrl(`${site}/?error=access_denied&error_code=otp_expired&error_description=x`)
		).toBe('/');
		expect(scrubbedUrl(`${site}/account?code=${uuid}&keep=1`)).toBe('/account?keep=1');
	});

	it('drops implicit-flow fragments everywhere, including the callback', () => {
		expect(scrubbedUrl(`${site}/#access_token=a&refresh_token=b&type=invite`)).toBe('/');
		expect(scrubbedUrl(`${site}/auth/callback#error=access_denied&error_code=otp_expired`)).toBe(
			'/auth/callback'
		);
		expect(scrubbedUrl(`${site}/x#a=1&refresh_token=b`)).toBe('/x');
	});

	it('leaves the callback its own code and token hash to read', () => {
		expect(scrubbedUrl(`${site}/auth/callback?code=${uuid}`)).toBeNull();
		expect(scrubbedUrl(`${site}/auth/callback?token_hash=abc&type=invite`)).toBeNull();
	});

	it('keeps other names and shapes for future pages', () => {
		expect(scrubbedUrl(`${site}/checkout?code=FREEDELIVERY`)).toBeNull();
		expect(scrubbedUrl(`${site}/cakes?type=birthday`)).toBeNull();
		expect(scrubbedUrl(`${site}/#section-2`)).toBeNull();
		expect(scrubbedUrl(`${site}/`)).toBeNull();
	});
});
