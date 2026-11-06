import { describe, expect, it } from 'vitest';
import type { StaffApi } from './catalogue';
import {
	attentionGroups,
	attentionList,
	groupOf,
	rowOf,
	titleOf,
	type AttentionItem
} from './attention';

const ORDER = 'a1b2c3d4-0000-4000-8000-000000000001';
const item = (over: Partial<AttentionItem>): AttentionItem => ({
	kind: 'job_attention',
	order_id: ORDER,
	collection_id: null,
	refund_operation_id: null,
	observation_id: null,
	job_id: null,
	reason: null,
	amount_minor: null,
	currency: null,
	window_ended: null,
	since: '2026-11-03T10:35:00Z',
	...over
});

describe('needs attention', () => {
	it('reads the list with the session', async () => {
		let url = '';
		let auth = '';
		const api: StaffApi = {
			baseUrl: 'https://api.test',
			token: 'tok',
			fetch: (async (u: string, init: RequestInit) => {
				url = u;
				auth = (init.headers as Record<string, string>).Authorization;
				return new Response(JSON.stringify({ items: [item({})] }), { status: 200 });
			}) as unknown as typeof fetch
		};
		expect(await attentionList(api)).toHaveLength(1);
		expect(url).toBe('https://api.test/api/v1/admin/attention');
		expect(auth).toBe('Bearer tok');
	});

	it('groups every kind the API lists as r03b2 does', () => {
		const g = (kind: string, window_ended: boolean | null = null) =>
			groupOf(item({ kind, window_ended }));
		expect(g('quarantined_observation')).toBe('money');
		expect(g('refund_contradiction')).toBe('money');
		expect(g('payment_contradiction')).toBe('money');
		expect(g('refund_failed_owed')).toBe('money');
		expect(g('refund_requires_action')).toBe('money');
		expect(g('refund_unknown', true)).toBe('money');
		expect(g('refund_unknown', false)).toBe('check');
		expect(g('delivery_pending')).toBe('todo');
		expect(g('manual_payout_reserved')).toBe('todo');
		expect(g('manual_payout_unknown')).toBe('todo');
		expect(g('job_attention')).toBe('tech');
		expect(g('something_new')).toBe('tech');
		expect(titleOf(item({ kind: 'something_new' }))).toBe('something new');
		expect(titleOf(item({ kind: 'refund_unknown', window_ended: true }))).toBe(
			'Refund result unknown · past its retry window'
		);
	});

	it('shows amounts only when the API sent them (the chef)', () => {
		const chef = rowOf(
			item({ kind: 'quarantined_observation', amount_minor: 2000, currency: 'GBP' }),
			'chef'
		);
		expect(chef).toMatchObject({
			title: 'Refund made outside GK Cakes',
			order: 'Order a1b2c3d4',
			orderId: ORDER,
			tone: 'bad'
		});
		expect(chef.lines[0]).toMatch(/^£20 refunded outside this workspace/);
		const support = rowOf(item({ kind: 'quarantined_observation' }), 'technical_support');
		expect(support.lines[0]).toMatch(/^A refund outside this workspace/);
		expect(support.lines.join(' ')).not.toMatch(/£/);
		const job = rowOf(
			item({ kind: 'job_attention', order_id: null, reason: 'provider key refused (401)' }),
			'technical_support'
		);
		expect(job).toMatchObject({ order: '', orderId: null, title: 'A background job stopped' });
		expect(job.lines[0]).toBe(
			'Stopped Tue 3 Nov, 10:35: “provider key refused (401)”. Nothing moved while it waits; Sasha fixes the cause, then requeues it.'
		);
		expect(
			rowOf(item({ kind: 'delivery_pending', amount_minor: 12000, currency: 'GBP' }), 'chef')
				.lines[0]
		).toMatch(/your separate step/);
		expect(rowOf(item({ kind: 'delivery_pending' }), 'technical_support').lines[0]).toMatch(
			/Elina’s separate step/
		);
	});

	it('orders the groups and leaves empty ones out', () => {
		const groups = attentionGroups(
			[
				item({ kind: 'job_attention' }),
				item({ kind: 'refund_unknown', window_ended: false, refund_operation_id: 'r1' }),
				item({ kind: 'refund_failed_owed', refund_operation_id: 'r2' }),
				item({ kind: 'refund_failed_owed', refund_operation_id: 'r3' })
			],
			'chef'
		);
		expect(groups.map((g) => [g.key, g.items.length])).toEqual([
			['money', 2],
			['check', 1],
			['tech', 1]
		]);
		expect(new Set(groups.flatMap((g) => g.items.map((i) => i.key))).size).toBe(4);
		expect(
			attentionGroups([item({ kind: 'delivery_pending' })], 'technical_support')[0]
		).toMatchObject({
			label: 'Elina’s open to-dos',
			note: 'Only Elina refunds money.'
		});
		expect(attentionGroups([], 'chef')).toEqual([]);
	});
});
