import { describe, expect, it } from 'vitest';
import { ApiError } from '../api';
import { BAG_KEY } from '../shop/bag';
import { FAVOURITES_KEY } from '../shop/favourites';
import {
	blockedReason,
	clearDevice,
	DELETION_MARKER_KEY,
	DELETION_MARKER_MS,
	deletionFailure,
	deletionMarked,
	deletionStep,
	forgetDeletion,
	freshSignIn,
	markDeletion,
	ordersTickLabel,
	RETENTION,
	signInAt,
	tickErrors,
	TICK_MESSAGE,
	upcomingLine,
	upcomingWarning
} from './privacy';

const A = '1a2b3c4d-1111-4111-8111-111111111111';
const B = '5e6f7a8b-2222-4222-8222-222222222222';
const NOW = Date.parse('2026-11-06T10:00:00Z');

function memory(initial: Record<string, string> = {}) {
	const data = new Map(Object.entries(initial));
	return {
		data,
		getItem: (k: string) => data.get(k) ?? null,
		setItem: (k: string, v: string) => void data.set(k, v),
		removeItem: (k: string) => void data.delete(k)
	};
}

function token(claims: object): string {
	const b64 = (o: object) =>
		btoa(JSON.stringify(o)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
	return `${b64({ alg: 'ES256' })}.${b64(claims)}.sig`;
}

function preview(over: Partial<Parameters<typeof deletionStep>[0]> = {}) {
	return { can_delete: true, blocked: [], upcoming: [], staff_capabilities: [], ...over };
}

describe('retention table', () => {
	it('has the six r02b rows, with payment records still being confirmed', () => {
		expect(RETENTION.map(([what]) => what)).toEqual([
			'Account, profile, favourites and order list',
			'Details of your orders and messages about them (name, email, cake, date)',
			'Custom requests (enquiries)',
			'Photos you send with a request',
			'Payment and refund records',
			'On this device'
		]);
		expect(RETENTION[4][1]).toContain('(being confirmed)');
		expect(RETENTION[0][1]).toContain('a warning 30 days before deleting it and a reminder 7 days');
	});
});

describe('deletion steps and wording', () => {
	it('chooses staff, blocked, warning or review from the preview', () => {
		expect(deletionStep(preview({ staff_capabilities: ['chef'], can_delete: false }))).toBe(
			'staff'
		);
		expect(
			deletionStep(
				preview({ can_delete: false, blocked: [{ order_id: A, reason: 'refund_in_progress' }] })
			)
		).toBe('blocked');
		// A refusal this build can't explain is never shown as deletable.
		expect(deletionStep(preview({ can_delete: false }))).toBe('blocked');
		expect(
			deletionStep(
				preview({ upcoming: [{ order_id: A, date: '2026-11-27', lifecycle: 'confirmed' }] })
			)
		).toBe('warn');
		expect(deletionStep(preview())).toBe('review');
	});

	it('words every blocking reason as the sketch does, and an unknown one safely', () => {
		expect(blockedReason('payment_being_checked')).toBe('we’re still checking a payment');
		expect(blockedReason('paid_awaiting_agreement')).toBe('payment received, date still to agree');
		expect(blockedReason('refund_in_progress')).toBe('a refund is on its way');
		expect(blockedReason('refund_being_checked')).toBe('we’re still checking a refund');
		expect(blockedReason('order_change_in_progress')).toBe('we’re still confirming a change to it');
		expect(blockedReason('payment_recorded')).toContain('payment recorded');
		expect(blockedReason('something_new')).toBe('it still has money to settle');
	});

	it('words upcoming orders and their ticks for one or several', () => {
		expect(upcomingLine({ date: '2026-11-27', lifecycle: 'confirmed' })).toBe(
			'Fri 27 November 2026, confirmed'
		);
		expect(upcomingLine({ date: null, lifecycle: 'in_production' })).toBe('being prepared');
		expect(upcomingLine({ date: '2026-11-28', lifecycle: 'later' })).toBe('Sat 28 November 2026');
		expect(ordersTickLabel([{ order_id: A }])).toBe(
			'I understand my order 1a2b3c4d goes ahead and I’ll arrange anything about it with the chef directly.'
		);
		expect(ordersTickLabel([{ order_id: A }, { order_id: B }])).toBe(
			'I understand my orders 1a2b3c4d and 5e6f7a8b go ahead and I’ll arrange anything about them with the chef directly.'
		);
		expect(upcomingWarning(1)).toContain('the order still goes ahead');
		expect(upcomingWarning(2)).toContain('the orders still go ahead');
	});

	it('requires the ticks that apply', () => {
		expect(tickErrors({ orders: false, undone: false }, false)).toEqual({ undone: TICK_MESSAGE });
		expect(tickErrors({ orders: false, undone: false }, true)).toEqual({
			orders: TICK_MESSAGE,
			undone: TICK_MESSAGE
		});
		expect(tickErrors({ orders: true, undone: true }, true)).toEqual({});
		expect(tickErrors({ orders: false, undone: false }, true, false)).toEqual({
			orders: TICK_MESSAGE
		});
	});
});

describe('fresh sign-in', () => {
	it('reads the latest email-link or Google sign-in from amr, never a password', () => {
		const t = token({
			amr: [
				{ method: 'password', timestamp: 1_800_000_000 },
				{ method: 'otp', timestamp: 1_700_000_000 },
				{ method: 'oauth', timestamp: 1_700_000_100 }
			]
		});
		expect(signInAt(t)).toBe(1_700_000_100_000);
		expect(signInAt(token({ amr: [{ method: 'password', timestamp: 1_800_000_000 }] }))).toBeNull();
		expect(signInAt(token({ amr: [{ method: 'magiclink', timestamp: 5 }] }))).toBe(5000);
		expect(signInAt(token({}))).toBeNull();
		expect(signInAt('not-a-token')).toBeNull();
		expect(signInAt('a.!!!.c')).toBeNull();
		expect(signInAt(undefined)).toBeNull();
	});

	it('allows 10 minutes and nothing from the future', () => {
		expect(freshSignIn(NOW - 9 * 60_000, NOW)).toBe(true);
		expect(freshSignIn(NOW - 10 * 60_000, NOW)).toBe(false);
		expect(freshSignIn(NOW + 5 * 60_000, NOW)).toBe(false);
		expect(freshSignIn(null, NOW)).toBe(false);
	});
});

describe('deletion marker', () => {
	it('holds only an expiry, lasts as long as the link and is removed when expired or damaged', () => {
		const s = memory();
		expect(markDeletion(s, NOW)).toBe(true);
		expect(s.data.get(DELETION_MARKER_KEY)).toBe(String(NOW + DELETION_MARKER_MS));
		expect(deletionMarked(s, NOW + DELETION_MARKER_MS - 1)).toBe(true);
		expect(deletionMarked(s, NOW + DELETION_MARKER_MS)).toBe(false);
		expect(s.data.has(DELETION_MARKER_KEY)).toBe(false);
		s.setItem(DELETION_MARKER_KEY, 'nonsense');
		expect(deletionMarked(s, NOW)).toBe(false);
		s.setItem(DELETION_MARKER_KEY, String(NOW + 10 * DELETION_MARKER_MS));
		expect(deletionMarked(s, NOW)).toBe(false);
		markDeletion(s, NOW);
		forgetDeletion(s);
		expect(deletionMarked(s, NOW)).toBe(false);
		expect(markDeletion(null, NOW)).toBe(false);
		expect(deletionMarked(null, NOW)).toBe(false);
	});
});

describe('clear this device', () => {
	it('removes favourites, the bag and the marker, and nothing else', () => {
		const local = memory({
			[FAVOURITES_KEY]: '{}',
			[DELETION_MARKER_KEY]: '1',
			'gk-auth': 'session'
		});
		const tab = memory({ [BAG_KEY]: '{}', 'gk-order-capability:x': 'proof' });
		expect(clearDevice(local, tab)).toBe(true);
		expect([...local.data.keys()]).toEqual(['gk-auth']);
		expect([...tab.data.keys()]).toEqual(['gk-order-capability:x']);
	});

	it('reports a store it could not reach or that refused', () => {
		const refusing = {
			...memory(),
			removeItem: () => {
				throw new Error('denied');
			}
		};
		expect(clearDevice(null, memory())).toBe(false);
		expect(clearDevice(memory(), refusing)).toBe(false);
	});
});

describe('deletion failures', () => {
	const err = (status: number, code: string, retryable = false) =>
		new ApiError(status, { code, message: 'x', request_id: 'r', retryable });

	it('tells a fresh sign-in from an ended session and sorts the refusals', () => {
		expect(deletionFailure(err(401, 'REAUTHENTICATION_REQUIRED'))).toBe('reauth');
		expect(deletionFailure(err(401, 'SESSION_ENDED'))).toBe('ended');
		expect(deletionFailure(err(409, 'ACCOUNT_DELETION_BLOCKED'))).toBe('blocked');
		expect(deletionFailure(err(409, 'STAFF_ACCOUNT'))).toBe('staff');
		expect(deletionFailure(err(409, 'DELETION_PREVIEW_STALE'))).toBe('stale');
		expect(deletionFailure(err(422, 'INVALID_INPUT'))).toBe('stale');
		expect(deletionFailure(err(403, 'ACCOUNT_DELETED'))).toBe('deleted');
		expect(deletionFailure(err(403, 'ACCOUNT_DISABLED'))).toBe('disabled');
		expect(deletionFailure(err(503, 'UNAVAILABLE', true))).toBe('retry');
		expect(deletionFailure(err(0, 'NETWORK', true))).toBe('retry');
		expect(deletionFailure(err(409, 'KEY_REUSED'))).toBe('failed');
		expect(deletionFailure(new Error('boom'))).toBe('failed');
	});
});
