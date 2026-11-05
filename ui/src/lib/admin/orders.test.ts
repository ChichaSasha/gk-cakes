import { describe, expect, it } from 'vitest';
import type { StaffApi } from './catalogue';
import {
	cakeText,
	isManagerCase,
	isReleasedUnpaid,
	matchesSearch,
	matchesStatus,
	moneyText,
	periods,
	productionText,
	shortDay,
	staffOrders,
	statusFilters,
	statusOf,
	viewOf,
	type StaffOrderRow
} from './orders';

const row = (over: Partial<StaffOrderRow> = {}): StaffOrderRow => ({
	id: 'a1b2c3d4-0000-4000-8000-000000000001',
	version: 3,
	lifecycle: 'confirmed',
	raw_lifecycle: 'confirmed',
	date: '2026-10-29',
	slot: { id: 's', starts_at: '2026-10-29T12:00:00Z', ends_at: '2026-10-29T13:00:00Z' },
	fulfillment_mode: 'collection',
	placed_at: '2026-10-20T10:00:00Z',
	items: [
		{
			line_no: 1,
			variant_id: 'v',
			lot_id: null,
			recipe_version_id: 'r',
			cake_name: 'Pink piping',
			flavour: 'Vanilla Raspberry',
			size_code: 's',
			quantity: 1
		}
	],
	customer_full_name: 'Mia Example',
	cancel_reason: null,
	production: {
		state: 'scheduled',
		scheduled_at: '2026-10-24T10:00:00Z',
		started_at: null,
		observed_at: null,
		source: null
	},
	attention: [],
	needs_attention: false,
	total_minor: 9000,
	currency: 'GBP',
	payment_requirement: 'full',
	payment_state: 'succeeded',
	refund: null,
	...over
});

describe('staffOrders', () => {
	it('reads every page of the range with the staff token, 100 at a time', async () => {
		const urls: string[] = [];
		const replies = [{ items: [row()], next_cursor: 'n2' }, { items: [row({ id: 'b' })] }];
		const api: StaffApi = {
			baseUrl: 'https://api.example.com',
			token: 't',
			fetch: (async (input: RequestInfo | URL, init?: RequestInit) => {
				urls.push(String(input));
				expect((init?.headers as Record<string, string>).Authorization).toBe('Bearer t');
				return new Response(JSON.stringify(replies.shift()));
			}) as typeof fetch
		};
		const got = await staffOrders(api, { from: '2026-10-22', to: '2026-11-30' });
		expect(got.map((o) => o.id)).toEqual([row().id, 'b']);
		expect(urls).toEqual([
			'https://api.example.com/api/v1/admin/orders?from=2026-10-22&to=2026-11-30&limit=100',
			'https://api.example.com/api/v1/admin/orders?from=2026-10-22&to=2026-11-30&limit=100&cursor=n2'
		]);
		await expect(staffOrders(api, { from: '22/10', to: '2026-11-30' })).rejects.toThrow(
			'date range'
		);
	});
});

describe('roles and periods', () => {
	it('uses the most privileged capability, as the API does', () => {
		expect(viewOf(['fulfillment', 'chef'])).toBe('chef');
		expect(viewOf(['fulfillment', 'technical_support'])).toBe('technical_support');
		expect(viewOf(['fulfillment'])).toBe('fulfillment');
		expect(viewOf([])).toBeNull();
		expect(statusFilters('fulfillment').map(([k]) => k)).not.toContain('attention');
		expect(statusFilters('chef').map(([k]) => k)).toContain('attention');
	});

	it('offers the sketch periods within the API’s 93-day range', () => {
		const p = periods('2026-10-22'); // a Thursday
		expect(p.upcoming).toMatchObject({ from: '2026-10-22', to: '2027-01-22' });
		expect(p.this).toEqual({
			label: 'This week (Mon 19 – Sun 25 Oct)',
			from: '2026-10-19',
			to: '2026-10-25'
		});
		expect(p.next).toEqual({
			label: 'Next week (Mon 26 Oct – Sun 1 Nov)',
			from: '2026-10-26',
			to: '2026-11-01'
		});
		expect(p.past).toMatchObject({ from: '2026-07-21', to: '2026-10-21' });
		expect(periods('2026-10-25').this.from).toBe('2026-10-19'); // Sunday belongs to the week before
		expect(shortDay('2026-10-29')).toBe('Thu 29 Oct');
	});
});

