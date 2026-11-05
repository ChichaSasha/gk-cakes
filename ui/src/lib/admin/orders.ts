// Staff order reads (api/README "Staff order reads", migration 0029) and the
// view model of the chef's Orders list, from the Creator-approved GK-018
// r03b2 sketch (draft PR #15, head 5df924b; approved on the GK-018 card
// 2026-11-05). Read only: no command is sent from here. Redaction is the
// API's: technical support gets no money, fulfilment no money or attention
// flags (hidden keys are absent).
import { apiGet } from '../api';
import { addDays, formatMoney, timeRange } from '../checkout/format';
import type { StaffApi } from './catalogue';

export type StaffView = 'chef' | 'technical_support' | 'fulfillment';

export type Lifecycle =
	| 'awaiting_payment'
	| 'confirmation_pending'
	| 'paid_awaiting_agreement'
	| 'confirmed'
	| 'in_production'
	| 'ready'
	| 'completed'
	| 'cancellation_pending'
	| 'cancelled';

export interface Production {
	state: 'not_scheduled' | 'scheduled' | 'started' | string;
	scheduled_at: string | null;
	started_at: string | null;
	observed_at: string | null;
	/** manual = started by staff; timer = the automatic start. */
	source: 'manual' | 'timer' | null | string;
}

export interface StaffOrderItem {
	line_no: number;
	variant_id: string;
	lot_id: string | null;
	recipe_version_id: string;
	cake_name: string;
	flavour: string;
	size_code: string;
	quantity: number;
}

export interface StaffRefund {
	state: 'requested' | 'pending' | 'partly_returned' | 'returned' | 'needs_attention' | string;
	target_minor: number;
	succeeded_minor: number;
	in_progress_minor?: number;
	currency?: string;
}

export type AttentionFlag =
	| 'refund_needs_attention'
	| 'payment_unknown'
	| 'payment_mismatch'
	| 'paid_awaiting_agreement'
	| 'refund_quarantine'
	| string;

/** One list row; the keys after `production` depend on the caller's view. */
export interface StaffOrderRow {
	id: string;
	version: number;
	lifecycle: Lifecycle | string;
	raw_lifecycle: string;
	date: string;
	slot: { id: string; starts_at: string; ends_at: string };
	fulfillment_mode: 'collection' | 'customer_courier' | 'gk_delivery' | string;
	placed_at: string;
	items: StaffOrderItem[];
	customer_full_name: string;
	cancel_reason: string | null;
	production: Production;
	// chef and technical support
	attention?: AttentionFlag[];
	needs_attention?: boolean;
	// chef only
	contact_email?: string;
	payment_requirement?: 'full' | 'none';
	total_minor?: number;
	currency?: string;
	payment_state?: string | null;
	paid_at?: string | null;
	free_confirmed_at?: string | null;
	refund?: StaffRefund | null;
}

/** Most pages one list read follows (100 orders each). */
export const MAX_ORDER_PAGES = 10;
const PAGE = 100;
const DATE = /^\d{4}-\d{2}-\d{2}$/;
/** The API's longest range (London handover days, inclusive). */
export const MAX_RANGE_DAYS = 93;

/** Every order with a handover day from `from` to `to` (inclusive), by slot start. */
export async function staffOrders(
	api: StaffApi,
	range: { from: string; to: string }
): Promise<StaffOrderRow[]> {
	if (!DATE.test(range.from) || !DATE.test(range.to)) throw new Error('not a date range');
	const all: StaffOrderRow[] = [];
	let cursor: string | undefined;
	for (let i = 0; i < MAX_ORDER_PAGES; i++) {
		const page = await apiGet<{ items: StaffOrderRow[]; next_cursor?: string }>(
			'/api/v1/admin/orders',
			{
				baseUrl: api.baseUrl,
				token: api.token,
				fetch: api.fetch,
				query: { from: range.from, to: range.to, limit: PAGE, cursor }
			}
		);
		all.push(...page.items);
		if (!page.next_cursor) return all;
		cursor = page.next_cursor;
	}
	throw new Error(`more than ${MAX_ORDER_PAGES * PAGE} orders in this range`);
}

