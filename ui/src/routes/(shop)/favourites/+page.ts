import { loadListings } from '../../../lib/shop/load';
import type { PageLoad } from './$types';

// The public catalogue; which cakes are favourites is known only in the browser.
export const load: PageLoad = async ({ fetch }) => ({ listings: await loadListings(fetch) });
