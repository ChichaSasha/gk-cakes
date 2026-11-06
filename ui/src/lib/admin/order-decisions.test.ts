import { describe, expect, it } from 'vitest';
import { ApiError } from '../api';
import type { StaffApi } from './catalogue';
import {
	agreementMissing,
	appointmentChoice,
	canRecordNotPossible,
	choiceStatus,
	decisionDone,
	decisionKey,
	decisionRefusal,
	decisionRequest,
	decisionSettled,
	decisionSlot,
	deliveryStatus,
	distanceText,
	failureChoices,
	failureConfirm,
	failureStatus,
	isoToLondonLocal,
	lastDispatch,
	latePaymentChoices,
	latePaymentStatus,
	lineProblem,
	londonLocalToIso,
	notPossibleConfirm,
	offerLines,
	onItsWay,
	originalHandover,
	parseDistance,
	parsePence,
	pollDecision,
	quoteChoices,
	quoteLines,
	quotePriceProblem,
	scheduleDay,
	sendDecision,
	slotOptions,
	type DecisionInput,
	type ScheduleChoices
} from './order-decisions';
import type { StaffOrderDetail } from './order-detail';

const ID = 'a1b2c3d4-0000-4000-8000-000000000001';
const NOW = new Date('2026-10-29T10:00:00Z'); // Thu 29 Oct, 10:00 London (GMT)

/** A chef's view of a confirmed collection order (handover Thu 29 Oct, 12:00). */
const order = (over: Partial<StaffOrderDetail> = {}): StaffOrderDetail => ({
	id: ID,
	version: 7,
	lifecycle: 'confirmed',
	raw_lifecycle: 'confirmed',
	recovery_state: 'secured',
	cancel_reason: null,
	cancelled_at: null,
	placed_at: '2026-10-12T09:00:00Z',
	date: '2026-10-29',
	fulfillment_mode: 'collection',
	slot: { id: 's1', starts_at: '2026-10-29T12:00:00Z', ends_at: '2026-10-29T13:00:00Z' },
	customer_full_name: 'Mia Example',
	items: [
		{
			line_no: 1,
			variant_id: 'v',
			lot_id: null,
			recipe_version_id: 'r',
			cake_name: 'Pink piping',
			flavour: 'Vanilla Raspberry',
			size_code: 's',
			quantity: 2
		}
	],
	production: {
		state: 'scheduled',
		scheduled_at: '2026-10-14T09:00:00Z',
		started_at: null,
		observed_at: null,
		source: null
	},
	payment_requirement: 'full',
	paid_at: '2026-10-12T09:00:00Z',
	total_minor: 9000,
	currency: 'GBP',
	attention: [],
	commands: [],
	appointment_history: [],
	fulfillment: { state: null, at: null, pending: null },
	fulfillment_history: [],
	...over
});

const late = (over: Partial<StaffOrderDetail> = {}) =>
	order({
		lifecycle: 'paid_awaiting_agreement',
		raw_lifecycle: 'paid_awaiting_agreement',
		slot: { id: 's1', starts_at: '2026-11-05T12:00:00Z', ends_at: '2026-11-05T13:00:00Z' },
		date: '2026-11-05',
		paid_at: '2026-10-28T20:00:00Z',
		late_payment: {
			state: 'waiting',
			offer: null,
			answer: null,
			paid_minor: 9000,
			currency: 'GBP'
		},
		...over
	});

const gk = (delivery: StaffOrderDetail['delivery'], over: Partial<StaffOrderDetail> = {}) =>
	order({
		fulfillment_mode: 'gk_delivery',
		slot: { id: 's1', starts_at: '2026-11-02T12:00:00Z', ends_at: '2026-11-02T13:00:00Z' },
		date: '2026-11-02',
		delivery,
		...over
	});

const quoted = {
	state: 'quoted',
	quote: {
		id: 'q1',
		quoted_at: '2026-10-28T09:00:00Z',
		amount_minor: 12000,
		currency: 'GBP',
		pay_by: '2026-10-31T12:00:00Z'
	},
	payable: true
};

const ev = (action: string, at: string, to = 'out_for_delivery') => ({
	action,
	from: 'awaiting_handover',
	to,
	received_at: at,
	command_id: null,
	state: 'done'
});

/** A ready GK delivery out since 09:30 today, delivery time 10:00–11:00. */
const out = (over: Partial<StaffOrderDetail> = {}) =>
	gk(
		{ state: 'paid', paid_minor: 12000 },
		{
			lifecycle: 'ready',
			raw_lifecycle: 'ready',
			date: '2026-10-29',
			slot: { id: 's1', starts_at: '2026-10-29T10:00:00Z', ends_at: '2026-10-29T11:00:00Z' },
			fulfillment: { state: 'out_for_delivery', at: '2026-10-29T09:30:00Z', pending: null },
			fulfillment_history: [
				ev('ready', '2026-10-29T08:00:00Z', 'awaiting_handover'),
				ev('dispatch', '2026-10-29T09:30:00Z')
			],
			...over
		}
	);

const err = (status: number, code: string, retryable = false) =>
	new ApiError(status, { code, message: code, request_id: 'r', retryable });

