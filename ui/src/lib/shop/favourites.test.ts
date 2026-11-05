import { describe, expect, it } from 'vitest';
import {
	anniversary,
	emptyFavourites,
	expiryAfter,
	FAVOURITES_KEY,
	focusAfterRemoval,
	londonEndOfDay,
	MAX_FAVOURITES,
	parseFavourites,
	readFavourites,
	savedListings,
	storeToggle,
	toggled
} from './favourites';
import { ID } from './test-fixtures';

const at = (iso: string) => Date.parse(iso);
const A = ID.cake;
const B = ID.made;

describe('favourites expiry (K09: one London calendar year after the last change)', () => {
	it('ends at the last instant of the same London date next year', () => {
		// 5 Nov 2026 (GMT) -> 5 Nov 2027 23:59:59.999 London = UTC (GMT).
		expect(expiryAfter(at('2026-11-05T10:00:00Z'))).toBe('2027-11-05T23:59:59.999Z');
		// 1 Jul 2026 (BST) -> 1 Jul 2027 23:59:59.999 London = 22:59:59.999 UTC.
		expect(expiryAfter(at('2026-07-01T12:00:00Z'))).toBe('2027-07-01T22:59:59.999Z');
		// 23:30 UTC on 30 Jun is already 1 Jul in London.
		expect(expiryAfter(at('2026-06-30T23:30:00Z'))).toBe('2027-07-01T22:59:59.999Z');
	});

	it('clamps 29 February to 28 February', () => {
		expect(anniversary('2028-02-29')).toBe('2029-02-28');
		expect(anniversary('2026-01-31')).toBe('2027-01-31');
		expect(londonEndOfDay('2027-03-28')).toBe(at('2027-03-28T22:59:59.999Z')); // BST began that day
		expect(londonEndOfDay('2027-10-31')).toBe(at('2027-10-31T23:59:59.999Z')); // GMT again
	});
});

describe('parseFavourites', () => {
	const now = at('2026-11-05T12:00:00Z');
	const stored = (v: unknown) => JSON.stringify(v);

	it('keeps valid, unexpired cake IDs once each, in order', () => {
		expect(
			parseFavourites(stored({ ids: [B, A, B, 'x', 7], expires: '2027-11-05T23:59:59.999Z' }), now)
		).toEqual({ ids: [B, A], expires: '2027-11-05T23:59:59.999Z' });
	});

	it('treats missing, damaged or expired entries as nothing saved', () => {
		for (const raw of [
			null,
			'',
			'{',
			'null',
			stored([A]),
			stored({ ids: [A] }),
			stored({ ids: A, expires: '2027-01-01T00:00:00Z' }),
			stored({ ids: [A], expires: 'soon' }),
			stored({ ids: [A], expires: '2026-11-05T12:00:00Z' }),
			stored({ ids: ['not-an-id'], expires: '2027-01-01T00:00:00Z' })
		]) {
			expect(parseFavourites(raw, now)).toEqual(emptyFavourites());
		}
	});

	it('keeps at most MAX_FAVOURITES', () => {
		const ids = Array.from(
			{ length: MAX_FAVOURITES + 5 },
			(_, i) => `00000000-0000-4000-8000-${String(i).padStart(12, '0')}`
		);
		expect(parseFavourites(stored({ ids, expires: '2027-01-01T00:00:00Z' }), now).ids).toHaveLength(
			MAX_FAVOURITES
		);
	});
});

describe('toggled', () => {
	const now = at('2026-11-05T12:00:00Z');

	it('saves and removes a cake, renewing the year on each change', () => {
		const one = toggled(emptyFavourites(), A, now);
		expect(one).toEqual({ ids: [A], expires: '2027-11-05T23:59:59.999Z' });
		const two = toggled(one, B, at('2026-12-01T12:00:00Z'));
		expect(two).toEqual({ ids: [A, B], expires: '2027-12-01T23:59:59.999Z' });
		expect(toggled(two, A, now)).toEqual({ ids: [B], expires: '2027-11-05T23:59:59.999Z' });
	});

	it('forgets the expiry when the last favourite is removed, and ignores non-IDs', () => {
		const one = toggled(emptyFavourites(), A, now);
		expect(toggled(one, A, now)).toEqual(emptyFavourites());
		expect(toggled(one, 'nope', now)).toBe(one);
	});
});

