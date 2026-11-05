// Catalogue view models for the discovery pages (GK-028, approved GK-016
// r01c sketch). Everything shown comes from the public API (contracts K01):
// names, prices (integer pence, only formatted here), servings, the exact
// allergen declaration of the current recipe version and the ready-made
// stock. Nothing is priced, scaled or inferred in the browser.
import type { Allergen, Cake, ReadyCake, Recipe, Variant } from '../checkout/types';

/** One thing a customer can choose on a cake page: a made-to-order size or a ready-made cake. */
export interface CakeOption {
	/** `v:<variantId>` or `lot:<lotId>`; stable across reloads. */
	key: string;
	kind: 'made' | 'ready';
	variantId: string;
	lotId?: string;
	flavour: string;
	sizeCode: string;
	/** Whether it can be bought now: a current recipe with its allergen declaration (K01). */
	purchasable: boolean;
	/** The current recipe version (null: no approved disclosure, buying disabled). */
	recipe: Recipe | null;
	/** Server price in pence: the recipe's price, or the ready cake's own price. */
	priceMinor: number | null;
	currency: string;
	/** Ready-made only: cakes left and the chef's collect-by instant (the lot's safe expiry). */
	available?: number;
	collectBy?: string;
}

/** Made-to-order options first (in the API's order), then ready-made cakes. */
export function cakeOptions(cake: Cake, ready: ReadyCake[]): CakeOption[] {
	const made: CakeOption[] = cake.variants
		.filter((v) => v.stock_mode === 'made_to_order')
		.map((v) => ({
			key: `v:${v.id}`,
			kind: 'made',
			variantId: v.id,
			flavour: v.flavour,
			sizeCode: v.size.code,
			purchasable: v.purchasable && v.recipe !== null && v.recipe.allergens.length > 0,
			recipe: v.recipe,
			priceMinor: v.recipe?.price_minor ?? null,
			currency: v.recipe?.currency ?? 'GBP'
		}));
	const variants = new Map(cake.variants.map((v) => [v.id, v] as [string, Variant]));
	const lots: CakeOption[] = [];
	for (const lot of ready) {
		if (lot.cake_id !== cake.id || lot.available < 1) continue;
		const v = variants.get(lot.variant_id);
		// A ready cake is offered only when it was made to the variant's current
		// recipe version, the only one whose allergens the catalogue shows (as checkout).
		if (!v?.recipe || v.recipe.recipe_version_id !== lot.recipe_version_id) continue;
		lots.push({
			key: `lot:${lot.id}`,
			kind: 'ready',
			variantId: v.id,
			lotId: lot.id,
			flavour: lot.flavour,
			sizeCode: lot.size.code,
			purchasable: v.purchasable && v.recipe.allergens.length > 0,
			recipe: v.recipe,
			priceMinor: lot.price_minor,
			currency: lot.currency,
			available: lot.available,
			collectBy: lot.expires_at
		});
	}
	return [...made, ...lots];
}

export type ListingStatus = 'made' | 'ready' | 'unavailable';

/** A cake's main colour, as the chef labels and orders it ("Pink", "Silver"). */
export interface CakeColour {
	code: string;
	label: string;
	/** The chef's order of colours in the filter (lowest first). */
	position: number;
}

const COLOUR_CODE = /^[a-z0-9][a-z0-9-]{0,39}$/;

/**
 * The cake's colour for the shop's Colour filter (approved GK-016 r01c
 * sketch). TODO(GK-028 colour filter): the public catalogue does not send a
 * colour yet; the proposed API field is
 * `"colour": {"code", "label", "position"} | null` on each cake of
 * GET /api/v1/cakes and /cakes/{id} (ui/README "Discovery pages"). Until then
 * every cake reads null and the filter stays hidden. Anything that is not
 * exactly that shape is ignored: code a slug other than "all" (the filter's
 * "All colours" value), label 1–40 Unicode code points without surrounding
 * whitespace, position an integer 0–10000.
 */
export function colourOf(cake: Cake): CakeColour | null {
	const c: unknown = (cake as Cake & { colour?: unknown }).colour;
	if (typeof c !== 'object' || c === null) return null;
	const { code, label, position } = c as Record<string, unknown>;
	if (typeof code !== 'string' || !COLOUR_CODE.test(code) || code === 'all') return null;
	if (typeof label !== 'string' || label !== label.trim()) return null;
	const points = [...label].length;
	if (points < 1 || points > 40) return null;
	if (typeof position !== 'number' || !Number.isInteger(position)) return null;
	if (position < 0 || position > 10000) return null;
	return { code, label, position };
}

