// The order timeline (GK-017 r02b state 19, Q17–Q20, GK-030 slice 3): two
// lanes side by side, Money and Your cake, each with its own ticked,
// current (ringed) and upcoming steps, so money and fulfilment move
// independently.
//
// Only the API decides: a step is done only when the order's blocks say so
// (lifecycle, fulfillment, paid_at, delivery, delivery_failure, refund and
// its parts); money is the API's integer pence, shown and never computed
// (no 50%, no sums). K11 (protected before visible) already hides an
// unsecured outcome in the API, so nothing is reconstructed here. Dates are
// shown only where the API carries the instant (there is no confirmed,
// production-start or ready time in the order view). Times are London time.
import { CHANGEABLE, cancellable, refundParts, shortDate } from './changes';
import { formatMoney, londonTime, londonToday, timeRange } from './format';
import type { Order, RefundPart } from './types';

export type LaneStepState = 'done' | 'current' | 'upcoming' | 'problem';

export interface LaneStep {
	label: string;
	detail?: string;
	state: LaneStepState;
}

export interface Timeline {
	money: LaneStep[];
	cake: LaneStep[];
}

/** The lifecycles the timeline is shown for (an accepted order). */
const ACCEPTED: readonly string[] = ['confirmed', 'in_production', 'ready', 'completed'];

const positive = (minor: number | null | undefined): minor is number =>
	Number.isSafeInteger(minor) && (minor as number) > 0;

/** "Thu 15 October, 18:20" (an instant, London time). */
function moment(at: string | null | undefined): string | undefined {
	if (!at || !Number.isFinite(Date.parse(at))) return undefined;
	return `${shortDate(londonToday(new Date(at)))}, ${londonTime(at)}`;
}

/**
 * The timeline for an accepted order; undefined for other orders and while
 * the GK delivery quote or the Q5 choice is open (states 14 and 15 show
 * their own choice without lanes). now: the browser's clock, for "Today"
 * only.
 */
export function orderTimeline(order: Order, now: Date = new Date()): Timeline | undefined {
	if (!ACCEPTED.includes(order.lifecycle)) return undefined;
	const dc = order.delivery_choice;
	if (dc?.open || dc?.pending || dc?.chosen?.state === 'processing') return undefined;
	const d = order.delivery;
	if (
		order.fulfillment_mode === 'gk_delivery' &&
		d &&
		['quoted', 'payment_checking', 'pay_by_passed'].includes(d.state)
	) {
		return undefined;
	}
	return { money: moneyLane(order, now), cake: cakeLane(order, now) };
}

// ---------------------------------------------------------------- Money

function moneyLane(order: Order, now: Date): LaneStep[] {
	const cur = order.currency || 'GBP';
	const m = (minor: number) => formatMoney(minor, cur);
	const d = order.delivery;
	const f = order.delivery_failure;
	const paidDelivery = positive(d?.paid_minor) ? d.paid_minor : undefined;
	const deliveryOrder =
		order.fulfillment_mode === 'gk_delivery' || paidDelivery !== undefined || Boolean(d?.waived);
	const steps: LaneStep[] = [];

	if (order.payment_requirement === 'none') {
		steps.push({
			label: 'Free order · nothing to pay',
			detail: moment(order.free_confirmed_at),
			state: order.free_confirmed_at ? 'done' : 'upcoming'
		});
	} else {
		steps.push({
			label: deliveryOrder
				? `Cakes paid · ${m(order.total_minor)}`
				: `Paid ${m(order.total_minor)}`,
			detail: moment(order.paid_at),
			state: order.paid_at ? 'done' : 'upcoming'
		});
	}

	if (paidDelivery !== undefined) {
		steps.push({ label: `Delivery paid · ${m(paidDelivery)}`, state: 'done' });
	} else if (d?.waived) {
		steps.push({ label: 'Delivery · no charge', detail: 'Waived by the chef', state: 'done' });
	} else if (order.fulfillment_mode === 'gk_delivery' && (!d || d.state === 'awaiting_quote')) {
		steps.push({
			label: 'Delivery quote',
			detail: 'Appears on this page once the chef has confirmed your delivery',
			state: 'upcoming'
		});
	}

	if (
		order.payment_requirement === 'full' &&
		CHANGEABLE.includes(order.lifecycle) &&
		!order.refund &&
		!f
	) {
		steps.push(refundWindow(order, now, paidDelivery !== undefined));
	}

	if (
		f?.cause === 'our_side' &&
		f.open &&
		positive(f.delivery_paid_minor) &&
		(f.choices ?? ['redelivery', 'collection']).includes('collection')
	) {
		steps.push({
			label: 'Delivery refund',
			detail: '100% if you collect instead',
			state: 'upcoming'
		});
	}

	if (order.refund) steps.push(...refundSteps(order));

	if (f?.cause === 'nobody_available' && f.state === 'no_show' && positive(f.delivery_kept_minor)) {
		steps.push({
			label: 'Delivery payment kept',
			detail: 'The delivery was attempted',
			state: 'upcoming'
		});
	}
	return steps;
}

