// Money and London-time formatting for checkout and orders (GK-030).
// Contracts K03: money is GBP pence, shown whole as £100 and fractional as
// £85.50. All dates and times are Europe/London, whatever the browser's zone.
// Labels are assembled from Intl parts so they do not depend on the ICU
// version's punctuation (some insert a comma after the weekday).

export const LONDON = 'Europe/London';

const HOUR_MS = 3_600_000;
const DAY_MS = 24 * HOUR_MS;

/** The full-refund deadline is the appointment start minus 120 elapsed hours (K05). */
export const FULL_REFUND_HOURS = 120;

/** Formats GBP pence: £100 when whole, £85.50 otherwise; negative as −£9. */
export function formatMoney(minor: number): string {
	if (!Number.isSafeInteger(minor)) throw new Error(`not an amount in pence: ${minor}`);
	const abs = Math.abs(minor);
	const pounds = Math.floor(abs / 100);
	const pence = abs % 100;
	const whole = pounds.toLocaleString('en-GB');
	const text = pence === 0 ? `£${whole}` : `£${whole}.${String(pence).padStart(2, '0')}`;
	return minor < 0 ? `−${text}` : text;
}

interface LondonParts {
	year: string;
	month: string;
	monthName: string;
	day: string;
	weekday: string;
	hour: string;
	minute: string;
}

const partsFormat = new Intl.DateTimeFormat('en-GB', {
	timeZone: LONDON,
	year: 'numeric',
	month: '2-digit',
	day: 'numeric',
	weekday: 'short',
	hour: '2-digit',
	minute: '2-digit',
	hourCycle: 'h23'
});
const monthFormat = new Intl.DateTimeFormat('en-GB', { timeZone: LONDON, month: 'long' });

function toDate(instant: string | number | Date): Date {
	const d = instant instanceof Date ? instant : new Date(instant);
	if (Number.isNaN(d.getTime())) throw new Error(`not an instant: ${String(instant)}`);
	return d;
}

/** The London wall-clock parts of an instant. */
export function londonParts(instant: string | number | Date): LondonParts {
	const d = toDate(instant);
	const p = Object.fromEntries(partsFormat.formatToParts(d).map((x) => [x.type, x.value]));
	return {
		year: p.year,
		month: p.month,
		monthName: monthFormat.format(d),
		day: String(Number(p.day)),
		weekday: p.weekday,
		hour: p.hour,
		minute: p.minute
	};
}

/** "11:00" in London time. */
export function londonTime(instant: string | number | Date): string {
	const p = londonParts(instant);
	return `${p.hour}:${p.minute}`;
}

/** "11:00–12:00" for an appointment, in London time. */
export function slotLabel(startsAt: string, endsAt: string): string {
	return `${londonTime(startsAt)}–${londonTime(endsAt)}`;
}

/** "Sat 17 October" (no year) for an instant, in London. */
export function londonDayLabel(instant: string | number | Date): string {
	const p = londonParts(instant);
	return `${p.weekday} ${p.day} ${p.monthName}`;
}

/**
 * "Thu 22 October 2026" for a calendar date (YYYY-MM-DD). The date is a
 * London calendar day, so noon UTC keeps it on the same day in any season.
 */
export function dateLabel(date: string): string {
	const p = londonParts(`${checkDate(date)}T12:00:00Z`);
	return `${p.weekday} ${p.day} ${p.monthName} ${p.year}`;
}

/** "Thu 22 Oct" for a calendar date (YYYY-MM-DD), for the day picker. */
export function shortDateLabel(date: string): string {
	const p = londonParts(`${checkDate(date)}T12:00:00Z`);
	return `${p.weekday} ${p.day} ${p.monthName.slice(0, 3)}`;
}

function checkDate(date: string): string {
	if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) throw new Error(`not a date: ${date}`);
	return date;
}

/** Today's calendar date in London (YYYY-MM-DD). */
export function londonToday(now: number = Date.now()): string {
	const p = londonParts(now);
	return `${p.year}-${p.month}-${p.day.padStart(2, '0')}`;
}

/** Adds whole days to a calendar date (YYYY-MM-DD). */
export function addDays(date: string, days: number): string {
	const t = Date.parse(`${date}T00:00:00Z`) + days * DAY_MS;
	return new Date(t).toISOString().slice(0, 10);
}

/** The full-refund deadline instant for an appointment start (start − 120 h, K05). */
export function refundDeadline(startsAt: string): Date {
	return new Date(toDate(startsAt).getTime() - FULL_REFUND_HOURS * HOUR_MS);
}

/** "11:00 (London time) on Sat 17 October" for a deadline instant. */
export function deadlineLabel(instant: string | number | Date): string {
	return `${londonTime(instant)} (London time) on ${londonDayLabel(instant)}`;
}
