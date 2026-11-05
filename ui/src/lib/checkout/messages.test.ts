import { describe, expect, it } from 'vitest';
import { ApiError } from '../api';
import { orderProblemFor, problemFor } from './messages';

const err = (status: number, code: string, message = '', retryable?: boolean) =>
	new ApiError(status, { code, message, request_id: 'req-1', retryable });

describe('problemFor', () => {
	it('maps checkout refusals to plain messages and next steps', () => {
		const cases: [ApiError, string, RegExp][] = [
			[err(409, 'PRICE_CHANGED'), 'requote', /price changed.*review the new total/i],
			[err(409, 'QUOTE_EXPIRED'), 'requote', /24 hours/],
			[err(409, 'POLICY_CHANGED'), 'reload_policy', /terms have just been updated/],
			[err(409, 'SLOT_UNAVAILABLE'), 'reload_slots', /no longer available/],
			[err(409, 'LEAD_TIME'), 'reload_dates', /at least 8 days/],
			[err(409, 'ITEM_UNAVAILABLE'), 'start_again', /no longer available as chosen/],
			[err(409, 'CODE_USED_UP'), 'remove_code', /used up/],
			[err(409, 'PAYMENT_IN_PROGRESS'), 'view_order', /don’t pay again/],
			[err(409, 'STALE_VERSION'), 'view_order', /changed/],
			[err(409, 'CONFLICT'), 'view_order', /changed/]
		];
		for (const [e, next, text] of cases) {
			const p = problemFor(e);
			expect(p.next, e.code).toBe(next);
			expect(p.message, e.code).toMatch(text);
		}
	});

	it('treats an unavailable service as retryable and never as a failed payment', () => {
		for (const e of [
			err(503, 'UNAVAILABLE', 'Payments are temporarily unavailable', true),
			err(0, 'NETWORK', '', true),
			err(502, 'UNKNOWN')
		]) {
			const p = problemFor(e);
			expect(p.next).toBe('retry');
			expect(p.message).toMatch(/Nothing was charged/);
		}
	});

	it('names the field of an invalid discount code or contact detail', () => {
		expect(problemFor(err(422, 'INVALID_INPUT', 'discount_code is not a current code'))).toEqual({
			message: 'We don’t recognise that code. Check the spelling, or continue without one.',
			next: 'fix_input',
			field: 'code'
		});
		expect(
			problemFor(
				err(422, 'INVALID_INPUT', 'discount_code needs you to sign in (it is limited per customer)')
			).message
		).toMatch(/signed-in customers/);
		expect(
			problemFor(err(422, 'INVALID_INPUT', 'discount_code applies to GK delivery only')).message
		).toMatch(/GK delivery/);
		expect(
			problemFor(err(422, 'INVALID_INPUT', 'contact.email must be an email address')).field
		).toBe('email');
		expect(problemFor(err(422, 'INVALID_INPUT', 'contact.full_name must be 1 to 120')).field).toBe(
			'name'
		);
		expect(problemFor(err(422, 'INVALID_INPUT', 'contact.payer_name must be 1 to 120')).field).toBe(
			'payer'
		);
	});

	it('asks to sign in after a refused session and gives a reference otherwise', () => {
		expect(problemFor(err(401, 'SESSION_ENDED')).next).toBe('sign_in');
		const p = problemFor(err(400, 'BAD_REQUEST'));
		expect(p.next).toBe('retry');
		expect(p.reference).toBe('req-1');
		expect(problemFor(err(422, 'INVALID_INPUT', 'lines must have 1 to 20 lines')).reference).toBe(
			'req-1'
		);
		expect(problemFor(new TypeError('x')).next).toBe('retry');
	});

	it('never shows the API’s internal message for a refusal', () => {
		const p = problemFor(err(409, 'PRICE_CHANGED', 'internal: recipe version 3 superseded'));
		expect(p.message).not.toContain('internal');
	});
});

describe('KEY_REUSED and order-page problems', () => {
	it('explains that an order was already started', () => {
		const p = problemFor(err(409, 'KEY_REUSED'));
		expect(p.next).toBe('key_reused');
		expect(p.message).toMatch(/already started/);
	});

	it('asks for a new order when a lead time or price window was missed on the order page', () => {
		for (const code of ['LEAD_TIME', 'QUOTE_EXPIRED']) {
			const p = orderProblemFor(err(409, code));
			expect(p.next, code).toBe('new_order');
			expect(p.message, code).toMatch(/start a new order/);
			expect(p.message, code).toMatch(/Nothing was charged/);
		}
		expect(orderProblemFor(err(409, 'PAYMENT_IN_PROGRESS')).next).toBe('view_order');
		expect(orderProblemFor(err(503, 'UNAVAILABLE', '', true)).next).toBe('retry');
		expect(orderProblemFor(err(409, 'CONFLICT')).next).toBe('none');
	});
});
