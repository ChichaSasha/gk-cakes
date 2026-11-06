import { describe, expect, it } from 'vitest';
import { orderTimeline, refundSteps, type LaneStep } from './timeline';
import type { Delivery, DeliveryFailure, Order, Refund, RefundPart } from './types';

// Fixture as the approved sketch (FX in poc/r02.js): Pink piping £90,
// Thu 22 October 2026 11:00–12:00 London (BST, so 10:00Z), deadline 11:00
// on Sat 17 October, GK delivery £120.
const NOW = new Date('2026-10-22T11:00:00Z'); // the handover day, 12:00 London
const NEXT_DAY = new Date('2026-10-23T09:00:00Z');
const BEFORE_DEADLINE = new Date('2026-10-14T09:00:00Z'); // the full refund still ahead

function order(over: Partial<Order> = {}): Order {
	return {
		id: '0b6c1b8e-6a0e-4c47-9a4e-2f1a7a3e5d10',
		version: 4,
		lifecycle: 'confirmed',
		cancel_reason: null,
		cancelled_at: null,
		payment_requirement: 'full',
		placed_at: '2026-10-06T13:00:00Z',
		date: '2026-10-22',
		fulfillment_mode: 'collection',
		slot: { id: 's11', starts_at: '2026-10-22T10:00:00Z', ends_at: '2026-10-22T11:00:00Z' },
		full_refund_deadline_at: '2026-10-17T10:00:00Z',
		customer_full_name: 'Alex Morgan',
		different_payer_name: null,
		contact_email: 'alex@example.com',
		subtotal_minor: 9000,
		discount_minor: 0,
		total_minor: 9000,
		currency: 'GBP',
		paid_at: '2026-10-06T13:05:00Z',
		free_confirmed_at: null,
		free_confirmation: null,
		payment: null,
		items: [],
		fulfillment: { state: 'awaiting_handover', at: null, pending: null },
		...over
	};
}

const paidDelivery = (over: Partial<Delivery> = {}): Delivery => ({
	state: 'paid',
	quote: {
		id: 'q1',
		amount_minor: 12000,
		currency: 'GBP',
		quoted_at: '2026-10-08T09:00:00Z',
		pay_by: '2026-10-20T10:00:00Z'
	},
	payable: false,
	paid_minor: 12000,
	payment: null,
	...over
});
const gk = (over: Partial<Order> = {}) =>
	order({ fulfillment_mode: 'gk_delivery', delivery: paidDelivery(), ...over });

const part = (kind: string, over: Partial<RefundPart> = {}): RefundPart => ({
	kind,
	target_minor: 4500,
	succeeded_minor: 0,
	in_progress_minor: 4500,
	pending_chef_minor: 0,
	...over
});
const refund = (over: Partial<Refund> = {}): Refund => ({
	state: 'requested',
	target_minor: 4500,
	succeeded_minor: 0,
	in_progress_minor: 4500,
	currency: 'GBP',
	requested_at: '2026-10-22T12:10:00Z',
	reason: 'no_show',
	policy_rate: 50,
	delivery_pending_minor: 0,
	parts: [part('cake')],
	...over
});
const failure = (over: Partial<DeliveryFailure> = {}): DeliveryFailure => ({
	cause: 'our_side',
	at: '2026-10-22T10:40:00Z',
	state: 'recorded',
	open: true,
	pending: false,
	choices: ['redelivery', 'collection'],
	times: [],
	delivery_paid_minor: 12000,
	chosen: null,
	...over
});

/** Each step as "label | detail | state" (easy to read in a failure). */
const rows = (steps: LaneStep[]) =>
	steps.map((s) => [s.label, s.detail ?? '', s.state].join(' | '));
const lanes = (o: Order, now = NOW) => {
	const t = orderTimeline(o, now);
	if (!t) throw new Error('no timeline');
	return { money: rows(t.money), cake: rows(t.cake) };
};

