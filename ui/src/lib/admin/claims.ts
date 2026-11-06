// Requests to add orders to customer accounts (order claims), the chef's
// side: the staff API (api/README "Customer accounts", migration 0033) and
// the view model of the Creator-approved GK-018 r04d-claims sketch (draft PR
// #59, head c372c9c; approved "as suggested" on the GK-018 card 2026-11-06).
// Chef only: fulfilment and technical support get 403 from the API.
import { apiGet, apiPost } from '../api';
import type { StaffApi } from './catalogue';

export type ClaimState = 'pending' | 'approved' | 'declined' | 'withdrawn';
/** evidence = "Add an older order" (their reference); email_link = from the order page or an order email. */
export type ClaimPath = 'evidence' | 'email_link';
export type ClaimChannel = 'instagram' | 'offline' | 'website_email_link';

/** One request as the chef's queue returns it. */
export interface StaffClaim {
	id: string;
	version: number;
	state: ClaimState | string;
	path: ClaimPath | string;
	channel: ClaimChannel | string;
	/** Their reference (path evidence); null on email_link requests and once erased. */
	reference: string | null;
	/** YYYY-MM or YYYY-MM-DD, as the customer gave it. */
	approximate_date: string | null;
	note: string | null;
	/** The decided order, or the order an email_link request came from. */
	order_id: string | null;
	created_at: string;
	decided_at: string | null;
	/** The staff account that decided (an account ID). */
	decided_by: string | null;
	/** The chef's internal note: never shown to the customer. */
	decision_note: string | null;
	/** From the customer's profile: typed by them, not checked, not proof (K02). */
	account: {
		id: string;
		full_name: string | null;
		instagram: string | null;
		phone: string | null;
	};
}

export interface ClaimPage {
	claims: StaffClaim[];
	next_cursor?: string;
}

/** The queue's tabs, in the sketch's order, with the API state each shows. */
export const CLAIM_TABS: readonly [ClaimState, string][] = [
	['pending', 'Waiting'],
	['approved', 'Added'],
	['declined', 'Couldn’t add'],
	['withdrawn', 'Withdrawn']
];

export function isClaimState(v: string | null): v is ClaimState {
	return CLAIM_TABS.some(([s]) => s === v);
}

/** Requests one queue read returns (the API's most). */
export const CLAIM_PAGE = 50;

/** One page of the chef's queue in `state` (oldest received first). */
export function staffClaims(api: StaffApi, state: ClaimState, cursor?: string): Promise<ClaimPage> {
	return apiGet<ClaimPage>('/api/v1/admin/order-claims', {
		baseUrl: api.baseUrl,
		token: api.token,
		fetch: api.fetch,
		query: { state, limit: CLAIM_PAGE, cursor }
	});
}

/** Most pages one search for a single request follows, per state. */
export const MAX_CLAIM_PAGES = 10;

/**
 * Finds one request by ID. The API has no single-request read yet (proposed
 * in the sketch), so this pages the four lists, waiting first. A request
 * closed when its customer deleted their account is in no list: null.
 */
