// Deciding a request to add an order (Creator-approved GK-018 r04d-claims
// sketch, request.js): finding the order, the contact-email check before a
// website order is added from a reference (Creator answer 3), the
// wrong-person warning, the internal note, and the API's refusals in the
// sketch's words. Pure functions; the page holds the state.
import { ApiError } from '../api';
import { addDays, timeRange } from '../checkout/format';
import { shortRef, type StaffClaim } from './claims';
import { shortDay, type StaffOrderRow } from './orders';

/** The address the live customer pages give for the business. */
export const BUSINESS_EMAIL = 'gk.cake.london@gmail.com';

// ------------------------------------------------------------ months

const YM = /^(\d{4})-(\d{2})$/;

/** YYYY-MM plus n months. */
export function shiftMonth(ym: string, n: number): string {
	const m = YM.exec(ym);
	if (!m) throw new Error(`not a month: ${ym}`);
	const total = Number(m[1]) * 12 + (Number(m[2]) - 1) + n;
	return `${Math.floor(total / 12)}-${String((total % 12) + 1).padStart(2, '0')}`;
}

/** Months from `a` to `b` inclusive (0 or less when `b` is before `a`). */
export function monthsSpan(a: string, b: string): number {
	const [y1, m1] = a.split('-').map(Number);
	const [y2, m2] = b.split('-').map(Number);
	return (y2 - y1) * 12 + (m2 - m1) + 1;
}

/** The most months one search covers (the order list reads at most 93 days). */
export const MAX_SEARCH_MONTHS = 3;

/** The months the handover selects offer: two years back to a year ahead of `today`'s month. */
export function monthOptions(today: string): string[] {
	const now = today.slice(0, 7);
	const out: string[] = [];
	for (let n = -24; n <= 12; n++) out.push(shiftMonth(now, n));
	return out;
}

/** The first search: their month ±1, or the last 3 months when they gave none (sketch). */
export function defaultMonths(
	approximateDate: string | null,
	today: string
): { from: string; to: string } {
	const theirs = approximateDate?.slice(0, 7);
	const options = monthOptions(today);
	const clamp = (ym: string) =>
		ym < options[0]
			? options[0]
			: ym > options[options.length - 1]
				? options[options.length - 1]
				: ym;
	if (theirs && YM.test(theirs)) {
		return { from: clamp(shiftMonth(theirs, -1)), to: clamp(shiftMonth(theirs, 1)) };
	}
	const now = today.slice(0, 7);
	return { from: shiftMonth(now, -2), to: now };
}

/** London handover days covering whole months `from`..`to` (inclusive). */
export function monthsRange(from: string, to: string): { from: string; to: string } {
	return { from: `${from}-01`, to: addDays(`${shiftMonth(to, 1)}-01`, -1) };
}

const MONTH_NAMES = [
	'January',
	'February',
	'March',
	'April',
	'May',
	'June',
	'July',
	'August',
	'September',
	'October',
	'November',
	'December'
];

export function monthName(ym: string): string {
	const [y, m] = ym.split('-').map(Number);
	return `${MONTH_NAMES[m - 1]} ${y}`;
}

// ------------------------------------------------------------ search

/**
 * Orders matching the start of their ID (a full order ID works too) or a
 * part of the name on the order, by handover date. No automatic suggestion:
 * the chef chooses (sketch: "choosing one is your judgement").
 */
export function matchOrders(rows: StaffOrderRow[], term: string): StaffOrderRow[] {
	const t = term.trim().toLowerCase();
	return rows
		.filter(
			(o) =>
				!t || o.id.toLowerCase().startsWith(t) || o.customer_full_name.toLowerCase().includes(t)
		)
		.sort((a, b) => a.slot.starts_at.localeCompare(b.slot.starts_at));
}

/** The last word of their profile name, for "Search for their surname". */
export function surname(fullName: string | null): string | null {
	const words = fullName?.trim().split(/\s+/).filter(Boolean) ?? [];
	return words.length ? words[words.length - 1] : null;
}

/** "Chocolate swirl · Sat 14 Nov, 12:30–13:30". */
export function orderLine(o: Pick<StaffOrderRow, 'items' | 'date' | 'slot'>): string {
	const cake = o.items.map((i) => i.cake_name).join(', ') || 'Order';
	return `${cake} · ${shortDay(o.date)}, ${timeRange(o.slot.starts_at, o.slot.ends_at)}`;
}

// ------------------------------------------------------------ what a wrong approval could do

