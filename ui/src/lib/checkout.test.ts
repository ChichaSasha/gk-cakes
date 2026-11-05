import { describe, expect, it } from 'vitest';
import { ApiError } from './api';
import {
	ActionKeys,
	CAPABILITY_TTL_MS,
	basketItems,
	checkoutProblem,
	confirmFree,
	createCheckout,
	createQuote,
	findCakesForVariants,
	getOrder,
	getSchedule,
	newGuestProof,
	nextPollSeconds,
	orderCapability,
	orderStatus,
	parseBasket,
	quoteUsable,
	restartQuery,
	setHandoff,
	takeHandoff,
	refundView,
	safeCheckoutUrl,
	saveOrderCapability,
	startPayment,
	type Cake,
	type Order,
	type Quote,
	type ReadyCake
} from './checkout';

const base = 'https://staging-api.gkcake.com';
const ORDER = '0b4c2a5e-1f3d-4c6b-9a8e-7d6c5b4a3f21';
const V1 = '11111111-1111-4111-8111-111111111111';
const V2 = '22222222-2222-4222-8222-222222222222';
const LOT = '33333333-3333-4333-8333-333333333333';
const PROOF = 'A'.repeat(43);

function recorder(status = 200, body: unknown = {}) {
	const seen: Request[] = [];
	const fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
		seen.push(new Request(input, init));
		return new Response(JSON.stringify(body), { status });
	}) as typeof globalThis.fetch;
	return { seen, fetch };
}

describe('request building', () => {
	it('creates a quote with the key header and a JSON body, no cookies', async () => {
		const { seen, fetch } = recorder(201, { id: 'q' });
		const input = {
			date: '2026-10-22',
			slot_id: 's1',
			fulfillment_mode: 'collection' as const,
			lines: [{ variant_id: V1, quantity: 2 }],
			guest_proof: PROOF
		};
		await createQuote({ baseUrl: base, fetch }, input, 'key-1');
		const r = seen[0];
		expect(r.method).toBe('POST');
		expect(r.url).toBe(`${base}/api/v1/checkout/quotes`);
		expect(r.headers.get('idempotency-key')).toBe('key-1');
		expect(r.headers.get('content-type')).toBe('application/json');
		expect(r.headers.has('authorization')).toBe(false);
		expect(r.credentials).toBe('omit');
		expect(await r.json()).toEqual(input);
	});

	it('checks out with the bearer when signed in', async () => {
		const { seen, fetch } = recorder(202, { order_id: ORDER, state: 'pending' });
		await createCheckout(
			{ baseUrl: base, token: 'tok', fetch },
			{
				quote_id: 'q',
				quote_version: 1,
				consented_policy_version: 'p',
				contact: { full_name: 'A B', email: 'a@example.com' }
			},
			'key-2'
		);
		expect(seen[0].headers.get('authorization')).toBe('Bearer tok');
		expect((await seen[0].json()).guest_proof).toBeUndefined();
	});

	it('sends the guest capability only as a header, and never with a bearer', async () => {
		const guest = recorder(200, {});
		await getOrder({ baseUrl: base, fetch: guest.fetch }, ORDER, PROOF);
		expect(guest.seen[0].url).toBe(`${base}/api/v1/orders/${ORDER}`);
		expect(guest.seen[0].headers.get('x-order-capability')).toBe(PROOF);
		expect(guest.seen[0].url).not.toContain(PROOF);
		const owner = recorder(200, {});
		await getOrder({ baseUrl: base, token: 'tok', fetch: owner.fetch }, ORDER, PROOF);
		expect(owner.seen[0].headers.has('x-order-capability')).toBe(false);
	});

	it('starts a payment or a free confirmation at the expected version', async () => {
		const { seen, fetch } = recorder(202, {});
		await startPayment({ baseUrl: base, fetch }, ORDER, 4, 'k3', PROOF);
		await confirmFree({ baseUrl: base, fetch }, ORDER, 2, 'k4', PROOF);
		expect(seen[0].url).toBe(`${base}/api/v1/orders/${ORDER}/payment`);
		expect(await seen[0].json()).toEqual({ expected_version: 4 });
		expect(seen[0].headers.get('idempotency-key')).toBe('k3');
		expect(seen[1].url).toBe(`${base}/api/v1/orders/${ORDER}/free-confirmation`);
		expect(await seen[1].json()).toEqual({ expected_version: 2 });
	});

	it('puts query parameters in the query string and refuses non-IDs in paths', async () => {
		const { seen, fetch } = recorder(200, { slots: [] });
		await getSchedule({ baseUrl: base, fetch }, '2026-10-22', 'customer_courier', 3);
		const url = new URL(seen[0].url);
		expect(url.pathname).toBe('/api/v1/schedule');
		expect(Object.fromEntries(url.searchParams)).toEqual({
			date: '2026-10-22',
			fulfillment_mode: 'customer_courier',
			purpose: 'new_purchase',
			quantity: '3'
		});
		expect(() => getOrder({ baseUrl: base, fetch }, '../me')).toThrow('not a resource ID');
	});

	it('pages the catalog until every variant is found', async () => {
		const pages: Record<string, unknown> = {
			'': { cakes: [{ id: 'c1', variants: [{ id: V1 }] }], next_cursor: 'n2' },
			n2: { cakes: [{ id: 'c2', variants: [{ id: V2 }] }], next_cursor: 'n3' }
		};
		const seen: string[] = [];
		const fetch = (async (input: RequestInfo | URL) => {
			const u = new URL(String(input));
			seen.push(u.search);
			return new Response(JSON.stringify(pages[u.searchParams.get('cursor') ?? '']));
		}) as typeof globalThis.fetch;
		const cakes = await findCakesForVariants({ baseUrl: base, fetch }, [V1, V2]);
		expect(cakes.map((c) => c.id)).toEqual(['c1', 'c2']);
		expect(seen).toEqual(['?limit=50', '?limit=50&cursor=n2']);
	});
});

