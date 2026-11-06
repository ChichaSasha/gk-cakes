import { describe, expect, it } from 'vitest';
import { ApiError } from '../api';
import type { StaffApi } from './catalogue';
import {
	asksFirst,
	availableSteps,
	banners,
	cakeTitle,
	cancelledText,
	commandKey,
	confirmText,
	dayLabel,
	deliveryText,
	detailStatus,
	dispatchBlock,
	doneText,
	historyLines,
	keySlot,
	latePaymentCard,
	moneyFacts,
	orderIdFrom,
	pendingStep,
	pollStep,
	progressSteps,
	reasonProblem,
	refundFacts,
	refundPartRows,
	refusalOf,
	sendStep,
	settled,
	staffOrder,
	stepLabel,
	unresolvedChange,
	when,
	type StaffOrderDetail
} from './order-detail';

const ID = 'a1b2c3d4-0000-4000-8000-000000000001';
const NOW = new Date('2026-10-29T10:00:00Z'); // Thu 29 Oct, 10:00 London (GMT)

/** A chef's view of a confirmed collection order (handover Thu 29 Oct, 12:00). */
const chef = (over: Partial<StaffOrderDetail> = {}): StaffOrderDetail => ({
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
	contact_email: 'mia@example.com',
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
	production: {
		state: 'scheduled',
		scheduled_at: '2026-10-14T09:00:00Z',
		started_at: null,
		observed_at: null,
		source: null
	},
	payment_requirement: 'full',
	full_refund_deadline_at: '2026-10-24T11:00:00Z',
	paid_at: '2026-10-12T09:00:00Z',
	free_confirmed_at: null,
	payment: {
		id: 'p',
		state: 'succeeded',
		amount_minor: 9000,
		currency: 'GBP',
		requested_at: '2026-10-12T08:58:00Z',
		expires_at: null
	},
	subtotal_minor: 10000,
	discount_minor: 1000,
	total_minor: 9000,
	currency: 'GBP',
	refund: null,
	attention: [],
	needs_attention: false,
	commands: [],
	fulfillment: { state: null, at: null, pending: null },
	fulfillment_history: [],
	appointment_history: [],
	...over
});

/** Fulfilment's keys only (no money, contact, attention or commands). */
const helper = (over: Partial<StaffOrderDetail> = {}): StaffOrderDetail => {
	const c = chef(over) as unknown as Record<string, unknown>;
	for (const k of [
		'recovery_state',
		'contact_email',
		'payment_requirement',
		'full_refund_deadline_at',
		'paid_at',
		'free_confirmed_at',
		'payment',
		'subtotal_minor',
		'discount_minor',
		'total_minor',
		'currency',
		'refund',
		'attention',
		'needs_attention',
		'commands'
	]) {
		delete c[k];
	}
	return c as unknown as StaffOrderDetail;
};

/** Technical support: no money or contact details. */
const support = (over: Partial<StaffOrderDetail> = {}): StaffOrderDetail => {
	const c = chef(over) as unknown as Record<string, unknown>;
	for (const k of [
		'contact_email',
		'subtotal_minor',
		'discount_minor',
		'total_minor',
		'currency'
	]) {
		delete c[k];
	}
	if (c.payment) {
		const p = { ...(c.payment as Record<string, unknown>) };
		delete p.amount_minor;
		delete p.currency;
		c.payment = p;
	}
	return c as unknown as StaffOrderDetail;
};

const ready = (over: Partial<StaffOrderDetail> = {}) =>
	chef({
		lifecycle: 'ready',
		raw_lifecycle: 'ready',
		production: {
			state: 'started',
			scheduled_at: '2026-10-14T09:00:00Z',
			started_at: '2026-10-14T09:00:00Z',
			observed_at: '2026-10-14T09:00:05Z',
			source: 'timer'
		},
		fulfillment: { state: 'awaiting_handover', at: '2026-10-29T08:00:00Z', pending: null },
		fulfillment_history: [
			{
				action: 'ready',
				from: 'awaiting_handover',
				to: 'awaiting_handover',
				received_at: '2026-10-29T08:00:00Z',
				command_id: null,
				state: 'done',
				reason: null
			}
		],
		...over
	});

const err = (status: number, code: string) =>
	new ApiError(status, { code, message: 'm', request_id: 'r', retryable: status >= 500 });

