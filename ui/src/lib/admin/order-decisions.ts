// The chef's date and delivery decisions on the order page (GK-031 slice 2,
// in the visual language of the Creator-approved GK-018 r03b2 sketch):
// late-payment offers and the r03b2 paid-late decision (api/README "Late-
// payment agreement on the order page", 0034, and "Production start, late
// payment and allocation"), the GK delivery quote and free-delivery waiver
// (0035), "GK can't deliver" (0036), a failed delivery (0037) and the chef's
// appointment change (0031), which also sets a redelivery time. The customer
// side of each is the Creator-approved GK-017 r02b wording (states 10, 14,
// 15, 19). Every command is the chef's (K00); fulfilment and technical
// support only read. Money is the server's integer pence: an entered price
// is parsed to pence as text, never through a float. Eligibility mirrors the
// README at the page's clock; the API still decides and its refusals are
// shown in plain words.
import { ApiError, apiGet, apiPost } from '../api';
import { addDays, formatMoney, londonTime, londonToday, timeRange } from '../checkout/format';
import type { StaffApi } from './catalogue';
import {
	dayShort,
	fulfillmentState,
	refusalOf,
	settled,
	unresolvedChange,
	when,
	type Accepted,
	type Refusal,
	type Slot,
	type StaffOrderDetail
} from './order-detail';
import type { StaffView } from './orders';

// ------------------------------------------------------------ the read

/** One late-payment offer as the chef and technical support see it (0034). */
export interface StaffLatePaymentOffer {
	id: string;
	kind: 'requested' | 'other' | string;
	date: string;
	slot_id: string;
	start: string;
	end: string;
	offered_at: string;
	accept_until: string;
	state:
		| 'open'
		| 'withdrawn'
		| 'processing'
		| 'accepted'
		| 'declined'
		| 'lapsed'
		| 'refused'
		| 'closed'
		| string;
	answer: {
		id: string;
		answer: 'accept' | 'decline' | string;
		received_at: string;
		access: 'account' | 'guest_link' | string;
		command_id: string | null;
		lapsed: boolean;
	} | null;
}

/** One quote of the delivery history (0035), oldest first. */
export interface DeliveryQuoteRecord {
	id: string;
	order_version: number;
	quoted_at: string;
	pay_by: string;
	current: boolean;
	/** Chef only. */
	amount_minor?: number;
	currency?: string;
	note?: string | null;
	waiver: {
		id: string;
		at: string;
		distance_m?: number;
		distance_source?: string;
		eligibility_approved?: boolean;
	} | null;
	payments: {
		collection_id: string;
		state: string;
		outcome: string | null;
		reason: string | null;
	}[];
}

export interface ChoiceTarget {
	fulfillment_mode: string;
	date: string;
	id: string;
	starts_at: string;
	ends_at: string;
}

/** The Q5 choice (0036) as the chef sees it (support: without money or reason). */
export interface StaffDeliveryChoice {
	open: boolean;
	reason: 'not_possible' | 'pay_by_passed' | string | null;
	opens_at?: string | null;
	pending: boolean;
	chosen?: {
		choice: 'collection' | 'customer_courier' | 'full_refund' | string;
		state: 'processing' | 'chosen' | 'cancelled' | 'refused' | string;
		refusal_reason?: string | null;
		requested_at?: string;
		to?: ChoiceTarget | null;
		delivery_refund_minor?: number;
	} | null;
	not_possible?: { id?: string; at: string; reason?: string | null } | null;
}

/** The latest failed delivery (0037) as the chef sees it. */
export interface StaffDeliveryFailure {
	cause: 'our_side' | 'nobody_available' | string;
	at: string;
	state: string;
	open: boolean;
	pending: boolean;
	delivery_paid_minor?: number | null;
	chosen?: {
		choice: 'redelivery' | 'collection' | string;
		state: 'processing' | 'chosen' | 'refused' | string;
		refusal_reason?: string | null;
		requested_at?: string;
		to?: ChoiceTarget | null;
		delivery_refund_minor?: number;
	} | null;
	redelivery_time_set?: boolean;
	cake_refund_minor?: number;
	delivery_kept_minor?: number;
	history?: { cause: string; at?: string; recorded_at?: string; reason?: string | null }[];
}

/** The delivery block (0035) with the fields this slice reads. */
export interface DeliveryBlock {
	state: string;
	quote?: {
		id: string;
		quoted_at: string;
		amount_minor?: number;
		currency?: string;
		pay_by?: string;
	} | null;
	payable?: boolean;
	paid_minor?: number;
	waived?: { at: string };
	unmatched_minor?: number;
}

/** The late_payment block (0034); technical support: without money. */
export interface LatePaymentBlock {
	state: 'waiting' | 'offered' | 'processing' | string;
	offer: {
		id?: string;
		kind?: string;
		date: string;
		start: string;
		end: string;
		accept_until: string;
		acceptable?: boolean;
	} | null;
	answer?: {
		offer_id: string;
		answer: string;
		received_at: string;
		state: 'processing' | 'lapsed' | 'refused' | string;
		reason: string | null;
	} | null;
	paid_minor?: number;
	currency?: string;
}

/** The order as the page reads it (StaffOrderDetail carries this slice's keys). */
export type DecisionOrder = StaffOrderDetail;

const asDetail = (o: DecisionOrder): StaffOrderDetail => o;
const money = (o: DecisionOrder, minor: number, currency?: string) =>
	formatMoney(minor, currency ?? o.currency ?? 'GBP');
const slotText = (date: string, start: string, end: string) =>
	`${dayShort(date)}, ${timeRange(start, end)}`;

// --------------------------------------------------------- input rules

/** One line, 1–max characters, no control characters (the API's oneLine). */
export function lineProblem(text: string, max: number, what: string): string {
	const t = text.trim();
	if (!t) return `Say ${what}.`;
	if ([...t].length > max) return `Keep it to ${max} characters.`;
	const control = (c: string) => {
		const n = c.codePointAt(0) ?? 0;
		return n < 0x20 || (n >= 0x7f && n <= 0x9f) || n === 0x2028 || n === 0x2029;
	};
	if ([...t].some(control)) return 'Keep it to one line.';
	return '';
}

/** An optional one-line reason: '' when empty (not sent), else as lineProblem. */
export function optionalLineProblem(text: string, max: number): string {
	return text.trim() ? lineProblem(text, max, 'it') : '';
}

/** The API's delivery quote bounds (K03 "from £100"; Stripe's GBP maximum). */
export const MIN_DELIVERY_MINOR = 10000;
export const MAX_DELIVERY_MINOR = 99999999;

