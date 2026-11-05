import { describe, expect, it } from 'vitest';
import { ApiError } from '../api';
import {
	archiveVariant,
	byState,
	catalogueReference,
	colours,
	createFlavour,
	createNotice,
	currentNotice,
	flavours,
	MAX_CAKE_PAGES,
	createCake,
	createColour,
	createRecipeVersion,
	createVariant,
	liveVariants,
	setCakeState,
	staffCake,
	staffCakes,
	updateCake,
	updateColour,
	type StaffCake,
	type StaffApi
} from './catalogue';

const CAKE = '11111111-1111-4111-8111-111111111111';
const COLOUR = '22222222-2222-4222-8222-222222222222';
const VARIANT = '33333333-3333-4333-8333-333333333333';
const NOTICE = '44444444-4444-4444-8444-444444444444';
const FLAVOUR = '55555555-5555-4555-8555-555555555555';

interface Call {
	method: string;
	url: string;
	headers: Record<string, string>;
	body?: unknown;
}

/** A staff API whose fetch records each call and answers from `replies` in order. */
function fakeApi(...replies: { status?: number; body: unknown }[]) {
	const calls: Call[] = [];
	const api: StaffApi = {
		baseUrl: 'https://api.example.com',
		token: 'staff-token',
		fetch: (async (input: RequestInfo | URL, init?: RequestInit) => {
			calls.push({
				method: init?.method ?? 'GET',
				url: String(input),
				headers: init?.headers as Record<string, string>,
				body: init?.body ? JSON.parse(String(init.body)) : undefined
			});
			const r = replies.shift() ?? { body: {} };
			return new Response(JSON.stringify(r.body), { status: r.status ?? 200 });
		}) as typeof fetch
	};
	return { api, calls };
}

const cake = (over: Partial<StaffCake> = {}): StaffCake => ({
	id: CAKE,
	name: 'Pink piping',
	description: '',
	colour: null,
	state: 'draft',
	created_at: '2026-11-05T10:00:00Z',
	updated_at: '2026-11-05T10:00:00Z',
	version: 1,
	variants: [],
	...over
});

describe('staff catalogue reads', () => {
	it('reads every page of cakes with the staff token, 50 at a time', async () => {
		const { api, calls } = fakeApi(
			{ body: { cakes: [cake()], next_cursor: 'c2' } },
			{ body: { cakes: [cake({ id: VARIANT })] } }
		);
		expect((await staffCakes(api)).map((c) => c.id)).toEqual([CAKE, VARIANT]);
		expect(calls.map((c) => c.url)).toEqual([
			'https://api.example.com/api/v1/admin/cakes?limit=50',
			'https://api.example.com/api/v1/admin/cakes?limit=50&cursor=c2'
		]);
		expect(calls[0].headers.Authorization).toBe('Bearer staff-token');
		expect(calls[0].method).toBe('GET');
	});

	it('reads one cake and the colour list; refuses a non-ID path segment', async () => {
		const { api, calls } = fakeApi({ body: cake() }, { body: { colours: [] } });
		await staffCake(api, CAKE);
		expect(await colours(api)).toEqual([]);
		expect(calls.map((c) => c.url)).toEqual([
			`https://api.example.com/api/v1/admin/cakes/${CAKE}`,
			'https://api.example.com/api/v1/admin/colours'
		]);
		expect(() => staffCake(api, '../orders')).toThrow('not an ID');
	});

	it('passes API refusals through as ApiError (e.g. no chef capability)', async () => {
		const { api } = fakeApi({
			status: 403,
			body: { error: { code: 'FORBIDDEN', message: 'No', request_id: 'r', retryable: false } }
		});
		await expect(colours(api)).rejects.toMatchObject({ status: 403, code: 'FORBIDDEN' });
		await expect(
			colours(fakeApi({ status: 401, body: {} }).api).catch((e: ApiError) => e.signInRequired)
		).resolves.toBe(true);
	});
});

