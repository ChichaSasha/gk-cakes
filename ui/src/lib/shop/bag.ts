// The bag (GK-028, approved GK-016 r01c sketch): which cakes the customer
// chose and their date and time, kept in this tab's sessionStorage until
// checkout. Only catalogue IDs and the chosen appointment are kept, never
// prices, names or personal details; checkout reads the cakes and date from
// its URL (ui/README "Basket") and asks the server for every price.
import type { FulfillmentMode } from '../checkout/types';

export const BAG_KEY = 'gk-shop-bag';
/** The API's limit on cakes per order (checkout.MaxCakes); one cake per bag line. */
export const MAX_BAG = 20;

/** Handover choices the picker offers online (GK delivery needs the chef's quote, not built). */
export type PickerMode = Extract<FulfillmentMode, 'collection' | 'customer_courier'>;

export interface ChosenSlot {
	id: string;
	starts_at: string;
	ends_at: string;
}

export interface BagState {
	/** `v:<variantId>` (made to order) or `lot:<lotId>` (ready-made), each at most once. */
	keys: string[];
	date?: string;
	mode: PickerMode;
	slot?: ChosenSlot;
}

const UUID = '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}';
const KEY = new RegExp(`^(v|lot):${UUID}$`);
const DATE = /^\d{4}-\d{2}-\d{2}$/;

/** A real calendar date (YYYY-MM-DD): not 2026-02-30 or 2099-13-01. */
export function isRealDate(v: unknown): v is string {
	if (typeof v !== 'string' || !DATE.test(v)) return false;
	const d = new Date(`${v}T00:00:00Z`);
	return !Number.isNaN(d.getTime()) && d.toISOString().slice(0, 10) === v;
}

export function emptyBag(): BagState {
	return { keys: [], mode: 'collection' };
}

function isMode(v: unknown): v is PickerMode {
	return v === 'collection' || v === 'customer_courier';
}

function isSlot(v: unknown): v is ChosenSlot {
	const s = v as ChosenSlot | null;
	return (
		typeof s === 'object' &&
		s !== null &&
		typeof s.id === 'string' &&
		s.id.length > 0 &&
		s.id.length <= 200 &&
		!Number.isNaN(Date.parse(s.starts_at)) &&
		!Number.isNaN(Date.parse(s.ends_at))
	);
}

/** Reads a stored bag; anything unexpected is dropped rather than trusted. */
export function parseBag(raw: string | null): BagState {
	if (!raw) return emptyBag();
	let v: Partial<BagState>;
	try {
		v = JSON.parse(raw);
	} catch {
		return emptyBag();
	}
	if (typeof v !== 'object' || v === null) return emptyBag();
	const keys = Array.isArray(v.keys)
		? [...new Set(v.keys.filter((k): k is string => typeof k === 'string' && KEY.test(k)))]
		: [];
	const bag: BagState = {
		keys: keys.slice(0, MAX_BAG),
		mode: isMode(v.mode) ? v.mode : 'collection'
	};
	if (isRealDate(v.date)) {
		bag.date = v.date;
		if (isSlot(v.slot))
			bag.slot = { id: v.slot.id, starts_at: v.slot.starts_at, ends_at: v.slot.ends_at };
	}
	return bag;
}

export function withCake(bag: BagState, key: string): BagState {
	if (!KEY.test(key) || bag.keys.includes(key) || bag.keys.length >= MAX_BAG) return bag;
	return { ...bag, keys: [...bag.keys, key] };
}

export function withoutCake(bag: BagState, key: string): BagState {
	return { ...bag, keys: bag.keys.filter((k) => k !== key) };
}

/** The appointment chosen in the picker (a new date always comes with its own time). */
export function withWhen(
	bag: BagState,
	date: string,
	mode: PickerMode,
	slot: ChosenSlot
): BagState {
	return { ...bag, date, mode, slot };
}

/** Made-to-order cakes use the day's places; ready-made cakes do not (K04). */
export function placesNeeded(keys: string[]): number {
	return keys.filter((k) => k.startsWith('v:')).length;
}

/** The bag's date if it is still today or later (London), else none. */
export function currentDate(bag: BagState, today: string): string | undefined {
	return bag.date && bag.date >= today ? bag.date : undefined;
}

/**
 * The checkout link for the bag: `?v=<variantId>` and `?lot=<lotId>` once per
 * cake plus the chosen date if it has not passed (ui/README "Basket") and is
 * not after `lastDate`, the London collect-by date of the bag's ready-made
 * cakes (a too-late date is left for checkout to ask again).
 * Checkout chooses the time itself and prices everything on the server.
 */
export function checkoutHref(bag: BagState, today: string, lastDate?: string): string {
	const q = new URLSearchParams();
	for (const k of bag.keys) {
		const [kind, id] = k.split(':');
		q.append(kind === 'v' ? 'v' : 'lot', id);
	}
	const date = currentDate(bag, today);
	if (date && (!lastDate || date <= lastDate)) q.set('date', date);
	return `/checkout?${q.toString()}`;
}

/** The earliest collect-by instant of the ready-made cakes among these keys. */
export function earliestCollectBy(
	keys: string[],
	ready: { id: string; expires_at: string }[]
): string | undefined {
	const lots = new Set(keys.filter((k) => k.startsWith('lot:')).map((k) => k.slice(4)));
	return ready
		.filter((r) => lots.has(r.id))
		.map((r) => r.expires_at)
		.sort((a, b) => Date.parse(a) - Date.parse(b))[0];
}
