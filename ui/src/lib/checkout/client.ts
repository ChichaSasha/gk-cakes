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
	CancellationPreview,
	Choices,
	MoveChoices,
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

/** The order page's change commands (GK-030): cancellation and the same-day pickup move. */
export interface OrderChangesApi {
	cancellationPreview(id: string, key: string, who: Who): Promise<CancellationPreview>;
	cancel(
		id: string,
		body: { expected_version: number; preview_id: string; preview_hash: string },
		key: string,
		who: Who
	): Promise<Accepted>;
	moveChoices(id: string, who: Who): Promise<MoveChoices>;
	movePickup(
		id: string,
		body: { target_slot_id: string; expected_version: number },
		key: string,
		who: Who
	): Promise<Accepted | { order_id: string; state: 'unchanged' }>;
}

export const changesApi: OrderChangesApi = {
	// A read-like POST (no body other than {}); the key header is ignored by the API.
	cancellationPreview: (id, key, who) =>
		apiPost<CancellationPreview>(
			`/api/v1/orders/${encodeURIComponent(id)}/cancellation-preview`,
			{},
			{ ...opts(who, 'X-Order-Capability'), idempotencyKey: key }
		),
	cancel: (id, body, key, who) =>
		apiPost<Accepted>(
			`/api/v1/orders/${encodeURIComponent(id)}/cancel`,
			{
				expected_version: body.expected_version,
				preview_id: body.preview_id,
				preview_hash: body.preview_hash
			},
			{ ...opts(who, 'X-Order-Capability'), idempotencyKey: key }
		),
	moveChoices: (id, who) =>
		apiGet<MoveChoices>('/api/v1/schedule', {
			...opts(who, 'X-Order-Capability'),
			query: { purpose: 'pickup_move', order_id: id }
		}),
	movePickup: (id, body, key, who) =>
		apiPost<Accepted | { order_id: string; state: 'unchanged' }>(
			`/api/v1/orders/${encodeURIComponent(id)}/pickup`,
			{ target_slot_id: body.target_slot_id, expected_version: body.expected_version },
			{ ...opts(who, 'X-Order-Capability'), idempotencyKey: key }
		)
};

/** Body of the Q5 and failed-delivery choices (target_slot_id only for a time choice). */
export interface ChoiceBody {
	expected_version: number;
	choice: string;
	target_slot_id?: string;
}

/**
 * The order page's slice 2 commands (GK-030): the late-payment answer, the
 * delivery payment and the two delivery choices. Each is a 202 command with
 * an Idempotency-Key; the page then reads the order.
 */
export interface OrderRecoveryApi {
	order(id: string, who: Who): Promise<Order>;
	latePaymentAnswer(
		id: string,
		body: { expected_version: number; offer_id: string; answer: 'accept' | 'decline' },
		key: string,
		who: Who
	): Promise<Accepted>;
	deliveryPayment(
		id: string,
		body: { expected_version: number; quote_id: string },
		key: string,
		who: Who
	): Promise<Accepted>;
	deliveryChoice(id: string, body: ChoiceBody, key: string, who: Who): Promise<Accepted>;
	deliveryFailureChoice(id: string, body: ChoiceBody, key: string, who: Who): Promise<Accepted>;
}

const post = (id: string, path: string, body: object, key: string, who: Who) =>
	apiPost<Accepted>(`/api/v1/orders/${encodeURIComponent(id)}/${path}`, body, {
		...opts(who, 'X-Order-Capability'),
		idempotencyKey: key
	});

const choiceBody = (b: ChoiceBody): ChoiceBody => ({
	expected_version: b.expected_version,
	choice: b.choice,
	...(b.target_slot_id ? { target_slot_id: b.target_slot_id } : {})
});

export const recoveryApi: OrderRecoveryApi = {
	order: (id, who) => api.order(id, who),
	latePaymentAnswer: (id, b, key, who) =>
		post(
			id,
			'late-payment/answer',
			{ expected_version: b.expected_version, offer_id: b.offer_id, answer: b.answer },
			key,
			who
		),
	deliveryPayment: (id, b, key, who) =>
		post(
			id,
			'delivery-payment',
			{ expected_version: b.expected_version, quote_id: b.quote_id },
			key,
			who
		),
	deliveryChoice: (id, b, key, who) => post(id, 'delivery-choice', choiceBody(b), key, who),
	deliveryFailureChoice: (id, b, key, who) =>
		post(id, 'delivery-failure-choice', choiceBody(b), key, who)
};
