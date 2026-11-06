// The customer account API (GK-029; api/README "Customer accounts", routes
// in api/internal/httpapi/account.go): typed reads and commands with the
// exact JSON the API accepts (strict JSON, K00). Every command carries an
// Idempotency-Key (one per user action, reused on retry); the profile and
// claims also carry the version the customer saw. Private answers only: the
// caller is the signed-in customer (bearer token), never a guest.
import { apiGet, apiPatch, apiPost, type ApiOptions } from '../api';
import type { FulfillmentMode, Lifecycle, PaymentState } from '../checkout/types';

/** One row of GET /me/orders: the owner's summary of an order (no contact details or links). */
export interface AccountOrder {
	id: string;
	version: number;
	lifecycle: Lifecycle | string;
	cancel_reason: string | null;
	cancelled_at: string | null;
	payment_requirement: 'full' | 'none' | string;
	placed_at: string;
	date: string;
	fulfillment_mode: FulfillmentMode | string;
	slot: { id?: string; starts_at: string; ends_at: string } | null;
	fulfillment_state: string | null;
	total_minor: number;
	currency: string;
	payment: {
		state: PaymentState | string;
		amount_minor: number;
		expires_at: string | null;
		payment_page_open: boolean;
	} | null;
	free_confirmation: { state: string; reason: string | null } | null;
	cancellation: { state: string; reason: string | null; amount_minor: number | null } | null;
	appointment_change: { kind: string; state: string } | null;
	refund: {
		state: string;
		target_minor: number;
		succeeded_minor: number;
		in_progress_minor?: number;
		currency?: string;
	} | null;
	items: { cake_name: string; flavour: string; size_code: string; quantity: number }[];
	added_by: 'checkout' | 'claim' | 'chef' | string;
	added_at: string;
}

export type ClaimState = 'pending' | 'approved' | 'declined' | 'withdrawn';

/** An order claim as its customer sees it (never the chef's note). */
export interface Claim {
	id: string;
	version: number;
	state: ClaimState | string;
	path: 'evidence' | 'email_link' | string;
	channel: 'instagram' | 'offline' | 'website_email_link' | string;
	reference: string | null;
	approximate_date: string | null;
	note: string | null;
	created_at: string;
	decided_at: string | null;
	/** Only once approved. */
	order_id: string | null;
}

export interface Profile {
	full_name: string | null;
	phone: string | null;
	instagram: string | null;
	version: number;
	updated_at: string | null;
}

export interface Me {
	account_id: string;
	capabilities: string[];
	session_expires_at?: string;
}

/** Where and as whom the account API is called. */
export interface AccountApi {
	baseUrl: string;
	token: string;
	fetch?: typeof fetch;
}

/** Most orders an account page reads (50 per page, 10 pages). */
export const MAX_ORDER_PAGES = 10;
const PAGE = 50;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

function id(value: string): string {
	if (!UUID.test(value)) throw new Error(`not an ID: ${value}`);
	return value;
}

function opts(api: AccountApi, query?: ApiOptions['query']): ApiOptions {
	return { baseUrl: api.baseUrl, token: api.token, fetch: api.fetch, query };
}

function cmd(api: AccountApi, key: string) {
	return { ...opts(api), idempotencyKey: key };
}

export function me(api: AccountApi): Promise<Me> {
	return apiGet<Me>('/api/v1/me', opts(api));
}

/** Every order of the account, newest placed first (follows next_cursor). */
export async function myOrders(api: AccountApi): Promise<AccountOrder[]> {
	const all: AccountOrder[] = [];
	let cursor: string | undefined;
	for (let i = 0; i < MAX_ORDER_PAGES; i++) {
		const page = await apiGet<{ orders: AccountOrder[]; next_cursor?: string }>(
			'/api/v1/me/orders',
			opts(api, { limit: PAGE, cursor })
		);
		all.push(...page.orders);
		if (!page.next_cursor) return all;
		cursor = page.next_cursor;
	}
	throw new Error(`more than ${MAX_ORDER_PAGES * PAGE} orders`);
}

/** The account's claims, newest first (at most 50). */
export async function myClaims(api: AccountApi): Promise<Claim[]> {
	return (await apiGet<{ claims: Claim[] }>('/api/v1/me/order-claims', opts(api))).claims;
}

export function myClaim(api: AccountApi, claimId: string): Promise<Claim> {
	return apiGet<Claim>(`/api/v1/me/order-claims/${id(claimId)}`, opts(api));
}

export function profile(api: AccountApi): Promise<Profile> {
	return apiGet<Profile>('/api/v1/me/profile', opts(api));
}

/** Changes the named fields; null clears one, absent keeps it. */
export function updateProfile(
	api: AccountApi,
	key: string,
	patch: {
		expected_version: number;
		full_name?: string | null;
		phone?: string | null;
		instagram?: string | null;
	}
): Promise<Profile> {
	const body: Record<string, unknown> = { expected_version: patch.expected_version };
	for (const k of ['full_name', 'phone', 'instagram'] as const) {
		if (patch[k] !== undefined) body[k] = patch[k];
	}
	return apiPatch<Profile>('/api/v1/me/profile', body, cmd(api, key));
}

/** Asks the chef to add an older order (path c: the chef's reference and a note). */
export function claimOlderOrder(
	api: AccountApi,
	key: string,
	input: {
		channel: 'instagram' | 'offline' | 'website_email_link';
		reference: string;
		approximate_date?: string;
		note: string;
	}
): Promise<Claim> {
	const body: Record<string, string> = {
		channel: input.channel,
		reference: input.reference,
		note: input.note
	};
	if (input.approximate_date) body.approximate_date = input.approximate_date;
	return apiPost<Claim>('/api/v1/me/order-claims', body, cmd(api, key));
}

export function withdrawClaim(
	api: AccountApi,
	key: string,
	claimId: string,
	expectedVersion: number
): Promise<Claim> {
	return apiPost<Claim>(
		`/api/v1/me/order-claims/${id(claimId)}/withdraw`,
		{ expected_version: expectedVersion },
		cmd(api, key)
	);
}
