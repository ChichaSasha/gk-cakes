import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';
import type { BasketItem } from '../flow';
import type { Quote } from '../types';
import Allergens from './Allergens.svelte';
import OrderSummary from './OrderSummary.svelte';
import OrderTimeline from './OrderTimeline.svelte';
import PaymentMethods from './PaymentMethods.svelte';
import RefundTerms from './RefundTerms.svelte';

const text = (html: string) =>
	html
		.replace(/<!--[^]*?-->/g, '')
		.replace(/<\/?(strong|span)[^>]*>/g, '')
		.replace(/<[^>]+>/g, ' ')
		.replace(/\s+/g, ' ')
		.trim();

const item: BasketItem = {
	id: 'v1',
	ready: false,
	recipeVersionId: 'r',
	quantity: 1,
	cakeName: 'Chocolate swirl',
	flavour: 'Snickers',
	size: 'S · 6 inches',
	servings: '7–10 servings',
	allergens: [
		{ code: 'eggs', label: 'EGGS', prominent: false },
		{ code: 'peanuts', label: 'PEANUTS', prominent: true }
	],
	composition: ['Chocolate sponge, peanut cream cheese cream'],
	coating: 'Swiss meringue buttercream',
	notice: 'Allergen notice: shared kitchen.'
};

const quote: Quote = {
	id: 'q',
	version: 1,
	sent_at: 'x',
	expires_at: 'x',
	date: '2026-10-22',
	slot: { id: 's', starts_at: '2026-10-22T10:00:00Z', ends_at: '2026-10-22T11:00:00Z' },
	fulfillment_mode: 'collection',
	subtotal_minor: 18001,
	discount_minor: 1800,
	total_minor: 16201,
	currency: 'GBP',
	discount: { code: 'DEMO10', kind: 'percentage', basis_points: 1000 },
	lines: [
		{
			line_no: 1,
			variant_id: 'v1',
			lot_id: null,
			recipe_version_id: 'r',
			cake_id: 'c',
			cake_name: 'Chocolate swirl',
			flavour: 'Snickers',
			size_code: 's',
			quantity: 2,
			unit_price_minor: 9000,
			discount_minor: 1800,
			net_minor: 16201
		}
	]
};

describe('RefundTerms', () => {
	it('asks for a time first, then shows the r01 deadline with the server total', () => {
		expect(
			text(
				render(RefundTerms, { props: { startsAt: null, totalMinor: null, mode: 'collection' } })
					.body
			)
		).toContain('Choose a time to see your exact cancellation deadline.');
		const t = text(
			render(RefundTerms, {
				props: { startsAt: '2026-10-22T10:00:00Z', totalMinor: 8100, mode: 'collection' }
			}).body
		);
		expect(t).toContain(
			'Cancel before 11:00 (London time) on Sat 17 October for a full refund of £81.'
		);
		expect(t).toContain('if the cake isn’t collected, you get 50% of what you paid');
		// No client-computed 50% amount.
		expect(t).not.toContain('£40.50');
	});

	it('names the courier case', () => {
		const t = text(
			render(RefundTerms, {
				props: { startsAt: '2026-10-22T10:00:00Z', totalMinor: null, mode: 'customer_courier' }
			}).body
		);
		expect(t).toContain('for a full refund.');
		expect(t).toContain('if your courier doesn’t collect it');
	});
});

describe('Allergens', () => {
	it('lists each cake’s declaration, calls out prominent allergens and shows the notice once', () => {
		const t = text(render(Allergens, { props: { items: [item, { ...item, id: 'v2' }] } }).body);
		expect(t).toContain('Chocolate swirl · allergens');
		expect(t).toContain('Contains PEANUTS');
		expect(t).toContain('Contains: EGGS, PEANUTS.');
		expect(t.match(/Allergen notice: shared kitchen\./g)).toHaveLength(1);
	});
});

describe('OrderSummary', () => {
	const props = {
		items: [item],
		quoteLoading: false,
		dateText: 'Thu 22 October 2026',
		timeText: '11:00–12:00 · London time',
		modeText: 'Collect · St Katharine Docks',
		fulfilLabel: 'Collection',
		fulfilValue: 'Free'
	};

	it('shows only the server’s totals', () => {
		const t = text(render(OrderSummary, { props: { ...props, quote } }).body);
		expect(t).toContain('2 × £90');
		expect(t).toContain('Cakes£180.01');
		expect(t).toContain('Discount · DEMO10 (10%)−£18');
		expect(t).toContain('To pay now£162.01');
	});

	it('says Continue confirms a free order', () => {
		const t = text(
			render(OrderSummary, { props: { ...props, quote: { ...quote, total_minor: 0 } } }).body
		);
		expect(t).toContain('Pressing Continue confirms your free order.');
		expect(t).not.toContain('when we receive the payment');
	});

	it('offers to try again when the total could not be fetched', () => {
		const t = text(
			render(OrderSummary, {
				props: { ...props, quote: null, quoteFailed: true, onRetry: () => {} }
			}).body
		);
		expect(t).toContain('We couldn’t get your total. Try again');
		expect(t).not.toContain('Updating your total');
	});

	it('asks for a date and time before any total exists', () => {
		const t = text(render(OrderSummary, { props: { ...props, quote: null } }).body);
		expect(t).toContain('Choose a date and time to see your total.');
		expect(t).not.toContain('To pay now');
		expect(t).not.toMatch(/£\d/);
	});
});

describe('PaymentMethods', () => {
	it('shows six methods and all 17 on demand', () => {
		const t = text(render(PaymentMethods).body);
		expect(t).toContain('Cards · Apple Pay · Onelink · Klarna · Revolut Pay · Amazon Pay');
		expect(t).toContain('Show all 17 methods');
		expect(t).not.toContain('Google Pay');
	});
});

describe('OrderTimeline (state 19)', () => {
	it('two labelled lanes; the current step is marked and every state is spoken', () => {
		const { body } = render(OrderTimeline, {
			props: {
				timeline: {
					money: [
						{ label: 'Paid £90', detail: 'Tue 6 October, 14:05', state: 'done' },
						{ label: 'Refund of £45 requested', state: 'current' }
					],
					cake: [
						{ label: 'Order confirmed', state: 'done' },
						{ label: 'Not collected', detail: 'The collection time passed', state: 'problem' },
						{ label: 'Later', state: 'upcoming' }
					]
				}
			}
		});
		expect(body).toContain('aria-labelledby="timeline-h"');
		expect(body).toMatch(/<h3 id="lane-money">Money<\/h3>/);
		expect(body).toMatch(/<h3 id="lane-cake">Your cake<\/h3>/);
		expect(body.match(/aria-current="step"/g)).toHaveLength(1);
		const t = text(body);
		// (text() drops the span tags; the detail is a block on the page.)
		expect(t).toContain('Paid £90Tue 6 October, 14:05 · done');
		expect(t).toContain('Refund of £45 requested · now');
		expect(t).toContain('Not collectedThe collection time passed · problem');
		expect(t).toContain('Later · not yet');
		expect(body).toMatch(/<li class="problem"/);
	});
});
