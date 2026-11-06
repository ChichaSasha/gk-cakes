import { describe, expect, it } from 'vitest';
import type { AccountOrder } from './client';
import { groupOrders, modeLabel, orderListItem, orderName, orderRef } from './orders';

const order = (over: Partial<AccountOrder> = {}): AccountOrder => ({
	id: '3f2a9c1b-1111-4111-8111-111111111111',
	version: 3,
	lifecycle: 'confirmed',
	cancel_reason: null,
	cancelled_at: null,
	payment_requirement: 'full',
	placed_at: '2026-11-05T10:00:00Z',
	date: '2026-11-21',
	fulfillment_mode: 'collection',
	slot: { starts_at: '2026-11-21T13:00:00Z', ends_at: '2026-11-21T14:00:00Z' },
	fulfillment_state: null,
	total_minor: 9000,
	currency: 'GBP',
	payment: null,
	free_confirmation: null,
	cancellation: null,
	appointment_change: null,
	refund: null,
	items: [{ cake_name: 'Pink piping', flavour: 'Vanilla raspberry', size_code: 's', quantity: 1 }],
	added_by: 'checkout',
	added_at: '2026-11-05T10:00:00Z',
	...over
});
const pay = (state: string, page = false) => ({
	state,
	amount_minor: 9000,
	expires_at: null,
	payment_page_open: page
});
const row = (over: Partial<AccountOrder>) => {
	const i = orderListItem(order(over));
	return [i.chip.tone, i.chip.text, i.action, i.group];
};

