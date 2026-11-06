import { describe, expect, it } from 'vitest';
import { ApiError } from '../api';
import { orderProblemFor } from './messages';
import {
	isPaymentPageUrl,
	newOrderQuery,
	nextPollDelay,
	orderView,
	POLL_LIMIT_MS,
	refundLine,
	withOrderProblem
} from './order';
import type { Order, OrderItem, Payment } from './types';

const stripe = 'https://checkout.stripe.com/c/pay/cs_test_123';

function order(over: Partial<Order> = {}): Order {
	return {
		id: '0b6c1b8e-6a0e-4c47-9a4e-2f1a7a3e5d10',
		version: 1,
		lifecycle: 'awaiting_payment',
		cancel_reason: null,
		cancelled_at: null,
		payment_requirement: 'full',
		placed_at: '2026-10-10T09:00:00Z',
		date: '2026-10-22',
		fulfillment_mode: 'collection',
		slot: { id: 's', starts_at: '2026-10-22T10:00:00Z', ends_at: '2026-10-22T11:00:00Z' },
		full_refund_deadline_at: '2026-10-17T10:00:00Z',
		customer_full_name: 'Ada Lovelace',
		different_payer_name: null,
		contact_email: 'ada@example.com',
		subtotal_minor: 9000,
		discount_minor: 900,
		total_minor: 8100,
		currency: 'GBP',
		paid_at: null,
		free_confirmed_at: null,
		free_confirmation: null,
		payment: null,
		items: [],
		...over
	};
}

function payment(over: Partial<Payment> = {}): Payment {
	return {
		id: 'p',
		state: 'pending',
		amount_minor: 8100,
		currency: 'GBP',
		requested_at: '2026-10-10T09:00:00Z',
		checkout_url: stripe,
		expires_at: '2026-10-10T11:00:00Z',
		...over
	};
}

describe('isPaymentPageUrl', () => {
	it('accepts only Stripe’s hosted Checkout over https', () => {
		expect(isPaymentPageUrl(stripe)).toBe(true);
		for (const u of [
			null,
			'',
			'http://checkout.stripe.com/x',
			'https://checkout.stripe.com.evil.example/x',
			'https://evil.example/?checkout.stripe.com',
			'https://checkout.stripe.com:8443/x',
			'javascript:alert(1)',
			'not a url'
		]) {
			expect(isPaymentPageUrl(u), String(u)).toBe(false);
		}
	});
});

describe('orderView: awaiting payment', () => {
	it('offers to start payment when none was started', () => {
		const v = orderView(order());
		expect(v.action).toBe('start_payment');
		expect(v.actionLabel).toBe('Continue to secure payment');
		expect(v.poll).toBe(false);
	});

	it('offers the open payment page and keeps checking', () => {
		const v = orderView(order({ payment: payment() }));
		expect(v.action).toBe('open_payment');
		expect(v.checkoutUrl).toBe(stripe);
		expect(v.poll).toBe(true);
	});

	it('never links to a payment page that is not Stripe’s', () => {
		const v = orderView(order({ payment: payment({ checkout_url: 'https://evil.example/pay' }) }));
		expect(v.checkoutUrl).toBeUndefined();
		expect(v.action).toBe('none');
	});

	it('after the return, says it is still checking and not to pay again; the return proves nothing', () => {
		for (const p of [
			payment({ checkout_url: null }),
			payment({ state: 'unknown', checkout_url: null }),
			payment({ state: 'created', checkout_url: null })
		]) {
			const v = orderView(order({ payment: p }), { returned: true });
			expect(v.title).toBe('Still checking payment; do not pay again');
			expect(v.doNotPayAgain).toBe(true);
			expect(v.poll).toBe(true);
			expect(v.action).toBe('none');
			expect(v.confirmed).toBe(false);
		}
	});

	it('after the return with the session still open, offers the same page as a secondary way back', () => {
		const v = orderView(order({ payment: payment() }), { returned: true });
		expect(v.title).toBe('Still checking payment; do not pay again');
		expect(v.action).toBe('open_payment');
		expect(v.actionLabel).toBe('Back to the secure payment page');
	});

	it('waits while the payment page is being prepared', () => {
		const v = orderView(order({ payment: payment({ state: 'created', checkout_url: null }) }));
		expect(v.title).toBe('Opening secure payment…');
		expect(v.poll).toBe(true);
	});

	it('lets the customer try again after a verified failed or cancelled payment', () => {
		for (const state of ['failed', 'cancelled'] as const) {
			const v = orderView(order({ payment: payment({ state, checkout_url: null }) }));
			expect(v.action).toBe('start_payment');
			expect(v.actionLabel).toBe('Try paying again');
		}
	});

	it('treats a succeeded payment as received while the order is confirmed', () => {
		const v = orderView(order({ payment: payment({ state: 'succeeded', checkout_url: null }) }));
		expect(v.title).toBe('Payment received — confirming your order');
		expect(v.poll).toBe(true);
		expect(v.confirmed).toBe(false);
	});
});

