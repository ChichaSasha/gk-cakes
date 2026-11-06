import { describe, expect, it } from 'vitest';
import { ApiError } from '../api';
import type { StaffApi } from './catalogue';
import {
	allocationsFrom,
	cancelChoice,
	cancelPreview,
	cancelPreviewLines,
	collectionKind,
	deliveryShare,
	hasPayouts,
	latestOperation,
	managerCases,
	moneyDone,
	moneyKey,
	moneyRefusal,
	moneyRequest,
	moneySettled,
	moneySlot,
	noteProblem,
	parseAmount,
	payoutOutcomes,
	pollMoney,
	previewPayout,
	reconciliationReasons,
	referenceProblem,
	refundBlock,
	refundChoice,
	refundIdProblem,
	refundInProgress,
	refundSources,
	room,
	sendMoney,
	strayReturn,
	targetCollections,
	type MoneyInput,
	type Payout,
	type PayoutPreview
} from './order-money';
import type { StaffCollection, StaffOrderDetail, StaffRefundOperation } from './order-detail';

const ID = 'a1b2c3d4-0000-4000-8000-000000000001';
const CAKE = 'c0000000-0000-4000-8000-000000000001';
const DEL = 'c0000000-0000-4000-8000-000000000002';
const STRAY = 'c0000000-0000-4000-8000-000000000003';

const coll = (over: Partial<StaffCollection> = {}): StaffCollection => ({
	id: CAKE,
	kind: 'cake',
	state: 'succeeded',
	mismatch_reason: null,
	amount_minor: 9000,
	currency: 'GBP',
	refunded_minor: 0,
	refund_reserved_minor: 0,
	...over
});

const op = (over: Partial<StaffRefundOperation> = {}): StaffRefundOperation => ({
	id: 'r0000000-0000-4000-8000-000000000001',
	collection_id: CAKE,
	channel: 'provider',
	state: 'succeeded',
	failure_reason: null,
	mismatch_reason: null,
	provider_status: 'succeeded',
	record_state: 'secured',
	window_ended: false,
	created_at: '2026-10-20T09:01:00Z',
	settled_at: '2026-10-20T09:05:00Z',
	amount_minor: 9000,
	currency: 'GBP',
	...over
});

/** The chef's view of a confirmed collection order, paid £90 (handover Sat 31 Oct, 13:30). */
const order = (over: Partial<StaffOrderDetail> = {}): StaffOrderDetail => ({
	id: ID,
	version: 7,
	lifecycle: 'confirmed',
	raw_lifecycle: 'confirmed',
	recovery_state: 'secured',
	cancel_reason: null,
	cancelled_at: null,
	placed_at: '2026-10-12T09:00:00Z',
	date: '2026-10-31',
	fulfillment_mode: 'collection',
	slot: { id: 's1', starts_at: '2026-10-31T13:30:00Z', ends_at: '2026-10-31T14:30:00Z' },
	customer_full_name: 'Noor Example',
	items: [],
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
	collections: [coll()],
	refund_operations: [],
	refund_observations: [],
	fulfillment: { state: null, at: null, pending: null },
	...over
});

const gk = (over: Partial<StaffOrderDetail> = {}) =>
	order({
		fulfillment_mode: 'gk_delivery',
		collections: [
			coll(),
			coll({ id: DEL, kind: 'delivery', amount_minor: 12000 }),
			coll({ id: STRAY, kind: 'delivery', amount_minor: 15000 })
		],
		delivery_history: [
			{
				id: 'q1',
				order_version: 3,
				quoted_at: '2026-10-13T09:00:00Z',
				pay_by: '2026-10-29T13:30:00Z',
				current: false,
				payments: [
					{
						collection_id: STRAY,
						state: 'succeeded',
						outcome: 'unmatched',
						reason: 'quote_replaced'
					}
				]
			},
			{
				id: 'q2',
				order_version: 5,
				quoted_at: '2026-10-14T09:00:00Z',
				pay_by: '2026-10-29T13:30:00Z',
				current: true,
				payments: [{ collection_id: DEL, state: 'succeeded', outcome: 'paid', reason: null }]
			}
		] as StaffOrderDetail['delivery_history'],
		...over
	});

