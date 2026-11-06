import { describe, expect, it } from 'vitest';
import type { Profile } from './client';
import {
	buildPatch,
	conflictLine,
	fieldError,
	formValues,
	invalidField,
	keptNote,
	MESSAGES,
	mergeConflict,
	normalise,
	SaveKey,
	validate,
	validPhone
} from './profile';

const profile = (over: Partial<Profile> = {}): Profile => ({
	full_name: 'Anna Clarke',
	phone: '07700 900123',
	instagram: 'anna.celebrates',
	version: 3,
	updated_at: '2026-11-05T10:00:00Z',
	...over
});

describe('profile values', () => {
	it('shows unset fields as empty inputs', () => {
		expect(formValues(profile({ phone: null, instagram: null }))).toEqual({
			full_name: 'Anna Clarke',
			phone: '',
			instagram: ''
		});
	});

	it('trims, turns an empty field into null and drops one @ from an Instagram name', () => {
		expect(normalise('full_name', '  Anna  ')).toBe('Anna');
		expect(normalise('phone', '   ')).toBeNull();
		expect(normalise('instagram', '@anna.celebrates')).toBe('anna.celebrates');
		expect(normalise('instagram', ' @ ')).toBeNull();
		expect(normalise('instagram', '@@anna')).toBe('@anna');
		expect(normalise('full_name', '@Anna')).toBe('@Anna');
	});
});

describe('profile rules (as the API checks them)', () => {
	it('accepts the phone numbers of the sketch and the API rule', () => {
		for (const ok of ['07700 900123', '+44 7700 900123', '+44 (0)20 7946-0000', '1234567']) {
			expect(validPhone(ok), ok).toBe(true);
		}
		for (const bad of [
			'0770 09',
			'123456',
			'1234567890123456',
			'(020) 7946 0000',
			'07700 900123 ',
			'07700-',
			'+',
			'0770O 900123',
			'++44 7700 900123'
		]) {
			expect(validPhone(bad), bad).toBe(false);
		}
	});

	it('words errors as the sketch (r02b state 45)', () => {
		expect(fieldError('phone', '0770 09')).toBe(
			'Enter a phone number like 07700 900123 or +44 7700 900123'
		);
		expect(fieldError('instagram', 'anna celebrates!')).toBe(
			'Instagram names use letters, numbers, full stops and underscores'
		);
		expect(fieldError('instagram', 'a'.repeat(31))).toBe(MESSAGES.instagramLong);
		expect(fieldError('instagram', '@' + 'a'.repeat(30))).toBeUndefined();
		expect(fieldError('instagram', 'anna_c.1')).toBeUndefined();
	});

	it('limits a name to 120 characters without control characters', () => {
		expect(fieldError('full_name', 'é'.repeat(120))).toBeUndefined();
		expect(fieldError('full_name', 'a'.repeat(121))).toBe(MESSAGES.nameLong);
		expect(fieldError('full_name', 'Anna\tClarke')).toBe(MESSAGES.nameControl);
		expect(fieldError('full_name', 'Anna\u2028Clarke')).toBe(MESSAGES.nameControl);
		expect(fieldError('full_name', '  ')).toBeUndefined();
	});

	it('returns every error in form order and nothing for empty (optional) fields', () => {
		expect(validate({ full_name: '', phone: '', instagram: '' })).toEqual({});
		expect(Object.keys(validate({ full_name: 'x', phone: '0770 09', instagram: 'a b' }))).toEqual([
			'phone',
			'instagram'
		]);
	});

	it('reads which field a 422 names', () => {
		expect(invalidField('phone must be a phone number like 07700 900123')).toBe('phone');
		expect(invalidField('full_name must be 1 to 120 characters')).toBe('full_name');
		expect(invalidField('profile must change at least one of …')).toBeUndefined();
	});
});