describe('staff catalogue commands (strict JSON, K00)', () => {
	it('sends each command with its Idempotency-Key and exactly the fields given', async () => {
		const { api, calls } = fakeApi(...Array.from({ length: 7 }, () => ({ body: {} })));
		await createColour(api, 'k1', { code: 'pink', label: 'Pink', position: 10 });
		await updateColour(api, 'k2', COLOUR, { expected_version: 1, archived: true });
		await createCake(api, 'k3', { name: 'Pink piping', description: 'Soft', colour_id: null });
		await updateCake(api, 'k4', CAKE, { expected_version: 2, colour_id: COLOUR });
		await updateCake(api, 'k5', CAKE, { expected_version: 3, colour_id: null, name: undefined });
		await setCakeState(api, 'k6', CAKE, { expected_version: 4, state: 'active' });
		await archiveVariant(api, 'k7', VARIANT, { expected_version: 1 });
		expect(
			calls.map((c) => [c.method, c.url.replace('https://api.example.com', ''), c.body])
		).toEqual([
			['POST', '/api/v1/admin/colours', { code: 'pink', label: 'Pink', position: 10 }],
			['PATCH', `/api/v1/admin/colours/${COLOUR}`, { expected_version: 1, archived: true }],
			[
				'POST',
				'/api/v1/admin/cakes',
				{ name: 'Pink piping', description: 'Soft', colour_id: null }
			],
			['PATCH', `/api/v1/admin/cakes/${CAKE}`, { expected_version: 2, colour_id: COLOUR }],
			['PATCH', `/api/v1/admin/cakes/${CAKE}`, { expected_version: 3, colour_id: null }],
			['POST', `/api/v1/admin/cakes/${CAKE}/state`, { expected_version: 4, state: 'active' }],
			['POST', `/api/v1/admin/variants/${VARIANT}/archive`, { expected_version: 1 }]
		]);
		expect(calls.map((c) => c.headers['Idempotency-Key'])).toEqual([
			'k1',
			'k2',
			'k3',
			'k4',
			'k5',
			'k6',
			'k7'
		]);
		expect(calls.every((c) => c.headers['Content-Type'] === 'application/json')).toBe(true);
	});

	it('creates variants and immutable recipe versions with whole-pence prices', async () => {
		const { api, calls } = fakeApi({ status: 201, body: {} }, { status: 201, body: {} });
		await createVariant(api, 'v1', CAKE, {
			flavour_id: FLAVOUR,
			size_code: 's',
			stock_mode: 'made_to_order'
		});
		const recipe = {
			variant_id: VARIANT,
			expected_version: 1,
			price_minor: 9000,
			servings_min: 7,
			servings_max: 10,
			coating: 'Swiss meringue buttercream',
			composition: ['Vanilla sponge'],
			allergen_notice_id: NOTICE,
			allergens: [{ code: 'eggs', label: 'EGGS', prominent: false }],
			lines: [{ component: 'Flour', quantity: '250', unit: 'g' as const }]
		};
		await createRecipeVersion(api, 'r1', recipe);
		expect(calls[0].url).toBe(`https://api.example.com/api/v1/admin/cakes/${CAKE}/variants`);
		expect(calls[1].body).toEqual(recipe);
		for (const price_minor of [90.5, 0, -1, 10_000_001]) {
			expect(() => createRecipeVersion(api, 'r2', { ...recipe, price_minor })).toThrow(
				'whole pence'
			);
		}
		expect(calls[0].body).toEqual({
			flavour_id: FLAVOUR,
			size_code: 's',
			stock_mode: 'made_to_order'
		});
		expect(() => createRecipeVersion(api, 'r3', { ...recipe, allergen_notice_id: 'x' })).toThrow(
			'not an ID'
		);
	});

	it('surfaces a stale version as STALE_VERSION (409)', async () => {
		const { api } = fakeApi({
			status: 409,
			body: {
				error: { code: 'STALE_VERSION', message: 'Changed', request_id: 'r', retryable: false }
			}
		});
		await expect(
			updateCake(api, 'k', CAKE, { expected_version: 1, name: 'New' })
		).rejects.toMatchObject({ status: 409, code: 'STALE_VERSION' });
	});
});