describe('orderTimeline: which orders have one', () => {
	it('accepted orders only', () => {
		for (const lifecycle of ['confirmed', 'in_production', 'ready', 'completed'] as const) {
			expect(orderTimeline(order({ lifecycle }))).toBeDefined();
		}
		for (const lifecycle of [
			'awaiting_payment',
			'confirmation_pending',
			'paid_awaiting_agreement',
			'cancellation_pending',
			'cancelled'
		] as const) {
			expect(orderTimeline(order({ lifecycle }))).toBeUndefined();
		}
	});

	it('not while the delivery quote or the Q5 choice is open (states 14, 15)', () => {
		for (const state of ['quoted', 'payment_checking', 'pay_by_passed']) {
			expect(
				orderTimeline(gk({ delivery: paidDelivery({ state, paid_minor: undefined }) }))
			).toBeUndefined();
		}
		const q5 = { open: true, reason: 'not_possible', pending: false };
		expect(orderTimeline(gk({ delivery_choice: q5 }))).toBeUndefined();
		expect(
			orderTimeline(gk({ delivery_choice: { ...q5, open: false, pending: true } }))
		).toBeUndefined();
	});
});

describe('orderTimeline: confirmed · being made (collection)', () => {
	it('confirmed: paid, the refund window, the cake still to be made', () => {
		expect(lanes(order(), BEFORE_DEADLINE)).toEqual({
			money: [
				'Paid £90 | Tue 6 October, 14:05 | done',
				'Full refund until 11:00 on Sat 17 October | From then on 50% if you cancel; 100% if we cancel | upcoming'
			],
			cake: [
				'Order confirmed |  | done',
				'Being made |  | upcoming',
				'Ready for collection | The chef will share the exact collection address before your collection | upcoming',
				'Collected | Thu 22 October, 11:00–12:00 · St Katharine Docks | upcoming'
			]
		});
	});

	it('in production: being made is the current (ringed) step', () => {
		expect(lanes(order({ lifecycle: 'in_production' })).cake.slice(1, 2)).toEqual([
			'Being made |  | current'
		]);
	});

	it('ready: prepared, ready for collection is current', () => {
		expect(lanes(order({ lifecycle: 'ready' })).cake.slice(1)).toEqual([
			'Prepared |  | done',
			'Ready for collection | Thu 22 October | current',
			'Collected | Thu 22 October, 11:00–12:00 · St Katharine Docks | upcoming'
		]);
	});

	it('never says paid before the API does', () => {
		expect(lanes(order({ paid_at: null })).money[0]).toBe('Paid £90 |  | upcoming');
	});

	it('a free order: nothing to pay, no refund window', () => {
		expect(
			lanes(
				order({
					payment_requirement: 'none',
					total_minor: 0,
					paid_at: null,
					free_confirmed_at: '2026-10-06T13:05:00Z'
				})
			).money
		).toEqual(['Free order · nothing to pay | Tue 6 October, 14:05 | done']);
	});
});

