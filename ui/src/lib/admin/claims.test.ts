import { describe, expect, it } from 'vitest';
import {
	countLabel,
	daysWaiting,
	decideClaim,
	decidedBy,
	findClaim,
	howLabel,
	isClaimState,
	looksLikeOrderRef,
	monthLabel,
	sameOrderRequests,
	staffClaims,
	waitText,
	whoContact,
	whoName,
	type StaffClaim
} from './claims';
import type { StaffApi } from './catalogue';

const ID = (n: number) => `00000000-0000-4000-8000-${String(n).padStart(12, '0')}`;
const claim = (over: Partial<StaffClaim> = {}): StaffClaim => ({
	id: ID(1),
	version: 1,
	state: 'pending',
	path: 'evidence',
	channel: 'instagram',
	reference: 'IG-0613',
	approximate_date: '2026-06',
	note: 'My daughter’s birthday cake',
	order_id: null,
	created_at: '2026-11-03T09:15:00Z',
	decided_at: null,
	decided_by: null,
	decision_note: null,
	account: { id: ID(90), full_name: 'Priya Shah', instagram: 'priya.bakes', phone: '07700 900123' },
	...over
});

interface Call {
	method: string;
	url: string;
	headers: Record<string, string>;
	body?: unknown;
}
function fakeApi(handler: (url: URL) => { status?: number; body: unknown }) {
	const calls: Call[] = [];
	const api: StaffApi = {
		baseUrl: 'https://api.example.com',
		token: 'tok',
		fetch: (async (input: RequestInfo | URL, init?: RequestInit) => {
			const url = new URL(String(input));
			calls.push({
				method: init?.method ?? 'GET',
				url: url.pathname + url.search,
				headers: init?.headers as Record<string, string>,
				body: init?.body ? JSON.parse(String(init.body)) : undefined
			});
			const r = handler(url);
			return new Response(JSON.stringify(r.body), { status: r.status ?? 200 });
		}) as typeof fetch
	};
	return { api, calls };
}

describe('staff claim reads', () => {
	it('reads one page of a state, 50 at a time, with the cursor', async () => {
		const { api, calls } = fakeApi(() => ({ body: { claims: [claim()], next_cursor: 'n2' } }));
		const page = await staffClaims(api, 'declined', 'c1');
		expect(page.claims).toHaveLength(1);
		expect(calls[0]).toMatchObject({
			method: 'GET',
			url: '/api/v1/admin/order-claims?state=declined&limit=50&cursor=c1'
		});
		expect(calls[0].headers.Authorization).toBe('Bearer tok');
	});

	it('finds a request by paging the four lists, waiting first', async () => {
		const target = claim({ id: ID(7), state: 'approved' });
		const { api, calls } = fakeApi((url) => {
			const state = url.searchParams.get('state');
			if (state === 'pending') return { body: { claims: [claim()] } };
			if (state === 'approved' && !url.searchParams.get('cursor'))
				return { body: { claims: [claim({ id: ID(5), state: 'approved' })], next_cursor: 'p2' } };
			if (state === 'approved') return { body: { claims: [target] } };
			return { body: { claims: [] } };
		});
		expect(await findClaim(api, ID(7))).toEqual(target);
		expect(calls.map((c) => c.url)).toEqual([
			'/api/v1/admin/order-claims?state=pending&limit=50',
			'/api/v1/admin/order-claims?state=approved&limit=50',
			'/api/v1/admin/order-claims?state=approved&limit=50&cursor=p2'
		]);
	});

	it('returns null for a request in no list (closed by account deletion) or a non-ID', async () => {
		const { api, calls } = fakeApi(() => ({ body: { claims: [] } }));
		expect(await findClaim(api, ID(8))).toBeNull();
		expect(calls).toHaveLength(4);
		expect(await findClaim(api, '../orders')).toBeNull();
		expect(calls).toHaveLength(4);
	});
});

