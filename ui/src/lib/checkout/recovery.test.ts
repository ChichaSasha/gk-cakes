import { describe, expect, it } from 'vitest';
import { ApiError } from '../api';
import type { OrderRecoveryApi } from './client';
import { ActionKeys, type Who } from './flow';
import { orderView } from './order';
import {
	CANT_CONFIRM,
	CANT_PAY,
	CANT_PAY_DELIVERY,
	LAPSED,
	choiceBaseline,
	choiceCard,
	choiceOutcomeSince,
	deliveryNote,
	deliveryRows,
	lateBaseline,
	lateOutcomeSince,
	recoveryProblemFor,
	sendAnswer,
	sendChoice,
	shortMoment,
	startDeliveryPayment,
	withActions,
	withPaymentCheck,
	withRecovery
} from './recovery';
import type {
	Delivery,
	DeliveryChoice,
	DeliveryFailure,
	LatePayment,
	LatePaymentOffer,
	Order
} from './types';

// Fixture as the approved sketch (FX in poc/r02.js): Pink piping £90,
// Thu 22 October 2026 11:00–12:00 London, deadline 11:00 on Sat 17 October,
// GK delivery £120 with pay-by 11:00 on Tue 20 October.
const guest: Who = { kind: 'guest', secret: 'a'.repeat(43) };
const NOW = new Date('2026-10-22T11:00:00Z'); // the handover day, 12:00 London
const NEXT_DAY = new Date('2026-10-23T09:00:00Z');

function order(over: Partial<Order> = {}): Order {
	return {
		id: '0b6c1b8e-6a0e-4c47-9a4e-2f1a7a3e5d10',
		version: 4,
		lifecycle: 'confirmed',
		cancel_reason: null,
		cancelled_at: null,
		payment_requirement: 'full',
		placed_at: '2026-10-10T09:00:00Z',
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
		paid_at: '2026-10-10T09:05:00Z',
		free_confirmed_at: null,
		free_confirmation: null,
		payment: null,
		items: [],
		fulfillment: { state: 'awaiting_handover', at: null, pending: null },
		...over
	};
}

const quote = {
	id: 'q1',
	amount_minor: 12000,
	currency: 'GBP',
	quoted_at: '2026-10-12T09:00:00Z',
	pay_by: '2026-10-20T10:00:00Z'
};
const delivery = (over: Partial<Delivery> = {}): Delivery => ({
	state: 'quoted',
	quote,
	payable: true,
	payment: null,
	...over
});
const gk = (over: Partial<Order> = {}) =>
	order({ fulfillment_mode: 'gk_delivery', delivery: delivery(), ...over });

const view = (o: Order, now = NOW) => withRecovery(orderView(o), o, { now });
const times = [
	{ id: 't12', starts_at: '2026-10-22T11:00:00Z', ends_at: '2026-10-22T12:00:00Z' },
	{ id: 't13', starts_at: '2026-10-22T12:00:00Z', ends_at: '2026-10-22T13:00:00Z' }
];

// ---------------------------------------------------------------- state 10

const offer = (over: Partial<LatePaymentOffer> = {}): LatePaymentOffer => ({
	id: 'f1',
	kind: 'requested',
	date: '2026-10-22',
	slot_id: 's11',
	start: '2026-10-22T10:00:00Z',
	end: '2026-10-22T11:00:00Z',
	offered_at: '2026-10-15T09:00:00Z',
	accept_until: '2026-10-22T10:00:00Z',
	acceptable: true,
	...over
});
const late = (over: Partial<LatePayment> = {}) =>
	order({
		lifecycle: 'paid_awaiting_agreement',
		late_payment: {
			state: 'waiting',
			offer: null,
			answer: null,
			paid_minor: 9000,
			currency: 'GBP',
			...over
		}
	});