export async function findClaim(api: StaffApi, id: string): Promise<StaffClaim | null> {
	if (!UUID.test(id)) return null;
	for (const [state] of CLAIM_TABS) {
		let cursor: string | undefined;
		for (let i = 0; i < MAX_CLAIM_PAGES; i++) {
			const page = await staffClaims(api, state, cursor);
			const found = page.claims.find((c) => c.id === id);
			if (found) return found;
			if (!page.next_cursor) break;
			cursor = page.next_cursor;
		}
	}
	return null;
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

export type ClaimDecision =
	| { expected_version: number; decision: 'approve'; order_id: string; note?: string }
	| { expected_version: number; decision: 'decline'; note?: string };

/** The chef's decision (strict JSON: only these fields; note is internal). */
export function decideClaim(
	api: StaffApi,
	key: string,
	claimId: string,
	d: ClaimDecision
): Promise<{ claim_id: string; state: ClaimState; order_id?: string }> {
	if (!UUID.test(claimId)) throw new Error(`not an ID: ${claimId}`);
	const body: Record<string, unknown> = {
		expected_version: d.expected_version,
		decision: d.decision
	};
	if (d.decision === 'approve') {
		if (!UUID.test(d.order_id)) throw new Error(`not an ID: ${d.order_id}`);
		body.order_id = d.order_id;
	}
	if (d.note !== undefined && d.note !== '') body.note = d.note;
	return apiPost(`/api/v1/admin/order-claims/${claimId}/decision`, body, {
		baseUrl: api.baseUrl,
		token: api.token,
		fetch: api.fetch,
		idempotencyKey: key
	});
}

// ------------------------------------------------------------ view model

const CHANNEL_CHEF: Record<string, string> = {
	instagram: 'Instagram',
	offline: 'Email, phone or in person',
	website_email_link: 'On this website'
};

/** How the customer ordered, as the chef reads it (the request's path first, then its channel). */
export function howLabel(c: Pick<StaffClaim, 'path' | 'channel'>): string {
	if (c.path === 'email_link') return 'From the order page or email';
	return CHANNEL_CHEF[c.channel] ?? 'Other';
}

/** A request from the order page or an order email: customers can't send these yet. */
export function isFuturePath(c: Pick<StaffClaim, 'path'>): boolean {
	return c.path === 'email_link';
}

/** The name from their profile (typed by the customer, not proof). */
export function whoName(c: Pick<StaffClaim, 'account'>): string {
	return c.account.full_name?.trim() || 'No name in their profile';
}

/** "Instagram priya.bakes · 07700 900123", or that neither is in their profile. */
export function whoContact(c: Pick<StaffClaim, 'account'>): string {
	const parts = [
		c.account.instagram ? `Instagram ${c.account.instagram}` : '',
		c.account.phone ?? ''
	].filter(Boolean);
	return parts.length ? parts.join(' · ') : 'No Instagram or phone';
}

/** A request's or order's short reference: the start of its ID (what the customer sees too). */
export function shortRef(id: string): string {
	return id.slice(0, 8);
}

const LONDON_DAY = new Intl.DateTimeFormat('en-CA', {
	timeZone: 'Europe/London',
	year: 'numeric',
	month: '2-digit',
	day: '2-digit'
});

/** Whole London calendar days from `iso` to `now` (0 = received today). */
export function daysWaiting(iso: string, now: Date): number {
	const a = Date.parse(`${LONDON_DAY.format(new Date(iso))}T12:00:00Z`);
	const b = Date.parse(`${LONDON_DAY.format(now)}T12:00:00Z`);
	return Math.max(0, Math.round((b - a) / 86_400_000));
}

/** Amber from this many days waiting: a visual cue only, no promise to customers (Creator answer 9). */
export const WAITING_AMBER_DAYS = 3;

export function waitText(iso: string, now: Date): string {
	const d = daysWaiting(iso, now);
	return d < 1 ? 'Waiting since today' : d === 1 ? 'Waiting since yesterday' : `Waiting ${d} days`;
}

const MONTHS = [
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

/** "June 2026" from YYYY-MM or YYYY-MM-DD. */
export function monthLabel(v: string | null): string | null {
	const m = /^(\d{4})-(\d{2})(?:-\d{2})?$/.exec(v ?? '');
	if (!m || +m[2] < 1 || +m[2] > 12) return null;
	return `${MONTHS[+m[2] - 1]} ${m[1]}`;
}

/**
 * Their reference looks like a website order's (the guest order page shows
 * the full order ID as "Order reference"; customers may paste its start). A
 * format hint, never a match.
 */
export function looksLikeOrderRef(ref: string | null): boolean {
	return /^[0-9a-f]{8}(-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})?$/i.test(
		ref?.trim() ?? ''
	);
}

/** The decided state in the chef's words, with its tone. */
export const STATE_CHEF: Record<string, [string, 'info' | 'ok' | 'neutral']> = {
	pending: ['Waiting', 'info'],
	approved: ['Added to their account', 'ok'],
	declined: ['Couldn’t add', 'neutral'],
	withdrawn: ['Withdrawn by the customer', 'neutral']
};

/** Who decided: the signed-in chef, or another chef account (there is no staff-name lookup). */
export function decidedBy(c: Pick<StaffClaim, 'decided_by'>, me: string | null): string {
	return c.decided_by && me && c.decided_by === me ? 'you' : 'another chef account';
}

/** London date and time, e.g. "Wed 4 Nov, 09:15". */
export function when(iso: string): string {
	return new Intl.DateTimeFormat('en-GB', {
		timeZone: 'Europe/London',
		weekday: 'short',
		day: 'numeric',
		month: 'short',
		hour: '2-digit',
		minute: '2-digit',
		hourCycle: 'h23'
	}).format(new Date(iso));
}

/** Other waiting requests that name the same order (future path: two accounts may ask). */
export function sameOrderRequests(c: StaffClaim, waiting: StaffClaim[]): StaffClaim[] {
	return c.order_id
		? waiting.filter((x) => x.id !== c.id && x.state === 'pending' && x.order_id === c.order_id)
		: [];
}

/** The sidebar count from the first waiting page: "50+" when there are more. */
export function countLabel(page: Pick<ClaimPage, 'claims' | 'next_cursor'>): string {
	return page.next_cursor ? `${page.claims.length}+` : String(page.claims.length);
}
