import { describe, expect, it } from 'vitest';
import {
	cakeOptions,
	colourOf,
	coloursOf,
	containsLine,
	filterListings,
	flavoursOf,
	listing
} from './catalog';
import { cake, EGGS, ID, lot, MILK, PEANUTS, readyVariant, recipe, variant } from './test-fixtures';

describe('cakeOptions', () => {
	it('lists made-to-order sizes with the recipe price, then ready-made cakes with their own price', () => {
		const c = cake({
			variants: [
				variant(),
				variant({
					id: ID.madeM,
					size: { code: 'm', label: 'M', diameter: '7 in' },
					recipe: recipe({ price_minor: 10500 })
				}),
				readyVariant()
			]
		});
		const opts = cakeOptions(c, [lot()]);
		expect(opts.map((o) => [o.key, o.kind, o.priceMinor])).toEqual([
			[`v:${ID.made}`, 'made', 9000],
			[`v:${ID.madeM}`, 'made', 10500],
			[`lot:${ID.lot}`, 'ready', 6000]
		]);
		expect(opts[2]).toMatchObject({
			available: 1,
			collectBy: '2026-11-15T12:00:00Z',
			variantId: ID.readyVariant
		});
	});

	it('never offers a ready-stock variant as made to order', () => {
		expect(cakeOptions(cake({ variants: [readyVariant()] }), [])).toEqual([]);
	});

	it('offers a ready cake only when made to the current recipe version (whose allergens are shown)', () => {
		const c = cake({ variants: [readyVariant()] });
		expect(cakeOptions(c, [lot({ recipe_version_id: 'rv-older' })])).toEqual([]);
		expect(cakeOptions(c, [lot({ available: 0 })])).toEqual([]);
		expect(cakeOptions(c, [lot({ cake_id: 'other' })])).toEqual([]);
		expect(cakeOptions(c, [lot()])).toHaveLength(1);
	});

	it('disables buying without an approved allergen disclosure (K01)', () => {
		const opts = cakeOptions(
			cake({
				variants: [
					variant({ purchasable: false }),
					variant({ id: ID.madeM, recipe: null }),
					variant({ id: ID.readyVariant, recipe: recipe({ allergens: [] }) })
				]
			}),
			[]
		);
		expect(opts.map((o) => o.purchasable)).toEqual([false, false, false]);
	});
});

describe('listing', () => {
	it('shows the lowest buyable price, "several" when prices differ, and prominent allergens as declared', () => {
		const l = listing(cake({ variants: [variant(), readyVariant()] }), [lot()]);
		expect(l).toMatchObject({
			fromMinor: 6000,
			several: true,
			status: 'made',
			hasReady: true,
			prominent: ['PEANUTS'],
			flavours: ['Snickers'],
			servings: { min: 7, max: 10 }
		});
	});

	it('is ready-made only, or unavailable, from what can be bought now', () => {
		expect(listing(cake({ variants: [readyVariant()] }), [lot()]).status).toBe('ready');
		const none = listing(cake({ variants: [variant({ purchasable: false })] }), []);
		expect(none).toMatchObject({
			status: 'unavailable',
			fromMinor: null,
			prominent: [],
			several: false
		});
	});

	it('takes prominent allergens only from options that can be bought', () => {
		const l = listing(
			cake({
				variants: [
					variant({ recipe: recipe({ allergens: [EGGS, MILK] }) }),
					variant({ id: ID.madeM, purchasable: false, recipe: recipe({ allergens: [PEANUTS] }) })
				]
			}),
			[]
		);
		expect(l.prominent).toEqual([]);
		expect(l.several).toBe(false);
	});
});

