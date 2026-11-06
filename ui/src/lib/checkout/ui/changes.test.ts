import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';
import type { OrderChangesApi } from '../client';
import { ActionKeys } from '../flow';
import CancelPanel from './CancelPanel.svelte';
import RefundProgress from './RefundProgress.svelte';
import TimeList from './TimeList.svelte';

const text = (html: string) =>
	html
		.replace(/<!--[^]*?-->/g, '')
		.replace(/<[^>]+>/g, ' ')
		.replace(/\s+/g, ' ')
		.trim();

describe('RefundProgress', () => {
	it('marks the current step for assistive technology and keeps past tense for finished steps', () => {
		const { body } = render(RefundProgress, {
			props: { refund: { state: 'pending', target_minor: 9000, succeeded_minor: 0 } }
		});
		expect(body).toContain('aria-label="Refund progress"');
		expect(body).toMatch(/<li class="done"[^>]*>/);
		expect(body).toMatch(/<li class="current" aria-current="step">/);
		// The spoken state keeps its separating space after the label.
		expect(body).toContain('<span class="sr-only"> · current step</span>');
		expect(body).toContain('<span class="sr-only"> · done</span>');
		expect(text(body)).toBe(
			'Refund requested · done Sent to our payment provider · waiting for them · current step Return to your payment method'
		);
	});

	it('shows the chef’s delivery refund on its own line', () => {
		const { body } = render(RefundProgress, {
			props: {
				refund: {
					state: 'requested',
					target_minor: 10500,
					succeeded_minor: 0,
					in_progress_minor: 4500,
					delivery_pending_minor: 6000
				}
			}
		});
		expect(body).toContain('aria-label="Cake refund progress"');
		expect(text(body)).toContain(
			'Cakes · to your payment method Requested Delivery · sent separately by the chef Waiting for the chef £60'
		);
	});
});

describe('TimeList', () => {
	it('is a fieldset of native radios with the legend, the current time checked and refused times disabled', () => {
		const { body } = render(TimeList, {
			props: {
				legend: 'Collection time on Thu 22 October · London time',
				name: 'move',
				value: 's11',
				options: [
					{
						id: 's11',
						label: '11:00–12:00',
						note: 'Your current time',
						current: true,
						disabled: false,
						gone: false
					},
					{
						id: 's12',
						label: '12:00–13:00',
						note: 'No longer available',
						current: false,
						disabled: true,
						gone: true
					},
					{
						id: 's14',
						label: '14:00–15:00',
						note: 'Available',
						current: false,
						disabled: false,
						gone: false
					}
				]
			}
		});
		expect(body).toMatch(/<fieldset class="oc-times"/);
		expect(body).toContain('<legend>Collection time on Thu 22 October · London time</legend>');
		expect(body.match(/type="radio"/g)).toHaveLength(3);
		expect(body).toMatch(/value="s11"[^>]*checked/);
		expect(body).toMatch(/value="s12"[^>]*disabled/);
		expect(body).toMatch(/class="oc-time is-off is-gone"/);
		expect(text(body)).toContain('11:00–12:00 Your current time');
	});
});

describe('CancelPanel', () => {
	it('opens as “Cancel your order?” while the amount is checked, with Keep my order', () => {
		const never = new Promise<never>(() => undefined);
		const api = {
			cancellationPreview: () => never,
			cancel: () => never,
			moveChoices: () => never,
			movePickup: () => never
		} as unknown as OrderChangesApi;
		const { body } = render(CancelPanel, {
			props: {
				orderId: 'o',
				who: { kind: 'guest', secret: 'a'.repeat(43) },
				api,
				keys: new ActionKeys(),
				delivery: false,
				onclose: () => undefined,
				oncancelled: () => undefined
			}
		});
		expect(body).toMatch(
			/<h2 class="co-h oc-focus" id="cancel-h" tabindex="-1">Cancel your order\?<\/h2>/
		);
		expect(text(body)).toContain('Checking your refund amount… Keep my order');
		// No amount is shown before the API's preview arrives.
		expect(body).not.toContain('£');
	});
});