describe('paid after the preparation time (state 10)', () => {
	it('waiting: requested date, everything back from the API’s paid_minor, what happens next', () => {
		const v = view(late());
		expect(v.title).toBe('Payment received');
		expect(v.tone).toBe('warning');
		expect(v.lines).toEqual([
			'Your payment arrived after the time we need to prepare your cake for this date, so the chef needs to check it first.',
			'Thu 22 October is your requested date. It isn’t confirmed yet, and we won’t change it without your agreement.',
			'If you and the chef can’t agree a date, you get everything you paid back: £90.',
			'Please don’t pay again.'
		]);
		expect(v.next?.items[2]).toBe(
			'You decide here. Nothing is confirmed or moved unless you agree; if you don’t, or no date works, you get £90 back in full.'
		);
		expect(v.promise).toBe(false);
		expect(v.refundTerms).toBe('agreement');
		expect(v.contactChef).toBe(true);
		expect(v.answer).toBeUndefined();
	});

	it('the amount is the API’s, never computed (a different paid_minor shows as is)', () => {
		expect(view(late({ paid_minor: 8585 })).lines[2]).toContain('£85.85');
	});

	it('the chef can make your date: Yes, confirm this date / No, refund everything · £90', () => {
		const v = view(late({ state: 'offered', offer: offer() }));
		expect(v.title).toBe('The chef can make your cake');
		expect(v.lines[1]).toBe(
			'The chef can make your cake for Thu 22 October, 11:00–12:00. Do you agree to this date and time?'
		);
		expect(v.lines[2]).toBe(
			'If you’d rather not, you get everything you paid back: £90. Nothing is made until you decide.'
		);
		expect(v.answer).toEqual({
			offerId: 'f1',
			acceptLabel: 'Yes, confirm this date',
			declineLabel: 'No, refund everything · £90',
			declineMinor: 9000,
			note: 'We keep a record of your answer and when you gave it.'
		});
	});

	it('the chef offers another date: only an offer, Accept <date, time>', () => {
		const other = offer({
			kind: 'other',
			date: '2026-10-24',
			start: '2026-10-24T12:00:00Z',
			end: '2026-10-24T13:00:00Z'
		});
		const v = view(late({ state: 'offered', offer: other }));
		expect(v.title).toBe('The chef offers another date');
		expect(v.lines[1]).toBe(
			'The chef can’t make Thu 22 October, but can offer Sat 24 October, 13:00–14:00 (London time). This is only an offer: nothing changes unless you accept it.'
		);
		expect(v.answer?.acceptLabel).toBe('Accept Sat 24 October, 13:00–14:00');
	});

	it('an offer the API says can no longer be accepted keeps only the decline', () => {
		const v = view(late({ state: 'offered', offer: offer({ acceptable: false }) }));
		expect(v.answer?.acceptLabel).toBeUndefined();
		expect(v.answer?.declineLabel).toBe('No, refund everything · £90');
		expect(v.lines).toContain(
			'This offer can no longer be accepted because its time has passed. You can still choose a full refund, or contact the chef.'
		);
	});

	it('answer pending: no buttons, polled, don’t pay again', () => {
		const v = view(
			late({
				state: 'processing',
				offer: offer(),
				answer: {
					offer_id: 'f1',
					answer: 'accept',
					received_at: '2026-10-15T17:20:00Z',
					state: 'processing',
					reason: null
				}
			})
		);
		expect(v.title).toBe('Recording your answer');
		expect(v.poll).toBe(true);
		expect(v.answer).toBeUndefined();
		expect(v.doNotPayAgain).toBe(true);
	});

	it('a lapsed offer: waiting again with the lapse explained', () => {
		const v = view(
			late({
				answer: {
					offer_id: 'f1',
					answer: 'accept',
					received_at: 'x',
					state: 'lapsed',
					reason: 'slot_unavailable'
				}
			})
		);
		expect(v.title).toBe('Payment received');
		expect(v.lines).toContain(
			'The time the chef offered is no longer available, so the offer has lapsed. Nothing has changed: the chef will offer again.'
		);
		// The same wording as the refusal: the page shows it once, from the order.
		expect(v.lines.filter((l) => l === LAPSED)).toHaveLength(1);
	});

	it('you agreed / you accepted another date (agreed block on the confirmed order)', () => {
		const at = '2026-10-15T17:20:00Z';
		const a = view(
			order({
				agreed: {
					at,
					kind: 'requested',
					date: '2026-10-22',
					start: '2026-10-22T10:00:00Z',
					end: '2026-10-22T11:00:00Z',
					offer_id: 'f1'
				}
			})
		);
		expect(a.title).toBe('Your order is confirmed');
		expect(a.lines).toEqual([
			'Thank you. You agreed to Thu 22 October, 11:00–12:00 on Thu 15 October, 18:20, and we’ve received your payment.'
		]);
		expect(a.refundTerms).toBe('standard');
		const b = view(
			order({
				agreed: {
					at,
					kind: 'other',
					date: '2026-10-24',
					start: '2026-10-24T12:00:00Z',
					end: '2026-10-24T13:00:00Z',
					offer_id: 'f2'
				}
			})
		);
		expect(b.lines[1]).toBe('Your new date replaces the date you first asked for.');
		expect(shortMoment(at)).toBe('Thu 15 October, 18:20');
	});

	it('own answer outcome: only a newer answer counts; leaving the state is done', () => {
		const o = late({ state: 'offered', offer: offer() });
		const b = lateBaseline(o);
		expect(lateOutcomeSince(o, b)).toBeUndefined();
		const processing = late({
			state: 'processing',
			offer: offer(),
			answer: {
				offer_id: 'f1',
				answer: 'accept',
				received_at: 'r2',
				state: 'processing',
				reason: null
			}
		});
		expect(lateOutcomeSince({ ...processing, version: 5 }, b)).toBeUndefined();
		expect(lateOutcomeSince({ ...o, lifecycle: 'confirmation_pending', version: 5 }, b)).toBe(
			'done'
		);
		const lapsed = late({
			answer: {
				offer_id: 'f1',
				answer: 'accept',
				received_at: 'r2',
				state: 'lapsed',
				reason: 'slot_unavailable'
			}
		});
		expect(lateOutcomeSince({ ...lapsed, version: 5 }, b)).toBe('lapsed');
		// An earlier lapse (the same answer as the baseline) is not this page's.
		expect(lateOutcomeSince({ ...lapsed, version: 6 }, lateBaseline(lapsed))).toBeUndefined();
	});
});

// ---------------------------------------------------------------- state 15