/** The caller's most privileged capability, as the API's view (chef, then support, then fulfilment). */
export function viewOf(capabilities: string[]): StaffView | null {
	if (capabilities.includes('chef')) return 'chef';
	if (capabilities.includes('technical_support')) return 'technical_support';
	if (capabilities.includes('fulfillment')) return 'fulfillment';
	return null;
}

// ------------------------------------------------------------ periods

export type Period = 'upcoming' | 'this' | 'next' | 'past';

function weekStart(date: string): string {
	const day = new Date(`${date}T12:00:00Z`).getUTCDay(); // 0 Sunday
	return addDays(date, -((day + 6) % 7));
}

/** A London calendar day as Thu 22 Oct (the sketch's day labels). */
export function shortDay(date: string): string {
	return new Intl.DateTimeFormat('en-GB', {
		timeZone: 'Europe/London',
		weekday: 'short',
		day: 'numeric',
		month: 'short'
	}).format(new Date(`${date}T12:00:00Z`));
}

/** "Mon 19 – Sun 25 Oct" (the sketch's week labels). */
function weekLabel(monday: string): string {
	const sunday = addDays(monday, 6);
	const [mw, md, mm] = shortDay(monday).split(' ');
	const [sw, sd, sm] = shortDay(sunday).split(' ');
	return mm === sm ? `${mw} ${md} – ${sw} ${sd} ${sm}` : `${mw} ${md} ${mm} – ${sw} ${sd} ${sm}`;
}

/**
 * The "When" choices and the handover days each reads. The API reads at most
 * 93 days at a time, so "From today" and "Before today" cover 93 days and
 * the sketch's "All dates" is not offered.
 */
export function periods(
	today: string
): Record<Period, { label: string; from: string; to: string }> {
	const monday = weekStart(today);
	const next = addDays(monday, 7);
	return {
		upcoming: { label: 'From today', from: today, to: addDays(today, MAX_RANGE_DAYS - 1) },
		this: { label: `This week (${weekLabel(monday)})`, from: monday, to: addDays(monday, 6) },
		next: { label: `Next week (${weekLabel(next)})`, from: next, to: addDays(next, 6) },
		past: { label: 'Before today', from: addDays(today, -MAX_RANGE_DAYS), to: addDays(today, -1) }
	};
}

// ------------------------------------------------------- filter chips

export const STATUS_FILTERS = [
	'all',
	'attention',
	'payment',
	'late',
	'confirmed',
	'production',
	'ready',
	'cancelled'
] as const;

export type StatusFilter =
	'all' | 'attention' | 'payment' | 'late' | 'confirmed' | 'production' | 'ready' | 'cancelled';

/**
 * The sketch's status chips that the API can answer. "Needs action"
 * (collection details not sent) needs email data the API doesn't give yet;
 * "Needs a manager" is for chef and technical support only (fulfilment gets
 * no attention flags).
 */
export function statusFilters(view: StaffView): [StatusFilter, string][] {
	const all: [StatusFilter, string][] = [
		['all', 'All'],
		['attention', 'Needs a manager'],
		['payment', 'Waiting for payment or confirming'],
		['late', 'Paid late'],
		['confirmed', 'Paid · not started'],
		['production', 'In production'],
		['ready', 'Ready'],
		['cancelled', 'Cancelled']
	];
	return view === 'fulfillment' ? all.filter(([k]) => k !== 'attention') : all;
}

export function matchesStatus(o: StaffOrderRow, f: StatusFilter): boolean {
	switch (f) {
		case 'all':
			return true;
		case 'attention':
			return isManagerCase(o);
		case 'payment':
			return o.lifecycle === 'awaiting_payment' || o.lifecycle === 'confirmation_pending';
		case 'late':
			return o.lifecycle === 'paid_awaiting_agreement';
		case 'confirmed':
			return o.lifecycle === 'confirmed';
		case 'production':
			return o.lifecycle === 'in_production';
		case 'ready':
			return o.lifecycle === 'ready';
		case 'cancelled':
			return o.lifecycle === 'cancelled' || o.lifecycle === 'cancellation_pending';
	}
}

/**
 * A case for a manager (the sketch's "Needs a manager" / money cases): a
 * refund needing attention, a payment that doesn't match, or a refund made
 * outside GK Cakes. Paid late is the chef's own decision (its own chip) and
 * an unknown payment is still being checked (r03b2 Needs attention), so the
 * API's broader needs_attention is not used.
 */
