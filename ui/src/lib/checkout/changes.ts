// Changes on the order page (GK-030 slice 1): cancellation (GK-017 r02b
// state 12), refund progress (state 13) and the same-day time change
// (state 18). Only the API decides: eligibility is the API's (K05, K06),
// amounts are the preview's and the order's integer pence (shown, never
// computed: no 50% and no sums here), the side of the deadline is the
// preview's rate, and a refund step is finished only when the API's refund
// state says so. Times are London time.
import { ApiError } from '../api';
import type { Who } from './flow';
import { dateLabel, deadlineLabel, formatMoney, londonTime, timeRange } from './format';
import type {
	CancellationPreview,
	CustomerAction,
	MoveChoices,
	Order,
	Refund,
	RefundPart
} from './types';

/** Lifecycles the API accepts a customer cancellation or pickup move in (K05, K06). */
export const CHANGEABLE: readonly string[] = ['confirmed', 'in_production', 'ready'];

/**
 * Whether this viewer may ask for changes at all: the signed-in owner, or a
 * guest holding this tab's order capability (checkout binds it with read,
 * pay, cancel and move_pickup). The API checks the capability again and
 * answers 404 when it does not allow the command.
 */
export function canAct(who: Who | null | undefined): who is Who {
	return who?.kind === 'account' ? who.token !== '' : who?.kind === 'guest' && who.secret !== '';
}

/**
 * The API's actions for this caller (0038), when it sends them; undefined
 * from an older API (the page then decides with its own rules).
 */
export function apiActions(order: Order): readonly string[] | undefined {
	return Array.isArray(order.actions) ? order.actions : undefined;
}

/**
 * Whether this viewer may ask for action: the API's actions when it sends
 * them (they follow the order's state at the API's clock and a guest
 * link's grants), otherwise the page's own rule (fallback). A hint only:
 * every command is still checked by the API.
 */
export function allows(
	order: Order,
	who: Who | null | undefined,
	action: CustomerAction,
	fallback: boolean
): boolean {
	if (!canAct(who)) return false;
	const actions = apiActions(order);
	return actions ? actions.includes(action) : fallback;
}

/** Whether a protected change of the order is unresolved (the API would answer CONFLICT). */
export function changeInProgress(order: Order): boolean {
	return (
		order.lifecycle === 'cancellation_pending' ||
		order.lifecycle === 'confirmation_pending' ||
		order.cancellation?.state === 'processing' ||
		order.appointment_change?.state === 'processing' ||
		Boolean(order.fulfillment?.pending) ||
		order.delivery_choice?.pending === true ||
		order.delivery_failure?.pending === true
	);
}

/**
 * Whether "Preview cancellation" is offered: an accepted order (confirmed,
 * in production or ready) with nothing else in progress; never a GK
 * delivery once it has been sent out (or has a delivery problem being
 * handled with the chef), and never a completed or cancelled order.
 */
export function mayCancel(order: Order, who: Who | null | undefined): boolean {
	// An open delivery choice is offered instead (it can return everything).
	if (!canAct(who) || deliveryProblemOpen(order)) return false;
	return allows(order, who, 'cancel', cancelFallback(order));
}

/** The page's own rule when the API sends no actions (an older API). */
/**
 * Whether the order can be cancelled by its owner now, as far as the order
 * itself says (the API's actions when sent, else the page's own rule):
 * the timeline's wording only ("if you cancel"), never a button.
 */
export function cancellable(order: Order): boolean {
	const actions = apiActions(order);
	return actions ? actions.includes('cancel') : cancelFallback(order);
}

function cancelFallback(order: Order): boolean {
	if (!CHANGEABLE.includes(order.lifecycle) || changeInProgress(order)) return false;
	const sent = ['out_for_delivery', 'exception'].includes(order.fulfillment?.state ?? '');
	return !(order.fulfillment_mode === 'gk_delivery' && sent);
}

/**
 * Whether a GK delivery problem gives the customer their own choice (the Q5
 * choice when GK can't deliver or the delivery quote's pay-by passed, or a
 * failed delivery): that choice can return everything paid, so the standard
 * cancellation (100% / 50%) is not offered here; the page shows the choice
 * itself (states 14 and 19, recovery.ts).
 */
