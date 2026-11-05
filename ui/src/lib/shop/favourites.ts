// Favourites kept on this device (approved GK-016 r01c sketch; contracts K09):
// the cake IDs a customer saved with the heart, in this browser's
// localStorage, until the end of the same London calendar date one year after
// the last save or removal (a missing day, 29 February, clamps to the month's
// last day). Reading never extends the year. Only public catalogue IDs are
// kept: no names, prices or personal data. Adding them to an account needs
// the account API (K02 favourites PUT/DELETE) and is not built yet.
import { londonToday } from '../checkout/format';
import { isId } from './client';

export const FAVOURITES_KEY = 'gk-shop-favourites';
/** More than any customer would save; keeps a damaged entry bounded. */
export const MAX_FAVOURITES = 200;

export interface FavouritesState {
	/** Saved cake IDs, oldest first. */
	ids: string[];
	/** When the device stops keeping them (ISO instant), or null when nothing is saved. */
	expires: string | null;
}

export function emptyFavourites(): FavouritesState {
	return { ids: [], expires: null };
}

/** The stored favourites, or none when missing, damaged or expired at `now`. */
export function parseFavourites(raw: string | null, now: number): FavouritesState {
	if (!raw) return emptyFavourites();
	let v: unknown;
	try {
		v = JSON.parse(raw);
	} catch {
		return emptyFavourites();
	}
	if (typeof v !== 'object' || v === null) return emptyFavourites();
	const { ids, expires } = v as Record<string, unknown>;
	if (!Array.isArray(ids) || typeof expires !== 'string') return emptyFavourites();
	const until = Date.parse(expires);
	if (!Number.isFinite(until) || until <= now) return emptyFavourites();
	// Never kept longer than a save made now would be (a damaged or edited entry).
	const latest = expiryAfter(now);
	const keptUntil = until > Date.parse(latest) ? latest : expires;
	const kept: string[] = [];
	for (const id of ids) {
		if (typeof id === 'string' && isId(id) && !kept.includes(id)) kept.push(id);
		if (kept.length === MAX_FAVOURITES) break;
	}
	return kept.length > 0 ? { ids: kept, expires: keptUntil } : emptyFavourites();
}

/** The parts of the Web Storage API used here (localStorage, or a test double). */
export type DeviceStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

/**
 * The favourites stored on this device at `now`. An expired or damaged entry
 * is removed (reading never extends the year). Null when the browser does
 * not let this page use its storage.
 */
export function readFavourites(storage: DeviceStorage, now: number): FavouritesState | null {
	try {
		const raw = storage.getItem(FAVOURITES_KEY);
		const state = parseFavourites(raw, now);
		if (raw !== null && state.ids.length === 0) storage.removeItem(FAVOURITES_KEY);
		return state;
	} catch {
		return null;
	}
}

/**
 * Presses the heart on `id` at `now`, starting from what is stored now (so
 * favourites that expired while a page stayed open never come back). Returns
 * the new favourites, or null when the browser could not keep the change.
 */
export function storeToggle(
	storage: DeviceStorage,
	id: string,
	now: number
): FavouritesState | null {
	const current = readFavourites(storage, now);
	if (current === null) return null;
	const next = toggled(current, id, now);
	try {
		if (next.ids.length === 0) storage.removeItem(FAVOURITES_KEY);
		else storage.setItem(FAVOURITES_KEY, JSON.stringify(next));
	} catch {
		return null;
	}
	return next;
}

/** The saved cakes that are still in the collection, in saved order. */
export function savedListings<T extends { id: string }>(ids: string[], all: T[]): T[] {
	const byId: Record<string, T> = {};
	for (const l of all) byId[l.id] = l;
	return ids.map((id) => byId[id]).filter((l): l is T => l !== undefined);
}

/**
 * Which heart gets focus after the one at `removed` (of `before` hearts) is
 * removed: the next one, else the new last one; null when none are left.
 */
export function focusAfterRemoval(removed: number, before: number): number | null {
	const remaining = before - 1;
	if (remaining <= 0) return null;
	return Math.min(Math.max(removed, 0), remaining - 1);
}

/** The favourites after the heart on `id` is pressed, saved or removed at `now`. */
export function toggled(state: FavouritesState, id: string, now: number): FavouritesState {
	if (!isId(id)) return state;
	const ids = state.ids.includes(id)
		? state.ids.filter((x) => x !== id)
		: [...state.ids, id].slice(-MAX_FAVOURITES);
	return ids.length > 0 ? { ids, expires: expiryAfter(now) } : emptyFavourites();
}

const PARTS = new Intl.DateTimeFormat('en-GB', {
	timeZone: 'Europe/London',
	year: 'numeric',
	month: '2-digit',
	day: '2-digit',
	hour: '2-digit',
	minute: '2-digit',
	second: '2-digit',
	hourCycle: 'h23'
});

function londonClock(t: number): string {
	const p = Object.fromEntries(PARTS.formatToParts(new Date(t)).map((x) => [x.type, x.value]));
	return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}:${p.second}`;
}

/** The last instant of a London calendar date (23:59:59.999 London time). */
export function londonEndOfDay(date: string): number {
	const [y, m, d] = date.split('-').map(Number);
	// London is UTC+0 (GMT) or UTC+1 (BST); late evening is never inside a clock change.
	for (const offsetHours of [0, 1]) {
		const t = Date.UTC(y, m - 1, d, 23, 59, 59, 999) - offsetHours * 3_600_000;
		if (londonClock(t) === `${date} 23:59:59`) return t;
	}
	throw new Error(`no London end of day for ${date}`);
}

/** The same London date one year on, clamped to the month's last day (29 Feb → 28 Feb). */
export function anniversary(date: string): string {
	const [y, m, d] = date.split('-').map(Number);
	const last = new Date(Date.UTC(y + 1, m, 0)).getUTCDate();
	return `${y + 1}-${String(m).padStart(2, '0')}-${String(Math.min(d, last)).padStart(2, '0')}`;
}

/** When favourites saved or changed at `now` stop being kept (ISO instant). */
export function expiryAfter(now: number): string {
	return new Date(londonEndOfDay(anniversary(londonToday(new Date(now))))).toISOString();
}