/**
 * The full-refund window (K05): ahead until full_refund_deadline_at, then
 * finished in the past tense. The browser's clock only picks the wording
 * (the amounts are always the API's preview). "If you cancel" only while
 * the order can be cancelled (the API's actions, or the page's own rule).
 */
function refundWindow(order: Order, now: Date, delivery: boolean): LaneStep {
	const at = order.full_refund_deadline_at;
	const when = `${londonTime(at)} on ${shortDate(londonToday(new Date(at)))}`;
	const canCancel = cancellable(order);
	if (now.getTime() < Date.parse(at)) {
		return {
			label: `Full refund until ${when}`,
			detail: delivery
				? 'Covers cakes and delivery; from then on 50%'
				: canCancel
					? 'From then on 50% if you cancel; 100% if we cancel'
					: 'From then on 50%; 100% if we cancel',
			state: 'upcoming'
		};
	}
	return {
		label: `Full refund ended ${when}`,
		detail: canCancel
			? delivery
				? 'Now 50% of cakes and delivery if you cancel; 100% if we cancel'
				: 'Now 50% if you cancel; 100% if we cancel'
			: '100% if we cancel',
		state: 'done'
	};
}

/**
 * The refund in the money lane: what was requested (one step per kind, from
 * the API's parts), then the return to the payment method, ticked only
 * when the API's refund state is returned.
 */
export function refundSteps(order: Order): LaneStep[] {
	const r = order.refund;
	if (!r) return [];
	const cur = r.currency ?? order.currency ?? 'GBP';
	const m = (minor: number) => formatMoney(minor, cur);
	const requested: LaneStepState = r.state === 'requested' ? 'current' : 'done';
	const parts = refundParts(r);
	// Each step with its state when it isn't the lane's first current step:
	// a part requested beside it has been requested (done); a share still
	// waiting for the chef, or the return, hasn't happened yet.
	const steps: (LaneStep & { later: LaneStepState })[] = [];
	if (parts && parts.length > 0) {
		for (const p of parts) {
			steps.push({
				label: partLabel(p, m),
				detail: partDetail(p, order),
				state: partState(p, requested),
				later: p.pending_chef_minor > 0 ? 'upcoming' : 'done'
			});
		}
	} else if (Number.isSafeInteger(r.target_minor)) {
		// An older API without parts: the whole refund, and the chef's
		// delivery share on its own step when there is one.
		steps.push({
			label: `Refund of ${m(r.target_minor)} requested`,
			detail: cakeDetail(order),
			state: requested,
			later: 'done'
		});
		if (positive(r.delivery_pending_minor)) {
			steps.push({
				label: `Delivery refund · ${m(r.delivery_pending_minor)}`,
				detail: 'Sent separately by the chef · waiting for the chef',
				state: 'current',
				later: 'upcoming'
			});
		}
	}
	steps.push({ ...returnStep(order), later: 'upcoming' });
	// One current (ringed) step per lane, as the design draws it: the first
	// one in progress is "now". A later one keeps its words: a part
	// requested beside it reads done (it was requested); a share waiting
	// for the chef, or the return, reads "not yet" until it comes first.
	const first = steps.findIndex((x) => x.state === 'current');
	return steps.map(({ later, ...x }, i) =>
		x.state === 'current' && i !== first ? { ...x, state: later } : x
	);
}

/**
 * A part's "requested" step from its own amounts, like the split's status:
 * back in full is done; the chef's delivery share not yet sent is current;
 * otherwise the block's state (requested: current; sent on: done).
 */
