import { describe, expect, it } from 'vitest';
import {
	addDays,
	dateLabel,
	deadlineLabel,
	formatMoney,
	londonTime,
	londonToday,
	modeLabel,
	momentLabel,
	refundDeadline,
	sizeLabel,
	timeRange
} from './format';

describe('formatMoney', () => {
	it('shows whole pounds without pence and other amounts with two places', () => {
		expect(formatMoney(9000)).toBe('£90');
		expect(formatMoney(8100)).toBe('£81');
		expect(formatMoney(4050)).toBe('£40.50');
		expect(formatMoney(1)).toBe('£0.01');
		expect(formatMoney(0)).toBe('£0');
		expect(formatMoney(123456)).toBe('£1,234.56');
	});

	it('refuses anything but whole minor units', () => {
		expect(() => formatMoney(10.5)).toThrow();
		expect(() => formatMoney(Number.NaN)).toThrow();
	});
});

describe('London times', () => {
	it('shows GMT and BST instants in London clock time', () => {
		expect(londonTime('2026-01-15T11:00:00Z')).toBe('11:00'); // GMT
		expect(londonTime('2026-07-15T10:00:00Z')).toBe('11:00'); // BST
		expect(timeRange('2026-10-22T10:00:00Z', '2026-10-22T11:00:00Z')).toBe('11:00–12:00');
	});

	it('labels calendar dates as London days', () => {
		expect(dateLabel('2026-10-22')).toBe('Thu 22 October 2026');
		expect(dateLabel('2026-03-29')).toBe('Sun 29 March 2026'); // clocks go forward
		expect(() => dateLabel('22/10/2026')).toThrow();
	});

	it('knows the London date around midnight', () => {
		expect(londonToday(new Date('2026-07-15T23:30:00Z'))).toBe('2026-07-16'); // 00:30 BST
		expect(londonToday(new Date('2026-01-15T23:30:00Z'))).toBe('2026-01-15'); // 23:30 GMT
		expect(addDays('2026-12-31', 1)).toBe('2027-01-01');
		expect(addDays('2026-03-01', 30)).toBe('2026-03-31');
	});
});

describe('refund deadline (K05: handover minus 120 elapsed hours)', () => {
	it('matches the r01 example: 22 Oct 11:00 → before 11:00 on Sat 17 October', () => {
		const handover = '2026-10-22T10:00:00Z'; // 11:00 BST
		expect(refundDeadline(handover).toISOString()).toBe('2026-10-17T10:00:00.000Z');
		expect(deadlineLabel(refundDeadline(handover))).toBe('11:00 (London time) on Sat 17 October');
	});

	it('counts elapsed hours across the clock change, so the local time moves', () => {
		// 30 Mar 2026 11:00 BST; 120 h earlier is 25 Mar 10:00 GMT.
		const spring = refundDeadline('2026-03-30T10:00:00Z');
		expect(deadlineLabel(spring)).toBe('10:00 (London time) on Wed 25 March');
		// 27 Oct 2026 11:00 GMT; 120 h earlier is 22 Oct 12:00 BST.
		const autumn = refundDeadline('2026-10-27T11:00:00Z');
		expect(deadlineLabel(autumn)).toBe('12:00 (London time) on Thu 22 October');
	});

	it('labels a moment with its year', () => {
		expect(momentLabel('2026-10-17T10:00:00Z')).toBe('Sat 17 October 2026, 11:00 (London time)');
		expect(() => momentLabel('nonsense')).toThrow();
	});
});

describe('labels', () => {
	it('names sizes and handover options', () => {
		expect(sizeLabel('s')).toBe('S · 6 inches');
		expect(sizeLabel('tiers_4_6_8')).toBe('3 tiers · 4 + 6 + 8 inches');
		expect(sizeLabel('new_size')).toBe('new_size');
		expect(modeLabel('collection')).toBe('Collect · St Katharine Docks');
		expect(modeLabel('customer_courier')).toBe('Your courier collects');
	});
});