describe('input rules', () => {
	it('parses pounds to whole pence as text', () => {
		expect(parsePence('120')).toEqual({ minor: 12000 });
		expect(parsePence('£120.50')).toEqual({ minor: 12050 });
		expect(parsePence(' £ 1,200 ')).toEqual({ minor: 120000 });
		expect(parsePence('0100.05')).toEqual({ minor: 10005 });
		// No floats: 0.1 + 0.2 style inputs stay exact.
		expect(parsePence('100.29')).toEqual({ minor: 10029 });
		expect(parsePence('120.5')).toEqual({ error: 'Give pence as two digits, for example 120.50.' });
		expect(parsePence('120.')).toHaveProperty('error');
		expect(parsePence('12,00')).toHaveProperty('error');
		expect(parsePence('1e3')).toHaveProperty('error');
		expect(parsePence('-120')).toHaveProperty('error');
		expect(parsePence('')).toEqual({ error: 'Enter the delivery price.' });
		expect(parsePence('12345678')).toEqual({ error: 'That price is too large.' });
	});

	it('checks the quote bounds (from £100, at most 99999999p)', () => {
		expect(quotePriceProblem('99.99')).toBe(
			'GK delivery starts from £100. Enter a price of at least £100.'
		);
		expect(quotePriceProblem('100')).toBe('');
		expect(quotePriceProblem('999,999.99')).toBe('');
		expect(quotePriceProblem('1000000')).toBe('A delivery price can be at most £999,999.99.');
	});

	it('turns a measured distance into whole metres without floats', () => {
		expect(parseDistance('5.1', 'mi')).toEqual({ metres: 8208 });
		expect(parseDistance('1', 'mi')).toEqual({ metres: 1609 });
		expect(parseDistance('8.25', 'km')).toEqual({ metres: 8250 });
		expect(parseDistance('0.001', 'km')).toEqual({ metres: 1 });
		expect(parseDistance('0', 'km')).toEqual({ error: 'The distance must be more than 0.' });
		expect(parseDistance('1001', 'km')).toEqual({ error: 'That distance is more than 1,000 km.' });
		expect(parseDistance('5,1', 'mi')).toHaveProperty('error');
		expect(distanceText(8208)).toBe('8.2 km (8,208 m)');
	});

	it('keeps notes and evidence to one line', () => {
		expect(lineProblem('', 500, 'what went wrong')).toBe('Say what went wrong.');
		expect(lineProblem('a\nb', 500, 'x')).toBe('Keep it to one line.');
		expect(lineProblem('x'.repeat(201), 200, 'x')).toBe('Keep it to 200 characters.');
	});

	it('reads datetime-local as London time in winter and summer', () => {
		expect(londonLocalToIso('2026-10-29T10:31')).toBe('2026-10-29T10:31:00Z');
		expect(londonLocalToIso('2026-07-01T10:31')).toBe('2026-07-01T09:31:00Z');
		expect(londonLocalToIso('nope')).toBeNull();
		expect(isoToLondonLocal('2026-07-01T09:31:00Z')).toBe('2026-07-01T10:31');
		expect(isoToLondonLocal(NOW)).toBe('2026-10-29T10:00');
	});

	it('lists what the agreement still needs (r03b M2)', () => {
		const empty = { channel: '', when: '', evidence: '', exact: false };
		expect(agreementMissing(empty, NOW, null, 'Thu 5 Nov, 12:00–13:00')).toEqual([
			'how they agreed',
			'when they agreed',
			'what they said, or where to find it',
			'a tick that they agreed to exactly Thu 5 Nov, 12:00–13:00'
		]);
		const full = {
			channel: 'instagram',
			when: '2026-10-29T09:30',
			evidence: 'DM 09:30',
			exact: true
		};
		expect(agreementMissing(full, NOW, null, 'x')).toEqual([]);
		expect(agreementMissing({ ...full, when: '2026-10-29T10:30' }, NOW, null, 'x')).toEqual([
			'a time that isn’t in the future'
		]);
		expect(
			agreementMissing(full, NOW, { at: '2026-10-29T09:45:00Z', label: 'the payment' }, 'x')
		).toEqual(['a time after the payment']);
	});
});

describe('reading', () => {
	it('finds the original handover and the latest dispatch', () => {
		const moved = order({
			appointment_history: [
				{
					command_id: 'c',
					kind: 'appointment_change',
					requested_by: 'chef',
					received_at: '2026-10-20T09:00:00Z',
					state: 'moved',
					reason: null,
					from: { date: '2026-10-28', id: 'a', starts_at: '2026-10-28T12:00:00Z', ends_at: '' },
					to: { date: '2026-10-29', id: 's1', starts_at: '2026-10-29T12:00:00Z', ends_at: '' }
				}
			]
		});
		expect(originalHandover(moved)).toBe('2026-10-28T12:00:00Z');
		expect(originalHandover(order())).toBe('2026-10-29T12:00:00Z');
		expect(lastDispatch(out())).toBe('2026-10-29T09:30:00Z');
		expect(lastDispatch(order())).toBeNull();
		expect(onItsWay(out())).toBe(true);
	});
});

