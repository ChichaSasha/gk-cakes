// The shared date-and-time picker's logic (GK-028, approved GK-016 r01c
// sketch): a Monday-first month of days labelled from GET /availability,
// and the times from GET /schedule. Availability is the server's; this only
// words it and hides what can't be chosen.
import type { Day, Slot } from '../checkout/types';
import { addDays, londonToday } from '../checkout/format';

export type DayStatus = Day['status'] | 'past' | 'too_late' | 'unknown';

export interface DayView {
	date: string;
	/** Day of the month, 1-31. */
	day: number;
	status: DayStatus;
	label: string;
	enabled: boolean;
	/** Added to the date in the button's accessible name. */
	aria: string;
}

const MONTH = /^(\d{4})-(\d{2})$/;

/** The month (YYYY-MM) of a date. */
export function monthOf(date: string): string {
	return date.slice(0, 7);
}

/** n months after a month (YYYY-MM). */
export function addMonths(month: string, n: number): string {
	const m = MONTH.exec(month);
	if (!m) throw new Error(`not a month: ${month}`);
	const d = new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1 + n, 1));
	return d.toISOString().slice(0, 7);
}

/** Every date of a month (YYYY-MM-DD) and how many blank cells precede the 1st (Monday first). */
export function monthGrid(month: string): { offset: number; dates: string[] } {
	const m = MONTH.exec(month);
	if (!m) throw new Error(`not a month: ${month}`);
	const first = new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, 1));
	const count = new Date(Date.UTC(Number(m[1]), Number(m[2]), 0)).getUTCDate();
	const offset = (first.getUTCDay() + 6) % 7;
	return { offset, dates: Array.from({ length: count }, (_, i) => addDays(`${month}-01`, i)) };
}

/** The dates to ask /availability for to show a month: from today (at most 31). */
export function monthRange(month: string, today: string): { from: string; to: string } | null {
	const { dates } = monthGrid(month);
	const from = dates[0] < today ? today : dates[0];
	const to = dates[dates.length - 1];
	return from > to ? null : { from, to };
}

/** "Fri 13 Nov". */
export function shortDate(date: string): string {
	return new Intl.DateTimeFormat('en-GB', {
		timeZone: 'UTC',
		weekday: 'short',
		day: 'numeric',
		month: 'short'
	}).format(new Date(`${date}T12:00:00Z`));
}

/** "November 2026". */
export function monthLabel(month: string): string {
	return new Intl.DateTimeFormat('en-GB', {
		timeZone: 'UTC',
		month: 'long',
		year: 'numeric'
	}).format(new Date(`${month}-01T12:00:00Z`));
}

/** The London date of an instant, e.g. a ready-made cake's collect-by date. */
export function londonDate(instant: string): string {
	return londonToday(new Date(instant));
}

const LABELS: Partial<Record<DayStatus, string>> = {
	closed: 'Closed',
	lead_time: 'Too soon',
	outside_horizon: 'Not open',
	full: 'Full',
	no_times: 'No times',
	too_late: 'Too late'
};

const ARIA: Partial<Record<DayStatus, string>> = {
	closed: ' · closed',
	lead_time: ' · less than 192 hours away',
	outside_horizon: ' · not open for booking yet',
	full: ' · full',
	no_times: ' · no times left',
	too_late: ' · after the ready-made cake’s collect-by date'
};

/**
 * One calendar day. `places` is how many of the day's places the bag needs
 * (made-to-order cakes); ready-made cakes need none, so a ready-only bag
 * shows "Open". `collectBy` is the earliest collect-by date of the bag's
 * ready-made cakes: later days are "Too late".
 */
export function dayView(
	date: string,
	today: string,
	day: Day | undefined,
	opts: { places: number; collectBy?: string }
): DayView {
	const n = Number(date.slice(8));
	let status: DayStatus;
	if (date < today) status = 'past';
	else if (!day) status = 'unknown';
	else if (day.status === 'open' && opts.collectBy && date > opts.collectBy) status = 'too_late';
	else status = day.status;
	if (status !== 'open') {
		return {
			date,
			day: n,
			status,
			label: LABELS[status] ?? '',
			enabled: false,
			aria: ARIA[status] ?? ''
		};
	}
	const left = day?.remaining_places;
	if (opts.places > 0 && typeof left === 'number') {
		return {
			date,
			day: n,
			status,
			label: left === 1 ? '1 left' : `${left} left`,
			enabled: true,
			aria: ` · ${left === 1 ? '1 place' : `${left} places`} left`
		};
	}
	return { date, day: n, status, label: 'Open', enabled: true, aria: '' };
}

export interface TimeChoices {
	/** Times that can be chosen (available, or requestable for GK delivery). */
	slots: Slot[];
	/** Times hidden because they are less than 192 hours away. */
	hiddenLead: number;
	/** Times hidden because they end after a ready-made cake's collect-by time. */
	hiddenLate: number;
}

export function timeChoices(slots: Slot[], collectByInstant?: string): TimeChoices {
	const limit = collectByInstant ? Date.parse(collectByInstant) : Infinity;
	let hiddenLead = 0;
	let hiddenLate = 0;
	const shown: Slot[] = [];
	for (const s of slots) {
		if (s.availability === 'unavailable') {
			if (s.reason_code === 'lead_time') hiddenLead++;
			continue;
		}
		if (Date.parse(s.ends_at) > limit) {
			hiddenLate++;
			continue;
		}
		shown.push(s);
	}
	return { slots: shown, hiddenLead, hiddenLate };
}

/** The next choosable days after a date, from the days already loaded. */
export function nextOpenDates(after: string, days: DayView[], n = 2): string[] {
	return days
		.filter((d) => d.enabled && d.date > after)
		.map((d) => d.date)
		.sort()
		.slice(0, n);
}

/** The first choosable day in a list, if any. */
export function firstOpen(days: DayView[]): string | undefined {
	return days.find((d) => d.enabled)?.date;
}