describe('orderView: free orders', () => {
	const free = { payment_requirement: 'none' as const, total_minor: 0 };

	it('asks to confirm, waits while processing and allows a retry after a refusal', () => {
		expect(orderView(order(free)).action).toBe('confirm_free');
		const processing = orderView(
			order({
				...free,
				free_confirmation: { state: 'processing', reason: null, requested_at: 'x' }
			})
		);
		expect(processing.poll).toBe(true);
		expect(processing.action).toBe('none');
		const refused = orderView(
			order({
				...free,
				free_confirmation: { state: 'refused', reason: 'order_changed', requested_at: 'x' }
			})
		);
		expect(refused.action).toBe('confirm_free');
		expect(refused.tone).toBe('warning');
	});

	it('words a refused free confirmation by its reason; a closed window needs a new order', () => {
		const refused = (reason: string) =>
			orderView(
				order({ ...free, free_confirmation: { state: 'refused', reason, requested_at: 'x' } })
			);
		const closed = refused('window_closed');
		expect(closed.action).toBe('none');
		expect(closed.lines[0]).toMatch(/time to confirm this order has passed.*new order/);
		const released = refused('holds_released');
		expect(released.action).toBe('none');
		expect(released.lines[0]).toMatch(/place on this date was released.*new order/);
		expect(refused('order_changed').actionLabel).toBe('Try again');
	});

	it('confirms a free order without mentioning payment', () => {
		const v = orderView(order({ ...free, lifecycle: 'confirmed' }));
		expect(v.lines.join(' ')).toMatch(/free order is confirmed/);
		expect(v.lines.join(' ')).not.toMatch(/payment/i);
	});
});

