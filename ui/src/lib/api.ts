// API client (GK-021, GK-025): calls the GK Cakes API with the signed-in
// user's access token or a guest's quote/order secret. Contracts K00: JSON
// over HTTPS, error body {error:{code,message,request_id,retryable}},
// commands with an Idempotency-Key; tokens and secrets only in headers, never
// in URLs or logs; no cookies.

export interface ApiErrorBody {
	code: string;
	message: string;
	request_id: string;
	retryable: boolean;
}

/** An API failure; status 0 means the API could not be reached. */
export class ApiError extends Error {
	readonly status: number;
	readonly code: string;
	readonly requestId: string;
	readonly retryable: boolean;

	constructor(status: number, body: Partial<ApiErrorBody>) {
		super(body.message || 'Something went wrong');
		this.name = 'ApiError';
		this.status = status;
		this.code = body.code || 'UNKNOWN';
		this.requestId = body.request_id || '';
		this.retryable = body.retryable ?? status >= 500;
	}

	/** The user must sign in (again): no, refused or ended session. */
	get signInRequired(): boolean {
		return this.status === 401;
	}
}

export interface ApiOptions {
	/** Exact API origin, e.g. https://staging-api.gkcake.com. */
	baseUrl: string;
	/** Access token for authenticated routes. */
	token?: string;
	/**
	 * A guest's secret for this quote or order (32 random bytes, base64url),
	 * sent only as a header (X-Quote-Proof or X-Order-Capability), never in
	 * the URL. Ignored when a token is sent: the API judges a request with a
	 * token as the signed-in customer's.
	 */
	guestSecret?: { header: 'X-Quote-Proof' | 'X-Order-Capability'; value: string };
	/** Query parameters, appended to the path (values are encoded). */
	query?: Record<string, string | number | undefined>;
	fetch?: typeof fetch;
}

export interface PostOptions extends ApiOptions {
	/**
	 * K00 command key: one per user action, reused when the same action is
	 * retried so the API answers with the original result.
	 */
	idempotencyKey: string;
}

const API_PATH = /^\/api\/v1\/[a-z0-9/_-]+$/;
const SECRET = /^[A-Za-z0-9_-]{43}$/;

function url(path: string, opts: ApiOptions): string {
	if (!API_PATH.test(path) || path.includes('//')) {
		throw new Error(`not an API path: ${path}`);
	}
	const params = new URLSearchParams();
	for (const [k, v] of Object.entries(opts.query ?? {})) {
		if (v !== undefined) params.set(k, String(v));
	}
	const q = params.toString();
	return opts.baseUrl + path + (q ? `?${q}` : '');
}

function headers(opts: ApiOptions): Record<string, string> {
	const h: Record<string, string> = { Accept: 'application/json' };
	if (opts.token) {
		h.Authorization = `Bearer ${opts.token}`;
	} else if (opts.guestSecret) {
		if (!SECRET.test(opts.guestSecret.value)) throw new Error('not a guest secret');
		h[opts.guestSecret.header] = opts.guestSecret.value;
	}
	return h;
}

async function send<T>(target: string, init: RequestInit, opts: ApiOptions): Promise<T> {
	let response: Response;
	try {
		response = await (opts.fetch ?? fetch)(target, {
			...init,
			credentials: 'omit',
			cache: 'no-store',
			referrerPolicy: 'no-referrer'
		});
	} catch {
		throw new ApiError(0, {
			code: 'NETWORK',
			message: 'The service could not be reached. Check your connection and try again.',
			retryable: true
		});
	}
	let body: unknown;
	try {
		body = await response.json();
	} catch {
		body = undefined;
	}
	if (!response.ok) {
		const error = (body as { error?: Partial<ApiErrorBody> } | undefined)?.error ?? {};
		throw new ApiError(response.status, error);
	}
	return body as T;
}

/** GETs an /api/v1 path and returns its JSON body, or throws ApiError. */
export async function apiGet<T>(path: string, opts: ApiOptions): Promise<T> {
	return send<T>(url(path, opts), { method: 'GET', headers: headers(opts) }, opts);
}

/**
 * POSTs a JSON command to an /api/v1 path with its Idempotency-Key and
 * returns the JSON answer, or throws ApiError. The body is sent exactly as
 * given: the API refuses unknown fields and nulls (strict JSON).
 */
export async function apiPost<T>(path: string, body: object, opts: PostOptions): Promise<T> {
	if (!/^[\x21-\x7e]{1,128}$/.test(opts.idempotencyKey)) {
		throw new Error('not an Idempotency-Key');
	}
	const h = headers(opts);
	h['Content-Type'] = 'application/json';
	h['Idempotency-Key'] = opts.idempotencyKey;
	return send<T>(url(path, opts), { method: 'POST', headers: h, body: JSON.stringify(body) }, opts);
}

/**
 * PATCHes an /api/v1 path with a JSON command and its Idempotency-Key (the
 * staff catalogue's partial updates; body sent exactly as given), or throws
 * ApiError.
 */
export async function apiPatch<T>(path: string, body: object, opts: PostOptions): Promise<T> {
	if (!/^[\x21-\x7e]{1,128}$/.test(opts.idempotencyKey)) {
		throw new Error('not an Idempotency-Key');
	}
	const h = headers(opts);
	h['Content-Type'] = 'application/json';
	h['Idempotency-Key'] = opts.idempotencyKey;
	return send<T>(
		url(path, opts),
		{ method: 'PATCH', headers: h, body: JSON.stringify(body) },
		opts
	);
}

/** A new Idempotency-Key for one user action. */
export function newIdempotencyKey(): string {
	return crypto.randomUUID();
}

/** Pages a sign-in may return to; anything else goes to /account. */
export const RETURN_PATHS = ['/account', '/admin'] as const;
export type ReturnPath = (typeof RETURN_PATHS)[number];

/** Returns path if it is an allowed return page, otherwise /account. */
export function safeReturnPath(path: string | null | undefined): ReturnPath {
	return (RETURN_PATHS as readonly string[]).includes(path ?? '')
		? (path as ReturnPath)
		: '/account';
}

export type TokenHashDecision = 'verify' | 'already-signed-in' | 'refused';

/**
 * Whether the callback may verify an emailed token hash. Unlike a PKCE code,
 * a token hash is not bound to the browser that asked for it, so a crafted
 * link could sign a visitor into someone else's account (login CSRF). Only
 * invitations use it (sign-in links use PKCE), and never over an existing
 * session.
 */
export function tokenHashDecision(type: string | null, signedIn: boolean): TokenHashDecision {
	if (type !== 'invite') return 'refused';
	return signedIn ? 'already-signed-in' : 'verify';
}
