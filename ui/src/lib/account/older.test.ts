import { describe, expect, it } from 'vitest';
import { ApiError } from '../api';
import {
	approximateDate,
	CHANNELS,
	channelLabel,
	claimBody,
	cleanNote,
	fieldError,
	invalidField,
	isClaimId,
	maybeArrived,
	MESSAGES,
	refusal,
	SEND_PROBLEM,
	SERVER_WORDS,
	validate,
	yearOptions,
	type OlderValues
} from './older';
import { SaveKey } from './profile';

const form = (over: Partial<OlderValues> = {}): OlderValues => ({
	reference: 'IG-0613',
	channel: 'instagram',
	month: '6',
	year: '2026',
	note: 'Two-tier chocolate raspberry cake for my mum’s 60th.',
	...over
});

const apiError = (status: number, code: string, message = 'x', retryable?: boolean) =>
	new ApiError(status, { code, message, retryable });

describe('older order form', () => {
	it('offers only the channels the API records (instagram, offline)', () => {
		expect(CHANNELS.map((c) => c.value)).toEqual(['instagram', 'offline']);
		expect(CHANNELS.map((c) => c.label)).toEqual(['Instagram', 'Email, phone or in person']);
		expect(channelLabel('website_email_link')).toBe('On this website');
		expect(channelLabel('offline')).toBe('Email, phone or in person');
		expect(channelLabel('fax')).toBe('Not shown');
	});

	it('asks for the reference and note in the sketch’s words (state 39)', () => {
		expect(validate(form({ reference: '  ', note: '\n ' }))).toEqual({
			reference: 'Enter the order reference from the chef',
			note: 'Add a short note for the chef'
		});
		expect(validate(form())).toEqual({});
	});

	it('checks the API’s limits: reference 1–64, note 1–500 characters (code points)', () => {
		expect(fieldError('reference', form({ reference: 'x'.repeat(64) }))).toBeUndefined();
		expect(fieldError('reference', form({ reference: 'x'.repeat(65) }))).toBe(
			MESSAGES.referenceLong
		);
		expect(fieldError('reference', form({ reference: 'IG\t0613' }))).toBe(
			MESSAGES.referenceControl
		);
		expect(fieldError('note', form({ note: '🎂'.repeat(500) }))).toBeUndefined();
		expect(fieldError('note', form({ note: 'x'.repeat(501) }))).toBe(MESSAGES.noteLong);
		expect(fieldError('note', form({ note: 'line one\nline two' }))).toBeUndefined();
		expect(fieldError('note', form({ note: 'a\tb' }))).toBe(MESSAGES.noteControl);
		expect(fieldError('note', form({ note: 'a\u2028b' }))).toBe(MESSAGES.noteControl);
	});

	it('needs both month and year, or neither', () => {
		expect(validate(form({ month: '', year: '' }))).toEqual({});
		expect(validate(form({ month: '6', year: '' }))).toEqual({ year: MESSAGES.year });
		expect(validate(form({ month: '', year: '2025' }))).toEqual({ month: MESSAGES.month });
	});

	it('builds the path c body: trimmed, YYYY-MM, no date when none was chosen', () => {
		expect(claimBody(form({ reference: ' IG-0613 ', note: ' Pink cake\r\nfor Mum \n' }))).toEqual({
			channel: 'instagram',
			reference: 'IG-0613',
			approximate_date: '2026-06',
			note: 'Pink cake\nfor Mum'
		});
		expect(claimBody(form({ channel: 'offline', month: '', year: '' }))).toEqual({
			channel: 'offline',
			reference: 'IG-0613',
			note: 'Two-tier chocolate raspberry cake for my mum’s 60th.'
		});
		expect(approximateDate('12', '2023')).toBe('2023-12');
		expect(approximateDate('13', '2023')).toBeUndefined();
		expect(approximateDate('1', '23')).toBeUndefined();
		expect(cleanNote('a\r\nb\rc')).toBe('a\nb\nc');
	});

	it('offers this year and the three before', () => {
		expect(yearOptions(new Date('2026-11-06T12:00:00Z'))).toEqual(['2026', '2025', '2024', '2023']);
	});
});

describe('older order refusals', () => {
	it('maps a 422 to its form field', () => {
		expect(invalidField('reference must be 1 to 64 characters without surrounding spaces')).toBe(
			'reference'
		);
		expect(invalidField('note is required: a short note for the chef')).toBe('note');
		expect(invalidField('approximate_date must be a month (YYYY-MM) or a date')).toBe('month');
		expect(invalidField('channel must be instagram')).toBe('channel');
		expect(invalidField('claim is not valid')).toBeUndefined();
		expect(SERVER_WORDS.month).toBe(MESSAGES.date);
	});

	it('words the 409s and everything else as a send problem', () => {
		expect(refusal(apiError(409, 'CLAIM_LIMIT'))).toMatch(/5 requests waiting for the chef/);
		expect(refusal(apiError(409, 'CLAIM_PENDING'))).toMatch(/already asked to add this order/);
		expect(refusal(apiError(409, 'ALREADY_IN_ACCOUNT'))).toBe(
			'This order is already in your account.'
		);
		expect(refusal(apiError(405, 'READ_ONLY_PROXY'))).toBe(SEND_PROBLEM);
		expect(refusal(apiError(503, 'UNAVAILABLE'))).toBe(SEND_PROBLEM);
		expect(refusal(new Error('boom'))).toBe(SEND_PROBLEM);
	});

	it('keeps the key only when the answer may have been lost', () => {
		expect(maybeArrived(apiError(0, 'NETWORK', 'x', true))).toBe(true);
		expect(maybeArrived(apiError(503, 'UNAVAILABLE'))).toBe(true);
		expect(maybeArrived(apiError(405, 'READ_ONLY_PROXY'))).toBe(false);
		expect(maybeArrived(apiError(409, 'CLAIM_LIMIT'))).toBe(false);
		expect(maybeArrived(new Error('x'))).toBe(false);
	});

	it('recognises request IDs', () => {
		expect(isClaimId('c0ffee12-2222-4222-8222-222222222222')).toBe(true);
		expect(isClaimId('not-an-id')).toBe(false);
		expect(isClaimId('C0FFEE12-2222-4222-8222-222222222222')).toBe(true); // capitals are accepted (the page lowercases)
	});
});

describe('one key per request', () => {
	it('reuses the key while the same claim or withdrawal is retried', () => {
		let n = 0;
		const keys = new SaveKey(() => `k${++n}`);
		const body = claimBody(form());
		expect(keys.for(body)).toBe('k1');
		expect(keys.for(claimBody(form()))).toBe('k1');
		expect(keys.for(claimBody(form({ note: 'Another note' })))).toBe('k2');
		keys.forget();
		expect(keys.for({ claim: 'c', expected_version: 1 })).toBe('k3');
	});
});

describe('slice 3 review follow-ups', () => {
	it('an unrecognised 422 is not "try again", and request IDs in capitals still work', async () => {
		const { ApiError } = await import('../api');
		const { refusal, isClaimId } = await import('./older');
		expect(refusal(new ApiError(422, { code: 'INVALID_INPUT', message: 'claim invalid' }))).toMatch(
			/check the details/
		);
		expect(isClaimId('C0FFEE12-2222-4222-8222-222222222222')).toBe(true);
		expect(isClaimId('not-an-id')).toBe(false);
	});
});
