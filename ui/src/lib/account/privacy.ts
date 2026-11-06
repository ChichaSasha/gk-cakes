// "Privacy & your data" (approved GK-016 r02b sketch, states 48 and 50–59)
// as pure helpers: the retention table (K09 and the owner's GK-005 rules, as
// r02b words them), the deletion preview turned into the step to show
// (blocked, warning or review) with the sketch's words, the review ticks,
// the fresh sign-in the API needs (the token's email-link or Google sign-in
// instant within 10 minutes), the marker that brings the customer back to
// the final step after the confirmation link, and clearing this device.
import { ApiError } from '../api';
import { dateLabel } from '../checkout/format';
import { BAG_KEY } from '../shop/bag';
import { FAVOURITES_KEY, type DeviceStorage } from '../shop/favourites';
import type { DeletionPreview, DeletionUpcoming } from './client';
import { orderRef } from './orders';

/** The retention table, row header and how long, exactly as r02b (state 48). */
export const RETENTION: readonly (readonly [string, string])[] = [
	[
		'Account, profile, favourites and order list',
		'While you use your account. After 24 months in which you don’t sign in or order, we email you a warning 30 days before deleting it and a reminder 7 days before; if you don’t sign in or order by then, we delete it.'
	],
	[
		'Details of your orders and messages about them (name, email, cake, date)',
		'Until an unfinished order is done, then 2 years after the cake is handed over or the order is cancelled, even if you delete your account (then no longer linked to it).'
	],
	['Custom requests (enquiries)', '6 months after your last conversation with the chef.'],
	[
		'Photos you send with a request',
		'Deleted within 30 days after handover, unless you gave specific permission (then 3 months).'
	],
	[
		'Payment and refund records',
		'For the period the law requires (being confirmed), with as little personal information as possible.'
	],
	[
		'On this device',
		'Favourites: a year after you save them. A request draft: 30 days after your last change, without its description or photos.'
	]
];

/** Why an order blocks deletion, in the sketch's words (state 51). */
const BLOCKED: Record<string, string> = {
	payment_being_checked: 'we’re still checking a payment',
	paid_awaiting_agreement: 'payment received, date still to agree',
	refund_in_progress: 'a refund is on its way',
	refund_being_checked: 'we’re still checking a refund',
	order_change_in_progress: 'we’re still confirming a change to it',
	payment_recorded: 'we have a payment recorded for it; we’ll contact you'
};

/** The words after the order reference in the blocked list. */
export function blockedReason(reason: string): string {
	return BLOCKED[reason] ?? 'it still has money to settle';
}

const UPCOMING: Record<string, string> = {
	confirmed: 'confirmed',
	in_production: 'being prepared',
	ready: 'ready'
};

/** "Fri 27 November 2026, confirmed" (the words after the order reference, state 52). */
export function upcomingLine(u: Pick<DeletionUpcoming, 'date' | 'lifecycle'>): string {
	const parts: string[] = [];
	if (u.date && /^\d{4}-\d{2}-\d{2}$/.test(u.date)) parts.push(dateLabel(u.date));
	const state = UPCOMING[u.lifecycle];
	if (state) parts.push(state);
	return parts.join(', ');
}

function list(words: string[]): string {
	if (words.length <= 1) return words.join('');
	return `${words.slice(0, -1).join(', ')} and ${words[words.length - 1]}`;
}

/** The extra tick for upcoming orders (state 52), one or several. */
export function ordersTickLabel(upcoming: Pick<DeletionUpcoming, 'order_id'>[]): string {
	const refs = list(upcoming.map((u) => orderRef({ id: u.order_id })));
	return upcoming.length === 1
		? `I understand my order ${refs} goes ahead and I’ll arrange anything about it with the chef directly.`
		: `I understand my orders ${refs} go ahead and I’ll arrange anything about them with the chef directly.`;
}

/** The warning's second sentence (state 52), for one or several upcoming orders. */
export function upcomingWarning(count: number): string {
	return count === 1
		? 'If you delete your account now, the order still goes ahead, so we keep the name and email it needs. You won’t be able to see, change or cancel it online; the chef will contact you about it.'
		: 'If you delete your account now, the orders still go ahead, so we keep the name and email they need. You won’t be able to see, change or cancel them online; the chef will contact you about them.';
}

