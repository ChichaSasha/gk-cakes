import { describe, expect, it } from 'vitest';
import {
	addDays,
	dateLabel,
	deadlineLabel,
	formatMoney,
	londonDayLabel,
	londonTime,
	londonToday,
	refundDeadline,
	slotLabel
} from './format';

describe('formatMoney', () => {
	it('shows whole pounds without pence and fractional amounts with two places (K03)', () => {
		expect(formatMoney(10000)).toBe('£100');
		expect(formatMoney(8550)).toBe('£85.50');
		expect(formatMoney(8101)).toBe('£81.01');
		expect(formatMoney(5)).toBe('£0.05');
		expect(formatMoney(0)).toBe('£0');
		expect(formatMoney(123456700)).toBe('£1,234,567');
		expect(formatMoney(-900)).toBe('−£9');
	});

	it('refuses anything but whole pence', () => {
		expect(() => formatMoney(1.5)).toThrow();
		expect(() => formatMoney(Number.NaN)).toThrow();
	});
});

describe('London time', () => {
	it('shows instants in London time in summer and winter', () => {
		// 22 Oct 2026 is BST (UTC+1); 27 Oct is GMT.
		expect(londonTime('2026-10-22T10:00:00Z')).toBe('11:00');
		expect(londonTime('2026-10-27T11:00:00Z')).toBe('11:00');
		expect(slotLabel('2026-10-22T10:00:00Z', '2026-10-22T11:00:00Z')).toBe('11:00–12:00');
		expect(londonDayLabel('2026-10-17T10:00:00Z')).toBe('Sat 17 October');
		expect(dateLabel('2026-10-22')).toBe('Thu 22 October 2026');
	});

	it('knows today in London, not in UTC', () => {
		// 23:30 UTC on 21 June is 00:30 on 22 June in London (BST).
		expect(londonToday(Date.parse('2026-06-21T23:30:00Z'))).toBe('2026-06-22');
		expect(londonToday(Date.parse('2026-01-05T23:30:00Z'))).toBe('2026-01-05');
		expect(addDays('2026-10-31', 1)).toBe('2026-11-01');
		expect(addDays('2026-03-28', 30)).toBe('2026-04-27');
	});
});

describe('refund deadline', () => {
	it('is 120 elapsed hours before the start, as in the sketch', () => {
		// GK-017 r01: a 22 Oct 11:00 handover → before 11:00 on Sat 17 October.
		const d = refundDeadline('2026-10-22T10:00:00Z');
		expect(d.toISOString()).toBe('2026-10-17T10:00:00.000Z');
		expect(deadlineLabel(d)).toBe('11:00 (London time) on Sat 17 October');
	});

	it('counts elapsed hours across the October clock change', () => {
		// Clocks go back on Sun 25 Oct 2026. A Tue 27 Oct 11:00 GMT handover
		// minus 120 h is 11:00 UTC on 22 Oct, which is 12:00 BST.
		const d = refundDeadline('2026-10-27T11:00:00Z');
		expect(d.toISOString()).toBe('2026-10-22T11:00:00.000Z');
		expect(deadlineLabel(d)).toBe('12:00 (London time) on Thu 22 October');
	});

	it('counts elapsed hours across the March clock change', () => {
		// Clocks go forward on Sun 29 Mar 2026: a Tue 31 Mar 11:00 BST start
		// (10:00 UTC) minus 120 h is 10:00 UTC on 26 Mar, 10:00 GMT.
		expect(deadlineLabel(refundDeadline('2026-03-31T10:00:00Z'))).toBe(
			'10:00 (London time) on Thu 26 March'
		);
	});
});