describe('delivery quote not paid (state 15)', () => {
	it('quote: amount, pay-by (Q9) and Pay for delivery · £120 from the API', () => {
		const v = view(gk());
		expect(v.title).toBe('Your delivery quote is waiting for payment');
		expect(v.lines[0]).toBe(
			'The chef can deliver your cake on Thu 22 October, 11:00–12:00 for £120.'
		);
		expect(v.lines[1]).toBe(
			'Please pay by 11:00 (London time) on Tue 20 October (48 hours before your delivery). Your cake is paid and confirmed; we can deliver only once the delivery is paid.'
		);
		expect(v.action).toBe('pay_delivery');
		expect(v.actionLabel).toBe('Pay for delivery · £120');
	});

	it('no pay button unless the API says payable', () => {
		const v = view(gk({ delivery: delivery({ payable: false }) }));
		expect(v.action).toBe('none');
	});

	it('a failed attempt is said; the quote is payable again', () => {
		const v = view(
			gk({
				delivery: delivery({
					payment: {
						id: 'p1',
						state: 'failed',
						amount_minor: 12000,
						currency: 'GBP',
						requested_at: 'x',
						checkout_url: null,
						expires_at: null
					}
				})
			})
		);
		expect(v.lines).toContain(
			'That payment didn’t go through. No payment was taken for that attempt.'
		);
		expect(v.action).toBe('pay_delivery');
	});

	it('checking: do not pay again, polled; back to an open (verified) payment page only', () => {
		const pay = {
			id: 'p1',
			state: 'pending' as const,
			amount_minor: 12000,
			currency: 'GBP',
			requested_at: 'x',
			checkout_url: 'https://checkout.stripe.com/c/pay/cs_1',
			expires_at: 'y'
		};
		const v = view(gk({ delivery: delivery({ state: 'payment_checking', payment: pay }) }));
		expect(v.title).toBe('Still checking delivery payment; do not pay again');
		expect(v.poll).toBe(true);
		expect(v.doNotPayAgain).toBe(true);
		expect(v.action).toBe('open_payment');
		expect(v.checkoutUrl).toBe(pay.checkout_url);
		const evil = view(
			gk({
				delivery: delivery({
					state: 'payment_checking',
					payment: { ...pay, checkout_url: 'https://evil.example/pay' }
				})
			})
		);
		expect(evil.action).toBe('none');
	});

	it('details: quoted, not paid · £120; paid separately once paid', () => {
		expect(deliveryRows(gk())).toEqual([
			{ label: 'GK delivery · quoted, not paid', amount: '£120' }
		]);
		expect(
			deliveryRows(gk({ delivery: delivery({ state: 'paid', paid_minor: 12000, payable: false }) }))
		).toEqual([{ label: 'GK delivery · paid separately', amount: '£120' }]);
	});

	it('a stray delivery payment is said once (unmatched_minor)', () => {
		const v = view(
			gk({ delivery: delivery({ state: 'paid', paid_minor: 12000, unmatched_minor: 12000 }) })
		);
		expect(v.lines.at(-1)).toBe(
			'We received a delivery payment of £120 that couldn’t be used for your order. The chef will return it to you: please don’t pay it again.'
		);
	});
});

// ---------------------------------------------------------------- state 14 / 15 passed

const q5 = (over: Partial<DeliveryChoice> = {}): DeliveryChoice => ({
	open: true,
	reason: 'not_possible',
	opens_at: null,
	pending: false,
	choices: ['collection', 'customer_courier', 'full_refund'],
	times,
	chosen: null,
	...over
});

