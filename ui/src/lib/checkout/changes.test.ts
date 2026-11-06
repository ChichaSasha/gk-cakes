import { describe, expect, it } from 'vitest';
import { ApiError } from '../api';
import {
	MOVE_FIXED,
	allows,
	apiActions,
	amountChanged,
	boundaryAlert,
	canAct,
	cancelBaseline,
	cancelOutcomeSince,
	cancelViaChef,
	deliveryProblemOpen,
	moveBaseline,
	moveOutcomeSince,
	cancelledStory,
	cancelProblemFor,
	changeInProgress,
	changePoll,
	mayCancel,
	moveIntro,
	moveOutcome,
	moveProblemFor,
	moveView,
	previewRefreshDelay,
	previewView,
	refundParts,
	refundProgress,
	refundSteps,
	timeChange,
	timeLeft,
	trackKind
} from './changes';
import type { Who } from './flow';
import type { CancellationPreview, MoveChoices, Order, Refund, RefundPart, Slot } from './types';

const guest: Who = { kind: 'guest', secret: 'a'.repeat(43) };
const owner: Who = { kind: 'account', token: 't' };

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

function preview(over: Partial<CancellationPreview> = {}): CancellationPreview {
	return {
		preview_id: 'p1',
		order_id: 'o',
		order_version: 4,
		rate: 100,
		paid_minor: 9000,
		amount_minor: 9000,
		currency: 'GBP',
		allocations: [{ collection_id: 'c1', amount_minor: 9000 }],
		full_refund_deadline_at: '2026-10-17T10:00:00Z',
		computed_at: '2026-10-14T06:00:00Z',
		expires_at: '2026-10-14T06:10:00Z',
		preview_hash: 'h'.repeat(64),
		delivery_pending_minor: 0,
		unmatched_minor: 0,
		...over
	};
}

describe('who may act (capability gating)', () => {
	it('needs the signed-in owner or this tab’s guest capability', () => {
		expect(canAct(owner)).toBe(true);
		expect(canAct(guest)).toBe(true);
		expect(canAct(null)).toBe(false);
		expect(canAct(undefined)).toBe(false);
		expect(canAct({ kind: 'account', token: '' })).toBe(false);
		expect(mayCancel(order(), null)).toBe(false);
		expect(timeChange(order(), null)).toBe('none');
	});
});

describe('mayCancel', () => {
	it('offers cancellation for confirmed, in production and ready orders only', () => {
		for (const lifecycle of ['confirmed', 'in_production', 'ready'] as const) {
			expect(mayCancel(order({ lifecycle }), guest)).toBe(true);
		}
		for (const lifecycle of [
			'awaiting_payment',
			'confirmation_pending',
			'paid_awaiting_agreement',
			'completed',
			'cancelled',
			'cancellation_pending'
		] as const) {
			expect(mayCancel(order({ lifecycle }), guest)).toBe(false);
		}
	});

	it('never for a GK delivery that has been sent out (or has a delivery problem)', () => {
		const sent = { state: 'out_for_delivery', at: 'x', pending: null };
		expect(mayCancel(order({ fulfillment_mode: 'gk_delivery', lifecycle: 'ready' }), owner)).toBe(
			true
		);
		expect(
			mayCancel(
				order({ fulfillment_mode: 'gk_delivery', lifecycle: 'ready', fulfillment: sent }),
				owner
			)
		).toBe(false);
		expect(
			mayCancel(
				order({
					fulfillment_mode: 'gk_delivery',
					lifecycle: 'ready',
					fulfillment: { state: 'exception', at: 'x', pending: null }
				}),
				owner
			)
		).toBe(false);
	});

	it('not while another protected change is unresolved', () => {
		const processing = {
			kind: 'pickup_move',
			state: 'processing',
			reason: null,
			requested_by: 'customer',
			requested_at: 'x',
			to: { date: '2026-10-22', id: 's13', starts_at: 'x', ends_at: 'x' }
		};
		expect(mayCancel(order({ appointment_change: processing }), guest)).toBe(false);
		expect(
			mayCancel(
				order({
					lifecycle: 'ready',
					fulfillment: { state: 'awaiting_handover', at: null, pending: 'handover' }
				}),
				guest
			)
		).toBe(false);
		expect(
			mayCancel(
				order({ delivery_choice: { open: true, reason: 'not_possible', pending: true } }),
				guest
			)
		).toBe(false);
		expect(changeInProgress(order({ appointment_change: processing }))).toBe(true);
		expect(changePoll(order({ appointment_change: processing }))).toBe(true);
		expect(changePoll(order())).toBe(false);
	});
});

describe('timeChange', () => {
	it('collection and own courier move; GK delivery contacts the chef', () => {
		expect(timeChange(order(), guest)).toBe('move');
		expect(timeChange(order({ fulfillment_mode: 'customer_courier' }), guest)).toBe('move');
		expect(timeChange(order({ fulfillment_mode: 'gk_delivery' }), guest)).toBe('contact_chef');
		expect(timeChange(order({ lifecycle: 'completed' }), guest)).toBe('none');
		expect(timeChange(order({ lifecycle: 'cancelled' }), guest)).toBe('none');
	});

	it('shows a move in progress', () => {
		const o = order({
			appointment_change: {
				kind: 'pickup_move',
				state: 'processing',
				reason: null,
				requested_by: 'customer',
				requested_at: 'x',
				to: { date: '2026-10-22', id: 's13', starts_at: 'x', ends_at: 'x' }
			}
		});
		expect(timeChange(o, guest)).toBe('moving');
	});
});

