import { describe, expect, it } from 'vitest';
import {
	claimOlderOrder,
	deleteAccount,
	deletionPreview,
	MAX_ORDER_PAGES,
	me,
	myClaim,
	myClaims,
	myOrders,
	profile,
	updateProfile,
	withdrawClaim,
	type AccountApi
} from './client';

const CLAIM = 'c0ffee12-2222-4222-8222-222222222222';

function fakeApi(...replies: { status?: number; body: unknown }[]) {
	const calls: { method: string; url: string; headers: Record<string, string>; body?: unknown }[] =
		[];
	const api: AccountApi = {
		baseUrl: 'https://api.example.com',
		token: 'customer-token',
		fetch: (async (input: RequestInfo | URL, init?: RequestInit) => {
			calls.push({
				method: init?.method ?? 'GET',
				url: String(input).replace('https://api.example.com', ''),
				headers: init?.headers as Record<string, string>,
				body: init?.body ? JSON.parse(String(init.body)) : undefined
			});
			const r = replies.shift() ?? { body: {} };
			return new Response(JSON.stringify(r.body), { status: r.status ?? 200 });
		}) as typeof fetch
	};
	return { api, calls };
}

describe('account API client', () => {
	it('reads every page of orders with the customer token', async () => {
		const { api, calls } = fakeApi(
			{ body: { orders: [{ id: '1' }], next_cursor: 'n1' } },
			{ body: { orders: [{ id: '2' }] } }
		);
		expect((await myOrders(api)).map((o) => o.id)).toEqual(['1', '2']);
		expect(calls.map((c) => c.url)).toEqual([
			'/api/v1/me/orders?limit=50',
			'/api/v1/me/orders?limit=50&cursor=n1'
		]);
		expect(calls[0].headers.Authorization).toBe('Bearer customer-token');
	});

	it('reads the token for every request when given a function (a refreshed session is used)', async () => {
		const { api, calls } = fakeApi({ body: { claims: [] } }, { body: { claims: [] } });
		let n = 0;
		const fresh: AccountApi = { ...api, token: async () => `token-${++n}` };
		await myClaims(fresh);
		await myClaims(fresh);
		expect(calls.map((c) => c.headers.Authorization)).toEqual(['Bearer token-1', 'Bearer token-2']);
	});

	it('throws rather than return a shortened order list past the page limit', async () => {
		const { api } = fakeApi(
			...Array.from({ length: MAX_ORDER_PAGES }, (_, i) => ({
				body: { orders: [], next_cursor: `c${i}` }
			}))
		);
		await expect(myOrders(api)).rejects.toThrow('more than');
	});

	it('reads me, claims, one claim and the profile', async () => {
		const { api, calls } = fakeApi(
			{ body: { account_id: 'a', capabilities: [] } },
			{ body: { claims: [] } },
			{ body: { id: CLAIM } },
			{ body: { full_name: null, phone: null, instagram: null, version: 1, updated_at: null } }
		);
		await me(api);
		expect(await myClaims(api)).toEqual([]);
		await myClaim(api, CLAIM);
		await profile(api);
		expect(calls.map((c) => c.url)).toEqual([
			'/api/v1/me',
			'/api/v1/me/order-claims',
			`/api/v1/me/order-claims/${CLAIM}`,
			'/api/v1/me/profile'
		]);
		await expect(myClaim(api, '../orders')).rejects.toThrow('not an ID');
	});

	it('sends commands with their key and exactly the documented fields', async () => {
		const { api, calls } = fakeApi(
			{ body: {} },
			{ status: 201, body: {} },
			{ status: 201, body: {} },
			{ body: {} }
		);
		await updateProfile(api, 'p1', { expected_version: 2, full_name: 'Anna', phone: null });
		await claimOlderOrder(api, 'c1', {
			channel: 'instagram',
			reference: 'IG-0613',
			note: 'Pink cake'
		});
		await claimOlderOrder(api, 'c2', {
			channel: 'offline',
			reference: 'X1',
			approximate_date: '2026-06',
			note: 'n'
		});
		await withdrawClaim(api, 'w1', CLAIM, 1);
		expect(calls.map((c) => [c.method, c.url, c.body, c.headers['Idempotency-Key']])).toEqual([
			[
				'PATCH',
				'/api/v1/me/profile',
				{ expected_version: 2, full_name: 'Anna', phone: null },
				'p1'
			],
			[
				'POST',
				'/api/v1/me/order-claims',
				{ channel: 'instagram', reference: 'IG-0613', note: 'Pink cake' },
				'c1'
			],
			[
				'POST',
				'/api/v1/me/order-claims',
				{ channel: 'offline', reference: 'X1', note: 'n', approximate_date: '2026-06' },
				'c2'
			],
			['POST', `/api/v1/me/order-claims/${CLAIM}/withdraw`, { expected_version: 1 }, 'w1']
		]);
	});

	it('passes refusals through (409 stale profile, 403 disabled)', async () => {
		const err = (code: string, status: number) => ({
			status,
			body: { error: { code, message: 'x', request_id: 'r', retryable: false } }
		});
		const { api } = fakeApi(err('STALE_VERSION', 409), err('ACCOUNT_DISABLED', 403));
		await expect(
			updateProfile(api, 'k', { expected_version: 1, phone: '1' })
		).rejects.toMatchObject({
			status: 409,
			code: 'STALE_VERSION'
		});
		await expect(me(api)).rejects.toMatchObject({ code: 'ACCOUNT_DISABLED' });
	});

	it('asks for a deletion preview with an empty body and deletes with exactly its ID and hash', async () => {
		const preview = {
			preview_id: CLAIM,
			preview_hash: 'a'.repeat(64),
			expires_at: '2026-11-06T10:15:00Z',
			can_delete: true,
			blocked: [],
			upcoming: [],
			linked_orders: 0,
			pending_claims: 0,
			staff_capabilities: []
		};
		const { api, calls } = fakeApi(
			{ body: preview },
			{
				body: {
					account_id: 'a',
					deleted: true,
					deleted_at: '2026-11-06T10:01:00Z',
					orders_unlinked: 2,
					provider_deletion: 'pending'
				}
			}
		);
		const p = await deletionPreview(api, 'pv1');
		const d = await deleteAccount(api, 'del1', { ...p, extra: 'never sent' } as typeof p);
		expect(d.orders_unlinked).toBe(2);
		expect(calls.map((c) => [c.method, c.url, c.body, c.headers['Idempotency-Key']])).toEqual([
			['POST', '/api/v1/me/deletion-preview', {}, 'pv1'],
			['POST', '/api/v1/me/deletion', { preview_id: CLAIM, preview_hash: 'a'.repeat(64) }, 'del1']
		]);
		expect(calls[1].headers.Authorization).toBe('Bearer customer-token');
	});

	it('passes deletion refusals through (fresh sign-in needed, blocked)', async () => {
		const err = (code: string, status: number) => ({
			status,
			body: { error: { code, message: 'x', request_id: 'r', retryable: false } }
		});
		const { api } = fakeApi(
			err('REAUTHENTICATION_REQUIRED', 401),
			err('ACCOUNT_DELETION_BLOCKED', 409)
		);
		const input = { preview_id: CLAIM, preview_hash: 'b'.repeat(64) };
		await expect(deleteAccount(api, 'k1', input)).rejects.toMatchObject({
			status: 401,
			code: 'REAUTHENTICATION_REQUIRED'
		});
		await expect(deleteAccount(api, 'k2', input)).rejects.toMatchObject({
			code: 'ACCOUNT_DELETION_BLOCKED'
		});
	});
});