describe('orderIdFrom', () => {
	it('accepts an order ID (lower-cased) and refuses anything else', () => {
		expect(orderIdFrom(ID.toUpperCase())).toBe(ID);
		expect(orderIdFrom('not-an-id')).toBeNull();
		expect(orderIdFrom(`${ID}/x`)).toBeNull();
	});
});

describe('staffOrder', () => {
	it('reads /api/v1/admin/orders/{id} with the session token', async () => {
		let url = '';
		let auth = '';
		const api: StaffApi = {
			baseUrl: 'https://api.example.com',
			token: 't0k',
			fetch: (async (u: string, init: RequestInit) => {
				url = u;
				auth = (init.headers as Record<string, string>).Authorization;
				return new Response(JSON.stringify(chef()), { status: 200 });
			}) as typeof fetch
		};
		const o = await staffOrder(api, ID);
		expect(o.id).toBe(ID);
		expect(url).toBe(`https://api.example.com/api/v1/admin/orders/${ID}`);
		expect(auth).toBe('Bearer t0k');
		expect(() => staffOrder(api, '../me')).toThrow();
	});
});

describe('formatting', () => {
	it('writes London times as the sketch does', () => {
		expect(when('2026-10-22T09:20:00Z')).toBe('Thu 22 Oct, 10:20');
		expect(when('2026-10-29T12:00:00Z')).toBe('Thu 29 Oct, 12:00');
		expect(dayLabel('2026-10-30')).toBe('Friday 30 October');
		expect(cakeTitle(chef())).toBe('Pink piping');
	});
});

describe('availableSteps: role gating', () => {
	it('gives technical support no steps in any state', () => {
		for (const o of [support(), support({ lifecycle: 'in_production' }), ready()]) {
			expect(availableSteps(o, 'technical_support', NOW).actions).toEqual([]);
		}
	});

	it('lets chef and fulfilment start a confirmed order, and only that', () => {
		expect(availableSteps(chef(), 'chef', NOW).actions).toEqual(['start']);
		expect(availableSteps(helper(), 'fulfillment', NOW).actions).toEqual(['start']);
	});

	it('keeps the no-show to the chef, from the start of the time', () => {
		const at = new Date('2026-10-29T12:00:00Z');
		expect(availableSteps(ready(), 'chef', at).actions).toEqual([
			'handover',
			'no_show',
			'delivery_exception'
		]);
		expect(availableSteps(helper(ready()), 'fulfillment', at).actions).toEqual([
			'handover',
			'delivery_exception'
		]);
	});

	it('says when a no-show can be recorded before the time starts', () => {
		const s = availableSteps(ready(), 'chef', NOW);
		expect(s.actions).toEqual(['handover', 'delivery_exception']);
		expect(s.note).toMatch(/from 12:00 on Thu 29 Oct/);
		expect(availableSteps(helper(ready()), 'fulfillment', NOW).note).toBe('');
	});
});

