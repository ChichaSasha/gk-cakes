// Checkout and order client (GK-030): typed calls to the GK Cakes API's
// storefront routes (api/README.md "Catalog", "Schedule", "Discount codes and
// quotes", "Orders and checkout holds", "Payments"), the temporary basket
// entry, idempotency keys, guest order capabilities, customer messages for
// API refusals and the mapping from an order snapshot to what the order page
// says. Prices always come from the server (K03); nothing here adds them up.
import { ApiError, apiRequest, type ApiOptions } from './api';

// ---------------------------------------------------------------------------
// Wire types (only the fields the UI reads).

export type FulfillmentMode = 'collection' | 'customer_courier' | 'gk_delivery';

export interface AvailabilityDay {
	date: string;
	status: 'open' | 'full' | 'lead_time' | 'closed' | 'outside_horizon' | 'no_times';
	remaining_places: number | null;
}

export interface Availability {
	timezone: string;
	as_of: string;
	schedule_version: number | null;
	days: AvailabilityDay[];
}

export interface Slot {
	id: string;
	starts_at: string;
	ends_at: string;
	availability: 'available' | 'requestable' | 'unavailable';
	reason_code: string | null;
}

export interface ScheduleChoices {
	date: string;
	fulfillment_mode: FulfillmentMode;
	day_status: string;
	slots: Slot[];
}

export interface Allergen {
	code: string;
	label: string;
	prominent: boolean;
}

export interface Recipe {
	recipe_version_id: string;
	price_minor: number;
	currency: string;
	servings: { min: number; max: number };
	coating: string;
	composition: string[];
	allergens: Allergen[];
	allergen_notice: { id: string; text: string };
}

export interface Variant {
	id: string;
	flavour: string;
	size: { code: string; label: string; diameter: string };
	stock_mode: string;
	purchasable: boolean;
	recipe: Recipe | null;
}

export interface Cake {
	id: string;
	name: string;
	description: string;
	variants: Variant[];
}

export interface CakePage {
	cakes: Cake[];
	next_cursor?: string;
}

export interface ReadyCake {
	id: string;
	kind: string;
	cake_id: string;
	cake_name: string;
	variant_id: string;
	recipe_version_id: string;
	flavour: string;
	size: { code: string; label: string; diameter: string };
	price_minor: number;
	currency: string;
	available: number;
}

export interface Policy {
	id: string;
	summary: string;
	effective_at: string;
	source_decision_id: string;
}

export interface BasketLine {
	variant_id?: string;
	lot_id?: string;
	quantity: number;
}

export interface QuoteInput {
	date: string;
	slot_id: string;
	fulfillment_mode: FulfillmentMode;
	lines: BasketLine[];
	discount_code?: string;
	guest_proof?: string;
}

export interface QuoteLine {
	line_no: number;
	variant_id: string;
	lot_id: string | null;
	cake_name: string;
	flavour: string;
	size_code: string;
	quantity: number;
	unit_price_minor: number;
	discount_minor: number;
	net_minor: number;
}

export interface Quote {
	id: string;
	version: number;
	sent_at: string;
	expires_at: string;
	date: string;
	slot: { id: string; starts_at: string; ends_at: string };
	fulfillment_mode: FulfillmentMode;
	subtotal_minor: number;
	discount_minor: number;
	total_minor: number;
	currency: string;
	discount: { code: string; kind: string; basis_points: number | null } | null;
	lines: QuoteLine[];
}

export interface Contact {
	full_name: string;
	email: string;
	payer_name?: string;
}

export interface CheckoutInput {
	quote_id: string;
	quote_version: number;
	consented_policy_version: string;
	contact: Contact;
	guest_proof?: string;
}

/** A 202 answer: never an accepted order or a payment (K04). */
export interface Accepted {
	order_id: string;
	operation_id?: string;
	state: string;
	poll_after_seconds: number;
}

export interface OrderItem {
	line_no: number;
	variant_id: string;
	lot_id: string | null;
	cake_name: string;
	flavour: string;
	size_code: string;
	quantity: number;
	unit_price_minor: number;
	discount_minor: number;
	net_paid_minor: number;
}

export interface OrderPayment {
	id: string;
	state: 'created' | 'pending' | 'unknown' | 'succeeded' | 'failed' | 'cancelled';
	amount_minor: number;
	currency: string;
	requested_at: string;
	checkout_url: string | null;
	expires_at: string | null;
}

