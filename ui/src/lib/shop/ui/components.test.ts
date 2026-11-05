import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';
import { listing } from '../catalog';
import { cake, ID, lot, readyVariant, recipe, variant } from '../test-fixtures';
import CakeCard from './CakeCard.svelte';

const text = (html: string) =>
	html
		.replace(/<!--[^]*?-->/g, '')
		.replace(/<[^>]+>/g, ' ')
		.replace(/\s+/g, ' ')
		.trim();

describe('CakeCard', () => {
	it('shows the server price, servings, how it is made and prominent allergens', () => {
		const item = listing(cake({ variants: [variant(), readyVariant()] }), [lot()]);
		const { body } = render(CakeCard, { props: { item, index: 0 } });
		expect(text(body)).toContain('Chocolate swirl From £60');
		expect(text(body)).toContain('7–10 servings · Made to order · Ready-made');
		expect(text(body)).toContain('Contains PEANUTS');
		expect(body).toContain(`href="/cakes/${ID.cake}"`);
		// The photo placeholder is decorative inside the link.
		expect(body).toContain('aria-hidden="true"');
	});

	it('says when a cake cannot be bought right now', () => {
		const item = listing(
			cake({ variants: [variant({ purchasable: false, recipe: recipe() })] }),
			[]
		);
		const { body } = render(CakeCard, { props: { item, index: 2 } });
		expect(text(body)).toContain('Not available');
		expect(text(body)).toContain('Not available right now');
		expect(text(body)).not.toContain('PEANUTS');
	});

	it('has a favourites heart outside the link, named for the cake and not pressed', () => {
		const item = listing(cake(), []);
		const { body } = render(CakeCard, { props: { item, index: 0 } });
		expect(body).toMatch(
			/<\/a>\s*(<!--[^]*?-->\s*)*<button class="save" type="button" aria-label="Save Chocolate swirl" aria-pressed="false"/
		);
	});
});
