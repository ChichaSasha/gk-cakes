// Needs attention (Creator-approved GK-018 r03b2 support.html, "Needs
// attention"): GET /api/v1/admin/attention (api/README "Manager recovery
// tools", 0025/0027), a read-only queue of the money cases and stopped jobs
// the API lists, each linking to its order. The chef sees amounts; technical
// support the same items without amounts (the API leaves them null and
// audits the read); fulfilment has no access (the page doesn't ask).
import { apiGet } from '../api';
import { formatMoney } from '../checkout/format';
import type { StaffApi } from './catalogue';
import { when } from './order-detail';
import type { StaffView } from './orders';

export interface AttentionItem {
	kind: string;
	order_id: string | null;
	collection_id: string | null;
	refund_operation_id: string | null;
	observation_id: string | null;
	job_id: string | null;
	reason: string | null;
	/** Chef only (null for technical support). */
	amount_minor: number | null;
	currency: string | null;
	window_ended: boolean | null;
	since: string;
}

/** The attention list (chef or technical support). */
export async function attentionList(api: StaffApi): Promise<AttentionItem[]> {
	const r = await apiGet<{ items: AttentionItem[] }>('/api/v1/admin/attention', {
		baseUrl: api.baseUrl,
		token: api.token,
		fetch: api.fetch
	});
	return r.items ?? [];
}

export type GroupKey = 'money' | 'check' | 'todo' | 'tech';

export interface AttentionGroup {
	key: GroupKey;
	label: string;
	note: string;
	items: AttentionRow[];
}

export interface AttentionRow {
	key: string;
	title: string;
	/** "Order 1a2b3c4d", or '' when not tied to an order. */
	order: string;
	orderId: string | null;
	lines: string[];
	tone: 'bad' | 'info' | 'warn';
}

const TITLES: Record<string, string> = {
	quarantined_observation: 'Refund made outside GK Cakes',
	refund_contradiction: 'Refund changed after it was confirmed',
	payment_contradiction: 'Payment doesn’t match the order',
	refund_failed_owed: 'Refund failed · won’t be resent automatically',
	refund_requires_action: 'Refund needs an action at the provider',
	delivery_pending: 'Delivery refund still owed',
	manual_payout_reserved: 'Verified transfer reserved · not sent yet',
	manual_payout_pending: 'Verified transfer sent · not confirmed',
	manual_payout_unknown: 'Verified transfer result unknown',
	job_attention: 'A background job stopped'
};

/** Which group an item belongs to (r03b2's groups for the kinds the API lists). */
export function groupOf(item: AttentionItem): GroupKey {
	switch (item.kind) {
		case 'quarantined_observation':
		case 'refund_contradiction':
		case 'payment_contradiction':
		case 'refund_failed_owed':
		case 'refund_requires_action':
			return 'money';
		case 'refund_unknown':
			return item.window_ended ? 'money' : 'check';
		case 'delivery_pending':
		case 'manual_payout_reserved':
		case 'manual_payout_pending':
		case 'manual_payout_unknown':
			return 'todo';
		default:
			return 'tech';
	}
}

/** An item's title (unknown kinds keep their name, so nothing is hidden). */
export function titleOf(item: AttentionItem): string {
	if (item.kind === 'refund_unknown') {
		return item.window_ended
			? 'Refund result unknown · past its retry window'
			: 'Refund result not confirmed';
	}
	return TITLES[item.kind] ?? item.kind.replace(/_/g, ' ');
}