/** A refund block, when the API reports one (amounts in pence). */
export interface OrderRefund {
	state?: string;
	requested_minor?: number;
	returned_minor?: number;
}

export interface Order {
	id: string;
	version: number;
	lifecycle: string;
	cancel_reason: string | null;
	cancelled_at: string | null;
	payment_requirement: string;
	placed_at: string;
	date: string;
	fulfillment_mode: FulfillmentMode;
	slot: { id: string; starts_at: string; ends_at: string };
	full_refund_deadline_at: string;
	policy_version_id: string;
	customer_full_name: string;
	different_payer_name: string | null;
	contact_email: string;
	subtotal_minor: number;
	discount_minor: number;
	total_minor: number;
	currency: string;
	paid_at: string | null;
	free_confirmed_at: string | null;
	free_confirmation: { state: string; reason: string | null; requested_at: string } | null;
	payment: OrderPayment | null;
	refund?: OrderRefund | null;
	items: OrderItem[] | null;
}

// ---------------------------------------------------------------------------
// Calls. ApiOptions carries the API origin and, when signed in, the bearer.

export function getAvailability(
	opts: ApiOptions,
	from: string,
	to: string,
	quantity?: number
): Promise<Availability> {
	return apiRequest('/api/v1/availability', { query: { from, to, quantity } }, opts);
}

export function getSchedule(
	opts: ApiOptions,
	date: string,
	mode: FulfillmentMode,
	quantity?: number
): Promise<ScheduleChoices> {
	return apiRequest(
		'/api/v1/schedule',
		{ query: { date, fulfillment_mode: mode, purpose: 'new_purchase', quantity } },
		opts
	);
}

export function getCake(opts: ApiOptions, id: string): Promise<Cake> {
	return apiRequest(`/api/v1/cakes/${checkId(id)}`, {}, opts);
}

/**
 * Finds the active cakes holding the given variants by paging the public
 * catalog (there is no variant lookup route yet). Stops when every variant
 * is found or after maxPages pages.
 */
export async function findCakesForVariants(
	opts: ApiOptions,
	variantIds: string[],
	maxPages = 10
): Promise<Cake[]> {
	const wanted = new Set(variantIds);
	const found: Cake[] = [];
	let cursor: string | undefined;
	for (let page = 0; page < maxPages && wanted.size > 0; page++) {
		const res = await apiRequest<CakePage>('/api/v1/cakes', { query: { limit: 50, cursor } }, opts);
		for (const cake of res.cakes) {
			if (cake.variants.some((v) => wanted.has(v.id))) {
				found.push(cake);
				for (const v of cake.variants) wanted.delete(v.id);
			}
		}
		if (!res.next_cursor) break;
		cursor = res.next_cursor;
	}
	return found;
}

export async function getReadyCakes(opts: ApiOptions): Promise<ReadyCake[]> {
	const res = await apiRequest<{ cakes: ReadyCake[] }>('/api/v1/ready-cakes', {}, opts);
	return res.cakes;
}

export function getPolicy(opts: ApiOptions): Promise<Policy> {
	return apiRequest('/api/v1/policies/current', {}, opts);
}

export function createQuote(opts: ApiOptions, input: QuoteInput, key: string): Promise<Quote> {
	return apiRequest(
		'/api/v1/checkout/quotes',
		{ method: 'POST', body: input, headers: { 'Idempotency-Key': key } },
		opts
	);
}

export function createCheckout(
	opts: ApiOptions,
	input: CheckoutInput,
	key: string
): Promise<Accepted> {
	return apiRequest(
		'/api/v1/checkouts',
		{ method: 'POST', body: input, headers: { 'Idempotency-Key': key } },
		opts
	);
}

export function getOrder(opts: ApiOptions, id: string, capability?: string): Promise<Order> {
	return apiRequest(
		`/api/v1/orders/${checkId(id)}`,
		{ headers: { 'X-Order-Capability': opts.token ? undefined : capability } },
		opts
	);
}

export function startPayment(
	opts: ApiOptions,
	id: string,
	expectedVersion: number,
	key: string,
	capability?: string
): Promise<Accepted> {
	return orderCommand(opts, id, 'payment', expectedVersion, key, capability);
}