const MANAGER_FLAGS = ['refund_needs_attention', 'payment_mismatch', 'refund_quarantine'];
export function isManagerCase(o: StaffOrderRow): boolean {
	return (o.attention ?? []).some((f) => MANAGER_FLAGS.includes(f));
}

/**
 * A released order nobody paid for. Sasha's answer to r03 Q5: hidden from
 * the list by default, with a filter to show them.
 */
export function isReleasedUnpaid(o: StaffOrderRow, view: StaffView): boolean {
	return statusOf(o, view).released;
}

/** The sketch's "How" choices, by the API's fulfilment mode. */
export const MODES: Record<string, string> = {
	collection: 'Collect · St Katharine Docks',
	customer_courier: 'Customer’s courier collects',
	gk_delivery: 'GK delivery'
};

/** Client-side search over the loaded rows: the customer's name or the order ID (no server search, K09). */
export function matchesSearch(o: StaffOrderRow, term: string): boolean {
	const t = term.trim().toLowerCase();
	if (!t) return true;
	return o.customer_full_name.toLowerCase().includes(t) || o.id.toLowerCase().startsWith(t);
}

// --------------------------------------------------------- the row

export type Stage =
	| 'awaiting'
	| 'confirming'
	| 'agreement'
	| 'confirmed'
	| 'preparing'
	| 'making'
	| 'ready'
	| 'done'
	| 'cancelling'
	| 'cancelled';

/** The sketch's stage of an order from the API's visible lifecycle and production. */
export function stageOf(o: StaffOrderRow): Stage {
	switch (o.lifecycle) {
		case 'awaiting_payment':
			return 'awaiting';
		case 'confirmation_pending':
			return 'confirming';
		case 'paid_awaiting_agreement':
			return 'agreement';
		case 'confirmed':
			return 'confirmed';
		case 'in_production':
			return o.production.source === 'manual' ? 'making' : 'preparing';
		case 'ready':
			return 'ready';
		case 'completed':
			return 'done';
		case 'cancellation_pending':
			return 'cancelling';
		default:
			return 'cancelled';
	}
}

const STAGES: Record<Stage, string> = {
	awaiting: 'Waiting for payment',
	confirming: 'Paid · confirming the order',
	agreement: 'Paid late · decision pending',
	confirmed: 'Paid · automatic start later',
	preparing: 'In production (automatic) · not started by you',
	making: 'You started making',
	ready: 'Ready for handover',
	done: 'Handed over',
	cancelling: 'Cancelling · not confirmed yet',
	cancelled: 'Cancelled'
};

const WAITING: Record<string, string> = {
	created: 'Waiting for payment · page being prepared',
	pending: 'Waiting for payment · in progress',
	unknown: 'Payment result unknown',
	failed: 'Waiting for payment · last try failed',
	cancelled: 'Waiting for payment · last try not completed'
};

const RELEASED: Record<string, string> = {
	payment_not_completed: 'Not paid · released',
	free_not_confirmed: 'Free order not confirmed · released'
};

/** The status pill: its stage (for the colour) and the sketch's words for this viewer. */
export function statusOf(
	o: StaffOrderRow,
	view: StaffView
): { stage: Stage; text: string; released: boolean } {
	const stage = stageOf(o);
	const free = o.payment_requirement === 'none';
	if (stage === 'awaiting') {
		if (o.attention?.includes('payment_mismatch')) {
			return { stage, text: 'Payment doesn’t match · held', released: false };
		}
		if (o.attention?.includes('payment_unknown')) {
			return { stage, text: WAITING.unknown, released: false };
		}
		if (free) return { stage, text: 'Free · not confirmed yet', released: false };
		// Only the chef's rows carry the payment state; others get the plain stage.
		if (view !== 'chef') return { stage, text: STAGES.awaiting, released: false };
		const p = o.payment_state;
		return { stage, text: (p && WAITING[p]) || 'Waiting for payment · link sent', released: false };
	}
	if (stage === 'confirming' && free) {
		return { stage, text: 'Free · confirming the order', released: false };
	}
	if (stage === 'agreement') {
		return {
			stage,
			text: view === 'chef' ? 'Paid late · your decision' : 'Paid late · Elina decides',
			released: false
		};
	}
	if (stage === 'cancelled' && o.cancel_reason && RELEASED[o.cancel_reason]) {
		if (view !== 'chef' && o.cancel_reason === 'payment_not_completed') {
			// Without the chef's money keys the page can't tell "not paid" from a
			// late payment being returned in full: say only what it knows.
			return { stage, text: 'Released', released: false };
		}
		// Money that arrived for a released order is returned in full (r03b2).
		const paid = o.refund != null || o.payment_state === 'succeeded';
		return paid
			? { stage, text: 'Cancelled · full return', released: false }
			: { stage, text: RELEASED[o.cancel_reason], released: true };
	}
	return { stage, text: STAGES[stage], released: false };
}

