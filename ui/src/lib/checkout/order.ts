// The order's state in customer words (GK-025, GK-017 r01). Only the API's
// order snapshot decides what is shown: the payment return URL
// (?payment=returned) never claims success by itself, a timer is never
// proof that money failed, and a refund is "returned" only when the API
// says so.
import { deadlineLabel } from './format';
import type { Order } from './types';

export type OrderAction =
	| 'none'
	| 'start_payment' // ask the API for a secure payment page
	| 'open_payment' // go to the open payment page (checkoutUrl)
	| 'confirm_free'; // confirm a £0 order

export interface OrderView {
	tone: 'info' | 'success' | 'warning' | 'error';
	title: string;
	lines: string[];
	action: OrderAction;
	actionLabel?: string;
	/** Only a verified Stripe Checkout URL (isPaymentPageUrl). */
	checkoutUrl?: string;
	/** Still changing: read the order again soon. */
	poll: boolean;
	/** Show "do not pay again" prominently. */
	doNotPayAgain: boolean;
	/** The order is accepted: show the confirmation details. */
	confirmed: boolean;
	/** A refund line, exactly as far as the API reports it. */
	refund?: string;
}

/** Whether url is Stripe's hosted Checkout page (the only place we send customers to pay). */
export function isPaymentPageUrl(url: string | null | undefined): url is string {
	if (!url) return false;
	try {
		const u = new URL(url);
		return u.protocol === 'https:' && u.hostname === 'checkout.stripe.com' && u.port === '';
	} catch {
		return false;
	}
}

const DO_NOT_PAY = 'Please don’t pay again. We’ll update this page when we know the result.';

/** The view of an order; returned is true after coming back from the payment page. */
export function orderView(order: Order, opts: { returned?: boolean } = {}): OrderView {
	const refund = refundLine(order);
	const base = {
		refund,
		doNotPayAgain: false,
		confirmed: false,
		poll: false,
		action: 'none' as const
	};
	const deadline = `Cancel before ${deadlineLabel(order.full_refund_deadline_at)} for a full refund of what you paid.`;

	switch (order.lifecycle) {
		case 'confirmed':
		case 'in_production':
		case 'ready':
		case 'completed':
			return {
				...base,
				tone: 'success',
				title: {
					confirmed: 'Your order is confirmed',
					in_production: 'Your cake is being made',
					ready: 'Your cake is ready',
					completed: 'Your order is complete'
				}[order.lifecycle],
				lines:
					order.lifecycle === 'completed'
						? ['Thank you for ordering with GK Cakes.']
						: [
								order.payment_requirement === 'none'
									? 'Your free order is confirmed. We’ll email you.'
									: 'We’ve received your payment. We’ll email you.',
								deadline
							],
				confirmed: true
			};
		case 'paid_awaiting_agreement':
			return {
				...base,
				tone: 'warning',
				title: 'Payment received',
				lines: [
					'Your payment arrived after the time we need to prepare your cake for this date. The chef will contact you about the date.',
					'Please don’t pay again.'
				],
				doNotPayAgain: true
			};
		case 'confirmation_pending':
			return {
				...base,
				tone: 'info',
				title:
					order.payment_requirement === 'none'
						? 'Confirming your order'
						: 'Payment received — confirming your order',
				lines: [
					order.payment_requirement === 'none'
						? 'We’re confirming your free order. This usually takes a few seconds.'
						: 'We’ve received your payment and are confirming your order. This usually takes a few seconds.'
				],
				poll: true,
				doNotPayAgain: order.payment_requirement !== 'none'
			};
		case 'cancelled':
			return cancelled(order, base);
		case 'awaiting_payment':
			return order.payment_requirement === 'none'
				? awaitingFree(order, base)
				: awaitingPayment(order, base, opts.returned === true);
	}
	return {
		...base,
		tone: 'info',
		title: 'Your order',
		lines: ['We can’t show this order’s status right now. Please refresh later.']
	};
}

type Base = Pick<OrderView, 'refund' | 'doNotPayAgain' | 'confirmed' | 'poll' | 'action'>;

function cancelled(order: Order, base: Base): OrderView {
	const lines: string[] = [];
	if (order.cancel_reason === 'payment_not_completed') {
		lines.push(
			'The payment wasn’t completed within the time your place was held, so this order was cancelled and your place released.'
		);
	} else if (order.cancel_reason === 'free_not_confirmed') {
		lines.push('This free order wasn’t confirmed in time, so it was cancelled.');
	} else {
		lines.push('This order was cancelled.');
	}
	if (order.payment?.state === 'succeeded' && !base.refund) {
		lines.push(
			'We have a payment recorded for this order. Please don’t pay again; we’ll contact you about it.'
		);
	}
	lines.push('To order again, please start a new order.');
	return { ...base, tone: 'error', title: 'This order was cancelled', lines };
}