describe('late payment', () => {
	it('is the chef’s, with the offer flow first', () => {
		const c = latePaymentChoices(late(), 'chef', NOW);
		expect(c).toEqual({ requested: true, other: true, honour: true, fullReturn: true, note: '' });
		for (const v of ['technical_support', 'fulfillment'] as const) {
			const n = latePaymentChoices(late(), v, NOW);
			expect(n.requested || n.other || n.honour || n.fullReturn).toBe(false);
		}
		expect(latePaymentChoices(order(), 'chef', NOW).other).toBe(false);
	});

	it('waits while a decision or answer is being processed', () => {
		const p = latePaymentChoices(
			late({ late_payment: { state: 'processing', offer: null, answer: null } }),
			'chef',
			NOW
		);
		expect(p.other).toBe(false);
		expect(p.note).toMatch(/being processed/);
		const cmd = latePaymentChoices(
			late({
				commands: [
					{
						id: 'c',
						kind: 'late_payment',
						requested_by: 'staff',
						received_at: '',
						state: 'prepared',
						outcome_status: null,
						decision: null
					}
				]
			}),
			'chef',
			NOW
		);
		expect(cmd.fullReturn).toBe(false);
	});

	it('offers the requested time only before the original handover (HONOUR_TOO_LATE)', () => {
		const after = latePaymentChoices(late(), 'chef', new Date('2026-11-05T12:00:00Z'));
		expect(after).toMatchObject({ requested: false, honour: false, other: true, fullReturn: true });
		expect(after.note).toMatch(/requested time has passed/);
	});

	it('does not offer the requested time again while that offer is open', () => {
		const o = late({
			late_payment: {
				state: 'offered',
				offer: {
					kind: 'requested',
					date: '2026-11-05',
					start: '2026-11-05T12:00:00Z',
					end: '2026-11-05T13:00:00Z',
					accept_until: '2026-11-05T12:00:00Z',
					acceptable: true
				},
				answer: null
			}
		});
		expect(latePaymentChoices(o, 'chef', NOW).requested).toBe(false);
		expect(latePaymentStatus(o, 'chef', NOW)[0]).toBe(
			'You offered Thu 5 Nov, 12:00–13:00 (their requested time). Waiting for the customer’s answer on their order page: they can accept until Thu 5 Nov, 12:00.'
		);
		expect(latePaymentStatus(o, 'technical_support', NOW)[0]).toMatch(/^Elina offered/);
	});

	it('says when an offer can no longer be accepted, lapsed or refused', () => {
		const o = late({
			late_payment: {
				state: 'offered',
				offer: {
					kind: 'other',
					date: '2026-11-06',
					start: '2026-11-06T12:00:00Z',
					end: '2026-11-06T13:00:00Z',
					accept_until: '2026-11-06T12:00:00Z',
					acceptable: false
				},
				answer: null
			}
		});
		const s = latePaymentStatus(o, 'chef', NOW);
		expect(s[0]).toMatch(
			/\(another date\)\. Waiting for the customer’s answer on their order page\.$/
		);
		expect(s[1]).toMatch(/can no longer be accepted/);
		const lapsed = late({
			late_payment: {
				state: 'waiting',
				offer: null,
				answer: {
					offer_id: 'f',
					answer: 'accept',
					received_at: '2026-10-29T09:00:00Z',
					state: 'lapsed',
					reason: 'slot_unavailable'
				}
			}
		});
		expect(latePaymentStatus(lapsed, 'chef', NOW).join(' ')).toMatch(
			/offer lapsed and nothing changed\. Offer again\./
		);
		const refused = late({
			late_payment: {
				state: 'waiting',
				offer: null,
				answer: {
					offer_id: 'f',
					answer: 'accept',
					received_at: '2026-10-29T09:00:00Z',
					state: 'refused',
					reason: 'holds_changed'
				}
			}
		});
		expect(latePaymentStatus(refused, 'chef', NOW).join(' ')).toMatch(
			/couldn’t be completed \(the day place or cakes changed\)/
		);
	});

	it('lists every offer and answer for the Record', () => {
		const lines = offerLines(
			late({
				late_payment_offers: [
					{
						id: 'f1',
						kind: 'requested',
						date: '2026-11-05',
						slot_id: 's1',
						start: '2026-11-05T12:00:00Z',
						end: '2026-11-05T13:00:00Z',
						offered_at: '2026-10-28T21:00:00Z',
						accept_until: '2026-11-05T12:00:00Z',
						state: 'withdrawn',
						answer: null
					},
					{
						id: 'f2',
						kind: 'other',
						date: '2026-11-06',
						slot_id: 's2',
						start: '2026-11-06T14:00:00Z',
						end: '2026-11-06T15:00:00Z',
						offered_at: '2026-10-29T08:00:00Z',
						accept_until: '2026-11-06T14:00:00Z',
						state: 'accepted',
						answer: {
							id: 'a',
							answer: 'accept',
							received_at: '2026-10-29T09:00:00Z',
							access: 'guest_link',
							command_id: 'c',
							lapsed: false
						}
					}
				]
			})
		);
		expect(lines.map((l) => l.text)).toEqual([
			'Offered Thu 5 Nov, 12:00–13:00 (their requested time) · replaced by a newer offer',
			'Offered Fri 6 Nov, 14:00–15:00 (another date) · accepted by the customer · accepted on the order page Thu 29 Oct, 09:00 (order link)'
		]);
	});
});

