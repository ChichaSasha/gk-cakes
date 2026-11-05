import { loadListings } from '../../../lib/shop/load';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => ({ listings: await loadListings(fetch) });