/**
 * A price typed in pounds ("120", "£120", "120.50", "1,200") as whole pence,
 * parsed as text (never a float). Pence must be two digits.
 */
export function parsePence(text: string): { minor: number } | { error: string } {
	const t = text.trim().replace(/^£\s*/, '');
	if (!t) return { error: 'Enter the delivery price.' };
	const m = /^(\d{1,3}(?:,\d{3})+|\d+)(?:\.(\d*))?$/.exec(t);
	if (!m) return { error: 'Enter a price in pounds, for example 120 or 120.50.' };
	const pence = m[2];
	if (pence !== undefined && pence.length !== 2) {
		return { error: 'Give pence as two digits, for example 120.50.' };
	}
	const pounds = m[1].replace(/,/g, '').replace(/^0+(?=\d)/, '');
	if (pounds.length > 7) return { error: 'That price is too large.' };
	const minor = Number(pounds) * 100 + Number(pence ?? '0');
	return { minor };
}

/** The quote price's problem ('' when it can be sent). */
export function quotePriceProblem(text: string): string {
	const p = parsePence(text);
	if ('error' in p) return p.error;
	if (p.minor < MIN_DELIVERY_MINOR) {
		return 'GK delivery starts from £100. Enter a price of at least £100.';
	}
	if (p.minor > MAX_DELIVERY_MINOR) return 'A delivery price can be at most £999,999.99.';
	return '';
}

export const MAX_DISTANCE_M = 1_000_000;

/**
 * A measured driving distance in miles or kilometres (up to three decimals)
 * as whole metres, by integer arithmetic on the typed digits.
 */
export function parseDistance(
	text: string,
	unit: 'mi' | 'km'
): { metres: number } | { error: string } {
	const t = text.trim();
	const m = /^(\d{1,4})(?:\.(\d{1,3}))?$/.exec(t);
	if (!t) return { error: 'Enter the measured driving distance.' };
	if (!m) return { error: 'Enter the distance as a number, for example 5.1.' };
	const thousandths = Number(m[1]) * 1000 + Number((m[2] ?? '').padEnd(3, '0'));
	// 1 mile = 1609.344 m exactly; thousandths × 1609344 / 10^6, rounded half up.
	const metres =
		unit === 'km' ? thousandths : Math.floor((thousandths * 1609344 + 500000) / 1000000);
	if (metres < 1) return { error: 'The distance must be more than 0.' };
	if (metres > MAX_DISTANCE_M) return { error: 'That distance is more than 1,000 km.' };
	return { metres };
}

/** "8.2 km (8,208 m)". */
export function distanceText(metres: number): string {
	const km = Math.floor(metres / 100) / 10;
	return `${km.toLocaleString('en-GB')} km (${metres.toLocaleString('en-GB')} m)`;
}

/** The London offset of an instant in minutes (e.g. 60 in summer). */
function londonOffset(ms: number): number {
	const p: Record<string, string> = {};
	for (const x of new Intl.DateTimeFormat('en-GB', {
		timeZone: 'Europe/London',
		year: 'numeric',
		month: '2-digit',
		day: '2-digit',
		hour: '2-digit',
		minute: '2-digit',
		hourCycle: 'h23'
	}).formatToParts(new Date(ms))) {
		p[x.type] = x.value;
	}
	const asUtc = Date.UTC(+p.year, +p.month - 1, +p.day, +p.hour, +p.minute);
	return Math.round((asUtc - Math.floor(ms / 60000) * 60000) / 60000);
}

/** A datetime-local value read as London time ("2026-10-29T10:31") → ISO instant, or null. */
export function londonLocalToIso(local: string): string | null {
	const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(local);
	if (!m) return null;
	const wall = Date.UTC(+m[1], +m[2] - 1, +m[3], +m[4], +m[5]);
	if (Number.isNaN(wall)) return null;
	let ms = wall - londonOffset(wall) * 60000;
	ms = wall - londonOffset(ms) * 60000;
	return new Date(ms).toISOString().replace('.000Z', 'Z');
}

/** An instant as a datetime-local value in London time. */
export function isoToLondonLocal(iso: string | Date): string {
	const ms = new Date(iso).getTime();
	const wall = new Date(ms + londonOffset(ms) * 60000).toISOString();
	return wall.slice(0, 16);
}

export const CHANNELS: [string, string][] = [
	['instagram', 'Instagram message'],
	['whatsapp', 'WhatsApp'],
	['email', 'Email'],
	['phone', 'Phone call'],
	['in_person', 'In person'],
	['other', 'Other']
];

export interface AgreementInput {
	channel: string;
	/** datetime-local, London time. */
	when: string;
	evidence: string;
	exact: boolean;
}

/**
 * What is still missing from the customer's recorded agreement (r03b M2
 * "Still needed: …"); empty when complete. `after`: the earliest allowed
 * time (the payment for a late payment, the placement for a time change).
 */
export function agreementMissing(
	a: AgreementInput,
	now: Date,
	after: { at: string; label: string } | null,
	exactLabel: string
): string[] {
	const out: string[] = [];
	if (!CHANNELS.some(([v]) => v === a.channel)) out.push('how they agreed');
	const at = londonLocalToIso(a.when);
	if (!at) out.push('when they agreed');
	else if (new Date(at) > now) out.push('a time that isn’t in the future');
	else if (after && new Date(at) < new Date(after.at)) out.push(`a time after ${after.label}`);
	if (!a.evidence.trim()) out.push('what they said, or where to find it');
	else if (lineProblem(a.evidence, 200, 'it'))
		out.push('evidence on one line, up to 200 characters');
	if (!a.exact) out.push(`a tick that they agreed to exactly ${exactLabel}`);
	return out;
}

// ------------------------------------------------------- shared reading

/** The order's original handover start (the immutable anchor of pay-by and the refund deadline). */
export function originalHandover(o: DecisionOrder): string {
	const h = [...(o.appointment_history ?? [])].sort((x, y) =>
		x.received_at.localeCompare(y.received_at)
	);
	return h[0]?.from?.starts_at ?? o.slot.starts_at;
}

/** The latest dispatch's time, or null when the cake never went out. */
export function lastDispatch(o: DecisionOrder): string | null {
	const d = [...(o.fulfillment_history ?? [])].reverse().find((e) => e.action === 'dispatch');
	return d?.received_at ?? null;
}

/** A secured (moved) chef appointment change requested after `at`. */
function movedAfter(o: DecisionOrder, at: string): boolean {
	return (o.appointment_history ?? []).some((a) => a.state === 'moved' && a.received_at > at);
}