describe('delivery quote and waiver', () => {
	it('quotes an awaiting order before pay-by (original handover − 48 h)', () => {
		const c = quoteChoices(gk({ state: 'awaiting_quote', quote: null }), 'chef', NOW);
		expect(c).toMatchObject({ quote: true, replace: false, waive: false });
		expect(c.payBy).toBe('2026-10-31T12:00:00.000Z');
		const late48 = quoteChoices(
			gk({ state: 'awaiting_quote', quote: null }),
			'chef',
			new Date('2026-10-31T12:00:00Z')
		);
		expect(late48.quote).toBe(false);
		expect(late48.note).toMatch(/pay-by time .* has passed/);
	});

	it('replaces or waives an unpaid quote; waives after pay-by; never over a paid one', () => {
		expect(quoteChoices(gk(quoted), 'chef', NOW)).toMatchObject({
			quote: false,
			replace: true,
			waive: true,
			payBy: '2026-10-31T12:00:00Z'
		});
		const passed = quoteChoices(gk({ ...quoted, state: 'pay_by_passed' }), 'chef', NOW);
		expect(passed).toMatchObject({ replace: false, waive: true });
		expect(quoteChoices(gk({ state: 'paid', paid_minor: 12000 }), 'chef', NOW)).toMatchObject({
			quote: false,
			replace: false,
			waive: false
		});
		expect(quoteChoices(gk({ ...quoted, state: 'waived' }), 'chef', NOW)).toMatchObject({
			replace: true,
			waive: false
		});
	});

	it('waits while a delivery payment is checked (PAYMENT_IN_PROGRESS)', () => {
		const c = quoteChoices(gk({ ...quoted, state: 'payment_checking' }), 'chef', NOW);
		expect(c.replace || c.waive).toBe(false);
		expect(c.note).toMatch(/being checked/);
	});

	it('is the chef’s only, and never after a dispatch or GK can’t deliver', () => {
		for (const v of ['technical_support', 'fulfillment'] as const) {
			expect(quoteChoices(gk(quoted), v, NOW).replace).toBe(false);
		}
		expect(quoteChoices(out(), 'chef', NOW).waive).toBe(false);
		expect(
			quoteChoices(
				gk(quoted, {
					delivery_choice: {
						open: true,
						reason: 'not_possible',
						pending: false,
						not_possible: { at: '2026-10-29T08:00:00Z' }
					}
				}),
				'chef',
				NOW
			).replace
		).toBe(false);
		expect(quoteChoices(order(), 'chef', NOW).quote).toBe(false);
		expect(
			quoteChoices(gk(quoted, { lifecycle: 'cancelled', raw_lifecycle: 'cancelled' }), 'chef', NOW)
				.replace
		).toBe(false);
	});

	it('words the delivery for each view (money for the chef only)', () => {
		expect(deliveryStatus(gk(quoted), 'chef')[0]).toBe(
			'Quote £120 sent Wed 28 Oct, 09:00. Not paid yet: the customer can pay on their order page until Sat 31 Oct, 12:00 (48 hours before the original delivery time).'
		);
		const support = gk({ ...quoted, quote: { ...quoted.quote, amount_minor: undefined } });
		expect(deliveryStatus(support, 'technical_support')[0]).not.toMatch(/£/);
		expect(deliveryStatus(gk({ ...quoted, state: 'pay_by_passed' }), 'chef')[0]).toMatch(
			/wasn’t paid by Sat 31 Oct, 12:00\. GK won’t deliver unpaid/
		);
		expect(deliveryStatus(gk({ state: 'awaiting_quote' }), 'fulfillment')).toEqual([
			'Waiting for Elina’s delivery price.'
		]);
	});

	it('lists the quote history with notes and distances for the chef', () => {
		const h = gk(quoted, {
			delivery_history: [
				{
					id: 'q0',
					order_version: 3,
					quoted_at: '2026-10-20T09:00:00Z',
					pay_by: '2026-10-31T12:00:00Z',
					current: false,
					amount_minor: 15000,
					currency: 'GBP',
					note: 'Zone 4',
					waiver: null,
					payments: [
						{
							collection_id: 'c',
							state: 'succeeded',
							outcome: 'unmatched',
							reason: 'quote_replaced'
						}
					]
				},
				{
					id: 'q1',
					order_version: 5,
					quoted_at: '2026-10-28T09:00:00Z',
					pay_by: '2026-10-31T12:00:00Z',
					current: true,
					amount_minor: 12000,
					currency: 'GBP',
					note: null,
					waiver: {
						id: 'w',
						at: '2026-10-28T10:00:00Z',
						distance_m: 8208,
						distance_source: 'Google Maps'
					},
					payments: []
				}
			]
		});
		expect(quoteLines(h, 'chef').map((l) => l.text)).toEqual([
			'Quote £150 · pay by Sat 31 Oct, 12:00 · replaced · note “Zone 4” · payment couldn’t be used (the quote had been replaced)',
			'Quote £120 · pay by Sat 31 Oct, 12:00 · current · waived Wed 28 Oct, 10:00 · 8.2 km (8,208 m) (Google Maps)'
		]);
		expect(quoteLines(h, 'technical_support')[0].text).not.toMatch(/Zone 4/);
	});
});