describe('exact bodies and remaining reads', () => {
	it('never sends a field the API does not document, even from a wider object', async () => {
		const { api, calls } = fakeApi(...Array.from({ length: 4 }, () => ({ body: {} })));
		const form = { name: 'Pink piping', description: 'Soft', colour_id: COLOUR, draft: true };
		await createCake(api, 'a', form);
		const loaded = { ...cake(), expected_version: 1, name: 'New' };
		await updateCake(api, 'b', CAKE, loaded);
		await createColour(api, 'c', { code: 'red', label: 'Red', position: 30, id: 'x' } as never);
		await createRecipeVersion(api, 'd', {
			variant_id: VARIANT,
			expected_version: 1,
			price_minor: 1,
			servings_min: 1,
			servings_max: 2,
			coating: 'c',
			composition: [],
			allergen_notice_id: NOTICE,
			allergens: [{ code: 'eggs', label: 'EGGS', prominent: false, note: 'x' } as never],
			lines: [{ component: 'Flour', quantity: '1', unit: 'g', cost: 2 } as never],
			extra: 1
		} as never);
		expect(calls[0].body).toEqual({ name: 'Pink piping', description: 'Soft', colour_id: COLOUR });
		expect(calls[1].body).toEqual({
			expected_version: 1,
			name: 'New',
			description: ''
		});
		expect(calls[2].body).toEqual({ code: 'red', label: 'Red', position: 30 });
		const recipe = calls[3].body as Record<string, unknown>;
		expect(Object.keys(recipe).sort()).toEqual(
			[
				'allergen_notice_id',
				'allergens',
				'coating',
				'composition',
				'expected_version',
				'lines',
				'price_minor',
				'servings_max',
				'servings_min',
				'variant_id'
			].sort()
		);
		expect(recipe.allergens).toEqual([{ code: 'eggs', label: 'EGGS', prominent: false }]);
		expect(recipe.lines).toEqual([{ component: 'Flour', quantity: '1', unit: 'g' }]);
	});

	it('refuses an empty colour ID instead of sending it', () => {
		const { api } = fakeApi();
		expect(() => createCake(api, 'k', { name: 'n', description: '', colour_id: '' })).toThrow(
			'not an ID'
		);
		expect(() => updateCake(api, 'k', CAKE, { expected_version: 1, colour_id: '' })).toThrow(
			'not an ID'
		);
		expect(() => staffCake(api, 'AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA')).toThrow('not an ID');
	});

	it('creates flavours and notice versions (request body, response text) and unwraps the lists', async () => {
		const { api, calls } = fakeApi(
			{ status: 201, body: { id: FLAVOUR, name: 'Vanilla' } },
			{ status: 201, body: { id: NOTICE, text: 'Shared kitchen' } },
			{ body: { flavours: [{ id: FLAVOUR, name: 'Vanilla' }] } },
			{ body: { id: NOTICE, text: 'Shared kitchen' } },
			{ body: { sizes: [], allergen_codes: ['eggs'], units: ['g'] } }
		);
		expect(await createFlavour(api, 'f', 'Vanilla')).toEqual({ id: FLAVOUR, name: 'Vanilla' });
		expect((await createNotice(api, 'n', 'Shared kitchen')).text).toBe('Shared kitchen');
		expect(await flavours(api)).toEqual([{ id: FLAVOUR, name: 'Vanilla' }]);
		expect((await currentNotice(api)).id).toBe(NOTICE);
		expect((await catalogueReference(api)).allergen_codes).toEqual(['eggs']);
		expect(
			calls.map((c) => [c.method, c.url.replace('https://api.example.com', ''), c.body])
		).toEqual([
			['POST', '/api/v1/admin/flavours', { name: 'Vanilla' }],
			['POST', '/api/v1/admin/allergen-notices', { body: 'Shared kitchen' }],
			['GET', '/api/v1/admin/flavours', undefined],
			['GET', '/api/v1/admin/allergen-notices/current', undefined],
			['GET', '/api/v1/admin/sizes', undefined]
		]);
	});

	it('throws rather than return a shortened catalogue past the page limit', async () => {
		const { api } = fakeApi(
			...Array.from({ length: MAX_CAKE_PAGES }, (_, i) => ({
				body: { cakes: [cake()], next_cursor: `c${i}` }
			}))
		);
		await expect(staffCakes(api)).rejects.toThrow('more than');
	});
});

describe('view helpers', () => {
	it('groups cakes by state and lists variants still offered', () => {
		const live = cake({ id: VARIANT, state: 'active' });
		expect(byState([cake(), live])).toEqual({ draft: [cake()], active: [live], archived: [] });
		const variants = [
			{ id: '1', archived_at: undefined },
			{ id: '2', archived_at: '2026-11-05T10:00:00Z' }
		] as StaffCake['variants'];
		expect(liveVariants(cake({ variants })).map((v) => v.id)).toEqual(['1']);
	});
});
