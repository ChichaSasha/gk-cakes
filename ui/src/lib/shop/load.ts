// Loads for the discovery pages (GK-028). The server renders the first view
// (public catalogue only, K01) and the browser reloads on "Try again". A
// failure never throws: the page shows its own recovery state.
import { PUBLIC_API_URL } from '$app/env/public';
import { listing, type Listing } from './catalog';
import { settle, shopApi, TIMEOUT_MS, type Loaded } from './client';
import type { Cake, ReadyCake } from '../checkout/types';

/** The public API for one page load: all its requests share one deadline. */
export function apiFor(f: typeof fetch) {
	return shopApi(PUBLIC_API_URL, f, AbortSignal.timeout(TIMEOUT_MS));
}

/**
 * Active cakes as shop cards (every cake, or the newest `limit`), with the
 * ready-made cakes on offer (their collect-by dates feed the date picker).
 */
export async function loadListings(
	f: typeof fetch,
	limit?: number
): Promise<Loaded<{ listings: Listing[]; ready: ReadyCake[] }>> {
	const api = apiFor(f);
	return settle(
		Promise.all([api.cakes(limit), api.readyCakes()]).then(([cakes, ready]) => ({
			listings: cakes.map((c) => listing(c, ready)),
			ready
		}))
	);
}

/**
 * One cake and every ready-made cake on offer (this cake's options, and the
 * collect-by dates of ready-made cakes already in the bag).
 */
export async function loadCake(
	f: typeof fetch,
	id: string
): Promise<Loaded<{ cake: Cake; ready: ReadyCake[] }>> {
	const api = apiFor(f);
	return settle(
		Promise.all([api.cake(id), api.readyCakes()]).then(([cake, ready]) => ({ cake, ready }))
	);
}
