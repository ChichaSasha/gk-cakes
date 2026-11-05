import { describe, expect, it } from 'vitest';
import { cakeOptions, containsLine, filterListings, flavoursOf, listing } from './catalog';
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

it('containsLine keeps the declaration order and exact labels', () => {
	expect(containsLine([EGGS, MILK, PEANUTS])).toBe('EGGS, MILK, PEANUTS');
});