describe('ActionKeys', () => {
	it('reuses the key when the same action is retried with the same body', () => {
		let n = 0;
		const keys = new ActionKeys(() => `k${++n}`);
		const body = { quote_id: 'q', contact: { email: 'a@example.com' } };
		expect(keys.keyFor('checkout', body)).toBe('k1');
		expect(keys.keyFor('checkout', { ...body })).toBe('k1');
		expect(keys.keyFor('payment', { expected_version: 1 })).toBe('k2');
		expect(keys.keyFor('checkout', { ...body, quote_id: 'q2' })).toBe('k3');
		keys.forget('payment');
		expect(keys.keyFor('payment', { expected_version: 1 })).toBe('k4');
	});

	it('makes 1-128 visible-character keys by default', () => {
		const key = new ActionKeys().keyFor('a', {});
		expect(key).toMatch(/^[\x21-\x7e]{1,128}$/);
	});
});

describe('guest proof and capability', () => {
	it('is 32 random bytes in unpadded base64url', () => {
		const proof = newGuestProof();
		expect(proof).toMatch(/^[A-Za-z0-9_-]{43}$/);
		expect(newGuestProof()).not.toBe(proof);
		expect(newGuestProof((b) => b.fill(0xff))).toBe('_'.repeat(42) + '8');
	});

	it('hands off to the order page once', () => {
		const store = memory();
		setHandoff(store, ORDER, { autopay: true, message: 'm', pollAfter: 3 });
		expect(takeHandoff(store, ORDER)).toEqual({ autopay: true, message: 'm', pollAfter: 3 });
		expect(takeHandoff(store, ORDER)).toEqual({ autopay: false });
		expect(takeHandoff(store, 'nope')).toEqual({});
	});

	function memory(): Storage {
		const m = new Map<string, string>();
		return {
			get length() {
				return m.size;
			},
			key: (i: number) => [...m.keys()][i] ?? null,
			getItem: (k: string) => m.get(k) ?? null,
			setItem: (k: string, v: string) => void m.set(k, v),
			removeItem: (k: string) => void m.delete(k),
			clear: () => m.clear()
		};
	}

	it('keeps an order capability for 30 days, keyed by order', () => {
		const store = memory();
		const now = Date.parse('2026-10-01T00:00:00Z');
		saveOrderCapability(store, ORDER, PROOF, now);
		expect(orderCapability(store, ORDER, now + 1000)).toBe(PROOF);
		expect(orderCapability(store, V1, now)).toBeUndefined();
		expect(orderCapability(store, ORDER, now + CAPABILITY_TTL_MS)).toBeUndefined();
		expect(store.length).toBe(0);
	});

	it('removes expired and unreadable capabilities when saving another', () => {
		const store = memory();
		const now = Date.parse('2026-10-01T00:00:00Z');
		saveOrderCapability(store, V1, PROOF, now);
		store.setItem(`gk-order-capability:${V2}`, 'not json');
		saveOrderCapability(store, ORDER, PROOF, now + CAPABILITY_TTL_MS);
		expect(store.length).toBe(1);
		expect(orderCapability(store, ORDER, now + CAPABILITY_TTL_MS)).toBe(PROOF);
	});
});