/**
 * The GK delivery is on its way (README `delivery_on_its_way`): ready, out
 * for delivery or an exception after a dispatch, no our-side failure for
 * that dispatch, not rescheduled since.
 */
export function onItsWay(o: DecisionOrder): boolean {
	if (o.fulfillment_mode !== 'gk_delivery' || o.lifecycle !== 'ready') return false;
	const fs = fulfillmentState(asDetail(o)) ?? '';
	if (fs !== 'out_for_delivery' && fs !== 'exception') return false;
	const at = lastDispatch(o);
	if (!at) return false;
	const f = o.delivery_failure;
	if (f && f.cause === 'our_side' && f.at >= at) return false;
	return !movedAfter(o, at);
}

/** Never dispatched, or dispatched and rescheduled since (waiting for a new dispatch). */
function neverDispatched(o: DecisionOrder): boolean {
	return lastDispatch(o) === null && fulfillmentState(asDetail(o)) !== 'out_for_delivery';
}

const OPEN_LIFECYCLES = ['confirmed', 'in_production', 'ready'];
const ONLY_CHEF = 'Only Elina (chef) can do this.';

// ----------------------------------------------------------- 1. late payment

export interface LatePaymentChoices {
	/** "Offer their requested time" (kind requested). */
	requested: boolean;
	/** "Offer another date…" (kind other). */
	other: boolean;
	/** The r03b2 paid-late form (the chef's /late-payment route). */
	honour: boolean;
	fullReturn: boolean;
	note: string;
}

/**
 * What the chef can do on a paid-late order (README 0034 and 0022): only
 * `paid_awaiting_agreement`, nothing while a protected change is unresolved
 * (CONFLICT). The requested time (an offer of it, or honouring it) only
 * before the original handover (HONOUR_TOO_LATE); another date and a full
 * return at any time.
 */
export function latePaymentChoices(
	o: DecisionOrder,
	view: StaffView,
	now: Date
): LatePaymentChoices {
	const none = (note = ''): LatePaymentChoices => ({
		requested: false,
		other: false,
		honour: false,
		fullReturn: false,
		note
	});
	if (o.lifecycle !== 'paid_awaiting_agreement') return none();
	if (view !== 'chef') return none('');
	if (o.late_payment?.state === 'processing' || unresolvedChange(asDetail(o))) {
		return none(
			'A decision or the customer’s answer is being processed. The choices come back if it doesn’t go through.'
		);
	}
	const beforeHandover = now < new Date(originalHandover(o));
	const open = o.late_payment?.state === 'offered' ? o.late_payment.offer : null;
	const openRequested = open?.kind === 'requested';
	return {
		requested: beforeHandover && !openRequested,
		other: true,
		honour: beforeHandover,
		fullReturn: true,
		note: beforeHandover
			? ''
			: 'Their requested time has passed, so it can’t be offered or confirmed any more. Offer another date, or return everything.'
	};
}

const OFFER_STATE: Record<string, string> = {
	open: 'waiting for the customer',
	withdrawn: 'replaced by a newer offer',
	processing: 'the customer accepted · being confirmed',
	accepted: 'accepted by the customer',
	declined: 'declined · everything is being returned',
	lapsed: 'accepted too late: that time could no longer take the order',
	refused: 'the customer’s answer couldn’t be completed',
	closed: 'closed'
};

/** The paid-late card's current-offer lines (chef: with the paid amount). */
export function latePaymentStatus(o: DecisionOrder, view: StaffView, now: Date): string[] {
	const lp = o.late_payment;
	if (o.lifecycle !== 'paid_awaiting_agreement' || !lp) return [];
	const out: string[] = [];
	const offer = lp.offer;
	if (lp.state === 'offered' && offer) {
		const kind = offer.kind === 'requested' ? 'their requested time' : 'another date';
		const acceptable = offer.acceptable ?? now < new Date(offer.accept_until);
		out.push(
			`${view === 'chef' ? 'You offered' : 'Elina offered'} ${slotText(offer.date, offer.start, offer.end)} (${kind}). Waiting for the customer’s answer on their order page${acceptable ? `: they can accept until ${when(offer.accept_until)}` : ''}.`
		);
		if (!acceptable) {
			out.push(
				'That time can no longer be accepted (it has passed). The customer can still decline, which returns everything. Offer another date.'
			);
		}
	} else if (lp.state === 'processing') {
		out.push(
			offer
				? `The customer answered the offer of ${slotText(offer.date, offer.start, offer.end)}; it’s being confirmed.`
				: 'A decision on this order is being processed.'
		);
	} else {
		out.push(
			view === 'chef'
				? 'No offer is open. The customer’s order page says the chef’s answer appears there; silence never counts as agreement.'
				: 'No offer is open yet.'
		);
	}
	const a = lp.answer;
	if (a && a.state === 'lapsed') {
		out.push(
			`The customer accepted ${when(a.received_at)}, but that time could no longer take the order, so the offer lapsed and nothing changed. Offer again.`
		);
	} else if (a && a.state === 'refused') {
		out.push(
			`The customer’s answer of ${when(a.received_at)} couldn’t be completed${a.reason ? ` (${refusedReason(a.reason)})` : ''}; nothing changed.`
		);
	}
	return out;
}

function refusedReason(r: string): string {
	const words: Record<string, string> = {
		slot_unavailable: 'the time was no longer available',
		holds_changed: 'the day place or cakes changed',
		order_changed: 'the order changed meanwhile',
		too_late: 'it was too late',
		not_eligible: 'the order couldn’t take it any more',
		not_open: 'the choice wasn’t open any more'
	};
	return words[r] ?? r.replace(/_/g, ' ');
}

/** Every offer, oldest first, for the Record (chef and technical support). */
export function offerLines(o: DecisionOrder): { at: string; text: string }[] {
	return (o.late_payment_offers ?? []).map((f) => {
		const kind = f.kind === 'requested' ? 'their requested time' : 'another date';
		const ans = f.answer
			? ` · ${f.answer.answer === 'accept' ? 'accepted' : 'declined'} on the order page ${when(f.answer.received_at)} (${f.answer.access === 'guest_link' ? 'order link' : 'account'})`
			: '';
		return {
			at: f.offered_at,
			text: `Offered ${slotText(f.date, f.start, f.end)} (${kind}) · ${OFFER_STATE[f.state] ?? f.state}${ans}`
		};
	});
}

// ------------------------------------------------- 2. delivery quote, waiver

