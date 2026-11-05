// Checkout orchestration (GK-025), independent of the page so it can be
// tested with a fake API. Commands carry one Idempotency-Key per user action,
// reused when that same action is retried; each new quote gets a fresh guest
// proof (the API refuses a reused one). Money is never computed here: totals
// are the quote's and the order's.
import { ApiError, newIdempotencyKey } from '../api';
import { newGuestProof, rememberLastOrder, saveOrderCapability } from '../guest';
import { quoteLines, type BasketLine } from './basket';
import { isPaymentPageUrl, nextPollDelay } from './order';
import { sizeLabel } from './format';
import type {
	Accepted,
	Allergen,
	Availability,
	Cake,
	CakePage,
	Choices,
	FulfillmentMode,
	Order,
	Policy,
	Quote,
	ReadyCake
} from './types';

/** Who is calling: a signed-in customer (token) or a guest (secret). */
export type Who = { kind: 'account'; token: string } | { kind: 'guest'; secret: string };

export interface CheckoutApi {
	cakesPage(cursor?: string): Promise<CakePage>;
	cake(id: string): Promise<Cake>;
	readyCakes(): Promise<ReadyCake[]>;
	policy(): Promise<Policy>;
	availability(from: string, to: string, quantity: number): Promise<Availability>;
	schedule(date: string, mode: FulfillmentMode, quantity: number): Promise<Choices>;
	createQuote(body: object, key: string, who?: Who): Promise<Quote>;
	checkout(body: object, key: string, who?: Who): Promise<Accepted>;
	order(id: string, who: Who): Promise<Order>;
	startPayment(id: string, version: number, key: string, who: Who): Promise<Accepted>;
	confirmFree(id: string, version: number, key: string, who: Who): Promise<Accepted>;
}

/** One Idempotency-Key (and guest proof) per distinct action. */
export class ActionKeys {
	private keys = new Map<string, { key: string; proof: string }>();
	constructor(
		private readonly makeKey = newIdempotencyKey,
		private readonly makeProof = () => newGuestProof()
	) {}

	/** The key and proof for an action, the same each time it is retried. */
	for(action: string): { key: string; proof: string } {
		let k = this.keys.get(action);
		if (!k) {
			k = { key: this.makeKey(), proof: this.makeProof() };
			this.keys.set(action, k);
		}
		return k;
	}

	/** Forget an action, so asking again is a new action (new key and proof). */
	forget(action: string): void {
		this.keys.delete(action);
	}
}

// ---------------------------------------------------------------- basket

export interface BasketItem {
	/** The variant or lot ID, as in the basket line. */
	id: string;
	ready: boolean;
	quantity: number;
	cakeName: string;
	flavour: string;
	size: string;
	servings?: string;
	allergens: Allergen[];
	composition: string[];
	coating: string;
	notice: string;
}

export class BasketProblem extends Error {
	constructor(readonly reason: 'unavailable' | 'not_found') {
		super(reason);
	}
}

/** The catalog pages scanned to find made-to-order variants (50 cakes each). */
const MAX_CATALOG_PAGES = 10;

/**
 * Looks up each basket line in the public catalog for its name and allergen
 * declaration (shown before paying, K01). Throws BasketProblem when a cake
 * is not on sale. Prices come later, from the quote.
 */
