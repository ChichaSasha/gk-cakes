<script lang="ts">
	// Your favourites (approved GK-016 r01c sketch): the cakes saved with the
	// heart on this device, as current shop cards (today's prices and
	// allergens from the server). Saved cakes no longer in the collection are
	// not shown. Adding favourites to an account needs the account pages and
	// API (K02) and is not offered yet.
	import { resolve } from '$app/paths';
	import { tick } from 'svelte';
	import { dateLabel } from '../../../lib/checkout/format';
	import { focusAfterRemoval, savedListings } from '../../../lib/shop/favourites';
	import { favourites } from '../../../lib/shop/favourites.svelte';
	import { londonDate } from '../../../lib/shop/picker';
	import CakeCard from '../../../lib/shop/ui/CakeCard.svelte';
	import LoadProblem from '../../../lib/shop/ui/LoadProblem.svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();
	let grid = $state<HTMLUListElement>();
	let heading = $state<HTMLHeadingElement>();

	const saved = $derived(favourites.state.ids);
	const items = $derived(data.listings.ok ? savedListings(saved, data.listings.data.listings) : []);
	const until = $derived(
		favourites.state.expires ? dateLabel(londonDate(favourites.state.expires)) : ''
	);

	// The removed card goes away: focus moves to the next heart (or the new
	// last one), or to the page heading when none are left (as the sketch).
	async function removed(button: HTMLButtonElement) {
		const before = [...(grid?.querySelectorAll<HTMLButtonElement>('.save') ?? [])];
		const target = focusAfterRemoval(before.indexOf(button), before.length);
		await tick();
		const rest = grid?.querySelectorAll<HTMLButtonElement>('.save');
		const next = target === null ? undefined : rest?.[target];
		if (next) next.focus();
		else heading?.focus();
	}
</script>

<svelte:head>
	<title>Your favourites · GK Cake London</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="wrap">
	<div class="page-title">
		<h1 tabindex="-1" bind:this={heading}>Your favourites</h1>
		<p>A little collection of cakes you love.</p>
	</div>

	{#if !favourites.loaded}
		<p role="status" aria-busy="true">Loading your favourites…</p>
	{:else if saved.length > 0 && !data.listings.ok}
		<LoadProblem what="your favourites" />
	{:else if items.length > 0}
		<ul class="grid" bind:this={grid}>
			{#each items as item, i (item.id)}
				<CakeCard {item} index={i} onremove={removed} />
			{/each}
		</ul>
		<p class="catalog-note">
			Saved on this device for a year (until {until}). Check today’s details before ordering.
		</p>
	{:else}
		<div class="empty">
			<h2>Find something you love</h2>
			<p>Tap the heart on a cake to save it on this device.</p>
			{#if !favourites.kept}
				<p>This browser cannot keep things on this device, so favourites can’t be saved here.</p>
			{/if}
			<a class="button primary" href={resolve('/(shop)/shop')}
				>Explore the cakes <span class="arrow" aria-hidden="true">→</span></a
			>
		</div>
	{/if}
</div>
