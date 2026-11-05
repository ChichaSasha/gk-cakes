import { describe, expect, it } from 'vitest';
import {
	checkoutHref,
	currentDate,
	isRealDate,
	earliestCollectBy,
	emptyBag,
	MAX_BAG,
	parseBag,
	placesNeeded,
	withCake,
	withoutCake,
	withWhen
} from './bag';
import { ID } from './test-fixtures';

const slot = { id: 's-1', starts_at: '2026-11-13T15:00:00Z', ends_at: '2026-11-13T16:00:00Z' };

describe('parseBag', () => {
	it('starts empty for nothing or anything malformed', () => {
		expect(parseBag(null)).toEqual(emptyBag());
		expect(parseBag('not json')).toEqual(emptyBag());
		expect(parseBag('[1,2]')).toEqual(emptyBag());
		expect(parseBag('null')).toEqual(emptyBag());
	});

	it('keeps only well-formed, unique catalogue keys (at most 20) and a valid date, mode and time', () => {
		const keys = [
			`v:${ID.made}`,
			`v:${ID.made}`,
			`lot:${ID.lot}`,
			'v:not-a-uuid',
			'x:' + ID.made,
			7
		];
		const bag = parseBag(
			JSON.stringify({ keys, date: '2026-11-13', mode: 'customer_courier', slot, price: 1 })
		);
		expect(bag).toEqual({
			keys: [`v:${ID.made}`, `lot:${ID.lot}`],
			date: '2026-11-13',
			mode: 'customer_courier',
			slot
		});
		const many = Array.from(
			{ length: 25 },
			(_, i) => `v:${ID.made.slice(0, -2)}${String(i).padStart(2, '0')}`
		);
		expect(parseBag(JSON.stringify({ keys: many })).keys).toHaveLength(MAX_BAG);
	});

	it('drops an unknown mode (GK delivery is not offered online), a bad date, or a time without a date', () => {
		expect(parseBag(JSON.stringify({ keys: [], mode: 'gk_delivery' })).mode).toBe('collection');
		expect(parseBag(JSON.stringify({ keys: [], date: '13/11/2026', slot }))).toEqual(emptyBag());
		expect(parseBag(JSON.stringify({ keys: [], slot }))).toEqual(emptyBag());
		expect(parseBag(JSON.stringify({ keys: [], date: '2026-11-13', slot: { id: 's' } }))).toEqual({
			keys: [],
			mode: 'collection',
			date: '2026-11-13'
		});
	});
});

describe('bag changes', () => {
	it('adds each cake once, up to the order limit, and removes it', () => {
		let bag = withCake(emptyBag(), `v:${ID.made}`);
		bag = withCake(bag, `v:${ID.made}`);
		bag = withCake(bag, 'v:bogus');
		expect(bag.keys).toEqual([`v:${ID.made}`]);
		expect(withoutCake(bag, `v:${ID.made}`).keys).toEqual([]);
		const full = { ...emptyBag(), keys: Array.from({ length: MAX_BAG }, () => `lot:${ID.lot}`) };
		expect(withCake(full, `v:${ID.made}`)).toBe(full);
	});

	it('keeps the chosen date, handover and time together', () => {
		expect(withWhen(emptyBag(), '2026-11-13', 'customer_courier', slot)).toEqual({
			keys: [],
			date: '2026-11-13',
			mode: 'customer_courier',
			slot
		});
	});

	it('counts day places for made-to-order cakes only (K04)', () => {
		expect(placesNeeded([`v:${ID.made}`, `lot:${ID.lot}`, `v:${ID.madeM}`])).toBe(2);
	});
});

describe('checkoutHref', () => {
	const today = '2026-11-05';
	it('hands checkout only catalogue IDs and the date (ui/README Basket)', () => {
		const bag = withWhen(
			{ ...emptyBag(), keys: [`v:${ID.made}`, `lot:${ID.lot}`, `v:${ID.madeM}`] },
			'2026-11-13',
			'collection',
			slot
		);
		expect(checkoutHref(bag, today)).toBe(
			`/checkout?v=${ID.made}&lot=${ID.lot}&v=${ID.madeM}&date=2026-11-13`
		);
		expect(checkoutHref({ ...emptyBag(), keys: [`v:${ID.made}`] }, today)).toBe(
			`/checkout?v=${ID.made}`
		);
	});

	it('never hands over a date that has passed', () => {
		const bag = withWhen(
			{ ...emptyBag(), keys: [`v:${ID.made}`] },
			'2026-10-01',
			'collection',
			slot
		);
		expect(checkoutHref(bag, today)).toBe(`/checkout?v=${ID.made}`);
		expect(currentDate(bag, today)).toBeUndefined();
		expect(currentDate(bag, '2026-10-01')).toBe('2026-10-01');
	});
});

describe('isRealDate', () => {
	it('accepts real calendar days only', () => {
		expect(isRealDate('2026-11-13')).toBe(true);
		expect(isRealDate('2028-02-29')).toBe(true);
		for (const bad of ['2099-13-01', '2026-02-30', '2026-11-31', '2026-1-01', 20261113, null]) {
			expect(isRealDate(bad)).toBe(false);
		}
	});

	it('drops an impossible stored date (it would break date labels)', () => {
		expect(parseBag(JSON.stringify({ keys: [], date: '2099-13-01', slot })).date).toBeUndefined();
	});
});

it('earliestCollectBy picks the soonest collect-by of the ready-made cakes in the bag', () => {
	const ready = [
		{ id: ID.lot, expires_at: '2026-11-20T12:00:00Z' },
		{ id: ID.lot2, expires_at: '2026-11-15T12:00:00Z' }
	];
	expect(earliestCollectBy([`lot:${ID.lot}`, `lot:${ID.lot2}`, `v:${ID.made}`], ready)).toBe(
		'2026-11-15T12:00:00Z'
	);
	expect(earliestCollectBy([`v:${ID.made}`], ready)).toBeUndefined();
});
