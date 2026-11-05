// Display formatting for checkout and orders (GK-025). Every time is shown
// in London time (Europe/London, including the clock changes); money is the
// server's integer pence, only formatted here.

const ZONE = 'Europe/London';

/** £90 for whole pounds, £40.50 otherwise (GK-017 r01). */
export function formatMoney(minor: number, currency = 'GBP'): string {
	if (!Number.isSafeInteger(minor)) throw new Error('money must be whole minor units');
	return new Intl.NumberFormat('en-GB', {
		style: 'currency',
		currency,
		minimumFractionDigits: minor % 100 === 0 ? 0 : 2,
		maximumFractionDigits: 2
	}).format(minor / 100);
}

function parts(instant: Date, options: Intl.DateTimeFormatOptions): Record<string, string> {
	const out: Record<string, string> = {};
	for (const p of new Intl.DateTimeFormat('en-GB', { timeZone: ZONE, ...options }).formatToParts(
		instant
	)) {
		out[p.type] = p.value;
	}
	return out;
}

function instant(value: string | Date): Date {
	const d = value instanceof Date ? value : new Date(value);
	if (Number.isNaN(d.getTime())) throw new Error(`not a time: ${String(value)}`);
	return d;
}

/** 24-hour London clock time, e.g. 09:00. */
export function londonTime(value: string | Date): string {
	const p = parts(instant(value), { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
	return `${p.hour}:${p.minute}`;
}

/** A slot as 11:00–12:00 (London time). */
export function timeRange(startsAt: string, endsAt: string): string {
	return `${londonTime(startsAt)}–${londonTime(endsAt)}`;
}

const DATE = /^(\d{4})-(\d{2})-(\d{2})$/;

/** A calendar date (YYYY-MM-DD, a London day) as Thu 22 October 2026. */
export function dateLabel(date: string): string {
	const m = DATE.exec(date);
	if (!m) throw new Error(`not a date: ${date}`);
	// Midday UTC is the same calendar day in London all year.
	const p = parts(new Date(`${date}T12:00:00Z`), {
		weekday: 'short',
		day: 'numeric',
		month: 'long',
		year: 'numeric'
	});
	return `${p.weekday} ${p.day} ${p.month} ${p.year}`;
}

/** Today's date in London (YYYY-MM-DD). */
export function londonToday(now: Date = new Date()): string {
	const p = parts(now, { year: 'numeric', month: '2-digit', day: '2-digit' });
	return `${p.year}-${p.month}-${p.day}`;
}

/** date plus n calendar days (YYYY-MM-DD). */
export function addDays(date: string, n: number): string {
	if (!DATE.test(date)) throw new Error(`not a date: ${date}`);
	const d = new Date(`${date}T00:00:00Z`);
	d.setUTCDate(d.getUTCDate() + n);
	return d.toISOString().slice(0, 10);
}

/** K05: a full refund needs cancelling more than 120 elapsed hours before the handover. */
export const FULL_REFUND_HOURS = 120;

/** The full-refund deadline for a handover instant: the start minus 120 elapsed hours. */
export function refundDeadline(startsAt: string): Date {
	return new Date(instant(startsAt).getTime() - FULL_REFUND_HOURS * 3600 * 1000);
}

/** A deadline as "11:00 (London time) on Sat 17 October" (GK-017 r01). */
export function deadlineLabel(value: string | Date): string {
	const d = instant(value);
	const p = parts(d, { weekday: 'short', day: 'numeric', month: 'long' });
	return `${londonTime(d)} (London time) on ${p.weekday} ${p.day} ${p.month}`;
}

/** A moment as "Sat 17 October 2026, 11:00 (London time)". */
export function momentLabel(value: string | Date): string {
	const d = instant(value);
	const p = parts(d, { weekday: 'short', day: 'numeric', month: 'long', year: 'numeric' });
	return `${p.weekday} ${p.day} ${p.month} ${p.year}, ${londonTime(d)} (London time)`;
}

// The catalog's GK-002 sizes (api/internal/catalog Sizes).
const SIZE_LABELS: Record<string, string> = {
	mini: 'Mini · 5 inches',
	s: 'S · 6 inches',
	m: 'M · 7 inches',
	l: 'L · 8 inches',
	xl: 'XL · 10 inches',
	xxl: 'XXL · 12 inches',
	tiers_4_7: '2 tiers · 4 + 7 inches',
	tiers_6_8: '2 tiers · 6 + 8 inches',
	tiers_8_10: '2 tiers · 8 + 10 inches',
	tiers_4_6_8: '3 tiers · 4 + 6 + 8 inches',
	tiers_6_9_11: '3 tiers · 6 + 9 + 11 inches'
};

/** A size code as shown to customers (Pink piping: "S · 6 inches"). */
export function sizeLabel(code: string): string {
	return SIZE_LABELS[code] ?? code;
}

/** How the customer gets the cake. */
export function modeLabel(mode: string): string {
	switch (mode) {
		case 'collection':
			return 'Collect · St Katharine Docks';
		case 'customer_courier':
			return 'Your courier collects';
		case 'gk_delivery':
			return 'GK delivery';
		default:
			return mode;
	}
}
