import { describe, expect, it } from 'vitest';
import { ApiError } from '../api';
import {
	canMovePickup,
	checkEmail,
	confirmPlaceholder,
	defaultMonths,
	isUpcoming,
	matchOrders,
	monthsRange,
	monthsSpan,
	monthOptions,
	newCheckCode,
	nextWaiting,
	noteLength,
	noteProblem,
	orderLine,
	ownerCan,
	refusalOf,
	retrySafe,
	shiftMonth,
	surname
} from './claimDecision';
import type { StaffClaim } from './claims';
import type { StaffOrderRow } from './orders';

const order = (over: Partial<StaffOrderRow> = {}): StaffOrderRow =>
	({
		id: '8b20d4e6-1a2b-4c3d-8e9f-0123456789ab',
		version: 3,
		lifecycle: 'completed',
		raw_lifecycle: 'completed',
		date: '2026-10-17',
		slot: { id: 's', starts_at: '2026-10-17T11:30:00Z', ends_at: '2026-10-17T12:30:00Z' },
		fulfillment_mode: 'collection',
		placed_at: '2026-10-01T10:00:00Z',
		items: [
			{
				line_no: 1,
				variant_id: 'v',
				lot_id: null,
				recipe_version_id: 'r',
				cake_name: 'Chocolate swirl',
				flavour: 'Snickers',
				size_code: 's',
				quantity: 1
			}
		],
		customer_full_name: 'Tom Becker',
		cancel_reason: null,
		production: {
			state: 'started',
			scheduled_at: null,
			started_at: null,
			observed_at: null,
			source: null
		},
		contact_email: 'tom.b@example.com',
		...over
	}) as StaffOrderRow;

describe('months for the search', () => {
	it('shifts and spans months across years', () => {
		expect(shiftMonth('2026-11', 2)).toBe('2027-01');
		expect(shiftMonth('2026-01', -1)).toBe('2025-12');
		expect(monthsSpan('2026-09', '2026-11')).toBe(3);
		expect(monthsSpan('2026-11', '2026-09')).toBe(-1);
	});

	it('starts at their month ±1, or the last 3 months when they gave none', () => {
		expect(defaultMonths('2026-06', '2026-11-06')).toEqual({ from: '2026-05', to: '2026-07' });
		expect(defaultMonths('2026-06-14', '2026-11-06')).toEqual({ from: '2026-05', to: '2026-07' });
		expect(defaultMonths(null, '2026-11-06')).toEqual({ from: '2026-09', to: '2026-11' });
		// A month further back than the options offer is clamped.
		expect(defaultMonths('2020-01', '2026-11-06').from).toBe(monthOptions('2026-11-06')[0]);
	});

	it('covers whole months within the order list’s 93 days', () => {
		expect(monthsRange('2026-09', '2026-11')).toEqual({ from: '2026-09-01', to: '2026-11-30' });
		expect(monthsRange('2026-12', '2027-02')).toEqual({ from: '2026-12-01', to: '2027-02-28' });
		for (const [a, b] of [
			['2026-07', '2026-09'],
			['2026-10', '2026-12'],
			['2027-12', '2028-02']
		]) {
			const r = monthsRange(a, b);
			const days = (Date.parse(r.to) - Date.parse(r.from)) / 86_400_000 + 1;
			expect(days).toBeLessThanOrEqual(93);
		}
	});
});

describe('finding the order (the chef chooses; nothing is suggested)', () => {
	const a = order();
	const b = order({
		id: '8b2a1f03-0000-4000-8000-000000000000',
		customer_full_name: 'Ana Silva',
		slot: { id: 's', starts_at: '2026-10-03T10:00:00Z', ends_at: '2026-10-03T11:00:00Z' }
	});

	it('matches the start of the ID (a full ID works) or part of the name, by handover', () => {
		expect(matchOrders([a, b], '8b2').map((o) => o.customer_full_name)).toEqual([
			'Ana Silva',
			'Tom Becker'
		]);
		expect(matchOrders([a, b], '8B20D4E6-1A2B-4C3D-8E9F-0123456789AB')).toEqual([a]);
		expect(matchOrders([a, b], 'becker')).toEqual([a]);
		expect(matchOrders([a, b], '  ')).toHaveLength(2);
		expect(matchOrders([a, b], 'IG-0613')).toEqual([]);
	});

	it('words the order and finds their surname', () => {
		expect(orderLine(a)).toBe('Chocolate swirl · Sat 17 Oct, 12:30–13:30');
		expect(surname('Tom  Becker ')).toBe('Becker');
		expect(surname(null)).toBeNull();
	});
});