describe('availableSteps: eligibility (README tables)', () => {
	it('offers Mark ready in production, whoever started it', () => {
		expect(availableSteps(chef({ lifecycle: 'in_production' }), 'chef', NOW).actions).toEqual([
			'ready'
		]);
	});

	it('offers nothing before confirmation or after completion or cancellation', () => {
		for (const lifecycle of [
			'awaiting_payment',
			'confirmation_pending',
			'paid_awaiting_agreement',
			'completed',
			'cancellation_pending',
			'cancelled'
		]) {
			expect(availableSteps(chef({ lifecycle }), 'chef', NOW).actions).toEqual([]);
		}
	});

	it('does not offer a start once production has started', () => {
		const o = chef({ production: { ...chef().production, state: 'started' } });
		expect(availableSteps(o, 'chef', NOW).actions).toEqual([]);
	});

	it('waits while a protected change is unresolved', () => {
		const busy = [
			chef({
				commands: [
					{
						id: 'c',
						kind: 'cancel',
						requested_by: 'staff',
						received_at: '2026-10-29T09:00:00Z',
						state: 'prepared',
						outcome_status: null,
						decision: null
					}
				]
			}),
			chef({ recovery_state: 'committed' }),
			helper({
				appointment_change: {
					state: 'processing',
					requested_by: 'customer',
					to: { date: '2026-10-29', id: 's2', starts_at: 'x', ends_at: 'y' }
				}
			})
		];
		for (const o of busy) {
			const s = availableSteps(o, o.commands ? 'chef' : 'fulfillment', NOW);
			expect(s.actions).toEqual([]);
			expect(s.note).toMatch(/still being processed/);
		}
	});

	it('shows nothing (and no note) while a handover is being recorded', () => {
		const o = ready({ fulfillment: { state: 'awaiting_handover', at: null, pending: 'handover' } });
		expect(unresolvedChange(o)).toBe(true);
		expect(availableSteps(o, 'chef', NOW)).toEqual({ actions: [], note: '' });
	});

	it('hands over an exception (collection) but not a completed order', () => {
		const ex = ready({ fulfillment: { state: 'exception', at: null, pending: null } });
		expect(availableSteps(ex, 'chef', NOW).actions).toContain('handover');
	});

	it('reads a NULL fulfilment state as awaiting handover', () => {
		const o = ready({ fulfillment: null });
		expect(availableSteps(o, 'chef', NOW).actions[0]).toBe('handover');
	});

	describe('GK delivery', () => {
		const gk = (over: Partial<StaffOrderDetail> = {}) =>
			ready({
				fulfillment_mode: 'gk_delivery',
				delivery: { state: 'paid', paid_minor: 12000 },
				...over
			});

		it('goes out once delivery is paid or waived, never with a no-show button', () => {
			expect(availableSteps(gk(), 'chef', new Date('2026-10-29T13:00:00Z')).actions).toEqual([
				'dispatch',
				'delivery_exception'
			]);
			expect(
				availableSteps(gk({ delivery: { state: 'waived', waived: { at: 'x' } } }), 'chef', NOW)
					.actions
			).toContain('dispatch');
		});

		it('explains why it cannot go out', () => {
			const unpaid = availableSteps(gk({ delivery: { state: 'quoted' } }), 'fulfillment', NOW);
			expect(unpaid.actions).toEqual(['delivery_exception']);
			expect(unpaid.note).toBe('Out for delivery is available once delivery is paid.');
			expect(
				dispatchBlock(
					gk({ delivery_choice: { open: true, reason: 'not_possible', pending: false } })
				)
			).toMatch(/can’t deliver/);
			expect(
				dispatchBlock(
					gk({
						delivery_failure: {
							cause: 'our_side',
							at: 'x',
							state: 'recorded',
							open: true,
							pending: false
						}
					})
				)
			).toMatch(/hasn’t chosen/);
			expect(
				dispatchBlock(
					gk({
						delivery_failure: {
							cause: 'our_side',
							at: 'x',
							state: 'recorded',
							open: false,
							pending: false,
							chosen: { choice: 'redelivery', state: 'chosen' },
							redelivery_time_set: false
						}
					})
				)
			).toMatch(/Agree the new time/);
		});

		it('is handed over only when out for delivery', () => {
			const out = gk({ fulfillment: { state: 'out_for_delivery', at: 'x', pending: null } });
			expect(availableSteps(out, 'fulfillment', NOW).actions).toEqual([
				'handover',
				'delivery_exception'
			]);
			expect(stepLabel('handover', out)).toBe('Delivered · hand over');
			expect(stepLabel('delivery_exception', out)).toBe('Delivery problem…');
		});
	});
});

describe('wording', () => {
	it('labels the steps with the sketch’s words', () => {
		expect(stepLabel('start', chef())).toBe('Start making');
		expect(stepLabel('ready', chef())).toBe('Mark ready');
		expect(stepLabel('handover', chef())).toBe('Hand over');
		expect(stepLabel('delivery_exception', chef())).toBe('Record a problem…');
		expect(asksFirst('handover') && asksFirst('no_show') && asksFirst('delivery_exception')).toBe(
			true
		);
		expect(asksFirst('start') || asksFirst('ready') || asksFirst('dispatch')).toBe(false);
	});

	it('names who records the handover and what it does', () => {
		expect(confirmText('handover', ready(), 'fulfillment')).toBe(
			'Hand over order a1b2c3d4 to Mia Example · Collect · St Katharine Docks. Recorded as you (Fulfilment). This completes the order.'
		);
		expect(confirmText('no_show', ready(), 'chef')).toMatch(/50% of what they paid/);
		expect(doneText('no_show', 'noshow')).toMatch(/not collected/);
	});

	it('checks a problem’s reason as the API does', () => {
		expect(reasonProblem('  ')).toBe('Say what went wrong.');
		expect(reasonProblem('a\nb')).toBe('Keep it to one line.');
		expect(reasonProblem('x'.repeat(501))).toBe('Keep it to 500 characters.');
		expect(reasonProblem('The customer didn’t arrive')).toBe('');
	});
});