/** What the item says, in r03b2's words; amounts only when the API sent them (chef). */
export function rowOf(item: AttentionItem, view: StaffView): AttentionRow {
	const amount =
		item.amount_minor != null ? formatMoney(item.amount_minor, item.currency ?? 'GBP') : '';
	const lines: string[] = [];
	const chef = view === 'chef';
	switch (item.kind) {
		case 'quarantined_observation':
			lines.push(
				`${amount ? `${amount} refunded` : 'A refund'} outside this workspace (for example in the payment provider’s dashboard), seen ${when(item.since)}. It counts as possibly returned and blocks refunds from the workspace for that payment until it’s matched or explained.`
			);
			break;
		case 'refund_contradiction':
			lines.push(
				`${amount ? `The ${amount} refund’s` : 'A refund’s'} recorded result no longer matches the payment provider (since ${when(item.since)}). Nothing is resent and nothing becomes owed automatically; no new refund for that payment until a manager reviews it.`
			);
			break;
		case 'payment_contradiction':
			lines.push(
				`${amount ? `The ${amount} payment’s` : 'A payment’s'} evidence doesn’t match the order (since ${when(item.since)}). It isn’t accepted on that evidence; a manager reconciles it.`
			);
			break;
		case 'refund_failed_owed':
			lines.push(
				`The payment provider definitively refused a refund (${when(item.since)}).${amount ? ` ${amount} can be reserved again.` : ''} It stays owed: a manager either reserves the remainder again for one new attempt or arranges a verified transfer with evidence (K05 Q5). Never both, never automatically.`
			);
			break;
		case 'refund_requires_action':
			lines.push(
				`${amount ? `The ${amount} refund` : 'A refund'} is waiting for an action the payment provider requires (since ${when(item.since)}). It counts as in progress: no other refund for that payment until it’s resolved.`
			);
			break;
		case 'refund_unknown':
			lines.push(
				item.window_ended
					? `${amount ? `The ${amount} refund’s` : 'A refund’s'} result is still unknown after its 23-hour retry window (since ${when(item.since)}). A manager decides it on the order, with the evidence the system requires.`
					: `${amount ? `${amount} sent` : 'A refund was sent'} to the payment provider; its result isn’t confirmed yet (last change ${when(item.since)}). Nobody can send another refund while it may still succeed; it never becomes owed again just because time passed. If it’s still unknown when its retry window ends, it goes to a manager.`
			);
			break;
		case 'delivery_pending':
			lines.push(
				`${amount ? `${amount} delivery refund` : 'A delivery refund'} is ${chef ? 'your' : 'Elina’s'} separate step after a cancellation or refund (since ${when(item.since)}).`
			);
			break;
		case 'manual_payout_reserved':
		case 'manual_payout_pending':
		case 'manual_payout_unknown':
			lines.push(
				`${amount ? `${amount} ` : ''}verified transfer, ${item.kind === 'manual_payout_reserved' ? 'reserved: make the transfer and record it on the order' : item.kind === 'manual_payout_pending' ? 'sent: record the result once it’s confirmed or fails' : 'result unknown: nothing else can be paid on that payment until it’s recorded'} (since ${when(item.since)}).`
			);
			break;
		default:
			lines.push(
				`Stopped ${when(item.since)}${item.reason ? `: “${item.reason}”` : ''}. Nothing moved while it waits; Sasha fixes the cause, then requeues it.`
			);
	}
	const tone: AttentionRow['tone'] =
		groupOf(item) === 'money' ? 'bad' : groupOf(item) === 'check' ? 'info' : 'warn';
	return {
		key: [
			item.kind,
			item.order_id,
			item.observation_id,
			item.refund_operation_id,
			item.job_id,
			item.collection_id
		]
			.map((x) => x ?? '')
			.join(':'),
		title: titleOf(item),
		order: item.order_id ? `Order ${item.order_id.slice(0, 8)}` : '',
		orderId: item.order_id,
		lines,
		tone
	};
}

/** The queue's groups in r03b2's order, empty groups left out. */
export function attentionGroups(items: AttentionItem[], view: StaffView): AttentionGroup[] {
	const chef = view === 'chef';
	const GROUPS: [GroupKey, string, string][] = [
		[
			'money',
			'Money · needs a manager decision',
			'Raised automatically when a payment or refund can’t be settled safely. Only one thing starts by itself: the full return of money that arrived for an order already released (cancelled). Nothing else is sent, refunded or confirmed automatically.'
		],
		[
			'check',
			'Still checking with the provider',
			'Results we don’t know yet. Most settle when the provider answers. A refund still unknown after its 23-hour retry window goes to a manager.'
		],
		[
			'todo',
			chef ? 'Your open to-dos' : 'Elina’s open to-dos',
			chef ? '' : 'Only Elina refunds money.'
		],
		[
			'tech',
			'Technical (Sasha)',
			'Jobs stopped for setup reasons: no money decision, nothing moved. Sasha fixes the cause, then requeues.'
		]
	];
	return GROUPS.map(([key, label, note]) => ({
		key,
		label,
		note,
		items: items.filter((i) => groupOf(i) === key).map((i) => rowOf(i, view))
	})).filter((g) => g.items.length > 0);
}
