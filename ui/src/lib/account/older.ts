// "Add an older order" (approved GK-016 r02b sketch, states 38–42) as pure
// helpers: the form checked with the API's rules (api/internal/checkout/
// accounts.go ClaimInput.Validate and SubmitClaim, path c) in the sketch's
// words, the claim body, and the words for the API's refusals.
import { ApiError } from '../api';

export type OlderField = 'reference' | 'channel' | 'month' | 'year' | 'note';
export type OlderChannel = 'instagram' | 'offline';

/** What the form holds: the radio's channel, the selects' values ('' when unset). */
export interface OlderValues {
	reference: string;
	channel: OlderChannel;
	month: string;
	year: string;
	note: string;
}

export type OlderErrors = Partial<Record<OlderField, string>>;

/** Claim limits, as the API checks them. */
export const REFERENCE_MAX = 64;
export const NOTE_MAX = 500;

/**
 * "How did you order?": the sketch offers Instagram / Email / In person /
 * Other, but the API records only instagram, offline or website_email_link
 * (a website order, added from its order email), so a choice the API can't
 * keep is never offered: email, phone and in-person orders are all
 * "offline". Website orders are added from their order page, not here.
 */
export const CHANNELS: readonly { value: OlderChannel; label: string }[] = [
	{ value: 'instagram', label: 'Instagram' },
	{ value: 'offline', label: 'Email, phone or in person' }
];

/** "How you ordered" on the request page, for every channel the API has. */
export function channelLabel(channel: string): string {
	switch (channel) {
		case 'instagram':
			return 'Instagram';
		case 'offline':
			return 'Email, phone or in person';
		case 'website_email_link':
			return 'On this website';
		default:
			return 'Not shown';
	}
}

export const MONTHS = [
	'January',
	'February',
	'March',
	'April',
	'May',
	'June',
	'July',
	'August',
	'September',
	'October',
	'November',
	'December'
] as const;

/** The year choices: this year and the three before (the sketch's four years). */
export function yearOptions(now: Date = new Date()): string[] {
	const y = now.getFullYear();
	return [y, y - 1, y - 2, y - 3].map(String);
}

export const MESSAGES = {
	reference: 'Enter the order reference from the chef',
	referenceLong: `Order references can be up to ${REFERENCE_MAX} characters`,
	referenceControl: 'Order references can’t contain tabs or other special characters',
	note: 'Add a short note for the chef',
	noteLong: `Notes can be up to ${NOTE_MAX} characters`,
	noteControl: 'Notes can’t contain tabs or other special characters, only line breaks',
	month: 'Choose the month as well, or leave both empty',
	year: 'Choose the year as well, or leave both empty',
	date: 'Choose a month and year, or leave both empty',
	channel: 'Choose how you ordered'
} as const;

// Control characters and the Unicode line/paragraph separators (the API's rules);
// a note may contain line breaks.
// eslint-disable-next-line no-control-regex
const CONTROL = /[\u0000-\u001f\u007f-\u009f\u2028\u2029]/;
// eslint-disable-next-line no-control-regex
const NOTE_CONTROL = /[\u0000-\u0009\u000b-\u001f\u007f-\u009f\u2028\u2029]/;

/** A note as sent: Windows line endings made plain, then trimmed (the API refuses surrounding spaces). */
export function cleanNote(raw: string): string {
	return raw.replace(/\r\n?/g, '\n').trim();
}

/** Characters as the API counts them (Unicode code points). */
function length(s: string): number {
	return [...s].length;
}

/** The error for one field, or undefined when the API would accept it. */
export function fieldError(field: OlderField, values: OlderValues): string | undefined {
	switch (field) {
		case 'reference': {
			const v = values.reference.trim();
			if (v === '') return MESSAGES.reference;
			if (length(v) > REFERENCE_MAX) return MESSAGES.referenceLong;
			return CONTROL.test(v) ? MESSAGES.referenceControl : undefined;
		}
		case 'note': {
			const v = cleanNote(values.note);
			if (v === '') return MESSAGES.note;
			if (length(v) > NOTE_MAX) return MESSAGES.noteLong;
			return NOTE_CONTROL.test(v) ? MESSAGES.noteControl : undefined;
		}
		case 'channel':
			return CHANNELS.some((c) => c.value === values.channel) ? undefined : MESSAGES.channel;
		// A month or a year alone can't be sent (the API takes YYYY-MM).
		case 'month':
			return values.year && !values.month ? MESSAGES.month : undefined;
		case 'year':
			return values.month && !values.year ? MESSAGES.year : undefined;
	}
}