describe('detailStatus', () => {
	it('uses the list’s words and adds the fulfilment states', () => {
		expect(detailStatus(chef(), 'chef').text).toBe('Paid · automatic start later');
		expect(detailStatus(helper(), 'fulfillment').text).toBe('Confirmed · automatic start later');
		const out = ready({ fulfillment: { state: 'out_for_delivery', at: 'x', pending: null } });
		expect(detailStatus(out, 'chef')).toMatchObject({ stage: 'out', text: 'Out for delivery' });
		expect(
			detailStatus(ready({ fulfillment: { state: 'exception', at: 'x', pending: null } }), 'chef')
				.text
		).toBe('Ready · problem recorded');
		expect(
			detailStatus(
				ready({ fulfillment: { state: 'awaiting_handover', at: 'x', pending: 'handover' } }),
				'chef'
			).text
		).toBe('Handover being recorded');
		expect(
			detailStatus(
				chef({ lifecycle: 'completed', fulfillment: { state: 'no_show', at: 'x', pending: null } }),
				'chef'
			).text
		).toBe('Not collected · completed');
	});

	it('lets technical support tell a returned late payment from an unpaid release', () => {
		const released = support({
			lifecycle: 'cancelled',
			cancel_reason: 'payment_not_completed',
			payment: { id: 'p', state: 'cancelled', requested_at: 'x', expires_at: null }
		});
		expect(detailStatus(released, 'technical_support').text).toBe('Not paid · released');
		expect(
			detailStatus({ ...released, refund: { state: 'pending' } as never }, 'technical_support').text
		).toBe('Cancelled · full return');
	});
});

describe('banners', () => {
	const awaiting = (
		p: Partial<StaffOrderDetail['payment']> | null,
		over: Partial<StaffOrderDetail> = {}
	) =>
		chef({
			lifecycle: 'awaiting_payment',
			raw_lifecycle: 'awaiting_payment',
			paid_at: null,
			payment: p
				? {
						id: 'p',
						state: 'pending',
						amount_minor: 9000,
						currency: 'GBP',
						requested_at: '2026-10-29T09:55:00Z',
						expires_at: null,
						...p
					}
				: null,
			...over
		});

	it('words a payment in progress, then still checking after 15 minutes', () => {
		const [b] = banners(awaiting({}), 'chef', NOW);
		expect(b.title).toBe('Payment in progress.');
		expect(b.text).toBe('The customer started paying Thu 29 Oct, 09:55. The payment page is open.');
		expect(b.small[0]).toMatch(/^No second payment can start/);
		const [late] = banners(awaiting({ requested_at: '2026-10-29T09:00:00Z' }), 'chef', NOW);
		expect(late.small[0]).toMatch(/Still checking payment; do not pay again/);
	});

	it('holds a mismatch and never offers it as paid', () => {
		const [b] = banners(
			awaiting({ state: 'unknown' }, { attention: ['payment_mismatch'] }),
			'chef',
			NOW
		);
		expect(b).toMatchObject({ tone: 'bad', title: 'Payment doesn’t match the order.' });
		expect(b.small.join(' ')).toMatch(/a manager has reconciled it/);
	});

	it('names an outside refund’s amount for the chef only', () => {
		const obs = {
			quarantined: true,
			resolved_at: null,
			observed_at: '2026-10-21T16:40:00Z',
			amount_minor: 2000,
			currency: 'GBP'
		};
		const c = banners(
			chef({ attention: ['refund_quarantine'], refund_observations: [obs] }),
			'chef',
			NOW
		);
		expect(c[0].text).toMatch(/^£20 was refunded on this payment Wed 21 Oct, 17:40/);
		const s = banners(
			support({
				attention: ['refund_quarantine'],
				refund_observations: [{ ...obs, amount_minor: undefined, currency: undefined }]
			}),
			'technical_support',
			NOW
		);
		expect(s[0].text).toMatch(/^A refund was made on this payment/);
		expect(JSON.stringify(s)).not.toMatch(/£/);
	});

	it('never shows money to technical support or fulfilment', () => {
		const states = [
			support({ lifecycle: 'confirmation_pending', paid_at: null }),
			support({
				lifecycle: 'cancelled',
				cancel_reason: 'payment_not_completed',
				refund: { state: 'pending' } as never
			}),
			support({ attention: ['refund_needs_attention'] })
		];
		for (const o of states)
			expect(JSON.stringify(banners(o, 'technical_support', NOW))).not.toMatch(/£/);
		expect(
			banners(chef({ lifecycle: 'confirmation_pending', paid_at: null }), 'chef', NOW)[0].text
		).toMatch(/The payment of £90 succeeded/);
	});

	it('tells a free order to wait for the customer', () => {
		expect(banners(awaiting(null, { payment_requirement: 'none' }), 'chef', NOW)[0].title).toBe(
			'Free order · waiting for the customer to confirm.'
		);
	});
});