describe('timeLeft', () => {
	it('words the time between two server instants', () => {
		expect(timeLeft('2026-10-14T06:00:00Z', '2026-10-17T10:00:00Z')).toBe('3 days 4 hours');
		expect(timeLeft('2026-10-16T10:00:00Z', '2026-10-17T10:00:00Z')).toBe('1 day');
		expect(timeLeft('2026-10-17T04:40:00Z', '2026-10-17T10:00:00Z')).toBe('5 hours 20 minutes');
		expect(timeLeft('2026-10-17T09:00:00Z', '2026-10-17T10:00:00Z')).toBe('1 hour');
		expect(timeLeft('2026-10-17T09:48:00Z', '2026-10-17T10:00:00Z')).toBe('12 minutes');
		expect(timeLeft('2026-10-17T09:59:30Z', '2026-10-17T10:00:00Z')).toBe('less than a minute');
		expect(timeLeft('2026-10-17T10:00:00Z', '2026-10-17T10:00:00Z')).toBe('no time');
	});
});

describe('previewView', () => {
	it('before the deadline: the API’s full amount, the side and the time left as of the server', () => {
		const v = previewView(preview());
		expect(v.before).toBe(true);
		expect(v.side).toBe('Cancelling before 11:00 (London time) on Sat 17 October');
		expect(v.amount).toBe('£90');
		expect(v.basis).toBe('Everything you paid: a full refund.');
		expect(v.timeLeft).toBe('3 days 4 hours');
		expect(v.checkedAt).toBe('07:00');
		expect(v.roundingExample).toBe(false);
		expect(v.split).toEqual([]);
		expect(v.buttonLabel).toBe('Cancel order · refund £90');
	});

	it('after the deadline: shows amount_minor as it is (never halves it) with the rounding example', () => {
		// A deliberately odd amount: the page must show it, not 50% of paid.
		const v = previewView(preview({ rate: 50, amount_minor: 4751, paid_minor: 9501 }));
		expect(v.before).toBe(false);
		expect(v.side).toBe('Cancelling after the full-refund deadline');
		expect(v.amount).toBe('£47.51');
		expect(v.basis).toBe('50% of what you paid, rounded in your favour.');
		expect(v.timeLeft).toBeUndefined();
		expect(v.roundingExample).toBe(true);
		expect(v.buttonLabel).toBe('Cancel order · refund £47.51');
	});

	it('with GK delivery: the cakes back to the payment method and the chef’s delivery part', () => {
		const v = previewView(
			preview({
				rate: 50,
				paid_minor: 21000,
				amount_minor: 10500,
				allocations: [{ collection_id: 'c1', amount_minor: 4500 }],
				delivery_pending_minor: 6000
			}),
			{ delivery: true }
		);
		expect(v.amount).toBe('£105');
		expect(v.basis).toBe('50% of what you paid (cakes and delivery), rounded in your favour.');
		expect(v.split).toEqual([
			{ label: 'Cakes · back to your payment method', amount: '£45' },
			{ label: 'Delivery · sent separately by the chef', amount: '£60' }
		]);
		expect(v.roundingExample).toBe(false);
		expect(v.buttonLabel).toBe('Cancel order · refund £105');
	});

	it('a stray delivery payment is stated beside the amount, not added to it', () => {
		const v = previewView(preview({ rate: 50, amount_minor: 5000, unmatched_minor: 12000 }));
		expect(v.amount).toBe('£50');
		expect(v.unmatched).toBe(
			'We’ll also return in full a delivery payment of £120 that couldn’t be used for your order.'
		);
	});

	it('a free order has nothing to refund', () => {
		const v = previewView(preview({ paid_minor: 0, amount_minor: 0, allocations: [] }));
		expect(v.nothingPaid).toBe(true);
		expect(v.basis).toBe('This order cost nothing, so there’s nothing to refund.');
		expect(v.buttonLabel).toBe('Cancel order');
	});
});

describe('boundaryAlert', () => {
	it('appears only when the new preview’s rate is lower than the one shown', () => {
		const seen = preview();
		const fresh = preview({
			preview_id: 'p2',
			rate: 50,
			amount_minor: 4500,
			computed_at: '2026-10-17T10:00:01Z'
		});
		expect(boundaryAlert(seen, fresh)).toEqual({
			deadline: '11:00 (London time) on Sat 17 October',
			amount: '£45'
		});
		expect(boundaryAlert(seen, preview({ preview_id: 'p3' }))).toBeUndefined();
		expect(boundaryAlert(null, fresh)).toBeUndefined();
	});
});