export interface QuoteChoices {
	/** A first quote (awaiting_quote). */
	quote: boolean;
	/** Replace the current unpaid (or waived) quote. */
	replace: boolean;
	/** Waive the current quote (free-delivery code). */
	waive: boolean;
	/** Pay-by: the quote's, or the original handover − 48 h before any quote. */
	payBy: string | null;
	note: string;
}

const H = 3600 * 1000;

/**
 * Quote and waiver (README 0035): a GK delivery whose cake is visibly
 * confirmed, never dispatched, no protected change unresolved, no delivery
 * payment in flight (payment_checking), not after "GK can't deliver". A
 * quote before pay-by only and never over a paid one; a waiver needs a
 * current quote that isn't paid or waived (also after pay-by) and the
 * order's free-delivery code, which this view can't see (the API checks).
 */
export function quoteChoices(o: DecisionOrder, view: StaffView, now: Date): QuoteChoices {
	const d = o.delivery;
	const payBy =
		d?.quote?.pay_by ?? new Date(new Date(originalHandover(o)).getTime() - 48 * H).toISOString();
	const none = (note = ''): QuoteChoices => ({
		quote: false,
		replace: false,
		waive: false,
		payBy,
		note
	});
	if (o.fulfillment_mode !== 'gk_delivery' || !d || view !== 'chef') return none();
	if (!OPEN_LIFECYCLES.includes(o.lifecycle)) return none();
	if (o.delivery_choice?.not_possible || o.delivery_choice?.reason === 'not_possible') {
		return none();
	}
	if (!neverDispatched(o)) return none();
	if (unresolvedChange(asDetail(o))) {
		return none('Another change to this order is still being processed.');
	}
	if (d.state === 'payment_checking') {
		return none(
			'A delivery payment is being checked. A new price or a waiver waits until its result is known; the customer mustn’t pay again.'
		);
	}
	if (d.state === 'paid' || d.state === 'cancelled_by_choice') return none();
	const beforePayBy = now < new Date(payBy);
	const hasQuote = Boolean(d.quote) && (d.state === 'quoted' || d.state === 'pay_by_passed');
	return {
		quote: d.state === 'awaiting_quote' && beforePayBy,
		replace: (d.state === 'quoted' || d.state === 'waived') && beforePayBy,
		waive: hasQuote,
		payBy,
		note:
			!beforePayBy && d.state === 'awaiting_quote'
				? 'The pay-by time (48 hours before the original delivery time) has passed, so a quote can’t be sent. Record that GK can’t deliver, so the customer can choose another way.'
				: ''
	};
}

/** The delivery card's state lines (money for the chef only). */
export function deliveryStatus(o: DecisionOrder, view: StaffView): string[] {
	const d = o.delivery;
	if (!d) return [];
	const q = d.quote;
	const chef = view === 'chef';
	const amount = chef && q?.amount_minor != null ? `${money(o, q.amount_minor, q.currency)} ` : '';
	switch (d.state) {
		case 'awaiting_quote':
			return [
				chef
					? 'The customer asked for GK delivery. No price yet: you price each address (from £100). The cake is already booked; a quote doesn’t hold anything.'
					: 'Waiting for Elina’s delivery price.'
			];
		case 'quoted':
			return [
				`Quote ${amount}sent${q ? ` ${when(q.quoted_at)}` : ''}. Not paid yet${q?.pay_by ? `: the customer can pay on their order page until ${when(q.pay_by)} (48 hours before the original delivery time)` : ''}.`,
				'The cake can’t go out for delivery until delivery is paid or waived.'
			];
		case 'payment_checking':
			return [
				`Quote ${amount}· delivery payment being checked. The customer sees “Still checking delivery payment; do not pay again”.`
			];
		case 'paid':
			return [
				`Delivery ${chef && d.paid_minor != null ? `${money(o, d.paid_minor)} ` : ''}paid separately.`
			];
		case 'waived':
			return [`Free (free-delivery code) · waived${d.waived ? ` ${when(d.waived.at)}` : ''}.`];
		case 'pay_by_passed':
			return [
				`Quote ${amount}wasn’t paid by ${q?.pay_by ? when(q.pay_by) : 'its pay-by time'}. GK won’t deliver unpaid: the customer can choose collection, their own courier or a full refund on their order page (no deadline).`
			];
		case 'cancelled_by_choice':
			return [
				`Not delivering: the customer chose another way.${chef && d.paid_minor != null ? ` The ${money(o, d.paid_minor)} delivery payment is returned in full (see Refund).` : ''}`
			];
		default:
			return [d.state];
	}
}

/** The quote history, oldest first (chef: amounts, notes, distances; support: without). */
export function quoteLines(o: DecisionOrder, view: StaffView): { at: string; text: string }[] {
	const chef = view === 'chef';
	return (o.delivery_history ?? []).map((q) => {
		const bits = [
			`${chef && q.amount_minor != null ? `Quote ${money(o, q.amount_minor, q.currency)}` : 'Quote'} · pay by ${when(q.pay_by)}`,
			q.current ? 'current' : 'replaced'
		];
		if (chef && q.note) bits.push(`note “${q.note}”`);
		if (q.waiver) {
			const dist =
				q.waiver.distance_m != null
					? ` · ${distanceText(q.waiver.distance_m)}${q.waiver.distance_source ? ` (${q.waiver.distance_source})` : ''}`
					: '';
			bits.push(`waived ${when(q.waiver.at)}${dist}`);
		}
		for (const p of q.payments ?? []) {
			bits.push(
				p.outcome === 'paid'
					? 'paid'
					: p.outcome === 'unmatched'
						? `payment couldn’t be used${p.reason ? ` (${UNMATCHED[p.reason] ?? p.reason})` : ''}`
						: `payment ${p.state}`
			);
		}
		return { at: q.quoted_at, text: bits.join(' · ') };
	});
}

const UNMATCHED: Record<string, string> = {
	quote_replaced: 'the quote had been replaced',
	quote_waived: 'the quote had been waived',
	already_paid: 'already paid',
	order_not_open: 'the order was no longer open',
	no_quote: 'no quote',
	delivery_not_possible: 'GK can’t deliver'
};

// ------------------------------------------------------ 3. GK can't deliver

/**
 * "GK can't deliver" (README 0036): chef only; a GK delivery whose cake is
 * visibly confirmed, never dispatched, nothing unresolved, no delivery
 * payment in flight, not already recorded (one per order, not reversible).
 */
