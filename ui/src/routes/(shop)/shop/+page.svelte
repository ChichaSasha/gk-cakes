<script lang="ts">
	// Shop (approved GK-016 r01c sketch): every active cake from the public
	// catalogue with selection, flavour and sort controls; loading, empty,
	// no-match and error states with recovery.
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { PUBLIC_API_URL } from '$app/env/public';
	import { page } from '$app/state';
	import { tick } from 'svelte';
	import { dateLabel, londonToday, timeRange } from '../../../lib/checkout/format';
	import {
		currentDate,
		earliestCollectBy,
		placesNeeded,
		withWhen,
		type ChosenSlot,
		type PickerMode
	} from '../../../lib/shop/bag';
	import { bag } from '../../../lib/shop/bag.svelte';
	import {
		filterListings,
		flavoursOf,
		isSelection,
		type Selection,
		type Sort
	} from '../../../lib/shop/catalog';
	import { shopApi } from '../../../lib/shop/client';
	import CakeCard from '../../../lib/shop/ui/CakeCard.svelte';
	import DatePicker from '../../../lib/shop/ui/DatePicker.svelte';
	import Icon from '../../../lib/shop/ui/Icon.svelte';
	import LoadProblem from '../../../lib/shop/ui/LoadProblem.svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const fromUrl = () => {
		const s = page.url.searchParams.get('selection');
		return isSelection(s) ? s : 'all';
	};
	// Follows the URL (the footer's Order online and Ready-made links) and the control.
	let selection = $derived<Selection>(fromUrl());
	let flavour = $state('all');
	let sort = $state<Sort>('featured');

	const all = $derived(data.listings.ok ? data.listings.data.listings : []);
	const ready = $derived(data.listings.ok ? data.listings.data.ready : []);
	const flavours = $derived(flavoursOf(all));
	const shown = $derived(filterListings(all, { selection, flavour, sort }));

	function keepSelectionInUrl() {
		const url = new URL(page.url.href);
		if (selection === 'all') url.searchParams.delete('selection');
		else url.searchParams.set('selection', selection);
		// eslint-disable-next-line svelte/no-navigation-without-resolve -- same page, query only
		void goto(url, { replace: true, shallow: true });
	}

	// "Start with your celebration date" (approved sketch): the shared picker,
	// for one place when the bag is empty.
	const api = shopApi(PUBLIC_API_URL);
	let picker = $state<DatePicker>();
	const today = londonToday();
	const bagDate = $derived(currentDate(bag.state, today));
	const places = $derived(bag.state.keys.length > 0 ? placesNeeded(bag.state.keys) : 1);

	function openPicker() {
		void picker?.show({
			date: bagDate,
			mode: bag.state.mode,
			slot: bagDate ? bag.state.slot : undefined
		});
	}

	function chooseWhen(date: string, mode: PickerMode, slot: ChosenSlot) {
		bag.set(withWhen(bag.state, date, mode, slot));
	}

	let selectionControl = $state<HTMLSelectElement>();

	// The Clear filters button goes away with the no-match message, so focus
	// moves to the first filter.
	async function clear() {
		selection = 'all';
		flavour = 'all';
		sort = 'featured';
		keepSelectionInUrl();
		await tick();
		selectionControl?.focus();
	}
</script>

<svelte:head>
	<title>Shop cakes · GK Cake London</title>
	<meta name="description" content="Handcrafted cakes from GK Cake London, made to celebrate." />
</svelte:head>

<div class="wrap">
	<p class="sr-only" role="status">
		{data.listings.ok && all.length > 0
			? `${shown.length} ${shown.length === 1 ? 'cake' : 'cakes'} shown`
			: ''}
	</p>
	<div class="page-title">
		<h1>Shop cakes</h1>
		<p>Handcrafted cakes, made to celebrate.</p>
	</div>

	{#if !data.listings.ok}
		<LoadProblem />
	{:else if all.length === 0}
		<div class="empty">
			<h2>No cakes to show yet</h2>
			<p>The chef is preparing the collection. Please come back soon.</p>
		</div>
	{:else}
		<div class="shop-controls">
			<label>
				<span class="filter-label">Cake selection</span>
				<select bind:this={selectionControl} bind:value={selection} onchange={keepSelectionInUrl}>
					<option value="all">All cakes</option>
					<option value="orderable">Order online</option>
					<option value="ready">Ready-made</option>
					<option value="unavailable">Not available right now</option>
				</select>
			</label>
			<label>
				<span class="filter-label">Flavour</span>
				<select bind:value={flavour}>
					<option value="all">All flavours</option>
					{#each flavours as f (f)}<option value={f}>{f}</option>{/each}
				</select>
			</label>
			<label>
				<span class="filter-label">Sort by</span>
				<select bind:value={sort}>
					<option value="featured">Featured</option>
					<option value="price">Price: low to high</option>
					<option value="name">Name: A–Z</option>
				</select>
			</label>
		</div>

		<div class="date-prompt">
			<p>
				<Icon name="calendar" />
				<span
					>{bagDate
						? `${dateLabel(bagDate)}${bag.state.slot ? ` · ${timeRange(bag.state.slot.starts_at, bag.state.slot.ends_at)}` : ''}`
						: 'Start with your celebration date.'}</span
				>
			</p>
			<button class="btn primary" type="button" onclick={openPicker}
				>{bagDate ? 'Change date →' : 'Choose date →'}</button
			>
		</div>
		<DatePicker
			bind:this={picker}
			{api}
			{places}
			collectBy={earliestCollectBy(bag.state.keys, ready)}
			onchoose={chooseWhen}
		/>

		{#if shown.length > 0}
			<ul class="grid">
				{#each shown as item, i (item.id)}
					<CakeCard {item} index={i} />
				{/each}
			</ul>
		{:else}
			<div class="empty">
				<h2>No cakes match those filters.</h2>
				<p>Try another combination, or see the sizes and starting prices.</p>
				<button class="btn" type="button" onclick={clear}>Clear filters</button>
			</div>
		{/if}
	{/if}

	<p class="catalog-note">
		Prices are the chef’s current prices; your total is checked again at checkout. The decoration
		shown is included. <a href={resolve('/(shop)/sizes')}>Sizes and starting prices</a>
	</p>
</div>