describe('GK can’t deliver', () => {
	it('is recorded once by the chef, before any dispatch and without a payment in flight', () => {
		expect(canRecordNotPossible(gk(quoted), 'chef')).toBe(true);
		expect(canRecordNotPossible(gk({ state: 'paid', paid_minor: 12000 }), 'chef')).toBe(true);
		expect(canRecordNotPossible(gk(quoted), 'fulfillment')).toBe(false);
		expect(canRecordNotPossible(gk({ ...quoted, state: 'payment_checking' }), 'chef')).toBe(false);
		expect(canRecordNotPossible(out(), 'chef')).toBe(false);
		expect(canRecordNotPossible(order(), 'chef')).toBe(false);
		expect(
			canRecordNotPossible(
				gk(quoted, {
					delivery_choice: {
						open: true,
						reason: 'not_possible',
						pending: false,
						not_possible: { at: 'x' }
					}
				}),
				'chef'
			)
		).toBe(false);
	});

	it('states the consequences, with the paid delivery coming back in full', () => {
		expect(notPossibleConfirm(gk({ state: 'paid', paid_minor: 12000 }))).toMatch(
			/Nothing is refunded now\. The £120 delivery payment comes back in full whatever they choose\. The order can’t go out for delivery afterwards, and this can’t be undone\.$/
		);
		expect(notPossibleConfirm(gk(quoted))).toMatch(/The unpaid quote is closed\./);
	});

	it('shows the decision (reason to the chef) and the customer’s choice', () => {
		const o = gk(
			{ state: 'cancelled_by_choice', paid_minor: 12000 },
			{
				delivery_choice: {
					open: false,
					reason: 'not_possible',
					pending: false,
					not_possible: { at: '2026-10-28T08:00:00Z', reason: 'Van in repair' },
					chosen: {
						choice: 'collection',
						state: 'chosen',
						to: {
							fulfillment_mode: 'collection',
							date: '2026-11-02',
							id: 's',
							starts_at: '2026-11-02T14:00:00Z',
							ends_at: '2026-11-02T15:00:00Z'
						},
						delivery_refund_minor: 12000
					}
				}
			}
		);
		expect(choiceStatus(o, 'chef')).toEqual([
			'GK can’t deliver this order (recorded Wed 28 Oct, 08:00). Your reason: “Van in repair” (only you see it).',
			'The customer chose collect it · Mon 2 Nov, 14:00–15:00 · confirmed. The £120 delivery payment is being returned in full.'
		]);
		expect(choiceStatus(o, 'technical_support').join(' ')).not.toMatch(/Van|£/);
		const open = gk(quoted, {
			delivery_choice: { open: true, reason: 'pay_by_passed', pending: false, chosen: null }
		});
		expect(choiceStatus(open, 'chef')[0]).toMatch(/^Delivery wasn’t paid in time/);
	});
});

describe('failed delivery', () => {
	it('is the chef’s while the delivery is on its way; nobody available from its start', () => {
		expect(failureChoices(out(), 'chef', NOW)).toEqual({
			ourSide: true,
			nobody: true,
			nobodyFrom: null
		});
		expect(failureChoices(out(), 'chef', new Date('2026-10-29T09:45:00Z'))).toEqual({
			ourSide: true,
			nobody: false,
			nobodyFrom: '2026-10-29T10:00:00Z'
		});
		expect(failureChoices(out(), 'fulfillment', NOW)).toBeNull();
		expect(failureChoices(gk({ state: 'paid', paid_minor: 12000 }), 'chef', NOW)).toBeNull();
	});

	it('not again for the same dispatch after an our-side failure, nor after a reschedule', () => {
		const failed = out({
			fulfillment: { state: 'exception', at: '2026-10-29T09:50:00Z', pending: null },
			delivery_failure: {
				cause: 'our_side',
				at: '2026-10-29T09:50:00Z',
				state: 'recorded',
				open: true,
				pending: false
			}
		});
		expect(failureChoices(failed, 'chef', NOW)).toBeNull();
		const exceptionAfterDispatch = out({
			fulfillment: { state: 'exception', at: '2026-10-29T09:50:00Z', pending: null }
		});
		expect(failureChoices(exceptionAfterDispatch, 'chef', NOW)?.ourSide).toBe(true);
	});

	it('states each cause’s consequences', () => {
		expect(failureConfirm(out(), 'our_side')).toMatch(
			/deliver it again .* or collect it today, with the £120 delivery payment returned in full/
		);
		expect(failureConfirm(out(), 'nobody_available')).toMatch(
			/50% of what they paid for the cake \(anything already refunded counts towards it\).*The £120 delivery payment is kept, because the delivery was attempted\. This can’t be undone\./
		);
	});

	it('shows the failure, the choice and the money for the chef', () => {
		const chosen = out({
			delivery_failure: {
				cause: 'our_side',
				at: '2026-10-29T09:50:00Z',
				state: 'recorded',
				open: false,
				pending: false,
				chosen: { choice: 'redelivery', state: 'chosen' },
				redelivery_time_set: false
			}
		});
		expect(failureStatus(chosen, 'chef')[1]).toMatch(/set it under Handover/);
		const nobody = out({
			delivery_failure: {
				cause: 'nobody_available',
				at: '2026-10-29T10:20:00Z',
				state: 'no_show',
				open: false,
				pending: false,
				cake_refund_minor: 4500,
				delivery_kept_minor: 12000
			}
		});
		expect(failureStatus(nobody, 'chef')).toEqual([
			'Nobody was available to take the delivery (Thu 29 Oct, 10:20).',
			'50% of the cake price back: £45, requested automatically. Delivery payment kept: £120, because the delivery was attempted.'
		]);
		expect(failureStatus(nobody, 'technical_support').join(' ')).not.toMatch(/£/);
	});
});

