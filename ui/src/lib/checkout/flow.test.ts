import { describe, expect, it } from 'vitest';
import { ApiError } from '../api';
import { orderCapability, lastOrder } from '../guest';
import {
	ActionKeys,
	BasketProblem,
	CapabilityNotStored,
	forgetQuote,
	placeOrder,
	quoteIsCurrent,
	quoteMatchesItems,
	requestQuote,
	resolveBasket,
	startPaymentFromOrder,
	type CheckoutApi
} from './flow';
import type { Cake, Order, Quote } from './types';

const V1 = '11111111-1111-4111-8111-111111111111';
const V2 = '22222222-2222-4222-8222-222222222222';
const LOT = '33333333-3333-4333-8333-333333333333';
const ORDER = '44444444-4444-4444-8444-444444444444';
const POLICY = '55555555-5555-4555-8555-555555555555';
const STRIPE = 'https://checkout.stripe.com/c/pay/cs_test_1';

function memoryStore(): Storage {
	const m = new Map<string, string>();
	return {
		get length() {
			return m.size;
		},
		clear: () => m.clear(),
		key: (i) => [...m.keys()][i] ?? null,
		getItem: (k) => m.get(k) ?? null,
		setItem: (k, v) => void m.set(k, v),
		removeItem: (k) => void m.delete(k)
	};
}

function counterKeys() {
	let n = 0;
	let p = 0;
	return new ActionKeys(
		() => `key-${++n}`,
		() => String(++p).padStart(43, 'A')
	);
}

const recipe = {
	recipe_version_id: 'r',
	price_minor: 9000,
	currency: 'GBP',
	servings: { min: 7, max: 10 },
	coating: 'Swiss meringue buttercream',
	composition: ['Vanilla sponge'],
	allergens: [{ code: 'eggs', label: 'EGGS', prominent: false }],
	allergen_notice: { id: 'n', text: 'Shared kitchen.' }
};

const cake = (id: string, variants: string[], over: Partial<Cake> = {}): Cake => ({
	id,
	name: `Cake ${id}`,
	description: '',
	variants: variants.map((v) => ({
		id: v,
		flavour: 'Vanilla Raspberry',
		size: { code: 's', label: 'S', diameter: '6 in' },
		stock_mode: 'made_to_order',
		purchasable: true,
		recipe
	})),
	...over
});

function quote(over: Partial<Quote> = {}): Quote {
	return {
		id: 'q1',
		version: 1,
		sent_at: '2026-10-10T09:00:00Z',
		expires_at: '2026-10-11T09:00:00Z',
		date: '2026-10-22',
		slot: { id: 's', starts_at: '2026-10-22T10:00:00Z', ends_at: '2026-10-22T11:00:00Z' },
		fulfillment_mode: 'collection',
		subtotal_minor: 9000,
		discount_minor: 0,
		total_minor: 9000,
		currency: 'GBP',
		discount: null,
		lines: [],
		...over
	};
}