describe('previewRefreshDelay', () => {
	it('refreshes at the deadline or the 10-minute expiry, whichever is first, by server time', () => {
		const t0 = 1_000_000;
		// 10 minutes to expiry; the deadline is days away.
		expect(previewRefreshDelay(preview(), t0, t0)).toBe(10 * 60_000 + 1000);
		// The deadline 2 minutes after the preview: refresh then.
		const near = preview({
			computed_at: '2026-10-17T09:58:00Z',
			expires_at: '2026-10-17T10:08:00Z'
		});
		expect(previewRefreshDelay(near, t0, t0)).toBe(2 * 60_000 + 1000);
		// 30 s have passed in this browser since the answer.
		expect(previewRefreshDelay(near, t0, t0 + 30_000)).toBe(90_000 + 1000);
		// After the deadline (rate 50) only the expiry counts.
		expect(previewRefreshDelay(preview({ rate: 50 }), t0, t0)).toBe(10 * 60_000 + 1000);
	});
});

describe('cancelProblemFor', () => {
	const err = (status: number, code: string, retryable = false) =>
		new ApiError(status, { code, retryable, message: 'x', request_id: 'r' });

	it('asks for a new preview at the boundary, an expired preview or a changed order', () => {
		expect(cancelProblemFor(err(409, 'PREVIEW_STALE')).next).toBe('repreview');
		expect(cancelProblemFor(err(422, 'INVALID_INPUT')).next).toBe('repreview');
		expect(cancelProblemFor(err(409, 'STALE_VERSION')).next).toBe('repreview');
	});

	it('closes the panel when the order can’t be cancelled now', () => {
		expect(cancelProblemFor(err(409, 'CANCELLATION_NOT_ELIGIBLE'))).toEqual({
			message: 'This order can no longer be cancelled here. Its current status is shown above.',
			next: 'close'
		});
		expect(cancelProblemFor(err(409, 'CONFLICT')).next).toBe('close');
		expect(cancelProblemFor(err(404, 'NOT_FOUND')).next).toBe('close');
		expect(cancelProblemFor(err(401, 'UNAUTHENTICATED')).next).toBe('close');
	});

	it('retries an unreachable service with the same key', () => {
		expect(cancelProblemFor(err(0, 'NETWORK', true)).next).toBe('retry');
		expect(cancelProblemFor(err(503, 'UNAVAILABLE', true)).next).toBe('retry');
	});
});

function refund(over: Partial<Refund> = {}): Refund {
	return {
		state: 'requested',
		target_minor: 9000,
		succeeded_minor: 0,
		in_progress_minor: 9000,
		...over
	};
}

describe('refund tracker', () => {
	const states = (r: Refund) => refundSteps(r).map((s) => `${s.label}:${s.state}`);

	it('requested: nothing sent yet', () => {
		expect(states(refund())).toEqual([
			'Refund requested:current',
			'Waiting to be sent to our payment provider:upcoming',
			'Return to your payment method:upcoming'
		]);
	});

	it('pending: with our payment provider', () => {
		expect(states(refund({ state: 'pending' }))).toEqual([
			'Refund requested:done',
			'Sent to our payment provider · waiting for them:current',
			'Return to your payment method:upcoming'
		]);
	});

	it('needs a check: never “returned”', () => {
		const s = states(refund({ state: 'needs_attention' }));
		expect(s[1]).toBe('Needs a check by our team:current');
		expect(s[2]).toBe('Return to your payment method:upcoming');
	});

	it('returned: past tense only now, every step ticked', () => {
		expect(
			states(refund({ state: 'returned', succeeded_minor: 9000, in_progress_minor: 0 }))
		).toEqual([
			'Refund requested:done',
			'Sent to our payment provider:done',
			'Returned to your payment method:done'
		]);
	});

	it('partly returned: the card part is back only when nothing is in progress and the chef has the rest', () => {
		expect(
			trackKind(
				refund({
					state: 'partly_returned',
					succeeded_minor: 4500,
					in_progress_minor: 0,
					delivery_pending_minor: 6000
				})
			)
		).toBe('returned');
		expect(
			trackKind(
				refund({ state: 'partly_returned', succeeded_minor: 4500, in_progress_minor: 4500 })
			)
		).toBe('pending');
		expect(trackKind(refund({ state: 'partly_returned', in_progress_minor: 0 }))).toBe('pending');
		expect(trackKind(refund({ state: 'something_new' }))).toBe('requested');
	});

	it('the chef’s delivery refund is its own line; amounts are the API’s', () => {
		const v = refundProgress(
			refund({
				state: 'partly_returned',
				target_minor: 10500,
				succeeded_minor: 4500,
				in_progress_minor: 0,
				delivery_pending_minor: 6000
			})
		);
		expect(v.trackLabel).toBe('Cake refund progress');
		expect(v.lines).toEqual([
			{ label: 'Cakes · to your payment method', status: 'Returned' },
			{
				label: 'Delivery · sent separately by the chef',
				status: 'Waiting for the chef',
				amount: '£60'
			}
		]);
		expect(v.due).toBe('£105');
		expect(v.returnedSoFar).toBe('£45');
		const plain = refundProgress(refund());
		expect(plain.trackLabel).toBe('Refund progress');
		expect(plain.lines).toEqual([]);
		expect(plain.returnedSoFar).toBe('£0');
	});
});

