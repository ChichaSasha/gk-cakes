import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

// Liveness for the UI service; it never calls the API or reads secrets.
export const GET: RequestHandler = () =>
	json({ status: 'ok' }, { headers: { 'cache-control': 'no-store' } });