describe('orderTimeline: collected (Q20) and not collected (Q19)', () => {
	const done = { state: 'completed', at: '2026-10-22T10:20:00Z', pending: null };

	it('collected: the cake lane ends ticked with the handover time', () => {
		const t = lanes(order({ lifecycle: 'completed', fulfillment: done }));
		expect(t.money).toEqual(['Paid £90 | Tue 6 October, 14:05 | done']);
		expect(t.cake.slice(1)).toEqual([
			'Prepared |  | done',
			'Ready for collection | Thu 22 October | done',
			'Collected | Thu 22 October, 11:20 | done'
		]);
	});

	it('a refund agreed with the chef after collection keeps "Collected" in the cake lane', () => {
		const t = lanes(
			order({
				lifecycle: 'completed',
				fulfillment: done,
				refund: refund({
					target_minor: 3000,
					in_progress_minor: 3000,
					reason: 'chef_exception',
					policy_rate: null,
					requested_at: '2026-10-23T09:00:00Z',
					parts: [part('cake', { target_minor: 3000, in_progress_minor: 3000 })]
				})
			})
		);
		expect(t.money).toEqual([
			'Paid £90 | Tue 6 October, 14:05 | done',
			'Refund of £30 requested | Amount agreed with the chef on Fri 23 October | current',
			'Return to your payment method |  | upcoming'
		]);
		expect(t.cake.at(-1)).toBe('Collected | Thu 22 October, 11:20 | done');
	});

	it('not collected: requested automatically when the chef recorded it, a problem in the cake lane', () => {
		const t = lanes(
			order({
				lifecycle: 'completed',
				fulfillment: { state: 'no_show', at: '2026-10-22T11:05:00Z', pending: null },
				refund: refund()
			})
		);
		expect(t.money).toEqual([
			'Paid £90 | Tue 6 October, 14:05 | done',
			'Refund of £45 requested | Automatically · 50% for a cake that wasn’t collected | current',
			'Return to your payment method |  | upcoming'
		]);
		expect(t.cake.at(-1)).toBe(
			'Not collected | The collection time passed on Thu 22 October | problem'
		);
	});

	it('a handover still being secured stays ready (K11: the API shows ready)', () => {
		const t = lanes(
			order({
				lifecycle: 'ready',
				fulfillment: { state: 'awaiting_handover', at: null, pending: 'handover' }
			})
		);
		expect(t.cake.at(-1)).toBe(
			'Collected | Thu 22 October, 11:00–12:00 · St Katharine Docks | upcoming'
		);
	});
});

describe('orderTimeline: the refund follows the API refund state only', () => {
	const completed = (r: Partial<Refund>) =>
		order({
			lifecycle: 'completed',
			fulfillment: { state: 'no_show', at: null, pending: null },
			refund: refund(r)
		});

	it.each([
		['requested', ['… | current', 'Return to your payment method |  | upcoming']],
		[
			'pending',
			[
				'… | done',
				'Return to your payment method | Sent to our payment provider · waiting for them | current'
			]
		],
		[
			'needs_attention',
			['… | done', 'Return to your payment method | Needs a check by our team | problem']
		],
		['returned', ['… | done', 'Returned to your payment method |  | done']]
	] as const)('%s', (state, expected) => {
		const money = lanes(
			completed({ state, succeeded_minor: state === 'returned' ? 4500 : 0 })
		).money;
		const requested = money[1].split(' | ').at(-1);
		expect([`… | ${requested}`, money[2]]).toEqual(expected);
	});

	it('partly returned: the API’s own amounts, no arithmetic', () => {
		const money = lanes(
			completed({
				state: 'partly_returned',
				target_minor: 10500,
				succeeded_minor: 4500,
				parts: [
					part('cake', { succeeded_minor: 4500, in_progress_minor: 0 }),
					part('delivery', { target_minor: 6000, in_progress_minor: 0, pending_chef_minor: 6000 })
				]
			})
		).money;
		// Each part from its own amounts: the cake back is done, the chef's
		// share not yet sent is current (one current step: the return waits).
		expect(money.slice(1)).toEqual([
			'Refund of £45 requested | Automatically · 50% for a cake that wasn’t collected | done',
			'Delivery refund of £60 requested | Sent separately by the chef · waiting for the chef | current',
			'Return to your payment method | £45 of £105 returned so far | upcoming'
		]);
	});

	it('a stray delivery payment is its own step', () => {
		const steps = rows(
			refundSteps(
				order({
					refund: refund({
						reason: 'customer_cancelled',
						parts: [part('cake'), part('stray', { target_minor: 12000, in_progress_minor: 12000 })]
					})
				})
			)
		);
		expect(steps).toEqual([
			'Refund of £45 requested |  | current',
			'Refund of £120 requested | A delivery payment that couldn’t be used · returned in full | current',
			'Return to your payment method |  | upcoming'
		]);
	});

	it('an older API without parts: the whole refund and the chef’s delivery share', () => {
		const steps = rows(
			refundSteps(
				order({
					refund: refund({ target_minor: 10500, delivery_pending_minor: 6000, parts: undefined })
				})
			)
		);
		expect(steps).toEqual([
			'Refund of £105 requested | Automatically · 50% for a cake that wasn’t collected | current',
			'Delivery refund · £60 | Sent separately by the chef · waiting for the chef | current',
			'Return to your payment method |  | upcoming'
		]);
	});

	it('malformed parts are not trusted (falls back to the block)', () => {
		const steps = rows(
			refundSteps(order({ refund: refund({ parts: [{ ...part('cake'), target_minor: 1.5 }] }) }))
		);
		expect(steps[0]).toBe(
			'Refund of £45 requested | Automatically · 50% for a cake that wasn’t collected | current'
		);
	});
});

