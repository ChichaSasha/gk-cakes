import { describe, expect, it } from 'vitest';
import { approximateLabel, claimRef, claimView } from './claims';
import type { Claim } from './client';

const claim = (over: Partial<Claim> = {}): Claim => ({
	id: 'c0ffee12-2222-4222-8222-222222222222',
	version: 1,
	state: 'pending',
	path: 'evidence',
	channel: 'instagram',
	reference: 'IG-0613',
	approximate_date: '2026-06',
	note: 'Pink cake for my mum',
	created_at: '2026-11-05T10:00:00Z',
	decided_at: null,
	order_id: null,
	...over
});

describe('claims in the account', () => {
	it('shows what was asked and its state, never order details before approval', () => {
		expect(claimView(claim())).toMatchObject({
			ref: 'c0ffee12',
			title: 'Instagram order · June 2026',
			chip: { tone: 'wait', text: 'Waiting for the chef' },
			pending: true
		});
		expect(claimView(claim({ path: 'email_link', channel: 'website_email_link' })).title).toBe(
			'Add an order to your account'
		);
		expect(claimView(claim({ state: 'approved', order_id: 'o' })).chip.text).toBe(
			'Added to your account'
		);
		const declined = claimView(claim({ state: 'declined' }));
		expect(declined.chip.text).toBe('Couldn’t add');
		expect(declined.line).not.toMatch(/order (GK|[0-9a-f]{8})/);
		expect(claimView(claim({ state: 'withdrawn' })).pending).toBe(false);
		expect(claimView(claim({ state: 'mystery' })).chip.text).toBe('Status not available');
		expect(claimView(claim({ channel: 'offline', approximate_date: null })).title).toBe(
			'Order made by email, phone or in person'
		);
	});

	it('words approximate dates and references', () => {
		expect(approximateLabel('2026-06')).toBe('June 2026');
		expect(approximateLabel('2026-06-13')).toBe('June 2026');
		expect(approximateLabel('2026-13')).toBe('2026-13');
		expect(approximateLabel(null)).toBeUndefined();
		expect(claimRef({ id: 'abcdef12-0000' })).toBe('abcdef12');
	});
});
