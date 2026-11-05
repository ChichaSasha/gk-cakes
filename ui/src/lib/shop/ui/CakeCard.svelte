<script lang="ts">
	// One shop card (approved GK-016 r01c sketch): photo, name, the server's
	// price ("From" when sizes differ), servings, how it is made and any
	// prominent allergen exactly as declared, and the favourites heart.
	import { resolve } from '$app/paths';
	import { formatMoney } from '../../checkout/format';
	import { servingsLabel, type Listing } from '../catalog';
	import CakePhoto from './CakePhoto.svelte';
	import SaveButton from './SaveButton.svelte';

	let {
		item,
		index,
		heading = 'h2',
		onremove
	}: {
		item: Listing;
		index: number;
		heading?: 'h2' | 'h3';
		/** Called after the heart removes this cake from favourites. */
		onremove?: (button: HTMLButtonElement) => void;
	} = $props();

	const price = $derived(
		item.fromMinor === null
			? 'Not available'
			: `${item.several ? 'From ' : ''}${formatMoney(item.fromMinor, item.currency)}`
	);
</script>

<li class="product">
	<a href={resolve('/(shop)/cakes/[id]', { id: item.id })}>
		<div class="product-photo">
			<span class="product-index" aria-hidden="true">{String(index + 1).padStart(2, '0')}</span>
			<CakePhoto name={item.name} decorative />
		</div>
		<div class="product-text">
			<svelte:element this={heading}>{item.name}</svelte:element>
			<span class="price">{price}</span>
		</div>
		<p class="meta">
			{#if item.status === 'unavailable'}
				<span class="status-dot off" aria-hidden="true"></span><span>Not available right now</span>
			{:else}
				<span class="status-dot" aria-hidden="true"></span>
				<span
					>{item.servings ? `${servingsLabel(item.servings)} · ` : ''}{item.status === 'ready'
						? 'Ready-made'
						: item.hasReady
							? 'Made to order · Ready-made'
							: 'Made to order'}</span
				>
				{#each item.prominent as label (label)}
					<strong class="peanut-tag">Contains {label}</strong>
				{/each}
			{/if}
		</p>
	</a>
	<SaveButton id={item.id} name={item.name} {onremove} />
</li>