describe('we can’t deliver (state 14) and pay-by passed (state 15)', () => {
	it('not possible: apology, no deadline (Q8); paid delivery back whatever they choose', () => {
		const paid = gk({
			delivery: delivery({ state: 'paid', paid_minor: 12000, payable: false }),
			delivery_choice: q5()
		});
		const v = view(paid);
		expect(v.title).toBe('We can’t deliver your cake');
		expect(v.lines).toEqual([
			'We’re sorry: the chef can’t deliver your cake on Thu 22 October. Your cake is paid and still booked for that day.',
			'Your delivery payment of £120 will be returned in full, whatever you choose.',
			'Please choose what you’d like below. There’s no deadline: the chef keeps your order open until you decide, and nothing changes until you confirm.'
		]);
		const card = choiceCard(paid, guest, NOW);
		expect(card?.kind).toBe('delivery');
		expect(card?.options.map((o) => o.label)).toEqual([
			'Collect it · free',
			'Send your own courier',
			'Full refund · everything paid'
		]);
		expect(card?.options[2].note).toBe(
			'Everything you paid back, cake and delivery. Your order is cancelled.'
		);
		expect(card?.times.map((t) => t.label)).toEqual(['12:00–13:00', '13:00–14:00']);
		expect(card?.legends.customer_courier).toBe(
			'Courier collection time on Thu 22 October · London time'
		);
		expect(card?.deadline).toBe(
			'Your refund deadline stays 11:00 (London time) on Sat 17 October.'
		);
	});

	it('not possible with an unpaid quote: GK delivery · Not paid', () => {
		const o = gk({
			delivery: delivery({ state: 'quoted', payable: false }),
			delivery_choice: q5()
		});
		const v = view(o);
		expect(v.lines).toHaveLength(2);
		// The API keeps the quote as quoted; the chef's decision closed it (the sketch).
		expect(deliveryRows(o)).toEqual([{ label: 'GK delivery', amount: 'Not paid' }]);
		expect(deliveryNote(o)).toBe('Delivery was a request until the chef confirmed it.');
		// No standard terms while the choice decides (as the failure view).
		expect(v.refundTerms).toBe('none');
		const noQuote = gk({
			delivery: delivery({ state: 'awaiting_quote', quote: null }),
			delivery_choice: q5()
		});
		expect(deliveryRows(noQuote)).toEqual([{ label: 'GK delivery', amount: 'Not paid' }]);
		expect(choiceCard(o, guest, NOW)?.options[2].note).toBe(
			'Everything you paid back. Your order is cancelled.'
		);
	});

	it('pay-by passed: the same choice with no deadline', () => {
		const o = gk({
			delivery: delivery({ state: 'pay_by_passed', payable: false }),
			delivery_choice: q5({ reason: 'pay_by_passed', opens_at: quote.pay_by })
		});
		const v = view(o);
		expect(v.title).toBe('Your delivery wasn’t paid in time');
		expect(v.lines[0]).toBe(
			'The delivery quote wasn’t paid by 11:00 (London time) on Tue 20 October, 48 hours before your delivery, so we can’t deliver your cake.'
		);
		expect(v.action).toBe('none');
		expect(v.refundTerms).toBe('none');
		expect(choiceCard(o, guest, NOW)?.lead).toBe('Nothing changes until you confirm.');
		// def('delivery-unpaid') passed: the quote stays listed, and can no longer be paid.
		expect(deliveryRows(o)).toEqual([{ label: 'GK delivery · quoted, not paid', amount: '£120' }]);
		expect(deliveryNote(o)).toBe('The delivery quote can no longer be paid.');
		expect(deliveryNote(gk())).toBeUndefined();
	});

	it('only the API’s listed choices; none without a capability or once closed', () => {
		const o = gk({ delivery_choice: q5({ choices: ['full_refund'] }) });
		expect(choiceCard(o, guest, NOW)?.options.map((x) => x.value)).toEqual(['full_refund']);
		expect(choiceCard(o, null, NOW)).toBeUndefined();
		expect(choiceCard(gk({ delivery_choice: q5({ open: false }) }), guest, NOW)).toBeUndefined();
	});

	it('pending: confirming your choice, polled, no card', () => {
		const o = gk({
			delivery_choice: q5({
				open: false,
				pending: true,
				choices: [],
				times: [],
				chosen: {
					choice: 'collection',
					state: 'processing',
					refusal_reason: null,
					requested_at: 'r1',
					to: { fulfillment_mode: 'collection', date: '2026-10-22', ...times[1] }
				}
			})
		});
		const v = view(o);
		expect(v.title).toBe('Confirming your choice');
		expect(v.lines[0]).toBe(
			'We’re confirming your choice: collecting your cake from St Katharine Docks at 13:00–14:00 on Thu 22 October. This usually takes a few seconds.'
		);
		expect(v.poll).toBe(true);
		expect(choiceCard(o, guest, NOW)).toBeUndefined();
	});

	it('results: collection / own courier with the delivery refund (only once secured)', () => {
		const chosen = (choice: string) => ({
			choice,
			state: 'chosen',
			refusal_reason: null,
			requested_at: 'r1',
			to: { fulfillment_mode: choice, date: '2026-10-22', ...times[0] },
			delivery_refund_minor: 12000
		});
		const o = order({
			fulfillment_mode: 'collection',
			slot: { id: 't12', starts_at: times[0].starts_at, ends_at: times[0].ends_at },
			delivery: delivery({ state: 'cancelled_by_choice', paid_minor: 12000, payable: false }),
			delivery_choice: q5({
				open: false,
				reason: 'not_possible',
				choices: [],
				times: [],
				chosen: chosen('collection')
			}),
			refund: {
				state: 'requested',
				target_minor: 12000,
				succeeded_minor: 0,
				reason: 'delivery_failure'
			}
		});
		const v = view(o);
		expect(v.title).toBe('You’ll collect your cake');
		expect(v.lines).toEqual([
			'Thank you. Your order is now for collection from St Katharine Docks on Thu 22 October, 12:00–13:00 (London time).',
			'A refund of your £120 delivery payment has been requested. This page shows its progress.',
			'Your refund deadline hasn’t changed: 11:00 (London time) on Sat 17 October.'
		]);
		expect(v.refund).toBeUndefined();
		expect(v.refundTracker).toBe(true);
		expect(deliveryRows(o)).toEqual([
			{ label: 'GK delivery · paid separately', amount: '£120' },
			{ label: 'Delivery refund · requested', amount: '£120' }
		]);
		const c = view({
			...o,
			fulfillment_mode: 'customer_courier',
			delivery_choice: q5({
				open: false,
				choices: [],
				times: [],
				chosen: chosen('customer_courier')
			})
		});
		expect(c.title).toBe('Your courier will collect your cake');
		expect(c.lines[0]).toBe(
			'Thank you. Your courier collects from St Katharine Docks on Thu 22 October, 12:00–13:00 (London time). You book and pay the courier; responsibility passes to them at handover.'
		);
	});

	it('a result after a later move shows the order’s current time, not the chosen one', () => {
		// Chosen 12:00, then moved to 16:00 the same day (chosen.to never changes).
		const o = order({
			version: 9,
			slot: { id: 's16', starts_at: '2026-10-22T15:00:00Z', ends_at: '2026-10-22T16:00:00Z' },
			delivery: delivery({ state: 'cancelled_by_choice', paid_minor: 12000, payable: false }),
			delivery_choice: q5({
				open: false,
				choices: [],
				times: [],
				chosen: {
					choice: 'collection',
					state: 'chosen',
					refusal_reason: null,
					requested_at: 'r1',
					to: { fulfillment_mode: 'collection', date: '2026-10-22', ...times[0] },
					delivery_refund_minor: 12000
				}
			})
		});
		const line = view(o).lines[0];
		expect(line).toContain('Thu 22 October, 16:00–17:00 (London time)');
		expect(line).not.toContain('12:00–13:00');
		// The failed delivery's collection result as well.
		const f = failed(
			{
				open: false,
				choices: [],
				times: [],
				chosen: {
					choice: 'collection',
					state: 'chosen',
					refusal_reason: null,
					requested_at: 'r1',
					to: { fulfillment_mode: 'collection', date: '2026-10-22', ...times[1] },
					delivery_refund_minor: 12000
				}
			},
			{
				fulfillment_mode: 'collection',
				slot: { id: 's16', starts_at: '2026-10-22T15:00:00Z', ends_at: '2026-10-22T16:00:00Z' }
			}
		);
		expect(view(f).lines[0]).toBe(
			'Thank you. Please collect your cake from St Katharine Docks today, Thu 22 October, 16:00–17:00 (London time).'
		);
	});

	it('refused: the choice is open again with the reason', () => {
		const o = gk({
			delivery_choice: q5({
				chosen: {
					choice: 'collection',
					state: 'refused',
					refusal_reason: 'slot_unavailable',
					requested_at: 'r1',
					to: null
				}
			})
		});
		expect(choiceCard(o, guest, NOW)?.refusal).toBe(
			'Your last choice couldn’t be made: that time is no longer available. Nothing has changed: please choose again.'
		);
	});

	it('own choice outcome: newer only; full refund done once cancelling', () => {
		const o = gk({ delivery_choice: q5() });
		const b = choiceBaseline(o, 'delivery');
		expect(choiceOutcomeSince(o, 'delivery', b)).toBeUndefined();
		const refused = gk({
			version: 5,
			delivery_choice: q5({
				chosen: {
					choice: 'collection',
					state: 'refused',
					refusal_reason: 'order_changed',
					requested_at: 'r1',
					to: null
				}
			})
		});
		expect(choiceOutcomeSince(refused, 'delivery', b)).toBe('refused');
		// The same refusal seen again (it was the baseline) is not new.
		expect(
			choiceOutcomeSince(
				{ ...refused, version: 6 },
				'delivery',
				choiceBaseline(refused, 'delivery')
			)
		).toBeUndefined();
		expect(choiceOutcomeSince({ ...o, lifecycle: 'cancellation_pending' }, 'delivery', b)).toBe(
			'done'
		);
	});
});