describe('amounts typed as text', () => {
	it('reads pounds as whole pence without floats', () => {
		expect(parseAmount('45')).toEqual({ minor: 4500 });
		expect(parseAmount('£45.50')).toEqual({ minor: 4550 });
		expect(parseAmount(' 1,200.05 ')).toEqual({ minor: 120005 });
		expect(parseAmount('0.29')).toEqual({ minor: 29 });
		// 0.1 + 0.2 territory: still exact.
		expect(parseAmount('1234567.89')).toEqual({ minor: 123456789 });
		expect(parseAmount('0012')).toEqual({ minor: 1200 });
	});

	it('refuses anything else in plain words', () => {
		expect(parseAmount('', 'how much')).toEqual({ error: 'Enter how much.' });
		expect(parseAmount('4.5')).toEqual({ error: 'Give pence as two digits, for example 20.50.' });
		expect(parseAmount('-5')).toHaveProperty('error');
		expect(parseAmount('1e3')).toHaveProperty('error');
		expect(parseAmount('12,34')).toHaveProperty('error');
		expect(parseAmount('12345678')).toEqual({ error: 'That amount is too large.' });
	});

	it('checks notes, refund IDs and evidence references as the API does', () => {
		expect(noteProblem('  ')).toBe('Say why.');
		expect(noteProblem('x'.repeat(501))).toBe('Keep it to 500 characters.');
		expect(noteProblem('a\nb')).toBe('Keep it to one line.');
		expect(noteProblem('Checked the dashboard')).toBe('');
		expect(refundIdProblem('re_3Pq1AbC')).toBe('');
		expect(refundIdProblem('pyr_1x')).toBe('');
		expect(refundIdProblem('ch_123')).toMatch(/starts with re_/);
		expect(refundIdProblem('')).toMatch(/^Enter/);
		expect(referenceProblem('Drive: GK refunds/Oct, card closed.pdf', 'x')).toBe('');
		// A dated file name can trip the rule (2026-10 is six digits once hyphens go).
		expect(referenceProblem('GK-refunds/2026-10.pdf', 'x')).toMatch(/Never type bank details/);
		// Six digits once spaces, hyphens and dots go: a sort code or account number.
		expect(referenceProblem('sort 12-34-56', 'x')).toMatch(/Never type bank details/);
		expect(referenceProblem('acct 1234.5678', 'x')).toMatch(/Never type bank details/);
		expect(referenceProblem('GB29 NWBK 6016 1331 9268 19', 'x')).toMatch(/Never type bank details/);
		expect(referenceProblem('reçu', 'x')).toMatch(/plain letters/);
		expect(referenceProblem('', 'the proof')).toBe('Enter the proof.');
		expect(referenceProblem('x'.repeat(201), 'x')).toBe('Keep it to 200 characters.');
	});
});

describe('reading the payments', () => {
	it('works out each payment’s room and kind', () => {
		expect(room(coll({ refunded_minor: 2000, refund_reserved_minor: 1000 }))).toBe(6000);
		expect(room(coll({ state: 'pending' }))).toBe(0);
		expect(room(coll({ amount_minor: undefined }))).toBe(0);
		const o = gk();
		expect(o.collections!.map((c) => collectionKind(o, c))).toEqual(['cake', 'delivery', 'stray']);
		expect(refundSources(o).map((s) => [s.label, s.roomMinor])).toEqual([
			['Cake payment', 9000],
			['Delivery payment', 12000],
			['Unmatched delivery payment', 15000]
		]);
	});

	it('names what needs reconciling first (RECONCILIATION_REQUIRED) and what is on its way', () => {
		expect(reconciliationReasons(order())).toEqual([]);
		const o = order({
			refund_observations: [
				{
					id: 'b1',
					collection_id: CAKE,
					quarantined: true,
					resolved_at: null,
					observed_at: '2026-10-21T17:40:00Z'
				}
			],
			refund_operations: [
				op({ state: 'requires_action' }),
				op({ id: 'r2', state: 'unknown', record_state: 'reconcile' })
			],
			collections: [coll({ mismatch_reason: 'amount' }), coll({ id: DEL, state: 'unknown' })]
		});
		expect(reconciliationReasons(o)).toEqual([
			'a refund made outside GK Cakes',
			'a payment that doesn’t match the order',
			'a refund waiting for an action at the provider',
			'a refund being reconciled',
			'a payment result unknown'
		]);
		expect(refundInProgress(o)).toBe(true);
		expect(refundInProgress(o, DEL)).toBe(false);
		expect(refundBlock(o)).toMatch(/manager case/);
		expect(refundBlock(order({ refund_operations: [op({ state: 'pending' })] }))).toMatch(
			/on its way/
		);
		expect(
			refundBlock(
				order({
					commands: [
						{
							id: 'c',
							kind: 'cancel',
							requested_by: 'customer',
							received_at: '',
							state: 'prepared',
							outcome_status: null,
							decision: null
						}
					]
				})
			)
		).toMatch(/still being processed/);
	});

	it('finds a payment’s latest operation, skipping withdrawn ones', () => {
		const o = order({
			refund_operations: [
				op({ id: 'r1', state: 'failed', created_at: '2026-10-20T09:00:00Z' }),
				op({ id: 'r2', state: 'cancelled', created_at: '2026-10-21T09:00:00Z' })
			]
		});
		expect(latestOperation(o, CAKE)?.id).toBe('r1');
		expect(latestOperation(o, DEL)).toBeNull();
	});
});