function partState(p: RefundPart, requested: LaneStepState): LaneStepState {
	if (p.succeeded_minor >= p.target_minor) return 'done';
	if (p.pending_chef_minor > 0) return 'current';
	return requested;
}

function partLabel(p: RefundPart, m: (minor: number) => string): string {
	return p.kind === 'delivery'
		? `Delivery refund of ${m(p.target_minor)} requested`
		: `Refund of ${m(p.target_minor)} requested`;
}

function partDetail(p: RefundPart, order: Order): string | undefined {
	switch (p.kind) {
		case 'cake':
			return cakeDetail(order);
		case 'delivery':
			if (p.pending_chef_minor > 0) return 'Sent separately by the chef · waiting for the chef';
			return order.refund?.reason === 'delivery_failure'
				? '100% of the delivery · sent by the chef'
				: undefined;
		case 'stray':
			return 'A delivery payment that couldn’t be used · returned in full';
		default:
			return undefined;
	}
}

/** Why the cake refund is this amount, for the reasons the design names. */
function cakeDetail(order: Order): string | undefined {
	const r = order.refund;
	switch (r?.reason) {
		case 'no_show':
			return order.delivery_failure?.cause === 'nobody_available'
				? 'Automatically · 50% of the cake price'
				: 'Automatically · 50% for a cake that wasn’t collected';
		case 'chef_exception': {
			// A goal with this reason was created by the agreement (an
			// existing goal keeps its reason), so requested_at is its time.
			const at =
				r.requested_at && Number.isFinite(Date.parse(r.requested_at)) ? r.requested_at : '';
			return at
				? `Amount agreed with the chef on ${shortDate(londonToday(new Date(at)))}`
				: 'Amount agreed with the chef';
		}
		default:
			return undefined;
	}
}

/** The last money step: the return to the payment method, as the API's refund state reports it. */
function returnStep(order: Order): LaneStep {
	const r = order.refund;
	const label = 'Return to your payment method';
	switch (r?.state) {
		case 'returned':
			return { label: 'Returned to your payment method', state: 'done' };
		case 'pending':
			return { label, detail: 'Sent to our payment provider · waiting for them', state: 'current' };
		case 'needs_attention':
			return { label, detail: 'Needs a check by our team', state: 'problem' };
		case 'partly_returned': {
			const cur = r.currency ?? order.currency ?? 'GBP';
			const whole = Number.isSafeInteger(r.succeeded_minor) && Number.isSafeInteger(r.target_minor);
			return {
				label,
				detail: whole
					? `${formatMoney(r.succeeded_minor, cur)} of ${formatMoney(r.target_minor, cur)} returned so far`
					: 'Part of your refund has been returned',
				state: 'current'
			};
		}
		default:
			return { label, state: 'upcoming' };
	}
}

// ---------------------------------------------------------------- Your cake