describe('orderView: after payment', () => {
	it('confirmation_pending: payment received, confirming, keep polling', () => {
		const v = orderView(order({ lifecycle: 'confirmation_pending' }));
		expect(v.title).toBe('Payment received — confirming your order');
		expect(v.poll).toBe(true);
		expect(v.doNotPayAgain).toBe(true);
	});

	it('confirmed: shows the refund deadline from the order in London time', () => {
		const v = orderView(order({ lifecycle: 'confirmed', paid_at: '2026-10-10T09:05:00Z' }));
		expect(v.confirmed).toBe(true);
		expect(v.tone).toBe('success');
		expect(v.poll).toBe(false);
		// The deadline appears once, in the details (refundTerms), not in the status lines.
		expect(v.lines.join(' ')).not.toMatch(/Cancel before/);
		expect(v.refundTerms).toBe('standard');
		expect(v.promise).toBe(true);
	});

	it('shows refund terms only for accepted, paid orders', () => {
		expect(orderView(order()).refundTerms).toBe('none');
		expect(orderView(order({ lifecycle: 'confirmation_pending' })).refundTerms).toBe('none');
		expect(
			orderView(order({ lifecycle: 'confirmed', payment_requirement: 'none', total_minor: 0 }))
				.refundTerms
		).toBe('none');
		expect(orderView(order({ lifecycle: 'completed' })).refundTerms).toBe('none');
		expect(
			orderView(order({ lifecycle: 'cancelled', cancel_reason: 'payment_not_completed' }))
				.refundTerms
		).toBe('none');
		expect(orderView(order({ lifecycle: 'in_production' })).refundTerms).toBe('standard');
	});

	it('paid_awaiting_agreement: the chef will contact you, with no promise about the date', () => {
		const v = orderView(order({ lifecycle: 'paid_awaiting_agreement' }));
		expect(v.title).toBe('Payment received');
		expect(v.lines.join(' ')).toMatch(/chef will contact you about the date/);
		expect(v.confirmed).toBe(false);
		expect(v.lines.join(' ')).not.toMatch(/confirmed/i);
		// The date is requested, not promised; everything back if it can't be agreed.
		expect(v.promise).toBe(false);
		expect(v.refundTerms).toBe('agreement');
		expect(v.doNotPayAgain).toBe(true);
	});

	it('cancelled: says why in plain words and implies no refund', () => {
		const v = orderView(
			order({ lifecycle: 'cancelled', cancel_reason: 'payment_not_completed', cancelled_at: 'x' })
		);
		expect(v.tone).toBe('error');
		expect(v.lines[0]).toMatch(/payment wasn’t completed/);
		expect(v.lines.join(' ')).not.toMatch(/refund/i);
		expect(
			orderView(order({ lifecycle: 'cancelled', cancel_reason: 'free_not_confirmed' })).lines[0]
		).toMatch(/free order wasn’t confirmed/);
	});

	it('cancellation pending: polls, with the confirmed preview amount, never a refund state yet', () => {
		const v = orderView(order({ lifecycle: 'cancellation_pending', paid_at: 'x' }), {
			cancelledMinor: 4500
		});
		expect(v.title).toBe('Cancelling your order');
		expect(v.poll).toBe(true);
		expect(v.action).toBe('none');
		expect(v.lines).toEqual([
			'We’re cancelling your order. This usually takes a few seconds.',
			'Refund of £45 requested. We’ll update this page as it progresses.'
		]);
		expect(orderView(order({ lifecycle: 'cancellation_pending' })).lines).toHaveLength(1);
	});

	it('cancelled by the customer: why this amount, then the built refund line (state 13)', () => {
		const v = orderView(
			order({
				lifecycle: 'cancelled',
				cancel_reason: 'customer_cancelled',
				cancelled_at: 'x',
				paid_at: 'x',
				payment: payment({ state: 'succeeded', checkout_url: null }),
				cancellation: {
					state: 'cancelled',
					reason: 'customer_cancelled',
					rate: 50,
					amount_minor: 4500,
					requested_by: 'customer',
					requested_at: 'x'
				},
				refund: { state: 'requested', target_minor: 4500, succeeded_minor: 0 }
			})
		);
		expect(v.title).toBe('Your order is cancelled');
		expect(v.tone).toBe('info');
		expect(v.lines).toEqual([
			'You cancelled after the full-refund deadline, so you get 50% of what you paid, rounded in your favour.',
			'Refund of £45 requested. We’ll update this page as it progresses.'
		]);
		expect(v.refund).toBeUndefined();
		expect(v.lines.join(' ')).not.toMatch(/new order|returned to/);
	});

	it('state 13 tones and the partly returned delivery wording', () => {
		const base = {
			lifecycle: 'cancelled' as const,
			cancel_reason: 'customer_cancelled',
			payment: payment({ state: 'succeeded', checkout_url: null })
		};
		expect(
			orderView(
				order({ ...base, refund: { state: 'returned', target_minor: 9000, succeeded_minor: 9000 } })
			).tone
		).toBe('success');
		expect(
			orderView(
				order({
					...base,
					refund: { state: 'needs_attention', target_minor: 9000, succeeded_minor: 0 }
				})
			).tone
		).toBe('warning');
		expect(
			orderView(
				order({
					...base,
					fulfillment_mode: 'gk_delivery',
					refund: {
						state: 'partly_returned',
						target_minor: 10500,
						succeeded_minor: 4500,
						in_progress_minor: 0,
						delivery_pending_minor: 6000
					}
				})
			).lines
		).toEqual([
			'£45 of £105 returned so far; the rest is still pending.',
			'The chef sends the delivery part separately.'
		]);
		const chef = orderView(
			order({
				...base,
				cancel_reason: 'chef_cancelled',
				refund: { state: 'requested', target_minor: 9000, succeeded_minor: 0 }
			})
		);
		expect(chef.title).toBe('We had to cancel your order');
		expect(chef.lines[0]).toMatch(/We’re sorry.*100%/);
	});

	it('cancelled with a late recorded payment: do not pay again, we will contact you', () => {
		const v = orderView(
			order({
				lifecycle: 'cancelled',
				cancel_reason: 'payment_not_completed',
				payment: payment({ state: 'succeeded', checkout_url: null })
			})
		);
		expect(v.lines.join(' ')).toMatch(/payment recorded.*don’t pay again/);
	});

	it('cancelled with a payment still being checked: do not pay again, never "not completed"', () => {
		for (const state of ['unknown', 'pending', 'created'] as const) {
			const v = orderView(
				order({
					lifecycle: 'cancelled',
					cancel_reason: 'payment_not_completed',
					payment: payment({ state, checkout_url: null })
				})
			);
			expect(v.title).toBe('Still checking payment; do not pay again');
			expect(v.doNotPayAgain).toBe(true);
			expect(v.action).toBe('none');
			expect(v.poll).toBe(true);
			const text = `${v.title} ${v.lines.join(' ')}`;
			expect(text).toMatch(/don’t pay again/);
			expect(text).not.toMatch(/wasn’t completed/);
			expect(text).not.toMatch(/new order/i);
			// A dead end handed over never replaces it either.
			expect(withOrderProblem(v, 'new_order')).toEqual(v);
		}
	});
});

