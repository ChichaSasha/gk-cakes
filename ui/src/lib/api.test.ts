import { describe, expect, it } from 'vitest';
import { ApiError, apiGet, safeReturnPath, tokenHashDecision } from './api';

const base = 'https://staging-api.gkcake.com';

function fakeFetch(status: number, body: unknown, seen: Request[] = []): typeof fetch {
	return (async (input: RequestInfo | URL, init?: RequestInit) => {
		seen.push(new Request(input, init));
		return new Response(body === undefined ? 'not json' : JSON.stringify(body), { status });
	}) as typeof fetch;
}

/** Awaits a promise expected to reject with an ApiError. */
async function failure(p: Promise<unknown>): Promise<ApiError> {
	try {
		await p;
	} catch (e) {
		expect(e).toBeInstanceOf(ApiError);
		return e as ApiError;
	}
	throw new Error('expected the call to fail');
}

describe('apiGet', () => {
	it('sends the access token as a bearer header only, never cookies', async () => {
		const seen: Request[] = [];
		const me = { account_id: 'a', capabilities: [], session_expires_at: 'x' };
		const got = await apiGet('/api/v1/me', {
			baseUrl: base,
			token: 'tok',
			fetch: fakeFetch(200, me, seen)
		});
		expect(got).toEqual(me);
		expect(seen[0].url).toBe(`${base}/api/v1/me`);
		expect(seen[0].headers.get('authorization')).toBe('Bearer tok');
		expect(seen[0].credentials).toBe('omit');
		expect(seen[0].cache).toBe('no-store');
		expect(seen[0].url).not.toContain('tok');
	});

	it('sends no authorization header without a token', async () => {
		const seen: Request[] = [];
		await apiGet('/api/v1/me', { baseUrl: base, fetch: fakeFetch(200, {}, seen) });
		expect(seen[0].headers.has('authorization')).toBe(false);
	});

	it('refuses anything but an /api/v1 path', async () => {
		for (const path of [
			'/healthz',
			'https://evil.example/api/v1/me',
			'/api/v1/me?x=1',
			'/api/v1/../x',
			'//evil'
		]) {
			await expect(apiGet(path, { baseUrl: base, fetch: fakeFetch(200, {}) })).rejects.toThrow(
				'not an API path'
			);
		}
	});

	it('maps the K00 error body', async () => {
		const body = {
			error: {
				code: 'SESSION_ENDED',
				message: 'Your session has ended.',
				request_id: 'r1',
				retryable: false
			}
		};
		const err = await failure(
			apiGet('/api/v1/admin/me', { baseUrl: base, fetch: fakeFetch(401, body) })
		);
		expect(err.status).toBe(401);
		expect(err.code).toBe('SESSION_ENDED');
		expect(err.requestId).toBe('r1');
		expect(err.signInRequired).toBe(true);
	});

	it('treats an unreadable error and an unreachable API as retryable failures', async () => {
		const bad = await failure(
			apiGet('/api/v1/me', { baseUrl: base, fetch: fakeFetch(502, undefined) })
		);
		expect(bad.status).toBe(502);
		expect(bad.code).toBe('UNKNOWN');
		expect(bad.retryable).toBe(true);
		const unreachable = (async () => {
			throw new TypeError('failed to fetch');
		}) as typeof fetch;
		const down = await failure(apiGet('/api/v1/me', { baseUrl: base, fetch: unreachable }));
		expect(down.status).toBe(0);
		expect(down.code).toBe('NETWORK');
		expect(down.retryable).toBe(true);
	});
});

describe('safeReturnPath', () => {
	it('allows only the known return pages', () => {
		expect(safeReturnPath('/admin')).toBe('/admin');
		expect(safeReturnPath('/account')).toBe('/account');
		for (const p of [
			null,
			undefined,
			'',
			'/',
			'//evil.example',
			'https://evil.example/admin',
			'/admin/../x',
			'/admin?x',
			'javascript:alert(1)'
		]) {
			expect(safeReturnPath(p)).toBe('/account');
		}
	});
});

describe('tokenHashDecision', () => {
	it('verifies invitations only, and never over an existing session', () => {
		expect(tokenHashDecision('invite', false)).toBe('verify');
		expect(tokenHashDecision('invite', true)).toBe('already-signed-in');
		for (const type of ['magiclink', 'email', 'signup', 'recovery', 'email_change', '', null]) {
			expect(tokenHashDecision(type, false)).toBe('refused');
			expect(tokenHashDecision(type, true)).toBe('refused');
		}
	});
});