// ---------------------------------------------------------------- state 19

const failure = (over: Partial<DeliveryFailure> = {}): DeliveryFailure => ({
	cause: 'our_side',
	at: '2026-10-22T10:40:00Z',
	state: 'recorded',
	open: true,
	pending: false,
	choices: ['redelivery', 'collection'],
	times,
	delivery_paid_minor: 12000,
	chosen: null,
	...over
});
const failed = (over: Partial<DeliveryFailure> = {}, o: Partial<Order> = {}) =>
	gk({
		lifecycle: 'ready',
		delivery: delivery({ state: 'paid', paid_minor: 12000, payable: false }),
		fulfillment: { state: 'exception', at: '2026-10-22T10:40:00Z', pending: null },
		delivery_failure: failure(over),
		...o
	});

describe('failed delivery (state 19)', () => {
	it('our side: apology and both choices, 100% of the delivery if collected', () => {
		const o = failed();
		const v = view(o);
		expect(v.title).toBe('We couldn’t deliver your cake today');
		expect(v.lines).toEqual([
			'We’re sorry: our delivery on Thu 22 October, 11:00–12:00 couldn’t be completed because of a problem on our side.',
			'Please choose: we deliver again, or you collect today. If you collect, your delivery payment of £120 comes back in full (100%).'
		]);
		const card = choiceCard(o, guest, NOW);
		expect(card?.kind).toBe('failure');
		expect(card?.options).toEqual([
			{
				value: 'redelivery',
				label: 'Deliver it again',
				note: 'The chef confirms a new delivery time with you. Nothing more to pay.',
				needsTime: false
			},
			{
				value: 'collection',
				label: 'Collect it today · delivery refunded',
				note: 'St Katharine Docks, at any opening time still ahead today. Your £120 delivery payment comes back in full.',
				needsTime: true
			}
		]);
		expect(card?.legends.collection).toBe('Collection time today, Thu 22 October · London time');
	});

	it('no time left today: redelivery alone (the API’s choices)', () => {
		const o = failed({ choices: ['redelivery'], times: [] });
		expect(view(o).lines[1]).toBe(
			'There are no collection times left today, so the chef will deliver your cake again. Please confirm below.'
		);
		expect(choiceCard(o, guest, NOW)?.options.map((x) => x.value)).toEqual(['redelivery']);
	});

	it('a waived delivery: no refund promised', () => {
		const card = choiceCard(failed({ delivery_paid_minor: null }), guest, NOW);
		expect(card?.options[1].label).toBe('Collect it today');
		expect(card?.options[1].note).toBe(
			'St Katharine Docks, at any opening time still ahead today.'
		);
	});

	it('viewed on a later day: no “today”', () => {
		expect(view(failed({ choices: ['redelivery'], times: [] }), NEXT_DAY).title).toBe(
			'We couldn’t deliver your cake'
		);
	});

	it('collection chosen: today, with “Delivery refund of £120 requested”', () => {
		const o = failed(
			{
				open: false,
				choices: [],
				times: [],
				chosen: {
					choice: 'collection',
					state: 'chosen',
					refusal_reason: null,
					requested_at: 'r1',
					to: { fulfillment_mode: 'collection', date: '2026-10-22', ...times[1] },
					delivery_refund_minor: 12000
				}
			},
			{
				fulfillment_mode: 'collection',
				slot: { id: 't13', starts_at: times[1].starts_at, ends_at: times[1].ends_at },
				delivery: delivery({ state: 'cancelled_by_choice', paid_minor: 12000 })
			}
		);
		const v = view(o);
		expect(v.title).toBe('You’ll collect your cake today');
		expect(v.lines).toEqual([
			'Thank you. Please collect your cake from St Katharine Docks today, Thu 22 October, 13:00–14:00 (London time).',
			'Your delivery payment of £120 comes back in full, because we didn’t deliver. Delivery refund of £120 requested.'
		]);
		expect(deliveryRows(o).at(-1)).toEqual({
			label: 'Delivery refund · requested',
			amount: '£120'
		});
		expect(choiceCard(o, guest, NOW)).toBeUndefined();
	});

	it('redelivery chosen: the chef confirms a new time; then the agreed time', () => {
		const chosen = {
			choice: 'redelivery',
			state: 'chosen',
			refusal_reason: null,
			requested_at: 'r1',
			to: null
		};
		const v = view(
			failed({ open: false, choices: [], times: [], chosen, redelivery_time_set: false })
		);
		expect(v.title).toBe('We’ll deliver your cake again');
		expect(v.lines).toEqual([
			'Thank you. The chef will confirm a new delivery time with you on this page. There’s nothing more to pay.',
			'Your delivery payment is kept for the new delivery.'
		]);
		const set = view(
			failed(
				{ open: false, choices: [], times: [], chosen, redelivery_time_set: true },
				{
					slot: { id: 's15', starts_at: '2026-10-23T14:00:00Z', ends_at: '2026-10-23T15:00:00Z' },
					date: '2026-10-23'
				}
			)
		);
		expect(set.lines[0]).toBe(
			'Thank you. Your new delivery time is Fri 23 October, 15:00–16:00 (London time), agreed with the chef. There’s nothing more to pay.'
		);
		// Re-dispatched: the failure is history; the normal view applies.
		const out = view(
			failed(
				{ open: false, choices: [], times: [], chosen, redelivery_time_set: true },
				{ fulfillment: { state: 'out_for_delivery', at: '2026-10-23T13:00:00Z', pending: null } }
			)
		);
		expect(out.title).toBe('Your cake is ready');
	});

	it('nobody available: 50% of the cake (cake_refund_minor), the delivery kept', () => {
		const pending = view(failed({ cause: 'nobody_available', state: 'processing', open: false }));
		expect(pending.title).toBe('We couldn’t deliver your cake');
		expect(pending.poll).toBe(true);
		const o = gk({
			lifecycle: 'completed',
			delivery: delivery({ state: 'paid', paid_minor: 12000, payable: false }),
			fulfillment: { state: 'no_show', at: '2026-10-22T10:25:00Z', pending: null },
			delivery_failure: failure({
				cause: 'nobody_available',
				state: 'no_show',
				open: false,
				choices: [],
				times: [],
				cake_refund_minor: 4500,
				delivery_kept_minor: 12000
			}),
			refund: { state: 'requested', target_minor: 4500, succeeded_minor: 0, reason: 'no_show' }
		});
		const v = view(o);
		expect(v.title).toBe('We couldn’t deliver your cake');
		expect(v.lines).toEqual([
			'The chef arrived on Thu 22 October, 11:00–12:00, but nobody was available to take the cake.',
			'As with a cake that isn’t collected, you get 50% of the cake price back: £45, requested automatically. The delivery payment of £120 isn’t refunded, because the delivery was attempted.'
		]);
		expect(v.refund).toBe('Refund of £45 requested. We’ll update this page as it progresses.');
		expect(v.refundTracker).toBe(true);
		expect(v.contactChef).toBe(true);
	});

	it('nobody available on a free-confirmed cake: no “£0” refund, the kept delivery said', () => {
		const o = gk({
			lifecycle: 'completed',
			payment_requirement: 'none',
			total_minor: 0,
			paid_at: null,
			delivery: delivery({ state: 'paid', paid_minor: 12000, payable: false }),
			fulfillment: { state: 'no_show', at: '2026-10-22T10:25:00Z', pending: null },
			delivery_failure: failure({
				cause: 'nobody_available',
				state: 'no_show',
				open: false,
				choices: [],
				times: [],
				cake_refund_minor: 0,
				delivery_kept_minor: 12000
			}),
			refund: null
		});
		const v = view(o);
		expect(v.lines).toEqual([
			'The chef arrived on Thu 22 October, 11:00–12:00, but nobody was available to take the cake.',
			'Your cakes cost nothing, so there’s no cake refund.',
			'The delivery payment of £120 isn’t refunded, because the delivery was attempted.'
		]);
		expect(v.lines.join(' ')).not.toContain('£0');
		expect(v.refundTracker).toBe(false);
	});

	it('own failure choice outcome', () => {
		const o = failed();
		const b = choiceBaseline(o, 'failure');
		const done = failed({
			open: false,
			chosen: {
				choice: 'redelivery',
				state: 'chosen',
				refusal_reason: null,
				requested_at: 'r1',
				to: null
			}
		});
		expect(choiceOutcomeSince({ ...done, version: 5 }, 'failure', b)).toBe('done');
		expect(choiceOutcomeSince(done, 'failure', b)).toBeUndefined(); // same version: not read yet
	});
});

