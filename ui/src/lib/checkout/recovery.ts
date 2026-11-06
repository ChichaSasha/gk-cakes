// Order page slice 2 (GK-030): the GK-017 r02b problem states after
// payment. State 10: paid after the preparation time (the chef's offer is
// answered on this page, Q6). State 15: the GK delivery quote, its payment
// and its pay-by (Q9). State 14: "we can't deliver" (the Q5 choice, no
// deadline, Q8; every opening time that day, Q10). State 19: a failed
// delivery (our side: redelivery or collection today, Q17; nobody
// available: like a no-show, Q18).
//
// Only the API decides: what can be chosen is the API's (`open`, `choices`,
// `times`, `payable`, `acceptable`, each at the API's clock); money is the
// API's integer pence (late_payment.paid_minor, the quote's amount,
// delivery.paid_minor, delivery_refund_minor once secured, the no-show's
// cake_refund_minor / delivery_kept_minor), shown and never computed (no
// sums: the Q5 full refund is "everything paid"); a choice's result is
// shown only once the API reports it secured. Times are London time.
import { ApiError } from '../api';
import {
	CHANGEABLE,
	canAct,
	newer,
	shortDate,
	type ChangeBaseline,
	type MoveOption
} from './changes';
import type { OrderRecoveryApi } from './client';
import type { ActionKeys, Who } from './flow';
import { deadlineLabel, formatMoney, londonTime, londonToday, timeRange } from './format';
import { isPaymentPageUrl, nextPollDelay, type OrderView } from './order';
import type { Accepted, ChoiceTime, Order } from './types';

const money = (minor: number | null | undefined, currency: string): string | undefined =>
	Number.isSafeInteger(minor) ? formatMoney(minor as number, currency || 'GBP') : undefined;

const positive = (minor: number | null | undefined): minor is number =>
	Number.isSafeInteger(minor) && (minor as number) > 0;

/** "Thu 15 October, 18:20" (an instant, London time). */
export function shortMoment(at: string): string {
	return `${shortDate(londonToday(new Date(at)))}, ${londonTime(at)}`;
}

/** "Sat 24 October, 13:00–14:00". */
function dayAndTime(date: string, start: string, end: string): string {
	return `${shortDate(date)}, ${timeRange(start, end)}`;
}

function isToday(date: string, now: Date): boolean {
	return londonToday(now) === date;
}

/** A paid GK delivery (its amount from the API), as far as the delivery block says. */
function paidDelivery(order: Order): number | undefined {
	const paid = order.delivery?.paid_minor;
	return positive(paid) ? paid : undefined;
}

function cakePaidWord(order: Order): string {
	return order.payment_requirement === 'none' ? 'confirmed' : 'paid';
}

export interface RecoveryOptions {
	/** The browser's clock, for "today" only (never for what can be chosen). */
	now?: Date;
}

/**
 * The order view with the slice 2 states applied: the late-payment answer
 * (paid_awaiting_agreement), an agreed date, a failed delivery, the Q5
 * choice and the delivery quote. Other orders are returned unchanged.
 */
export function withRecovery(view: OrderView, order: Order, opts: RecoveryOptions = {}): OrderView {
	const now = opts.now ?? new Date();
	let v = view;
	if (order.lifecycle === 'paid_awaiting_agreement') v = latePaymentView(order, view);
	else if (order.lifecycle === 'completed') v = nobodyAvailableView(order, view) ?? view;
	else if (CHANGEABLE.includes(order.lifecycle)) {
		v =
			failureView(order, view, now) ??
			deliveryChoiceView(order, view) ??
			deliveryQuoteView(order, agreedView(order, view)) ??
			agreedView(order, view);
	}
	return withUnmatched(order, v);
}

// ---------------------------------------------------------------- state 10: paid late