describe('filters and search', () => {
	it('maps the status chips to the visible lifecycle and attention', () => {
		const pay = row({ lifecycle: 'awaiting_payment' });
		const confirming = row({ lifecycle: 'confirmation_pending' });
		const late = row({ lifecycle: 'paid_awaiting_agreement' });
		const cancelling = row({ lifecycle: 'cancellation_pending' });
		const flagged = row({ needs_attention: true, attention: ['refund_needs_attention'] });
		expect(matchesStatus(pay, 'payment') && matchesStatus(confirming, 'payment')).toBe(true);
		expect(matchesStatus(late, 'late')).toBe(true);
		expect(matchesStatus(cancelling, 'cancelled')).toBe(true);
		expect(matchesStatus(flagged, 'attention')).toBe(true);
		expect(matchesStatus(row(), 'attention')).toBe(false);
		// Paid late is the chef's own decision; an unknown payment is still being checked.
		for (const flag of ['paid_awaiting_agreement', 'payment_unknown']) {
			expect(matchesStatus(row({ needs_attention: true, attention: [flag] }), 'attention')).toBe(
				false
			);
		}
		for (const flag of ['refund_needs_attention', 'payment_mismatch', 'refund_quarantine']) {
			expect(isManagerCase(row({ needs_attention: true, attention: [flag] }))).toBe(true);
		}
		expect(matchesStatus(row({ lifecycle: 'in_production' }), 'production')).toBe(true);
		expect(matchesStatus(row({ lifecycle: 'ready' }), 'ready')).toBe(true);
		expect(matchesStatus(row(), 'confirmed')).toBe(true);
	});

	it('searches the loaded rows by name or the start of the order ID', () => {
		expect(matchesSearch(row(), ' mia ')).toBe(true);
		expect(matchesSearch(row(), 'A1B2')).toBe(true);
		expect(matchesSearch(row(), 'c3d4')).toBe(false);
		expect(matchesSearch(row(), '')).toBe(true);
	});
});

describe('role-safe wording, free orders and released orders (review fixes)', () => {
	it('gives technical support only what its rows can tell', () => {
		const supportRow = (over: Partial<StaffOrderRow>) =>
			row({ payment_state: undefined, refund: undefined, total_minor: undefined, ...over });
		expect(statusOf(supportRow({ lifecycle: 'awaiting_payment' }), 'technical_support').text).toBe(
			'Waiting for payment'
		);
		expect(
			statusOf(
				supportRow({ lifecycle: 'cancelled', cancel_reason: 'payment_not_completed' }),
				'technical_support'
			)
		).toMatchObject({ text: 'Released', released: false });
	});

	it('words free orders without payment', () => {
		const free = (over: Partial<StaffOrderRow>) =>
			row({ payment_requirement: 'none', total_minor: 0, payment_state: null, ...over });
		expect(statusOf(free({ lifecycle: 'awaiting_payment' }), 'chef').text).toBe(
			'Free · not confirmed yet'
		);
		expect(statusOf(free({ lifecycle: 'confirmation_pending' }), 'chef').text).toBe(
			'Free · confirming the order'
		);
		expect(moneyText(free({ lifecycle: 'confirmation_pending' }))).toBe('Free');
	});

	it('marks released unpaid orders (hidden by default, Sasha r03 Q5) but not returned ones', () => {
		const released = row({
			lifecycle: 'cancelled',
			cancel_reason: 'payment_not_completed',
			payment_state: 'cancelled'
		});
		expect(isReleasedUnpaid(released, 'chef')).toBe(true);
		expect(
			isReleasedUnpaid(
				{ ...released, refund: { state: 'pending', target_minor: 9000, succeeded_minor: 0 } },
				'chef'
			)
		).toBe(false);
		expect(isReleasedUnpaid(row(), 'chef')).toBe(false);
	});
});