/** An in-memory localStorage; `fail` makes every call throw (storage blocked). */
function fakeStorage(initial: Record<string, string> = {}, fail = false) {
	const data = { ...initial };
	const guard = () => {
		if (fail) throw new Error('SecurityError');
	};
	return {
		data,
		getItem: (k: string) => (guard(), k in data ? data[k] : null),
		setItem: (k: string, v: string) => (guard(), void (data[k] = v)),
		removeItem: (k: string) => (guard(), void delete data[k])
	};
}

describe('device storage', () => {
	const now = at('2026-11-05T12:00:00Z');

	it('removes an expired or damaged entry when reading, and never extends one', () => {
		const expired = fakeStorage({
			[FAVOURITES_KEY]: JSON.stringify({ ids: [A], expires: '2026-11-05T11:59:59Z' })
		});
		expect(readFavourites(expired, now)).toEqual(emptyFavourites());
		expect(expired.data).toEqual({});
		const damaged = fakeStorage({ [FAVOURITES_KEY]: '{' });
		expect(readFavourites(damaged, now)).toEqual(emptyFavourites());
		expect(damaged.data).toEqual({});
		const live = { ids: [A], expires: '2027-01-01T00:00:00.000Z' };
		const kept = fakeStorage({ [FAVOURITES_KEY]: JSON.stringify(live) });
		expect(readFavourites(kept, now)).toEqual(live);
		expect(kept.data[FAVOURITES_KEY]).toBe(JSON.stringify(live));
	});

	it('caps an expiry beyond a year from now (damaged or edited entry)', () => {
		const s = fakeStorage({
			[FAVOURITES_KEY]: JSON.stringify({ ids: [A], expires: '9999-01-01T00:00:00Z' })
		});
		expect(readFavourites(s, now)?.expires).toBe('2027-11-05T23:59:59.999Z');
		// Stored capped, so it does not slide forward on later days.
		expect(JSON.parse(s.data[FAVOURITES_KEY]).expires).toBe('2027-11-05T23:59:59.999Z');
		expect(readFavourites(s, at('2027-11-06T00:00:00Z'))).toEqual(emptyFavourites());
	});

	it('starts each change from what is stored now, so expired favourites never come back', () => {
		// A page loaded while A was saved; A then expired before B was pressed.
		const s = fakeStorage({
			[FAVOURITES_KEY]: JSON.stringify({ ids: [A], expires: '2026-11-05T12:00:04Z' })
		});
		const later = at('2026-11-05T12:00:06Z');
		expect(storeToggle(s, B, later)).toEqual({ ids: [B], expires: '2027-11-05T23:59:59.999Z' });
		expect(JSON.parse(s.data[FAVOURITES_KEY]).ids).toEqual([B]);
	});

	it('removes the entry with the last favourite and follows other tabs', () => {
		const s = fakeStorage();
		storeToggle(s, A, now);
		// Another tab removed A and saved B meanwhile.
		s.data[FAVOURITES_KEY] = JSON.stringify({ ids: [B], expires: '2027-11-05T23:59:59.999Z' });
		expect(storeToggle(s, A, now)?.ids).toEqual([B, A]);
		storeToggle(s, A, now);
		storeToggle(s, B, now);
		expect(s.data).toEqual({});
	});

	it('reports a browser that blocks storage instead of pretending to save', () => {
		const blocked = fakeStorage({}, true);
		expect(readFavourites(blocked, now)).toBeNull();
		expect(storeToggle(blocked, A, now)).toBeNull();
	});
});

describe('favourites page helpers', () => {
	it('lists saved cakes still in the collection, in saved order', () => {
		const all = [{ id: A }, { id: B }];
		expect(savedListings([B, 'gone', A], all)).toEqual([{ id: B }, { id: A }]);
		expect(savedListings(['gone'], all)).toEqual([]);
	});

	it('moves focus to the next heart, else the new last one, else nowhere', () => {
		expect(focusAfterRemoval(0, 3)).toBe(0);
		expect(focusAfterRemoval(1, 3)).toBe(1);
		expect(focusAfterRemoval(2, 3)).toBe(1);
		expect(focusAfterRemoval(0, 1)).toBeNull();
		expect(focusAfterRemoval(-1, 2)).toBe(0);
	});
});