/** What a shop card shows for one listing. */
export interface Listing {
	id: string;
	name: string;
	/** Lowest server price of the options that can be bought now. */
	fromMinor: number | null;
	currency: string;
	/** More than one price among buyable options ("From £90"). */
	several: boolean;
	servings: { min: number; max: number } | null;
	status: ListingStatus;
	/** Ready-made cakes are among the buyable options. */
	hasReady: boolean;
	flavours: string[];
	colour: CakeColour | null;
	/** Prominent allergens of the buyable options, exactly as declared (K01: PEANUTS). */
	prominent: string[];
}

export function listing(cake: Cake, ready: ReadyCake[]): Listing {
	const options = cakeOptions(cake, ready);
	const buyable = options.filter((o) => o.purchasable && o.priceMinor !== null);
	const prices = buyable.map((o) => o.priceMinor as number);
	const cheapest = buyable.find((o) => o.priceMinor === Math.min(...prices));
	const prominent = new Set<string>();
	for (const o of buyable) {
		for (const a of o.recipe?.allergens ?? []) if (a.prominent) prominent.add(a.label);
	}
	const hasMade = buyable.some((o) => o.kind === 'made');
	const hasReady = buyable.some((o) => o.kind === 'ready');
	return {
		id: cake.id,
		name: cake.name,
		fromMinor: cheapest?.priceMinor ?? null,
		currency: cheapest?.currency ?? 'GBP',
		several: new Set(prices).size > 1,
		servings: cheapest?.recipe?.servings ?? null,
		status: hasMade ? 'made' : hasReady ? 'ready' : 'unavailable',
		hasReady,
		flavours: [...new Set(options.map((o) => o.flavour))],
		colour: colourOf(cake),
		prominent: [...prominent]
	};
}

export type Selection = 'all' | 'orderable' | 'ready' | 'unavailable';
export type Sort = 'featured' | 'price' | 'name';

export function isSelection(v: string | null): v is Selection {
	return v === 'all' || v === 'orderable' || v === 'ready' || v === 'unavailable';
}

/** Shop filters and sorting. "Featured" keeps the API's order (newest first). */
export function filterListings(
	all: Listing[],
	f: { selection: Selection; flavour: string; colour?: string; sort: Sort }
): Listing[] {
	const shown = all.filter(
		(l) =>
			(f.selection === 'all' ||
				(f.selection === 'orderable' && l.status !== 'unavailable') ||
				(f.selection === 'ready' && l.hasReady) ||
				(f.selection === 'unavailable' && l.status === 'unavailable')) &&
			(f.flavour === 'all' || l.flavours.includes(f.flavour)) &&
			(!f.colour || f.colour === 'all' || l.colour?.code === f.colour)
	);
	if (f.sort === 'price') {
		return [...shown].sort(
			(a, b) => (a.fromMinor ?? Number.MAX_SAFE_INTEGER) - (b.fromMinor ?? Number.MAX_SAFE_INTEGER)
		);
	}
	if (f.sort === 'name') return [...shown].sort((a, b) => a.name.localeCompare(b.name, 'en-GB'));
	return shown;
}

/** Every flavour offered, in first-seen order. */
export function flavoursOf(all: Listing[]): string[] {
	return [...new Set(all.flatMap((l) => l.flavours))];
}

/** Every colour the cakes have, once per code, in the chef's order (then by label). */
export function coloursOf(all: Listing[]): CakeColour[] {
	const codes = new Set<string>();
	const out: CakeColour[] = [];
	for (const l of all) {
		if (l.colour && !codes.has(l.colour.code)) {
			codes.add(l.colour.code);
			out.push(l.colour);
		}
	}
	return out.sort((a, b) => a.position - b.position || a.label.localeCompare(b.label, 'en-GB'));
}

/** "7–10 servings". */
export function servingsLabel(s: { min: number; max: number }): string {
	return s.min === s.max ? `${s.min} servings` : `${s.min}–${s.max} servings`;
}

/** The declaration as one line: "EGGS, MILK, PEANUTS" (the recipe version's order). */
export function containsLine(allergens: Allergen[]): string {
	return allergens.map((a) => a.label).join(', ');
}