describe('status, production and money (sketch wording)', () => {
	const now = new Date('2026-10-22T09:45:00Z');

	it('words each stage, with role-dependent late-payment wording', () => {
		expect(statusOf(row(), 'chef').text).toBe('Paid · automatic start later');
		expect(
			statusOf(
				row({
					lifecycle: 'in_production',
					production: { ...row().production, state: 'started', source: 'timer' }
				}),
				'chef'
			)
		).toMatchObject({
			stage: 'preparing',
			text: 'In production (automatic) · not started by you'
		});
		expect(
			statusOf(
				row({
					lifecycle: 'in_production',
					production: { ...row().production, state: 'started', source: 'manual' }
				}),
				'chef'
			).text
		).toBe('You started making');
		const late = row({ lifecycle: 'paid_awaiting_agreement' });
		expect(statusOf(late, 'chef').text).toBe('Paid late · your decision');
		expect(statusOf(late, 'fulfillment').text).toBe('Paid late · Elina decides');
		expect(statusOf(row({ lifecycle: 'ready' }), 'chef').text).toBe('Ready for handover');
		expect(statusOf(row({ lifecycle: 'completed' }), 'chef').text).toBe('Handed over');
		expect(statusOf(row({ lifecycle: 'cancellation_pending' }), 'chef').text).toBe(
			'Cancelling · not confirmed yet'
		);
	});

	it('words waiting, unknown and mismatching payments', () => {
		const waiting = (over: Partial<StaffOrderRow>) =>
			statusOf(row({ lifecycle: 'awaiting_payment', ...over }), 'chef').text;
		expect(waiting({ payment_state: null })).toBe('Waiting for payment · link sent');
		expect(waiting({ payment_state: 'pending' })).toBe('Waiting for payment · in progress');
		expect(waiting({ payment_state: 'pending', attention: ['payment_unknown'] })).toBe(
			'Payment result unknown'
		);
		expect(waiting({ attention: ['payment_mismatch'] })).toBe('Payment doesn’t match · held');
		// Technical support has no payment_state: the plain stage.
		expect(
			statusOf(
				row({ lifecycle: 'awaiting_payment', payment_state: undefined }),
				'technical_support'
			).text
		).toBe('Waiting for payment');
	});

	it('tells released unpaid orders from released orders whose money is returned', () => {
		const released = row({
			lifecycle: 'cancelled',
			cancel_reason: 'payment_not_completed',
			payment_state: 'cancelled'
		});
		expect(statusOf(released, 'chef')).toMatchObject({
			text: 'Not paid · released',
			released: true
		});
		const returned = {
			...released,
			refund: { state: 'pending', target_minor: 9000, succeeded_minor: 0 }
		};
		expect(statusOf(returned, 'chef')).toMatchObject({
			text: 'Cancelled · full return',
			released: false
		});
		expect(
			statusOf(
				row({ lifecycle: 'cancelled', cancel_reason: 'free_not_confirmed', payment_state: null }),
				'chef'
			).text
		).toBe('Free order not confirmed · released');
	});

	it('words the production column', () => {
		expect(productionText(row(), 'chef', now)).toBe('Automatic from Sat 24 Oct');
		expect(productionText(row(), 'chef', new Date('2026-10-25T00:00:00Z'))).toBe(
			'Automatic since Sat 24 Oct'
		);
		const started = (source: string) =>
			row({
				lifecycle: 'in_production',
				production: {
					state: 'started',
					scheduled_at: null,
					started_at: '2026-10-20T08:00:00Z',
					observed_at: null,
					source
				}
			});
		expect(productionText(started('manual'), 'chef', now)).toBe('Started Tue 20 Oct');
		expect(productionText(started('timer'), 'chef', now)).toBe('Automatic since Tue 20 Oct');
		const unscheduled = {
			state: 'not_scheduled',
			scheduled_at: null,
			started_at: null,
			observed_at: null,
			source: null
		};
		expect(
			productionText(row({ lifecycle: 'awaiting_payment', production: unscheduled }), 'chef', now)
		).toBe('After payment');
		expect(
			productionText(
				row({ lifecycle: 'paid_awaiting_agreement', production: unscheduled }),
				'chef',
				now
			)
		).toBe('Held · your decision');
		expect(
			productionText(
				row({ lifecycle: 'paid_awaiting_agreement', production: unscheduled }),
				'technical_support',
				now
			)
		).toBe('Held · Elina decides');
		expect(productionText(row({ lifecycle: 'cancelled' }), 'chef', now)).toBe('—');
	});

	it('words the money column from the server’s amounts only', () => {
		expect(moneyText(row())).toBe('£90');
		expect(moneyText(row({ lifecycle: 'awaiting_payment', payment_state: null }))).toBe(
			'Link sent · not paid'
		);
		expect(moneyText(row({ lifecycle: 'awaiting_payment', payment_state: 'unknown' }))).toBe(
			'Payment result unknown'
		);
		expect(moneyText(row({ lifecycle: 'confirmation_pending' }))).toBe('£90 received · confirming');
		const cancelled = (refund: StaffOrderRow['refund']) =>
			moneyText(row({ lifecycle: 'cancelled', refund }));
		expect(cancelled(null)).toBe('Nothing taken');
		expect(cancelled({ state: 'pending', target_minor: 4500, succeeded_minor: 0 })).toBe(
			'Refund £45 · not confirmed'
		);
		expect(cancelled({ state: 'needs_attention', target_minor: 4500, succeeded_minor: 0 })).toBe(
			'Refund needs a manager'
		);
		expect(cancelled({ state: 'partly_returned', target_minor: 9000, succeeded_minor: 3500 })).toBe(
			'Partly refunded · £35 of £90'
		);
		expect(cancelled({ state: 'requested', target_minor: 4500, succeeded_minor: 0 })).toBe(
			'Refund £45 owed · nothing sent yet'
		);
		expect(cancelled({ state: 'returned', target_minor: 4550, succeeded_minor: 4550 })).toBe(
			'Refunded £45.50'
		);
		expect(moneyText(row({ payment_requirement: 'none', total_minor: 0 }))).toBe('Free');
	});

	it('lists each line’s cake, with quantities', () => {
		const two = row({
			items: [
				{ ...row().items[0], quantity: 2 },
				{ ...row().items[0], line_no: 2, cake_name: 'Garden bloom' }
			]
		});
		expect(cakeText(two)).toBe('2 × Pink piping, Garden bloom');
	});
});
