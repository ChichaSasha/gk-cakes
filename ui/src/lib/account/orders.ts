// The account's order list (approved GK-016 r02b sketch, "Order list
// mapping"): one chip, one line and one action per order, in the words of
// the built order page (ui/src/lib/checkout/order.ts) so the list and the
// order page never disagree. Only the API's snapshot decides; an unknown
// lifecycle or payment state enables nothing (K02).
import { formatMoney } from '../checkout/format';
import type { AccountOrder } from './client';

export type ChipTone = 'ok' | 'wait' | 'bad' | 'info' | 'done';
export type ListAction = 'pay' | 'retry_pay' | 'confirm' | 'retry_confirm' | 'view';

export interface OrderListItem {
	chip: { tone: ChipTone; text: string };
	line: string;
	/** A refund line under the chip line, exactly as far as the API reports it. */
	refund?: string;
	action: ListAction;
	/** The primary button's words; every other row shows "View order". */
	actionLabel: string;
	/** In progress while payment, date, cake or refund is still open; Past once finished. */
	group: 'current' | 'past';
	/** The date and time are only requested (paid, date to agree). */
	requested: boolean;
}

const KNOWN_PAYMENT = ['created', 'pending', 'unknown', 'succeeded', 'failed', 'cancelled'];
const UNKNOWN: OrderListItem = {
	chip: { tone: 'info', text: 'Status not available' },
	line: 'We can’t show this order’s status right now. Please refresh later.',
	action: 'view',
	actionLabel: 'View order',
	group: 'current',
	requested: false
};

function view(
	tone: ChipTone,
	text: string,
	line: string,
	group: 'current' | 'past',
	extra: Partial<OrderListItem> = {}
): OrderListItem {
	return {
		chip: { tone, text },
		line,
		action: 'view',
		actionLabel: 'View order',
		group,
		requested: false,
		...extra
	};
}

/** The refund line, worded only as far as the API's refund block goes (as the order page). */
export function refundLine(o: AccountOrder): string | undefined {
	const r = o.refund;
	if (!r) return undefined;
	const currency = r.currency ?? o.currency;
	const money = (minor: number | undefined) =>
		Number.isSafeInteger(minor) ? formatMoney(minor as number, currency) : undefined;
	const target = money(r.target_minor);
	const done = money(r.succeeded_minor);
	const of = target ? ` of ${target}` : '';
	switch (r.state) {
		case 'pending':
			return `Refund${of} pending: we’ve asked our payment provider to return it.`;
		case 'partly_returned':
			return done && target
				? `${done} of ${target} returned so far; the rest is still pending.`
				: 'Part of your refund has been returned; the rest is still pending.';
		case 'returned':
			return `Refund${done ? ` of ${done}` : ''} returned to your payment method.`;
		case 'needs_attention':
			return `Your refund${of} needs a check by our team before we can send it.`;
		default:
			return `Refund${of} requested.`;
	}
}

const REFUND_CHIP: Record<string, string> = {
	requested: 'Cancelled · refund requested',
	pending: 'Cancelled · refund pending',
	partly_returned: 'Cancelled · refund partly returned',
	needs_attention: 'Cancelled · refund being checked'
};