describe('latePaymentCard', () => {
	it('is the chef’s decision, and Elina’s for everyone else', () => {
		const o = chef({
			lifecycle: 'paid_awaiting_agreement',
			paid_at: '2026-10-22T08:20:00Z',
			late_payment: { state: 'waiting', offer: null, paid_minor: 9000, currency: 'GBP' }
		});
		const c = latePaymentCard(o, 'chef');
		expect(c?.title).toBe('Paid late · your decision');
		expect(c?.text.join(' ')).toMatch(/The £90 is received/);
		expect(c?.text.join(' ')).not.toMatch(/next update/);
		expect(latePaymentCard(helper(o), 'fulfillment')?.title).toBe('Paid late · Elina decides');
		expect(latePaymentCard(chef(), 'chef')).toBeNull();
	});
});

describe('progressSteps', () => {
	it('follows a collection order through production, ready and handover', () => {
		const steps = progressSteps(ready(), 'chef', NOW);
		expect(steps.map((s) => s.label)).toEqual([
			'Paid',
			'In production (automatic)',
			'Ready',
			'Handed over'
		]);
		expect(steps.map((s) => s.done)).toEqual([true, true, true, false]);
		expect(steps[0].note).toBe('£90');
	});

	it('shows a manual start, the delivery step and no money for fulfilment', () => {
		const o = helper(
			ready({
				fulfillment_mode: 'gk_delivery',
				production: { ...ready().production, source: 'manual' }
			})
		);
		const steps = progressSteps(o, 'fulfillment', NOW);
		expect(steps.map((s) => s.label)).toEqual([
			'Confirmed',
			'Started making',
			'Ready',
			'Out for delivery',
			'Handed over'
		]);
		expect(steps[0].done).toBe(true);
		expect(JSON.stringify(steps)).not.toMatch(/£/);
	});

	it('marks a scheduled automatic start as not done yet', () => {
		const o = chef({ production: { ...chef().production, scheduled_at: '2026-10-30T09:00:00Z' } });
		expect(progressSteps(o, 'chef', NOW)[1]).toMatchObject({
			done: false,
			at: '2026-10-30T09:00:00Z'
		});
	});
});

describe('money', () => {
	it('lists the server’s amounts for the chef', () => {
		expect(moneyFacts(chef()).map((f) => [f.label, f.value])).toEqual([
			['Cake price', '£100'],
			['Discount', '−£10'],
			['Paid for the cake', '£90'],
			['Customer’s full-refund deadline', 'Sat 24 Oct, 12:00']
		]);
		expect(moneyFacts(support())).toEqual([]);
		expect(moneyFacts(helper())).toEqual([]);
	});

	it('shows the refund ledger as the server reports it', () => {
		const o = chef({
			lifecycle: 'cancelled',
			refund: {
				state: 'partly_returned',
				target_minor: 11500,
				succeeded_minor: 6000,
				in_progress_minor: 0,
				delivery_pending_minor: 5500,
				currency: 'GBP'
			},
			refund_goal: { remaining_minor: 5500, currency: 'GBP' }
		});
		expect(refundFacts(o).map((f) => f.value)).toEqual(['£115', '£60', '£0', '£55', '£55']);
	});

	it('words a delivery for each view', () => {
		expect(deliveryText(chef({ delivery: { state: 'paid', paid_minor: 12000 } }))).toBe(
			'£120 · paid separately'
		);
		expect(deliveryText(helper({ delivery: { state: 'paid' } }))).toBe('Paid separately');
	});
});

