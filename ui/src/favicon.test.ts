import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

// The site icon is served from static/ (so /favicon.ico is not a 404) and
// linked from every page; it is plain SVG/ICO from this site (CSP img-src
// 'self') in the visual foundation's ink, white and accent.
const read = (path: string) => readFileSync(new URL(path, import.meta.url));

describe('favicon', () => {
	it('is linked from the page template', () => {
		const html = read('./app.html').toString();
		expect(html).toContain(
			'<link rel="icon" href="%sveltekit.assets%/favicon.ico" sizes="32x32" />'
		);
		expect(html).toContain(
			'<link rel="icon" href="%sveltekit.assets%/favicon.svg" type="image/svg+xml" />'
		);
	});

	it('is a 32x32 ICO at /favicon.ico', () => {
		const ico = read('../static/favicon.ico');
		// ICONDIR: reserved 0, type 1 (icon), one image of 32x32 at 32 bits.
		expect([ico.readUInt16LE(0), ico.readUInt16LE(2), ico.readUInt16LE(4)]).toEqual([0, 1, 1]);
		expect([ico[6], ico[7], ico.readUInt16LE(12)]).toEqual([32, 32, 32]);
		expect(ico.readUInt32LE(14) + ico.readUInt32LE(18)).toBe(ico.length);
	});

	it('is a self-contained SVG in the foundation colours', () => {
		const svg = read('../static/favicon.svg').toString();
		expect(svg.startsWith('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">')).toBe(
			true
		);
		expect(svg).not.toMatch(/<script|href=|url\(|style=|on[a-z]+=/i);
		const colours = new Set(svg.match(/#[0-9a-f]{6}/gi));
		expect([...colours].sort()).toEqual(['#141416', '#5b1a2e', '#b1b1b1', '#f3f4f6', '#ffffff']);
	});
});