export function canRecordNotPossible(o: DecisionOrder, view: StaffView): boolean {
	if (view !== 'chef' || o.fulfillment_mode !== 'gk_delivery' || !o.delivery) return false;
	if (!OPEN_LIFECYCLES.includes(o.lifecycle)) return false;
	if (o.delivery_choice?.not_possible || o.delivery_choice?.reason === 'not_possible') return false;
	if (!neverDispatched(o) || unresolvedChange(asDetail(o))) return false;
	return o.delivery.state !== 'payment_checking' && o.delivery.state !== 'cancelled_by_choice';
}

/** The confirm step's words for "GK can't deliver". */
export function notPossibleConfirm(o: DecisionOrder): string {
	const paid =
		o.delivery?.state === 'paid' && o.delivery.paid_minor != null
			? ` The ${money(o, o.delivery.paid_minor)} delivery payment comes back in full whatever they choose.`
			: o.delivery?.state === 'quoted' || o.delivery?.state === 'pay_by_passed'
				? ' The unpaid quote is closed.'
				: '';
	return `Record that GK can’t deliver this order. The customer then chooses on their order page: collect it, send their own courier, or a full refund of everything paid. There’s no deadline. Nothing is refunded now.${paid} The order can’t go out for delivery afterwards, and this can’t be undone.`;
}

const CHOICE_WORDS: Record<string, string> = {
	collection: 'collect it',
	customer_courier: 'send their own courier',
	full_refund: 'a full refund of everything paid',
	redelivery: 'a new delivery',
	collection_failure: 'collect it today'
};

const CHOSEN_STATE: Record<string, string> = {
	processing: 'being confirmed',
	chosen: 'confirmed',
	cancelled: 'the order is cancelled with a full refund',
	refused: 'it couldn’t be completed; the choice is open again'
};

/** The Q5 lines: the decision (chef: its reason) and the customer's choice. */
export function choiceStatus(o: DecisionOrder, view: StaffView): string[] {
	const c = o.delivery_choice;
	if (!c) return [];
	const out: string[] = [];
	if (c.not_possible) {
		out.push(
			`GK can’t deliver this order (recorded ${when(c.not_possible.at)}).${view === 'chef' && c.not_possible.reason ? ` Your reason: “${c.not_possible.reason}” (only you see it).` : ''}`
		);
	}
	if (c.pending) out.push('The customer’s choice is being confirmed.');
	else if (c.open) {
		out.push(
			c.reason === 'pay_by_passed'
				? 'Delivery wasn’t paid in time, so the customer chooses: collect it, send their own courier, or a full refund. No deadline: the order stays open until they decide.'
				: 'Waiting for the customer’s choice: collect it, send their own courier, or a full refund of everything paid. No deadline: the order stays open until they decide.'
		);
	}
	const ch = c.chosen;
	if (ch) {
		const to = ch.to ? ` · ${slotText(ch.to.date, ch.to.starts_at, ch.to.ends_at)}` : '';
		const refund =
			view === 'chef' && ch.delivery_refund_minor != null
				? ` The ${money(o, ch.delivery_refund_minor)} delivery payment is being returned in full.`
				: '';
		out.push(
			`The customer chose ${CHOICE_WORDS[ch.choice] ?? ch.choice}${to} · ${CHOSEN_STATE[ch.state] ?? ch.state}.${refund}`
		);
	}
	return out;
}

// --------------------------------------------------------- 4. failed delivery

export interface FailureChoices {
	ourSide: boolean;
	nobody: boolean;
	/** When "nobody available" becomes possible (the delivery time's start), if later. */
	nobodyFrom: string | null;
}

/**
 * A failed delivery (README 0037): chef only, while the delivery is on its
 * way (ready, out for delivery or an exception after a dispatch, no our-side
 * failure for it, not rescheduled since), nothing unresolved. Nobody
 * available is the no-show: at or after the delivery time's start.
 */
export function failureChoices(
	o: DecisionOrder,
	view: StaffView,
	now: Date
): FailureChoices | null {
	if (view !== 'chef' || !onItsWay(o) || unresolvedChange(asDetail(o))) return null;
	const due = now >= new Date(o.slot.starts_at);
	return { ourSide: true, nobody: due, nobodyFrom: due ? null : o.slot.starts_at };
}

/** The confirm words for each cause. */
export function failureConfirm(o: DecisionOrder, cause: 'our_side' | 'nobody_available'): string {
	const paid = o.delivery?.paid_minor != null ? money(o, o.delivery.paid_minor) : null;
	if (cause === 'our_side') {
		return `The customer chooses on their order page: deliver it again (you agree a new time with them; nothing more to pay) or collect it today, with ${paid ? `the ${paid} delivery payment` : 'any delivery payment'} returned in full. Nothing is refunded now and there’s no deadline. It can’t go out again until they choose.`;
	}
	return `Treated like a no-show: the order completes as not collected. The refund owed becomes 50% of what they paid for the cake (anything already refunded counts towards it); it’s requested automatically. ${paid ? `The ${paid} delivery payment is kept` : 'Any delivery payment is kept'}, because the delivery was attempted. This can’t be undone.`;
}

/** The latest failure's lines (money and the reason for the chef only). */
export function failureStatus(o: DecisionOrder, view: StaffView): string[] {
	const f = o.delivery_failure;
	if (!f) return [];
	const chef = view === 'chef';
	const out: string[] = [];
	if (f.cause === 'nobody_available') {
		const st =
			f.state === 'processing'
				? ' · being recorded'
				: f.state === 'refused'
					? ' · it couldn’t be recorded'
					: '';
		out.push(`Nobody was available to take the delivery (${when(f.at)})${st}.`);
		if (chef && f.cake_refund_minor != null) {
			out.push(
				`50% of the cake price back: ${money(o, f.cake_refund_minor)}, requested automatically.${f.delivery_kept_minor ? ` Delivery payment kept: ${money(o, f.delivery_kept_minor)}, because the delivery was attempted.` : ''}`
			);
		}
		return out;
	}
	out.push(`The delivery failed on our side (${when(f.at)}).`);
	const ch = f.chosen;
	if (f.pending) out.push('The customer’s choice is being confirmed.');
	else if (f.open) {
		out.push(
			'Waiting for the customer’s choice: deliver it again, or collect it today with the delivery refunded. No deadline.'
		);
	}
	if (ch) {
		if (ch.choice === 'redelivery') {
			out.push(
				`The customer chose a new delivery · ${CHOSEN_STATE[ch.state] ?? ch.state}.${ch.state === 'chosen' && f.redelivery_time_set === false ? ' Agree the new time with them and set it under Handover; it can go out again once that change is confirmed.' : ch.state === 'chosen' && f.redelivery_time_set ? ' The new time is set; it can go out again.' : ''}`
			);
		} else {
			const to = ch.to ? ` · ${slotText(ch.to.date, ch.to.starts_at, ch.to.ends_at)}` : '';
			out.push(
				`The customer will collect it today${to} · ${CHOSEN_STATE[ch.state] ?? ch.state}.${chef && ch.delivery_refund_minor != null ? ` Delivery refund of ${money(o, ch.delivery_refund_minor)} requested.` : ''}`
			);
		}
	}
	if (chef && f.delivery_paid_minor != null && !ch) {
		out.push(`Delivery payment at stake: ${money(o, f.delivery_paid_minor)}.`);
	}
	return out;
}