// ---------------------------------------------------------------- commands and refusals

describe('commands', () => {
	function fakeApi(orders: Order[]) {
		const calls: { path: string; body: unknown; key: string }[] = [];
		const accepted = { order_id: 'o', state: 'x', poll_after_seconds: 1 };
		const api: OrderRecoveryApi = {
			order: async () => orders.shift() as Order,
			latePaymentAnswer: async (_id, body, key) => (
				calls.push({ path: 'answer', body, key }),
				accepted
			),
			deliveryPayment: async (_id, body, key) => (calls.push({ path: 'pay', body, key }), accepted),
			deliveryChoice: async (_id, body, key) => (calls.push({ path: 'q5', body, key }), accepted),
			deliveryFailureChoice: async (_id, body, key) => (
				calls.push({ path: 'fail', body, key }),
				accepted
			)
		};
		return { api, calls };
	}

	it('answer: a network retry at the same version reuses the key', async () => {
		const { api, calls } = fakeApi([]);
		const keys = new ActionKeys();
		const o = late({ state: 'offered', offer: offer() });
		await sendAnswer(api, keys, o, 'f1', 'accept', guest);
		await sendAnswer(api, keys, o, 'f1', 'accept', guest);
		await sendAnswer(api, keys, o, 'f1', 'decline', guest);
		expect(calls[0].body).toEqual({ expected_version: 4, offer_id: 'f1', answer: 'accept' });
		expect(calls[1].body).toEqual(calls[0].body);
		expect(calls[0].key).toBe(calls[1].key);
		expect(calls[2].key).not.toBe(calls[0].key);
	});

	it('answer: after a refusal (the version moved on) answering again uses a new key', async () => {
		const { api, calls } = fakeApi([]);
		const keys = new ActionKeys();
		await sendAnswer(api, keys, late({ state: 'offered', offer: offer() }), 'f1', 'accept', guest);
		// Refused (order_changed): the same offer is open again at version 6.
		const again = {
			...late({
				state: 'offered',
				offer: offer(),
				answer: {
					offer_id: 'f1',
					answer: 'accept',
					received_at: 'r1',
					state: 'refused',
					reason: 'order_changed'
				}
			}),
			version: 6
		};
		await sendAnswer(api, keys, again, 'f1', 'accept', guest);
		expect(calls[1].body).toEqual({ expected_version: 6, offer_id: 'f1', answer: 'accept' });
		// A new body never goes with an old key (the API would answer KEY_REUSED).
		expect(calls[1].key).not.toBe(calls[0].key);
	});

	it('choices: target_slot_id only for a time choice', async () => {
		const { api, calls } = fakeApi([]);
		const keys = new ActionKeys();
		await sendChoice(
			api,
			keys,
			gk({ delivery_choice: q5() }),
			'delivery',
			'full_refund',
			undefined,
			guest
		);
		await sendChoice(api, keys, failed(), 'failure', 'collection', 't13', guest);
		expect(calls[0]).toMatchObject({
			path: 'q5',
			body: { expected_version: 4, choice: 'full_refund' }
		});
		expect(calls[0].body).not.toHaveProperty('target_slot_id');
		expect(calls[1]).toMatchObject({
			path: 'fail',
			body: { expected_version: 4, choice: 'collection', target_slot_id: 't13' }
		});
	});

	it('delivery payment: the current quote, then only a verified Stripe page', async () => {
		const pay = (state: 'created' | 'pending' | 'failed', url: string | null) =>
			gk({
				delivery: delivery({
					state: state === 'failed' ? 'quoted' : 'payment_checking',
					payment: {
						id: 'p1',
						state,
						amount_minor: 12000,
						currency: 'GBP',
						requested_at: 'x',
						checkout_url: url,
						expires_at: null
					}
				})
			});
		const deps = { sleep: async () => undefined, now: () => 0 };
		const ok = fakeApi([
			pay('created', null),
			pay('pending', 'https://checkout.stripe.com/c/pay/cs_1')
		]);
		expect(await startDeliveryPayment(ok.api, new ActionKeys(), gk(), guest, deps)).toBe(
			'https://checkout.stripe.com/c/pay/cs_1'
		);
		expect(ok.calls[0].body).toEqual({ expected_version: 4, quote_id: 'q1' });
		const evil = fakeApi([pay('pending', 'https://evil.example/pay'), pay('failed', null)]);
		expect(
			await startDeliveryPayment(evil.api, new ActionKeys(), gk(), guest, deps)
		).toBeUndefined();
	});

	it('delivery payment: a failed read after the 202 keeps checking, never a refusal', async () => {
		let reads = 0;
		const { api } = fakeApi([]);
		api.order = async () => {
			reads++;
			if (reads < 3) throw new ApiError(0, {});
			return gk({
				delivery: delivery({
					state: 'payment_checking',
					payment: {
						id: 'p1',
						state: 'pending',
						amount_minor: 12000,
						currency: 'GBP',
						requested_at: 'x',
						checkout_url: 'https://checkout.stripe.com/c/pay/cs_2',
						expires_at: null
					}
				})
			});
		};
		const deps = { sleep: async () => undefined, now: () => 0 };
		expect(await startDeliveryPayment(api, new ActionKeys(), gk(), guest, deps)).toBe(
			'https://checkout.stripe.com/c/pay/cs_2'
		);
		// Reads keep failing until the wait ends: undefined (the page keeps checking).
		let t = 0;
		api.order = async () => {
			throw new ApiError(0, {});
		};
		const late2 = { sleep: async () => void (t += 10_000), now: () => t };
		await expect(startDeliveryPayment(api, new ActionKeys(), gk(), guest, late2)).resolves.toBe(
			undefined
		);
	});

	it('delivery payment: one key per version (a chef’s change keeps the quote)', async () => {
		const { api, calls } = fakeApi([gk(), gk()]);
		const keys = new ActionKeys();
		const deps = { sleep: async () => undefined, now: () => 0 };
		await startDeliveryPayment(api, keys, gk(), guest, deps);
		await startDeliveryPayment(api, keys, gk({ version: 5 }), guest, deps);
		expect(calls[1].body).toEqual({ expected_version: 5, quote_id: 'q1' });
		expect(calls[1].key).not.toBe(calls[0].key);
	});

	it('refusals in the customer’s words (nothing changed)', () => {
		const e = (status: number, code: string) =>
			new ApiError(status, { code, message: 'x', request_id: 'r', retryable: false });
		expect(recoveryProblemFor(e(409, 'SLOT_UNAVAILABLE'), 'answer')).toEqual({
			message: LAPSED,
			next: 'refresh',
			outcome: true
		});
		expect(recoveryProblemFor(e(409, 'KEY_REUSED'), 'answer')).toEqual({
			message:
				'Your order changed since this was first sent. Nothing has changed: we’ve refreshed it, so please check it and try again.',
			next: 'refresh'
		});
		expect(recoveryProblemFor(e(409, 'SLOT_UNAVAILABLE'), 'choice')).toEqual({
			message: 'That time is no longer available. Nothing has changed: please choose another time.',
			next: 'refresh'
		});
		expect(recoveryProblemFor(e(409, 'CHOICE_NOT_OPEN'), 'choice').next).toBe('refresh');
		expect(recoveryProblemFor(e(409, 'PAYMENT_IN_PROGRESS'), 'delivery_payment').message).toContain(
			'Please don’t pay again'
		);
		expect(recoveryProblemFor(e(503, 'UNAVAILABLE'), 'delivery_payment').message).toContain(
			'Nothing was charged'
		);
		expect(recoveryProblemFor(e(404, 'NOT_FOUND'), 'choice').next).toBe('none');
		expect(recoveryProblemFor(new ApiError(0, {}), 'choice').next).toBe('retry');
	});
});