describe('appointment change', () => {
	it('is the chef’s on an open order with nothing unresolved', () => {
		expect(appointmentChoice(order(), 'chef')).toEqual({
			allowed: true,
			redelivery: false,
			note: ''
		});
		expect(appointmentChoice(order(), 'fulfillment').allowed).toBe(false);
		expect(appointmentChoice(order(), 'technical_support').allowed).toBe(false);
		expect(
			appointmentChoice(order({ lifecycle: 'completed', raw_lifecycle: 'completed' }), 'chef')
				.allowed
		).toBe(false);
		expect(appointmentChoice(late(), 'chef').allowed).toBe(false);
		expect(
			appointmentChoice(
				order({
					appointment_change: {
						state: 'processing',
						requested_by: 'customer',
						to: { date: '', id: '', starts_at: '', ends_at: '' }
					}
				}),
				'chef'
			).allowed
		).toBe(false);
	});

	it('not while a GK delivery is on its way or its failure choice is open', () => {
		const a = appointmentChoice(out(), 'chef');
		expect(a.allowed).toBe(false);
		expect(a.note).toMatch(/on its way/);
		const open = out({
			fulfillment: { state: 'exception', at: '2026-10-29T09:50:00Z', pending: null },
			delivery_failure: {
				cause: 'our_side',
				at: '2026-10-29T09:50:00Z',
				state: 'recorded',
				open: true,
				pending: false
			}
		});
		expect(appointmentChoice(open, 'chef').allowed).toBe(false);
	});

	it('sets the redelivery time after a secured redelivery choice', () => {
		const chosen = out({
			fulfillment: { state: 'exception', at: '2026-10-29T09:50:00Z', pending: null },
			delivery_failure: {
				cause: 'our_side',
				at: '2026-10-29T09:50:00Z',
				state: 'recorded',
				open: false,
				pending: false,
				chosen: { choice: 'redelivery', state: 'chosen' },
				redelivery_time_set: false
			}
		});
		expect(appointmentChoice(chosen, 'chef')).toEqual({
			allowed: true,
			redelivery: true,
			note: ''
		});
		const processing = out({
			fulfillment: { state: 'exception', at: '2026-10-29T09:50:00Z', pending: null },
			delivery_failure: {
				cause: 'our_side',
				at: '2026-10-29T09:50:00Z',
				state: 'recorded',
				open: false,
				pending: true,
				chosen: { choice: 'redelivery', state: 'processing' }
			}
		});
		expect(appointmentChoice(processing, 'chef').allowed).toBe(false);
	});
});

describe('the schedule', () => {
	const day = (over: Partial<ScheduleChoices> = {}): ScheduleChoices => ({
		date: '2026-10-29',
		day_status: 'open',
		slots: [
			{
				id: 's0',
				starts_at: '2026-10-29T09:00:00Z',
				ends_at: '2026-10-29T10:00:00Z',
				availability: 'unavailable',
				reason_code: 'lead_time'
			},
			{
				id: 's1',
				starts_at: '2026-10-29T12:00:00Z',
				ends_at: '2026-10-29T13:00:00Z',
				availability: 'unavailable',
				reason_code: 'lead_time'
			},
			{
				id: 's2',
				starts_at: '2026-10-29T14:00:00Z',
				ends_at: '2026-10-29T15:00:00Z',
				availability: 'unavailable',
				reason_code: 'full'
			}
		],
		...over
	});

	it('offers the starts still ahead, never the order’s own time; fullness is a hint', () => {
		const { options, note } = slotOptions(day(), order(), NOW, 'appointment');
		expect(note).toBe('');
		expect(options.map((o) => [o.label, o.disabled, o.hint])).toEqual([
			['12:00–13:00', true, 'the current time'],
			['14:00–15:00', false, 'day full online · an extra place']
		]);
		expect(slotOptions(day(), order(), NOW, 'offer').options[1].hint).toBe(
			'day full online · may be refused'
		);
	});

	it('never a closed date; says when nothing is left', () => {
		const closed = slotOptions(day({ day_status: 'closed' }), order(), NOW, 'offer');
		expect(closed.options.every((o) => o.disabled)).toBe(true);
		expect(closed.note).toBe('Thu 29 Oct is a closed date. Choose another day.');
		expect(slotOptions(day({ slots: [] }), order(), NOW, 'offer').note).toMatch(/no opening times/);
		expect(slotOptions(day(), order(), new Date('2026-10-29T16:00:00Z'), 'offer').note).toMatch(
			/No opening times are left/
		);
	});

	it('reads the public schedule for the order’s mode and cake count', async () => {
		let url = '';
		let auth: string | undefined;
		const api: StaffApi = {
			baseUrl: 'https://api.test',
			token: 'tok',
			fetch: (async (u: string, init: RequestInit) => {
				url = u;
				auth = (init.headers as Record<string, string>).Authorization;
				return new Response(JSON.stringify(day()), { status: 200 });
			}) as unknown as typeof fetch
		};
		await scheduleDay(api, order(), '2026-11-02');
		expect(url).toBe(
			'https://api.test/api/v1/schedule?date=2026-11-02&fulfillment_mode=collection&purpose=new_purchase&quantity=2'
		);
		expect(auth).toBeUndefined();
	});
});