describe('parseBasket', () => {
	it('reads variants and lots, counting repeats as quantity', () => {
		const b = parseBasket(new URLSearchParams(`item=${V1}&item=${V1}&lot=${LOT}&item=${V2}`));
		expect(b.lines).toEqual([
			{ variant_id: V1, quantity: 2 },
			{ lot_id: LOT, quantity: 1 },
			{ variant_id: V2, quantity: 1 }
		]);
		expect(b.madeToOrder).toBe(3);
		expect(b.problem).toBeUndefined();
	});

	it('reports invalid links and too many cakes', () => {
		expect(parseBasket(new URLSearchParams('item=../x')).problem).toBe(
			'This checkout link is not valid.'
		);
		expect(parseBasket(new URLSearchParams(`item=${V1}&item=bad`)).problem).toBe(
			'Part of this checkout link is not valid.'
		);
		const many = new URLSearchParams(Array.from({ length: 21 }, () => ['item', V1]));
		expect(parseBasket(many).problem).toBe('An online order can have at most 20 cakes.');
		expect(parseBasket(new URLSearchParams('')).lines).toEqual([]);
	});
});

describe('basketItems', () => {
	const recipe = {
		recipe_version_id: 'r1',
		price_minor: 9000,
		currency: 'GBP',
		servings: { min: 7, max: 10 },
		coating: 'Swiss meringue buttercream',
		composition: ['Vanilla sponge'],
		allergens: [{ code: 'peanuts', label: 'PEANUTS', prominent: true }],
		allergen_notice: { id: 'n', text: 'Shared kitchen.' }
	};
	const cake: Cake = {
		id: 'c1',
		name: 'Pink piping',
		description: '',
		variants: [
			{
				id: V1,
				flavour: 'Vanilla Raspberry',
				size: { code: 's', label: 'S', diameter: '6 inches' },
				stock_mode: 'made_to_order',
				purchasable: true,
				recipe
			}
		]
	};
	const lot = (recipeId: string): ReadyCake => ({
		id: LOT,
		kind: 'ready',
		cake_id: 'c1',
		cake_name: 'Pink piping',
		variant_id: V1,
		recipe_version_id: recipeId,
		flavour: 'Vanilla Raspberry',
		size: { code: 's', label: 'S', diameter: '6 inches' },
		price_minor: 7000,
		currency: 'GBP',
		available: 1
	});

	it('joins catalog details and disclosures', () => {
		const [item] = basketItems([{ variant_id: V1, quantity: 1 }], [cake], []);
		expect(item.name).toBe('Pink piping');
		expect(item.servings).toBe('7–10 servings');
		expect(item.allergens[0].label).toBe('PEANUTS');
		expect(item.notice).toBe('Shared kitchen.');
		expect(item.problem).toBeUndefined();
	});

	it('uses the lot price and blocks a ready cake whose recipe is not the public one', () => {
		const [ok] = basketItems([{ lot_id: LOT, quantity: 1 }], [cake], [lot('r1')]);
		expect(ok.unitPriceMinor).toBe(7000);
		expect(ok.problem).toBeUndefined();
		const [old] = basketItems([{ lot_id: LOT, quantity: 1 }], [cake], [lot('r0')]);
		expect(old.problem).toMatch(/allergen list/);
		const [gone] = basketItems([{ lot_id: LOT, quantity: 2 }], [cake], [lot('r1')]);
		expect(gone.problem).toMatch(/no longer available/);
	});

	it('blocks a missing cake or one without an allergen declaration', () => {
		expect(basketItems([{ variant_id: V2, quantity: 1 }], [cake], [])[0].problem).toBeTruthy();
		const bare = structuredClone(cake);
		bare.variants[0].recipe!.allergens = [];
		expect(basketItems([{ variant_id: V1, quantity: 1 }], [bare], [])[0].problem).toBeTruthy();
	});
});