// ------------------------------------------------------ 5. appointment change

export interface AppointmentChoice {
	allowed: boolean;
	/** The change is the new delivery time after a redelivery choice. */
	redelivery: boolean;
	/** Why the chef can't change it now ('' when allowed or not the chef's to say). */
	note: string;
}

/**
 * The chef's appointment change (README 0031, 0037): any mode; confirmed,
 * in production or ready; nothing unresolved; never once completed, out for
 * delivery or on its way after a dispatch; while a failure's choice is open
 * or a redelivery isn't secured: no (MOVE_NOT_ELIGIBLE). After a secured
 * redelivery choice it sets the new delivery time.
 */
export function appointmentChoice(o: DecisionOrder, view: StaffView): AppointmentChoice {
	const no = (note = ''): AppointmentChoice => ({ allowed: false, redelivery: false, note });
	if (view !== 'chef' || !OPEN_LIFECYCLES.includes(o.lifecycle)) return no();
	if (unresolvedChange(asDetail(o))) return no();
	if (o.fulfillment_mode === 'gk_delivery') {
		if (onItsWay(o)) {
			return no(
				'The time can’t be changed while the delivery is on its way. If it didn’t arrive, record what happened first (Delivery failed…).'
			);
		}
		const at = lastDispatch(o);
		const f = o.delivery_failure;
		if (at && !movedAfter(o, at)) {
			// Dispatched and failed on our side: only after the customer's secured redelivery choice.
			if (
				f?.cause === 'our_side' &&
				f.chosen?.choice === 'redelivery' &&
				f.chosen.state === 'chosen'
			) {
				return { allowed: true, redelivery: true, note: '' };
			}
			return no(
				f?.cause === 'our_side'
					? 'The time can’t be changed until the customer has chosen what happens after the failed delivery.'
					: ''
			);
		}
		if (f?.cause === 'our_side' && f.open) return no();
	}
	return { allowed: true, redelivery: false, note: '' };
}

// ------------------------------------------------------ the schedule

export interface ScheduleSlot extends Slot {
	availability: string;
	reason_code: string | null;
}

export interface ScheduleChoices {
	date: string;
	day_status: string;
	slots: ScheduleSlot[];
}

/**
 * A day's published starts (the public schedule; slot IDs are stable per
 * schedule version and the same for every mode). There is no staff
 * schedule-choices read: availability is the online booking view, which
 * the chef's commands don't follow (they check their own rules).
 */
export function scheduleDay(
	api: StaffApi,
	o: DecisionOrder,
	date: string
): Promise<ScheduleChoices> {
	const quantity = Math.min(
		20,
		Math.max(
			1,
			(o.items ?? []).reduce((n, i) => n + i.quantity, 0)
		)
	);
	return apiGet<ScheduleChoices>('/api/v1/schedule', {
		baseUrl: api.baseUrl,
		fetch: api.fetch,
		query: { date, fulfillment_mode: o.fulfillment_mode, purpose: 'new_purchase', quantity }
	});
}

export interface SlotOption {
	id: string;
	label: string;
	starts_at: string;
	ends_at: string;
	disabled: boolean;
	hint: string;
}

/**
 * The times the chef can choose on a day: published starts still ahead
 * (the API refuses a time in the past), never on a closed date, never the
 * order's own time (offer: their requested time has its own button). Online
 * fullness is a hint: an offer must fit the day's standard places, a time
 * change takes an extra place.
 */
export function slotOptions(
	c: ScheduleChoices,
	o: DecisionOrder,
	now: Date,
	purpose: 'offer' | 'appointment'
): { options: SlotOption[]; note: string } {
	const closed = c.day_status === 'closed';
	const options = c.slots
		.filter((s) => new Date(s.starts_at) > now)
		.map((s): SlotOption => {
			const own = c.date === o.date && s.id === o.slot.id;
			const full = s.reason_code === 'full';
			return {
				id: s.id,
				label: timeRange(s.starts_at, s.ends_at),
				starts_at: s.starts_at,
				ends_at: s.ends_at,
				disabled: closed || own,
				hint: closed
					? 'closed date'
					: own
						? purpose === 'offer'
							? 'their requested time'
							: 'the current time'
						: full
							? purpose === 'offer'
								? 'day full online · may be refused'
								: 'day full online · an extra place'
							: ''
			};
		});
	const note = closed
		? `${dayShort(c.date)} is a closed date. Choose another day.`
		: !c.slots.length
			? `There are no opening times on ${dayShort(c.date)}. Choose another day.`
			: !options.length
				? `No opening times are left on ${dayShort(c.date)}. Choose another day.`
				: '';
	return { options, note };
}

/** The earliest date the pickers offer (today in London). */
export function firstDate(now: Date): string {
	return londonToday(now);
}

/** The latest date the pickers offer (a year ahead; the schedule's horizon is the chef's). */
export function lastDate(now: Date): string {
	return addDays(londonToday(now), 366);
}

// ------------------------------------------------------------ commands

export type Decision =
	| 'offer'
	| 'honour'
	| 'full_return'
	| 'quote'
	| 'waiver'
	| 'not_possible'
	| 'failed_our_side'
	| 'failed_nobody'
	| 'appointment';

export type DecisionInput =
	| { d: 'offer'; kind: 'requested' }
	| { d: 'offer'; kind: 'other'; date: string; slotId: string }
	| { d: 'honour'; channel: string; agreedAt: string; evidence: string }
	| { d: 'full_return' }
	| { d: 'quote'; amountMinor: number; note: string }
	| { d: 'waiver'; distanceM: number; source: string }
	| { d: 'not_possible'; reason: string }
	| { d: 'failed_our_side'; reason: string }
	| { d: 'failed_nobody'; reason: string }
	| {
			d: 'appointment';
			date: string;
			slotId: string;
			channel: string;
			agreedAt: string;
			evidence: string;
			feasibility: string;
	  };

