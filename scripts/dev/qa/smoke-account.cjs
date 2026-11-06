// Account smoke check (GK-029): the signed-in customer pages at
// 1440/1024/390/320 px — loaded without an error notice and not still
// loading, one h1, the account nav marks this page, no sideways scrolling,
// no page errors — then Sign out and the signed-out redirect.
// Read only, and checked: every API request other than GET/HEAD/OPTIONS and
// every sign-in provider call other than the password sign-in and a
// scope=local sign-out is stopped in the browser and reported. The delete
// page is skipped (opening it asks the API for a deletion preview).
//   CUSTOMER_PW="$(security find-generic-password -a gk-test-customer@gkcake.com -s gk-supabase-test-customer -w)" \
//     scripts/dev/qa/with-ui.sh -- node scripts/dev/qa/smoke-account.cjs
//   GK_QA_BASE=https://staging.gkcake.com CUSTOMER_PW=… node scripts/dev/qa/smoke-account.cjs
// (Node from ui/.nvmrc.) Exit code 1 on any problem. Screenshots go to
// GK_QA_OUT when set.
const qa = require('./lib.cjs');

const PAGES = ['/account', '/account/profile', '/account/older', '/account/privacy'];
const READS = ['GET', 'HEAD', 'OPTIONS'];

/** Whether a request may leave the browser (reads, the password sign-in, a local sign-out). */
function allowed(req) {
	const url = new URL(req.url());
	if (READS.includes(req.method())) return true;
	if (url.pathname.startsWith('/api/')) return false;
	if (url.pathname.endsWith('/auth/v1/token')) return url.searchParams.get('grant_type') === 'password';
	if (url.pathname.endsWith('/auth/v1/logout')) return url.searchParams.get('scope') === 'local';
	return !url.pathname.includes('/auth/v1/');
}

(async () => {
	const { browser, page, errors } = await qa.open();
	const problems = [];
	let checked = 0;
	try {
		await page.setRequestInterception(true);
		page.on('request', (req) => {
			if (allowed(req)) return void req.continue();
			const url = new URL(req.url());
			problems.push(`stopped a write: ${req.method()} ${url.pathname}${url.search}`);
			void req.abort();
		});
		await qa.customerSignIn(page);
		for (const width of qa.WIDTHS) {
			await page.setViewport({ width, height: 900 });
			for (const path of PAGES) {
				const where = `${width} ${path}`;
				const r = await page.goto(qa.BASE + path, { waitUntil: 'networkidle0' });
				const settled = await page
					.waitForFunction(() => !document.querySelector('[aria-busy="true"]'), { timeout: 15000 })
					.then(() => true)
					.catch(() => false);
				const facts = await page.evaluate(() => ({
					h1: document.querySelectorAll('h1').length,
					current: document.querySelector('.acc-nav a[aria-current="page"]')?.getAttribute('href') ?? null,
					error: document.querySelector('.acc-main .notice.error')?.textContent?.trim() ?? null,
					path: location.pathname
				}));
				if (r?.status() !== 200) problems.push(`${where}: status ${r?.status()}`);
				if (!settled) problems.push(`${where}: still loading after 15 s`);
				if (facts.path !== path) problems.push(`${where}: ended on ${facts.path}`);
				if (facts.h1 !== 1) problems.push(`${where}: ${facts.h1} h1`);
				if (facts.current !== path) problems.push(`${where}: nav marks ${facts.current ?? 'nothing'}`);
				if (facts.error) problems.push(`${where}: error notice "${facts.error.slice(0, 80)}"`);
				if (await qa.overflows(page)) problems.push(`${where}: overflow`);
				if (process.env.GK_QA_OUT && width === 1440) {
					const name = path.split('/').filter(Boolean).join('-');
					await page.screenshot({ path: `${process.env.GK_QA_OUT}/${name}.png`, fullPage: true });
				}
				checked++;
			}
		}
		// Sign out from the account nav, then the account asks to sign in.
		await page.setViewport({ width: 1440, height: 900 });
		await page.goto(`${qa.BASE}/account`, { waitUntil: 'networkidle0' });
		const out = await page.waitForSelector('.acc-nav .acc-nav-out', { timeout: 15000 }).catch(() => null);
		if (!out) {
			problems.push('no Sign out in the account nav');
		} else {
			await out.click();
			await page.waitForFunction(() => location.pathname === '/', { timeout: 15000 }).catch(() => undefined);
			if (new URL(page.url()).pathname !== '/') problems.push(`sign out ended on ${new URL(page.url()).pathname}`);
			await page.goto(`${qa.BASE}/account`, { waitUntil: 'networkidle0' });
			await page.waitForFunction(() => location.pathname === '/sign-in', { timeout: 15000 }).catch(() => undefined);
			const after = new URL(page.url());
			if (after.pathname + after.search !== '/sign-in?next=/account') {
				problems.push(`signed out /account went to ${after.pathname}${after.search}`);
			}
		}
	} catch (e) {
		problems.push(`stopped early: ${String(e?.message ?? e).slice(0, 200)}`);
	} finally {
		await browser.close().catch(() => undefined);
	}
	console.log(JSON.stringify({ base: qa.BASE, pages: checked, problems, errors }, null, 1));
	process.exit(problems.length || errors.length ? 1 : 0);
})();