export function deliveryProblemOpen(order: Order): boolean {
	return order.delivery_choice?.open === true || order.delivery_failure?.open === true;
}

/** Whether the page says "To cancel this order, please contact the chef." instead of offering cancellation. */
export function cancelViaChef(order: Order, who: Who | null | undefined): boolean {
	if (!canAct(who)) return false;
	const open = CHANGEABLE.includes(order.lifecycle) && !changeInProgress(order);
	if (deliveryProblemOpen(order)) return allows(order, who, 'cancel', open);
	// The API offers this caller no cancellation on a changeable order (for
	// example after the dispatch, or a link without the cancel grant): the
	// terms still say "Cancel before…", so say who to ask.
	const actions = apiActions(order);
	return open && actions !== undefined && !actions.includes('cancel');
}

/**
 * The time-change section (state 18): collection and own-courier orders can
 * move on the same day ('move'; 'moving' while a move is processed); a GK
 * delivery time changes only with the chef ('contact_chef'). With the API's
 * actions, an order the API doesn't offer a move for says so ('fixed').
 */
export function timeChange(
	order: Order,
	who: Who | null | undefined
): 'none' | 'move' | 'moving' | 'contact_chef' | 'fixed' {
	if (!canAct(who) || !CHANGEABLE.includes(order.lifecycle)) return 'none';
	if (order.fulfillment_mode === 'gk_delivery') return 'contact_chef';
	if (order.appointment_change?.state === 'processing') return 'moving';
	if (changeInProgress(order)) return 'none';
	if (apiActions(order)) return allows(order, who, 'move_pickup', false) ? 'move' : 'fixed';
	return 'move';
}

/** What the time-change section says when the API offers no move ('fixed'). */
export const MOVE_FIXED =
	'This order’s time can’t be changed online. If you need a different time, please contact the chef.';

/** MOVE_FIXED when cancelling also goes through the chef (one contact-the-chef sentence). */
export const MOVE_FIXED_CANCEL =
	'This order’s time can’t be changed online. To change the time or cancel this order, please contact the chef.';

/** Whether the page should read the order again soon for these changes. */
export function changePoll(order: Order): boolean {
	return (
		order.appointment_change?.state === 'processing' || order.cancellation?.state === 'processing'
	);
}

/** Whether the order has a GK delivery payment (it is part of what the customer paid). */
export function deliveryPaid(order: Order): boolean {
	const paid = order.delivery?.paid_minor;
	return (
		order.fulfillment_mode === 'gk_delivery' && Number.isSafeInteger(paid) && (paid as number) > 0
	);
}

// ---------------------------------------------------------------- cancellation preview (state 12)

/** "Thu 22 October" (a London day, without the year). */
export function shortDate(date: string): string {
	return dateLabel(date).replace(/ \d{4}$/, '');
}