describe('cancelledText and history', () => {
	it('words releases and cancellations per view', () => {
		expect(
			cancelledText(
				chef({
					lifecycle: 'cancelled',
					cancel_reason: 'payment_not_completed',
					cancelled_at: '2026-10-21T20:12:00Z'
				}),
				'chef'
			)
		).toMatch(/^Nobody completed the payment, so the order was released Wed 21 Oct, 21:12/);
		expect(
			cancelledText(
				helper({
					lifecycle: 'cancelled',
					cancel_reason: 'customer_cancelled'
				}),
				'fulfillment'
			)
		).toBe('This order was cancelled. Nothing to make or hand over. Refunds are handled by Elina.');
	});

	it('lists fulfilment steps with their reasons, oldest first', () => {
		const o = ready({
			fulfillment_history: [
				{
					action: 'delivery_exception',
					from: 'awaiting_handover',
					to: 'exception',
					received_at: '2026-10-29T12:20:00Z',
					command_id: null,
					state: 'done',
					reason: 'Customer didn’t arrive'
				},
				...(ready().fulfillment_history ?? [])
			]
		});
		expect(historyLines(o).map((l) => l.text)).toEqual([
			'In production (automatic start)',
			'Marked ready',
			'Problem recorded · “Customer didn’t arrive”'
		]);
	});
});

describe('commands', () => {
	const capture = () => {
		const sent: { url: string; body: unknown; key: string }[] = [];
		let answer: { status: number; body: unknown } = { status: 200, body: {} };
		const api: StaffApi = {
			baseUrl: 'https://api.example.com',
			token: 't',
			fetch: (async (u: string, init: RequestInit) => {
				sent.push({
					url: u,
					body: JSON.parse(init.body as string),
					key: (init.headers as Record<string, string>)['Idempotency-Key']
				});
				return new Response(JSON.stringify(answer.body), { status: answer.status });
			}) as typeof fetch
		};
		return { api, sent, answer: (a: typeof answer) => (answer = a) };
	};

	it('puts the version in the key', () => {
		expect(commandKey('ready', chef(), 'n1')).toBe(`chef-ready-${ID}-v7-n1`);
	});

	it('starts production with the expected version', async () => {
		const c = capture();
		c.answer({ status: 200, body: { order_id: ID, lifecycle: 'in_production' } });
		const r = await sendStep(c.api, chef(), 'start', 'k1');
		expect(r).toEqual({ kind: 'done', lifecycle: 'in_production' });
		expect(c.sent[0]).toEqual({
			url: `https://api.example.com/api/v1/admin/orders/${ID}/start-production`,
			body: { expected_version: 7 },
			key: 'k1'
		});
	});

	it('sends a reason only with a problem', async () => {
		const c = capture();
		c.answer({
			status: 200,
			body: { order_id: ID, lifecycle: 'ready', fulfillment_state: 'exception' }
		});
		await sendStep(c.api, ready(), 'ready', 'k2');
		await sendStep(c.api, ready(), 'delivery_exception', 'k3', '  Customer didn’t arrive ');
		expect(c.sent.map((s) => s.body)).toEqual([
			{ action: 'ready', expected_version: 7 },
			{ action: 'delivery_exception', expected_version: 7, reason: 'Customer didn’t arrive' }
		]);
	});

	it('returns the 202 of a handover to poll', async () => {
		const c = capture();
		const accepted = {
			order_id: ID,
			operation_id: 'op1',
			state: 'handover_pending',
			poll_after_seconds: 3
		};
		c.answer({ status: 202, body: accepted });
		expect(await sendStep(c.api, ready(), 'handover', 'k4')).toEqual({
			kind: 'accepted',
			accepted
		});
	});
});

describe('settled and pollStep', () => {
	const op = {
		order_id: ID,
		operation_id: 'op1',
		state: 'handover_pending',
		poll_after_seconds: 3
	};
	const pending = ready({
		fulfillment: { state: 'awaiting_handover', at: null, pending: 'handover' }
	});
	const done = chef({
		lifecycle: 'completed',
		fulfillment: { state: 'completed', at: 'x', pending: null }
	});

	it('reads pending, done, not collected and refused', () => {
		expect(settled(pending, 'op1')).toBe('pending');
		expect(settled(done, 'op1')).toBe('done');
		expect(
			settled(
				chef({ lifecycle: 'completed', fulfillment: { state: 'no_show', at: 'x', pending: null } }),
				'op1'
			)
		).toBe('noshow');
		expect(settled(helper(ready()), 'op1')).toBe('refused');
		const cmd = {
			id: 'op1',
			kind: 'handover',
			requested_by: 'staff',
			received_at: 'x',
			state: 'secured',
			outcome_status: 'refused',
			decision: null
		};
		expect(settled(ready({ commands: [cmd] }), 'op1')).toBe('refused');
		expect(settled(ready({ commands: [{ ...cmd, state: 'committed' }] }), 'op1')).toBe('pending');
	});

	it('polls until the outcome is secured', async () => {
		const reads = [pending, pending, done];
		const waits: number[] = [];
		const r = await pollStep(async () => reads.shift() as StaffOrderDetail, op, {
			wait: async (ms) => {
				waits.push(ms);
			}
		});
		expect(r.outcome).toBe('done');
		expect(waits).toEqual([3000, 3000, 3000]);
	});

	it('gives up as still pending after its tries, and survives a failed read', async () => {
		let n = 0;
		const r = await pollStep(
			async () => {
				n++;
				if (n === 1) throw err(503, 'UNAVAILABLE');
				return pending;
			},
			op,
			{ tries: 3, wait: async () => {} }
		);
		expect(r.outcome).toBe('pending');
		expect(n).toBe(3);
	});

	it('stops when the session ends', async () => {
		await expect(
			pollStep(
				async () => {
					throw err(401, 'UNAUTHENTICATED');
				},
				op,
				{ wait: async () => {} }
			)
		).rejects.toMatchObject({ status: 401 });
	});
});