describe('what a wrong approval could do (r04d wording rules)', () => {
	it('knows when the owner could still act, and whether they could move the pickup', () => {
		expect(isUpcoming(order())).toBe(false);
		expect(isUpcoming(order({ lifecycle: 'cancelled' }))).toBe(false);
		expect(isUpcoming(order({ lifecycle: 'confirmed' }))).toBe(true);
		expect(canMovePickup(order({ lifecycle: 'confirmed' }))).toBe(true);
		expect(canMovePickup(order({ lifecycle: 'confirmed', fulfillment_mode: 'gk_delivery' }))).toBe(
			false
		);
		expect(ownerCan(order({ lifecycle: 'ready' }))).toBe(
			'can cancel it and may be able to move its pickup time'
		);
		expect(ownerCan(order({ lifecycle: 'ready', fulfillment_mode: 'gk_delivery' }))).toBe(
			'can cancel it'
		);
	});
});

describe('the contact-email check', () => {
	it('makes a fresh 4-digit code', () => {
		expect(newCheckCode(() => 0)).toBe('1000');
		expect(newCheckCode(() => 0.99999)).toBe('9999');
		expect(newCheckCode()).toMatch(/^[1-9]\d{3}$/);
	});

	it('emails the code to the order’s address, never the request reference', () => {
		const text = checkEmail(order(), '4487');
		expect(text).toContain('4487');
		expect(text).toContain('Chocolate swirl · Sat 17 Oct, 12:30–13:30');
		expect(text).not.toMatch(/9a3f2c71/);
		expect(text).toContain('the request reference shown in your account');
		expect(
			confirmPlaceholder('tom.b@example.com', '4487', {
				id: '9a3f2c71-0000-4000-8000-000000000000'
			})
		).toBe('e.g. Emailed tom.b@example.com with code 4487; reply quoted 4487 and 9a3f2c71');
	});
});

describe('the internal note (one line, ≤500 code points)', () => {
	it('requires "how you confirmed" only when adding from a reference', () => {
		expect(noteProblem('', true)).toBe('Say how you confirmed it before adding.');
		expect(noteProblem('   ', false)).toBeNull();
		expect(noteProblem('Emailed; reply quoted code', true)).toBeNull();
		expect(noteProblem('two\nlines', false)).toBe('Keep the note to one line.');
		expect(noteProblem('x'.repeat(501), false)).toBe('The note is 1 characters too long.');
		expect(noteLength('🎂'.repeat(500))).toBe(500);
		expect(noteProblem('🎂'.repeat(500), false)).toBeNull();
	});
});

describe('refusals', () => {
	const api = (status: number, code: string) => new ApiError(status, { code, message: 'x' });

	it('maps the API’s answers to the sketch’s cases, and says which are safe to retry', () => {
		expect(refusalOf(api(409, 'ORDER_OWNED'))).toBe('order_owned');
		expect(refusalOf(api(409, 'STALE_VERSION'))).toBe('stale');
		expect(refusalOf(api(409, 'CONFLICT'))).toBe('stale');
		expect(refusalOf(api(404, 'NOT_FOUND'))).toBe('stale');
		expect(refusalOf(api(422, 'INVALID_INPUT'))).toBe('not_found_order');
		expect(refusalOf(api(409, 'ACCOUNT_UNAVAILABLE'))).toBe('account_unavailable');
		expect(refusalOf(api(503, 'UNAVAILABLE'))).toBe('paused');
		expect(refusalOf(api(0, 'NETWORK'))).toBe('lost');
		expect(refusalOf(api(500, 'INTERNAL'))).toBe('lost');
		expect(refusalOf(api(401, 'UNAUTHENTICATED'))).toBe('signed_out');
		expect(refusalOf(api(403, 'FORBIDDEN'))).toBe('forbidden');
		expect(refusalOf(new Error('boom'))).toBe('lost');
		expect(retrySafe('lost')).toBe(true);
		expect(retrySafe('paused')).toBe(true);
		expect(retrySafe('order_owned')).toBe(false);
	});
});

describe('next waiting request', () => {
	const c = (id: string, at: string, state = 'pending') =>
		({ id, created_at: at, state }) as StaffClaim;
	it('takes the next one received after this, else any other waiting one', () => {
		const list = [
			c('a', '2026-11-01T10:00:00Z'),
			c('b', '2026-11-02T10:00:00Z'),
			c('d', '2026-11-03T10:00:00Z')
		];
		expect(nextWaiting(list[1], list)?.id).toBe('d');
		expect(nextWaiting(list[2], list)?.id).toBe('a');
		expect(nextWaiting(list[0], [list[0]])).toBeNull();
	});
});
