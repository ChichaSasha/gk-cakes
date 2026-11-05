// API client (GK-021): calls the GK Cakes API with the signed-in user's
// access token. Contracts K00: JSON over HTTPS, error body
// {error:{code,message,request_id,retryable}}; tokens never in URLs or logs.

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
	fetch?: typeof fetch;
}

/** Request headers a caller may add (besides Accept, Content-Type, Authorization). */
export type ExtraHeader = 'Idempotency-Key' | 'X-Order-Capability' | 'X-Quote-Proof';

export interface ApiRequest {
	method?: 'GET' | 'POST';
	/** Query parameters, appended to the path. */
	query?: Record<string, string | number | undefined>;
	/** JSON body (POST). */
	body?: unknown;
	headers?: Partial<Record<ExtraHeader, string>>;
}

/** GETs an /api/v1 path and returns its JSON body, or throws ApiError. */
export async function apiGet<T>(path: string, opts: ApiOptions): Promise<T> {
	return apiRequest<T>(path, {}, opts);
}

/**
 * Calls an /api/v1 path and returns its JSON body, or throws ApiError.
 * Credentials travel only in headers (never cookies or the URL).
 */
export async function apiRequest<T>(path: string, req: ApiRequest, opts: ApiOptions): Promise<T> {
	if (!/^\/api\/v1\/[a-z0-9/_-]+$/.test(path)) {
		throw new Error(`not an API path: ${path}`);
	}
	const method = req.method ?? 'GET';
	const headers: Record<string, string> = { Accept: 'application/json' };
	if (opts.token) headers.Authorization = `Bearer ${opts.token}`;
	for (const [name, value] of Object.entries(req.headers ?? {})) {
		if (value) headers[name] = value;
	}
	let body: string | undefined;
	if (req.body !== undefined) {
		headers['Content-Type'] = 'application/json';
		body = JSON.stringify(req.body);
	}
	const query = new URLSearchParams();
	for (const [name, value] of Object.entries(req.query ?? {})) {
		if (value !== undefined) query.set(name, String(value));
	}
	const url = opts.baseUrl + path + (query.size > 0 ? `?${query}` : '');
	let response: Response;
	try {
		response = await (opts.fetch ?? fetch)(url, {
			method,
			headers,
			body,
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
	let parsed: unknown;
	try {
		parsed = await response.json();
	} catch {
		parsed = undefined;
	}
	if (!response.ok) {
		const error = (parsed as { error?: Partial<ApiErrorBody> } | undefined)?.error ?? {};
		throw new ApiError(response.status, error);
	}
	return parsed as T;
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