describe('cancelledStory', () => {
	const cancelled = (over: Partial<Order>) =>
		order({ lifecycle: 'cancelled', cancelled_at: 'x', refund: refund(), ...over });
	const c = (rate: number | null, requested_by = 'customer') => ({
		state: 'cancelled',
		reason: 'customer_cancelled',
		rate,
		amount_minor: 9000,
		requested_by,
		requested_at: 'x'
	});

	it('customer cancellation before and after the deadline, with and without delivery', () => {
		expect(
			cancelledStory(cancelled({ cancel_reason: 'customer_cancelled', cancellation: c(100) }))?.why
		).toBe('You cancelled before the full-refund deadline, so you get everything you paid back.');
		expect(
			cancelledStory(cancelled({ cancel_reason: 'customer_cancelled', cancellation: c(50) }))?.why
		).toBe(
			'You cancelled after the full-refund deadline, so you get 50% of what you paid, rounded in your favour.'
		);
		expect(
			cancelledStory(
				cancelled({
					cancel_reason: 'customer_cancelled',
					cancellation: c(50),
					fulfillment_mode: 'gk_delivery',
					refund: refund({ delivery_pending_minor: 6000 })
				})
			)?.why
		).toMatch(/50% of everything you paid, cakes and delivery/);
	});

	it('the chef’s cancellation apologises and gives 100%', () => {
		expect(cancelledStory(cancelled({ cancel_reason: 'chef_cancelled' }))).toEqual({
			title: 'We had to cancel your order',
			why: 'We’re sorry: we can’t make your cake for this date, so we’ve cancelled your order. You get 100% of what you paid back.'
		});
	});

	it('full refunds: date not agreed, and we couldn’t deliver', () => {
		expect(
			cancelledStory(cancelled({ cancel_reason: 'late_payment_returned', cancellation: c(100) }))
		).toEqual({
			title: 'Your order is cancelled · full refund',
			why: 'You chose not to go ahead with the date, so you get everything you paid back.'
		});
		expect(
			cancelledStory(
				cancelled({ cancel_reason: 'late_payment_returned', cancellation: c(100, 'chef') })
			)?.why
		).toBe('The date couldn’t be agreed, so you get everything you paid back.');
		expect(cancelledStory(cancelled({ cancel_reason: 'delivery_not_possible' }))?.why).toBe(
			'We couldn’t deliver your cake and you chose a full refund, so you get everything you paid back.'
		);
		expect(
			cancelledStory(
				cancelled({
					cancel_reason: 'delivery_not_possible',
					fulfillment_mode: 'gk_delivery',
					delivery: { state: 'paid', quote: null, paid_minor: 12000 }
				})
			)?.why
		).toBe(
			'We couldn’t deliver your cake and you chose a full refund: everything you paid, cakes and delivery, comes back.'
		);
	});

	it('leaves other cancellations to the order page’s own wording', () => {
		expect(cancelledStory(cancelled({ cancel_reason: 'payment_not_completed' }))).toBeUndefined();
	});
});

const slot = (id: string, h: number, over: Partial<Slot> = {}): Slot => ({
	id,
	starts_at: `2026-10-22T${String(h - 1).padStart(2, '0')}:00:00Z`,
	ends_at: `2026-10-22T${String(h).padStart(2, '0')}:00:00Z`,
	availability: 'available',
	reason_code: null,
	...over
});

function choices(over: Partial<MoveChoices> = {}): MoveChoices {
	return {
		schedule_version: 3,
		as_of: '2026-10-21T20:00:00Z',
		timezone: 'Europe/London',
		date: '2026-10-22',
		fulfillment_mode: 'collection',
		day_status: 'open',
		purpose: 'pickup_move',
		order_id: 'o',
		current_slot: { id: 's11', starts_at: '2026-10-22T10:00:00Z', ends_at: '2026-10-22T11:00:00Z' },
		move_until: '2026-10-22T04:00:00Z',
		slots: [
			slot('s11', 11, { availability: 'unavailable', reason_code: 'current_slot' }),
			slot('s12', 12),
			slot('s13', 13, { availability: 'unavailable', reason_code: 'shelf_life' }),
			slot('s14', 14)
		],
		...over
	};
}

