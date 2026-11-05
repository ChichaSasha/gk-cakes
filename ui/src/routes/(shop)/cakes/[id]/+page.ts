import { error } from '@sveltejs/kit';
import { loadCake } from '../../../../lib/shop/load';
import type { PageLoad } from './$types';

// A cake that is not active (archived, unpublished or never existed) is a
// 404 for everyone (K01: public archived detail 404), shown by +error.svelte.
export const load: PageLoad = async ({ fetch, params }) => {
	const result = await loadCake(fetch, params.id);
	if (!result.ok && result.notFound) error(404, 'Not found');
	return { result };
};