/** An order the account's owner could still act on (cancel, move the pickup, answer a late payment). */
export function isUpcoming(o: Pick<StaffOrderRow, 'lifecycle'>): boolean {
	return o.lifecycle !== 'completed' && o.lifecycle !== 'cancelled';
}

/** The customer's own move: collection and customer-courier orders only (GK delivery times are the chef's). */
export function canMovePickup(o: Pick<StaffOrderRow, 'lifecycle' | 'fulfillment_mode'>): boolean {
	return isUpcoming(o) && o.fulfillment_mode !== 'gk_delivery';
}

/** "can cancel it and may be able to move its pickup time" / "can cancel it". */
export function ownerCan(o: Pick<StaffOrderRow, 'lifecycle' | 'fulfillment_mode'>): string {
	return canMovePickup(o)
		? 'can cancel it and may be able to move its pickup time'
		: 'can cancel it';
}

// ------------------------------------------------------------ the contact-email check

/** A fresh one-off 4-digit code for this check (never reused; never sent to the requester). */
export function newCheckCode(random: () => number = secureRandom): string {
	return String(1000 + Math.floor(random() * 9000));
}

function secureRandom(): number {
	const a = new Uint32Array(1);
	crypto.getRandomValues(a);
	return a[0] / 2 ** 32;
}

/**
 * The email the chef sends to the order's contact address: the code, never
 * the request reference (the real buyer could reply with it and confirm
 * someone else's request), and nothing about the requester.
 */
export function checkEmail(
	o: Pick<StaffOrderRow, 'items' | 'date' | 'slot'>,
	code: string
): string {
	return `Hello! Someone has asked to add your GK Cake London order (${orderLine(o)}) to their account on our website. If that was you, please reply to this email with the code ${code} and the request reference shown in your account under “Your requests”. If it wasn’t you, just tell us and nothing will be added.`;
}

/** The "How you confirmed" example for this order and request. */
export function confirmPlaceholder(
	contactEmail: string,
	code: string,
	claim: Pick<StaffClaim, 'id'>
): string {
	return `e.g. Emailed ${contactEmail} with code ${code}; reply quoted ${code} and ${shortRef(claim.id)}`;
}

// ------------------------------------------------------------ the internal note

/** The API's note limit, counted in code points as the API does. */
export const NOTE_MAX = 500;

export function noteLength(v: string): number {
	return [...v].length;
}

/** Why a note can't be saved, or null: one line, no surrounding spaces, at most 500 characters. */
export function noteProblem(v: string, required: boolean): string | null {
	const n = v.trim();
	if (!n) return required ? 'Say how you confirmed it before adding.' : null;
	if (/[\r\n\u2028\u2029]/.test(v)) return 'Keep the note to one line.';
	if (noteLength(n) > NOTE_MAX)
		return `The note is ${noteLength(n) - NOTE_MAX} characters too long.`;
	return null;
}

// ------------------------------------------------------------ refusals

export type DecisionRefusal =
	| 'order_owned'
	| 'stale'
	| 'not_found_order'
	| 'account_unavailable'
	| 'paused'
	| 'lost'
	| 'forbidden'
	| 'signed_out';

/** The API's answer to a decision, in the sketch's cases. */
export function refusalOf(e: unknown): DecisionRefusal {
	if (!(e instanceof ApiError)) return 'lost';
	if (e.signInRequired) return 'signed_out';
	switch (e.code) {
		case 'ORDER_OWNED':
			return 'order_owned';
		case 'STALE_VERSION':
		case 'CONFLICT':
		case 'NOT_FOUND':
			return 'stale';
		case 'INVALID_INPUT':
			return 'not_found_order';
		case 'ACCOUNT_UNAVAILABLE':
			return 'account_unavailable';
		case 'FORBIDDEN':
		case 'ACCOUNT_DISABLED':
			return 'forbidden';
	}
	if (e.status === 503) return 'paused';
	return 'lost';
}

/** Whether "Try again" may resend the same decision with the same key. */
export function retrySafe(r: DecisionRefusal): boolean {
	return r === 'lost' || r === 'paused';
}

/** The waiting request received after this one, for "Next waiting request". */
export function nextWaiting(c: StaffClaim, waiting: StaffClaim[]): StaffClaim | null {
	return (
		waiting.find((x) => x.id !== c.id && x.state === 'pending' && x.created_at >= c.created_at) ??
		waiting.find((x) => x.id !== c.id && x.state === 'pending') ??
		null
	);
}