/** Time between two server instants as "3 days 4 hours", "5 hours 20 minutes" or "12 minutes". */
export function timeLeft(from: string, to: string): string {
	const ms = Date.parse(to) - Date.parse(from);
	if (!Number.isFinite(ms) || ms <= 0) return 'no time';
	const minutes = Math.floor(ms / 60_000);
	const days = Math.floor(minutes / 1440);
	const hours = Math.floor((minutes % 1440) / 60);
	const mins = minutes % 60;
	const unit = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`;
	if (days > 0)
		return hours > 0 ? `${unit(days, 'day')} ${unit(hours, 'hour')}` : unit(days, 'day');
	if (hours > 0)
		return mins > 0 ? `${unit(hours, 'hour')} ${unit(mins, 'minute')}` : unit(hours, 'hour');
	return minutes > 0 ? unit(minutes, 'minute') : 'less than a minute';
}

export interface PreviewLine {
	label: string;
	amount: string;
}

export interface PreviewView {
	/** The full refund applies (the preview's rate is 100). */
	before: boolean;
	/** Which side of the deadline, e.g. "Cancelling before 11:00 (London time) on Sat 17 October". */
	side: string;
	/** The preview's amount_minor, formatted. */
	amount: string;
	/** Why this amount, in one sentence. */
	basis: string;
	/** Before the deadline: the time left as of the server's preview time, and that time. */
	timeLeft?: string;
	checkedAt?: string;
	deadline: string;
	/** GK delivery: what goes back to the payment method and what the chef sends. */
	split: PreviewLine[];
	/** A stray delivery payment returned in full beside the amount. */
	unmatched?: string;
	/** After the deadline (without a delivery split): the labelled rounding example. */
	roundingExample: boolean;
	/** Nothing was paid (a free order): nothing to refund. */
	nothingPaid: boolean;
	buttonLabel: string;
}

/** Each labelled allocation of a preview (0038 kind) in the split. */
const ALLOCATION_LABEL: Record<string, string> = {
	cake: 'Cakes · back to your payment method',
	delivery: 'Delivery · back to your payment method',
	// Recognised, but said in its own sentence beside the amount (never split).
	stray: ''
};

/** The cancellation panel for a preview; delivery: the order has a paid GK delivery. */
export function previewView(
	p: CancellationPreview,
	opts: { delivery?: boolean } = {}
): PreviewView {
	const money = (minor: number) => formatMoney(minor, p.currency || 'GBP');
	const before = p.rate === 100;
	const deadline = deadlineLabel(p.full_refund_deadline_at);
	const nothingPaid = p.paid_minor === 0;
	const pending = Number.isSafeInteger(p.delivery_pending_minor) ? p.delivery_pending_minor : 0;
	const delivery = opts.delivery === true || pending > 0;
	// Labelled allocations (0038 kind): split whenever more than the cakes
	// come back; otherwise, as before, only beside the chef's delivery share.
	const labelled =
		p.allocations.length > 0 &&
		p.allocations.every((a) => typeof a.kind === 'string' && a.kind in ALLOCATION_LABEL);
	// A stray payment comes back beside amount_minor, not inside it: it is
	// never a line of the split under "You get back" (its own sentence).
	const inAmount = labelled ? p.allocations.filter((a) => a.kind !== 'stray') : p.allocations;
	const splitNeeded = pending > 0 || (labelled && inAmount.some((a) => a.kind !== 'cake'));
	const chef =
		pending > 0
			? [{ label: 'Delivery · sent separately by the chef', amount: money(pending) }]
			: [];
	const split: PreviewLine[] = !splitNeeded
		? []
		: labelled
			? [
					...inAmount.map((a) => ({
						label: ALLOCATION_LABEL[a.kind as string],
						amount: money(a.amount_minor)
					})),
					...chef
				]
			: [
					...p.allocations.map((a) => ({
						label:
							p.allocations.length === 1 && !(p.unmatched_minor > 0)
								? 'Cakes · back to your payment method'
								: 'Back to your payment method',
						amount: money(a.amount_minor)
					})),
					...chef
				];
	const strays = labelled ? p.allocations.filter((a) => a.kind === 'stray') : [];
	const stray =
		p.unmatched_minor > 0
			? p.unmatched_minor
			: strays.length === 1 && strays[0].amount_minor > 0
				? strays[0].amount_minor
				: undefined;
	let basis: string;
	if (nothingPaid) basis = 'This order cost nothing, so there’s nothing to refund.';
	else if (before) basis = 'Everything you paid: a full refund.';
	else if (p.rate === 50)
		basis = `50% of what you paid${delivery ? ' (cakes and delivery)' : ''}, rounded in your favour.`;
	else basis = `${p.rate}% of what you paid.`;
	return {
		before,
		side: before ? `Cancelling before ${deadline}` : 'Cancelling after the full-refund deadline',
		amount: money(p.amount_minor),
		basis,
		timeLeft: before ? timeLeft(p.computed_at, p.full_refund_deadline_at) : undefined,
		checkedAt: before ? londonTime(p.computed_at) : undefined,
		deadline,
		split,
		unmatched:
			stray !== undefined
				? `We’ll also return in full a delivery payment of ${money(stray)} that couldn’t be used for your order.`
				: undefined,
		roundingExample: !before && !nothingPaid && split.length === 0,
		nothingPaid,
		buttonLabel: nothingPaid ? 'Cancel order' : `Cancel order · refund ${money(p.amount_minor)}`
	};
}

/**
 * The boundary alert: the full-refund deadline passed between the preview
 * the customer saw and the new one (the new rate is lower).
 */
export function boundaryAlert(
	seen: CancellationPreview | null | undefined,
	fresh: CancellationPreview
): { deadline: string; amount: string } | undefined {
	if (!seen || !(fresh.rate < seen.rate)) return undefined;
	return {
		deadline: deadlineLabel(fresh.full_refund_deadline_at),
		amount: formatMoney(fresh.amount_minor, fresh.currency || 'GBP')
	};
}

/**
 * How long until the open preview must be asked for again (by this
 * browser's clock, corrected by the server's computed_at at receipt): the
 * full-refund deadline while the rate is 100, otherwise the preview's
 * 10-minute expiry. Undefined when that is too far away for a timer.
 */
export function previewRefreshDelay(
	p: CancellationPreview,
	receivedAt: number,
	now: number
): number | undefined {
	const serverNow = Date.parse(p.computed_at) + (now - receivedAt);
	const targets = [Date.parse(p.expires_at)];
	if (p.rate === 100) targets.push(Date.parse(p.full_refund_deadline_at));
	const at = Math.min(...targets.filter(Number.isFinite));
	if (!Number.isFinite(at)) return undefined;
	const ms = Math.max(0, at - serverNow) + 1000;
	return ms <= 2_147_000_000 ? ms : undefined;
}

export type CancelNext =
	| 'repreview' // ask for a new preview and confirm again
	| 'close' // the order can't be cancelled now: close the panel, show the order
	| 'retry' // the same confirmation may be sent again (same key)
	| 'none';

/** A refused preview or cancellation in the customer's words. */
export function cancelProblemFor(err: unknown): { message: string; next: CancelNext } {
	if (err instanceof ApiError) {
		switch (err.code) {
			case 'PREVIEW_STALE':
			case 'INVALID_INPUT':
				return {
					message: 'We’ve refreshed your refund amount. Please check it and confirm again.',
					next: 'repreview'
				};
			case 'STALE_VERSION':
				return {
					message:
						'Your order changed while this was open, so we’ve refreshed the amount. Please check it and confirm again.',
					next: 'repreview'
				};
			case 'CANCELLATION_NOT_ELIGIBLE':
				return {
					message: 'This order can no longer be cancelled here. Its current status is shown above.',
					next: 'close'
				};
			case 'CONFLICT':
				return {
					message:
						'Another change to this order is still being processed. Please wait a moment and try again.',
					next: 'close'
				};
			case 'NOT_FOUND':
				return {
					message:
						'This order can’t be cancelled from this browser. Open it in the tab where you ordered, or sign in if you ordered with your account.',
					next: 'close'
				};
		}
		if (err.status === 401) {
			return {
				message: 'Your session has ended. Please sign in again to continue.',
				next: 'close'
			};
		}
		if (err.status === 0 || err.retryable || err.status >= 500) {
			return {
				message: 'We couldn’t reach our service just now. Please try again.',
				next: 'retry'
			};
		}
	}
	return {
		message: 'Something went wrong. Please try again, or contact us.',
		next: 'retry'
	};
}

// ---------------------------------------------------------------- refund progress (state 13)

export type StepState = 'done' | 'current' | 'upcoming';

export interface RefundStep {
	label: string;
	state: StepState;
}

export type TrackKind = 'requested' | 'pending' | 'attention' | 'returned';

const DELIVERY_PENDING = (r: Refund) =>
	Number.isSafeInteger(r.delivery_pending_minor) ? (r.delivery_pending_minor as number) : 0;

/**
 * Where the card refund is, from the API's refund state only. Partly
 * returned: still with the payment provider while anything is in progress
 * or nothing is left to the chef; otherwise the card part is back and only
 * the chef's delivery part is waiting.
 */
export function trackKind(r: Refund): TrackKind {
	switch (r.state) {
		case 'pending':
			return 'pending';
		case 'needs_attention':
			return 'attention';
		case 'returned':
			return 'returned';
		case 'partly_returned': {
			// The card part is back only when nothing is in progress and what is
			// returned plus the chef's part covers the whole refund.
			const delivery = DELIVERY_PENDING(r);
			const covered =
				Number.isSafeInteger(r.succeeded_minor) &&
				Number.isSafeInteger(r.target_minor) &&
				r.succeeded_minor + delivery >= r.target_minor;
			return (r.in_progress_minor ?? 0) === 0 && delivery > 0 && covered ? 'returned' : 'pending';
		}
		default:
			return 'requested';
	}
}

const TRACKS: Record<TrackKind, { labels: [string, string, string]; current: number }> = {
	requested: {
		labels: [
			'Refund requested',
			'Waiting to be sent to our payment provider',
			'Return to your payment method'
		],
		current: 0
	},
	pending: {
		labels: [
			'Refund requested',
			'Sent to our payment provider · waiting for them',
			'Return to your payment method'
		],
		current: 1
	},
	attention: {
		labels: ['Refund requested', 'Needs a check by our team', 'Return to your payment method'],
		current: 1
	},
	returned: {
		labels: ['Refund requested', 'Sent to our payment provider', 'Returned to your payment method'],
		current: 3
	}
};

/** The card refund's tracker steps; past tense only for finished steps. */
export function refundSteps(r: Refund): RefundStep[] {
	const t = TRACKS[trackKind(r)];
	return t.labels.map((label, i) => ({
		label,
		state: i < t.current ? 'done' : i === t.current ? 'current' : 'upcoming'
	}));
}

export interface RefundLineItem {
	label: string;
	status: string;
	amount?: string;
}

export interface RefundProgressView {
	steps: RefundStep[];
	trackLabel: string;
	/** The card refund and the chef's delivery refund, each on its own line (only with a delivery part). */
	lines: RefundLineItem[];
	due: string;
	returnedSoFar: string;
	/** The refund state in the summary's words (state 11's "Refund · requested"). */
	stateWord: string;
	/** A refund the chef agreed (chef_exception): "Refund agreed" in the summary (Q20). */
	agreed: boolean;
}

const STATE_WORD: Record<string, string> = {
	requested: 'requested',
	pending: 'pending',
	partly_returned: 'partly returned',
	returned: 'returned',
	needs_attention: 'needs a check'
};

/**
 * The refund's parts (0038) when the API sends them whole (every amount an
 * integer), without kinds whose target is 0; undefined otherwise.
 */
export function refundParts(r: Refund): RefundPart[] | undefined {
	if (!Array.isArray(r.parts) || r.parts.length === 0) return undefined;
	const whole = r.parts.every(
		(p) =>
			typeof p?.kind === 'string' &&
			[p.target_minor, p.succeeded_minor, p.in_progress_minor, p.pending_chef_minor].every(
				(n) => Number.isSafeInteger(n) && n >= 0
			)
	);
	return whole ? r.parts.filter((p) => p.target_minor > 0) : undefined;
}

/** A part's line label in the refund split (state 13). */
function partLabel(p: RefundPart): string {
	switch (p.kind) {
		case 'cake':
			return 'Cakes · to your payment method';
		case 'delivery':
			return p.pending_chef_minor > 0
				? 'Delivery · sent separately by the chef'
				: 'Delivery · to your payment method';
		case 'stray':
			return 'Delivery payment that couldn’t be used · returned in full';
		default:
			return 'To your payment method';
	}
}

/** A part's status, from its own amounts and the block's state (never ahead of them). */
function partStatus(p: RefundPart, r: Refund): string {
	if (p.succeeded_minor >= p.target_minor) return 'Returned';
	if (p.pending_chef_minor > 0) return 'Waiting for the chef';
	if (r.state === 'needs_attention') return 'Needs a check by our team';
	if (p.in_progress_minor > 0 || p.succeeded_minor > 0)
		return 'Sent · waiting for our payment provider';
	return 'Requested';
}

/** The refund's tracker, lines and summary amounts, as far as the API reports them. */
export function refundProgress(r: Refund, currency = 'GBP'): RefundProgressView {
	const cur = r.currency ?? currency;
	const money = (minor: number | undefined) =>
		formatMoney(Number.isSafeInteger(minor) ? (minor as number) : 0, cur);
	const delivery = DELIVERY_PENDING(r);
	const kind = trackKind(r);
	const parts = refundParts(r);
	// With parts: each kind on its own line with its own amount whenever more
	// than one kind comes back or the chef sends the delivery part.
	const chefPart = parts?.some((p) => p.kind === 'delivery' && p.pending_chef_minor > 0) === true;
	const byPart = parts !== undefined && (parts.length > 1 || chefPart);
	const lines: RefundLineItem[] =
		parts && byPart
			? parts.map((p) => ({
					label: partLabel(p),
					status: partStatus(p, r),
					amount: money(p.target_minor)
				}))
			: delivery > 0
				? [
						{
							label: 'Cakes · to your payment method',
							status: {
								requested: 'Requested',
								pending: 'Sent · waiting for our payment provider',
								attention: 'Needs a check by our team',
								returned: 'Returned'
							}[kind]
						},
						{
							label: 'Delivery · sent separately by the chef',
							status: 'Waiting for the chef',
							amount: money(delivery)
						}
					]
				: [];
	return {
		steps: refundSteps(r),
		trackLabel: delivery > 0 || chefPart ? 'Cake refund progress' : 'Refund progress',
		lines,
		due: money(r.target_minor),
		returnedSoFar: money(r.succeeded_minor),
		stateWord: STATE_WORD[r.state] ?? 'requested',
		agreed: r.reason === 'chef_exception'
	};
}

export interface CancelledStory {
	title: string;
	/** Why this amount, in one sentence (none when the reason is unknown). */
	why?: string;
}

/**
 * The title and reason sentence of a cancelled order with a refund (state
 * 13), for the cancellations this page knows; undefined for others (the
 * order page's own wording applies).
 */
export function cancelledStory(order: Order): CancelledStory | undefined {
	const c = order.cancellation;
	const rate = c?.rate ?? order.refund?.policy_rate ?? null;
	const delivery =
		deliveryPaid(order) || (order.refund ? DELIVERY_PENDING(order.refund) > 0 : false);
	switch (order.cancel_reason) {
		case 'customer_cancelled':
			if (!order.refund && order.payment_requirement === 'none') {
				return {
					title: 'Your order is cancelled',
					why: 'You cancelled your order. It cost nothing, so there’s nothing to refund.'
				};
			}
			return {
				title: 'Your order is cancelled',
				why:
					rate === 100
						? 'You cancelled before the full-refund deadline, so you get everything you paid back.'
						: rate === 50
							? delivery
								? 'You cancelled after the full-refund deadline, so you get 50% of everything you paid, cakes and delivery, rounded in your favour.'
								: 'You cancelled after the full-refund deadline, so you get 50% of what you paid, rounded in your favour.'
							: undefined
			};
		case 'chef_cancelled':
			return {
				title: 'We had to cancel your order',
				why: 'We’re sorry: we can’t make your cake for this date, so we’ve cancelled your order. You get 100% of what you paid back.'
			};
		case 'late_payment_returned':
			return {
				title: 'Your order is cancelled · full refund',
				why:
					c?.requested_by === 'chef'
						? 'The date couldn’t be agreed, so you get everything you paid back.'
						: 'You chose not to go ahead with the date, so you get everything you paid back.'
			};
		case 'delivery_not_possible':
			return {
				title: 'Your order is cancelled · full refund',
				why: delivery
					? 'We couldn’t deliver your cake and you chose a full refund: everything you paid, cakes and delivery, comes back.'
					: 'We couldn’t deliver your cake and you chose a full refund, so you get everything you paid back.'
			};
	}
	return undefined;
}

// ---------------------------------------------------------------- time change (state 18)

/** The same-day rule above the time list (state 18), with the API's six-hour limit when known. */
export function moveIntro(moveUntil: string | null | undefined): string {
	const until = moveUntil ? deadlineLabel(moveUntil) : undefined;
	return until
		? `You can move to another time on the same day until ${until} (6 hours before your current time). For another date, please contact the chef.`
		: 'You can move to another time on the same day until 6 hours before your current time. For another date, please contact the chef.';
}

export interface MoveOption {
	id: string;
	label: string;
	note: string;
	current: boolean;
	disabled: boolean;
	/** The customer's chosen time that was refused as no longer available (struck through). */
	gone: boolean;
}

export interface MoveView {
	status: 'open' | 'too_late' | 'pending' | 'closed' | 'not_eligible';
	legend: string;
	/** The six-hour limit, e.g. "05:00 (London time) on Thu 22 October". */
	until?: string;
	/** The explanation above the list. */
	intro: string;
	/** Why nothing can be chosen now, when that is so. */
	note?: string;
	options: MoveOption[];
	currentId: string;
	/** Something can be chosen and moved to. */
	canMove: boolean;
}

/**
 * The move choices as a radio list: the current time (marked) and every
 * other start of the day the API offers; while the six-hour cutoff has
 * passed, the day's later times are shown disabled. A time refused as no
 * longer available is kept struck through only when it was the customer's
 * choice (gone); other gone times are dropped (Q15).
 */
export function moveView(c: MoveChoices, gone?: string): MoveView {
	const status = (['open', 'too_late', 'pending', 'closed', 'not_eligible'] as const).find(
		(s) => s === c.day_status
	);
	const st = status ?? 'not_eligible';
	const until = c.move_until ? deadlineLabel(c.move_until) : undefined;
	const asOf = Date.parse(c.as_of);
	const options: MoveOption[] = [];
	for (const s of c.slots) {
		const current = s.id === c.current_slot.id || s.reason_code === 'current_slot';
		const available = st === 'open' && s.availability === 'available';
		const lost = !current && !available && s.id === gone;
		const later = Date.parse(s.starts_at) > asOf;
		if (!(current || available || lost || (st === 'too_late' && later))) continue;
		options.push({
			id: s.id,
			label: timeRange(s.starts_at, s.ends_at),
			note: current
				? 'Your current time'
				: lost
					? 'No longer available'
					: available
						? 'Available'
						: 'Can’t be chosen now',
			current,
			disabled: !current && !available,
			gone: lost
		});
	}
	if (!options.some((o) => o.current)) {
		options.unshift({
			id: c.current_slot.id,
			label: timeRange(c.current_slot.starts_at, c.current_slot.ends_at),
			note: 'Your current time',
			current: true,
			disabled: false,
			gone: false
		});
		options.sort((a, b) => a.label.localeCompare(b.label));
	}
	const canMove = st === 'open' && options.some((o) => !o.current && !o.disabled);
	const chef = 'If you need a different time, please contact the chef.';
	const note = {
		open: canMove ? undefined : `There are no other times left on this day. ${chef}`,
		too_late: `Times can be changed online until 6 hours before your current time${until ? ` (${until})` : ''}, and that has passed. ${chef}`,
		pending: 'A change to your order is being processed. Please wait a moment, then try again.',
		closed: `No other times can be chosen on this day. ${chef}`,
		not_eligible: `This order’s time can’t be changed online. ${chef}`
	}[st];
	return {
		status: st,
		legend: `${c.fulfillment_mode === 'customer_courier' ? 'Courier collection time' : 'Collection time'} on ${shortDate(c.date)} · London time`,
		until,
		intro: moveIntro(c.move_until),
		note,
		options,
		currentId: c.current_slot.id,
		canMove
	};
}

export type MoveNext =
	| 'reload' // keep the old booking; show fresh choices
	| 'refresh' // read the order again (and the choices)
	| 'retry' // the same move may be sent again (same key)
	| 'none';

/** A refused move in the customer's words; the booking is unchanged in every case. */
export function moveProblemFor(err: unknown): { message: string; next: MoveNext; gone?: boolean } {
	if (err instanceof ApiError) {
		switch (err.code) {
			case 'SLOT_UNAVAILABLE':
				return {
					message:
						'That time is no longer available. Your booking hasn’t changed: please choose another time.',
					next: 'reload',
					gone: true
				};
			case 'MOVE_TOO_LATE':
				return {
					message:
						'It’s now less than 6 hours before your time, so it can’t be changed online. Your booking hasn’t changed.',
					next: 'reload'
				};
			case 'MOVE_NOT_ELIGIBLE':
				return {
					message:
						'This order’s time can no longer be changed online. Your booking hasn’t changed.',
					next: 'refresh'
				};
			case 'STALE_VERSION':
			case 'CONFLICT':
				return {
					message:
						'Your order changed while this was open. Your booking hasn’t changed: please check the times and try again.',
					next: 'refresh'
				};
			case 'NOT_FOUND':
				return {
					message:
						'This order’s time can’t be changed from this browser. Open it in the tab where you ordered, or sign in if you ordered with your account.',
					next: 'none'
				};
		}
		if (err.status === 401) {
			return { message: 'Your session has ended. Please sign in again to continue.', next: 'none' };
		}
		if (err.status === 0 || err.retryable || err.status >= 500) {
			return {
				message:
					'We couldn’t reach our service just now. Your booking hasn’t changed. Please try again.',
				next: 'retry'
			};
		}
	}
	return {
		message: 'Something went wrong. Your booking hasn’t changed. Please try again, or contact us.',
		next: 'retry'
	};
}

/**
 * What the order showed before this page asked for a change: the latest
 * command's request time (and the version). Only an outcome of a newer
 * command is this page's own (the order still carries the previous move or
 * cancellation until it is read again).
 */
export interface ChangeBaseline {
	requestedAt: string | null;
	version: number;
}

export function moveBaseline(order: Order): ChangeBaseline {
	return { requestedAt: order.appointment_change?.requested_at ?? null, version: order.version };
}

export function cancelBaseline(order: Order): ChangeBaseline {
	return { requestedAt: order.cancellation?.requested_at ?? null, version: order.version };
}

/** Whether a command requested at requestedAt is newer than the baseline's. */
export function newer(
	requestedAt: string | null | undefined,
	order: Order,
	b: ChangeBaseline
): boolean {
	return Boolean(requestedAt) && requestedAt !== b.requestedAt && order.version > b.version;
}

/** The outcome of this page's own move, once the API reports it (moved or refused). */
export function moveOutcomeSince(
	order: Order,
	baseline: ChangeBaseline
): { ok: boolean; message: string } | undefined {
	return newer(order.appointment_change?.requested_at, order, baseline)
		? moveOutcome(order)
		: undefined;
}

/**
 * The outcome of this page's own cancellation: refused (the order changed
 * meanwhile) or cancelled. An earlier command's outcome never counts.
 */
export function cancelOutcomeSince(
	order: Order,
	baseline: ChangeBaseline
): 'refused' | 'cancelled' | undefined {
	if (order.lifecycle === 'cancelled') return 'cancelled';
	const c = order.cancellation;
	if (!c || !newer(c.requested_at, order, baseline)) return undefined;
	if (c.state === 'refused') return 'refused';
	return c.state === 'cancelled' ? 'cancelled' : undefined;
}

/** A refreshed preview at the same rate whose amount or paid basis differs from the one shown. */
export function amountChanged(
	seen: CancellationPreview | null | undefined,
	fresh: CancellationPreview
): boolean {
	return Boolean(
		seen &&
		fresh.rate === seen.rate &&
		(fresh.amount_minor !== seen.amount_minor || fresh.paid_minor !== seen.paid_minor)
	);
}

/** The latest move's outcome as the order reports it (moved or refused). */
export function moveOutcome(order: Order): { ok: boolean; message: string } | undefined {
	const ch = order.appointment_change;
	if (!ch || ch.kind !== 'pickup_move' || ch.state === 'processing') return undefined;
	if (ch.state === 'moved') {
		return {
			ok: true,
			message: `Your time is now ${timeRange(ch.to.starts_at, ch.to.ends_at)} on ${shortDate(ch.to.date)}. Moving your time doesn’t change your refund deadline.`
		};
	}
	const why =
		ch.reason === 'slot_unavailable'
			? 'that time is no longer available.'
			: ch.reason === 'too_late'
				? 'it’s now less than 6 hours before your time.'
				: 'your order changed while we were moving it.';
	return { ok: false, message: `We couldn’t move your time: ${why} Your booking is unchanged.` };
}