describe('account order list mapping (r02b "Order list mapping")', () => {
	it('awaiting payment: waiting, failed, open page, checking, received', () => {
		expect(row({ lifecycle: 'awaiting_payment' })).toEqual([
			'wait',
			'Waiting for payment',
			'pay',
			'current'
		]);
		expect(orderListItem(order({ lifecycle: 'awaiting_payment' })).actionLabel).toBe(
			'Continue to payment'
		);
		expect(row({ lifecycle: 'awaiting_payment', payment: pay('failed') })).toEqual([
			'wait',
			'Payment didn’t go through',
			'retry_pay',
			'current'
		]);
		expect(row({ lifecycle: 'awaiting_payment', payment: pay('cancelled') })[2]).toBe('retry_pay');
		expect(row({ lifecycle: 'awaiting_payment', payment: pay('created', true) })).toEqual([
			'wait',
			'Waiting for payment',
			'pay',
			'current'
		]);
		for (const s of ['created', 'pending', 'unknown']) {
			expect(row({ lifecycle: 'awaiting_payment', payment: pay(s) })).toEqual([
				'wait',
				'Still checking payment',
				'view',
				'current'
			]);
		}
		expect(row({ lifecycle: 'awaiting_payment', payment: pay('succeeded') })[1]).toBe(
			'Payment received · confirming'
		);
	});

	it('free orders: confirm, refused (retry or start again), confirming', () => {
		const free = { lifecycle: 'awaiting_payment', payment_requirement: 'none', total_minor: 0 };
		expect(row(free)).toEqual(['wait', 'Confirm your order', 'confirm', 'current']);
		expect(
			row({ ...free, free_confirmation: { state: 'refused', reason: 'order_changed' } })
		).toEqual(['wait', 'Couldn’t confirm yet', 'retry_confirm', 'current']);
		for (const reason of ['window_closed', 'holds_released']) {
			expect(row({ ...free, free_confirmation: { state: 'refused', reason } })).toEqual([
				'bad',
				'Couldn’t confirm',
				'view',
				'current'
			]);
		}
		expect(row({ ...free, free_confirmation: { state: 'processing', reason: null } })[1]).toBe(
			'Confirming'
		);
		expect(row({ lifecycle: 'confirmation_pending', payment_requirement: 'none' })[1]).toBe(
			'Confirming'
		);
		expect(row({ lifecycle: 'confirmation_pending' })[1]).toBe('Payment received · confirming');
	});

	it('paid, date to agree shows the date as requested', () => {
		const i = orderListItem(
			order({ lifecycle: 'paid_awaiting_agreement', payment: pay('succeeded') })
		);
		expect([i.chip.text, i.requested, i.group]).toEqual([
			'Payment received · date to agree',
			true,
			'current'
		]);
	});

	it('confirmed, being prepared, ready and completed follow the handover mode', () => {
		expect(orderListItem(order()).line).toBe('Collect at St Katharine Docks.');
		expect(orderListItem(order({ fulfillment_mode: 'customer_courier' })).line).toBe(
			'Your courier collects.'
		);
		expect(orderListItem(order({ fulfillment_mode: 'gk_delivery' })).line).toBe(
			'GK delivery confirmed by the chef.'
		);
		expect(row({ lifecycle: 'in_production' })).toEqual([
			'ok',
			'Being prepared',
			'view',
			'current'
		]);
		expect(orderListItem(order({ lifecycle: 'in_production' })).line).toContain(
			'about 48 hours before'
		);
		expect(row({ lifecycle: 'ready' })[1]).toBe('Ready to collect');
		expect(row({ lifecycle: 'ready', fulfillment_mode: 'gk_delivery' })[1]).toBe(
			'Ready for delivery'
		);
		expect(row({ lifecycle: 'completed' })).toEqual(['done', 'Collected', 'view', 'past']);
		expect(row({ lifecycle: 'completed', fulfillment_mode: 'gk_delivery' })[1]).toBe('Delivered');
		expect(row({ lifecycle: 'completed', fulfillment_mode: 'customer_courier' })[1]).toBe(
			'Handed to your courier'
		);
	});

	it('cancelled: checking payment, refunds, not paid, not confirmed, payment recorded', () => {
		expect(row({ lifecycle: 'cancelled', payment: pay('pending') })).toEqual([
			'wait',
			'Still checking payment',
			'view',
			'current'
		]);
		const refund = (state: string, succeeded = 0) => ({
			lifecycle: 'cancelled',
			payment: pay('succeeded'),
			refund: { state, target_minor: 4500, succeeded_minor: succeeded, currency: 'GBP' }
		});
		expect(row(refund('pending'))).toEqual([
			'bad',
			'Cancelled · refund pending',
			'view',
			'current'
		]);
		expect(orderListItem(order(refund('pending'))).line).toBe(
			'Refund of £45 pending: we’ve asked our payment provider to return it.'
		);
		expect(row(refund('requested'))[1]).toBe('Cancelled · refund requested');
		expect(row(refund('needs_attention'))[1]).toBe('Cancelled · refund being checked');
		expect(orderListItem(order(refund('partly_returned', 2000))).line).toBe(
			'£20 of £45 returned so far; the rest is still pending.'
		);
		expect(row(refund('returned', 4500))).toEqual(['done', 'Cancelled · refunded', 'view', 'past']);
		expect(orderListItem(order(refund('returned', 4500))).line).toBe(
			'Refund of £45 returned to your payment method.'
		);
		expect(row({ lifecycle: 'cancelled', cancel_reason: 'payment_not_completed' })).toEqual([
			'done',
			'Cancelled · not paid',
			'view',
			'past'
		]);
		expect(row({ lifecycle: 'cancelled', cancel_reason: 'free_not_confirmed' })[1]).toBe(
			'Cancelled · not confirmed'
		);
		expect(row({ lifecycle: 'cancelled', payment: pay('succeeded') })).toEqual([
			'wait',
			'Cancelled · payment recorded',
			'view',
			'current'
		]);
		expect(row({ lifecycle: 'cancelled' })).toEqual(['done', 'Cancelled', 'view', 'past']);
	});

	it('a refund on a live order is a second line and keeps it in progress', () => {
		const i = orderListItem(
			order({
				lifecycle: 'completed',
				refund: { state: 'pending', target_minor: 1000, succeeded_minor: 0 }
			})
		);
		expect([i.chip.text, i.group]).toEqual(['Collected', 'current']);
		expect(i.refund).toBe('Refund of £10 pending: we’ve asked our payment provider to return it.');
	});

	it('an unknown handover mode never claims collection', () => {
		const lines = ['confirmed', 'in_production', 'ready', 'completed'].map((lifecycle) =>
			orderListItem(order({ lifecycle, fulfillment_mode: 'teleport' }))
		);
		expect(lines.map((i) => i.chip.text)).toEqual([
			'Confirmed',
			'Being prepared',
			'Ready',
			'Completed'
		]);
		for (const i of lines) expect(i.line).not.toMatch(/St Katharine|collection details|courier/);
	});

	it('an unknown lifecycle or payment state enables nothing', () => {
		expect(row({ lifecycle: 'teleported' })).toEqual([
			'info',
			'Status not available',
			'view',
			'current'
		]);
		expect(row({ lifecycle: 'awaiting_payment', payment: pay('refunded_by_magic') })[1]).toBe(
			'Status not available'
		);
	});
});

describe('order list helpers', () => {
	it('names, references, modes and groups', () => {
		expect(orderName(order())).toBe('Pink piping');
		expect(
			orderName(
				order({
					items: [
						{ cake_name: 'A', flavour: 'x', size_code: 's', quantity: 2 },
						{ cake_name: 'B', flavour: 'y', size_code: 'm', quantity: 1 }
					]
				})
			)
		).toBe('A × 2, B');
		expect(orderName(order({ items: [] }))).toBe('Your order');
		expect(orderRef(order())).toBe('3f2a9c1b');
		expect(modeLabel('collection')).toBe('Collect · St Katharine Docks');
		expect(modeLabel('gk_delivery')).toBe('GK delivery');
		expect(modeLabel('teleport')).toBeUndefined();
		const g = groupOrders([
			order(),
			order({ id: 'b', lifecycle: 'completed' }),
			order({ id: 'c' })
		]);
		expect(g.current.map((r) => r.order.id)).toEqual([order().id, 'c']);
		expect(g.past.map((r) => r.order.id)).toEqual(['b']);
	});
});
