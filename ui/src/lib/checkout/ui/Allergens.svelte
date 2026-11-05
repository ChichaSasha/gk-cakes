<script lang="ts">
	// Allergens per cake before paying (K01, GK-017 r01): the declaration of
	// each cake's current recipe, prominent allergens called out, then the
	// shared-kitchen notice (shown once per distinct text).
	import type { BasketItem } from '../flow';

	let { items }: { items: BasketItem[] } = $props();

	const notices = $derived([...new Set(items.map((i) => i.notice).filter(Boolean))]);
</script>

{#each items as item (item.id)}
	<div class="co-box">
		<p class="co-label">{item.cakeName} · allergens</p>
		{#each item.allergens.filter((a) => a.prominent) as a (a.code)}
			<p class="allergen-alert">Contains {a.label}</p>
		{/each}
		<p><strong>Contains: {item.allergens.map((a) => a.label).join(', ')}.</strong></p>
		{#if item.composition.length > 0 || item.coating}
			<p class="small">
				{[...item.composition, item.coating].filter(Boolean).join('. ')}.
			</p>
		{/if}
	</div>
{/each}
{#each notices as notice (notice)}
	<p class="cross-contact">{notice}</p>
{/each}