function latePaymentView(order: Order, view: OrderView): OrderView {
	const lp = order.late_payment;
	if (!lp) return view;
	const back = money(lp.paid_minor, lp.currency || order.currency);
	const base: OrderView = {
		...view,
		tone: 'warning',
		doNotPayAgain: true,
		promise: false,
		refundTerms: 'agreement',
		action: 'none'
	};
	const intro =
		'Your payment arrived after the time we need to prepare your cake, so we checked with the chef first.';
	if (lp.state === 'processing') {
		const own = Boolean(lp.offer && lp.answer?.state === 'processing');
		return {
			...base,
			tone: 'info',
			title: own ? 'Recording your answer' : 'Payment received',
			lines: [
				own
					? 'We’re recording your answer. This usually takes a few seconds.'
					: 'The chef is updating your order. This usually takes a few seconds.',
				'Please don’t pay again.'
			],
			poll: true
		};
	}
	const offer = lp.offer;
	if (lp.state === 'offered' && offer) {
		const other = offer.kind === 'other';
		const when = dayAndTime(offer.date, offer.start, offer.end);
		const acceptable = offer.acceptable !== false;
		const refused = lp.answer?.state === 'refused' && lp.answer.offer_id === offer.id;
		return {
			...base,
			title: other ? 'The chef offers another date' : 'The chef can make your cake',
			lines: [
				intro,
				other
					? `The chef can’t make ${shortDate(order.date)}, but can offer ${when} (London time). This is only an offer: nothing changes unless you accept it.`
					: `The chef can make your cake for ${when}. Do you agree to this date and time?`,
				`If you’d rather not, you get everything you paid back${back ? `: ${back}` : ''}. Nothing is made until you decide.`,
				...(acceptable
					? []
					: [
							'This offer can no longer be accepted because its time has passed. You can still choose a full refund, or contact the chef.'
						]),
				...(refused
					? [
							'We couldn’t record your last answer because your order changed meanwhile. Nothing has changed: please answer again.'
						]
					: [])
			],
			answer: {
				offerId: offer.id,
				acceptLabel: acceptable ? (other ? `Accept ${when}` : 'Yes, confirm this date') : undefined,
				declineLabel: back ? `No, refund everything · ${back}` : 'No, refund everything',
				declineMinor: Number.isSafeInteger(lp.paid_minor) ? lp.paid_minor : undefined,
				note: 'We keep a record of your answer and when you gave it.'
			},
			contactChef: !acceptable
		};
	}
	// Waiting for the chef (also after an offer lapsed or was withdrawn).
	const lapsed = lp.answer?.state === 'lapsed';
	return {
		...base,
		title: 'Payment received',
		lines: [
			'Your payment arrived after the time we need to prepare your cake for this date, so the chef needs to check it first.',
			`${shortDate(order.date)} is your requested date. It isn’t confirmed yet, and we won’t change it without your agreement.`,
			...(lapsed
				? [
						'The time the chef offered is no longer available, so that offer has lapsed. Nothing has changed: the chef will offer again.'
					]
				: []),
			`If you and the chef can’t agree a date, you get everything you paid back${back ? `: ${back}` : ''}.`,
			'Please don’t pay again.'
		],
		next: {
			title: 'What happens next',
			items: [
				'The chef checks whether your cake can still be made in time.',
				'Their answer appears on this page: your date, or another date they can offer.',
				`You decide here. Nothing is confirmed or moved unless you agree; if you don’t, or no date works, you get ${back ? `${back} back in full` : 'everything you paid back'}.`
			]
		},
		contactChef: true
	};
}

/** "You agreed to …" once an acceptance on this page is secured (state 10, agreed variants). */
function agreedView(order: Order, view: OrderView): OrderView {
	const a = order.agreed;
	if (!a || order.payment_requirement !== 'full') return view;
	const when = dayAndTime(a.date, a.start, a.end);
	return {
		...view,
		lines: [
			`Thank you. You agreed to ${when} on ${shortMoment(a.at)}, and we’ve received your payment.`,
			...(a.kind === 'other' ? ['Your new date replaces the date you first asked for.'] : [])
		]
	};
}

/** What a late-payment answer refusal says (the error path and the order's answer state). */
export const LAPSED =
	'The time the chef offered is no longer available, so the offer has lapsed. Nothing has changed: the chef will offer again.';

// ---------------------------------------------------------------- state 15: the delivery quote