describe('commands', () => {
	const agreed = { channel: 'instagram', agreedAt: '2026-10-29T09:30:00Z', evidence: ' DM 09:30 ' };
	const cases: [DecisionInput, string, Record<string, unknown>, boolean][] = [
		[
			{ d: 'offer', kind: 'requested' },
			'/late-payment/offer',
			{ expected_version: 7, kind: 'requested' },
			false
		],
		[
			{ d: 'offer', kind: 'other', date: '2026-11-06', slotId: 's2' },
			'/late-payment/offer',
			{ expected_version: 7, kind: 'other', target_date: '2026-11-06', target_slot_id: 's2' },
			false
		],
		[
			{ d: 'honour', ...agreed },
			'/late-payment',
			{
				expected_version: 7,
				decision: 'honour',
				customer_agreement: {
					channel: 'instagram',
					agreed_at: '2026-10-29T09:30:00Z',
					evidence_ref: 'DM 09:30'
				}
			},
			true
		],
		[{ d: 'full_return' }, '/late-payment', { expected_version: 7, decision: 'full_return' }, true],
		[
			{ d: 'quote', amountMinor: 12050, note: '' },
			'/delivery-quote',
			{ expected_version: 7, amount_minor: 12050 },
			false
		],
		[
			{ d: 'quote', amountMinor: 12000, note: ' Zone 4 ' },
			'/delivery-quote',
			{ expected_version: 7, amount_minor: 12000, note: 'Zone 4' },
			false
		],
		[
			{ d: 'waiver', distanceM: 8208, source: ' Google Maps ' },
			'/delivery-waiver',
			{ expected_version: 7, distance_m: 8208, distance_source: 'Google Maps' },
			false
		],
		[{ d: 'not_possible', reason: '  ' }, '/delivery-not-possible', { expected_version: 7 }, false],
		[
			{ d: 'not_possible', reason: 'Van' },
			'/delivery-not-possible',
			{ expected_version: 7, reason: 'Van' },
			false
		],
		[
			{ d: 'failed_our_side', reason: '' },
			'/delivery-failed',
			{ expected_version: 7, cause: 'our_side' },
			false
		],
		[
			{ d: 'failed_nobody', reason: 'Rang twice' },
			'/delivery-failed',
			{ expected_version: 7, cause: 'nobody_available', reason: 'Rang twice' },
			true
		],
		[
			{
				d: 'appointment',
				date: '2026-11-03',
				slotId: 's9',
				...agreed,
				feasibility: ' Fine, cake keeps '
			},
			'/appointment',
			{
				expected_version: 7,
				target_date: '2026-11-03',
				target_slot_id: 's9',
				customer_agreement: {
					channel: 'instagram',
					agreed_at: '2026-10-29T09:30:00Z',
					evidence_ref: 'DM 09:30'
				},
				feasibility_note: 'Fine, cake keeps'
			},
			true
		]
	];

	it('sends exactly the API’s body at the version shown (no nulls, no extras)', () => {
		for (const [input, path, body, prot] of cases) {
			const r = decisionRequest(order(), input);
			expect(r.path).toBe(`/api/v1/admin/orders/${ID}${path}`);
			expect(r.body).toEqual(body);
			expect(r.protectedCommand).toBe(prot);
			expect(JSON.stringify(r.body)).not.toMatch(/null/);
		}
	});

	it('keys follow the version and exactly what is sent', () => {
		const o = order();
		const a = decisionSlot(o, { d: 'quote', amountMinor: 12000, note: '' });
		expect(decisionSlot(o, { d: 'quote', amountMinor: 12000, note: ' ' })).toBe(a);
		expect(decisionSlot(o, { d: 'quote', amountMinor: 12500, note: '' })).not.toBe(a);
		expect(
			decisionSlot(order({ version: 8 }), { d: 'quote', amountMinor: 12000, note: '' })
		).not.toBe(a);
		expect(decisionSlot(o, { d: 'failed_our_side', reason: '' })).not.toBe(
			decisionSlot(o, { d: 'failed_nobody', reason: '' })
		);
		const key = decisionKey(o, 'failed_our_side', 'abcd1234');
		expect(key).toBe(`chef-failed_our_side-${ID}-v7-abcd1234`);
		expect(key.length).toBeLessThanOrEqual(128);
		expect(/^[\x21-\x7e]+$/.test(key)).toBe(true);
	});

	it('posts with the key and reads 200, 202 and “unchanged”', async () => {
		const seen: { url: string; body: unknown; key: string }[] = [];
		let reply: unknown = { order_id: ID, quote_id: 'q' };
		let status = 200;
		const api: StaffApi = {
			baseUrl: 'https://api.test',
			token: 'tok',
			fetch: (async (u: string, init: RequestInit) => {
				seen.push({
					url: u,
					body: JSON.parse(String(init.body)),
					key: (init.headers as Record<string, string>)['Idempotency-Key']
				});
				return new Response(JSON.stringify(reply), { status });
			}) as unknown as typeof fetch
		};
		expect(
			await sendDecision(api, order(), { d: 'quote', amountMinor: 12000, note: '' }, 'k1')
		).toEqual({
			kind: 'done'
		});
		expect(seen[0]).toEqual({
			url: `https://api.test/api/v1/admin/orders/${ID}/delivery-quote`,
			body: { expected_version: 7, amount_minor: 12000 },
			key: 'k1'
		});
		reply = {
			order_id: ID,
			operation_id: 'op',
			state: 'appointment_change_pending',
			poll_after_seconds: 1
		};
		status = 202;
		const input: DecisionInput = {
			d: 'appointment',
			date: '2026-11-03',
			slotId: 's9',
			...agreed,
			feasibility: 'ok'
		};
		expect(await sendDecision(api, order(), input, 'k2')).toEqual({
			kind: 'accepted',
			accepted: reply
		});
		reply = { order_id: ID, state: 'unchanged' };
		status = 200;
		expect(await sendDecision(api, order(), input, 'k3')).toEqual({ kind: 'unchanged' });
		reply = { order_id: ID, failure_id: 'f' };
		expect(await sendDecision(api, out(), { d: 'failed_our_side', reason: '' }, 'k4')).toEqual({
			kind: 'done'
		});
	});

	it('settles each protected decision from a fresh read', () => {
		const sec = (outcome: string | null) => [
			{
				id: 'op',
				kind: 'x',
				requested_by: 'staff',
				received_at: '',
				state: 'secured',
				outcome_status: outcome,
				decision: null
			}
		];
		const prep = [
			{
				id: 'op',
				kind: 'x',
				requested_by: 'staff',
				received_at: '',
				state: 'prepared',
				outcome_status: null,
				decision: null
			}
		];
		expect(decisionSettled(late({ commands: prep }), 'honour', 'op')).toBe('pending');
		expect(decisionSettled(order({ commands: sec('confirmed') }), 'honour', 'op')).toBe('done');
		expect(
			decisionSettled(
				order({ lifecycle: 'cancelled', commands: sec('cancelled') }),
				'full_return',
				'op'
			)
		).toBe('returned');
		expect(decisionSettled(order({ lifecycle: 'cancellation_pending' }), 'full_return', 'op')).toBe(
			'pending'
		);
		expect(decisionSettled(late({ commands: sec('refused') }), 'honour', 'op')).toBe('refused');
		expect(
			decisionSettled(late({ late_payment: { state: 'processing', offer: null } }), 'honour', 'op')
		).toBe('pending');
		const moved = (state: string) =>
			order({
				appointment_history: [
					{
						command_id: 'op',
						kind: 'appointment_change',
						requested_by: 'chef',
						received_at: '',
						state,
						reason: null,
						from: { date: '', id: '', starts_at: '', ends_at: '' },
						to: { date: '', id: '', starts_at: '', ends_at: '' }
					}
				]
			});
		expect(decisionSettled(moved('moved'), 'appointment', 'op')).toBe('done');
		expect(decisionSettled(moved('refused'), 'appointment', 'op')).toBe('refused');
		expect(decisionSettled(moved('processing'), 'appointment', 'op')).toBe('pending');
		expect(
			decisionSettled(
				out({ lifecycle: 'completed', fulfillment: { state: 'no_show', at: '', pending: null } }),
				'failed_nobody',
				'op'
			)
		).toBe('done');
		expect(
			decisionSettled(
				out({ fulfillment: { state: 'out_for_delivery', at: '', pending: 'no_show' } }),
				'failed_nobody',
				'op'
			)
		).toBe('pending');
	});

	it('polls until settled, or stays pending after its reads', async () => {
		const reads = [late({ late_payment: { state: 'processing', offer: null } }), order()];
		const r = await pollDecision(
			async () => reads.shift() ?? order(),
			'honour',
			{ operation_id: 'op', poll_after_seconds: 1 },
			{ wait: async () => {} }
		);
		expect(r.outcome).toBe('done');
		const still = await pollDecision(
			async () => late({ late_payment: { state: 'processing', offer: null } }),
			'honour',
			{ operation_id: 'op', poll_after_seconds: 1 },
			{ tries: 2, wait: async () => {} }
		);
		expect(still.outcome).toBe('pending');
	});

	it('words what was done', () => {
		expect(decisionDone({ d: 'offer', kind: 'requested' })).toMatch(
			/Nothing changes unless they accept it/
		);
		expect(decisionDone({ d: 'full_return' }, 'returned')).toMatch(
			/everything paid is being returned/
		);
		expect(decisionDone({ d: 'failed_nobody', reason: '' })).toMatch(/not collected/);
	});
});