describe('orderTimeline: GK delivery', () => {
	it('being made: cakes and delivery paid separately, delivery confirmed by the chef', () => {
		expect(lanes(gk({ lifecycle: 'in_production' }), BEFORE_DEADLINE)).toEqual({
			money: [
				'Cakes paid · £90 | Tue 6 October, 14:05 | done',
				'Delivery paid · £120 |  | done',
				'Full refund until 11:00 on Sat 17 October | Covers cakes and delivery; from then on 50% | upcoming'
			],
			cake: [
				'Order confirmed |  | done',
				'Delivery confirmed by the chef | Thu 22 October, 11:00–12:00 | done',
				'Being made |  | current',
				'Out for delivery | On Thu 22 October | upcoming',
				'Delivered | You receive it at your address | upcoming'
			]
		});
	});

	it('before the quote: the delivery quote still to come, delivery not confirmed', () => {
		const t = lanes(
			gk({ delivery: { state: 'awaiting_quote', quote: null, payable: false, payment: null } }),
			BEFORE_DEADLINE
		);
		expect(t.money[1]).toBe(
			'Delivery quote | Appears on this page once the chef has confirmed your delivery | upcoming'
		);
		expect(t.money[2]).toContain('From then on 50% if you cancel; 100% if we cancel');
		expect(t.cake[1]).toBe('Delivery confirmed by the chef |  | upcoming');
	});

	it('a delivery the chef waived', () => {
		const t = lanes(
			gk({
				delivery: paidDelivery({
					state: 'waived',
					paid_minor: undefined,
					waived: { at: '2026-10-09T09:00:00Z' }
				})
			})
		);
		expect(t.money[1]).toBe('Delivery · no charge | Waived by the chef | done');
		expect(t.cake[1]).toContain('| done');
	});

	it('out for delivery, then delivered', () => {
		const out = lanes(
			gk({
				lifecycle: 'ready',
				fulfillment: { state: 'out_for_delivery', at: '2026-10-22T09:15:00Z', pending: null }
			})
		);
		expect(out.cake.slice(2)).toEqual([
			'Prepared |  | done',
			'Out for delivery | Thu 22 October, 10:15 | current',
			'Delivered | You receive it at your address | upcoming'
		]);
		const done = lanes(
			gk({
				lifecycle: 'completed',
				fulfillment: { state: 'completed', at: '2026-10-22T10:05:00Z', pending: null }
			})
		);
		expect(done.cake.slice(3)).toEqual([
			'Out for delivery |  | done',
			'Delivered | Thu 22 October, 11:05 | done'
		]);
		expect(done.money).toHaveLength(2);
	});
});

