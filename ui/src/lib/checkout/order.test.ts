import { describe, expect, it } from 'vitest';
import { isPaymentPageUrl, nextPollDelay, orderView, POLL_LIMIT_MS, refundLine } from './order';
import type { Order, Payment } from './types';

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
		expect(v.lines.join(' ')).toContain('Cancel before 11:00 (London time) on Sat 17 October');
		expect(v.poll).toBe(false);
	});

	it('paid_awaiting_agreement: the chef will contact you, with no promise about the date', () => {
		const v = orderView(order({ lifecycle: 'paid_awaiting_agreement' }));
		expect(v.title).toBe('Payment received');
		expect(v.lines.join(' ')).toMatch(/chef will contact you about the date/);
		expect(v.confirmed).toBe(false);
		expect(v.lines.join(' ')).not.toMatch(/confirmed/i);
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
});

describe('refundLine', () => {
	it('never claims money was returned unless the API says so', () => {
		expect(refundLine(order())).toBeUndefined();
		expect(refundLine(order({ refund: { state: 'requested' } }))).toMatch(/^Refund requested/);
		expect(refundLine(order({ refund: { state: 'pending' } }))).toMatch(/^Refund pending/);
		expect(refundLine(order({ refund: { state: 'succeeded' } }))).toMatch(/^Refund returned/);
		expect(refundLine(order({ refund: { state: 'something_new' } }))).not.toMatch(/returned/);
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