describe('refusals in plain words', () => {
	it('maps each code, never showing the code itself', () => {
		const cases: [ApiError, Parameters<typeof decisionRefusal>[1], RegExp][] = [
			[err(409, 'HONOUR_TOO_LATE'), 'offer', /requested time has passed/],
			[err(409, 'SLOT_UNAVAILABLE'), 'offer', /can’t use an extra place/],
			[err(409, 'SLOT_UNAVAILABLE'), 'appointment', /past a ready cake’s use-by/],
			[err(409, 'PAYMENT_IN_PROGRESS'), 'quote', /delivery payment is in progress/],
			[err(409, 'DELIVERY_PAY_BY_PASSED'), 'quote', /pay-by time/],
			[err(409, 'WAIVER_NOT_ELIGIBLE'), 'waiver', /free-delivery code/],
			[err(409, 'MOVE_NOT_ELIGIBLE'), 'appointment', /can’t be changed now/],
			[err(409, 'TRANSITION_NOT_ALLOWED'), 'failed_our_side', /failed delivery can’t be recorded/],
			[err(409, 'HANDOVER_NOT_DUE'), 'failed_nobody', /hasn’t started yet/],
			[err(409, 'CONFLICT'), 'not_possible', /may already be recorded/],
			[err(403, 'FORBIDDEN'), 'quote', /Only Elina \(chef\)/],
			[err(422, 'INVALID_INPUT'), 'honour', /after the payment/],
			[err(422, 'INVALID_INPUT'), 'appointment', /after the order was placed/]
		];
		for (const [e, d, re] of cases) {
			const r = decisionRefusal(e, d);
			expect(r.text).toMatch(re);
			expect(r.text).not.toMatch(/[A-Z]{3,}_[A-Z]/);
		}
	});

	it('keeps the generic rules: stale reloads, a freeze and a lost answer retry with the key', () => {
		expect(decisionRefusal(err(409, 'STALE_VERSION'), 'quote')).toMatchObject({ stale: true });
		expect(decisionRefusal(err(503, 'UNAVAILABLE', true), 'quote')).toMatchObject({
			retry: true,
			reload: false
		});
		expect(decisionRefusal(new ApiError(0, { code: 'NETWORK' }), 'offer')).toMatchObject({
			retry: true
		});
		expect(decisionRefusal(err(401, 'UNAUTHENTICATED'), 'offer')).toMatchObject({ signIn: true });
		expect(decisionRefusal(err(409, 'SLOT_UNAVAILABLE'), 'offer').reload).toBe(false);
		expect(decisionRefusal(err(409, 'KEY_REUSED'), 'quote').text).toMatch(/different details/);
	});
});