describe('decideClaim (strict JSON, K00)', () => {
	it('approves with the order and declines without one; note only when given', async () => {
		const { api, calls } = fakeApi(() => ({ body: { claim_id: ID(1), state: 'approved' } }));
		await decideClaim(api, 'k1', ID(1), {
			expected_version: 2,
			decision: 'approve',
			order_id: ID(3),
			note: 'Emailed with code 4487'
		});
		await decideClaim(api, 'k2', ID(1), { expected_version: 2, decision: 'decline', note: '' });
		expect(calls.map((c) => [c.method, c.url, c.body, c.headers['Idempotency-Key']])).toEqual([
			[
				'POST',
				`/api/v1/admin/order-claims/${ID(1)}/decision`,
				{
					expected_version: 2,
					decision: 'approve',
					order_id: ID(3),
					note: 'Emailed with code 4487'
				},
				'k1'
			],
			[
				'POST',
				`/api/v1/admin/order-claims/${ID(1)}/decision`,
				{ expected_version: 2, decision: 'decline' },
				'k2'
			]
		]);
	});

	it('refuses non-ID path or order IDs before calling', () => {
		const { api, calls } = fakeApi(() => ({ body: {} }));
		expect(() =>
			decideClaim(api, 'k', 'x/../y', { expected_version: 1, decision: 'decline' })
		).toThrow('not an ID');
		expect(() =>
			decideClaim(api, 'k', ID(1), {
				expected_version: 1,
				decision: 'approve',
				order_id: '8b20d4e6'
			})
		).toThrow('not an ID');
		expect(calls).toHaveLength(0);
	});
});

describe('view model (GK-018 r04d-claims words)', () => {
	it('labels how they ordered by path, then channel', () => {
		expect(howLabel(claim())).toBe('Instagram');
		expect(howLabel(claim({ channel: 'offline' }))).toBe('Email, phone or in person');
		expect(howLabel(claim({ channel: 'website_email_link' }))).toBe('On this website');
		expect(howLabel(claim({ path: 'email_link', channel: 'website_email_link' }))).toBe(
			'From the order page or email'
		);
	});

	it('shows profile details as typed, or says they are missing', () => {
		expect(whoName(claim())).toBe('Priya Shah');
		expect(whoContact(claim())).toBe('Instagram priya.bakes · 07700 900123');
		const empty = claim({ account: { id: ID(9), full_name: ' ', instagram: null, phone: null } });
		expect(whoName(empty)).toBe('No name in their profile');
		expect(whoContact(empty)).toBe('No Instagram or phone');
	});

	it('counts waiting time in London calendar days', () => {
		const now = new Date('2026-11-06T10:30:00Z');
		expect(daysWaiting('2026-11-03T09:15:00Z', now)).toBe(3);
		expect(waitText('2026-11-03T09:15:00Z', now)).toBe('Waiting 3 days');
		expect(waitText('2026-11-05T23:30:00Z', now)).toBe('Waiting since yesterday');
		expect(waitText('2026-11-06T00:30:00Z', now)).toBe('Waiting since today');
		// 23:30 UTC on 31 Oct (GMT) is still 31 Oct in London; BST ended on 25 Oct.
		expect(daysWaiting('2026-10-31T23:30:00Z', new Date('2026-11-01T00:30:00Z'))).toBe(1);
		// 23:30 UTC on 30 Jun is 1 Jul in London (BST).
		expect(daysWaiting('2026-06-30T23:30:00Z', new Date('2026-07-01T08:00:00Z'))).toBe(0);
	});

	it('words months and spots website order references (a hint, not a match)', () => {
		expect(monthLabel('2026-06')).toBe('June 2026');
		expect(monthLabel('2026-06-14')).toBe('June 2026');
		expect(monthLabel('2026-13')).toBeNull();
		expect(monthLabel(null)).toBeNull();
		expect(looksLikeOrderRef('8b20d4e6')).toBe(true);
		expect(looksLikeOrderRef('8b20d4e6-1a2b-4c3d-8e9f-0123456789ab')).toBe(true);
		expect(looksLikeOrderRef('IG-0613')).toBe(false);
		expect(looksLikeOrderRef(null)).toBe(false);
	});

	it('says who decided, finds other requests for the same order and labels the count', () => {
		expect(decidedBy(claim({ decided_by: ID(50) }), ID(50))).toBe('you');
		expect(decidedBy(claim({ decided_by: ID(51) }), ID(50))).toBe('another chef account');
		const a = claim({ id: ID(1), path: 'email_link', order_id: ID(3) });
		const b = claim({ id: ID(2), path: 'email_link', order_id: ID(3) });
		const c = claim({ id: ID(4), path: 'email_link', order_id: ID(6) });
		expect(sameOrderRequests(a, [a, b, c]).map((x) => x.id)).toEqual([ID(2)]);
		expect(sameOrderRequests(claim(), [a, b])).toEqual([]);
		expect(countLabel({ claims: [claim(), claim()] })).toBe('2');
		expect(
			countLabel({ claims: Array.from({ length: 50 }, () => claim()), next_cursor: 'x' })
		).toBe('50+');
		expect(isClaimState('approved')).toBe(true);
		expect(isClaimState('closed')).toBe(false);
	});
});
