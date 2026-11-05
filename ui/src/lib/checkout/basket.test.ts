import { describe, expect, it } from 'vitest';
import { madeToOrderQuantity, parseBasket, quoteLines } from './basket';

const a = '11111111-1111-4111-8111-111111111111';
const b = '22222222-2222-4222-8222-222222222222';

describe('parseBasket', () => {
	it('reads repeated variants and lots as quantities, in order', () => {
		const got = parseBasket(new URLSearchParams(`v=${a}&v=${a}&lot=${b}&date=2026-10-22`));
		expect(got).toEqual({
			lines: [
				{ variantId: a, quantity: 2 },
				{ lotId: b, quantity: 1 }
			],
			date: '2026-10-22'
		});
		expect(madeToOrderQuantity(got.lines)).toBe(2);
		expect(quoteLines(got.lines)).toEqual([
			{ variant_id: a, quantity: 2 },
			{ lot_id: b, quantity: 1 }
		]);
	});

	it('refuses malformed IDs and reports an empty basket', () => {
		expect(parseBasket(new URLSearchParams(`v=${a}&v=not-an-id`)).problem).toBe('invalid');
		expect(parseBasket(new URLSearchParams(`v=AAAAAAAA-1111-4111-8111-111111111111`)).problem).toBe(
			'invalid'
		);
		expect(parseBasket(new URLSearchParams('')).problem).toBe('empty');
	});

	it('ignores an invalid date and enforces the API’s 20-cake limit', () => {
		expect(parseBasket(new URLSearchParams(`v=${a}&date=tomorrow`)).date).toBeUndefined();
		const many = new URLSearchParams();
		for (let i = 0; i < 21; i++) many.append('v', a);
		expect(parseBasket(many).problem).toBe('too_many');
	});
});