function order(over: Partial<Order> = {}): Order {
	return {
		id: ORDER,
		version: 1,
		lifecycle: 'awaiting_payment',
		cancel_reason: null,
		cancelled_at: null,
		payment_requirement: 'full',
		placed_at: 'x',
		date: '2026-10-22',
		fulfillment_mode: 'collection',
		slot: quote().slot,
		full_refund_deadline_at: '2026-10-17T10:00:00Z',
		customer_full_name: 'Ada',
		different_payer_name: null,
		contact_email: 'ada@example.com',
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

type Call = { name: string; args: unknown[] };

function fakeApi(over: Partial<CheckoutApi>, calls: Call[] = []): CheckoutApi {
	const fail = (name: string) => () => {
		throw new Error(`unexpected call ${name}`);
	};
	const base: CheckoutApi = {
		cakesPage: fail('cakesPage'),
		cake: fail('cake'),
		readyCakes: fail('readyCakes'),
		policy: fail('policy'),
		availability: fail('availability'),
		schedule: fail('schedule'),
		createQuote: fail('createQuote'),
		checkout: fail('checkout'),
		order: fail('order'),
		startPayment: fail('startPayment'),
		confirmFree: fail('confirmFree')
	};
	const merged = { ...base, ...over } as CheckoutApi;
	return new Proxy(merged, {
		get(target, prop: string) {
			const fn = target[prop as keyof CheckoutApi] as (...a: unknown[]) => unknown;
			return (...args: unknown[]) => {
				calls.push({ name: prop, args });
				return fn(...args);
			};
		}
	});
}

const noSleep = async () => {};

describe('ActionKeys', () => {
	it('reuses a key and proof for the same action and makes new ones after forget', () => {
		const keys = counterKeys();
		const a = keys.for('x');
		expect(keys.for('x')).toEqual(a);
		expect(keys.for('y').key).not.toBe(a.key);
		keys.forget('x');
		expect(keys.for('x').key).not.toBe(a.key);
		expect(keys.for('x').proof).not.toBe(a.proof);
	});
});

describe('resolveBasket', () => {
	it('finds variants across catalog pages and ready cakes by their cake', async () => {
		const calls: Call[] = [];
		const api = fakeApi(
			{
				cakesPage: async (cursor) =>
					cursor
						? { cakes: [cake('c2', [V2])] }
						: { cakes: [cake('c1', [V1])], next_cursor: 'next' },
				readyCakes: async () => [
					{
						id: LOT,
						kind: 'ready',
						cake_id: 'c3',
						cake_name: 'Cake c3',
						variant_id: 'rv',
						recipe_version_id: 'r',
						flavour: 'Snickers',
						size: { code: 's', label: 'S', diameter: '6 in' },
						price_minor: 5000,
						currency: 'GBP',
						available: 1,
						expires_at: 'x'
					}
				],
				cake: async (id) => cake(id, ['rv'])
			},
			calls
		);
		const items = await resolveBasket(api, [
			{ variantId: V1, quantity: 1 },
			{ variantId: V2, quantity: 2 },
			{ lotId: LOT, quantity: 1 }
		]);
		expect(items.map((i) => [i.cakeName, i.quantity, i.ready])).toEqual([
			['Cake c1', 1, false],
			['Cake c2', 2, false],
			['Cake c3', 1, true]
		]);
		expect(items[0].allergens[0].label).toBe('EGGS');
		expect(items[0].size).toBe('S · 6 inches');
		expect(items[0].servings).toBe('7–10 servings');
		expect(items.map((i) => i.recipeVersionId)).toEqual(['r', 'r', 'r']);
		expect(calls.filter((c) => c.name === 'cakesPage')).toHaveLength(2);
	});

	it('refuses a ready cake made to another recipe version than the one shown', async () => {
		const lot = {
			id: LOT,
			kind: 'ready',
			cake_id: 'c3',
			cake_name: 'Cake c3',
			variant_id: 'rv',
			recipe_version_id: 'r-old',
			flavour: 'Snickers',
			size: { code: 's', label: 'S', diameter: '6 in' },
			price_minor: 5000,
			currency: 'GBP',
			available: 1,
			expires_at: 'x'
		};
		const api = fakeApi({ readyCakes: async () => [lot], cake: async (id) => cake(id, ['rv']) });
		await expect(resolveBasket(api, [{ lotId: LOT, quantity: 1 }])).rejects.toThrow('unavailable');
	});

	it('refuses unknown, unpurchasable and sold-out cakes', async () => {
		const page = async () => ({ cakes: [cake('c1', [V1])] });
		await expect(
			resolveBasket(fakeApi({ cakesPage: page }), [{ variantId: V2, quantity: 1 }])
		).rejects.toBeInstanceOf(BasketProblem);
		const noRecipe = cake('c1', [V1]);
		noRecipe.variants[0].recipe = null;
		await expect(
			resolveBasket(fakeApi({ cakesPage: async () => ({ cakes: [noRecipe] }) }), [
				{ variantId: V1, quantity: 1 }
			])
		).rejects.toThrow('unavailable');
		await expect(
			resolveBasket(fakeApi({ readyCakes: async () => [] }), [{ lotId: LOT, quantity: 1 }])
		).rejects.toThrow('unavailable');
	});
});

describe('quoteMatchesItems', () => {
	const line = (over: Partial<Quote['lines'][number]>) => ({
		line_no: 1,
		variant_id: V1,
		lot_id: null,
		recipe_version_id: 'r',
		cake_id: 'c1',
		cake_name: 'Cake',
		flavour: 'f',
		size_code: 's',
		quantity: 1,
		unit_price_minor: 9000,
		discount_minor: 0,
		net_minor: 9000,
		...over
	});
	const item = {
		id: V1,
		ready: false,
		recipeVersionId: 'r',
		quantity: 1,
		cakeName: 'Cake',
		flavour: 'f',
		size: 'S',
		allergens: [],
		composition: [],
		coating: '',
		notice: ''
	};

	it('accepts a quote pricing the recipe versions shown', () => {
		expect(quoteMatchesItems(quote({ lines: [line({})] }), [item])).toBe(true);
		const ready = { ...item, id: LOT, ready: true, recipeVersionId: 'r-lot' };
		expect(
			quoteMatchesItems(
				quote({
					lines: [
						line({}),
						line({ line_no: 2, variant_id: 'rv', lot_id: LOT, recipe_version_id: 'r-lot' })
					]
				}),
				[item, ready]
			)
		).toBe(true);
	});

	it('refuses a quote for another recipe version or other lines', () => {
		expect(quoteMatchesItems(quote({ lines: [line({ recipe_version_id: 'r2' })] }), [item])).toBe(
			false
		);
		expect(quoteMatchesItems(quote({ lines: [] }), [item])).toBe(false);
		expect(quoteMatchesItems(quote({ lines: [line({ variant_id: V2 })] }), [item])).toBe(false);
		// A lot line never stands in for a made-to-order line of the same variant.
		expect(quoteMatchesItems(quote({ lines: [line({ lot_id: LOT })] }), [item])).toBe(false);
	});
});

describe('quoteIsCurrent', () => {
	const choice = { date: '2026-10-22', slotId: 's', mode: 'collection' as const, code: null };

	it('accepts only a quote for the date, time, option and code on screen', () => {
		expect(quoteIsCurrent(quote(), choice)).toBe(true);
		expect(quoteIsCurrent(null, choice)).toBe(false);
		// The handover option changed while times reloaded: the old quote must not be submitted.
		expect(quoteIsCurrent(quote(), { ...choice, mode: 'customer_courier' })).toBe(false);
		expect(quoteIsCurrent(quote(), { ...choice, slotId: 'other' })).toBe(false);
		expect(quoteIsCurrent(quote(), { ...choice, date: '2026-10-23' })).toBe(false);
		expect(quoteIsCurrent(quote(), { ...choice, code: 'DEMO10' })).toBe(false);
	});

	it('compares discount codes as the API normalises them', () => {
		const withCode = quote({
			discount: { code: 'DEMO10', kind: 'percentage', basis_points: 1000 }
		});
		expect(quoteIsCurrent(withCode, { ...choice, code: ' demo10 ' })).toBe(true);
		expect(quoteIsCurrent(withCode, choice)).toBe(false);
	});
});

describe('requestQuote', () => {
	const req = {
		date: '2026-10-22',
		slotId: 's',
		mode: 'collection' as const,
		lines: [{ variantId: V1, quantity: 1 }]
	};
	const now = () => Date.parse('2026-10-10T09:00:00Z');

	it('sends exactly the API fields with a fresh guest proof, and replays on retry', async () => {
		const calls: Call[] = [];
		const keys = counterKeys();
		const api = fakeApi({ createQuote: async () => quote() }, calls);
		const first = await requestQuote(api, keys, { ...req, code: 'DEMO10' }, undefined, now);
		expect(calls[0].args[0]).toEqual({
			date: '2026-10-22',
			slot_id: 's',
			fulfillment_mode: 'collection',
			lines: [{ variant_id: V1, quantity: 1 }],
			discount_code: 'DEMO10',
			guest_proof: first.proof
		});
		await requestQuote(api, keys, { ...req, code: 'DEMO10' }, undefined, now);
		expect(calls[1].args[1]).toBe(calls[0].args[1]);
		const other = await requestQuote(api, keys, req, undefined, now);
		expect(calls[2].args[1]).not.toBe(calls[0].args[1]);
		expect(other.proof).not.toBe(first.proof);
		forgetQuote(keys, req, undefined);
		await requestQuote(api, keys, req, undefined, now);
		expect(calls[3].args[1]).not.toBe(calls[2].args[1]);
	});

	it('uses the bearer token and no proof when signed in', async () => {
		const calls: Call[] = [];
		const got = await requestQuote(
			fakeApi({ createQuote: async () => quote() }, calls),
			counterKeys(),
			req,
			'tok',
			now
		);
		expect(got.proof).toBeUndefined();
		expect(calls[0].args[0]).not.toHaveProperty('guest_proof');
		expect(calls[0].args[2]).toEqual({ kind: 'account', token: 'tok' });
	});

	it('replaces a replayed quote that is about to expire', async () => {
		const calls: Call[] = [];
		let n = 0;
		const api = fakeApi(
			{
				createQuote: async () =>
					++n === 1 ? quote({ expires_at: '2026-10-10T09:01:00Z' }) : quote({ id: 'q2' })
			},
			calls
		);
		const got = await requestQuote(api, counterKeys(), req, undefined, now);
		expect(got.quote.id).toBe('q2');
		expect(calls[1].args[1]).not.toBe(calls[0].args[1]);
	});
});

describe('placeOrder', () => {
	const contact = { full_name: 'Ada Lovelace', email: 'ada@example.com' };
	const proof = 'P'.repeat(43);

	it('checks out with the consented policy, keeps the guest capability and opens the payment page', async () => {
		const calls: Call[] = [];
		const store = memoryStore();
		let reads = 0;
		const api = fakeApi(
			{
				checkout: async () => ({ order_id: ORDER, state: 'pending', poll_after_seconds: 3 }),
				order: async () => {
					reads++;
					if (reads === 1) return order();
					if (reads === 2)
						return order({
							payment: {
								id: 'p',
								state: 'created',
								amount_minor: 9000,
								currency: 'GBP',
								requested_at: 'x',
								checkout_url: null,
								expires_at: null
							}
						});
					return order({
						payment: {
							id: 'p',
							state: 'pending',
							amount_minor: 9000,
							currency: 'GBP',
							requested_at: 'x',
							checkout_url: STRIPE,
							expires_at: 'x'
						}
					});
				},
				startPayment: async () => ({
					order_id: ORDER,
					operation_id: 'p',
					state: 'payment_pending',
					poll_after_seconds: 3
				})
			},
			calls
		);
		const got = await placeOrder(
			api,
			counterKeys(),
			{ quote: quote(), proof, policyId: POLICY, contact },
			{ sleep: noSleep, store }
		);
		expect(got).toEqual({ kind: 'redirect', orderId: ORDER, url: STRIPE });
		const checkout = calls.find((c) => c.name === 'checkout');
		expect(checkout?.args[0]).toEqual({
			quote_id: 'q1',
			quote_version: 1,
			consented_policy_version: POLICY,
			contact,
			guest_proof: proof
		});
		expect(calls.find((c) => c.name === 'startPayment')?.args.slice(0, 2)).toEqual([ORDER, 1]);
		expect(calls.find((c) => c.name === 'order')?.args[1]).toEqual({
			kind: 'guest',
			secret: proof
		});
		expect(orderCapability(ORDER, store)).toBe(proof);
		expect(lastOrder(store)).toBe(ORDER);
	});

	it('includes a different payer only when given and never sends the proof when signed in', async () => {
		const calls: Call[] = [];
		const api = fakeApi(
			{
				checkout: async () => ({ order_id: ORDER, state: 'pending', poll_after_seconds: 3 }),
				order: async () => order({ lifecycle: 'confirmation_pending' })
			},
			calls
		);
		await placeOrder(
			api,
			counterKeys(),
			{
				quote: quote(),
				token: 'tok',
				policyId: POLICY,
				contact: { ...contact, payer_name: 'Bob' }
			},
			{ sleep: noSleep, store: memoryStore() }
		);
		expect(calls[0].args[0]).toEqual({
			quote_id: 'q1',
			quote_version: 1,
			consented_policy_version: POLICY,
			contact: { ...contact, payer_name: 'Bob' }
		});
		expect(calls[0].args[2]).toEqual({ kind: 'account', token: 'tok' });
	});

	it('confirms a £0 order instead of paying', async () => {
		const calls: Call[] = [];
		const api = fakeApi(
			{
				checkout: async () => ({ order_id: ORDER, state: 'pending', poll_after_seconds: 3 }),
				order: async () => order({ payment_requirement: 'none', total_minor: 0, version: 2 }),
				confirmFree: async () => ({
					order_id: ORDER,
					state: 'confirmation_pending',
					poll_after_seconds: 3
				})
			},
			calls
		);
		const got = await placeOrder(
			api,
			counterKeys(),
			{ quote: quote({ total_minor: 0 }), proof, policyId: POLICY, contact },
			{ sleep: noSleep, store: memoryStore() }
		);
		expect(got).toEqual({ kind: 'order', orderId: ORDER });
		expect(calls.find((c) => c.name === 'confirmFree')?.args.slice(0, 2)).toEqual([ORDER, 2]);
		expect(calls.some((c) => c.name === 'startPayment')).toBe(false);
	});

	it('starts no payment when the guest capability cannot be kept, and retrying replays the checkout', async () => {
		const calls: Call[] = [];
		const api = fakeApi(
			{
				checkout: async () => ({ order_id: ORDER, state: 'pending', poll_after_seconds: 3 }),
				order: async () => order({ lifecycle: 'confirmation_pending' })
			},
			calls
		);
		const keys = counterKeys();
		const input = { quote: quote(), proof, policyId: POLICY, contact };
		const full = {
			...memoryStore(),
			setItem: () => {
				throw new Error('quota');
			}
		} as Storage;
		const err = await placeOrder(api, keys, input, { sleep: noSleep, store: full }).catch((e) => e);
		expect(err).toBeInstanceOf(CapabilityNotStored);
		expect((err as CapabilityNotStored).orderId).toBe(ORDER);
		expect(calls.map((c) => c.name)).toEqual(['checkout']);
		await placeOrder(api, keys, input, { sleep: noSleep, store: memoryStore() });
		expect(calls[1].name).toBe('checkout');
		expect(calls[1].args[1]).toBe(calls[0].args[1]);
	});

	it('throws refusals before the order exists and reuses the key when retried', async () => {
		const calls: Call[] = [];
		let n = 0;
		const api = fakeApi(
			{
				checkout: async () => {
					if (++n === 1) throw new ApiError(503, { code: 'UNAVAILABLE', retryable: true });
					return { order_id: ORDER, state: 'pending', poll_after_seconds: 3 };
				},
				order: async () => order({ lifecycle: 'confirmation_pending' })
			},
			calls
		);
		const keys = counterKeys();
		const input = { quote: quote(), proof, policyId: POLICY, contact };
		await expect(
			placeOrder(api, keys, input, { sleep: noSleep, store: memoryStore() })
		).rejects.toThrow();
		await placeOrder(api, keys, input, { sleep: noSleep, store: memoryStore() });
		expect(calls[0].args[1]).toBe(calls[1].args[1]);
	});

	it('after the order exists, a payment problem points to the order page', async () => {
		const problem = new ApiError(409, { code: 'PAYMENT_IN_PROGRESS' });
		const api = fakeApi({
			checkout: async () => ({ order_id: ORDER, state: 'pending', poll_after_seconds: 3 }),
			order: async () => order(),
			startPayment: async () => {
				throw problem;
			}
		});
		const got = await placeOrder(
			api,
			counterKeys(),
			{ quote: quote(), proof, policyId: POLICY, contact },
			{ sleep: noSleep, store: memoryStore() }
		);
		expect(got).toEqual({ kind: 'order', orderId: ORDER, problem });
	});

	it('stops waiting for the payment page after the limit and never redirects elsewhere', async () => {
		let t = 0;
		const api = fakeApi({
			checkout: async () => ({ order_id: ORDER, state: 'pending', poll_after_seconds: 3 }),
			order: async () =>
				order({
					payment: {
						id: 'p',
						state: 'pending',
						amount_minor: 9000,
						currency: 'GBP',
						requested_at: 'x',
						checkout_url: 'https://evil.example/pay',
						expires_at: 'x'
					}
				})
		});
		const got = await placeOrder(
			api,
			counterKeys(),
			{ quote: quote(), proof, policyId: POLICY, contact },
			{
				sleep: async (ms) => void (t += ms),
				now: () => t,
				paymentWaitMs: 10_000,
				store: memoryStore()
			}
		);
		expect(got).toEqual({ kind: 'order', orderId: ORDER });
	});
});

describe('startPaymentFromOrder', () => {
	it('starts payment at the order version and returns the Stripe page', async () => {
		const calls: Call[] = [];
		const api = fakeApi(
			{
				startPayment: async () => ({
					order_id: ORDER,
					state: 'payment_pending',
					poll_after_seconds: 3
				}),
				order: async () =>
					order({
						version: 3,
						payment: {
							id: 'p2',
							state: 'pending',
							amount_minor: 9000,
							currency: 'GBP',
							requested_at: 'x',
							checkout_url: STRIPE,
							expires_at: 'x'
						}
					})
			},
			calls
		);
		const who = { kind: 'guest' as const, secret: 'P'.repeat(43) };
		const url = await startPaymentFromOrder(api, counterKeys(), order({ version: 3 }), who, {
			sleep: noSleep
		});
		expect(url).toBe(STRIPE);
		expect(calls[0].args.slice(0, 2)).toEqual([ORDER, 3]);
	});

	it('a failed read after the 202 keeps checking (never a refusal), then the page', async () => {
		let reads = 0;
		const page = order({
			version: 3,
			payment: {
				id: 'p2',
				state: 'pending',
				amount_minor: 9000,
				currency: 'GBP',
				requested_at: 'x',
				checkout_url: STRIPE,
				expires_at: 'x'
			}
		});
		const api = fakeApi(
			{
				startPayment: async () => ({
					order_id: ORDER,
					state: 'payment_pending',
					poll_after_seconds: 1
				}),
				order: async () => {
					if (++reads < 3) throw new ApiError(0, {});
					return page;
				}
			},
			[]
		);
		const who = { kind: 'guest' as const, secret: 'P'.repeat(43) };
		await expect(
			startPaymentFromOrder(api, counterKeys(), order({ version: 3 }), who, { sleep: noSleep })
		).resolves.toBe(STRIPE);
	});

	it('reads failing for the whole wait: undefined (the page keeps checking), not an error', async () => {
		let t = 0;
		const api = fakeApi(
			{
				startPayment: async () => ({
					order_id: ORDER,
					state: 'payment_pending',
					poll_after_seconds: 1
				}),
				order: async () => {
					throw new ApiError(0, {});
				}
			},
			[]
		);
		const who = { kind: 'guest' as const, secret: 'P'.repeat(43) };
		await expect(
			startPaymentFromOrder(api, counterKeys(), order({ version: 3 }), who, {
				sleep: async () => void (t += 10_000),
				now: () => t
			})
		).resolves.toBeUndefined();
	});
});