describe('withOrderProblem: a dead end (lead time or price window passed)', () => {
	const handed = (code: string) => orderProblemFor(new ApiError(409, { code })).next;

	it('offers a new order instead of paying for LEAD_TIME and QUOTE_EXPIRED', () => {
		for (const code of ['LEAD_TIME', 'QUOTE_EXPIRED']) {
			for (const o of [
				order(),
				order({ payment: payment({ state: 'cancelled', checkout_url: null }) }),
				order({ payment: payment({ state: 'failed', checkout_url: null }) })
			]) {
				const v = withOrderProblem(orderView(o), handed(code));
				expect(v.action).toBe('new_order');
				expect(v.actionLabel).toBe('Start a new order');
				expect(v.actionLabel).not.toMatch(/pay/i);
				expect(v.checkoutUrl).toBeUndefined();
			}
		}
	});

	it('offers a new order instead of confirming a free order', () => {
		const free = order({ payment_requirement: 'none', total_minor: 0 });
		expect(orderView(free).action).toBe('confirm_free');
		expect(withOrderProblem(orderView(free), handed('QUOTE_EXPIRED')).action).toBe('new_order');
	});

	it('keeps the order’s own view without a dead-end problem', () => {
		const v = orderView(order());
		expect(withOrderProblem(v, undefined)).toEqual(v);
		expect(withOrderProblem(v, handed('PAYMENT_IN_PROGRESS'))).toEqual(v);
		expect(withOrderProblem(v, handed('SERVICE_UNAVAILABLE'))).toEqual(v);
		expect(v.actionLabel).toBe('Continue to secure payment');
	});

	it('never replaces a payment under way or a decided order', () => {
		// An open payment page that was never used is a payment under way (K03).
		const open = orderView(order({ payment: payment() }));
		expect(open.action).toBe('open_payment');
		expect(open.actionLabel).toBe('Continue to secure payment');
		expect(withOrderProblem(open, handed('LEAD_TIME'))).toEqual(open);
		for (const o of [
			order({ payment: payment({ state: 'pending' }) }),
			order({ payment: payment({ state: 'unknown', checkout_url: null }) }),
			order({ payment: payment({ state: 'succeeded', checkout_url: null }) }),
			order({ lifecycle: 'confirmation_pending' }),
			order({ lifecycle: 'confirmed' }),
			order({ lifecycle: 'cancelled', cancel_reason: 'payment_not_completed' })
		]) {
			const v = orderView(o, { returned: o.payment?.state === 'pending' });
			expect(withOrderProblem(v, handed('LEAD_TIME'))).toEqual(v);
		}
	});
});

describe('newOrderQuery', () => {
	const item = (over: Partial<OrderItem>): OrderItem => ({
		line_no: 1,
		variant_id: '11111111-1111-4111-8111-111111111111',
		lot_id: null,
		cake_name: 'Honey',
		flavour: 'Honey',
		size_code: 'S',
		quantity: 1,
		unit_price_minor: 4500,
		discount_minor: 0,
		net_paid_minor: 4500,
		...over
	});

	it('repeats each cake once per piece, ready cakes by lot, without the date', () => {
		const q = newOrderQuery(
			order({
				items: [
					item({ quantity: 2 }),
					item({ line_no: 2, lot_id: '22222222-2222-4222-8222-222222222222' })
				]
			})
		);
		const p = new URLSearchParams(q);
		expect(p.getAll('v')).toEqual([
			'11111111-1111-4111-8111-111111111111',
			'11111111-1111-4111-8111-111111111111'
		]);
		expect(p.getAll('lot')).toEqual(['22222222-2222-4222-8222-222222222222']);
		expect(p.has('date')).toBe(false);
	});

	it('is undefined without cakes', () => {
		expect(newOrderQuery(order({ items: [] }))).toBeUndefined();
		expect(newOrderQuery(order({ items: null }))).toBeUndefined();
	});
});