function deliveryQuoteView(order: Order, view: OrderView): OrderView | undefined {
	const d = order.delivery;
	if (order.fulfillment_mode !== 'gk_delivery' || !d) return undefined;
	const currency = d.quote?.currency || order.currency;
	if (d.state === 'quoted' && d.quote) {
		const amount = money(d.quote.amount_minor, currency);
		const failed = d.payment?.state === 'failed' || d.payment?.state === 'cancelled';
		return {
			...view,
			tone: 'warning',
			title: 'Your delivery quote is waiting for payment',
			lines: [
				`The chef can deliver your cake on ${dayAndTime(order.date, order.slot.starts_at, order.slot.ends_at)}${amount ? ` for ${amount}` : ''}.`,
				`Please pay by ${deadlineLabel(d.quote.pay_by)} (48 hours before your delivery). Your cake is ${cakePaidWord(order)} and confirmed; we can deliver only once the delivery is paid.`,
				...(failed
					? ['That payment didn’t go through. No payment was taken for that attempt.']
					: []),
				'Don’t want delivery? Please contact the chef.'
			],
			action: d.payable === true && amount ? 'pay_delivery' : 'none',
			actionLabel: d.payable === true && amount ? `Pay for delivery · ${amount}` : undefined
		};
	}
	if (d.state === 'payment_checking') {
		const url = isPaymentPageUrl(d.payment?.checkout_url)
			? (d.payment?.checkout_url ?? undefined)
			: undefined;
		return {
			...view,
			tone: 'info',
			title: 'Still checking delivery payment; do not pay again',
			lines: [
				'Please don’t pay again. We’ll update this page when we know the result.',
				'Your cake order is confirmed and isn’t affected.',
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
	return undefined;
}

/** A stray delivery payment the chef returns (0035 unmatched_minor). */
function withUnmatched(order: Order, view: OrderView): OrderView {
	const amount = positive(order.delivery?.unmatched_minor)
		? money(order.delivery?.unmatched_minor, order.currency)
		: undefined;
	if (!amount || order.lifecycle === 'cancelled') return view;
	return {
		...view,
		lines: [
			...view.lines,
			`We received a delivery payment of ${amount} that couldn’t be used for your order. The chef will return it to you: please don’t pay it again.`
		]
	};
}

// ---------------------------------------------------------------- state 14: we can't deliver (Q5)

function choiceWords(
	choice: string,
	to: { date: string; starts_at: string; ends_at: string } | null | undefined
): string {
	const at = to ? ` at ${timeRange(to.starts_at, to.ends_at)} on ${shortDate(to.date)}` : '';
	switch (choice) {
		case 'collection':
			return `collecting your cake from St Katharine Docks${at}`;
		case 'customer_courier':
			return `your own courier collecting your cake${at}`;
		case 'full_refund':
			return 'a full refund of everything you paid';
		case 'redelivery':
			return 'delivering your cake again, at a new time the chef confirms with you';
		default:
			return 'your choice';
	}
}

function pendingChoice(
	view: OrderView,
	choice: string,
	to: Parameters<typeof choiceWords>[1]
): OrderView {
	return {
		...view,
		tone: 'info',
		title: 'Confirming your choice',
		lines: [
			`We’re confirming your choice: ${choiceWords(choice, to)}. This usually takes a few seconds.`
		],
		poll: true,
		refund: undefined
	};
}

function updating(view: OrderView): OrderView {
	return {
		...view,
		tone: 'info',
		title: 'Updating your order',
		lines: ['We’re updating your order. This usually takes a few seconds.'],
		poll: true
	};
}

function deliveryChoiceView(order: Order, view: OrderView): OrderView | undefined {
	const dc = order.delivery_choice;
	if (!dc) return undefined;
	const chosen = dc.chosen;
	if (chosen?.state === 'processing') return pendingChoice(view, chosen.choice, chosen.to);
	if (dc.pending && dc.reason) return updating(view);
	if (dc.open) {
		const paid = paidDelivery(order);
		const paidText = money(paid, order.currency);
		if (dc.reason === 'pay_by_passed') {
			const payBy = order.delivery?.quote?.pay_by;
			return {
				...view,
				tone: 'warning',
				title: 'Your delivery wasn’t paid in time',
				lines: [
					payBy
						? `The delivery quote wasn’t paid by ${deadlineLabel(payBy)}, 48 hours before your delivery, so we can’t deliver your cake.`
						: 'The delivery quote wasn’t paid in time, so we can’t deliver your cake.',
					`Your cake is ${cakePaidWord(order)} and still booked for ${shortDate(order.date)}. Please choose below. There’s no deadline: the chef keeps your order open until you decide.`
				],
				action: 'none'
			};
		}
		return {
			...view,
			tone: 'warning',
			title: 'We can’t deliver your cake',
			lines: [
				`We’re sorry: the chef can’t deliver your cake on ${shortDate(order.date)}. Your cake is ${cakePaidWord(order)} and still booked for that day.`,
				...(paidText
					? [`Your delivery payment of ${paidText} will be returned in full, whatever you choose.`]
					: []),
				'Please choose what you’d like below. There’s no deadline: the chef keeps your order open until you decide, and nothing changes until you confirm.'
			],
			action: 'none'
		};
	}
	if (chosen?.state === 'chosen' && chosen.to) {
		const courier = chosen.choice === 'customer_courier';
		const when = `${dayAndTime(chosen.to.date, chosen.to.starts_at, chosen.to.ends_at)} (London time)`;
		const refund = money(
			positive(chosen.delivery_refund_minor) ? chosen.delivery_refund_minor : undefined,
			order.currency
		);
		return {
			...view,
			tone: 'success',
			title: courier ? 'Your courier will collect your cake' : 'You’ll collect your cake',
			lines: [
				courier
					? `Thank you. Your courier collects from St Katharine Docks on ${when}. You book and pay the courier; responsibility passes to them at handover.`
					: `Thank you. Your order is now for collection from St Katharine Docks on ${when}.`,
				...(refund
					? [
							`A refund of your ${refund} delivery payment has been requested. This page shows its progress.`
						]
					: []),
				`Your refund deadline hasn’t changed: ${deadlineLabel(order.full_refund_deadline_at)}.`
			],
			refund: refund && order.refund?.state === 'requested' ? undefined : view.refund,
			refundTracker: Boolean(refund && order.refund)
		};
	}
	return undefined;
}

// ---------------------------------------------------------------- state 19: a failed delivery

function failureView(order: Order, view: OrderView, now: Date): OrderView | undefined {
	const f = order.delivery_failure;
	if (!f) return undefined;
	const slot = dayAndTime(order.date, order.slot.starts_at, order.slot.ends_at);
	if (f.cause === 'nobody_available') {
		if (f.state !== 'processing') return undefined;
		return {
			...view,
			tone: 'warning',
			title: 'We couldn’t deliver your cake',
			lines: [
				`The chef arrived on ${slot}, but nobody was available to take the cake.`,
				'We’re updating your order. This page updates by itself.'
			],
			poll: true,
			refundTerms: 'none'
		};
	}
	if (f.cause !== 'our_side') return undefined;
	const chosen = f.chosen;
	if (chosen?.state === 'processing') return pendingChoice(view, chosen.choice, chosen.to);
	const exception = order.fulfillment?.state === 'exception';
	const today = isToday(order.date, now);
	if (f.open) {
		const paid = money(
			positive(f.delivery_paid_minor) ? f.delivery_paid_minor : undefined,
			order.currency
		);
		const collect = (f.choices ?? ['redelivery', 'collection']).includes('collection');
		const when = today ? 'today' : `on ${shortDate(order.date)}`;
		return {
			...view,
			tone: 'warning',
			title: today ? 'We couldn’t deliver your cake today' : 'We couldn’t deliver your cake',
			lines: [
				`We’re sorry: our delivery on ${slot} couldn’t be completed because of a problem on our side.`,
				collect
					? `Please choose: we deliver again, or you collect ${when}.${paid ? ` If you collect, your delivery payment of ${paid} comes back in full (100%).` : ''}`
					: `There are no collection times left ${when}, so the chef will deliver your cake again. Please confirm below.`
			],
			action: 'none',
			refundTerms: 'none'
		};
	}
	if (f.pending && exception && chosen?.state !== 'chosen') return updating(view);
	if (chosen?.state === 'chosen' && exception) {
		if (chosen.choice === 'collection' && chosen.to) {
			const refund = money(
				positive(chosen.delivery_refund_minor) ? chosen.delivery_refund_minor : undefined,
				order.currency
			);
			const sameDay = isToday(chosen.to.date, now);
			return {
				...view,
				tone: 'success',
				title: sameDay ? 'You’ll collect your cake today' : 'You’ll collect your cake',
				lines: [
					`Thank you. Please collect your cake from St Katharine Docks ${sameDay ? 'today, ' : 'on '}${dayAndTime(chosen.to.date, chosen.to.starts_at, chosen.to.ends_at)} (London time).`,
					...(refund
						? [
								`Your delivery payment of ${refund} comes back in full, because we didn’t deliver. Delivery refund of ${refund} requested.`
							]
						: [])
				],
				refund: refund && order.refund?.state === 'requested' ? undefined : view.refund,
				refundTracker: Boolean(refund && order.refund)
			};
		}
		if (chosen.choice === 'redelivery') {
			const kept = positive(f.delivery_paid_minor)
				? ['Your delivery payment is kept for the new delivery.']
				: [];
			return {
				...view,
				tone: 'success',
				title: 'We’ll deliver your cake again',
				lines: f.redelivery_time_set
					? [
							`Thank you. Your new delivery time is ${slot} (London time), agreed with the chef. There’s nothing more to pay.`,
							...kept
						]
					: [
							'Thank you. The chef will confirm a new delivery time with you on this page. There’s nothing more to pay.',
							...kept
						]
			};
		}
	}
	return undefined;
}

/** Nobody available at the address (Q18): like a no-show, once secured. */
function nobodyAvailableView(order: Order, view: OrderView): OrderView | undefined {
	const f = order.delivery_failure;
	if (f?.cause !== 'nobody_available' || f.state !== 'no_show') return undefined;
	const cake = money(f.cake_refund_minor, order.currency);
	const kept = positive(f.delivery_kept_minor)
		? money(f.delivery_kept_minor, order.currency)
		: undefined;
	return {
		...view,
		tone: 'warning',
		title: 'We couldn’t deliver your cake',
		lines: [
			`The chef arrived on ${dayAndTime(order.date, order.slot.starts_at, order.slot.ends_at)}, but nobody was available to take the cake.`,
			...(cake
				? [
						`As with a cake that isn’t collected, you get 50% of the cake price back: ${cake}, requested automatically.${kept ? ` The delivery payment of ${kept} isn’t refunded, because the delivery was attempted.` : ''}`
					]
				: [])
		],
		contactChef: true,
		refundTracker: Boolean(order.refund)
	};
}

// ---------------------------------------------------------------- the choice card (states 14, 15 passed, 19)

export interface ChoiceOption {
	value: string;
	label: string;
	note: string;
	/** Collection or courier: a time of the day must be chosen too. */
	needsTime: boolean;
}

export type ChoiceKind = 'delivery' | 'failure';

export interface ChoiceCard {
	kind: ChoiceKind;
	heading: string;
	lead?: string;
	options: ChoiceOption[];
	times: MoveOption[];
	/** The time list's legend for each option that needs a time. */
	legends: Record<string, string>;
	/** When an option needs a time and none is left (the API listed none). */
	noTimes: string;
	/** "Your refund deadline stays …" (Q5). */
	deadline?: string;
	/** The latest choice was refused and the choice is open again. */
	refusal?: string;
}

function timeOptions(times: ChoiceTime[] | undefined): MoveOption[] {
	return (times ?? []).map((t) => ({
		id: t.id,
		label: timeRange(t.starts_at, t.ends_at),
		note: 'Available',
		current: false,
		disabled: false,
		gone: false
	}));
}

const REFUSAL_WHY: Record<string, string> = {
	slot_unavailable: 'that time is no longer available.',
	order_changed: 'your order changed meanwhile.',
	not_open: 'this choice wasn’t open at that moment.'
};

function refusalNote(chosen: { state: string; refusal_reason: string | null } | null | undefined) {
	if (chosen?.state !== 'refused') return undefined;
	const why = REFUSAL_WHY[chosen.refusal_reason ?? ''] ?? 'something changed meanwhile.';
	return `Your last choice couldn’t be made: ${why} Nothing has changed: please choose again.`;
}

/**
 * The open choice this viewer can make: a failed delivery (our side) or
 * the Q5 choice, with only the options and times the API lists.
 */
export function choiceCard(
	order: Order,
	who: Who | null | undefined,
	now = new Date()
): ChoiceCard | undefined {
	if (!canAct(who) || !CHANGEABLE.includes(order.lifecycle)) return undefined;
	const day = shortDate(order.date);
	const f = order.delivery_failure;
	if (f?.cause === 'our_side' && f.open) {
		const paid = money(
			positive(f.delivery_paid_minor) ? f.delivery_paid_minor : undefined,
			order.currency
		);
		const today = isToday(order.date, now);
		const listed = f.choices ?? ['redelivery', 'collection'];
		const all: ChoiceOption[] = [
			{
				value: 'redelivery',
				label: 'Deliver it again',
				note: 'The chef confirms a new delivery time with you. Nothing more to pay.',
				needsTime: false
			},
			{
				value: 'collection',
				label: `${today ? 'Collect it today' : `Collect it on ${day}`}${paid ? ' · delivery refunded' : ''}`,
				note: `St Katharine Docks, at any opening time still ahead ${today ? 'today' : `on ${day}`}.${paid ? ` Your ${paid} delivery payment comes back in full.` : ''}`,
				needsTime: true
			}
		];
		return {
			kind: 'failure',
			heading: 'What would you like to do?',
			options: all.filter((o) => listed.includes(o.value)),
			times: timeOptions(f.times),
			legends: {
				collection: today
					? `Collection time today, ${day} · London time`
					: `Collection time on ${day} · London time`
			},
			noTimes: `There are no collection times left ${today ? 'today' : `on ${day}`}. Please choose to have it delivered again, or contact the chef.`,
			refusal: refusalNote(f.chosen)
		};
	}
	const dc = order.delivery_choice;
	if (dc?.open) {
		const listed = dc.choices ?? ['collection', 'customer_courier', 'full_refund'];
		const all: ChoiceOption[] = [
			{
				value: 'collection',
				label: 'Collect it · free',
				note: `St Katharine Docks, London, on ${day}.`,
				needsTime: true
			},
			{
				value: 'customer_courier',
				label: 'Send your own courier',
				note: `You book and pay your courier to collect on ${day}. Responsibility passes to them at handover.`,
				needsTime: true
			},
			{
				value: 'full_refund',
				label: 'Full refund · everything paid',
				note: paidDelivery(order)
					? 'Everything you paid back, cake and delivery. Your order is cancelled.'
					: 'Everything you paid back. Your order is cancelled.',
				needsTime: false
			}
		];
		return {
			kind: 'delivery',
			heading: 'What would you like to do?',
			lead: dc.reason === 'pay_by_passed' ? 'Nothing changes until you confirm.' : undefined,
			options: all.filter((o) => listed.includes(o.value)),
			times: timeOptions(dc.times),
			legends: {
				collection: `Collection time on ${day} · London time`,
				customer_courier: `Courier collection time on ${day} · London time`
			},
			noTimes: `There are no opening times left on ${day}. Please choose a full refund, or contact the chef.`,
			deadline: `Your refund deadline stays ${deadlineLabel(order.full_refund_deadline_at)}.`,
			refusal: refusalNote(dc.chosen)
		};
	}
	return undefined;
}

// ---------------------------------------------------------------- order details rows

export interface DetailRow {
	label: string;
	amount: string;
}

/** The GK delivery's rows in the order details (paid, quoted and not paid, not paid, refunded). */
export function deliveryRows(order: Order): DetailRow[] {
	const d = order.delivery;
	const rows: DetailRow[] = [];
	const paid = paidDelivery(order);
	if (paid !== undefined) {
		rows.push({
			label: 'GK delivery · paid separately',
			amount: money(paid, order.currency) as string
		});
	} else if (
		order.fulfillment_mode === 'gk_delivery' &&
		d?.quote &&
		['quoted', 'payment_checking', 'pay_by_passed'].includes(d.state)
	) {
		const amount = money(d.quote.amount_minor, d.quote.currency || order.currency);
		if (amount) rows.push({ label: 'GK delivery · quoted, not paid', amount });
	} else if (
		order.fulfillment_mode === 'gk_delivery' &&
		order.delivery_choice?.open &&
		!d?.waived
	) {
		rows.push({ label: 'GK delivery', amount: 'Not paid' });
	}
	const refunded =
		order.delivery_failure?.chosen?.state === 'chosen'
			? order.delivery_failure.chosen.delivery_refund_minor
			: order.delivery_choice?.chosen?.state === 'chosen'
				? order.delivery_choice.chosen.delivery_refund_minor
				: undefined;
	if (positive(refunded)) {
		rows.push({
			label: 'Delivery refund · requested',
			amount: money(refunded, order.currency) as string
		});
	}
	return rows;
}

// ---------------------------------------------------------------- this page's own commands

/** What the order showed before this page answered the chef's offer. */
export function lateBaseline(order: Order): ChangeBaseline {
	return { requestedAt: order.late_payment?.answer?.received_at ?? null, version: order.version };
}

/**
 * The outcome of this page's own answer: done once the order left
 * paid_awaiting_agreement (confirming, confirmed, cancelling or cancelled),
 * or a newer answer that lapsed or was refused.
 */
export function lateOutcomeSince(
	order: Order,
	b: ChangeBaseline
): 'done' | 'lapsed' | 'refused' | undefined {
	if (order.lifecycle !== 'paid_awaiting_agreement') return 'done';
	const a = order.late_payment?.answer;
	if (!a || !newer(a.received_at, order, b)) return undefined;
	if (a.state === 'lapsed') return 'lapsed';
	return a.state === 'refused' ? 'refused' : undefined;
}

function chosenOf(order: Order, kind: ChoiceKind) {
	return kind === 'failure' ? order.delivery_failure?.chosen : order.delivery_choice?.chosen;
}

/** What the order showed before this page sent a choice. */
export function choiceBaseline(order: Order, kind: ChoiceKind): ChangeBaseline {
	return { requestedAt: chosenOf(order, kind)?.requested_at ?? null, version: order.version };
}

/** The outcome of this page's own choice (an earlier choice's outcome never counts). */
export function choiceOutcomeSince(
	order: Order,
	kind: ChoiceKind,
	b: ChangeBaseline
): 'done' | 'refused' | undefined {
	if (kind === 'delivery' && ['cancellation_pending', 'cancelled'].includes(order.lifecycle)) {
		return 'done';
	}
	const c = chosenOf(order, kind);
	if (!c || !newer(c.requested_at, order, b)) return undefined;
	if (c.state === 'refused') return 'refused';
	return c.state === 'chosen' || c.state === 'cancelled' ? 'done' : undefined;
}

export type RecoveryNext =
	| 'refresh' // read the order again (it changed, or the choice closed)
	| 'retry' // the same request may be sent again (same key)
	| 'none';

export type RecoveryCommand = 'answer' | 'choice' | 'delivery_payment';

/** A refused answer, choice or delivery payment in the customer's words; nothing changed. */
export function recoveryProblemFor(
	err: unknown,
	command: RecoveryCommand
): { message: string; next: RecoveryNext } {
	if (err instanceof ApiError) {
		switch (err.code) {
			case 'OFFER_NOT_CURRENT':
				return {
					message:
						'This offer has changed or is no longer open. Its current status is shown above.',
					next: 'refresh'
				};
			case 'HONOUR_TOO_LATE':
				return {
					message:
						'This date can no longer be accepted because its time has passed. You can still choose a full refund, or contact the chef.',
					next: 'refresh'
				};
			case 'SLOT_UNAVAILABLE':
				return command === 'answer'
					? { message: LAPSED, next: 'refresh' }
					: {
							message:
								'That time is no longer available. Nothing has changed: please choose another time.',
							next: 'refresh'
						};
			case 'CHOICE_NOT_OPEN':
				return {
					message: 'This choice is no longer open. Your order’s current status is shown above.',
					next: 'refresh'
				};
			case 'PAYMENT_IN_PROGRESS':
				return {
					message:
						'A delivery payment is already in progress. Please don’t pay again: this page shows the result.',
					next: 'refresh'
				};
			case 'DELIVERY_PAY_BY_PASSED':
				return {
					message:
						'The time to pay for delivery has passed, so it can no longer be paid. Your options are shown above.',
					next: 'refresh'
				};
			case 'QUOTE_NOT_CURRENT':
				return {
					message:
						'The chef has updated your delivery quote. Please check the new amount before you pay.',
					next: 'refresh'
				};
			case 'STALE_VERSION':
			case 'CONFLICT':
				return {
					message:
						'Your order changed while this was open. Nothing has changed: please check it and try again.',
					next: 'refresh'
				};
			case 'NOT_FOUND':
				return {
					message:
						'This can’t be done from this browser. Open the order in the tab where you ordered, or sign in if you ordered with your account.',
					next: 'none'
				};
		}
		if (err.status === 401) {
			return { message: 'Your session has ended. Please sign in again to continue.', next: 'none' };
		}
		if (err.status === 503 && command === 'delivery_payment') {
			return {
				message:
					'Delivery payment isn’t available just now. Nothing was charged. Please try again later.',
				next: 'none'
			};
		}
		if (err.status === 0 || err.retryable || err.status >= 500) {
			return {
				message: 'We couldn’t reach our service just now. Nothing has changed. Please try again.',
				next: 'retry'
			};
		}
	}
	return {
		message: 'Something went wrong. Nothing has changed. Please try again, or contact us.',
		next: 'retry'
	};
}

/** Answers the chef's offer (accept or decline); 202, then the page reads the order. */
export function sendAnswer(
	api: OrderRecoveryApi,
	keys: ActionKeys,
	order: Order,
	offerId: string,
	answer: 'accept' | 'decline',
	who: Who
): Promise<Accepted> {
	return api.latePaymentAnswer(
		order.id,
		{ expected_version: order.version, offer_id: offerId, answer },
		keys.for(`late:${order.id}:${offerId}:${answer}`).key,
		who
	);
}

/** Sends the Q5 or failed-delivery choice; target only for a time choice. */
export function sendChoice(
	api: OrderRecoveryApi,
	keys: ActionKeys,
	order: Order,
	kind: ChoiceKind,
	choice: string,
	target: string | undefined,
	who: Who
): Promise<Accepted> {
	const body = {
		expected_version: order.version,
		choice,
		...(target ? { target_slot_id: target } : {})
	};
	const key = keys.for(`${kind}:${order.id}:${order.version}:${choice}:${target ?? ''}`).key;
	return kind === 'failure'
		? api.deliveryFailureChoice(order.id, body, key, who)
		: api.deliveryChoice(order.id, body, key, who);
}

export interface PayDeps {
	sleep?: (ms: number) => Promise<void>;
	now?: () => number;
	waitMs?: number;
}

/**
 * Pays the current delivery quote: asks the API for a delivery payment, then
 * reads the order until its Checkout URL is ready (only a verified Stripe
 * Checkout URL is returned). Undefined when no page is ready in time or the
 * payment ended: the page then shows the order's delivery state.
 */
export async function startDeliveryPayment(
	api: OrderRecoveryApi,
	keys: ActionKeys,
	order: Order,
	who: Who,
	deps: PayDeps = {}
): Promise<string | undefined> {
	const quote = order.delivery?.quote;
	if (!quote) return undefined;
	const sleep = deps.sleep ?? ((ms: number) => new Promise<void>((r) => setTimeout(r, ms)));
	const now = deps.now ?? Date.now;
	const started = await api.deliveryPayment(
		order.id,
		{ expected_version: order.version, quote_id: quote.id },
		keys.for(`delivery-pay:${order.id}:${quote.id}:${order.delivery?.payment?.id ?? ''}`).key,
		who
	);
	const until = now() + (deps.waitMs ?? 45_000);
	await sleep(Math.max(1, started.poll_after_seconds) * 1000);
	for (let attempt = 0; ; attempt++) {
		const o = await api.order(order.id, who);
		const p = o.delivery?.payment;
		if (isPaymentPageUrl(p?.checkout_url)) return p?.checkout_url ?? undefined;
		if (p?.state !== 'created' && p?.state !== 'pending') return undefined;
		if (now() >= until) return undefined;
		await sleep(Math.min(nextPollDelay(attempt, 1), 5000));
	}
}