describe('checkoutProblem', () => {
	const err = (status: number, code: string, message = '', retryable = false) =>
		new ApiError(status, { code, message, retryable });

	it('maps the 409 refusals to the sketch messages', () => {
		expect(checkoutProblem(err(409, 'PRICE_CHANGED')).kind).toBe('price_changed');
		expect(checkoutProblem(err(409, 'QUOTE_EXPIRED')).kind).toBe('quote_expired');
		expect(checkoutProblem(err(409, 'SLOT_UNAVAILABLE'))).toMatchObject({
			kind: 'slot',
			field: 'slot'
		});
		expect(checkoutProblem(err(409, 'ITEM_UNAVAILABLE')).kind).toBe('item');
		expect(checkoutProblem(err(409, 'CODE_USED_UP'))).toMatchObject({ field: 'code' });
		expect(checkoutProblem(err(409, 'LEAD_TIME'))).toMatchObject({ field: 'slot' });
		const inProgress = checkoutProblem(err(409, 'PAYMENT_IN_PROGRESS'));
		expect(inProgress.kind).toBe('in_progress');
		expect(inProgress.message).toContain('don’t pay again');
		expect(checkoutProblem(err(409, 'POLICY_CHANGED'))).toMatchObject({ field: 'reviewed' });
	});

	it('maps code refusals, retryable failures and the rest', () => {
		expect(
			checkoutProblem(err(422, 'INVALID_INPUT', 'discount_code is not a current code'))
		).toMatchObject({ kind: 'code', field: 'code' });
		expect(
			checkoutProblem(err(422, 'INVALID_INPUT', 'discount_code needs you to sign in')).message
		).toContain('sign in');
		expect(checkoutProblem(err(503, 'UNAVAILABLE', '', true)).kind).toBe('retry');
		expect(checkoutProblem(new ApiError(0, { code: 'NETWORK' })).kind).toBe('retry');
		expect(checkoutProblem(err(401, 'UNAUTHENTICATED')).kind).toBe('sign_in');
		expect(checkoutProblem(err(400, 'BAD_REQUEST')).kind).toBe('other');
		expect(checkoutProblem(new Error('x')).kind).toBe('other');
	});
});

function order(over: Partial<Order> = {}): Order {
	return {
		id: ORDER,
		version: 1,
		lifecycle: 'awaiting_payment',
		cancel_reason: null,
		cancelled_at: null,
		payment_requirement: 'full',
		placed_at: '2026-10-01T10:00:00Z',
		date: '2026-10-22',
		fulfillment_mode: 'collection',
		slot: { id: 's', starts_at: '2026-10-22T10:00:00Z', ends_at: '2026-10-22T11:00:00Z' },
		full_refund_deadline_at: '2026-10-17T10:00:00Z',
		policy_version_id: 'p',
		customer_full_name: 'A B',
		different_payer_name: null,
		contact_email: 'a@example.com',
		subtotal_minor: 9000,
		discount_minor: 0,
		total_minor: 9000,
		currency: 'GBP',
		paid_at: null,
		free_confirmed_at: null,
		free_confirmation: null,
		payment: null,
		items: [],
		...over
	};
}

function payment(state: string, checkout_url: string | null = null): Order['payment'] {
	return {
		id: 'pc',
		state: state as NonNullable<Order['payment']>['state'],
		amount_minor: 9000,
		currency: 'GBP',
		requested_at: '2026-10-01T10:00:00Z',
		checkout_url,
		expires_at: null
	};
}