describe('orderTimeline: delivery failed · our side (Q17)', () => {
	const exception = { state: 'exception', at: '2026-10-22T10:40:00Z', pending: null };

	it('choose: the cake lane ends with the problem; the delivery refund if you collect', () => {
		expect(
			lanes(gk({ lifecycle: 'ready', fulfillment: exception, delivery_failure: failure() }))
		).toEqual({
			money: [
				'Cakes paid · £90 | Tue 6 October, 14:05 | done',
				'Delivery paid · £120 |  | done',
				'Delivery refund | 100% if you collect instead | upcoming'
			],
			cake: [
				'Order confirmed |  | done',
				'Delivery confirmed by the chef |  | done',
				'Prepared |  | done',
				'Out for delivery |  | done',
				'Delivery couldn’t be completed | Thu 22 October, 11:40 | problem'
			]
		});
	});

	it('no collection time left: no delivery refund row', () => {
		const t = lanes(
			gk({
				lifecycle: 'ready',
				fulfillment: exception,
				delivery_failure: failure({ choices: ['redelivery'] })
			})
		);
		expect(t.money).toHaveLength(2);
	});

	it('collection chosen: the delivery refund requested, collection today is current', () => {
		const t = lanes(
			order({
				lifecycle: 'ready',
				slot: { id: 's14', starts_at: '2026-10-22T13:00:00Z', ends_at: '2026-10-22T14:00:00Z' },
				delivery: paidDelivery({ state: 'cancelled_by_choice' }),
				fulfillment: { state: 'awaiting_handover', at: null, pending: null },
				delivery_failure: failure({
					open: false,
					choices: [],
					chosen: {
						choice: 'collection',
						state: 'chosen',
						refusal_reason: null,
						requested_at: '2026-10-22T10:50:00Z',
						to: null,
						delivery_refund_minor: 12000
					}
				}),
				refund: refund({
					target_minor: 12000,
					in_progress_minor: 12000,
					reason: 'delivery_failure',
					policy_rate: null,
					parts: [part('delivery', { target_minor: 12000, in_progress_minor: 12000 })]
				})
			})
		);
		expect(t.money).toEqual([
			'Cakes paid · £90 | Tue 6 October, 14:05 | done',
			'Delivery paid · £120 |  | done',
			'Delivery refund of £120 requested | 100% of the delivery · sent by the chef | current',
			'Return to your payment method |  | upcoming'
		]);
		expect(t.cake.slice(-2)).toEqual([
			'Delivery couldn’t be completed | Thu 22 October, 11:40 | problem',
			'Collection | Today, 14:00–15:00 · St Katharine Docks | current'
		]);
		const later = orderTimeline(
			order({
				lifecycle: 'ready',
				delivery_failure: failure({
					open: false,
					chosen: {
						choice: 'collection',
						state: 'chosen',
						refusal_reason: null,
						requested_at: '2026-10-22T10:50:00Z',
						to: null
					}
				})
			}),
			NEXT_DAY
		);
		expect(later?.cake.at(-1)?.detail).toBe('Thu 22 October, 11:00–12:00 · St Katharine Docks');
	});

	it('redelivery chosen: a new time to be confirmed, then the time the chef set', () => {
		const chosen = {
			choice: 'redelivery',
			state: 'chosen',
			refusal_reason: null,
			requested_at: '2026-10-22T10:50:00Z',
			to: null
		};
		const base = { lifecycle: 'ready' as const, fulfillment: exception };
		const t = lanes(
			gk({ ...base, delivery_failure: failure({ open: false, choices: [], chosen }) })
		);
		expect(t.money).toHaveLength(2);
		expect(t.cake.slice(-2)).toEqual([
			'Redelivery | New time to be confirmed by the chef | current',
			'Delivered | You receive it at your address | upcoming'
		]);
		const set = lanes(
			gk({
				...base,
				delivery_failure: failure({ open: false, choices: [], chosen, redelivery_time_set: true })
			})
		);
		expect(set.cake.at(-2)).toBe('Redelivery | Thu 22 October, 11:00–12:00 | current');
	});

	it('a choice not yet secured shows no result (K11)', () => {
		const t = lanes(
			gk({
				lifecycle: 'ready',
				fulfillment: exception,
				delivery_failure: failure({
					open: false,
					pending: true,
					chosen: {
						choice: 'collection',
						state: 'processing',
						refusal_reason: null,
						requested_at: '2026-10-22T10:50:00Z',
						to: null
					}
				})
			})
		);
		expect(t.cake.at(-1)).toBe('Delivery couldn’t be completed | Thu 22 October, 11:40 | problem');
	});
});

