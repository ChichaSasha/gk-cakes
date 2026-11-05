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
		expect(r('returned', 8100)).toBe('Refund of £81 returned to your payment method.');
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