export type DeletionStep = 'staff' | 'blocked' | 'warn' | 'review';

/**
 * Which step a preview leads to: a staff account can't be deleted here;
 * unsettled money blocks (any refusal this build can't explain is shown as
 * blocked, never as deletable); a confirmed upcoming order warns; otherwise
 * the review.
 */
export function deletionStep(
	p: Pick<DeletionPreview, 'can_delete' | 'blocked' | 'upcoming' | 'staff_capabilities'>
): DeletionStep {
	if (p.staff_capabilities.length > 0) return 'staff';
	if (p.blocked.length > 0 || !p.can_delete) return 'blocked';
	if (p.upcoming.length > 0) return 'warn';
	return 'review';
}

export const TICK_MESSAGE = 'Tick to confirm you understand.';

export interface Ticks {
	/** "I understand my order … goes ahead" (only when there are upcoming orders). */
	orders: boolean;
	/** "I understand that deleting my account can’t be undone." */
	undone: boolean;
}

/** The missing ticks, in form order (empty when the customer may continue). */
export function tickErrors(
	ticks: Ticks,
	needOrders: boolean,
	needUndone = true
): Partial<Record<keyof Ticks, string>> {
	const errors: Partial<Record<keyof Ticks, string>> = {};
	if (needOrders && !ticks.orders) errors.orders = TICK_MESSAGE;
	if (needUndone && !ticks.undone) errors.undone = TICK_MESSAGE;
	return errors;
}

/** How recent the sign-in must be for the API to delete the account. */
export const FRESH_SIGN_IN_MS = 10 * 60 * 1000;
// The sign-in methods the API counts: email link (otp, magiclink) and Google (oauth).
const FRESH_METHODS = new Set(['otp', 'magiclink', 'oauth']);

/**
 * The latest email-link or Google sign-in instant (ms) in an access token's
 * `amr` claim, as the API reads it; null when there is none (a password
 * sign-in never counts). Decoded for the page's routing only: the API
 * checks the signed token.
 */
export function signInAt(accessToken: string | undefined): number | null {
	const payload = accessToken?.split('.')[1];
	if (!payload) return null;
	let claims: unknown;
	try {
		const b64 = payload.replace(/-/g, '+').replace(/_/g, '/');
		claims = JSON.parse(atob(b64.padEnd(b64.length + ((4 - (b64.length % 4)) % 4), '=')));
	} catch {
		return null;
	}
	const amr = (claims as { amr?: unknown } | null)?.amr;
	if (!Array.isArray(amr)) return null;
	let latest: number | null = null;
	for (const entry of amr) {
		const { method, timestamp } = (entry ?? {}) as { method?: unknown; timestamp?: unknown };
		if (typeof method !== 'string' || !FRESH_METHODS.has(method)) continue;
		if (typeof timestamp !== 'number' || !Number.isFinite(timestamp)) continue;
		const at = timestamp * 1000;
		if (latest === null || at > latest) latest = at;
	}
	return latest;
}

/** Whether a sign-in at `at` still allows the deletion at `now` (10 minutes; not in the future). */
export function freshSignIn(at: number | null, now: number): boolean {
	return at !== null && at <= now + 60_000 && now - at < FRESH_SIGN_IN_MS;
}

/**
 * The marker that a customer reviewed the deletion and asked for a
 * confirmation link: its expiry and a one-way hash of the account it was set
 * for (`who`, see session.ts accountMarkerId), no personal data. It counts
 * only for that same account, so someone else signing in on this browser
 * never skips the review. It is kept in localStorage because the email link
 * usually opens in a new tab (as the sign-in return page is), for as long as
 * the link works, and the account pages' Sign out, Sign out everywhere and
 * the ended-session sign-out remove it (guard.ts signOutHere).
 */
export const DELETION_MARKER_KEY = 'gk-account-deletion';
export const DELETION_MARKER_MS = 30 * 60 * 1000;
const WHO = /^[0-9a-f]{64}$/;

export function markDeletion(storage: DeviceStorage | null, now: number, who: string): boolean {
	if (!WHO.test(who)) return false;
	try {
		storage?.setItem(DELETION_MARKER_KEY, `${now + DELETION_MARKER_MS}:${who}`);
		return storage !== null;
	} catch {
		return false;
	}
}