describe('refundLine', () => {
	it('never claims money was returned unless the API says so', () => {
		const r = (state: string, succeeded = 0) =>
			refundLine(
				order({
					refund: { state, target_minor: 8100, succeeded_minor: succeeded, currency: 'GBP' }
				})
			);
		expect(refundLine(order())).toBeUndefined();
		expect(r('requested')).toMatch(/^Refund of £81 requested/);
		expect(r('pending')).toMatch(/^Refund of £81 pending/);
		expect(r('pending')).not.toMatch(/returned to/);
		expect(r('partly_returned', 4050)).toBe(
			'£40.50 of £81 returned so far; the rest is still pending.'
		);
		expect(r('returned', 8100)).toBe(
			'Refund of £81 returned to your payment method. Your bank may take a few days to show it.'
		);
		expect(r('needs_attention')).toMatch(/needs a check by our team/);
		expect(r('needs_attention')).not.toMatch(/returned/);
		expect(r('something_new')).not.toMatch(/returned/);
	});
});

describe('polling', () => {
	it('backs off from the API hint up to 30 seconds and stops after 5 minutes', () => {
		expect(nextPollDelay(0)).toBe(3000);
		expect(nextPollDelay(1)).toBe(4500);
		expect(nextPollDelay(20)).toBe(30_000);
		expect(nextPollDelay(0, 0)).toBe(1000);
		expect(POLL_LIMIT_MS).toBe(300_000);
	});
});

// ---------------------------------------------------------------- slice 3 (GK-017 r02b states 11 and 19)

describe('paid after the order was cancelled (state 11, Q7)', () => {
	const late = (state: string, over: Partial<Order> = {}) =>
		orderView(
			order({
				lifecycle: 'cancelled',
				cancel_reason: 'payment_not_completed',
				cancelled_at: '2026-10-10T09:20:00Z',
				total_minor: 9000,
				discount_minor: 0,
				payment: payment({ state: 'succeeded', amount_minor: 9000, checkout_url: null }),
				refund: {
					state,
					target_minor: 9000,
					succeeded_minor: state === 'returned' ? 9000 : 0,
					reason: 'late_capture_terminal'
				},
				...over
			})
		);

	it('requested: kept safe and all of it being returned; a new order offered', () => {
		const v = late('requested');
		expect(v.title).toBe('Payment received after this order was cancelled');
		expect(v.tone).toBe('warning');
		expect(v.lines).toEqual([
			'This order had already been cancelled because the payment wasn’t completed in time, and then your payment of £90 arrived. We’ve kept it safe and are returning all of it to you.',
			'Refund of £90 requested. We’ll update this page as it progresses.',
			'Please don’t pay again. Your cake won’t be made for this order.'
		]);
		expect(v.refund).toBeUndefined();
		expect(v.action).toBe('new_order');
		expect(v.actionLabel).toBe('Start a new order');
		expect(v.paidAfterCancel).toBe(true);
	});

	it.each([
		[
			'pending',
			'Refund of £90 pending: we’ve asked our payment provider to return it. We’ll update this page when it has been returned.'
		],
		[
			'needs_attention',
			'Your refund of £90 needs a check by our team before we can send it. We’ll contact you if we need anything.'
		]
	])('%s: the API’s refund state, never returned', (state, line) => {
		const v = late(state);
		expect(v.lines[1]).toBe(line);
		expect(v.lines.join(' ')).not.toMatch(/returned all/);
	});

	it('returned: past tense only once the API says so', () => {
		const v = late('returned');
		expect(v.tone).toBe('success');
		expect(v.lines[0]).toMatch(/We kept it safe and have returned all of it to you\.$/);
		expect(v.lines[1]).toBe(
			'Refund of £90 returned to your payment method. Your bank may take a few days to show it.'
		);
	});

	it('no refund yet (K11 hides it until secured): as before', () => {
		const v = late('requested', { refund: null });
		expect(v.title).toBe('This order was cancelled');
		expect(v.paidAfterCancel).toBeUndefined();
	});
});