/** The route and body of a decision at the order's version (strict JSON: no nulls, no extras). */
export function decisionRequest(
	o: DecisionOrder,
	input: DecisionInput
): { path: string; body: Record<string, unknown>; protectedCommand: boolean } {
	const base = `/api/v1/admin/orders/${o.id}`;
	const v = { expected_version: o.version };
	const reason = (r: string) => (r.trim() ? { reason: r.trim() } : {});
	switch (input.d) {
		case 'offer':
			return {
				path: `${base}/late-payment/offer`,
				body:
					input.kind === 'requested'
						? { ...v, kind: 'requested' }
						: { ...v, kind: 'other', target_date: input.date, target_slot_id: input.slotId },
				protectedCommand: false
			};
		case 'honour':
			return {
				path: `${base}/late-payment`,
				body: {
					...v,
					decision: 'honour',
					customer_agreement: {
						channel: input.channel,
						agreed_at: input.agreedAt,
						evidence_ref: input.evidence.trim()
					}
				},
				protectedCommand: true
			};
		case 'full_return':
			return {
				path: `${base}/late-payment`,
				body: { ...v, decision: 'full_return' },
				protectedCommand: true
			};
		case 'quote':
			return {
				path: `${base}/delivery-quote`,
				body: {
					...v,
					amount_minor: input.amountMinor,
					...(input.note.trim() ? { note: input.note.trim() } : {})
				},
				protectedCommand: false
			};
		case 'waiver':
			return {
				path: `${base}/delivery-waiver`,
				body: { ...v, distance_m: input.distanceM, distance_source: input.source.trim() },
				protectedCommand: false
			};
		case 'not_possible':
			return {
				path: `${base}/delivery-not-possible`,
				body: { ...v, ...reason(input.reason) },
				protectedCommand: false
			};
		case 'failed_our_side':
		case 'failed_nobody':
			return {
				path: `${base}/delivery-failed`,
				body: {
					...v,
					cause: input.d === 'failed_our_side' ? 'our_side' : 'nobody_available',
					...reason(input.reason)
				},
				protectedCommand: input.d === 'failed_nobody'
			};
		case 'appointment':
			return {
				path: `${base}/appointment`,
				body: {
					...v,
					target_date: input.date,
					target_slot_id: input.slotId,
					customer_agreement: {
						channel: input.channel,
						agreed_at: input.agreedAt,
						evidence_ref: input.evidence.trim()
					},
					feasibility_note: input.feasibility.trim()
				},
				protectedCommand: true
			};
	}
}

/**
 * The key slot of one request: the decision, the version and exactly what
 * is sent. A retry of the same request reuses its key; anything edited is a
 * new request with a new key (the API refuses an old key with another body:
 * KEY_REUSED).
 */
export function decisionSlot(o: DecisionOrder, input: DecisionInput): string {
	const { body } = decisionRequest(o, input);
	const rest = { ...body };
	delete rest.expected_version;
	return `${input.d}:${o.version}:${JSON.stringify(rest)}`;
}

/** K00 command key: the decision, the order, its version and a nonce. */
export function decisionKey(o: DecisionOrder, d: Decision, nonce: string): string {
	return `chef-${d}-${o.id}-v${o.version}-${nonce}`;
}

export type DecisionAnswer =
	{ kind: 'done' } | { kind: 'unchanged' } | { kind: 'accepted'; accepted: Accepted };

/** Sends one decision with its key. */
export async function sendDecision(
	api: StaffApi,
	o: DecisionOrder,
	input: DecisionInput,
	key: string
): Promise<DecisionAnswer> {
	const { path, body, protectedCommand } = decisionRequest(o, input);
	const r = await apiPost<Partial<Accepted> & { state?: string }>(path, body, {
		baseUrl: api.baseUrl,
		token: api.token,
		fetch: api.fetch,
		idempotencyKey: key
	});
	if (input.d === 'appointment' && r?.state === 'unchanged') return { kind: 'unchanged' };
	if (protectedCommand && r?.operation_id) return { kind: 'accepted', accepted: r as Accepted };
	return { kind: 'done' };
}

export type DecisionOutcome = 'pending' | 'done' | 'refused' | 'returned';

/**
 * Where a protected decision stands in a fresh read. Late payment: done
 * when the order is confirmed, returned when cancelled; nobody available:
 * as the no-show; a time change: its history entry, moved or refused.
 */
export function decisionSettled(
	o: DecisionOrder,
	d: Decision,
	operationId: string
): DecisionOutcome {
	const cmd = (o.commands ?? []).find((c) => c.id === operationId);
	if (cmd && cmd.state !== 'secured') return 'pending';
	if (d === 'failed_nobody') {
		const s = settled(asDetail(o), operationId);
		return s === 'noshow' || s === 'done' ? 'done' : s;
	}
	if (d === 'honour' || d === 'full_return') {
		if (o.lifecycle === 'cancellation_pending') return 'pending';
		if (o.lifecycle === 'cancelled') return 'returned';
		if (o.lifecycle !== 'paid_awaiting_agreement') return 'done';
		if (o.late_payment?.state === 'processing' && !cmd) return 'pending';
		return cmd || o.late_payment?.state !== 'processing' ? 'refused' : 'pending';
	}
	if (d === 'appointment') {
		if (o.appointment_change?.state === 'processing') return 'pending';
		const h = (o.appointment_history ?? []).find((a) => a.command_id === operationId);
		if (h?.state === 'moved') return 'done';
		if (h?.state === 'refused') return 'refused';
		if (cmd?.outcome_status === 'moved') return 'done';
		if (cmd?.outcome_status === 'refused') return 'refused';
		return 'pending';
	}
	return 'done';
}

/** Polls the order for a protected decision's outcome. */
export async function pollDecision(
	read: () => Promise<DecisionOrder>,
	d: Decision,
	accepted: Pick<Accepted, 'operation_id' | 'poll_after_seconds'>,
	opts: { tries?: number; wait?: (ms: number) => Promise<void>; active?: () => boolean } = {}
): Promise<{ order: DecisionOrder | null; outcome: DecisionOutcome }> {
	const tries = opts.tries ?? 20;
	const wait = opts.wait ?? ((ms: number) => new Promise<void>((r) => setTimeout(r, ms)));
	const every = Math.max(1, Math.min(accepted.poll_after_seconds || 3, 10)) * 1000;
	let last: DecisionOrder | null = null;
	for (let i = 0; i < tries; i++) {
		await wait(every);
		if (opts.active && !opts.active()) break;
		try {
			last = await read();
		} catch (e) {
			if (e instanceof ApiError && e.signInRequired) throw e;
			continue;
		}
		const s = decisionSettled(last, d, accepted.operation_id);
		if (s !== 'pending') return { order: last, outcome: s };
	}
	return { order: last, outcome: 'pending' };
}

