// Shop smoke check: every discovery page at 1440/1024/390/320 px, no
// sideways scrolling, expected status codes, no page errors.
//   scripts/dev/qa/with-ui.sh --auth none -- node scripts/dev/qa/smoke-shop.cjs
//   GK_QA_BASE=https://staging.gkcake.com node scripts/dev/qa/smoke-shop.cjs
// Exit code 1 on any problem. Screenshots go to GK_QA_OUT when set.
const qa = require('./lib.cjs');

(async () => {
	const { browser, page, errors } = await qa.open();
	const api = await page.goto(`${qa.BASE}/shop`, { waitUntil: 'networkidle0' });
	const cake = await page.$eval('.grid .product a', (a) => a.getAttribute('href')).catch(() => null);
	const paths = ['/', '/shop', '/sizes', '/bag', '/favourites', ...(cake ? [cake] : [])];
	const rows = await qa.sweep(page, paths);
	if (process.env.GK_QA_OUT) {
		await page.setViewport({ width: 1440, height: 900 });
		await page.goto(`${qa.BASE}/shop`, { waitUntil: 'networkidle0' });
		await page.screenshot({ path: `${process.env.GK_QA_OUT}/shop.png`, fullPage: true });
	}
	// The unknown cake's 404 logs an expected console error: ignore only those.
	const before = errors.length;
	const missing = await page.goto(`${qa.BASE}/cakes/00000000-0000-4000-8000-000000000000`, { waitUntil: 'networkidle0' });
	const expected = errors.splice(before).filter((e) => !/status of 404/.test(e));
	errors.push(...expected);
	const problems = rows.filter((r) => r.status !== 200 || r.overflow).map((r) => `${r.width} ${r.path}: status ${r.status}${r.overflow ? ', overflow' : ''}`);
	if (missing.status() !== 404) problems.push(`unknown cake: status ${missing.status()}, want 404`);
	if (!api || api.status() !== 200) problems.push('shop did not load');
	await browser.close();
	console.log(JSON.stringify({ base: qa.BASE, pages: rows.length, problems, errors }, null, 1));
	process.exit(problems.length || errors.length ? 1 : 0);
})();
