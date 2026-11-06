// The "Profile & contact" form (approved GK-016 r02b sketch, states 43–46) as
// pure helpers: the API's rules checked before sending (api/internal/
// checkout/accounts.go ProfilePatch.Validate) with the sketch's wording, the
// PATCH with only the changed fields, and the merge after a 409
// STALE_VERSION (latest details loaded, the customer's own changes kept).
import { newIdempotencyKey } from '../api';
import type { Profile } from './client';

export type Field = 'full_name' | 'phone' | 'instagram';
export const FIELDS: readonly Field[] = ['full_name', 'phone', 'instagram'];

/** What the form shows: one string per field ('' when unset). */
export type FormValues = Record<Field, string>;

export type FieldErrors = Partial<Record<Field, string>>;

/** Profile limits, as the API checks them. */
export const NAME_MAX = 120;
export const INSTAGRAM_MAX = 30;
/** Longest phone the API pattern accepts (+, digit, 22 more, digit). */
export const PHONE_MAX = 25;

const PHONE = /^\+?[0-9][0-9 ()-]{5,22}[0-9]$/;
const INSTAGRAM = /^[A-Za-z0-9._]{1,30}$/;
// Control characters and the Unicode line/paragraph separators (the API's name rule).
// eslint-disable-next-line no-control-regex
const CONTROL = /[\u0000-\u001f\u007f-\u009f\u2028\u2029]/;

export const MESSAGES = {
	nameLong: `Names can be up to ${NAME_MAX} characters`,
	nameControl: 'Names can’t contain tabs or other special characters',
	phone: 'Enter a phone number like 07700 900123 or +44 7700 900123',
	instagram: 'Instagram names use letters, numbers, full stops and underscores',
	instagramLong: `Instagram names have up to ${INSTAGRAM_MAX} characters`
} as const;

/** The form's starting values from a profile. */
export function formValues(p: Pick<Profile, Field>): FormValues {
	return { full_name: p.full_name ?? '', phone: p.phone ?? '', instagram: p.instagram ?? '' };
}

/**
 * What a field means for the API: trimmed, empty is null (clears it). An
 * Instagram name may be typed with its @ (the sketch shows "@anna.celebrates"
 * and allows one); the API stores it without.
 */
export function normalise(field: Field, raw: string): string | null {
	let v = raw.trim();
	if (field === 'instagram' && v.startsWith('@')) v = v.slice(1).trim();
	return v === '' ? null : v;
}

/** The API's phone rule: optional +, digits, spaces, hyphens and brackets, 7 to 15 digits. */
export function validPhone(s: string): boolean {
	if (!PHONE.test(s)) return false;
	const digits = s.replace(/[^0-9]/g, '').length;
	return digits >= 7 && digits <= 15;
}

/** The error for one field's value, or undefined when the API would accept it. */
export function fieldError(field: Field, raw: string): string | undefined {
	const v = normalise(field, raw);
	if (v === null) return undefined;
	switch (field) {
		case 'full_name':
			if ([...v].length > NAME_MAX) return MESSAGES.nameLong;
			if (CONTROL.test(v)) return MESSAGES.nameControl;
			return undefined;
		case 'phone':
			return validPhone(v) ? undefined : MESSAGES.phone;
		case 'instagram':
			if (v.length > INSTAGRAM_MAX && /^[A-Za-z0-9._]+$/.test(v)) return MESSAGES.instagramLong;
			return INSTAGRAM.test(v) ? undefined : MESSAGES.instagram;
	}
}

/** Every field's error, in form order (empty when the form can be saved). */
export function validate(values: FormValues): FieldErrors {
	const errors: FieldErrors = {};
	for (const f of FIELDS) {
		const e = fieldError(f, values[f]);
		if (e) errors[f] = e;
	}
	return errors;
}

export interface ProfilePatch {
	expected_version: number;
	full_name?: string | null;
	phone?: string | null;
	instagram?: string | null;
}

/**
 * The PATCH for what changed since `saved` (only those fields; null clears),
 * or null when nothing changed.
 */
export function buildPatch(saved: Profile, values: FormValues): ProfilePatch | null {
	const patch: ProfilePatch = { expected_version: saved.version };
	let changed = false;
	for (const f of FIELDS) {
		const v = normalise(f, values[f]);
		if (v !== (saved[f] ?? null)) {
			patch[f] = v;
			changed = true;
		}
	}
	return changed ? patch : null;
}

/** Which field a 422 INVALID_INPUT names (its message starts with the field). */
export function invalidField(message: string): Field | undefined {
	const first = message.split(' ', 1)[0];
	return (FIELDS as readonly string[]).includes(first) ? (first as Field) : undefined;
}

export interface Merge {
	values: FormValues;
	/** Fields where the customer's unsaved change was kept, with the value saved elsewhere. */
	kept: Partial<Record<Field, string | null>>;
}

/**
 * After 409 STALE_VERSION: the latest saved details, except fields the
 * customer changed since `started` (what the form showed when they began)
 * that now differ from the latest; those keep the customer's value and are
 * reported as kept (r02b state 46).
 */
export function mergeConflict(started: Profile, latest: Profile, values: FormValues): Merge {
	const out = formValues(latest);
	const kept: Merge['kept'] = {};
	for (const f of FIELDS) {
		const mine = normalise(f, values[f]);
		const edited = mine !== (started[f] ?? null);
		if (edited && mine !== (latest[f] ?? null)) {
			out[f] = values[f];
			kept[f] = latest[f] ?? null;
		}
	}
	return { values: out, kept };
}

const NOUN: Record<Field, string> = {
	full_name: 'name',
	phone: 'phone number',
	instagram: 'Instagram name'
};

function list(words: string[]): string {
	if (words.length <= 1) return words.join('');
	return `${words.slice(0, -1).join(', ')} and ${words[words.length - 1]}`;
}

/** The conflict notice's second sentence (r02b state 46), naming what was kept. */
export function conflictLine(kept: Merge['kept']): string {
	const fields = FIELDS.filter((f) => f in kept).map((f) => NOUN[f]);
	if (fields.length === 0)
		return 'We’ve loaded the latest saved details below. Check them and save again.';
	return `We’ve loaded the latest saved details below and kept your ${list(fields)} ${
		fields.length === 1 ? 'change' : 'changes'
	}. Check them and save again.`;
}

/** The note under a kept field: "Your change, not saved yet. Saved elsewhere: …". */
export function keptNote(saved: string | null): string {
	return `Your change, not saved yet. Saved elsewhere: ${saved ?? 'none'}.`;
}

/**
 * One Idempotency-Key per save: the same key while the same PATCH is retried
 * (so a save whose answer was lost is not applied twice), a new key for any
 * other PATCH (the API refuses a key reused with another body).
 */
export class SaveKey {
	private last: { body: string; key: string } | null = null;
	constructor(private readonly makeKey: () => string = newIdempotencyKey) {}

	for(patch: ProfilePatch): string {
		const body = JSON.stringify(patch);
		if (this.last?.body !== body) this.last = { body, key: this.makeKey() };
		return this.last.key;
	}

	/** After an answer that settles the save (saved, refused or stale). */
	forget(): void {
		this.last = null;
	}
}