describe('filterListings', () => {
	const a = { ...listing(cake({ id: 'a', name: 'Banana' }), []), fromMinor: 10500 };
	const b = {
		...listing(cake({ id: 'b', name: 'Apple', variants: [readyVariant()] }), [
			lot({ cake_id: 'b' })
		])
	};
	const c = listing(
		cake({
			id: 'c',
			name: 'Cherry',
			variants: [variant({ purchasable: false, flavour: 'Vanilla' })]
		}),
		[]
	);
	const all = [a, b, c];
	const f = (
		selection: 'all' | 'orderable' | 'ready' | 'unavailable',
		flavour = 'all',
		sort: 'featured' | 'price' | 'name' = 'featured'
	) => filterListings(all, { selection, flavour, sort }).map((l) => l.id);

	it('filters by selection and flavour, keeping the API order for Featured', () => {
		expect(f('all')).toEqual(['a', 'b', 'c']);
		expect(f('orderable')).toEqual(['a', 'b']);
		expect(f('ready')).toEqual(['b']);
		expect(f('unavailable')).toEqual(['c']);
		expect(f('all', 'Vanilla')).toEqual(['c']);
	});

	it('sorts by price (unpriced last) and by name', () => {
		expect(f('all', 'all', 'price')).toEqual(['b', 'a', 'c']);
		expect(f('all', 'all', 'name')).toEqual(['b', 'a', 'c']);
		expect(flavoursOf(all)).toEqual(['Snickers', 'Vanilla']);
	});
});

describe('cake colour (Colour filter)', () => {
	const pink = { code: 'pink', label: 'Pink', position: 10 };
	const silver = { code: 'silver', label: 'Silver', position: 20 };
	const withColour = (
		id: string,
		colour: unknown,
		over: Partial<Parameters<typeof cake>[0]> = {}
	) => listing({ ...cake({ id, ...over }), colour } as Parameters<typeof listing>[0], []);

	it('reads only an exact {code, label, position} colour; anything else is no colour', () => {
		expect(colourOf(cake())).toBeNull();
		expect(colourOf({ ...cake(), colour: pink } as never)).toEqual(pink);
		expect(colourOf({ ...cake(), colour: { ...pink, label: '🌸'.repeat(40) } } as never)).toEqual({
			...pink,
			label: '🌸'.repeat(40)
		});
		for (const bad of [
			null,
			'pink',
			{ code: 'pink', label: 'Pink' },
			{ ...pink, code: 'Pink!' },
			{ ...pink, code: 'all' },
			{ ...pink, code: 7 },
			{ ...pink, label: '' },
			{ ...pink, label: ' Pink' },
			{ ...pink, label: 'x'.repeat(41) },
			{ ...pink, position: 1.5 },
			{ ...pink, position: -1 },
			{ ...pink, position: '10' }
		]) {
			expect(colourOf({ ...cake(), colour: bad } as never)).toBeNull();
		}
	});

	it("lists each colour once in the chef's order and filters by its code", () => {
		const all = [
			withColour('a', silver),
			withColour('b', null),
			withColour('c', pink),
			withColour('d', silver),
			withColour('e', { code: 'blush', label: 'Blush', position: 10 })
		];
		expect(coloursOf(all)).toEqual([{ code: 'blush', label: 'Blush', position: 10 }, pink, silver]);
		const ids = (colour: string) =>
			filterListings(all, { selection: 'all', flavour: 'all', colour, sort: 'featured' }).map(
				(l) => l.id
			);
		expect(ids('all')).toEqual(['a', 'b', 'c', 'd', 'e']);
		expect(ids('silver')).toEqual(['a', 'd']);
		expect(ids('pink')).toEqual(['c']);
		expect(ids('red')).toEqual([]);
	});

	it('combines colour with selection and flavour', () => {
		const all = [
			withColour('made', pink),
			withColour('off', pink, { variants: [variant({ purchasable: false, flavour: 'Vanilla' })] }),
			withColour('silver', silver)
		];
		const ids = (selection: 'all' | 'orderable' | 'unavailable', flavour: string) =>
			filterListings(all, { selection, flavour, colour: 'pink', sort: 'name' }).map((l) => l.id);
		expect(ids('all', 'all')).toEqual(['made', 'off']);
		expect(ids('orderable', 'all')).toEqual(['made']);
		expect(ids('unavailable', 'all')).toEqual(['off']);
		expect(ids('all', 'Vanilla')).toEqual(['off']);
	});

	it('has no colours while the catalogue sends none (the filter stays hidden)', () => {
		expect(coloursOf([listing(cake(), [])])).toEqual([]);
	});
});

it('containsLine keeps the declaration order and exact labels', () => {
	expect(containsLine([EGGS, MILK, PEANUTS])).toBe('EGGS, MILK, PEANUTS');
});
