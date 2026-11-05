// Public catalogue and availability reads for the discovery pages (GK-028).
// Anonymous calls only (K01): no token, guest secret or personal data. Used
// by universal loads (the server renders the first page; the browser
// refetches on "Try again") with SvelteKit's fetch, and by the date picker.
import { ApiError, apiGet } from '../api';
import type {
	Availability,
	Cake,
	CakePage,
	Choices,
	FulfillmentMode,
	ReadyCake
} from '../checkout/types';

export interface ShopApi {
	/** Every active cake (all pages), or only the newest `limit` (one page). */
	cakes(limit?: number): Promise<Cake[]>;
	cake(id: string): Promise<Cake>;
	readyCakes(): Promise<ReadyCake[]>;
	availability(from: string, to: string, quantity: number): Promise<Availability>;
	schedule(date: string, mode: FulfillmentMode, quantity: number): Promise<Choices>;
}

/** The API's page size limit, and how many pages a shop listing reads at most (500 cakes). */
const PAGE_LIMIT = 50;
const MAX_PAGES = 10;
/**
 * How long one page load (all its requests together, given as `deadline`) or
 * one picker request may wait for the API.
 */
export const TIMEOUT_MS = 8000;

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

/** Whether an ID can be a catalogue ID (anything else is answered as not found without a call). */
export function isId(id: string): boolean {
	return UUID.test(id);
}

function withTimeout(f: typeof fetch, deadline?: AbortSignal): typeof fetch {
	return (input, init) =>
		f(input, { ...init, signal: deadline ?? AbortSignal.timeout(TIMEOUT_MS) });
}

/**
 * Public reads. `deadline` bounds every request of one page load together;
 * without it each request gets TIMEOUT_MS (the long-lived picker).
 */
export function shopApi(
	baseUrl: string | undefined,
	f: typeof fetch = fetch,
	deadline?: AbortSignal
): ShopApi {
	const opts = () => {
		if (!baseUrl) {
			throw new ApiError(503, {
				code: 'NOT_CONFIGURED',
				message: 'The shop is not configured.',
				retryable: false
			});
		}
		return { baseUrl, fetch: withTimeout(f, deadline) };
	};
	return {
		async cakes(limit) {
			const all: Cake[] = [];
			let cursor: string | undefined;
			for (let i = 0; i < MAX_PAGES; i++) {
				const page = await apiGet<CakePage>('/api/v1/cakes', {
					...opts(),
					query: { limit: limit ?? PAGE_LIMIT, cursor }
				});
				if (limit !== undefined) return page.cakes;
				all.push(...page.cakes);
				cursor = page.next_cursor;
				if (!cursor) break;
			}
			return all;
		},
		async cake(id) {
			if (!isId(id)) throw new ApiError(404, { code: 'NOT_FOUND', message: 'Not found' });
			return apiGet<Cake>(`/api/v1/cakes/${id}`, opts());
		},
		async readyCakes() {
			return (await apiGet<{ cakes: ReadyCake[] }>('/api/v1/ready-cakes', opts())).cakes;
		},
		availability: (from, to, quantity) =>
			apiGet<Availability>('/api/v1/availability', { ...opts(), query: { from, to, quantity } }),
		schedule: (date, mode, quantity) =>
			apiGet<Choices>('/api/v1/schedule', {
				...opts(),
				query: { date, fulfillment_mode: mode, purpose: 'new_purchase', quantity }
			})
	};
}

/** A load result the page can always render: data, or a failure to recover from. */
export type Loaded<T> =
	{ ok: true; data: T } | { ok: false; notFound: boolean; retryable: boolean; message: string };

export async function settle<T>(work: Promise<T>): Promise<Loaded<T>> {
	try {
		return { ok: true, data: await work };
	} catch (e) {
		const err =
			e instanceof ApiError
				? e
				: new ApiError(0, { code: 'NETWORK', message: 'Unavailable', retryable: true });
		return {
			ok: false,
			notFound: err.status === 404,
			retryable: err.retryable || err.status === 0,
			message: err.message
		};
	}
}
