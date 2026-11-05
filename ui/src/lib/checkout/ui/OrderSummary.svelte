<script lang="ts">
	// The order summary (GK-017 r01): cakes, appointment and the server's
	// quote totals. Nothing here adds up money; before a quote exists the
	// totals say how to get one.
	import type { Snippet } from 'svelte';
	import { formatMoney, sizeLabel } from '../format';
	import type { BasketItem } from '../flow';
	import type { Quote } from '../types';

	interface Props {
		items: BasketItem[];
		quote: Quote | null;
		quoteLoading: boolean;
		dateText: string;
		timeText: string;
		modeText: string;
		fulfilLabel: string;
		fulfilValue: string;
		children?: Snippet;
	}

	let {
		items,
		quote,
		quoteLoading,
		dateText,
		timeText,
		modeText,
		fulfilLabel,
		fulfilValue,
		children
	}: Props = $props();

	function discountLabel(q: Quote): string {
		const d = q.discount;
		if (!d) return 'Discount';
		return d.kind === 'percentage' && d.basis_points
			? `Discount · ${d.code} (${d.basis_points / 100}%)`
			: `Discount · ${d.code}`;
	}
</script>

<aside class="co-summary" aria-labelledby="sum-title">
	<h2 id="sum-title">Your order</h2>
	<ul class="sum-items">
		{#if quote}
			{#each quote.lines as line (line.line_no)}
				<li class="sum-item">
					<div>
						<strong>{line.cake_name}</strong>
						<span
							>{line.flavour} · {sizeLabel(line.size_code)}{line.lot_id
								? ' · ready-made'
								: ''}</span
						>
					</div>
					<span class="sum-price"
						>{line.quantity > 1 ? `${line.quantity} × ` : ''}{formatMoney(
							line.unit_price_minor,
							quote.currency
						)}</span
					>
				</li>
			{/each}
		{:else}
			{#each items as item (item.id)}
				<li class="sum-item">
					<div>
						<strong>{item.cakeName}</strong>
						<span
							>{[item.flavour, item.size, item.servings, item.ready ? 'ready-made' : '']
								.filter(Boolean)
								.join(' · ')}</span
						>
					</div>
					{#if item.quantity > 1}<span class="sum-price">× {item.quantity}</span>{/if}
				</li>
			{/each}
		{/if}
	</ul>
	<dl class="sum-meta">
		<div>
			<dt>Date</dt>
			<dd>{dateText}</dd>
		</div>
		<div>
			<dt>Time</dt>
			<dd>{timeText}</dd>
		</div>
		<div>
			<dt>How you’ll get it</dt>
			<dd>{modeText}</dd>
		</div>
	</dl>
	<div aria-live="polite" aria-busy={quoteLoading}>
		{#if quote}
			<div class="totals">
				<span>Cakes</span><span>{formatMoney(quote.subtotal_minor, quote.currency)}</span>
			</div>
			{#if quote.discount_minor > 0}
				<div class="totals">
					<span>{discountLabel(quote)}</span><span
						>−{formatMoney(quote.discount_minor, quote.currency)}</span
					>
				</div>
			{/if}
			<div class="totals"><span>{fulfilLabel}</span><span>{fulfilValue}</span></div>
			<div class="totals total" id="sum-total">
				<span>To pay now</span><span>{formatMoney(quote.total_minor, quote.currency)}</span>
			</div>
		{:else}
			<p class="small">
				{quoteLoading ? 'Updating your total…' : 'Choose a date and time to see your total.'}
			</p>
		{/if}
	</div>
	<p class="small">In pounds (GBP). Decoration shown is included.</p>
	<ul class="co-assure">
		<li>
			Your place on this date is held for 15 minutes while you pay. If your payment is still
			processing after that, we’ll wait for the result.
		</li>
		<li>
			{quote?.total_minor === 0
				? 'Your order is confirmed only when you confirm it on the next page.'
				: 'Your order is confirmed only when we receive the payment.'}
		</li>
		<li>If a payment result is unclear, we check it for you. Please don’t pay twice.</li>
	</ul>
	{@render children?.()}
</aside>
