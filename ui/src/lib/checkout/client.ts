// Browser calls to the GK Cakes API for checkout and orders (GK-025). A
// signed-in customer is identified by the access token; a guest by the
// quote's proof (in the body or X-Quote-Proof) and then the order capability
// (X-Order-Capability). Thin by design: logic lives in flow.ts.
import { PUBLIC_API_URL } from '$app/env/public';
import { apiGet, apiPost, type ApiOptions } from '../api';
import { currentSession, signInConfigured } from '../auth';
import type { CheckoutApi, Who } from './flow';
import type {
	Accepted,
	Availability,
	Cake,
	CakePage,
	Choices,
	Order,
	Policy,
	Quote,
	ReadyCake
} from './types';

/** Whether this environment can take orders at all (the API origin is set). */
export const checkoutConfigured = Boolean(PUBLIC_API_URL);

function opts(who?: Who, header?: 'X-Quote-Proof' | 'X-Order-Capability'): ApiOptions {
	const o: ApiOptions = { baseUrl: PUBLIC_API_URL as string };
	if (who?.kind === 'account') o.token = who.token;
	else if (who?.kind === 'guest' && header) o.guestSecret = { header, value: who.secret };
	return o;
}

/** The signed-in customer's access token, or undefined for a guest. */
export async function accountToken(): Promise<string | undefined> {
	if (!signInConfigured) return undefined;
	try {
		return (await currentSession())?.access_token;
	} catch {
		return undefined;
	}
}

export const api: CheckoutApi = {
	cakesPage: (cursor) =>
		apiGet<CakePage>('/api/v1/cakes', { ...opts(), query: { limit: 50, cursor } }),
	cake: (id) => apiGet<Cake>(`/api/v1/cakes/${encodeURIComponent(id)}`, opts()),
	readyCakes: async () =>
		(await apiGet<{ cakes: ReadyCake[] }>('/api/v1/ready-cakes', opts())).cakes,
	policy: () => apiGet<Policy>('/api/v1/policies/current', opts()),
	availability: (from, to, quantity) =>
		apiGet<Availability>('/api/v1/availability', { ...opts(), query: { from, to, quantity } }),
	schedule: (date, mode, quantity) =>
		apiGet<Choices>('/api/v1/schedule', {
			...opts(),
			query: { date, fulfillment_mode: mode, purpose: 'new_purchase', quantity }
		}),
	createQuote: (body, key, who) =>
		apiPost<Quote>('/api/v1/checkout/quotes', body, { ...opts(who), idempotencyKey: key }),
	checkout: (body, key, who) =>
		apiPost<Accepted>('/api/v1/checkouts', body, { ...opts(who), idempotencyKey: key }),
	order: (id, who) =>
		apiGet<Order>(`/api/v1/orders/${encodeURIComponent(id)}`, opts(who, 'X-Order-Capability')),
	startPayment: (id, version, key, who) =>
		apiPost<Accepted>(
			`/api/v1/orders/${encodeURIComponent(id)}/payment`,
			{ expected_version: version },
			{ ...opts(who, 'X-Order-Capability'), idempotencyKey: key }
		),
	confirmFree: (id, version, key, who) =>
		apiPost<Accepted>(
			`/api/v1/orders/${encodeURIComponent(id)}/free-confirmation`,
			{ expected_version: version },
			{ ...opts(who, 'X-Order-Capability'), idempotencyKey: key }
		)
};
