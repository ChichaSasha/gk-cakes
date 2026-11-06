// The order's state in customer words (GK-025, GK-017 r01). Only the API's
// order snapshot decides what is shown: the payment return URL
// (?payment=returned) never claims success by itself, a timer is never
// proof that money failed, and a refund is "returned" only when the API
// says so.
import { cancelledStory, shortDate } from './changes';
import { formatMoney, timeRange } from './format';
import type { Next } from './messages';
import type { Order } from './types';

export type OrderAction =
	| 'none'
	| 'start_payment' // ask the API for a secure payment page
	| 'open_payment' // go to the open payment page (checkoutUrl)
	| 'confirm_free' // confirm a £0 order
	| 'pay_delivery' // pay the GK delivery quote (GK-030 slice 2, state 15)
	| 'new_order'; // this order can no longer be paid or confirmed: start a new one

/** The answer buttons to a chef's offer after a late payment (state 10). */
export interface AnswerView {
	offerId: string;
	/** Absent once the offer can no longer be accepted (declining has no limit). */
	acceptLabel?: string;
	declineLabel: string;
	/** The amount a decline returns (the API's paid_minor), shown while it is secured. */
	declineMinor?: number;
	note: string;
}

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
	/**
	 * Whether the order's date and time are a promise. False while the date
	 * still has to be agreed (paid_awaiting_agreement): show them as requested.
	 */
	promise: boolean;
	/**
	 * Which refund terms to show: the K05 cancellation terms for an accepted,
	 * paid order; "everything back if the date can't be agreed" while the date
	 * is open; none otherwise (nothing paid, cancelled or handed over).
	 */
	refundTerms: 'none' | 'standard' | 'agreement';
	/** A chef's offer to answer (state 10). */
	answer?: AnswerView;
	/** A "What happens next" list (state 10, waiting). */
	next?: { title: string; items: string[] };
	/** Show a "Contact the chef" link in the status card. */
	contactChef?: boolean;
	/** The contact link's words when not "Contact the chef" (state 19, collected). */
	contactLabel?: string;
	/** Paid after the order was cancelled (state 11): the summary says so. */
	paidAfterCancel?: boolean;
	/** Show the refund tracker (a refund on an order that isn't cancelled). */
	refundTracker?: boolean;
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

/**
 * The view of an order; returned is true after coming back from the payment
 * page; cancelledMinor is the amount of the preview this page just confirmed
 * (shown while the cancellation is being secured).
 */