describe('refusalOf', () => {
	it('reloads and shows the changed banner for a stale version', () => {
		expect(refusalOf(err(409, 'STALE_VERSION'), 'ready')).toMatchObject({
			stale: true,
			reload: true
		});
	});

	it('maps each refusal to plain words', () => {
		const cases: [string, number, RegExp][] = [
			['TRANSITION_NOT_ALLOWED', 409, /isn’t possible for the order as it is now/],
			['HANDOVER_NOT_DUE', 409, /hasn’t started yet/],
			['DELIVERY_NOT_PAID', 409, /isn’t paid or waived/],
			['DELIVERY_NOT_POSSIBLE', 409, /can’t deliver/],
			['DELIVERY_FAILURE_OPEN', 409, /hasn’t chosen/],
			['REDELIVERY_TIME_NOT_SET', 409, /new delivery time/],
			['CONFLICT', 409, /still being processed/],
			['NOT_FOUND', 404, /wasn’t found/]
		];
		for (const [code, status, words] of cases) {
			const r = refusalOf(err(status, code), 'handover');
			expect(r.text, code).toMatch(words);
			expect(r.text, code).not.toMatch(/[A-Z]{3,}_/);
		}
		expect(refusalOf(err(409, 'CONFLICT'), 'start').text).toMatch(
			/Production can’t be started now/
		);
		expect(refusalOf(err(403, 'FORBIDDEN'), 'no_show').text).toMatch(/Only Elina/);
	});

	it('keeps the key for a retry after a network failure, a freeze or a server error', () => {
		for (const e of [err(0, 'NETWORK'), err(503, 'UNAVAILABLE'), err(500, 'INTERNAL')]) {
			expect(refusalOf(e, 'ready')).toMatchObject({ retry: true, reload: false });
		}
		expect(refusalOf(err(401, 'UNAUTHENTICATED'), 'ready').signIn).toBe(true);
	});
});