export async function resolveBasket(api: CheckoutApi, lines: BasketLine[]): Promise<BasketItem[]> {
	const wantVariants = new Set(lines.filter((l) => l.variantId).map((l) => l.variantId as string));
	const variantCake = new Map<string, Cake>();
	if (wantVariants.size > 0) {
		let cursor: string | undefined;
		for (let page = 0; page < MAX_CATALOG_PAGES; page++) {
			const p = await api.cakesPage(cursor);
			for (const cake of p.cakes)
				for (const v of cake.variants) if (wantVariants.has(v.id)) variantCake.set(v.id, cake);
			cursor = p.next_cursor;
			if (!cursor || variantCake.size === wantVariants.size) break;
		}
	}
	const lots = lines.some((l) => l.lotId) ? await api.readyCakes() : [];
	const cakes = new Map<string, Cake>();
	const items: BasketItem[] = [];
	for (const line of lines) {
		let cake: Cake | undefined;
		let variantId: string;
		let lot: ReadyCake | undefined;
		if (line.variantId) {
			variantId = line.variantId;
			cake = variantCake.get(variantId);
		} else {
			lot = lots.find((x) => x.id === line.lotId);
			if (!lot) throw new BasketProblem('unavailable');
			if (lot.available < line.quantity) throw new BasketProblem('unavailable');
			variantId = lot.variant_id;
			cake = cakes.get(lot.cake_id);
			if (!cake) {
				try {
					cake = await api.cake(lot.cake_id);
				} catch (e) {
					if (e instanceof ApiError && e.status === 404) throw new BasketProblem('unavailable');
					throw e;
				}
			}
		}
		if (!cake) throw new BasketProblem('not_found');
		cakes.set(cake.id, cake);
		const variant = cake.variants.find((v) => v.id === variantId);
		const recipe = variant?.recipe;
		// A variant without a current recipe (and so without its allergen
		// declaration) is not purchasable (K01).
		if (!variant || !recipe || (!lot && !variant.purchasable))
			throw new BasketProblem('unavailable');
		items.push({
			id: (line.variantId ?? line.lotId) as string,
			ready: Boolean(lot),
			quantity: line.quantity,
			cakeName: cake.name,
			flavour: variant.flavour,
			size: variant.size?.code ? sizeLabel(variant.size.code) : '',
			servings: recipe.servings
				? `${recipe.servings.min}–${recipe.servings.max} servings`
				: undefined,
			allergens: recipe.allergens,
			composition: recipe.composition,
			coating: recipe.coating,
			notice: recipe.allergen_notice?.text ?? ''
		});
	}
	return items;
}

// ---------------------------------------------------------------- quotes

export interface QuoteRequest {
	date: string;
	slotId: string;
	mode: FulfillmentMode;
	lines: BasketLine[];
	code?: string;
}

/** A quote must have at least this long left to be used for checkout. */
const QUOTE_MARGIN_MS = 2 * 60 * 1000;

function quoteAction(req: QuoteRequest, who: 'account' | 'guest'): string {
	return JSON.stringify(['quote', who, req.date, req.slotId, req.mode, req.lines, req.code ?? '']);
}

/**
 * Prices the basket for the chosen appointment on the server. Retrying the
 * same choice replays the same quote; an expired one is replaced by a new
 * quote (new key and proof). Returns the quote and, for a guest, its proof.
 */
export async function requestQuote(
	api: CheckoutApi,
	keys: ActionKeys,
	req: QuoteRequest,
	token: string | undefined,
	now: () => number = Date.now
): Promise<{ quote: Quote; proof?: string }> {
	const action = quoteAction(req, token ? 'account' : 'guest');
	for (let attempt = 0; attempt < 2; attempt++) {
		const { key, proof } = keys.for(action);
		const body: Record<string, unknown> = {
			date: req.date,
			slot_id: req.slotId,
			fulfillment_mode: req.mode,
			lines: quoteLines(req.lines)
		};
		if (req.code) body.discount_code = req.code;
		if (!token) body.guest_proof = proof;
		const quote = await api.createQuote(body, key, token ? { kind: 'account', token } : undefined);
		if (Date.parse(quote.expires_at) - now() > QUOTE_MARGIN_MS) {
			return token ? { quote } : { quote, proof };
		}
		keys.forget(action); // a replay of an old quote: ask for a new one
	}
	throw new ApiError(409, { code: 'QUOTE_EXPIRED', message: 'quote expired' });
}

/** Forget a quote choice so the next request makes a new quote (after PRICE_CHANGED and the like). */
export function forgetQuote(keys: ActionKeys, req: QuoteRequest, token: string | undefined): void {
	keys.forget(quoteAction(req, token ? 'account' : 'guest'));
}

// ---------------------------------------------------------------- placing

export interface Contact {
	full_name: string;
	email: string;
	payer_name?: string;
}

export type Placed =
	| { kind: 'redirect'; orderId: string; url: string }
	| { kind: 'order'; orderId: string; problem?: unknown };

export interface PlaceInput {
	quote: Quote;
	/** The guest proof of this quote; absent for a signed-in customer. */
	proof?: string;
	token?: string;
	policyId: string;
	contact: Contact;
}

export interface PlaceDeps {
	sleep?: (ms: number) => Promise<void>;
	/** How long to wait for the payment page before showing the order page instead. */
	paymentWaitMs?: number;
	now?: () => number;
	store?: Storage | null;
}

const defaultSleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

/**
 * Turns the reviewed quote into an order, then starts payment (or the free
 * confirmation of a £0 order). Refusals before the order exists are thrown
 * (the customer stays on checkout); once it exists the result always points
 * to the order, with any later problem attached.
 */
export async function placeOrder(
	api: CheckoutApi,
	keys: ActionKeys,
	input: PlaceInput,
	deps: PlaceDeps = {}
): Promise<Placed> {
	const sleep = deps.sleep ?? defaultSleep;
	const now = deps.now ?? Date.now;
	const guest = !input.token;
	if (guest && !input.proof) throw new Error('a guest checkout needs the quote proof');
	const body: Record<string, unknown> = {
		quote_id: input.quote.id,
		quote_version: input.quote.version,
		consented_policy_version: input.policyId,
		contact: { ...input.contact }
	};
	if (!input.contact.payer_name) delete (body.contact as Contact).payer_name;
	if (guest) body.guest_proof = input.proof;
	const placed = await api.checkout(
		body,
		keys.for(JSON.stringify(['checkout', body])).key,
		input.token ? { kind: 'account', token: input.token } : undefined
	);
	const orderId = placed.order_id;
	const who: Who = input.token
		? { kind: 'account', token: input.token }
		: { kind: 'guest', secret: input.proof as string };
	if (guest) saveOrderCapability(orderId, input.proof as string, deps.store);
	rememberLastOrder(orderId, deps.store);

	try {
		let order = await api.order(orderId, who);
		if (order.lifecycle !== 'awaiting_payment') return { kind: 'order', orderId };
		if (order.payment_requirement === 'none') {
			await api.confirmFree(
				orderId,
				order.version,
				keys.for(`free:${orderId}:${order.version}`).key,
				who
			);
			return { kind: 'order', orderId };
		}
		const p = order.payment;
		if (!p || p.state === 'failed' || p.state === 'cancelled') {
			const started = await api.startPayment(
				orderId,
				order.version,
				keys.for(`pay:${orderId}:${order.version}:${p?.id ?? ''}`).key,
				who
			);
			await sleep(Math.max(1, started.poll_after_seconds) * 1000);
		}
		// Wait for the worker to create the secure payment page.
		const until = now() + (deps.paymentWaitMs ?? 45_000);
		for (let attempt = 0; ; attempt++) {
			order = await api.order(orderId, who);
			const url = order.payment?.checkout_url;
			if (order.lifecycle === 'awaiting_payment' && isPaymentPageUrl(url)) {
				return { kind: 'redirect', orderId, url };
			}
			const state = order.payment?.state;
			if (order.lifecycle !== 'awaiting_payment' || (state !== 'created' && state !== 'pending')) {
				return { kind: 'order', orderId };
			}
			if (now() >= until) return { kind: 'order', orderId };
			await sleep(Math.min(nextPollDelay(attempt, 1), 5000));
		}
	} catch (problem) {
		return { kind: 'order', orderId, problem };
	}
}

/**
 * Starts (or retries) payment from the order page and waits for the secure
 * payment page. Returns its URL, or undefined when it is not ready yet.
 */
export async function startPaymentFromOrder(
	api: CheckoutApi,
	keys: ActionKeys,
	order: Order,
	who: Who,
	deps: PlaceDeps = {}
): Promise<string | undefined> {
	const sleep = deps.sleep ?? defaultSleep;
	const now = deps.now ?? Date.now;
	const started = await api.startPayment(
		order.id,
		order.version,
		keys.for(`pay:${order.id}:${order.version}:${order.payment?.id ?? ''}`).key,
		who
	);
	const until = now() + (deps.paymentWaitMs ?? 45_000);
	await sleep(Math.max(1, started.poll_after_seconds) * 1000);
	for (let attempt = 0; ; attempt++) {
		const o = await api.order(order.id, who);
		if (o.lifecycle === 'awaiting_payment' && isPaymentPageUrl(o.payment?.checkout_url)) {
			return o.payment?.checkout_url ?? undefined;
		}
		if (o.payment?.state !== 'created' && o.payment?.state !== 'pending') return undefined;
		if (now() >= until) return undefined;
		await sleep(Math.min(nextPollDelay(attempt, 1), 5000));
	}
}