describe('moveView', () => {
	it('lists the current time (marked) and the times the API offers on the same day', () => {
		const v = moveView(choices());
		expect(v.status).toBe('open');
		expect(v.legend).toBe('Collection time on Thu 22 October · London time');
		expect(v.until).toBe('05:00 (London time) on Thu 22 October');
		expect(v.intro).toBe(
			'You can move to another time on the same day until 05:00 (London time) on Thu 22 October (6 hours before your current time). For another date, please contact the chef.'
		);
		expect(v.options.map((o) => [o.label, o.note, o.disabled])).toEqual([
			['11:00–12:00', 'Your current time', false],
			['12:00–13:00', 'Available', false],
			['14:00–15:00', 'Available', false]
		]);
		expect(v.currentId).toBe('s11');
		expect(v.canMove).toBe(true);
		expect(v.note).toBeUndefined();
	});

	it('own courier: the courier’s collection time', () => {
		expect(moveView(choices({ fulfillment_mode: 'customer_courier' })).legend).toBe(
			'Courier collection time on Thu 22 October · London time'
		);
	});

	it('after the six-hour cutoff: the day’s later times disabled, with the reason', () => {
		const v = moveView(
			choices({
				day_status: 'too_late',
				as_of: '2026-10-22T08:30:00Z',
				slots: [
					slot('s9', 9, { availability: 'unavailable', reason_code: 'too_late' }),
					slot('s11', 11, { availability: 'unavailable', reason_code: 'current_slot' }),
					slot('s12', 12, { availability: 'unavailable', reason_code: 'too_late' })
				]
			})
		);
		expect(v.status).toBe('too_late');
		expect(v.canMove).toBe(false);
		expect(v.options.map((o) => [o.label, o.disabled])).toEqual([
			['11:00–12:00', false],
			['12:00–13:00', true]
		]);
		expect(v.note).toBe(
			'Times can be changed online until 6 hours before your current time (05:00 (London time) on Thu 22 October), and that has passed. If you need a different time, please contact the chef.'
		);
	});

	it('a refused choice stays struck through; other gone times are dropped (Q15)', () => {
		const v = moveView(
			choices({
				slots: [
					slot('s11', 11, { reason_code: 'current_slot', availability: 'unavailable' }),
					slot('s12', 12, { availability: 'unavailable', reason_code: 'past' }),
					slot('s14', 14)
				]
			}),
			's12'
		);
		expect(v.options.map((o) => [o.label, o.note, o.gone, o.disabled])).toEqual([
			['11:00–12:00', 'Your current time', false, false],
			['12:00–13:00', 'No longer available', true, true],
			['14:00–15:00', 'Available', false, false]
		]);
	});

	it('no other times, pending, closed and not eligible say so', () => {
		expect(moveView(choices({ slots: [] })).note).toBe(
			'There are no other times left on this day. If you need a different time, please contact the chef.'
		);
		expect(moveView(choices({ slots: [] })).options[0]).toMatchObject({ id: 's11', current: true });
		expect(moveView(choices({ day_status: 'pending' })).canMove).toBe(false);
		expect(moveView(choices({ day_status: 'closed' })).note).toMatch(/No other times/);
		expect(moveView(choices({ day_status: 'not_eligible' })).note).toMatch(
			/can’t be changed online/
		);
		expect(moveView(choices({ day_status: 'surprise' })).status).toBe('not_eligible');
	});

	it('the intro without a known limit', () => {
		expect(moveIntro(null)).toBe(
			'You can move to another time on the same day until 6 hours before your current time. For another date, please contact the chef.'
		);
	});
});

describe('moveProblemFor', () => {
	const err = (status: number, code: string, retryable = false) =>
		new ApiError(status, { code, retryable, message: 'x', request_id: 'r' });

	it('a stale choice keeps the old booking and reloads the choices with it struck through', () => {
		expect(moveProblemFor(err(409, 'SLOT_UNAVAILABLE'))).toEqual({
			message:
				'That time is no longer available. Your booking hasn’t changed: please choose another time.',
			next: 'reload',
			gone: true
		});
		expect(moveProblemFor(err(409, 'MOVE_TOO_LATE')).next).toBe('reload');
		expect(moveProblemFor(err(409, 'MOVE_NOT_ELIGIBLE')).next).toBe('refresh');
		expect(moveProblemFor(err(409, 'STALE_VERSION')).next).toBe('refresh');
		expect(moveProblemFor(err(503, 'UNAVAILABLE', true)).next).toBe('retry');
		for (const code of ['SLOT_UNAVAILABLE', 'MOVE_TOO_LATE', 'MOVE_NOT_ELIGIBLE', 'CONFLICT']) {
			expect(moveProblemFor(err(409, code)).message).toMatch(/hasn’t changed/);
		}
	});
});

describe('moveOutcome', () => {
	const ch = (state: string, reason: string | null = null) =>
		order({
			appointment_change: {
				kind: 'pickup_move',
				state,
				reason,
				requested_by: 'customer',
				requested_at: 'x',
				to: {
					date: '2026-10-22',
					id: 's14',
					starts_at: '2026-10-22T13:00:00Z',
					ends_at: '2026-10-22T14:00:00Z'
				}
			}
		});

	it('moved, refused and still processing', () => {
		expect(moveOutcome(ch('processing'))).toBeUndefined();
		expect(moveOutcome(ch('moved'))).toEqual({
			ok: true,
			message:
				'Your time is now 14:00–15:00 on Thu 22 October. Moving your time doesn’t change your refund deadline.'
		});
		expect(moveOutcome(ch('refused', 'slot_unavailable'))).toEqual({
			ok: false,
			message:
				'We couldn’t move your time: that time is no longer available. Your booking is unchanged.'
		});
		expect(moveOutcome(ch('refused', 'too_late'))?.message).toMatch(/less than 6 hours/);
		expect(moveOutcome(order())).toBeUndefined();
	});
});