/**
 * Whether the marker is set for this account (`who`) and not expired at
 * `now`. An expired, damaged or another account's marker is removed.
 */
export function deletionMarked(
	storage: DeviceStorage | null,
	now: number,
	who: string | undefined
): boolean {
	try {
		const raw = storage?.getItem(DELETION_MARKER_KEY);
		if (raw === null || raw === undefined) return false;
		const m = /^(\d{1,16}):([0-9a-f]{64})$/.exec(raw);
		const until = m ? Number(m[1]) : NaN;
		// Never longer than a marker set now would last (an edited entry).
		const live = Number.isFinite(until) && until > now && until <= now + DELETION_MARKER_MS;
		if (live && m && who !== undefined && m[2] === who) return true;
		storage?.removeItem(DELETION_MARKER_KEY);
		return false;
	} catch {
		return false;
	}
}

/**
 * What the delete page may do now, from who opened it and who is signed in:
 * 'final' only for the same account, marked and freshly signed in;
 * 'reauth' for the same account whose fresh sign-in or marker has run out
 * (sign in again; the server would refuse anyway); 'review' when another
 * account's session (or none we can identify) is in this browser — never a
 * shortcut for anyone else.
 */
export function finalDecision(f: {
	openedFor: string | undefined;
	who: string | undefined;
	marked: boolean;
	fresh: boolean;
}): 'final' | 'reauth' | 'review' {
	if (f.who === undefined || f.who !== f.openedFor) return 'review';
	return f.marked && f.fresh ? 'final' : 'reauth';
}

/** Whether a confirmation link may be marked for this deletion: the account that reviewed it is still the one signed in. */
export function mayMark(openedFor: string | undefined, who: string | undefined): who is string {
	return who !== undefined && who === openedFor;
}

export function forgetDeletion(storage: DeviceStorage | null): void {
	try {
		storage?.removeItem(DELETION_MARKER_KEY);
	} catch {
		// nothing to forget
	}
}

/**
 * What "Clear this device" removes: favourites (localStorage), the bag
 * (this tab's sessionStorage) and the deletion marker. Not the sign-in
 * session (Sign out does that) and not a guest order's access in this tab,
 * which the guest needs to pay or see that order.
 */
export const DEVICE_KEYS = {
	local: [FAVOURITES_KEY, DELETION_MARKER_KEY],
	tab: [BAG_KEY]
} as const;

/** Removes this device's saved shop data. Returns false when a store refused. */
export function clearDevice(local: DeviceStorage | null, tab: DeviceStorage | null): boolean {
	let ok = true;
	for (const [store, keys] of [
		[local, DEVICE_KEYS.local],
		[tab, DEVICE_KEYS.tab]
	] as const) {
		if (!store) {
			ok = false;
			continue;
		}
		for (const key of keys) {
			try {
				store.removeItem(key);
			} catch {
				ok = false;
			}
		}
	}
	return ok;
}

export type DeletionFailure =
	'reauth' | 'ended' | 'blocked' | 'staff' | 'stale' | 'deleted' | 'disabled' | 'retry' | 'failed';

/**
 * What a refused or failed deletion means: a fresh sign-in is needed
 * (checked before an ended session, both are 401), the facts changed
 * (blocked, or stale: the preview expired, changed or isn't this
 * account's), a staff account, an account already deleted or disabled, a
 * lost answer worth retrying with the same key, or another failure.
 */
export function deletionFailure(e: unknown): DeletionFailure {
	if (!(e instanceof ApiError)) return 'failed';
	if (e.code === 'REAUTHENTICATION_REQUIRED') return 'reauth';
	if (e.signInRequired) return 'ended';
	switch (e.code) {
		case 'ACCOUNT_DELETION_BLOCKED':
			return 'blocked';
		case 'STAFF_ACCOUNT':
			return 'staff';
		case 'DELETION_PREVIEW_STALE':
			return 'stale';
		case 'ACCOUNT_DELETED':
			return 'deleted';
		case 'ACCOUNT_DISABLED':
			return 'disabled';
	}
	if (e.status === 422) return 'stale';
	if (e.status === 0 || e.retryable) return 'retry';
	return 'failed';
}