describe('being made, collected and not collected (state 19)', () => {
	const accepted = (over: Partial<Order> = {}) =>
		order({ lifecycle: 'confirmed', paid_at: '2026-10-06T13:05:00Z', ...over });

	it('being made: the cake is now being prepared', () => {
		expect(orderView(accepted({ lifecycle: 'in_production' })).lines).toEqual([
			'We’ve received your payment and your order is confirmed. Your cake is now being prepared.'
		]);
		expect(orderView(accepted()).lines).toEqual([
			'We’ve received your payment and your order is confirmed.'
		]);
		expect(
			orderView(
				accepted({
					lifecycle: 'in_production',
					fulfillment_mode: 'gk_delivery',
					delivery: { state: 'paid', quote: null, paid_minor: 12000 }
				})
			).lines
		).toEqual([
			'Your cakes and delivery are paid. The chef has confirmed your delivery and started making your cake.'
		]);
		expect(
			orderView(accepted({ lifecycle: 'in_production', payment_requirement: 'none' })).lines
		).toEqual(['Your free order is confirmed. Your cake is now being prepared.']);
	});

	it('collected: ask the chef about anything wrong (Q20)', () => {
		const v = orderView(
			accepted({
				lifecycle: 'completed',
				fulfillment: { state: 'completed', at: 'x', pending: null }
			})
		);
		expect(v.title).toBe('Your order is complete');
		expect(v.lines).toEqual([
			'Thank you for ordering with GK Cakes.',
			'Something wrong with your cake? Please contact the chef: they’ll look at it with you and agree any refund.'
		]);
		expect(v.contactChef).toBe(true);
		expect(v.contactLabel).toBe('Contact the chef about your cake');
		expect(v.refundTerms).toBe('none');
	});

	it('a refund agreed with the chef after collection (Q20)', () => {
		const agreed = (state: string) =>
			orderView(
				accepted({
					lifecycle: 'completed',
					fulfillment: { state: 'completed', at: 'x', pending: null },
					refund: {
						state,
						target_minor: 3000,
						succeeded_minor: state === 'returned' ? 3000 : 0,
						reason: 'chef_exception'
					}
				})
			);
		const v = agreed('requested');
		expect(v.lines).toEqual([
			'Thank you for ordering with GK Cakes.',
			'Refund of £30 requested: the amount you agreed with the chef after collection. We’ll update this page as it progresses.'
		]);
		expect(v.refund).toBeUndefined();
		expect(v.contactChef).toBeUndefined();
		const back = agreed('returned');
		expect(back.lines[1]).toBe(
			'Refund of £30 requested: the amount you agreed with the chef after collection.'
		);
		expect(back.refund).toBe(
			'Refund of £30 returned to your payment method. Your bank may take a few days to show it.'
		); // A delivered GK order says "after delivery" (review nit 6).
		const delivered = orderView(
			accepted({
				lifecycle: 'completed',
				fulfillment_mode: 'gk_delivery',
				fulfillment: { state: 'completed', at: 'x', pending: null },
				refund: {
					state: 'requested',
					target_minor: 3000,
					succeeded_minor: 0,
					reason: 'chef_exception'
				}
			})
		);
		expect(delivered.lines[1]).toBe(
			'Refund of £30 requested: the amount you agreed with the chef after delivery. We’ll update this page as it progresses.'
		);
	});

	it('not collected: 50% requested automatically when the chef recorded it (Q19)', () => {
		const v = orderView(
			accepted({
				lifecycle: 'completed',
				fulfillment: { state: 'no_show', at: 'x', pending: null },
				refund: {
					state: 'requested',
					target_minor: 16500,
					succeeded_minor: 0,
					reason: 'no_show',
					// A stray refund beside it: the cake part is the 50%, not the block.
					parts: [
						{
							kind: 'cake',
							target_minor: 4500,
							succeeded_minor: 0,
							in_progress_minor: 4500,
							pending_chef_minor: 0
						},
						{
							kind: 'stray',
							target_minor: 12000,
							succeeded_minor: 0,
							in_progress_minor: 12000,
							pending_chef_minor: 0
						}
					]
				}
			})
		);
		expect(v.title).toBe('Your cake wasn’t collected');
		expect(v.tone).toBe('warning');
		expect(v.lines).toEqual([
			'Your cake was ready at St Katharine Docks on Thu 22 October, 11:00–12:00, but it wasn’t collected.',
			'Under the refund terms you get 50% of what you paid back: £45. We requested it automatically when the chef recorded that the cake wasn’t collected.'
		]);
		expect(v.refund).toBe('Refund of £165 requested. We’ll update this page as it progresses.');
		expect(v.contactChef).toBe(true);
	});

	it('not collected, nothing refunded (a free order): no amount', () => {
		const v = orderView(
			accepted({
				lifecycle: 'completed',
				payment_requirement: 'none',
				fulfillment: { state: 'no_show', at: 'x', pending: null }
			})
		);
		expect(v.lines).toHaveLength(1);
	});
});