function awaitingFree(order: Order, base: Base): OrderView {
	const fc = order.free_confirmation;
	if (fc?.state === 'processing' || fc?.state === 'confirmed') {
		return {
			...base,
			tone: 'info',
			title: 'Confirming your order',
			lines: ['We’re confirming your free order. This usually takes a few seconds.'],
			poll: true
		};
	}
	return {
		...base,
		tone: fc?.state === 'refused' ? 'warning' : 'info',
		title: fc?.state === 'refused' ? 'We couldn’t confirm your order yet' : 'Confirm your order',
		lines: [
			fc?.state === 'refused'
				? 'Something changed while we were confirming it. You can try again; if it still can’t be confirmed, please start a new order.'
				: 'Your order costs nothing to pay. Confirm it to book your date.'
		],
		action: 'confirm_free',
		actionLabel: 'Confirm order'
	};
}

function awaitingPayment(order: Order, base: Base, returned: boolean): OrderView {
	const p = order.payment;
	const url = isPaymentPageUrl(p?.checkout_url) ? (p?.checkout_url ?? undefined) : undefined;
	if (!p || p.state === 'failed' || p.state === 'cancelled') {
		return {
			...base,
			tone: p ? 'warning' : 'info',
			title: p ? 'That payment didn’t go through' : 'Your order is waiting for payment',
			lines: [
				p
					? 'No payment was taken for that attempt. You can try again while your place is still held.'
					: 'Your place on this date is held for 15 minutes while you pay.'
			],
			action: 'start_payment',
			actionLabel: p ? 'Try paying again' : 'Continue to secure payment'
		};
	}
	if (p.state === 'succeeded') {
		return {
			...base,
			tone: 'info',
			title: 'Payment received — confirming your order',
			lines: ['We’ve received your payment and are confirming your order.'],
			poll: true,
			doNotPayAgain: true
		};
	}
	// created, pending or unknown: the result is not known yet.
	if (url && !returned) {
		return {
			...base,
			tone: 'info',
			title: 'Your order is waiting for payment',
			lines: [
				'Your secure payment page is ready. Your place is held while you pay; if your payment is still processing after that, we’ll wait for the result.'
			],
			action: 'open_payment',
			actionLabel: 'Continue to secure payment',
			checkoutUrl: url,
			poll: true
		};
	}
	if (p.state === 'created' && !returned) {
		return {
			...base,
			tone: 'info',
			title: 'Opening secure payment…',
			lines: ['We’re preparing your secure payment page. This usually takes a few seconds.'],
			poll: true,
			doNotPayAgain: true
		};
	}
	return {
		...base,
		tone: 'info',
		title: 'Still checking payment; do not pay again',
		lines: [
			DO_NOT_PAY,
			...(url
				? [
						'If you left the payment page before paying, you can go back to it. It’s the same payment, so you won’t be charged twice.'
					]
				: [])
		],
		action: url ? 'open_payment' : 'none',
		actionLabel: url ? 'Back to the secure payment page' : undefined,
		checkoutUrl: url,
		poll: true,
		doNotPayAgain: true
	};
}

/** The refund line, worded only as far as the API's refund block goes. */
export function refundLine(order: Order): string | undefined {
	const r = order.refund;
	if (!r) return undefined;
	switch (r.state) {
		case 'requested':
			return 'Refund requested. We’ll update this page as it progresses.';
		case 'pending':
		case 'processing':
		case 'dispatched':
			return 'Refund pending. We’ll update this page when it has been returned.';
		case 'succeeded':
		case 'returned':
			return 'Refund returned to your payment method.';
		default:
			return 'Refund requested. We’ll update this page as it progresses.';
	}
}

/** The next wait before reading the order again: back off from the API's hint, capped at 30 s. */
export function nextPollDelay(attempt: number, hintSeconds = 3): number {
	const base = Math.max(1, hintSeconds) * 1000;
	return Math.min(30_000, Math.round(base * 1.5 ** Math.max(0, attempt)));
}

/** Polling stops after this long; the page then offers a refresh button. */
export const POLL_LIMIT_MS = 5 * 60 * 1000;