describe('cancelling', () => {
	it('is the chef’s, for a secured confirmed, in-production or ready order with nothing in progress', () => {
		expect(cancelChoice(order(), 'chef').can).toBe(true);
		expect(cancelChoice(order({ lifecycle: 'ready' }), 'chef').can).toBe(true);
		expect(cancelChoice(order(), 'technical_support').can).toBe(false);
		expect(cancelChoice(order(), 'fulfillment').can).toBe(false);
		for (const l of [
			'awaiting_payment',
			'confirmation_pending',
			'paid_awaiting_agreement',
			'completed',
			'cancelled',
			'cancellation_pending'
		]) {
			expect(cancelChoice(order({ lifecycle: l }), 'chef')).toEqual({ can: false, note: '' });
		}
		expect(cancelChoice(order({ recovery_state: 'prepared' }), 'chef')).toEqual({
			can: false,
			note: 'Cancelling comes back when the change being processed is done.'
		});
	});

	it('previews 100% of everything paid from the server’s figures', () => {
		const p = cancelPreview(
			gk({
				refund: {
					state: 'pending',
					target_minor: 1000,
					succeeded_minor: 0,
					in_progress_minor: 1000
				}
			})
		);
		expect(p).toMatchObject({
			paidMinor: 21000,
			cakeMinor: 9000,
			deliveryMinor: 12000,
			strayMinor: 15000,
			alreadyMinor: 1000,
			deliveryAutomatic: false
		});
		const lines = cancelPreviewLines(gk(), cancelPreview(gk()));
		expect(lines[0]).toBe(
			'When you cancel, the customer gets 100% of everything paid: £210. The customer is told by email from “GK Cakes”; no confirmation from them is needed.'
		);
		expect(lines).toContain(
			'Cake £90: sent back to their original payment automatically once the cancellation is confirmed.'
		);
		expect(lines).toContain(
			'Delivery £120 was paid separately: after cancelling it appears as your next step, “Delivery refund · your step”.'
		);
		expect(lines).toContain(
			'A delivery payment of £150 that couldn’t be used is also returned in full.'
		);
		const floored = gk({
			delivery_choice: {
				not_possible: { id: 'np' }
			} as unknown as StaffOrderDetail['delivery_choice']
		});
		expect(cancelPreviewLines(floored, cancelPreview(floored))).toContain(
			'Delivery £120: returned in full automatically, because GK couldn’t deliver.'
		);
		const free = order({
			collections: [],
			paid_at: null,
			total_minor: 0,
			payment_requirement: 'none'
		});
		expect(cancelPreviewLines(free, cancelPreview(free))[0]).toMatch(/^Nothing was paid/);
	});
});

describe('refunds', () => {
	it('offers an agreed partial refund only when the API would take it', () => {
		expect(refundChoice(order(), 'chef')).toMatchObject({ can: true, limitMinor: 9000 });
		expect(refundChoice(order({ lifecycle: 'completed' }), 'chef').can).toBe(true);
		expect(refundChoice(order(), 'technical_support').can).toBe(false);
		expect(refundChoice(order({ lifecycle: 'paid_awaiting_agreement' }), 'chef').can).toBe(false);
		expect(
			refundChoice(order({ collections: [coll({ refunded_minor: 9000 })] }), 'chef')
		).toMatchObject({ can: false, note: '' });
		// The last refund failed definitively: the manager decision, not another refund.
		expect(
			refundChoice(order({ refund_operations: [op({ state: 'failed' })] }), 'chef')
		).toMatchObject({ can: false, sources: [] });
		const busy = refundChoice(order({ refund_operations: [op({ state: 'unknown' })] }), 'chef');
		expect(busy.can).toBe(false);
		expect(busy.note).toMatch(/on its way/);
	});

	it('turns the typed amounts into allocations within each payment’s room', () => {
		const sources = refundSources(gk());
		const ok = allocationsFrom(sources, { [CAKE]: '20', [DEL]: '', [STRAY]: '0' });
		expect(ok).toMatchObject({
			total: '',
			totalMinor: 2000,
			allocations: [{ collection_id: CAKE, amount_minor: 2000 }]
		});
		const over = allocationsFrom(sources, { [CAKE]: '95' });
		expect(over.fields[CAKE]).toBe('At most £90 can come back from the cake payment.');
		expect(over.allocations).toEqual([]);
		expect(allocationsFrom(sources, {}).total).toBe('Enter how much to refund.');
		expect(allocationsFrom(sources, { [CAKE]: 'abc' }).fields[CAKE]).toMatch(/pounds/);
		const split = allocationsFrom(sources, { [CAKE]: '10.50', [DEL]: '120' });
		expect(split.totalMinor).toBe(13050);
		expect(split.allocations).toHaveLength(2);
	});

	it('offers the delivery share the goal waits for, from the paid delivery', () => {
		const o = gk({
			refund: {
				state: 'partly_returned',
				target_minor: 21000,
				succeeded_minor: 9000,
				delivery_pending_minor: 12000
			}
		});
		expect(deliveryShare(o, 'chef')).toEqual({
			collections: [{ id: DEL, roomMinor: 12000 }],
			pendingMinor: 12000,
			note: ''
		});
		expect(deliveryShare(o, 'technical_support').collections).toEqual([]);
		expect(deliveryShare(gk(), 'chef').collections).toEqual([]);
		const busy = { ...o, refund_operations: [op({ collection_id: DEL, state: 'pending' })] };
		expect(deliveryShare(busy, 'chef')).toMatchObject({
			collections: [],
			note: expect.stringMatching(/on its way/)
		});
	});

	it('returns an unmatched delivery payment in full', () => {
		expect(strayReturn(gk(), 'chef')).toEqual({
			collections: [{ id: STRAY, amountMinor: 15000 }],
			note: ''
		});
		expect(strayReturn(order(), 'chef').collections).toEqual([]);
		expect(strayReturn(gk(), 'fulfillment').collections).toEqual([]);
		const partly = gk({
			collections: [
				coll(),
				coll({ id: STRAY, kind: 'delivery', amount_minor: 15000, refunded_minor: 5000 })
			]
		});
		expect(strayReturn(partly, 'chef').collections).toEqual([{ id: STRAY, amountMinor: 10000 }]);
	});
});

