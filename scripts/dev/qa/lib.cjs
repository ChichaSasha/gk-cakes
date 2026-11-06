// Small helpers for browser QA scripts run through with-ui.sh (or against a
// deployed environment with GK_QA_BASE=https://staging.gkcake.com).
//   const qa = require('<repo>/scripts/dev/qa/lib.cjs');
//   const { browser, page, errors } = await qa.open();
// Never prints passwords or tokens.
const path = require('node:path');
const os = require('node:os');

const puppeteer = require(
	process.env.GK_PUPPETEER || path.join(os.homedir(), '.cache/gk-qa/node_modules/puppeteer-core')
);
const CHROME = process.env.GK_CHROME || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const BASE = process.env.GK_QA_BASE || 'http://127.0.0.1:4192';
const PROXY = process.env.GK_QA_PROXY || 'http://127.0.0.1:4191';
const WIDTHS = [1440, 1024, 390, 320];

/** A headless Chrome page that collects page errors and console errors. */
async function open({ width = 1440, height = 900 } = {}) {
	// Chrome never gets the test passwords in its environment.
	const env = { ...process.env };
	delete env.STAFF_PW;
	delete env.CUSTOMER_PW;
	const browser = await puppeteer.launch({ executablePath: CHROME, headless: 'new', env });
	const page = await browser.newPage();
	await page.setViewport({ width, height });
	const errors = [];
	page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`));
	page.on('console', (m) => {
		if (m.type() === 'error') errors.push(`console: ${m.text().slice(0, 300)}`);
	});
	return { browser, page, errors };
}

/** Sets a QA proxy rule, e.g. ctl({ path: '/api/v1/schedule', delay: 2500 }) or ctl({ reset: 1 }). */
async function ctl(params) {
	const q = new URLSearchParams(Object.entries(params).map(([k, v]) => [k, String(v)]));
	const r = await fetch(`${PROXY}/__ctl?${q}`, { headers: { 'x-qa-ctl': process.env.GK_QA_CTL_TOKEN || '' } });
	if (!r.ok) throw new Error(`QA proxy refused the control (${r.status})`);
}

/** True when the page scrolls sideways at the current width. */
function overflows(page) {
	return page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
}

/** Visits each path at each width; returns [{width, path, status, overflow}]. */
async function sweep(page, paths, widths = WIDTHS) {
	const out = [];
	for (const width of widths) {
		await page.setViewport({ width, height: 900 });
		for (const p of paths) {
			const r = await page.goto(BASE + p, { waitUntil: 'networkidle0' });
			out.push({ width, path: p, status: r?.status(), overflow: await overflows(page) });
		}
	}
	return out;
}

/**
 * Signs in on /sign-in with a password (test accounts) and waits for the
 * return page. The password is passed in by the caller (read from the
 * Keychain into the environment) and is never printed.
 */
async function signIn(page, email, password, next = '/admin') {
	if (!password) throw new Error(`no password for ${email}`);
	await page.goto(`${BASE}/sign-in?next=${next}`, { waitUntil: 'networkidle0' });
	const [toggle] = await page.$$('xpath///button[contains(., "Use a password instead")]');
	if (toggle) await toggle.click();
	await page.type('input[type=email]', email);
	await page.type('input[type=password]', password);
	await page.click('form button[type=submit]');
	await page.waitForFunction((n) => location.pathname === n, { timeout: 20000 }, next);
}

/** The staging staff test user (password in STAFF_PW). */
function staffSignIn(page, email = 'gk-test-staff@gkcake.com') {
	return signIn(page, email, process.env.STAFF_PW, '/admin');
}

/** The staging customer test user (password in CUSTOMER_PW), back on /account. */
function customerSignIn(page, email = 'gk-test-customer@gkcake.com') {
	return signIn(page, email, process.env.CUSTOMER_PW, '/account');
}

/** Signs out from /admin (ends the provider session). */
async function staffSignOut(page) {
	await page.goto(`${BASE}/admin`, { waitUntil: 'networkidle0' });
	const [button] = await page.$$('xpath///button[contains(., "Sign out")]');
	if (button) {
		await button.click();
		await new Promise((r) => setTimeout(r, 1500));
	}
}

module.exports = { puppeteer, BASE, PROXY, WIDTHS, open, ctl, overflows, sweep, signIn, staffSignIn, customerSignIn, staffSignOut };