describe('the PATCH', () => {
	it('sends only changed fields, null to clear, with the version seen', () => {
		const saved = profile();
		expect(
			buildPatch(saved, { full_name: ' Anna Clarke ', phone: '', instagram: '@anna.celebrates' })
		).toEqual({ expected_version: 3, phone: null });
		expect(
			buildPatch(profile({ full_name: null }), {
				full_name: 'GK Test',
				phone: '07700 900123',
				instagram: 'anna_c'
			})
		).toEqual({ expected_version: 3, full_name: 'GK Test', instagram: 'anna_c' });
	});

	it('is null when nothing changed', () => {
		expect(buildPatch(profile(), formValues(profile()))).toBeNull();
		expect(
			buildPatch(profile({ phone: null }), {
				full_name: 'Anna Clarke',
				phone: '  ',
				instagram: 'anna.celebrates'
			})
		).toBeNull();
	});
});

describe('conflict merge (r02b state 46)', () => {
	it('loads the latest, keeps the customer’s differing change and reports it', () => {
		const started = profile({ full_name: 'Anna C', phone: '07700 900123' });
		const latest = profile({ full_name: 'Anna Clarke', phone: '07700 900123', version: 4 });
		const typed = { full_name: 'Anna C', phone: '07700 900456', instagram: 'anna.celebrates' };
		const m = mergeConflict(started, latest, typed);
		expect(m.values).toEqual({
			full_name: 'Anna Clarke',
			phone: '07700 900456',
			instagram: 'anna.celebrates'
		});
		expect(m.kept).toEqual({ phone: '07700 900123' });
		expect(conflictLine(m.kept)).toBe(
			'We’ve loaded the latest saved details below and kept your phone number change. Check them and save again.'
		);
		expect(buildPatch(latest, m.values)).toEqual({ expected_version: 4, phone: '07700 900456' });
	});

	it('does not report a change that already matches the latest', () => {
		const started = profile({ phone: null });
		const latest = profile({ phone: '07700 900456', version: 4 });
		const m = mergeConflict(started, latest, {
			full_name: 'Anna Clarke',
			phone: '07700 900456 ',
			instagram: 'anna.celebrates'
		});
		expect(m.kept).toEqual({});
		expect(conflictLine(m.kept)).toBe(
			'We’ve loaded the latest saved details below. Check them and save again.'
		);
	});

	it('keeps a cleared field and names several kept changes', () => {
		const started = profile();
		const latest = profile({ full_name: 'Anna B', instagram: 'other', version: 5 });
		const m = mergeConflict(started, latest, {
			full_name: 'Anna K',
			phone: '07700 900123',
			instagram: ''
		});
		expect(m.values).toEqual({ full_name: 'Anna K', phone: '07700 900123', instagram: '' });
		expect(m.kept).toEqual({ full_name: 'Anna B', instagram: 'other' });
		expect(conflictLine(m.kept)).toContain('kept your name and Instagram name changes.');
		expect(conflictLine({ full_name: null, phone: null, instagram: null })).toContain(
			'kept your name, phone number and Instagram name changes.'
		);
	});

	it('words the kept note as the sketch, with "none" for an empty saved value', () => {
		expect(keptNote('07700 900123')).toBe(
			'Your change, not saved yet. Saved elsewhere: 07700 900123.'
		);
		expect(keptNote(null)).toBe('Your change, not saved yet. Saved elsewhere: none.');
	});
});

describe('save key', () => {
	it('reuses the key while the same PATCH is retried, and a new one otherwise', () => {
		let n = 0;
		const keys = new SaveKey(() => `k${++n}`);
		const a = { expected_version: 3, phone: null };
		expect(keys.for(a)).toBe('k1');
		expect(keys.for({ ...a })).toBe('k1');
		expect(keys.for({ expected_version: 3, phone: '07700 900456' })).toBe('k2');
		keys.forget();
		expect(keys.for({ expected_version: 3, phone: '07700 900456' })).toBe('k3');
	});
});