describe('manager cases', () => {
	const cancelled = (over: Partial<StaffOrderDetail> = {}) =>
		order({
			lifecycle: 'cancelled',
			raw_lifecycle: 'cancelled',
			cancel_reason: 'chef_cancelled',
			refund_goal: { remaining_minor: 9000, currency: 'GBP' },
			...over
		});

	it('lists an outside refund with the refunds it may match (GK-1050)', () => {
		const o = order({
			refund_observations: [
				{
					id: 'b1',
					collection_id: CAKE,
					quarantined: true,
					resolved_at: null,
					observed_at: '2026-10-21T17:40:00Z',
					status: 'succeeded',
					source: 'dashboard',
					amount_minor: 2000,
					currency: 'GBP',
					named_operation_id: 'r2'
				}
			],
			refund_operations: [
				op({ id: 'r1', amount_minor: 2000, state: 'unknown' }),
				op({ id: 'r2', amount_minor: 2000, state: 'unknown' }),
				op({ id: 'r3', amount_minor: 500 })
			]
		});
		const [c] = managerCases(o, 'chef');
		expect(c).toMatchObject({
			kind: 'quarantine',
			title: 'Refund made outside GK Cakes',
			tools: ['match_or_explain'],
			observationId: 'b1'
		});
		expect(c.lines[0]).toBe(
			'£20 was refunded on the cake payment Wed 21 Oct, 18:40, not from this workspace (in the payment provider’s dashboard); the provider reports it succeeded.'
		);
		expect(c.candidates!.map((k) => k.id)).toEqual(['r2', 'r1']);
		const [s] = managerCases(o, 'technical_support');
		expect(s.tools).toEqual([]);
		expect(s.lines.join(' ')).not.toMatch(/£/);
		expect(managerCases(o, 'fulfillment')).toEqual([]);
	});

	it('offers review for a changed refund and reconcile for a decided payment mismatch (GK-1084, GK-1083)', () => {
		const o = cancelled({
			refund_operations: [op({ mismatch_reason: 'status_changed', provider_status: 'failed' })],
			collections: [coll({ mismatch_reason: 'amount' })]
		});
		const cs = managerCases(o, 'chef');
		expect(cs.map((c) => [c.kind, c.tools])).toEqual([
			['payment_contradiction', ['reconcile']],
			['refund_contradiction', ['review']]
		]);
		expect(cs[1].title).toBe('Refund changed after it was confirmed');
		expect(cs[1].lines[0]).toBe(
			'The £90 refund on the cake payment was recorded as confirmed Tue 20 Oct, 10:05; the payment provider now reports it failed.'
		);
		const unknownPay = order({
			lifecycle: 'awaiting_payment',
			collections: [coll({ state: 'cancelled', mismatch_reason: 'amount' })]
		});
		const [u] = managerCases(unknownPay, 'chef');
		expect(u.tools).toEqual([]);
		expect(u.note).toMatch(/isn’t available in the workspace yet/);
	});

	it('decides an unknown refund only past its window', () => {
		const open = managerCases(
			cancelled({ refund_operations: [op({ state: 'unknown', window_ended: false })] }),
			'chef'
		);
		expect(open[0]).toMatchObject({
			kind: 'refund_unknown',
			title: 'Refund result not confirmed',
			tools: []
		});
		expect(open[0].note).toMatch(/23-hour retry window/);
		const ended = managerCases(
			cancelled({ refund_operations: [op({ state: 'unknown', window_ended: true })] }),
			'chef'
		);
		expect(ended[0]).toMatchObject({
			title: 'Refund result unknown · needs a manager',
			tools: ['decide']
		});
		const ra = managerCases(
			cancelled({ refund_operations: [op({ state: 'requires_action' })] }),
			'chef'
		);
		expect(ra[0]).toMatchObject({ kind: 'refund_requires_action', tools: [] });
	});

	it('offers a failed remainder again or a verified transfer, never while something is open (GK-1087)', () => {
		const failed = op({
			state: 'failed',
			failure_reason: 'expired_or_canceled_card',
			provider_status: 'failed'
		});
		const [c] = managerCases(cancelled({ refund_operations: [failed] }), 'chef');
		expect(c).toMatchObject({
			kind: 'refund_failed',
			title: 'Refund failed · won’t be resent automatically',
			tools: ['re_reserve', 'transfer'],
			remainderMinor: 9000
		});
		expect(c.lines[0]).toBe(
			'The payment provider definitively refused the £90 refund on the cake payment Tue 20 Oct, 10:05. The original card is closed or expired and can’t receive refunds.'
		);
		// Superseded by a newer attempt: not offered again.
		const newer = op({ id: 'r9', state: 'pending', created_at: '2026-10-22T09:00:00Z' });
		expect(
			managerCases(cancelled({ refund_operations: [failed, newer] }), 'chef').map((x) => x.kind)
		).toEqual([]);
		// Nothing owed any more: no case.
		expect(
			managerCases(
				cancelled({
					refund_operations: [failed],
					refund_goal: { remaining_minor: 0, currency: 'GBP' }
				}),
				'chef'
			)
		).toEqual([]);
		// A failed manual transfer: only another transfer.
		const manual = op({ state: 'failed', channel: 'manual_alternative' });
		expect(managerCases(cancelled({ refund_operations: [manual] }), 'chef')[0].tools).toEqual([
			'transfer'
		]);
		// Something else to reconcile on the order: shown, without the tools.
		const blocked = managerCases(
			cancelled({ refund_operations: [failed], collections: [coll({ mismatch_reason: 'x' })] }),
			'chef'
		);
		expect(blocked.find((x) => x.kind === 'refund_failed')).toMatchObject({
			tools: [],
			note: expect.stringMatching(/Resolve the other open case/)
		});
		// Support: the case (the refund reads needs_attention) without amounts or tools.
		const support = managerCases(
			cancelled({
				refund_operations: [failed],
				refund_goal: { currency: 'GBP' },
				refund: { state: 'needs_attention', target_minor: 0, succeeded_minor: 0 }
			}),
			'technical_support'
		);
		expect(support[0]).toMatchObject({ kind: 'refund_failed', tools: [] });
		expect(support[0].lines.join(' ')).not.toMatch(/£/);
	});

	it('holds every tool while another change is being processed', () => {
		const o = cancelled({
			refund_operations: [op({ state: 'unknown', window_ended: true })],
			recovery_state: 'prepared'
		});
		const [c] = managerCases(o, 'chef');
		expect(c.tools).toEqual([]);
		expect(c.note).toMatch(/still being processed/);
	});

	it('lists a manual payout in progress and its allowed outcomes', () => {
		const o = cancelled({
			refund_operations: [op({ channel: 'manual_alternative', state: 'reserved' })]
		});
		expect(managerCases(o, 'chef')[0]).toMatchObject({
			kind: 'manual_payout',
			title: 'Verified transfer reserved · not sent yet'
		});
		expect(hasPayouts(o, 'chef')).toBe(true);
		expect(hasPayouts(o, 'technical_support')).toBe(false);
		const p = (over: Partial<Payout>): Payout => ({
			refund_operation_id: 'r1',
			collection_id: CAKE,
			state: 'reserved',
			amount_minor: 9000,
			currency: 'GBP',
			unavailable_evidence_ref: 'x',
			instructions: {
				amount_minor: 9000,
				currency: 'GBP',
				recipient_evidence_ref: 'y',
				payment_reference: 'r1'
			},
			submitted_at: null,
			transfer_identity: null,
			decided_at: null,
			outcome_pending: false,
			...over
		});
		expect(payoutOutcomes(p({}))).toEqual(['submitted', 'withdrawn']);
		expect(payoutOutcomes(p({ state: 'pending' }))).toEqual(['succeeded', 'failed', 'unknown']);
		expect(payoutOutcomes(p({ state: 'unknown' }))).toEqual(['succeeded', 'failed']);
		expect(payoutOutcomes(p({ state: 'succeeded' }))).toEqual([]);
		expect(payoutOutcomes(p({ outcome_pending: true }))).toEqual([]);
		expect(payoutOutcomes(p({ state: 'protecting', instructions: null }))).toEqual([]);
	});
});