describe('orderStatus', () => {
	const stripe = 'https://checkout.stripe.com/c/pay/cs_test_1';

	it('offers to start a payment when none is in progress', () => {
		expect(orderStatus(order())).toMatchObject({
			kind: 'awaiting_payment',
			action: { kind: 'start' },
			terminal: false
		});
		expect(orderStatus(order({ payment: payment('failed') })).message).toContain(
			'didn’t go through'
		);
		expect(orderStatus(order({ payment: payment('cancelled') })).action.kind).toBe('start');
	});

	it('opens the Stripe page while the session is open', () => {
		expect(orderStatus(order({ payment: payment('pending', stripe) }))).toMatchObject({
			kind: 'awaiting_payment',
			title: 'Payment pending',
			action: { kind: 'open', url: stripe }
		});
	});

	it('says still checking, do not pay again, for unknown or pending without a page', () => {
		for (const p of [
			payment('unknown'),
			payment('pending'),
			payment('pending', 'https://evil.example/x')
		]) {
			const s = orderStatus(order({ payment: p }));
			expect(s.kind).toBe('checking');
			expect(s.message).toContain('Still checking payment; do not pay again');
			expect(s.doNotPayAgain).toBe(true);
			expect(s.action.kind).toBe('none');
		}
		expect(orderStatus(order({ payment: payment('created') })).title).toBe('Payment pending');
	});

	it('never calls a 202 or a pending payment accepted', () => {
		for (const s of ['created', 'pending', 'unknown']) {
			expect(orderStatus(order({ payment: payment(s) })).title).not.toMatch(/confirm|accept/i);
		}
	});

	it('distinguishes confirmation being secured, confirmed and paid awaiting agreement', () => {
		expect(orderStatus(order({ lifecycle: 'confirmation_pending' })).message).toBe(
			'Payment received, confirmation being secured. This usually takes a few seconds.'
		);
		const confirmed = orderStatus(order({ lifecycle: 'confirmed' }));
		expect(confirmed).toMatchObject({ kind: 'confirmed', terminal: true, tone: 'success' });
		const late = orderStatus(order({ lifecycle: 'paid_awaiting_agreement' }));
		expect(late.message).toContain('The chef will contact you about the date');
		expect(late.message).not.toMatch(/confirmed/);
		expect(late.terminal).toBe(true);
		expect(orderStatus(order({ payment: payment('succeeded') })).kind).toBe('payment_received');
	});

	it('handles free orders', () => {
		const free = { payment_requirement: 'none', total_minor: 0 };
		expect(orderStatus(order(free)).action.kind).toBe('free');
		expect(
			orderStatus(
				order({
					...free,
					free_confirmation: { state: 'processing', reason: null, requested_at: 'x' }
				})
			)
		).toMatchObject({ kind: 'confirmation_pending', title: 'Confirming your order' });
		expect(
			orderStatus(
				order({
					...free,
					free_confirmation: { state: 'refused', reason: 'window_closed', requested_at: 'x' }
				})
			).message
		).toContain('the time to confirm has passed');
	});

	it('explains cancellations without implying a refund', () => {
		const s = orderStatus(
			order({ lifecycle: 'cancelled', cancel_reason: 'payment_not_completed' })
		);
		expect(s).toMatchObject({ kind: 'cancelled', terminal: true });
		expect(s.message).toContain('payment wasn’t completed');
		expect(s.message).not.toMatch(/refund/i);
		expect(
			orderStatus(order({ lifecycle: 'cancelled', cancel_reason: 'free_not_confirmed' })).message
		).toContain('wasn’t confirmed');
		const late = orderStatus(
			order({
				lifecycle: 'cancelled',
				cancel_reason: 'payment_not_completed',
				payment: payment('succeeded')
			})
		);
		expect(late.doNotPayAgain).toBe(true);
		expect(late.message).not.toMatch(/refund/i);
	});
});

describe('helpers', () => {
	it('shows a refund as returned only when the API says it finished in full', () => {
		expect(refundView(undefined)).toBeUndefined();
		expect(refundView({ state: 'pending', requested_minor: 4050 })).toEqual({
			requestedMinor: 4050,
			returnedMinor: 0,
			done: false
		});
		expect(
			refundView({ state: 'succeeded', requested_minor: 4050, returned_minor: 4050 })?.done
		).toBe(true);
		expect(
			refundView({ state: 'succeeded', requested_minor: 4050, returned_minor: 2000 })?.done
		).toBe(false);
	});

	it('accepts only Stripe Checkout as the payment page', () => {
		expect(safeCheckoutUrl('https://checkout.stripe.com/c/pay/cs_test_a#x')).toBe(
			'https://checkout.stripe.com/c/pay/cs_test_a#x'
		);
		for (const bad of [
			null,
			'',
			'http://checkout.stripe.com/x',
			'https://checkout.stripe.com.evil.example/x',
			'https://evil.example/?https://checkout.stripe.com',
			'javascript:alert(1)',
			'https://u@checkout.stripe.com/x'
		]) {
			expect(safeCheckoutUrl(bad)).toBeUndefined();
		}
	});

	it('backs off polling from the API hint to 30 seconds', () => {
		expect([0, 1, 2, 3, 4, 10].map((a) => nextPollSeconds(a, 3))).toEqual([3, 5, 7, 10, 15, 30]);
		expect(nextPollSeconds(0, 0)).toBe(3);
	});

	it('links back to checkout with the same cakes', () => {
		const item = (variant_id: string, lot_id: string | null, quantity: number) =>
			({ variant_id, lot_id, quantity }) as NonNullable<Order['items']>[number];
		expect(restartQuery([item(V1, null, 2), item(V2, LOT, 1)])).toBe(
			`?item=${V1}&item=${V1}&lot=${LOT}`
		);
		expect(restartQuery(null)).toBe('');
	});

	it('uses a quote only with a minute to spare', () => {
		const q = { expires_at: '2026-10-01T10:00:00Z' } as Quote;
		expect(quoteUsable(q, Date.parse('2026-10-01T09:58:00Z'))).toBe(true);
		expect(quoteUsable(q, Date.parse('2026-10-01T09:59:30Z'))).toBe(false);
	});
});
