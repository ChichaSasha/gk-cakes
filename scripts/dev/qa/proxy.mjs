// Loopback proxy from a local UI build to the staging API, for browser QA.
// The staging API only allows its own UI origin, so a local build reaches it
// through this proxy, which adds CORS for loopback origins.
//
//   PORT=4191 UPSTREAM=https://staging-api.gkcake.com node scripts/dev/qa/proxy.mjs
//   (Node 18 or later; with-ui.sh uses the version in ui/.nvmrc)
//
// Read-only by design: only GET (and CORS preflight) is forwarded, so no
// command can reach staging through it. A signed-in test user's bearer token
// and a guest's X-Quote-Proof / X-Order-Capability secret are forwarded and
// never logged. CORS: a loopback origin is echoed (the UI's server-side render
// sends one, and SvelteKit checks CORS like a browser), a request without an
// Origin gets '*', and any other site gets no CORS header. Test controls
// (GET only) need the header `x-qa-ctl: $GK_QA_CTL_TOKEN` when that variable is
// set (with-ui.sh sets a random one) and are refused for any cross-site request:
//   GET /__ctl?path=/api/v1/schedule&delay=2500   delay matching requests (ms)
//   GET /__ctl?path=/api/v1/cakes&fail=2          answer the next 2 with 503
//   GET /__ctl?path=/api/v1/schedule&body=<json>  answer with this JSON (200, or &status=)
//   GET /__ctl?path=/api/v1/x&method=POST&status=409&body=<json>
//                                                 a fixed answer for a command; commands
//                                                 are never forwarded, only answered here
//   GET /__ctl?reset=1                            clear every rule
//   GET /__ctl?log=1                              recent request paths (and clear)
// A rule matches a request whose path starts with `path`.
import http from 'node:http';

const PORT = Number(process.env.PORT || 4191);
const UPSTREAM = process.env.UPSTREAM || 'https://staging-api.gkcake.com';
const rules = new Map(); // "METHOD path prefix" -> { delay, fail, body, status }
let log = [];
// SvelteKit's server-side fetch sends the page origin, as https.
const LOOPBACK = /^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?$/;
const FORWARD = ['authorization', 'x-quote-proof', 'x-order-capability'];
const PASS_BACK = ['content-type', 'cache-control', 'x-request-id', 'retry-after'];
const CTL_TOKEN = process.env.GK_QA_CTL_TOKEN || '';

function rule(method, pathname) {
	let best;
	for (const [k, r] of rules) {
		const [m, prefix] = [k.slice(0, k.indexOf(' ')), k.slice(k.indexOf(' ') + 1)];
		if (m === method && pathname.startsWith(prefix) && (!best || prefix.length > best[0].length)) best = [prefix, r];
	}
	return best?.[1];
}

http
	.createServer(async (req, res) => {
		const origin = req.headers.origin;
		const cors = { vary: 'origin' };
		if (!origin || LOOPBACK.test(origin)) {
			cors['access-control-allow-origin'] = origin || '*';
			cors['access-control-allow-headers'] = 'authorization, content-type, x-quote-proof, x-order-capability';
			cors['access-control-allow-methods'] = 'GET';
		}
		const url = new URL(req.url, 'http://proxy');
		if (url.pathname === '/__ctl') {
			const site = req.headers['sec-fetch-site'];
			const crossSite = (origin && !LOOPBACK.test(origin)) || (site && site !== 'same-origin' && site !== 'none');
			if (req.method !== 'GET' || crossSite || (CTL_TOKEN && req.headers['x-qa-ctl'] !== CTL_TOKEN)) {
				res.writeHead(403);
				return res.end();
			}
			const q = url.searchParams;
			if (q.has('reset')) rules.clear();
			if (q.has('log')) {
				res.writeHead(200, { 'content-type': 'application/json' });
				res.end(JSON.stringify(log));
				log = [];
				return;
			}
			if (q.has('path')) {
				const key = `${(q.get('method') || 'GET').toUpperCase()} ${q.get('path')}`;
				const r = rules.get(key) || {};
				if (q.has('delay')) r.delay = Number(q.get('delay'));
				if (q.has('fail')) r.fail = Number(q.get('fail'));
				if (q.has('body')) r.body = q.get('body') || undefined;
				if (q.has('status')) r.status = Number(q.get('status')) || undefined;
				rules.set(key, r);
			}
			res.writeHead(204);
			return res.end();
		}
		if (req.method === 'OPTIONS') {
			if (cors['access-control-allow-origin']) {
				cors['access-control-allow-methods'] = 'GET, POST, PATCH';
				cors['access-control-allow-headers'] += ', idempotency-key';
			}
			res.writeHead(204, cors);
			return res.end();
		}
		log.push(`${req.method === 'GET' ? '' : `${req.method} `}${url.pathname}${url.search}`);
		if (log.length > 500) log = log.slice(-500);
		const r = rule(req.method, url.pathname);
		if (req.method !== 'GET') {
			// A command is answered only by a test rule, never forwarded to staging.
			req.resume();
			if (r?.delay) await new Promise((done) => setTimeout(done, r.delay));
			if (r?.fail > 0) {
				r.fail--;
				// The answer is lost: the connection breaks with a malformed reply, so the browser sees a
				// network error (closing an idle socket instead would make it resend the command).
				req.socket.end('XX\r\n\r\n');
				return;
			}
			if (r?.body || r?.status) {
				res.writeHead(r.status || 200, { ...cors, 'content-type': 'application/json' });
				return res.end(r.body || '{}');
			}
			res.writeHead(405, { ...cors, 'content-type': 'application/json' });
			return res.end('{"error":{"code":"READ_ONLY_PROXY","message":"Commands are never forwarded","request_id":"","retryable":false}}');
		}
		if (r?.delay) await new Promise((done) => setTimeout(done, r.delay));
		if (r?.fail > 0) {
			r.fail--;
			res.writeHead(503, { ...cors, 'content-type': 'application/json' });
			return res.end('{"error":{"code":"UNAVAILABLE","message":"QA proxy failure","request_id":"","retryable":true}}');
		}
		if (r?.body) {
			res.writeHead(r.status || 200, { ...cors, 'content-type': 'application/json' });
			return res.end(r.body);
		}
		const headers = { accept: 'application/json' };
		for (const h of FORWARD) if (req.headers[h]) headers[h] = req.headers[h];
		try {
			const up = await fetch(UPSTREAM + url.pathname + url.search, { headers });
			const body = Buffer.from(await up.arrayBuffer());
			const back = { ...cors };
			for (const h of PASS_BACK) if (up.headers.get(h)) back[h] = up.headers.get(h);
			res.writeHead(up.status, back);
			res.end(body);
		} catch (e) {
			console.error(`QA proxy: upstream ${url.pathname} failed: ${e.message}`);
			res.writeHead(502, cors);
			res.end();
		}
	})
	.listen(PORT, '127.0.0.1', () =>
		console.error(`QA proxy on http://127.0.0.1:${PORT} -> ${UPSTREAM} (GET only${CTL_TOKEN ? '' : '; test controls without a token'})`)
	);