describe('commands', () => {
	const preview: PayoutPreview = {
		preview_id: 'p0000000-0000-4000-8000-000000000001',
		order_id: ID,
		order_version: 6,
		collection_id: CAKE,
		failed_refund_operation_id: 'r1',
		amount_minor: 9000,
		currency: 'GBP',
		unavailable_evidence_ref: 'a',
		recipient_evidence_ref: 'b',
		computed_at: '',
		expires_at: '',
		preview_hash: 'f'.repeat(64)
	};
	const cases: [MoneyInput, string, Record<string, unknown>, string][] = [
		[
			{ m: 'cancel', reason: ' Oven broke ', amountMinor: 9000 },
			'/cancel',
			{ expected_version: 7, reason: 'Oven broke' },
			'command'
		],
		[
			{
				m: 'refund',
				allocations: [
					{ collection_id: CAKE, amount_minor: 1050 },
					{ collection_id: DEL, amount_minor: 2000 }
				],
				reason: ' Goodwill '
			},
			'/refunds',
			{
				expected_version: 7,
				amount_minor: 3050,
				currency: 'GBP',
				allocations: [
					{ collection_id: CAKE, amount_minor: 1050 },
					{ collection_id: DEL, amount_minor: 2000 }
				],
				reason: 'Goodwill'
			},
			'refund'
		],
		[
			{
				m: 'stray',
				allocations: [{ collection_id: STRAY, amount_minor: 15000 }],
				reason: 'Unused'
			},
			'/refunds',
			{
				expected_version: 7,
				amount_minor: 15000,
				currency: 'GBP',
				allocations: [{ collection_id: STRAY, amount_minor: 15000 }],
				reason: 'Unused'
			},
			'refund'
		],
		[
			{ m: 'delivery_refund', collectionId: DEL, amountMinor: 12000 },
			'/delivery-refund',
			{ expected_version: 7, collection_id: DEL },
			'refund'
		],
		[
			{ m: 'resolve', observationId: 'b1', resolution: 'match', operationId: 'r1', note: ' n ' },
			'/refund-observations/b1/resolve',
			{ expected_version: 7, resolution: 'match', refund_operation_id: 'r1', note: 'n' },
			'sync'
		],
		[
			{
				m: 'resolve',
				observationId: 'b1',
				resolution: 'accept_external',
				operationId: 'r1',
				note: 'n'
			},
			'/refund-observations/b1/resolve',
			{ expected_version: 7, resolution: 'accept_external', note: 'n' },
			'sync'
		],
		[
			{ m: 'decide', operationId: 'r1', decision: 'failed', refundId: 're_x', note: 'n' },
			'/refund-operations/r1/decide',
			{ expected_version: 7, decision: 'failed', note: 'n' },
			'sync'
		],
		[
			{ m: 'decide', operationId: 'r1', decision: 'succeeded', refundId: ' re_x ', note: 'n' },
			'/refund-operations/r1/decide',
			{ expected_version: 7, decision: 'succeeded', provider_refund_id: 're_x', note: 'n' },
			'sync'
		],
		[
			{ m: 'clear_refund', operationId: 'r1', decision: 'keep', note: 'n' },
			'/refund-operations/r1/clear-mismatch',
			{ expected_version: 7, decision: 'keep', note: 'n' },
			'sync'
		],
		[
			{ m: 'clear_payment', collectionId: CAKE, note: 'n' },
			`/payment-collections/${CAKE}/clear-mismatch`,
			{ expected_version: 7, note: 'n' },
			'sync'
		],
		[
			{ m: 're_reserve', operationId: 'r1', collectionId: CAKE, note: '  ' },
			'/refund-operations/r1/re-reserve',
			{ expected_version: 7 },
			'refund'
		],
		[
			{ m: 're_reserve', operationId: 'r1', collectionId: CAKE, note: 'again' },
			'/refund-operations/r1/re-reserve',
			{ expected_version: 7, note: 'again' },
			'refund'
		],
		[
			{ m: 'payout', preview },
			'/manual-payouts',
			{ expected_version: 6, preview_id: preview.preview_id, preview_hash: preview.preview_hash },
			'command'
		],
		[
			{
				m: 'payout_outcome',
				operationId: 'r1',
				outcome: 'submitted',
				transferIdentity: 'x',
				evidence: 'y',
				amountMinor: 1
			},
			'/manual-payouts/r1/outcome',
			{ expected_version: 7, outcome: 'submitted' },
			'command'
		],
		[
			{ m: 'payout_outcome', operationId: 'r1', outcome: 'withdrawn', evidence: ' e ' },
			'/manual-payouts/r1/outcome',
			{ expected_version: 7, outcome: 'withdrawn', evidence_ref: 'e' },
			'command'
		],
		[
			{
				m: 'payout_outcome',
				operationId: 'r1',
				outcome: 'succeeded',
				transferIdentity: ' T1 ',
				evidence: 'e',
				amountMinor: 9000
			},
			'/manual-payouts/r1/outcome',
			{
				expected_version: 7,
				outcome: 'succeeded',
				transfer_identity: 'T1',
				amount_minor: 9000,
				currency: 'GBP',
				evidence_ref: 'e'
			},
			'command'
		]
	];

	it.each(cases)('builds the exact body (%#)', (input, path, body, mode) => {
		const r = moneyRequest(order(), input);
		expect(r.path).toBe(`/api/v1/admin/orders/${ID}${path}`);
		expect(r.body).toEqual(body);
		expect(r.mode).toBe(mode);
		expect(JSON.stringify(r.body)).not.toMatch(/null|undefined/);
	});

	it('keys one request per version and exact body', () => {
		const a: MoneyInput = {
			m: 'refund',
			allocations: [{ collection_id: CAKE, amount_minor: 100 }],
			reason: 'x'
		};
		const b: MoneyInput = { ...a, reason: 'y' };
		expect(moneySlot(order(), a)).toBe(moneySlot(order(), { ...a, reason: ' x ' }));
		expect(moneySlot(order(), a)).not.toBe(moneySlot(order(), b));
		expect(moneySlot(order(), a)).not.toBe(moneySlot(order({ version: 8 }), a));
		const key = moneyKey(order(), 'payout_outcome', 'abcd1234');
		expect(key).toBe(`chef-payout-outcome-${ID}-v7-abcd1234`);
		expect(/^[\x21-\x7e]{1,128}$/.test(key)).toBe(true);
		expect(targetCollections(a)).toEqual([CAKE]);
		expect(targetCollections({ m: 'cancel', reason: 'x', amountMinor: 0 })).toEqual([]);
	});

	it('posts with the key and reads 200 and 202', async () => {
		const seen: { url: string; body: unknown; key: string }[] = [];
		let reply: unknown = { order_id: ID, decision_id: 'd1' };
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
			await sendMoney(api, order(), { m: 'clear_payment', collectionId: CAKE, note: 'n' }, 'k1')
		).toEqual({ kind: 'done' });
		expect(seen[0].key).toBe('k1');
		reply = { order_id: ID, operation_id: 'op1', state: 'refund_pending', poll_after_seconds: 2 };
		status = 202;
		expect(
			await sendMoney(
				api,
				order(),
				{ m: 'delivery_refund', collectionId: DEL, amountMinor: 1 },
				'k2'
			)
		).toEqual({
			kind: 'accepted',
			mode: 'refund',
			accepted: reply
		});
		reply = {
			order_id: ID,
			operation_id: 'c1',
			state: 'cancellation_pending',
			poll_after_seconds: 3
		};
		expect(
			await sendMoney(api, order(), { m: 'cancel', reason: 'x', amountMinor: 0 }, 'k3')
		).toMatchObject({ kind: 'accepted', mode: 'command' });
		status = 200;
		reply = preview;
		expect(
			await previewPayout(
				api,
				order(),
				{ collectionId: CAKE, unavailable: ' a ', recipient: 'b ' },
				'k4'
			)
		).toEqual(preview);
		expect(seen[3]).toEqual({
			url: `https://api.test/api/v1/admin/orders/${ID}/manual-payouts/preview`,
			body: { collection_id: CAKE, unavailable_evidence_ref: 'a', recipient_evidence_ref: 'b' },
			key: 'k4'
		});
	});

	it('settles each accepted command from a fresh read', () => {
		const cmd = (state: string, outcome: string | null = null) => [
			{
				id: 'c1',
				kind: 'cancel',
				requested_by: 'staff',
				received_at: '',
				state,
				outcome_status: outcome,
				decision: null
			}
		];
		const cancel: MoneyInput = { m: 'cancel', reason: 'x', amountMinor: 9000 };
		expect(moneySettled(order({ commands: cmd('prepared') }), cancel, 'c1')).toBe('pending');
		expect(
			moneySettled(
				order({ lifecycle: 'cancellation_pending', commands: cmd('secured') }),
				cancel,
				'c1'
			)
		).toBe('pending');
		expect(
			moneySettled(
				order({ lifecycle: 'cancelled', commands: cmd('secured', 'cancelled') }),
				cancel,
				'c1'
			)
		).toBe('done');
		expect(moneySettled(order({ commands: cmd('secured', 'refused') }), cancel, 'c1')).toBe(
			'refused'
		);
		const payout: MoneyInput = { m: 'payout', preview };
		expect(moneySettled(order(), payout, 'c1')).toBe('pending');
		expect(moneySettled(order({ commands: cmd('secured', 'reserved') }), payout, 'c1')).toBe(
			'done'
		);
		expect(moneySettled(order({ commands: cmd('secured', 'refused') }), payout, 'c1')).toBe(
			'refused'
		);
		const refund: MoneyInput = {
			m: 'refund',
			allocations: [{ collection_id: CAKE, amount_minor: 1000 }],
			reason: 'x'
		};
		const old = op({ id: 'old', state: 'succeeded' });
		expect(moneySettled(order({ refund_operations: [old] }), refund, 'e1', ['old'])).toBe(
			'pending'
		);
		expect(
			moneySettled(
				order({ refund_operations: [old, op({ id: 'n', state: 'reserved' })] }),
				refund,
				'e1',
				['old']
			)
		).toBe('reserved');
		expect(
			moneySettled(
				order({ refund_operations: [old, op({ id: 'n', state: 'pending' })] }),
				refund,
				'e1',
				['old']
			)
		).toBe('sent');
		expect(
			moneySettled(
				order({ refund_operations: [old, op({ id: 'n', state: 'succeeded' })] }),
				refund,
				'e1',
				['old']
			)
		).toBe('succeeded');
		expect(
			moneySettled(
				order({ refund_operations: [old, op({ id: 'n', state: 'failed' })] }),
				refund,
				'e1',
				['old']
			)
		).toBe('failed');
	});

	it('polls until a refund leaves “reserved” or a command is secured', async () => {
		const refund: MoneyInput = { m: 'delivery_refund', collectionId: DEL, amountMinor: 12000 };
		const reads = [
			order({ refund_operations: [op({ id: 'n', collection_id: DEL, state: 'reserved' })] }),
			order({ refund_operations: [op({ id: 'n', collection_id: DEL, state: 'submitting' })] }),
			order({ refund_operations: [op({ id: 'n', collection_id: DEL, state: 'pending' })] })
		];
		let i = 0;
		const waits: number[] = [];
		const r = await pollMoney(
			async () => reads[Math.min(i++, 2)],
			refund,
			{ operation_id: 'n', poll_after_seconds: 2 },
			{ wait: async (ms) => void waits.push(ms) }
		);
		expect(r.outcome).toBe('sent');
		expect(i).toBe(3);
		expect(waits).toEqual([2000, 2000, 2000]);
		// Still reserved after every try: reported as reserved.
		const stuck = await pollMoney(
			async () => reads[0],
			refund,
			{ operation_id: 'n', poll_after_seconds: 30 },
			{ tries: 2, wait: async (ms) => void waits.push(ms) }
		);
		expect(stuck.outcome).toBe('reserved');
		expect(waits.slice(-1)).toEqual([10000]);
		// A failed read is skipped; a 401 stops.
		let n = 0;
		const flaky = await pollMoney(
			async () => {
				if (n++ === 0) throw new ApiError(503, { code: 'UNAVAILABLE' });
				return order({ lifecycle: 'cancelled' });
			},
			{ m: 'cancel', reason: 'x', amountMinor: 0 },
			{ operation_id: 'c', poll_after_seconds: 1 },
			{ wait: async () => {} }
		);
		expect(flaky.outcome).toBe('done');
		await expect(
			pollMoney(
				async () => {
					throw new ApiError(401, {});
				},
				refund,
				{ operation_id: 'n', poll_after_seconds: 1 },
				{ wait: async () => {} }
			)
		).rejects.toThrow();
		// Leaving the page stops polling.
		const gone = await pollMoney(
			async () => reads[2],
			refund,
			{ operation_id: 'n', poll_after_seconds: 1 },
			{ wait: async () => {}, active: () => false }
		);
		expect(gone).toEqual({ order: null, outcome: 'pending' });
	});

	it('says what happened in plain words', () => {
		const o = order();
		expect(moneyDone(o, { m: 'cancel', reason: 'x', amountMinor: 9000 })).toMatch(
			/^Cancelled ✓ · .*£90 \(everything paid\) is being returned/
		);
		const refund: MoneyInput = {
			m: 'refund',
			allocations: [{ collection_id: CAKE, amount_minor: 2000 }],
			reason: 'x'
		};
		expect(moneyDone(o, refund, 'reserved')).toMatch(
			/^Reserved ✓ · the refund of £20 goes to the payment provider next/
		);
		expect(moneyDone(o, refund, 'sent')).toMatch(/^Sent ✓/);
		expect(moneyDone(o, refund, 'succeeded')).toMatch(/^Refunded ✓/);
		expect(moneyDone(o, refund, 'failed')).toMatch(/stays owed/);
		expect(
			moneyDone(o, { m: 'resolve', observationId: 'b', resolution: 'accept_external', note: 'n' })
		).toMatch(/^Accepted ✓/);
		expect(moneyDone(o, { m: 'payout_outcome', operationId: 'r', outcome: 'withdrawn' })).toMatch(
			/^Withdrawn ✓/
		);
	});

	it('words every refusal without codes', () => {
		const e = (code: string, status = 409) => new ApiError(status, { code });
		for (const code of [
			'CONFLICT',
			'CANCELLATION_NOT_ELIGIBLE',
			'REFUND_LIMIT',
			'RECONCILIATION_REQUIRED',
			'REFUND_IN_PROGRESS',
			'NO_REFUND_PENDING',
			'IDENTITY_UNPROVEN',
			'WINDOW_OPEN',
			'EVIDENCE_REQUIRED',
			'DUPLICATE',
			'MANUAL_PAYOUT_NOT_ELIGIBLE',
			'PREVIEW_STALE',
			'FORBIDDEN',
			'NOT_FOUND',
			'KEY_REUSED'
		]) {
			const r = moneyRefusal(e(code), 'refund');
			expect(r.text).not.toMatch(/[A-Z]{3,}_[A-Z]/);
			expect(r.stale).toBe(false);
		}
		expect(moneyRefusal(e('STALE_VERSION'), 'cancel').stale).toBe(true);
		expect(moneyRefusal(e('REFUND_LIMIT'), 'refund').text).toMatch(/more than can still come back/);
		expect(moneyRefusal(e('INVALID_INPUT', 422), 'preview').text).toMatch(/never bank details/);
		expect(moneyRefusal(e('EVIDENCE_REQUIRED'), 'decide').text).toMatch(/complete check/);
		expect(moneyRefusal(e('UNAVAILABLE', 503), 'cancel')).toMatchObject({
			retry: true,
			reload: false
		});
		expect(moneyRefusal(new ApiError(0, { code: 'NETWORK' }), 'refund')).toMatchObject({
			retry: true
		});
		expect(moneyRefusal(new ApiError(401, {}), 'refund').signIn).toBe(true);
		expect(moneyRefusal(e('FORBIDDEN', 403), 'resolve').text).toBe(
			'Only Elina (chef) can do this.'
		);
	});
});