describe('a GK delivery problem with its own choice (M3)', () => {
	const gk = (over: Partial<Order> = {}) => order({ fulfillment_mode: 'gk_delivery', ...over });

	it('no standard cancellation while the can’t-deliver choice is open; contact the chef instead', () => {
		const open = gk({ delivery_choice: { open: true, reason: 'not_possible', pending: false } });
		expect(deliveryProblemOpen(open)).toBe(true);
		expect(mayCancel(open, guest)).toBe(false);
		expect(cancelViaChef(open, guest)).toBe(true);
		expect(timeChange(open, guest)).toBe('contact_chef');
		const closed = gk({ delivery_choice: { open: false, reason: null, pending: false } });
		expect(mayCancel(closed, guest)).toBe(true);
		expect(cancelViaChef(closed, guest)).toBe(false);
	});

	it('the same for an open failed-delivery choice (when the API sends it)', () => {
		const failed = gk({
			lifecycle: 'ready',
			delivery_failure: {
				cause: 'our_side',
				at: '2026-10-22T10:40:00Z',
				state: 'recorded',
				open: true,
				pending: false
			}
		});
		expect(mayCancel(failed, owner)).toBe(false);
		expect(cancelViaChef(failed, owner)).toBe(true);
		expect(
			mayCancel(
				gk({
					delivery_failure: {
						cause: 'our_side',
						at: '2026-10-22T10:40:00Z',
						state: 'recorded',
						open: false,
						pending: false
					}
				}),
				owner
			)
		).toBe(true);
	});

	it('never for a viewer without access, a closed order or while a change is processed', () => {
		const open = { delivery_choice: { open: true, reason: 'not_possible', pending: false } };
		expect(cancelViaChef(gk(open), null)).toBe(false);
		expect(cancelViaChef(gk({ ...open, lifecycle: 'cancelled' }), guest)).toBe(false);
		expect(
			cancelViaChef(
				gk({ delivery_choice: { open: true, reason: 'not_possible', pending: true } }),
				guest
			)
		).toBe(false);
	});
});

describe('partly returned: the card part reads returned only when covered (m4)', () => {
	it('nothing in progress and a chef part, but returned + chef part short of the target: pending', () => {
		expect(
			trackKind(
				refund({
					state: 'partly_returned',
					target_minor: 10500,
					succeeded_minor: 2000,
					in_progress_minor: 0,
					delivery_pending_minor: 6000
				})
			)
		).toBe('pending');
		expect(
			trackKind(
				refund({
					state: 'partly_returned',
					target_minor: 10500,
					succeeded_minor: 4500,
					in_progress_minor: 0,
					delivery_pending_minor: 6000
				})
			)
		).toBe('returned');
	});
});

describe('this page’s own outcome only (M1, M2)', () => {
	const change = (state: string, requested_at: string, to = 's12') => ({
		kind: 'pickup_move',
		state,
		reason: state === 'refused' ? 'slot_unavailable' : null,
		requested_by: 'customer',
		requested_at,
		to: {
			date: '2026-10-22',
			id: to,
			starts_at: to === 's12' ? '2026-10-22T11:00:00Z' : '2026-10-22T13:00:00Z',
			ends_at: to === 's12' ? '2026-10-22T12:00:00Z' : '2026-10-22T14:00:00Z'
		}
	});

	it('moving twice: the earlier move’s outcome is never taken for the new one', () => {
		// Already moved to 12:00; the customer now asks for 14:00.
		const before = order({
			version: 7,
			appointment_change: change('moved', '2026-10-21T18:00:00Z')
		});
		const base = moveBaseline(before);
		// Read again before the new command shows: still the old outcome.
		expect(moveOutcomeSince(before, base)).toBeUndefined();
		const processing = order({
			version: 8,
			appointment_change: change('processing', '2026-10-21T20:01:00Z', 's14')
		});
		expect(moveOutcomeSince(processing, base)).toBeUndefined();
		const moved = order({
			version: 9,
			appointment_change: change('moved', '2026-10-21T20:01:00Z', 's14')
		});
		expect(moveOutcomeSince(moved, base)?.message).toMatch(/^Your time is now 14:00–15:00/);
		// A second move from there: the 14:00 outcome is the baseline now.
		const base2 = moveBaseline(moved);
		expect(moveOutcomeSince(moved, base2)).toBeUndefined();
		const refused = order({
			version: 10,
			appointment_change: change('refused', '2026-10-21T21:00:00Z')
		});
		expect(moveOutcomeSince(refused, base2)).toEqual({
			ok: false,
			message:
				'We couldn’t move your time: that time is no longer available. Your booking is unchanged.'
		});
	});

	it('retrying a cancellation after a refusal: the old refusal is not the new outcome', () => {
		const c = (state: string, requested_at: string) => ({
			state,
			reason: state === 'refused' ? 'order_changed' : 'customer_cancelled',
			rate: state === 'cancelled' ? 100 : null,
			amount_minor: state === 'cancelled' ? 9000 : null,
			requested_by: 'customer',
			requested_at
		});
		const before = order({ version: 6, cancellation: c('refused', '2026-10-12T06:00:00Z') });
		const base = cancelBaseline(before);
		expect(cancelOutcomeSince(before, base)).toBeUndefined();
		const pending = order({
			version: 7,
			lifecycle: 'cancellation_pending',
			cancellation: c('processing', '2026-10-14T06:01:00Z')
		});
		expect(cancelOutcomeSince(pending, base)).toBeUndefined();
		expect(
			cancelOutcomeSince(
				order({
					version: 8,
					lifecycle: 'cancelled',
					cancellation: c('cancelled', '2026-10-14T06:01:00Z')
				}),
				base
			)
		).toBe('cancelled');
		expect(
			cancelOutcomeSince(
				order({ version: 8, cancellation: c('refused', '2026-10-14T06:01:00Z') }),
				base
			)
		).toBe('refused');
	});
});

