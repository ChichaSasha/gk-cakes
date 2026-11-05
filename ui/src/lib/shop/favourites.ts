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
	const kept: string[] = [];
	for (const id of ids) {
		if (typeof id === 'string' && isId(id) && !kept.includes(id)) kept.push(id);
		if (kept.length === MAX_FAVOURITES) break;
	}
	return kept.length > 0 ? { ids: kept, expires } : emptyFavourites();
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