describe('orderTimeline: delivery failed · nobody available (Q18)', () => {
	it('50% of the cake requested automatically, the delivery payment kept', () => {
		const t = lanes(
			gk({
				lifecycle: 'completed',
				fulfillment: { state: 'no_show', at: '2026-10-22T10:25:00Z', pending: null },
				delivery_failure: failure({
					cause: 'nobody_available',
					at: '2026-10-22T10:25:00Z',
					state: 'no_show',
					open: false,
					choices: [],
					cake_refund_minor: 4500,
					delivery_kept_minor: 12000
				}),
				refund: refund()
			})
		);
		expect(t.money).toEqual([
			'Cakes paid · £90 | Tue 6 October, 14:05 | done',
			'Delivery paid · £120 |  | done',
			'Refund of £45 requested | Automatically · 50% of the cake price | current',
			'Return to your payment method |  | upcoming',
			'Delivery payment kept | The delivery was attempted | upcoming'
		]);
		expect(t.cake.slice(-2)).toEqual([
			'Out for delivery |  | done',
			'Nobody available at the address | Thu 22 October, 11:25 | problem'
		]);
	});

	it('while it is being recorded: no money step yet', () => {
		const t = lanes(
			gk({
				lifecycle: 'ready',
				fulfillment: { state: 'exception', at: null, pending: 'no_show' },
				delivery_failure: failure({
					cause: 'nobody_available',
					state: 'processing',
					open: false,
					choices: []
				})
			})
		);
		expect(t.money).toHaveLength(2);
	});
});

describe('orderTimeline: the full-refund window after its deadline (review finding 1)', () => {
	const window = (o: Order, now: Date) =>
		lanes(o, now).money.find((x) => x.startsWith('Full refund'));

	it('finished in the past tense once the deadline has passed', () => {
		expect(window(order({ actions: ['cancel', 'move_pickup'] }), NOW)).toBe(
			'Full refund ended 11:00 on Sat 17 October | Now 50% if you cancel; 100% if we cancel | done'
		);
		expect(window(gk({ lifecycle: 'in_production', actions: ['cancel'] }), NOW)).toBe(
			'Full refund ended 11:00 on Sat 17 October | Now 50% of cakes and delivery if you cancel; 100% if we cancel | done'
		);
		// The deadline instant itself has passed (K05: 50% at the deadline).
		expect(window(order(), new Date('2026-10-17T10:00:00Z'))).toContain('Full refund ended');
		expect(window(order(), new Date('2026-10-17T09:59:59Z'))).toContain('Full refund until');
	});

	it('no "if you cancel" when the order can’t be cancelled', () => {
		expect(window(order({ lifecycle: 'ready', actions: [] }), NOW)).toBe(
			'Full refund ended 11:00 on Sat 17 October | 100% if we cancel | done'
		);
		expect(window(order({ actions: ['move_pickup'] }), BEFORE_DEADLINE)).toBe(
			'Full refund until 11:00 on Sat 17 October | From then on 50%; 100% if we cancel | upcoming'
		);
	});

	it('out for delivery: can’t be cancelled (actions, or the page’s own rule without them)', () => {
		const out = { state: 'out_for_delivery', at: '2026-10-22T09:15:00Z', pending: null };
		for (const o of [
			gk({ lifecycle: 'ready', fulfillment: out }),
			gk({ lifecycle: 'ready', fulfillment: out, actions: [] })
		]) {
			expect(window(o, NOW)).toBe(
				'Full refund ended 11:00 on Sat 17 October | 100% if we cancel | done'
			);
		}
	});
});