const SHORT = new Intl.DateTimeFormat('en-GB', {
	timeZone: 'Europe/London',
	weekday: 'short',
	day: 'numeric',
	month: 'short'
});
const short = (iso: string) => SHORT.format(new Date(iso));

/** The Production column (the sketch's wording; the automatic start is the API's timer). */
export function productionText(o: StaffOrderRow, view: StaffView, now: Date): string {
	const stage = stageOf(o);
	const p = o.production;
	if (stage === 'cancelled' || stage === 'cancelling') return '—';
	if (stage === 'agreement')
		return view === 'chef' ? 'Held · your decision' : 'Held · Elina decides';
	if (p.state === 'started' && p.started_at) {
		return p.source === 'manual'
			? `Started ${short(p.started_at)}`
			: `Automatic since ${short(p.started_at)}`;
	}
	if (p.state === 'scheduled' && p.scheduled_at) {
		const past = new Date(p.scheduled_at) < now;
		return `${past ? 'Automatic since' : 'Automatic from'} ${short(p.scheduled_at)}`;
	}
	if (stage === 'awaiting' || stage === 'confirming') return 'After payment';
	return '—';
}

/** The Money column (chef only; the API sends money to the chef alone). */
export function moneyText(o: StaffOrderRow): string {
	const cur = o.currency ?? 'GBP';
	const m = (n: number) => formatMoney(n, cur);
	const stage = stageOf(o);
	if (o.payment_requirement === 'none' && stage !== 'cancelled' && stage !== 'cancelling') {
		return 'Free';
	}
	if (stage === 'awaiting') {
		if (o.attention?.includes('payment_mismatch')) return 'Doesn’t match · held';
		const labels: Record<string, string> = {
			created: 'Payment page being prepared',
			pending: 'Payment in progress',
			unknown: 'Payment result unknown',
			failed: 'Payment failed · may try again within the 15-minute hold',
			cancelled: 'Payment not completed'
		};
		return (o.payment_state && labels[o.payment_state]) || 'Link sent · not paid';
	}
	if (stage === 'confirming') {
		return o.total_minor != null
			? `${m(o.total_minor)} received · confirming`
			: 'Received · confirming';
	}
	if (stage === 'cancelled' || stage === 'cancelling') {
		// Only the server's amounts, as they are (no amount is worked out here).
		const r = o.refund;
		if (!r) return stage === 'cancelling' ? 'Cancelling' : 'Nothing taken';
		if (r.state === 'needs_attention') return 'Refund needs a manager';
		if (r.state === 'pending') return `Refund ${m(r.target_minor)} · not confirmed`;
		if (r.state === 'requested') return `Refund ${m(r.target_minor)} owed · nothing sent yet`;
		if (r.state === 'partly_returned') {
			return `Partly refunded · ${m(r.succeeded_minor)} of ${m(r.target_minor)}`;
		}
		return `Refunded ${m(r.succeeded_minor)}`;
	}
	return o.total_minor != null ? m(o.total_minor) : '';
}

/** The Cake column: each line's cake (with quantity when more than one). */
export function cakeText(o: StaffOrderRow): string {
	return o.items
		.map((i) => (i.quantity > 1 ? `${i.quantity} × ${i.cake_name}` : i.cake_name))
		.join(', ');
}

/** The Handover column's second line, e.g. 11:00–12:00. */
export function slotText(o: StaffOrderRow): string {
	return timeRange(o.slot.starts_at, o.slot.ends_at);
}

/** An order's short label in the list (the API has no order number yet): its ID's first 8 characters. */
export function shortId(id: string): string {
	return id.slice(0, 8);
}
