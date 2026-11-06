<script lang="ts">
	// One order in the account list (approved GK-016 r02b sketch): reference,
	// cakes, date and handover, the status chip with its line (words plus a
	// dot, never colour alone), the server's total and one action. Paying and
	// confirming happen on the order page, which reads the order again.
	import { resolve } from '$app/paths';
	import { dateLabel, formatMoney, timeRange } from '../../checkout/format';
	import Icon from '../../shop/ui/Icon.svelte';
	import type { AccountOrder } from '../client';
	import { modeLabel, orderName, orderRef, type OrderListItem } from '../orders';

	let { order, item }: { order: AccountOrder; item: OrderListItem } = $props();

	const ref = $derived(orderRef(order));
	const when = $derived.by(() => {
		let text = /^\d{4}-\d{2}-\d{2}$/.test(order.date) ? dateLabel(order.date) : order.date;
		if (order.slot) text += ` · ${timeRange(order.slot.starts_at, order.slot.ends_at)}`;
		if (item.requested) text += ' (requested, not confirmed)';
		return `${text} · ${modeLabel(order.fulfillment_mode)}`;
	});
	const amount = $derived(
		order.payment_requirement === 'none' && order.total_minor === 0
			? 'Free'
			: formatMoney(order.total_minor, order.currency)
	);
	const href = $derived(resolve('/orders/[id]', { id: order.id }));
	const primary = $derived(item.action !== 'view');
</script>

<article class="acc-order">
	<span class="acc-tile" aria-hidden="true"><Icon name="cake" /></span>
	<div class="acc-order-body">
		<p class="acc-ref">
			Order {ref}{#if order.added_by === 'chef'}&nbsp;· added by the chef{/if}
		</p>
		<h3>{orderName(order)}</h3>
		<p class="small">{when}</p>
		<p><span class="acc-chip {item.chip.tone}">{item.chip.text}</span></p>
		<p class="small">{item.line}</p>
		{#if item.refund && item.refund !== item.line}<p class="small">{item.refund}</p>{/if}
	</div>
	<div class="acc-order-side">
		<p class="acc-amount">{amount}</p>
		<a
			class="button acc-small-btn"
			class:primary
			{href}
			aria-label={`${item.actionLabel}, order ${ref}`}>{item.actionLabel}</a
		>
	</div>
</article>