function cakeLane(order: Order, now: Date): LaneStep[] {
	const f = order.delivery_failure;
	const fs = order.fulfillment?.state ?? null;
	const day = shortDate(order.date);
	const slot = timeRange(order.slot.starts_at, order.slot.ends_at);
	const handedOut = ['out_for_delivery', 'exception', 'completed', 'no_show'].includes(fs ?? '');
	const steps: LaneStep[] = [{ label: 'Order confirmed', state: 'done' }];
	const made: LaneStep =
		['ready', 'completed'].includes(order.lifecycle) || handedOut || Boolean(f)
			? { label: 'Prepared', state: 'done' }
			: order.lifecycle === 'in_production'
				? { label: 'Being made', state: 'current' }
				: { label: 'Being made', state: 'upcoming' };

	// A failed GK delivery (0037): its history stays, whatever was chosen.
	if (f?.cause === 'our_side' || f?.cause === 'nobody_available') {
		steps.push({ label: 'Delivery confirmed by the chef', state: 'done' }, made, {
			label: 'Out for delivery',
			state: 'done'
		});
		if (f.cause === 'nobody_available') {
			steps.push({
				label: 'Nobody available at the address',
				detail: moment(f.at),
				state: 'problem'
			});
			return steps;
		}
		steps.push({ label: 'Delivery couldn’t be completed', detail: moment(f.at), state: 'problem' });
		const chosen = f.chosen?.state === 'chosen' ? f.chosen.choice : undefined;
		if (chosen === 'collection') {
			steps.push(handoverAfterChoice(order, now));
		} else if (chosen === 'redelivery') {
			steps.push(...redelivery(order, f.redelivery_time_set === true));
		}
		return steps;
	}

	if (order.fulfillment_mode === 'gk_delivery') {
		const d = order.delivery;
		const confirmed =
			positive(d?.paid_minor) || Boolean(d?.waived) || d?.state === 'paid' || d?.state === 'waived';
		steps.push(
			confirmed
				? { label: 'Delivery confirmed by the chef', detail: `${day}, ${slot}`, state: 'done' }
				: { label: 'Delivery confirmed by the chef', state: 'upcoming' },
			made
		);
		if (fs === 'out_for_delivery') {
			steps.push(
				{ label: 'Out for delivery', detail: moment(order.fulfillment?.at), state: 'current' },
				{ label: 'Delivered', detail: 'You receive it at your address', state: 'upcoming' }
			);
		} else if (fs === 'completed' && order.lifecycle === 'completed') {
			steps.push(
				{ label: 'Out for delivery', state: 'done' },
				{ label: 'Delivered', detail: moment(order.fulfillment?.at), state: 'done' }
			);
		} else if (fs === 'exception') {
			steps.push(
				{ label: 'Out for delivery', state: 'done' },
				{
					label: 'Delivery couldn’t be completed',
					detail: moment(order.fulfillment?.at),
					state: 'problem'
				}
			);
		} else {
			steps.push(
				{ label: 'Out for delivery', detail: `On ${day}`, state: 'upcoming' },
				{ label: 'Delivered', detail: 'You receive it at your address', state: 'upcoming' }
			);
		}
		return steps;
	}

	// Collection or own courier.
	const ended = order.lifecycle === 'completed';
	steps.push(
		made,
		ended
			? { label: 'Ready for collection', detail: day, state: 'done' }
			: order.lifecycle === 'ready'
				? { label: 'Ready for collection', detail: day, state: 'current' }
				: {
						label: 'Ready for collection',
						detail: 'The chef will share the exact collection address before your collection',
						state: 'upcoming'
					},
		collectedStep(order, `${day}, ${slot} · St Katharine Docks`)
	);
	return steps;
}

/** Collected, not collected (Q19), or the collection still ahead. */
function collectedStep(order: Order, ahead: string): LaneStep {
	const fs = order.fulfillment?.state;
	if (order.lifecycle === 'completed' && fs === 'no_show') {
		return {
			label: 'Not collected',
			detail: `The collection time passed on ${shortDate(order.date)}`,
			state: 'problem'
		};
	}
	if (order.lifecycle === 'completed') {
		return { label: 'Collected', detail: moment(order.fulfillment?.at), state: 'done' };
	}
	return { label: 'Collected', detail: ahead, state: 'upcoming' };
}

/** After an our-side failure, collection was chosen (Q17): the order's current appointment. */
function handoverAfterChoice(order: Order, now: Date): LaneStep {
	if (order.lifecycle === 'completed') return collectedStep(order, '');
	const today = londonToday(now) === order.date;
	return {
		label: 'Collection',
		detail: `${today ? 'Today' : shortDate(order.date)}, ${timeRange(order.slot.starts_at, order.slot.ends_at)} · St Katharine Docks`,
		state: 'current'
	};
}

/** After an our-side failure, redelivery was chosen (Q17). */
function redelivery(order: Order, timeSet: boolean): LaneStep[] {
	const fs = order.fulfillment?.state;
	const when = `${shortDate(order.date)}, ${timeRange(order.slot.starts_at, order.slot.ends_at)}`;
	if (order.lifecycle === 'completed' && fs === 'completed') {
		return [
			{ label: 'Redelivery', state: 'done' },
			{ label: 'Delivered', detail: moment(order.fulfillment?.at), state: 'done' }
		];
	}
	return [
		{
			label: 'Redelivery',
			detail:
				fs === 'out_for_delivery'
					? `Out for delivery since ${moment(order.fulfillment?.at) ?? 'today'}`
					: timeSet
						? when
						: 'New time to be confirmed by the chef',
			state: 'current'
		},
		{ label: 'Delivered', detail: 'You receive it at your address', state: 'upcoming' }
	];
}
