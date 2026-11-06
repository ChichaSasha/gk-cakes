// Account smoke check (GK-029): the signed-in customer pages at
// 1440/1024/390/320 px — loaded without an error notice, one h1, the account
// nav marks the page, no sideways scrolling, no page errors — then Sign out
// and the signed-out redirect. Read only: it opens pages and clicks nothing
// that writes (the delete page is skipped: opening it asks the API for a
// deletion preview). Sign out ends only this browser's session.
//   CUSTOMER_PW="$(security find-generic-password -a gk-test-customer@gkcake.com -s gk-supabase-test-customer -w)" \
//     scripts/dev/qa/with-ui.sh -- node scripts/dev/qa/smoke-account.cjs
//   GK_QA_BASE=https://staging.gkcake.com CUSTOMER_PW=… node scripts/dev/qa/smoke-account.cjs
// Exit code 1 on any problem. Screenshots go to GK_QA_OUT when set.
const qa = require('./lib.cjs');

const PAGES = ['/account', '/account/profile', '/account/older', '/account/privacy'];

async function settled(page) {
	await page
		.waitForFunction(() => !document.querySelector('[aria-busy="true"]'), { timeout: 15000 })
		.catch(() => undefined);
}

(async () => {
	const { browser, page, errors } = await qa.open();
	const problems = [];
	await qa.customerSignIn(page);
	let checked = 0;
	for (const width of qa.WIDTHS) {
		await page.setViewport({ width, height: 900 });
		for (const path of PAGES) {
			const r = await page.goto(qa.BASE + path, { waitUntil: 'networkidle0' });
			await settled(page);
			const facts = await page.evaluate(() => ({
				h1: document.querySelectorAll('h1').length,
				current: document.querySelector('.acc-nav [aria-current]')?.textContent?.trim() ?? null,
				error: document.querySelector('.acc-main .notice.error')?.textContent?.trim() ?? null,
				path: location.pathname
			}));
			const where = `${width} ${path}`;
			if (r?.status() !== 200) problems.push(`${where}: status ${r?.status()}`);
			if (facts.path !== path) problems.push(`${where}: ended on ${facts.path}`);
			if (facts.h1 !== 1) problems.push(`${where}: ${facts.h1} h1`);
			if (!facts.current) problems.push(`${where}: no current nav item`);
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
	await settled(page);
	await page.click('.acc-nav .acc-nav-out');
	await page.waitForFunction(() => location.pathname === '/', { timeout: 15000 }).catch(() => undefined);
	if (new URL(page.url()).pathname !== '/') problems.push(`sign out ended on ${page.url()}`);
	await page.goto(`${qa.BASE}/account`, { waitUntil: 'networkidle0' });
	await page.waitForFunction(() => location.pathname === '/sign-in', { timeout: 15000 }).catch(() => undefined);
	const after = new URL(page.url());
	if (after.pathname + after.search !== '/sign-in?next=/account') {
		problems.push(`signed out /account went to ${after.pathname}${after.search}`);
	}
	await browser.close();
	console.log(JSON.stringify({ base: qa.BASE, pages: checked, problems, errors }, null, 1));
	process.exit(problems.length || errors.length ? 1 : 0);
})();