/** The list row of one order. */
export function orderListItem(o: AccountOrder): OrderListItem {
	const p = o.payment;
	if (p && !KNOWN_PAYMENT.includes(p.state)) return UNKNOWN;
	const refund = refundLine(o);
	const refundOpen = !!o.refund && o.refund.state !== 'returned';
	const withRefund = (item: OrderListItem): OrderListItem =>
		refund ? { ...item, refund, group: refundOpen ? 'current' : item.group } : item;
	const delivery = o.fulfillment_mode === 'gk_delivery';
	const courier = o.fulfillment_mode === 'customer_courier';

	switch (o.lifecycle) {
		case 'awaiting_payment': {
			if (o.payment_requirement === 'none') {
				const fc = o.free_confirmation;
				if (fc?.state === 'processing' || fc?.state === 'confirmed') {
					return view('info', 'Confirming', 'We’re confirming your free order.', 'current');
				}
				if (fc?.state === 'refused') {
					if (fc.reason === 'window_closed' || fc.reason === 'holds_released') {
						return view('bad', 'Couldn’t confirm', 'Please start a new order.', 'current');
					}
					return {
						...view(
							'wait',
							'Couldn’t confirm yet',
							'Something changed; you can try again.',
							'current'
						),
						action: 'retry_confirm',
						actionLabel: 'Try again'
					};
				}
				return {
					...view(
						'wait',
						'Confirm your order',
						'Your order costs nothing to pay. Confirm it to book your date.',
						'current'
					),
					action: 'confirm',
					actionLabel: 'Confirm order'
				};
			}
			if (!p) {
				return {
					...view(
						'wait',
						'Waiting for payment',
						'Your place is held for 15 minutes while you pay. Nothing has been paid yet.',
						'current'
					),
					action: 'pay',
					actionLabel: 'Continue to payment'
				};
			}
			if (p.state === 'failed' || p.state === 'cancelled') {
				return {
					...view(
						'wait',
						'Payment didn’t go through',
						'No payment was taken for that attempt. You can try again while your place is still held.',
						'current'
					),
					action: 'retry_pay',
					actionLabel: 'Try paying again'
				};
			}
			if (p.state === 'succeeded') {
				return view(
					'info',
					'Payment received · confirming',
					'We’ve received your payment and are confirming your order.',
					'current'
				);
			}
			if (p.payment_page_open) {
				return {
					...view(
						'wait',
						'Waiting for payment',
						'Your secure payment page is ready. Your place is held while you pay.',
						'current'
					),
					action: 'pay',
					actionLabel: 'Continue to payment'
				};
			}
			return view(
				'wait',
				'Still checking payment',
				'We’re waiting for your payment result. Please don’t pay again.',
				'current'
			);
		}
		case 'confirmation_pending':
			return o.payment_requirement === 'none'
				? view('info', 'Confirming', 'We’re confirming your free order.', 'current')
				: view(
						'info',
						'Payment received · confirming',
						'We’ve received your payment and are confirming your order.',
						'current'
					);
		case 'paid_awaiting_agreement':
			return withRefund(
				view(
					'wait',
					'Payment received · date to agree',
					'Your payment arrived after the preparation time for this date. The chef will contact you about the date.',
					'current',
					{ requested: true }
				)
			);
		case 'confirmed':
			return withRefund(
				view(
					'ok',
					'Confirmed',
					delivery
						? 'GK delivery confirmed by the chef.'
						: courier
							? 'Your courier collects.'
							: 'Collect at St Katharine Docks.',
					'current'
				)
			);
		case 'in_production':
			return withRefund(
				view(
					'ok',
					'Being prepared',
					delivery
						? 'Your cake is being prepared.'
						: 'Your cake is being prepared. The collection details will show here about 48 hours before.',
					'current'
				)
			);
		case 'ready':
			return withRefund(
				view(
					'ok',
					delivery ? 'Ready for delivery' : 'Ready to collect',
					delivery
						? 'Your cake is ready for delivery.'
						: courier
							? 'Your cake is ready. Your courier collects it at your time.'
							: 'Your cake is ready. Collect it at St Katharine Docks at your time.',
					'current'
				)
			);
		case 'completed':
			return withRefund(
				view(
					'done',
					delivery ? 'Delivered' : courier ? 'Handed to your courier' : 'Collected',
					'Thank you for ordering with GK Cake London.',
					'past'
				)
			);
		case 'cancelled': {
			if (p && (p.state === 'created' || p.state === 'pending' || p.state === 'unknown')) {
				return view(
					'wait',
					'Still checking payment',
					'This order was cancelled, but we’re still checking a payment for it. Please don’t pay again.',
					'current'
				);
			}
			if (o.refund) {
				return o.refund.state === 'returned'
					? view('done', 'Cancelled · refunded', refund as string, 'past')
					: view(
							'bad',
							REFUND_CHIP[o.refund.state] ?? 'Cancelled · refund requested',
							refund as string,
							'current'
						);
			}
			if (p?.state === 'succeeded') {
				return view(
					'wait',
					'Cancelled · payment recorded',
					'We have a payment recorded for this order. Please don’t pay again; we’ll contact you.',
					'current'
				);
			}
			if (o.cancel_reason === 'payment_not_completed') {
				return view(
					'done',
					'Cancelled · not paid',
					'The payment wasn’t completed in time, so your place was released. No payment was taken.',
					'past'
				);
			}
			if (o.cancel_reason === 'free_not_confirmed') {
				return view(
					'done',
					'Cancelled · not confirmed',
					'This free order wasn’t confirmed in time, so it was cancelled.',
					'past'
				);
			}
			return view('done', 'Cancelled', 'This order was cancelled.', 'past');
		}
	}
	return UNKNOWN;
}

/** "Chocolate swirl, Pink piping" (the order's cakes, as the API names them). */
export function orderName(o: AccountOrder): string {
	const names = o.items.map((i) =>
		i.quantity > 1 ? `${i.cake_name} × ${i.quantity}` : i.cake_name
	);
	return names.length > 0 ? names.join(', ') : 'Your order';
}

const MODE: Record<string, string> = {
	collection: 'Collect',
	customer_courier: 'Your courier',
	gk_delivery: 'GK delivery'
};

export function modeLabel(mode: string): string {
	return MODE[mode] ?? 'Handover to agree';
}

/**
 * A short order reference: the start of the order ID, which order emails and
 * the order page give in full ("Order reference: <id>").
 */
export function orderRef(o: { id: string }): string {
	return o.id.slice(0, 8);
}

/** Orders split into In progress and Past, each newest placed first (the API's order). */
export function groupOrders(orders: AccountOrder[]): {
	current: { order: AccountOrder; item: OrderListItem }[];
	past: { order: AccountOrder; item: OrderListItem }[];
} {
	const current: { order: AccountOrder; item: OrderListItem }[] = [];
	const past: { order: AccountOrder; item: OrderListItem }[] = [];
	for (const order of orders) {
		const item = orderListItem(order);
		(item.group === 'current' ? current : past).push({ order, item });
	}
	return { current, past };
}