export function confirmFree(
	opts: ApiOptions,
	id: string,
	expectedVersion: number,
	key: string,
	capability?: string
): Promise<Accepted> {
	return orderCommand(opts, id, 'free-confirmation', expectedVersion, key, capability);
}

function orderCommand(
	opts: ApiOptions,
	id: string,
	command: 'payment' | 'free-confirmation',
	expectedVersion: number,
	key: string,
	capability?: string
): Promise<Accepted> {
	return apiRequest(
		`/api/v1/orders/${checkId(id)}/${command}`,
		{
			method: 'POST',
			body: { expected_version: expectedVersion },
			headers: {
				'Idempotency-Key': key,
				'X-Order-Capability': opts.token ? undefined : capability
			}
		},
		opts
	);
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

/** Whether s is a lower-case UUID (the API's resource IDs). */
export function isId(s: string | null | undefined): s is string {
	return typeof s === 'string' && UUID.test(s);
}

function checkId(id: string): string {
	if (!isId(id)) throw new Error(`not a resource ID: ${id}`);
	return id;
}

// ---------------------------------------------------------------------------
// Temporary basket entry: /checkout?item=<variant_id>&item=...&lot=<lot_id>.
// A repeated ID adds one more of that cake. Replaced by the approved
// discovery pages (GK-016) when they exist.

export const MAX_LINES = 20;
export const MAX_CAKES = 20;

export interface Basket {
	lines: BasketLine[];
	/** Made-to-order cakes (what day places count). */
	madeToOrder: number;
	problem?: string;
}

export function parseBasket(params: URLSearchParams): Basket {
	const counts = new Map<string, BasketLine>();
	let bad = false;
	for (const [name, raw] of params) {
		if (name !== 'item' && name !== 'lot') continue;
		const id = raw.trim().toLowerCase();
		if (!isId(id)) {
			bad = true;
			continue;
		}
		const k = `${name}:${id}`;
		const line: BasketLine =
			counts.get(k) ??
			(name === 'item' ? { variant_id: id, quantity: 0 } : { lot_id: id, quantity: 0 });
		line.quantity += 1;
		counts.set(k, line);
	}
	const lines = [...counts.values()];
	const cakes = lines.reduce((n, l) => n + l.quantity, 0);
	const madeToOrder = lines.filter((l) => l.variant_id).reduce((n, l) => n + l.quantity, 0);
	let problem: string | undefined;
	if (lines.length === 0) problem = bad ? 'This checkout link is not valid.' : undefined;
	else if (lines.length > MAX_LINES || cakes > MAX_CAKES)
		problem = `An online order can have at most ${MAX_CAKES} cakes.`;
	else if (bad) problem = 'Part of this checkout link is not valid.';
	return { lines, madeToOrder, problem };
}

// ---------------------------------------------------------------------------
// Basket details for the "Before you pay" section, from the public catalog.

export interface BasketItem {
	key: string;
	line: BasketLine;
	name: string;
	flavour: string;
	size: string;
	servings: string;
	/** Catalog unit price (pence), shown per cake before a quote exists. */
	unitPriceMinor: number;
	allergens: Allergen[];
	notice: string;
	composition: string;
	/** Set when the item cannot be bought (missing, unavailable or no disclosure). */
	problem?: string;
}

export function basketItems(
	lines: BasketLine[],
	cakes: Cake[],
	readyCakes: ReadyCake[]
): BasketItem[] {
	const variants = new Map<string, { cake: Cake; variant: Variant }>();
	for (const cake of cakes)
		for (const variant of cake.variants) variants.set(variant.id, { cake, variant });
	return lines.map((line) => {
		const lot = line.lot_id ? readyCakes.find((r) => r.id === line.lot_id) : undefined;
		const variantId = line.variant_id ?? lot?.variant_id;
		const found = variantId ? variants.get(variantId) : undefined;
		const recipe = found?.variant.recipe ?? null;
		const key = line.variant_id ? `item:${line.variant_id}` : `lot:${line.lot_id}`;
		const base: BasketItem = {
			key,
			line,
			name: found?.cake.name ?? lot?.cake_name ?? 'Cake',
			flavour: found?.variant.flavour ?? lot?.flavour ?? '',
			size: found?.variant.size.label ?? lot?.size.label ?? '',
			servings: recipe ? servingsLabel(recipe.servings) : '',
			unitPriceMinor: lot ? lot.price_minor : (recipe?.price_minor ?? 0),
			allergens: recipe?.allergens ?? [],
			notice: recipe?.allergen_notice.text ?? '',
			composition: recipe ? [...recipe.composition, recipe.coating].filter(Boolean).join(' · ') : ''
		};
		if (line.lot_id && (!lot || lot.available < line.quantity)) {
			return { ...base, problem: 'This ready cake is no longer available.' };
		}
		if (!found || !found.variant.purchasable || !recipe || recipe.allergens.length === 0) {
			return { ...base, problem: 'This cake cannot be ordered online at the moment.' };
		}
		if (lot && lot.recipe_version_id !== recipe.recipe_version_id) {
			// The physical cake was made to an earlier recipe; its allergen
			// list is not public, so it cannot be shown (K01: no disclosure, no sale).
			return {
				...base,
				problem:
					'We can’t show the allergen list for this ready cake. Please contact us to order it.'
			};
		}
		return base;
	});
}

function servingsLabel(s: { min: number; max: number }): string {
	return s.min === s.max ? `${s.min} servings` : `${s.min}–${s.max} servings`;
}

// ---------------------------------------------------------------------------
// Idempotency keys and guest secrets.

/** A fresh Idempotency-Key (1-128 visible characters). */
export function newIdempotencyKey(): string {
	return crypto.randomUUID();
}

/**
 * Remembers one key per user action. Retrying the same action with the same
 * body reuses its key, so a lost answer is replayed instead of repeated; a
 * changed body is a new action with a new key.
 */
export class ActionKeys {
	#keys = new Map<string, { body: string; key: string }>();
	#make: () => string;

	constructor(make: () => string = newIdempotencyKey) {
		this.#make = make;
	}

	keyFor(action: string, body: unknown): string {
		const text = JSON.stringify(body);
		const known = this.#keys.get(action);
		if (known && known.body === text) return known.key;
		const key = this.#make();
		this.#keys.set(action, { body: text, key });
		return key;
	}

	/** Forgets an action once it is settled, so the next one gets a new key. */
	forget(action: string): void {
		this.#keys.delete(action);
	}
}

/** A fresh guest proof: 32 random bytes as unpadded base64url (43 characters). */
export function newGuestProof(
	random: (bytes: Uint8Array<ArrayBuffer>) => Uint8Array = (b) => crypto.getRandomValues(b)
): string {
	const bytes = random(new Uint8Array(32));
	let binary = '';
	for (const b of bytes) binary += String.fromCharCode(b);
	return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

type KeyValueStore = Pick<Storage, 'getItem' | 'setItem' | 'removeItem' | 'key' | 'length'>;

const CAPABILITY_PREFIX = 'gk-order-capability:';
/** How long this browser keeps a guest order's capability. */
export const CAPABILITY_TTL_MS = 30 * 24 * 3_600_000;

/**
 * Keeps a guest order's capability (the quote's proof) in this browser so the
 * order page can read and pay for it (X-Order-Capability). Expired entries
 * are removed on every save.
 */
export function saveOrderCapability(
	store: KeyValueStore,
	orderId: string,
	proof: string,
	now: number = Date.now()
): void {
	pruneCapabilities(store, now);
	store.setItem(
		CAPABILITY_PREFIX + checkId(orderId),
		JSON.stringify({ proof, expires: now + CAPABILITY_TTL_MS })
	);
}

/** The stored capability for a guest order, or undefined. */
export function orderCapability(
	store: KeyValueStore,
	orderId: string,
	now: number = Date.now()
): string | undefined {
	if (!isId(orderId)) return undefined;
	const raw = store.getItem(CAPABILITY_PREFIX + orderId);
	try {
		const saved = JSON.parse(raw ?? 'null') as { proof?: unknown; expires?: unknown } | null;
		if (
			saved &&
			typeof saved.proof === 'string' &&
			typeof saved.expires === 'number' &&
			saved.expires > now
		) {
			return saved.proof;
		}
	} catch {
		// unreadable: treated as absent
	}
	if (raw !== null) store.removeItem(CAPABILITY_PREFIX + orderId);
	return undefined;
}

function pruneCapabilities(store: KeyValueStore, now: number): void {
	const keys: string[] = [];
	for (let i = 0; i < store.length; i++) {
		const k = store.key(i);
		if (k?.startsWith(CAPABILITY_PREFIX)) keys.push(k);
	}
	for (const k of keys) orderCapability(store, k.slice(CAPABILITY_PREFIX.length), now);
}

// ---------------------------------------------------------------------------
// Customer messages for API refusals (K03/K04 wording).

export type ProblemKind =
	| 'price_changed'
	| 'quote_expired'
	| 'slot'
	| 'item'
	| 'code'
	| 'lead_time'
	| 'in_progress'
	| 'policy'
	| 'stale'
	| 'retry'
	| 'not_found'
	| 'sign_in'
	| 'other';

export interface Problem {
	kind: ProblemKind;
	message: string;
	/** The form field the problem belongs to, if any. */
	field?: 'slot' | 'code' | 'reviewed';
}

export function checkoutProblem(err: unknown): Problem {
	if (!(err instanceof ApiError)) {
		return { kind: 'other', message: 'Something went wrong. Please try again.' };
	}
	switch (err.code) {
		case 'PRICE_CHANGED':
			return {
				kind: 'price_changed',
				message: 'A price has changed. Please check the new total, then continue.'
			};
		case 'QUOTE_EXPIRED':
			return {
				kind: 'quote_expired',
				message: 'Your total needed refreshing. Please check it, then continue.'
			};
		case 'SLOT_UNAVAILABLE':
			return {
				kind: 'slot',
				field: 'slot',
				message: 'That time is no longer available. Please choose another time or date.'
			};
		case 'ITEM_UNAVAILABLE':
			return {
				kind: 'item',
				message: 'A cake in your order is no longer available. Please choose again.'
			};
		case 'CODE_USED_UP':
			return {
				kind: 'code',
				field: 'code',
				message: 'This code has been fully used. Remove it to continue.'
			};
		case 'LEAD_TIME':
			return {
				kind: 'lead_time',
				field: 'slot',
				message: 'This time is now too soon for us to make your cake. Please choose a later time.'
			};
		case 'PAYMENT_IN_PROGRESS':
			return {
				kind: 'in_progress',
				message: 'A payment for this order is already in progress. Please don’t pay again.'
			};
		case 'POLICY_CHANGED':
			return {
				kind: 'policy',
				field: 'reviewed',
				message: 'Our refund terms have just been updated. Please read them and tick again.'
			};
		case 'STALE_VERSION':
			return { kind: 'stale', message: 'Your order has just changed. Here is its latest status.' };
		case 'INVALID_INPUT':
			if (err.message.startsWith('discount_code')) return codeProblem(err.message);
			break;
		case 'NOT_FOUND':
			return { kind: 'not_found', message: 'We couldn’t find this order from this browser.' };
	}
	if (err.status === 401) {
		return { kind: 'sign_in', message: 'Your session has ended. Please sign in again.' };
	}
	if (err.status === 0 || err.status >= 500 || err.retryable) {
		return {
			kind: 'retry',
			message: 'Checkout is temporarily unavailable. Please try again in a few minutes.'
		};
	}
	return { kind: 'other', message: 'Something went wrong. Please try again.' };
}

function codeProblem(message: string): Problem {
	let text = 'We don’t recognise that code. Check the spelling, or continue without one.';
	if (message.includes('sign in')) text = 'This code needs you to sign in before you use it.';
	else if (message.includes('GK delivery'))
		text = 'This code is for GK delivery, which isn’t available online yet.';
	return { kind: 'code', field: 'code', message: text };
}

// ---------------------------------------------------------------------------
// Order status: what the order page says, from the authoritative snapshot.

export type OrderStatusKind =
	| 'awaiting_payment'
	| 'preparing_payment'
	| 'checking'
	| 'payment_received'
	| 'confirmation_pending'
	| 'confirmed'
	| 'paid_awaiting_agreement'
	| 'cancelled';

export type OrderAction =
	{ kind: 'open'; url: string } | { kind: 'start' } | { kind: 'free' } | { kind: 'none' };

export interface OrderStatus {
	kind: OrderStatusKind;
	title: string;
	message: string;
	/** Show "Please don't pay again". */
	doNotPayAgain: boolean;
	/** No further change is expected without someone acting: stop polling. */
	terminal: boolean;
	action: OrderAction;
	tone: 'info' | 'success' | 'error';
}

const CONFIRMED_STAGES = new Set(['confirmed', 'in_production', 'ready', 'completed']);

const FREE_REFUSALS: Record<string, string> = {
	order_changed: 'your order changed',
	holds_released: 'your place was released',
	window_closed: 'the time to confirm has passed'
};

export function orderStatus(order: Order): OrderStatus {
	const pay = order.payment;
	const free = order.payment_requirement === 'none' || order.total_minor === 0;
	const lifecycle = order.lifecycle;

	if (lifecycle === 'cancelled') {
		const paid = pay?.state === 'succeeded';
		const reason =
			order.cancel_reason === 'payment_not_completed'
				? 'The payment wasn’t completed in time, so your place was released.'
				: order.cancel_reason === 'free_not_confirmed'
					? 'The order wasn’t confirmed in time, so your place was released.'
					: 'This order was cancelled.';
		return {
			kind: 'cancelled',
			title: 'This order was cancelled',
			message: paid
				? `${reason} We have received a payment for it; the chef will contact you about it.`
				: reason,
			doNotPayAgain: paid,
			terminal: true,
			action: { kind: 'none' },
			tone: 'error'
		};
	}
	if (CONFIRMED_STAGES.has(lifecycle)) {
		return {
			kind: 'confirmed',
			title: 'Your order is confirmed',
			message: free
				? 'Thank you. Your order is confirmed.'
				: 'Thank you. We have received your payment and your order is confirmed.',
			doNotPayAgain: false,
			terminal: true,
			action: { kind: 'none' },
			tone: 'success'
		};
	}
	if (lifecycle === 'paid_awaiting_agreement') {
		return {
			kind: 'paid_awaiting_agreement',
			title: 'Payment received',
			message:
				'Your payment arrived too late for us to promise this date. The chef will contact you about the date.',
			doNotPayAgain: true,
			terminal: true,
			action: { kind: 'none' },
			tone: 'info'
		};
	}
	if (
		lifecycle === 'confirmation_pending' ||
		(lifecycle === 'awaiting_payment' && order.free_confirmation?.state === 'processing')
	) {
		return {
			kind: 'confirmation_pending',
			title: free ? 'Confirming your order' : 'Payment received',
			message: free
				? 'We’re securing your confirmation. This usually takes a few seconds.'
				: 'Payment received, confirmation being secured. This usually takes a few seconds.',
			doNotPayAgain: !free,
			terminal: false,
			action: { kind: 'none' },
			tone: 'info'
		};
	}
	if (lifecycle !== 'awaiting_payment') {
		return {
			kind: 'checking',
			title: 'We’re checking your order',
			message: 'Please check back shortly.',
			doNotPayAgain: false,
			terminal: false,
			action: { kind: 'none' },
			tone: 'info'
		};
	}
	// Awaiting payment.
	switch (pay?.state) {
		case 'succeeded':
			// A recorded payment whose confirmation was refused (the order changed).
			return {
				kind: 'payment_received',
				title: 'Payment received',
				message:
					'We have received your payment and are checking your order. The chef will contact you.',
				doNotPayAgain: true,
				terminal: false,
				action: { kind: 'none' },
				tone: 'info'
			};
		case 'unknown':
			return checking();
		case 'created':
			return {
				kind: 'preparing_payment',
				title: 'Payment pending',
				message: 'We’re preparing your secure payment page.',
				doNotPayAgain: true,
				terminal: false,
				action: { kind: 'none' },
				tone: 'info'
			};
		case 'pending': {
			const url = safeCheckoutUrl(pay.checkout_url);
			if (!url) return checking();
			return {
				kind: 'awaiting_payment',
				title: 'Payment pending',
				message:
					'Your secure payment page is ready. Your order is confirmed only when we receive the payment.',
				doNotPayAgain: false,
				terminal: false,
				action: { kind: 'open', url },
				tone: 'info'
			};
		}
	}
	if (free) {
		const refused = order.free_confirmation?.state === 'refused';
		const why = FREE_REFUSALS[order.free_confirmation?.reason ?? ''];
		return {
			kind: 'awaiting_payment',
			title: 'Confirm your order',
			message: refused
				? `We couldn’t confirm your order${why ? ` because ${why}` : ''}. You can try again.`
				: 'There is nothing to pay. Confirm your order to secure your place.',
			doNotPayAgain: false,
			terminal: false,
			action: { kind: 'free' },
			tone: refused ? 'error' : 'info'
		};
	}
	return {
		kind: 'awaiting_payment',
		title: 'Waiting for payment',
		message:
			pay?.state === 'failed'
				? 'That payment didn’t go through. You can try again.'
				: pay?.state === 'cancelled'
					? 'That payment wasn’t completed. You can start it again.'
					: 'Your order is confirmed only when we receive the payment.',
		doNotPayAgain: false,
		terminal: false,
		action: { kind: 'start' },
		tone: pay?.state === 'failed' ? 'error' : 'info'
	};
}

function checking(): OrderStatus {
	return {
		kind: 'checking',
		title: 'Still checking payment',
		message: 'Still checking payment; do not pay again. We’ll update this page when we know.',
		doNotPayAgain: true,
		terminal: false,
		action: { kind: 'none' },
		tone: 'info'
	};
}

export interface RefundView {
	requestedMinor: number;
	returnedMinor: number;
	/** True only when the API reports the refund finished and fully returned. */
	done: boolean;
}

/** The refund block for the order page; never "refunded" while anything is pending. */
export function refundView(refund: OrderRefund | null | undefined): RefundView | undefined {
	if (!refund || typeof refund.requested_minor !== 'number') return undefined;
	const returned = refund.returned_minor ?? 0;
	return {
		requestedMinor: refund.requested_minor,
		returnedMinor: returned,
		done:
			(refund.state === 'succeeded' || refund.state === 'completed') &&
			returned >= refund.requested_minor
	};
}

/** Only Stripe's hosted Checkout may be opened as the payment page. */
export function safeCheckoutUrl(url: string | null | undefined): string | undefined {
	if (!url) return undefined;
	try {
		const u = new URL(url);
		return u.protocol === 'https:' && u.hostname === 'checkout.stripe.com' && !u.username
			? u.href
			: undefined;
	} catch {
		return undefined;
	}
}

/** Seconds until the next order poll: the API's hint, backing off to 30 s. */
export function nextPollSeconds(attempt: number, hint = 3): number {
	const base = Number.isFinite(hint) && hint > 0 ? hint : 3;
	return Math.min(30, Math.round(base * 1.5 ** Math.max(0, attempt)));
}

/** Whether a quote can still be used to check out (a minute of margin). */
export function quoteUsable(quote: Quote, now: number = Date.now()): boolean {
	return Date.parse(quote.expires_at) - now > 60_000;
}

// ---------------------------------------------------------------------------
// Checkout → order page hand-off (this tab only, sessionStorage).

export interface Handoff {
	/** Open the Stripe page as soon as the order shows it (set by checkout only). */
	autopay?: boolean;
	/** A message to show once on the order page. */
	message?: string;
	/** The API's poll hint from the last 202. */
	pollAfter?: number;
}

const HANDOFF_PREFIX = 'gk-order-handoff:';

export function setHandoff(store: KeyValueStore, orderId: string, handoff: Handoff): void {
	store.setItem(HANDOFF_PREFIX + checkId(orderId), JSON.stringify(handoff));
}

/** Reads and removes the hand-off for an order (shown once). */
export function takeHandoff(store: KeyValueStore, orderId: string): Handoff {
	if (!isId(orderId)) return {};
	const raw = store.getItem(HANDOFF_PREFIX + orderId);
	store.removeItem(HANDOFF_PREFIX + orderId);
	try {
		const h = JSON.parse(raw ?? '{}') as Record<string, unknown>;
		return {
			autopay: h.autopay === true,
			message: typeof h.message === 'string' ? h.message : undefined,
			pollAfter: typeof h.pollAfter === 'number' ? h.pollAfter : undefined
		};
	} catch {
		return {};
	}
}

/** The query of the temporary checkout link for the same cakes (to start again). */
export function restartQuery(items: OrderItem[] | null): string {
	const q = new URLSearchParams();
	for (const i of items ?? []) {
		for (let n = 0; n < i.quantity; n++) {
			if (i.lot_id) q.append('lot', i.lot_id);
			else q.append('item', i.variant_id);
		}
	}
	return q.size > 0 ? `?${q}` : '';
}