describe('a payment this page asked for, while the order can’t be read (r2 minor)', () => {
	it('delivery: do not pay again, no pay button, polled (over the old quote view)', () => {
		const before = view(gk());
		expect(before.action).toBe('pay_delivery');
		const v = withPaymentCheck(before, 'delivery');
		expect(v.title).toBe('Still checking delivery payment; do not pay again');
		expect(v.lines).toEqual([
			'Please don’t pay again. We’ll update this page when we know the result.',
			'Your cake order is confirmed and isn’t affected.'
		]);
		expect(v.action).toBe('none');
		expect(v.actionLabel).toBeUndefined();
		expect(v.poll).toBe(true);
		expect(v.doNotPayAgain).toBe(true);
		expect(v.lines.join(' ')).not.toContain('Nothing was charged');
	});

	it('cake: the same, over “waiting for payment”; nothing changes without a check', () => {
		const waiting = orderView(order({ lifecycle: 'awaiting_payment', paid_at: null }));
		expect(waiting.action).toBe('start_payment');
		const v = withPaymentCheck(waiting, 'cake');
		expect(v.title).toBe('Still checking payment; do not pay again');
		expect(v.action).toBe('none');
		expect(v.doNotPayAgain).toBe(true);
		expect(withPaymentCheck(waiting, null)).toBe(waiting);
	});
});

