import { describe, expect, it } from 'vitest';
import type { Day, Slot } from '../checkout/types';
import {
	addMonths,
	dayView,
	londonDate,
	monthGrid,
	monthLabel,
	monthRange,
	nextOpenDates,
	shortDate,
	timeChoices
} from './picker';

const day = (date: string, status: Day['status'], remaining: number | null = 7): Day => ({
	date,
	status,
	remaining_places: remaining
});

describe('month grid', () => {
	it('starts weeks on Monday and lists every date of the month', () => {
		const g = monthGrid('2026-11'); // 1 Nov 2026 is a Sunday
		expect(g.offset).toBe(6);
		expect(g.dates).toHaveLength(30);
		expect(g.dates[0]).toBe('2026-11-01');
		expect(monthGrid('2026-02').dates).toHaveLength(28);
		expect(monthGrid('2027-02').offset).toBe(0); // 1 Feb 2027 is a Monday
	});

	it('moves across years and asks availability from today (never the past)', () => {
		expect(addMonths('2026-12', 1)).toBe('2027-01');
		expect(addMonths('2027-01', -1)).toBe('2026-12');
		expect(monthRange('2026-11', '2026-11-05')).toEqual({ from: '2026-11-05', to: '2026-11-30' });
		expect(monthRange('2026-12', '2026-11-05')).toEqual({ from: '2026-12-01', to: '2026-12-31' });
		expect(monthRange('2026-10', '2026-11-05')).toBeNull();
	});

	it('words dates in London terms', () => {
		expect(shortDate('2026-11-13')).toBe('Fri 13 Nov');
		expect(monthLabel('2026-11')).toBe('November 2026');
		// 00:30 on 1 July in London is still 30 June in UTC.
		expect(londonDate('2026-06-30T23:30:00Z')).toBe('2026-07-01');
		expect(londonDate('2026-11-19T03:56:56Z')).toBe('2026-11-19');
	});
});

describe('dayView', () => {
	const today = '2026-11-05';
	const v = (d: Day | undefined, places = 1, collectBy?: string, date = '2026-11-13') =>
		dayView(date, today, d, { places, collectBy });

	it('labels each server status and disables what cannot be chosen', () => {
		expect(v(day('2026-11-13', 'closed'))).toMatchObject({
			label: 'Closed',
			enabled: false,
			aria: ' · closed'
		});
		expect(v(day('2026-11-13', 'lead_time'))).toMatchObject({ label: 'Too soon', enabled: false });
		expect(v(day('2026-11-13', 'outside_horizon'))).toMatchObject({
			label: 'Not open',
			enabled: false
		});
		expect(v(day('2026-11-13', 'full', 0))).toMatchObject({ label: 'Full', enabled: false });
		expect(v(day('2026-11-13', 'no_times'))).toMatchObject({ label: 'No times', enabled: false });
		expect(v(undefined)).toMatchObject({ status: 'unknown', enabled: false });
		expect(dayView('2026-11-01', today, day('2026-11-01', 'open'), { places: 1 })).toMatchObject({
			status: 'past',
			label: '',
			enabled: false
		});
	});

	it('shows places left for made-to-order cakes, "Open" when none are needed', () => {
		expect(v(day('2026-11-13', 'open', 1))).toMatchObject({
			label: '1 left',
			enabled: true,
			aria: ' · 1 place left'
		});
		expect(v(day('2026-11-13', 'open', 6))).toMatchObject({
			label: '6 left',
			aria: ' · 6 places left'
		});
		expect(v(day('2026-11-13', 'open', 6), 0)).toMatchObject({ label: 'Open', enabled: true });
		expect(v(day('2026-11-13', 'open', null))).toMatchObject({ label: 'Open', enabled: true });
	});

	it('marks days after a ready-made cake’s collect-by date "Too late"', () => {
		expect(v(day('2026-11-16', 'open'), 0, '2026-11-15', '2026-11-16')).toMatchObject({
			status: 'too_late',
			label: 'Too late',
			enabled: false
		});
		expect(v(day('2026-11-15', 'open'), 0, '2026-11-15', '2026-11-15').enabled).toBe(true);
	});

	it('offers the next open days after a date', () => {
		const days = ['2026-11-13', '2026-11-14', '2026-11-19', '2026-11-20'].map((d, i) =>
			dayView(d, today, day(d, i === 1 ? 'full' : 'open'), { places: 1 })
		);
		expect(nextOpenDates('2026-11-13', days)).toEqual(['2026-11-19', '2026-11-20']);
	});
});

describe('timeChoices', () => {
	const s = (
		id: string,
		availability: Slot['availability'],
		reason: string | null,
		ends = '2026-11-15T12:00:00Z'
	): Slot => ({
		id,
		starts_at: '2026-11-15T11:00:00Z',
		ends_at: ends,
		availability,
		reason_code: reason
	});

	it('shows available and requestable times, counting those hidden by the 192-hour notice', () => {
		const tc = timeChoices([
			s('a', 'unavailable', 'lead_time'),
			s('b', 'available', null),
			s('c', 'requestable', null),
			s('d', 'unavailable', 'full')
		]);
		expect(tc.slots.map((x) => x.id)).toEqual(['b', 'c']);
		expect(tc.hiddenLead).toBe(1);
		expect(tc.hiddenLate).toBe(0);
	});

	it('hides times ending after a ready-made cake’s collect-by time (equality allowed)', () => {
		const tc = timeChoices(
			[
				s('a', 'available', null, '2026-11-15T12:00:00Z'),
				s('b', 'available', null, '2026-11-15T12:30:00Z')
			],
			'2026-11-15T12:00:00Z'
		);
		expect(tc.slots.map((x) => x.id)).toEqual(['a']);
		expect(tc.hiddenLate).toBe(1);
	});
});