describe('review fixes (PR #66)', () => {
	it('gives an edited problem reason a new key slot, and the same reason the same one', () => {
		const o = ready();
		expect(keySlot('delivery_exception', o, 'Customer late')).toBe(
			keySlot('delivery_exception', o, '  Customer late ')
		);
		expect(keySlot('delivery_exception', o, 'Customer late')).not.toBe(
			keySlot('delivery_exception', o, 'Customer didn’t arrive')
		);
		expect(keySlot('delivery_exception', { ...o, version: 8 }, 'Customer late')).not.toBe(
			keySlot('delivery_exception', o, 'Customer late')
		);
		expect(keySlot('ready', o, 'ignored')).toBe('ready:7');
	});

	it('words KEY_REUSED plainly, keeping the form (no reload, a new key next time)', () => {
		const r = refusalOf(err(409, 'KEY_REUSED'), 'delivery_exception');
		expect(r.text).toMatch(/already sent once with different details/);
		expect(r).toMatchObject({ reload: false, retry: false, stale: false });
	});

	it('words the no-show refund as the cumulative goal, and points to the ledger when done', () => {
		expect(confirmText('no_show', ready(), 'chef')).toMatch(
			/The refund owed becomes 50% of what they paid \(anything already refunded counts towards it\); it’s requested automatically and can’t be undone\.$/
		);
		const done = doneText('no_show', 'noshow');
		expect(done).toMatch(/under Refund, as recorded/);
		expect(done).not.toMatch(/50%|being returned/);
	});

	it('never shows a pending handover or no-show as finished', () => {
		for (const pending of ['handover', 'no_show']) {
			const s = detailStatus(
				ready({ fulfillment: { state: 'awaiting_handover', at: null, pending } }),
				'fulfillment'
			);
			expect(s.stage).toBe('pending');
			expect(['ready', 'done']).not.toContain(s.stage);
		}
	});

	it('finds a handover or no-show already pending when the page opens', () => {
		const cmd = {
			id: 'op9',
			kind: 'no_show',
			requested_by: 'staff',
			received_at: 'x',
			state: 'prepared',
			outcome_status: null,
			decision: null
		};
		expect(
			pendingStep(
				ready({
					fulfillment: { state: 'awaiting_handover', at: null, pending: 'no_show' },
					commands: [cmd]
				})
			)
		).toEqual({ action: 'no_show', operation_id: 'op9' });
		// Fulfilment has no command list: the outcome is read from the order.
		expect(
			pendingStep(
				helper(
					ready({ fulfillment: { state: 'awaiting_handover', at: null, pending: 'handover' } })
				)
			)
		).toEqual({ action: 'handover', operation_id: '' });
		expect(pendingStep(ready())).toBeNull();
	});

	it('reads an at-load pending handover through to done or refused', async () => {
		const pending = helper(
			ready({ fulfillment: { state: 'awaiting_handover', at: null, pending: 'handover' } })
		);
		const done = helper(
			chef({ lifecycle: 'completed', fulfillment: { state: 'completed', at: 'x', pending: null } })
		);
		const p = pendingStep(pending)!;
		const acc = { order_id: ID, operation_id: p.operation_id, state: '', poll_after_seconds: 3 };
		const reads = [pending, done];
		const r = await pollStep(async () => reads.shift() as StaffOrderDetail, acc, {
			tries: 5,
			wait: async () => {}
		});
		expect(r.outcome).toBe('done');
		const back = await pollStep(async () => helper(ready()), acc, {
			tries: 5,
			wait: async () => {}
		});
		expect(back.outcome).toBe('refused');
		const still = await pollStep(async () => pending, acc, { tries: 5, wait: async () => {} });
		expect(still.outcome).toBe('pending');
	});

	it('lists the refund parts by payment with the server’s amounts (chef only)', () => {
		const o = chef({
			lifecycle: 'cancelled',
			refund: {
				state: 'partly_returned',
				target_minor: 11000,
				succeeded_minor: 5000,
				in_progress_minor: 0,
				delivery_pending_minor: 6000,
				currency: 'GBP',
				parts: [
					{
						kind: 'cake',
						target_minor: 5000,
						succeeded_minor: 5000,
						in_progress_minor: 0,
						pending_chef_minor: 0
					},
					{
						kind: 'delivery',
						target_minor: 6000,
						succeeded_minor: 0,
						in_progress_minor: 0,
						pending_chef_minor: 6000
					}
				]
			}
		});
		expect(
			refundPartRows(o).map((r) => [r.label, ...r.facts.map((f) => `${f.label} ${f.value}`)])
		).toEqual([
			['Cake payment', 'Share owed £50', 'Refunded £50', 'Sent, not confirmed —'],
			['Delivery payment', 'Share owed £60', 'Refunded —', 'Sent, not confirmed —', 'Your step £60']
		]);
		expect(
			refundPartRows(
				chef({ refund: { state: 'requested', target_minor: 9000, succeeded_minor: 0 } })
			)
		).toEqual([]);
		expect(
			refundPartRows(support({ lifecycle: 'cancelled', refund: { state: 'pending' } as never }))
		).toEqual([]);
	});

	it('words a freeze neutrally and “can’t deliver” without naming who recorded it', () => {
		expect(refusalOf(err(503, 'UNAVAILABLE'), 'ready').text).toBe(
			'Order changes aren’t available right now. Try again shortly: repeating it is safe.'
		);
		const gk = ready({
			fulfillment_mode: 'gk_delivery',
			delivery: { state: 'paid' },
			delivery_choice: { open: false, reason: 'not_possible', pending: false }
		});
		expect(refusalOf(err(409, 'DELIVERY_NOT_POSSIBLE'), 'dispatch').text).toBe(dispatchBlock(gk));
		expect(dispatchBlock(gk)).not.toMatch(/^You /);
	});
});