describe('amountChanged (m1)', () => {
	it('flags another amount or paid basis at the same rate; a rate change is the boundary alert', () => {
		const seen = preview();
		expect(amountChanged(seen, preview({ preview_id: 'p2' }))).toBe(false);
		expect(amountChanged(seen, preview({ amount_minor: 8000, paid_minor: 8000 }))).toBe(true);
		expect(amountChanged(seen, preview({ paid_minor: 9500 }))).toBe(true);
		expect(amountChanged(seen, preview({ rate: 50, amount_minor: 4500 }))).toBe(false);
		expect(amountChanged(null, preview())).toBe(false);
	});
});

// ---------------------------------------------------------------- slice 3: the API's actions, parts, kinds

describe('the API’s actions for this caller (0038)', () => {
	const q5 = { open: true, reason: 'pay_by_passed', pending: false };

	it('cancel follows actions, not the page’s own guess', () => {
		expect(mayCancel(order({ actions: ['cancel', 'move_pickup'] }), guest)).toBe(true);
		expect(mayCancel(order({ actions: ['move_pickup'] }), guest)).toBe(false);
		expect(mayCancel(order({ actions: [] }), owner)).toBe(false);
		// The API allows a cancellation after a secured redelivery choice
		// (the page's own rule never offered one after a dispatch).
		const exception = { state: 'exception', at: 'x', pending: null };
		const redelivery = order({
			fulfillment_mode: 'gk_delivery',
			lifecycle: 'ready',
			fulfillment: exception
		});
		expect(mayCancel(redelivery, owner)).toBe(false);
		expect(mayCancel({ ...redelivery, actions: ['cancel'] }, owner)).toBe(true);
	});

	it('an open delivery choice is still offered instead of the standard cancellation', () => {
		const o = order({
			fulfillment_mode: 'gk_delivery',
			delivery_choice: q5,
			actions: ['cancel', 'delivery_choice']
		});
		expect(mayCancel(o, owner)).toBe(false);
		expect(cancelViaChef(o, owner)).toBe(true);
		expect(cancelViaChef({ ...o, actions: ['delivery_choice'] }, owner)).toBe(false);
	});

	it('no viewer: nothing, whatever the actions say', () => {
		expect(allows(order({ actions: ['cancel'] }), null, 'cancel', true)).toBe(false);
		expect(mayCancel(order({ actions: ['cancel'] }), null)).toBe(false);
	});

	it('actions absent (an older API): the page’s own rule', () => {
		expect(apiActions(order())).toBeUndefined();
		expect(allows(order(), guest, 'cancel', true)).toBe(true);
		expect(allows(order(), guest, 'cancel', false)).toBe(false);
		expect(mayCancel(order(), guest)).toBe(true);
		expect(timeChange(order(), guest)).toBe('move');
	});

	it('move_pickup: a move, or the order’s time can’t be changed online', () => {
		expect(timeChange(order({ actions: ['cancel', 'move_pickup'] }), guest)).toBe('move');
		expect(timeChange(order({ actions: ['cancel'] }), guest)).toBe('fixed');
		expect(MOVE_FIXED).toBe(
			'This order’s time can’t be changed online. If you need a different time, please contact the chef.'
		);
		// GK delivery and a move in progress are unchanged.
		expect(timeChange(order({ fulfillment_mode: 'gk_delivery', actions: [] }), guest)).toBe(
			'contact_chef'
		);
		expect(timeChange(order({ lifecycle: 'completed', actions: [] }), guest)).toBe('none');
	});
});