/** What the page says once a decision is done. */
export function decisionDone(input: DecisionInput, outcome: DecisionOutcome = 'done'): string {
	switch (input.d) {
		case 'offer':
			return 'Offer sent ✓ · it’s on the customer’s order page. Nothing changes unless they accept it.';
		case 'honour':
			return 'Confirmed ✓ · the order goes ahead on its requested date. Your check and the customer’s agreement are saved with it.';
		case 'full_return':
			return outcome === 'returned'
				? 'Returned ✓ · the order is cancelled and everything paid is being returned (see Refund).'
				: 'Recorded ✓';
		case 'quote':
			return 'Quote saved ✓ · the customer sees it on their order page and pays there.';
		case 'waiver':
			return 'Delivery waived ✓ · nothing to pay for delivery; it can go out when the cake is ready.';
		case 'not_possible':
			return 'Recorded ✓ · the customer now chooses another way on their order page.';
		case 'failed_our_side':
			return 'Recorded ✓ · the customer now chooses: deliver it again, or collect it today.';
		case 'failed_nobody':
			return 'Recorded ✓ · the order is complete as not collected. The refund owed is under Refund, as recorded.';
		case 'appointment':
			return outcome === 'done'
				? 'Time changed ✓ · the customer is emailed the new time.'
				: 'Recorded ✓';
	}
}

export const DECISION_REFUSED =
	'It wasn’t recorded: the order changed before it could be. The latest version is shown; check it and try again if it’s still right.';

/** Refusals in plain words for this slice's commands (api/README codes). */
export function decisionRefusal(e: unknown, d: Decision): Refusal {
	const base = refusalOf(e, 'ready');
	if (!(e instanceof ApiError)) return base;
	const r = (text: string, over: Partial<Refusal> = {}): Refusal => ({
		...base,
		text,
		reload: true,
		...over
	});
	switch (e.code) {
		case 'CONFLICT':
			return r(CONFLICT_WORDS[d]);
		case 'HONOUR_TOO_LATE':
			return r(
				'Their requested time has passed, so it can’t be offered or confirmed any more. Offer another date, or return everything.'
			);
		case 'SLOT_UNAVAILABLE':
			return r(
				d === 'offer'
					? 'That time can’t take this order: the day may be full (an offer can’t use an extra place), closed, past the cakes’ use-by, or the time no longer published. Choose another time, or raise the day’s limit first.'
					: 'That time can’t take this order: it may be closed, already started, past a ready cake’s use-by, or no longer published. Choose another time.',
				{ reload: false }
			);
		case 'PAYMENT_IN_PROGRESS':
			return r(
				'A delivery payment is in progress. Wait for its result (the customer mustn’t pay again), then try again.'
			);
		case 'DELIVERY_PAY_BY_PASSED':
			return r(
				'The pay-by time (48 hours before the original delivery time) has passed, so a quote can’t be sent now. The customer chooses another way on their order page.'
			);
		case 'WAIVER_NOT_ELIGIBLE':
			return r(
				'This order wasn’t placed with a free-delivery code, so its delivery can’t be waived.',
				{ reload: false }
			);
		case 'MOVE_NOT_ELIGIBLE':
			return r(
				'The time of this order can’t be changed now (for example it’s out for delivery, a failed delivery is waiting for the customer’s choice, or the order is complete). The latest version is shown.'
			);
		case 'TRANSITION_NOT_ALLOWED':
			return r(
				'A failed delivery can’t be recorded for the order as it is now (it may not be on its way, or a failure is already recorded for this delivery). The latest version is shown.'
			);
		case 'HANDOVER_NOT_DUE':
			return r(
				'The delivery time hasn’t started yet, so you can’t record that nobody was available. Try again once it has started.'
			);
		case 'FORBIDDEN':
			return r(ONLY_CHEF, { reload: false });
		case 'INVALID_INPUT':
			return r(INVALID_WORDS[d], { reload: false });
	}
	return base;
}

const CONFLICT_WORDS: Record<Decision, string> = {
	offer:
		'An offer can’t be made now: the order isn’t waiting for your decision any more, or a decision or the customer’s answer is still being processed. The latest version is shown.',
	honour:
		'It can’t be decided now: another decision or the customer’s answer is still being processed, or the order has already changed. The latest version is shown.',
	full_return:
		'It can’t be decided now: another decision or the customer’s answer is still being processed, or the order has already changed. The latest version is shown.',
	quote:
		'A quote can’t be saved now: the delivery may already be paid, the order may be out for delivery or no longer open, or another change is still being processed. The latest version is shown.',
	waiver:
		'Delivery can’t be waived now: the quote may already be paid or waived, there may be no quote, or another change is still being processed. The latest version is shown.',
	not_possible:
		'It can’t be recorded now: the order may already be out for delivery, “GK can’t deliver” may already be recorded, or another change is still being processed. The latest version is shown.',
	failed_our_side:
		'Another change to this order is still being processed. Wait a moment and check the order again.',
	failed_nobody:
		'Another change to this order is still being processed. Wait a moment and check the order again.',
	appointment:
		'Another change to this order is still being processed (for example the customer’s own move). Wait a moment and check the order again.'
};

const INVALID_WORDS: Record<Decision, string> = {
	offer: 'Choose another time: an offer of another date can’t be their requested time.',
	honour:
		'Check the customer’s agreement: when they agreed must be after the payment and not in the future, and the evidence one line of up to 200 characters.',
	full_return: 'This couldn’t be sent as it is. Reload the page and try again.',
	quote: 'Check the price (whole pence from £100) and the note (one line, up to 500 characters).',
	waiver:
		'Check the distance (up to 1,000 km) and how you measured it (one line, up to 200 characters).',
	not_possible: 'Check the reason: one line, up to 500 characters.',
	failed_our_side: 'Check the reason: one line, up to 500 characters.',
	failed_nobody: 'Check the reason: one line, up to 500 characters.',
	appointment:
		'Check the details: when they agreed must be after the order was placed and not in the future; your note (up to 500 characters) and the evidence (up to 200) one line each.'
};

/** A chef-only hint for views that can't act (fulfilment, technical support). */
export function lookOnly(view: StaffView): string {
	return view === 'chef'
		? ''
		: view === 'fulfillment'
			? 'Elina decides these.'
			: 'Elina decides these; technical support can look but not change orders.';
}

/** "11:00 on Thu 29 Oct" (London time). */
export function atTime(iso: string): string {
	return `${londonTime(iso)} on ${dayShort(londonToday(new Date(iso)))}`;
}