/** Form order, as the error summary lists them. */
export const FIELDS: readonly OlderField[] = ['reference', 'channel', 'month', 'year', 'note'];

/** Every field's error (empty when the form can be sent). */
export function validate(values: OlderValues): OlderErrors {
	const errors: OlderErrors = {};
	for (const f of FIELDS) {
		const e = fieldError(f, values);
		if (e) errors[f] = e;
	}
	return errors;
}

/** YYYY-MM from the selects (month 1–12, a four-digit year), or undefined. */
export function approximateDate(month: string, year: string): string | undefined {
	const m = Number(month);
	if (!/^\d{4}$/.test(year) || !Number.isInteger(m) || m < 1 || m > 12) return undefined;
	return `${year}-${String(m).padStart(2, '0')}`;
}

export interface OlderClaimBody {
	channel: OlderChannel;
	reference: string;
	approximate_date?: string;
	note: string;
}

/** The path c body (call after validate found nothing). */
export function claimBody(values: OlderValues): OlderClaimBody {
	const body: OlderClaimBody = {
		channel: values.channel,
		reference: values.reference.trim(),
		note: cleanNote(values.note)
	};
	const date = approximateDate(values.month, values.year);
	if (date) body.approximate_date = date;
	return body;
}

/**
 * Which field a 422 INVALID_INPUT names (its message starts with the API's
 * field name), as a form field; approximate_date is the month select.
 */
export function invalidField(message: string): OlderField | undefined {
	const first = message.split(' ', 1)[0];
	switch (first) {
		case 'reference':
		case 'channel':
		case 'note':
			return first;
		case 'approximate_date':
			return 'month';
		default:
			return undefined;
	}
}

/** The words for a field the API refused although this page found nothing wrong. */
export const SERVER_WORDS: Record<OlderField, string> = {
	reference: `Enter the order reference from the chef (up to ${REFERENCE_MAX} characters, without tabs or other special characters)`,
	channel: MESSAGES.channel,
	month: MESSAGES.date,
	year: MESSAGES.date,
	note: `Add a short note for the chef (up to ${NOTE_MAX} characters, without tabs or other special characters)`
};

export const SEND_PROBLEM = 'We couldn’t send your request right now. Please try again.';

/**
 * The words for a refused request that isn't about one field (409s; the API's
 * own messages for CLAIM_LIMIT and CLAIM_PENDING), or the general problem.
 */
export function refusal(e: unknown): string {
	if (e instanceof ApiError) {
		switch (e.code) {
			case 'CLAIM_LIMIT':
				return 'You already have 5 requests waiting for the chef. Please wait for her answer before sending another.';
			case 'CLAIM_PENDING':
				return 'You’ve already asked to add this order. The chef will check it.';
			case 'ALREADY_IN_ACCOUNT':
				return 'This order is already in your account.';
			case 'INVALID_INPUT':
				// A check we can't tie to one field: trying again won't help.
				return 'We couldn’t send this request as it is. Please check the details, or contact us.';
		}
	}
	return SEND_PROBLEM;
}

/** Whether an answer was lost, so the same request may already have arrived (keep its key). */
export function maybeArrived(e: unknown): boolean {
	return e instanceof ApiError && (e.status === 0 || e.retryable);
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

/** Whether a request page's path names a request ID at all (anything else is "not found"). */
export function isClaimId(value: string): boolean {
	return UUID.test(value.toLowerCase());
}

export const WITHDRAW_PROBLEM = 'We couldn’t withdraw this request right now. Please try again.';