export function orderView(
	order: Order,
	opts: { returned?: boolean; cancelledMinor?: number } = {}
): OrderView {
	const refund = refundLine(order);
	const base: Base = {
		refund,
		doNotPayAgain: false,
		confirmed: false,
		poll: false,
		action: 'none',
		promise: true,
		refundTerms: 'none'
	};

	switch (order.lifecycle) {
		case 'completed':
			return completed(order, base);
		case 'confirmed':
		case 'in_production':
		case 'ready':
			return {
				...base,
				tone: 'success',
				title: {
					confirmed: 'Your order is confirmed',
					in_production: 'Your cake is being made',
					ready: 'Your cake is ready'
				}[order.lifecycle],
				lines: [acceptedLine(order)],
				confirmed: true,
				refundTerms: order.payment_requirement === 'full' ? 'standard' : 'none'
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
				doNotPayAgain: true,
				promise: false,
				refundTerms: 'agreement'
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
		case 'cancellation_pending':
			return {
				...base,
				tone: 'info',
				title: 'Cancelling your order',
				lines: [
					'We’re cancelling your order. This usually takes a few seconds.',
					...(Number.isSafeInteger(opts.cancelledMinor) && (opts.cancelledMinor as number) > 0
						? [
								`Refund of ${formatMoney(opts.cancelledMinor as number, order.currency)} requested. We’ll update this page as it progresses.`
							]
						: [])
				],
				poll: true
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

type Base = Pick<
	OrderView,
	'refund' | 'doNotPayAgain' | 'confirmed' | 'poll' | 'action' | 'promise' | 'refundTerms'
>;

/** The accepted order's line; being made (state 19) says the cake is being prepared. */
function acceptedLine(order: Order): string {
	const making = order.lifecycle === 'in_production';
	if (order.payment_requirement === 'none') {
		return making
			? 'Your free order is confirmed. Your cake is now being prepared.'
			: 'Your free order is confirmed.';
	}
	if (!making) return 'We’ve received your payment and your order is confirmed.';
	const paid = order.delivery?.paid_minor;
	return order.fulfillment_mode === 'gk_delivery' &&
		Number.isSafeInteger(paid) &&
		(paid as number) > 0
		? 'Your cakes and delivery are paid. The chef has confirmed your delivery and started making your cake.'
		: 'We’ve received your payment and your order is confirmed. Your cake is now being prepared.';
}

/** The cake part of a refund (0038 parts), or the whole refund without parts. */
function cakeRefundMinor(order: Order): number | undefined {
	const r = order.refund;
	if (!r) return undefined;
	const cake = Array.isArray(r.parts) ? r.parts.find((p) => p?.kind === 'cake') : undefined;
	const minor = Array.isArray(r.parts) ? cake?.target_minor : r.target_minor;
	return Number.isSafeInteger(minor) && (minor as number) > 0 ? (minor as number) : undefined;
}

/**
 * A completed order (state 19): collected (ask the chef about anything
 * wrong), a refund agreed with the chef after collection (Q20), or a cake
 * that wasn't collected (Q19: 50% requested automatically when the chef
 * recorded it). A GK delivery nobody took is recovery.ts's (Q18).
 */
function completed(order: Order, base: Base): OrderView {
	const done: OrderView = {
		...base,
		tone: 'success',
		title: 'Your order is complete',
		lines: ['Thank you for ordering with GK Cakes.'],
		confirmed: true
	};
	const r = order.refund;
	if (
		order.fulfillment?.state === 'no_show' &&
		order.delivery_failure?.cause !== 'nobody_available'
	) {
		const half = cakeRefundMinor(order);
		const amount =
			half !== undefined ? formatMoney(half, r?.currency ?? order.currency) : undefined;
		return {
			...done,
			tone: 'warning',
			title: 'Your cake wasn’t collected',
			lines: [
				`Your cake was ready at St Katharine Docks on ${shortDate(order.date)}, ${timeRange(order.slot.starts_at, order.slot.ends_at)}, but it wasn’t collected.`,
				...(amount && r?.reason === 'no_show'
					? [
							`Under the refund terms you get 50% of what you paid back: ${amount}. We requested it automatically when the chef recorded that the cake wasn’t collected.`
						]
					: [])
			],
			contactChef: true
		};
	}
	if (r && r.reason === 'chef_exception' && Number.isSafeInteger(r.target_minor)) {
		const amount = formatMoney(r.target_minor, r.currency ?? order.currency);
		const requested = r.state === 'requested';
		return {
			...done,
			lines: [
				'Thank you for ordering with GK Cakes.',
				`Refund of ${amount} requested: the amount you agreed with the chef after collection.${requested ? ' We’ll update this page as it progresses.' : ''}`
			],
			refund: requested ? undefined : base.refund
		};
	}
	return {
		...done,
		lines: [
			'Thank you for ordering with GK Cakes.',
			'Something wrong with your cake? Please contact the chef: they’ll look at it with you and agree any refund.'
		],
		contactChef: true,
		contactLabel: 'Contact the chef about your cake'
	};
}

/**
 * Paid after the order was cancelled (state 11, Q7): the payment arrived
 * after the place was released; it is kept safe and all of it returned.
 * Shown once the API reports the refund (K11); "returned" only when it says so.
 */
function paidAfterCancel(order: Order, base: Base): OrderView {
	const r = order.refund;
	const p = order.payment;
	const amount =
		p?.state === 'succeeded' && Number.isSafeInteger(p.amount_minor)
			? formatMoney(p.amount_minor, p.currency || order.currency)
			: undefined;
	const returned = r?.state === 'returned';
	return {
		...base,
		refund: undefined,
		tone: returned ? 'success' : 'warning',
		title: 'Payment received after this order was cancelled',
		lines: [
			`This order had already been cancelled because the payment wasn’t completed in time, and then your payment${amount ? ` of ${amount}` : ''} arrived. ${returned ? 'We kept it safe and have returned all of it to you.' : 'We’ve kept it safe and are returning all of it to you.'}`,
			...(base.refund ? [base.refund] : []),
			'Please don’t pay again. Your cake won’t be made for this order.'
		],
		action: 'new_order',
		actionLabel: 'Start a new order',
		paidAfterCancel: true
	};
}

function cancelled(order: Order, base: Base): OrderView {
	const state = order.payment?.state;
	if (state === 'created' || state === 'pending' || state === 'unknown') {
		// A payment whose result is not known yet (for example one under
		// review): never "not completed", never "start a new order".
		return {
			...base,
			tone: 'info',
			title: 'Still checking payment; do not pay again',
			lines: ['This order was cancelled, but we’re still checking a payment for it.', DO_NOT_PAY],
			poll: true,
			doNotPayAgain: true
		};
	}
	if (
		order.cancel_reason === 'payment_not_completed' &&
		order.refund &&
		(order.refund.reason === undefined || order.refund.reason === 'late_capture_terminal')
	) {
		return paidAfterCancel(order, base);
	}
	// A cancellation by the customer or the chef, or a full return (state 13).
	const story = cancelledStory(order);
	if (story) {
		const r = order.refund;
		const partly = r?.state === 'partly_returned';
		const delivery = (r?.delivery_pending_minor ?? 0) > 0;
		const lines = partly
			? [base.refund, delivery ? 'The chef sends the delivery part separately.' : undefined]
			: [story.why, base.refund];
		return {
			...base,
			refund: undefined,
			tone:
				r?.state === 'returned' ? 'success' : r?.state === 'needs_attention' ? 'warning' : 'info',
			title: story.title,
			lines: lines.filter((l): l is string => Boolean(l))
		};
	}
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
	if (fc?.state === 'refused') {
		// Reasons from the API: order_changed, holds_released, window_closed.
		if (fc.reason === 'window_closed' || fc.reason === 'holds_released') {
			return {
				...base,
				tone: 'error',
				title: 'We couldn’t confirm your order',
				lines: [
					fc.reason === 'window_closed'
						? 'The time to confirm this order has passed, so it can’t be confirmed now. Please start a new order.'
						: 'Your place on this date was released, so this order can’t be confirmed. Please start a new order.'
				]
			};
		}
		return {
			...base,
			tone: 'warning',
			title: 'We couldn’t confirm your order yet',
			lines: [
				'Something changed while we were confirming it. You can try again; if it still can’t be confirmed, please start a new order.'
			],
			action: 'confirm_free',
			actionLabel: 'Try again'
		};
	}
	return {
		...base,
		tone: 'info',
		title: 'Confirm your order',
		lines: ['Your order costs nothing to pay. Confirm it to book your date.'],
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

/**
 * The view while a dead-end problem is shown (a missed lead time or an
 * expired price, handed over from checkout or answered to this page's own
 * action: next 'new_order'). Nothing that would ask the API about this order
 * again (start a payment, confirm) is offered: "Start a new order" instead.
 * A payment already under way (an open payment page, or "do not pay again")
 * keeps its own wording and action: money may be moving, so it is never
 * replaced.
 */
export function withOrderProblem(view: OrderView, next: Next | undefined): OrderView {
	if (next !== 'new_order' || view.doNotPayAgain) return view;
	// An open payment page is a payment already under way (K03): kept.
	if (view.action !== 'start_payment' && view.action !== 'confirm_free') return view;
	return {
		...view,
		action: 'new_order',
		actionLabel: 'Start a new order',
		checkoutUrl: undefined
	};
}

/**
 * The checkout query for a new order with the same cakes
 * (v=<variant> / lot=<lot>, repeated once per cake; see basket.ts), without
 * this order's date. Undefined when the order lists no usable cakes.
 */
export function newOrderQuery(order: Order): string | undefined {
	const q = new URLSearchParams();
	for (const item of order.items ?? []) {
		const n = Number.isSafeInteger(item.quantity) ? Math.min(Math.max(item.quantity, 0), 20) : 0;
		for (let i = 0; i < n; i++) {
			if (item.lot_id) q.append('lot', item.lot_id);
			else if (item.variant_id) q.append('v', item.variant_id);
		}
	}
	const query = q.toString();
	return query ? query : undefined;
}

/** The refund line, worded only as far as the API's refund block goes. */
export function refundLine(order: Order): string | undefined {
	const r = order.refund;
	if (!r) return undefined;
	const currency = r.currency ?? order.currency;
	const money = (minor: number | undefined) =>
		Number.isSafeInteger(minor) ? formatMoney(minor as number, currency) : undefined;
	const target = money(r.target_minor);
	const done = money(r.succeeded_minor);
	const of = target ? ` of ${target}` : '';
	switch (r.state) {
		case 'requested':
			return `Refund${of} requested. We’ll update this page as it progresses.`;
		case 'pending':
			return `Refund${of} pending: we’ve asked our payment provider to return it. We’ll update this page when it has been returned.`;
		case 'partly_returned':
			return done && target
				? `${done} of ${target} returned so far; the rest is still pending.`
				: 'Part of your refund has been returned; the rest is still pending.';
		case 'returned':
			return `Refund${done ? ` of ${done}` : ''} returned to your payment method. Your bank may take a few days to show it.`;
		case 'needs_attention':
			return `Your refund${of} needs a check by our team before we can send it. We’ll contact you if we need anything.`;
		default:
			return `Refund${of} requested. We’ll update this page as it progresses.`;
	}
}

/** The next wait before reading the order again: back off from the API's hint, capped at 30 s. */
export function nextPollDelay(attempt: number, hintSeconds = 3): number {
	const base = Math.max(1, hintSeconds) * 1000;
	return Math.min(30_000, Math.round(base * 1.5 ** Math.max(0, attempt)));
}

/** Polling stops after this long; the page then offers a refresh button. */
export const POLL_LIMIT_MS = 5 * 60 * 1000;