// ---------------------------------------------------------------- slice 3: the API's actions (0038)

describe('the choice cards follow the API’s actions', () => {
	const owner: Who = { kind: 'account', token: 't' };

	it('Q5: only the choices this caller may make (allowed_choices)', () => {
		const o = gk({
			delivery: delivery({ payable: false }),
			delivery_choice: q5({ allowed_choices: ['collection', 'customer_courier'] }),
			actions: ['delivery_choice']
		});
		expect(choiceCard(o, guest, NOW)?.options.map((x) => x.value)).toEqual([
			'collection',
			'customer_courier'
		]);
		const refundOnly = { ...o, delivery_choice: q5({ allowed_choices: ['full_refund'] }) };
		expect(choiceCard(refundOnly, guest, NOW)?.options.map((x) => x.value)).toEqual([
			'full_refund'
		]);
		// Nothing allowed, or no delivery_choice action: no card.
		expect(
			choiceCard({ ...o, delivery_choice: q5({ allowed_choices: [] }) }, guest, NOW)
		).toBeUndefined();
		expect(choiceCard({ ...o, actions: ['cancel'] }, owner, NOW)).toBeUndefined();
		// Never more than the order's own choices.
		const extra = {
			...o,
			delivery_choice: q5({
				choices: ['full_refund'],
				allowed_choices: ['collection', 'full_refund']
			})
		};
		expect(choiceCard(extra, owner, NOW)?.options.map((x) => x.value)).toEqual(['full_refund']);
	});

	it('Q5 without actions (an older API): every listed choice', () => {
		const o = gk({ delivery: delivery({ payable: false }), delivery_choice: q5() });
		expect(choiceCard(o, guest, NOW)?.options).toHaveLength(3);
	});

	it('a failed delivery’s choice needs delivery_failure_choice', () => {
		expect(choiceCard(failed({}, { actions: ['delivery_failure_choice'] }), guest, NOW)?.kind).toBe(
			'failure'
		);
		expect(choiceCard(failed({}, { actions: ['cancel'] }), guest, NOW)).toBeUndefined();
		expect(choiceCard(failed(), guest, NOW)?.kind).toBe('failure');
	});
});

describe('withActions: only the buttons the API offers this caller', () => {
	const owner: Who = { kind: 'account', token: 't' };
	const full = (o: Order, who: Who | null = guest) =>
		withActions(withRecovery(orderView(o), o, { now: NOW }), o, who);
	const awaiting = (over: Partial<Order> = {}) =>
		order({ lifecycle: 'awaiting_payment', paid_at: null, ...over });

	it('pay: offered with the action; otherwise a new order instead', () => {
		expect(full(awaiting({ actions: ['pay', 'cancel'] })).action).toBe('start_payment');
		const v = full(awaiting({ actions: [] }));
		expect(v.action).toBe('new_order');
		expect(v.actionLabel).toBe('Start a new order');
		expect(v.lines).toEqual([CANT_PAY]);
		const failedAttempt = full(
			awaiting({
				actions: [],
				payment: {
					id: 'p',
					state: 'failed',
					amount_minor: 9000,
					currency: 'GBP',
					requested_at: 'x',
					checkout_url: null,
					expires_at: null
				}
			})
		);
		expect(failedAttempt.title).toBe('That payment didn’t go through');
		expect(failedAttempt.lines).toEqual(['No payment was taken for that attempt.', CANT_PAY]);
	});

	it('an open payment page stays (it is the same payment)', () => {
		const url = 'https://checkout.stripe.com/c/pay/cs_test_1';
		const v = full(
			awaiting({
				actions: [],
				payment: {
					id: 'p',
					state: 'pending',
					amount_minor: 9000,
					currency: 'GBP',
					requested_at: 'x',
					checkout_url: url,
					expires_at: 'x'
				}
			})
		);
		expect(v.action).toBe('open_payment');
	});

	it('free confirmation', () => {
		const free = (actions: string[]) =>
			awaiting({ payment_requirement: 'none', total_minor: 0, actions });
		expect(full(free(['free_confirmation'])).action).toBe('confirm_free');
		const v = full(free([]));
		expect(v.action).toBe('new_order');
		expect(v.lines).toEqual([CANT_CONFIRM]);
	});

	it('pay_delivery: no pay button without it, and a line saying so', () => {
		expect(full(gk({ actions: ['cancel', 'pay_delivery'] })).action).toBe('pay_delivery');
		const v = full(gk({ actions: ['cancel'] }));
		expect(v.action).toBe('none');
		expect(v.lines.at(-1)).toBe(CANT_PAY_DELIVERY);
	});

	it('the late-payment answer: no buttons without answer_late_payment (contact the chef)', () => {
		const o = late({ state: 'offered', offer: offer() });
		expect(full({ ...o, actions: ['answer_late_payment'] }).answer).toBeDefined();
		const v = full({ ...o, actions: [] }, owner);
		expect(v.answer).toBeUndefined();
		expect(v.contactChef).toBe(true);
	});

	it('actions absent (an older API): unchanged', () => {
		const o = late({ state: 'offered', offer: offer() });
		expect(full(o).answer).toBeDefined();
		expect(full(awaiting()).action).toBe('start_payment');
		expect(full(gk()).action).toBe('pay_delivery');
	});
});