describe('labelled cancellation allocations (0038 kind)', () => {
	it('cake only: no split (as before)', () => {
		const v = previewView(
			preview({ allocations: [{ collection_id: 'c1', amount_minor: 9000, kind: 'cake' }] })
		);
		expect(v.split).toEqual([]);
	});

	it('cake and a stray payment: each labelled exactly; the stray isn’t said twice', () => {
		const v = previewView(
			preview({
				rate: 50,
				amount_minor: 4500,
				unmatched_minor: 12000,
				allocations: [
					{ collection_id: 'c1', amount_minor: 4500, kind: 'cake' },
					{ collection_id: 'c2', amount_minor: 12000, kind: 'stray' }
				]
			})
		);
		expect(v.split).toEqual([
			{ label: 'Cakes · back to your payment method', amount: '£45' },
			{
				label: 'Delivery payment that couldn’t be used · back to your payment method',
				amount: '£120'
			}
		]);
		expect(v.unmatched).toBeUndefined();
		expect(v.roundingExample).toBe(false);
	});

	it('a delivery returned automatically and the chef’s share keep their own lines', () => {
		const delivery = previewView(
			preview({
				allocations: [
					{ collection_id: 'c1', amount_minor: 9000, kind: 'cake' },
					{ collection_id: 'c2', amount_minor: 12000, kind: 'delivery' }
				]
			})
		);
		expect(delivery.split.map((l) => l.label)).toEqual([
			'Cakes · back to your payment method',
			'Delivery · back to your payment method'
		]);
		const chef = previewView(
			preview({
				rate: 50,
				amount_minor: 10500,
				delivery_pending_minor: 6000,
				allocations: [{ collection_id: 'c1', amount_minor: 4500, kind: 'cake' }]
			})
		);
		expect(chef.split).toEqual([
			{ label: 'Cakes · back to your payment method', amount: '£45' },
			{ label: 'Delivery · sent separately by the chef', amount: '£60' }
		]);
	});

	it('an unknown kind: the unlabelled lines (and the stray sentence) as before', () => {
		const v = previewView(
			preview({
				unmatched_minor: 12000,
				delivery_pending_minor: 6000,
				allocations: [{ collection_id: 'c1', amount_minor: 4500, kind: 'other' }]
			})
		);
		expect(v.split[0].label).toBe('Back to your payment method');
		expect(v.unmatched).toContain('£120');
	});
});

describe('refund parts (0038): each kind with its own amount, no arithmetic', () => {
	const part = (kind: string, over: Partial<RefundPart> = {}): RefundPart => ({
		kind,
		target_minor: 4500,
		succeeded_minor: 0,
		in_progress_minor: 4500,
		pending_chef_minor: 0,
		...over
	});

	it('cake and the chef’s delivery: both amounts from the parts', () => {
		const v = refundProgress(
			refund({
				target_minor: 10500,
				in_progress_minor: 4500,
				delivery_pending_minor: 6000,
				parts: [
					part('cake'),
					part('delivery', { target_minor: 6000, in_progress_minor: 0, pending_chef_minor: 6000 })
				]
			})
		);
		expect(v.lines).toEqual([
			{
				label: 'Cakes · to your payment method',
				status: 'Sent · waiting for our payment provider',
				amount: '£45'
			},
			{
				label: 'Delivery · sent separately by the chef',
				status: 'Waiting for the chef',
				amount: '£60'
			}
		]);
		expect(v.trackLabel).toBe('Cake refund progress');
	});

	it('the chef has sent the delivery part: the line stays, with its status', () => {
		const v = refundProgress(
			refund({
				state: 'partly_returned',
				target_minor: 10500,
				succeeded_minor: 4500,
				in_progress_minor: 6000,
				delivery_pending_minor: 0,
				parts: [
					part('cake', { succeeded_minor: 4500, in_progress_minor: 0 }),
					part('delivery', { target_minor: 6000, in_progress_minor: 6000 })
				]
			})
		);
		expect(v.lines).toEqual([
			{ label: 'Cakes · to your payment method', status: 'Returned', amount: '£45' },
			{
				label: 'Delivery · to your payment method',
				status: 'Sent · waiting for our payment provider',
				amount: '£60'
			}
		]);
	});

	it('a single cake part: no split lines; a needs-a-check refund says so per part', () => {
		expect(refundProgress(refund({ parts: [part('cake', { target_minor: 9000 })] })).lines).toEqual(
			[]
		);
		const v = refundProgress(
			refund({
				state: 'needs_attention',
				parts: [part('cake'), part('stray', { target_minor: 12000, in_progress_minor: 0 })]
			})
		);
		expect(v.lines.map((l) => l.status)).toEqual([
			'Needs a check by our team',
			'Needs a check by our team'
		]);
		expect(v.lines[1].label).toBe('Delivery payment that couldn’t be used · returned in full');
	});

	it('parts with all-zero kinds are left out; malformed parts are ignored', () => {
		expect(
			refundParts(
				refund({
					parts: [part('cake'), part('delivery', { target_minor: 0, in_progress_minor: 0 })]
				})
			)
		).toHaveLength(1);
		expect(refundParts(refund({ parts: [part('cake', { target_minor: -1 })] }))).toBeUndefined();
		expect(refundParts(refund({ parts: [] }))).toBeUndefined();
		expect(refundParts(refund())).toBeUndefined();
	});

	it('summary words: state 11’s refund state and an agreed refund (Q20)', () => {
		expect(refundProgress(refund({ state: 'needs_attention' })).stateWord).toBe('needs a check');
		expect(refundProgress(refund({ state: 'partly_returned' })).stateWord).toBe('partly returned');
		expect(refundProgress(refund({ reason: 'chef_exception' })).agreed).toBe(true);
		expect(refundProgress(refund({ reason: 'no_show' })).agreed).toBe(false);
	});
});
