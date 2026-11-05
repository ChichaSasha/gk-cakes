// Shared test data in the public API's shapes (api/internal/catalog, schedule).
import type { Allergen, Cake, ReadyCake, Recipe, Variant } from '../checkout/types';

export const EGGS: Allergen = { code: 'eggs', label: 'EGGS', prominent: false };
export const MILK: Allergen = { code: 'milk', label: 'MILK', prominent: false };
export const PEANUTS: Allergen = { code: 'peanuts', label: 'PEANUTS', prominent: true };

export const ID = {
	cake: '11111111-1111-4111-8111-111111111111',
	made: '22222222-2222-4222-8222-222222222222',
	madeM: '33333333-3333-4333-8333-333333333333',
	readyVariant: '44444444-4444-4444-8444-444444444444',
	lot: '55555555-5555-4555-8555-555555555555',
	lot2: '66666666-6666-4666-8666-666666666666'
};

export function recipe(over: Partial<Recipe> = {}): Recipe {
	return {
		recipe_version_id: 'rv-1',
		price_minor: 9000,
		currency: 'GBP',
		servings: { min: 7, max: 10 },
		coating: 'Swiss meringue buttercream',
		composition: ['Chocolate sponge', 'peanut cream cheese cream'],
		allergens: [EGGS, MILK, PEANUTS],
		allergen_notice: { id: 'n1', text: 'Allergen notice: shared kitchen.' },
		...over
	};
}

export function variant(over: Partial<Variant> = {}): Variant {
	return {
		id: ID.made,
		flavour: 'Snickers',
		size: { code: 's', label: 'S', diameter: '6 in' },
		stock_mode: 'made_to_order',
		purchasable: true,
		recipe: recipe(),
		...over
	};
}

export function cake(over: Partial<Cake> = {}): Cake {
	return {
		id: ID.cake,
		name: 'Chocolate swirl',
		description: 'A swirl.',
		variants: [variant()],
		...over
	};
}

export function lot(over: Partial<ReadyCake> = {}): ReadyCake {
	return {
		id: ID.lot,
		kind: 'ready',
		cake_id: ID.cake,
		cake_name: 'Chocolate swirl',
		variant_id: ID.readyVariant,
		recipe_version_id: 'rv-ready',
		flavour: 'Snickers',
		size: { code: 's', label: 'S', diameter: '6 in' },
		price_minor: 6000,
		currency: 'GBP',
		available: 1,
		expires_at: '2026-11-15T12:00:00Z',
		...over
	};
}

export const readyVariant = () =>
	variant({
		id: ID.readyVariant,
		stock_mode: 'ready_stock',
		recipe: recipe({ recipe_version_id: 'rv-ready', price_minor: 9000 })
	});
