import { loadListings } from '../lib/shop/load';
import type { PageLoad } from './$types';

/** The home page shows the four newest cakes (one catalogue page). */
export const load: PageLoad = async ({ fetch }) => ({ listings: await loadListings(fetch, 4) });
