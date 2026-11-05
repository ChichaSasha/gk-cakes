<script lang="ts">
	// Bag (approved GK-016 r01c sketch): the chosen cakes checked against the
	// current catalogue, the chosen date and time, and the hand-off to
	// checkout. Each price is the server's current price; the total is worked
	// out by the server at checkout, never added up here.
	import { resolve } from '$app/paths';
	import { PUBLIC_API_URL } from '$app/env/public';
	import { onMount, tick } from 'svelte';
	import {
		dateLabel,
		formatMoney,
		modeLabel,
		sizeLabel,
		timeRange
	} from '../../../lib/checkout/format';
	import type { Cake, ReadyCake } from '../../../lib/checkout/types';
	import {
		checkoutHref,
		earliestCollectBy,
		placesNeeded,
		withoutCake,
		withWhen,
		type ChosenSlot,
		type PickerMode
	} from '../../../lib/shop/bag';
	import { bag } from '../../../lib/shop/bag.svelte';
	import { cakeOptions, servingsLabel, type CakeOption } from '../../../lib/shop/catalog';
	import { shopApi } from '../../../lib/shop/client';
	import { londonDate, shortDate } from '../../../lib/shop/picker';
	import CakePhoto from '../../../lib/shop/ui/CakePhoto.svelte';
	import DatePicker from '../../../lib/shop/ui/DatePicker.svelte';
	import { londonToday } from '../../../lib/checkout/format';

	interface Line {
		option: CakeOption;
		cake: Cake;
	}

	const api = shopApi(PUBLIC_API_URL);
	let picker = $state<DatePicker>();
	let phase = $state<'loading' | 'ready' | 'error'>('loading');
	let cakes = $state<Cake[]>([]);
	let ready = $state<ReadyCake[]>([]);
	let removedCount = $state(0);
	let heading = $state<HTMLHeadingElement>();

	const lines = $derived.by<Line[]>(() => {
		const byKey: Record<string, Line> = {};
		for (const cake of cakes) {
			for (const option of cakeOptions(cake, ready)) {
				if (option.purchasable) byKey[option.key] = { option, cake };
			}
		}
		return bag.state.keys.map((k) => byKey[k]).filter((l): l is Line => l !== undefined);
	});
	const today = londonToday();
	const bagDate = $derived(bag.state.date && bag.state.date >= today ? bag.state.date : undefined);
	const collectBy = $derived(earliestCollectBy(bag.state.keys, ready));
	const tooLate = $derived(!!bagDate && !!collectBy && bagDate > londonDate(collectBy));

	async function load() {
		phase = 'loading';
		try {
			[cakes, ready] = await Promise.all([api.cakes(), api.readyCakes()]);
			phase = 'ready';
			// Drop cakes that can no longer be bought (archived, sold out or
			// without an allergen declaration), and say so.
			const known = new Set(lines.map((l) => l.option.key));
			const gone = bag.state.keys.filter((k) => !known.has(k));
			if (gone.length > 0) {
				let next = bag.state;
				for (const k of gone) next = withoutCake(next, k);
				bag.set(next);
				removedCount = gone.length;
			}
		} catch {
			phase = 'error';
		}
	}

	onMount(() => {
		bag.load();
		void load();
	});

	async function remove(key: string, index: number) {
		bag.set(withoutCake(bag.state, key));
		removedCount = 0;
		// Keep focus in the list: the next row's remove button, else the heading.
		await tick();
		const buttons = document.querySelectorAll<HTMLButtonElement>('[data-remove]');
		const target = buttons[Math.min(index, buttons.length - 1)];
		if (target) target.focus();
		else heading?.focus();
	}

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
</script>

<svelte:head>
	<title>Your bag · GK Cake London</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="wrap">
	<div class="page-title">
		<h1 tabindex="-1" bind:this={heading}>Your bag</h1>
		<p>
			{bag.state.keys.length > 0
				? 'A celebration in the making.'
				: 'A little room for something special.'}
		</p>
	</div>

	{#if !bag.kept}
		<div class="notice error" role="alert">
			<div><p>This browser can’t keep your bag between pages. Please allow site storage.</p></div>
		</div>
	{/if}
	{#if removedCount > 0}
		<div class="notice" role="status">
			<div>
				<p>
					{removedCount === 1 ? 'One cake' : `${removedCount} cakes`} in your bag can no longer be bought,
					so {removedCount === 1 ? 'it was' : 'they were'} removed.
				</p>
			</div>
		</div>
	{/if}

	{#if bag.state.keys.length === 0 && phase !== 'loading'}
		<div class="empty">
			<h2>Your bag is waiting</h2>
			<p>Explore the cakes, choose a date and find your favourite.</p>
			<a class="button primary" href={resolve('/(shop)/shop')}
				>Explore the cakes <span class="arrow" aria-hidden="true">→</span></a
			>
		</div>
	{:else if phase === 'loading'}
		<p role="status" aria-busy="true">Loading your bag…</p>
	{:else if phase === 'error'}
		<div class="empty" role="alert">
			<h2>We couldn’t load your bag</h2>
			<p>Your cakes are kept. Please check your connection and try again.</p>
			<button class="btn primary" type="button" onclick={() => void load()}>Try again</button>
		</div>
	{:else}
		<div class="bag-layout">
			<div>
				<ul class="bag-list">
					{#each lines as l, i (l.option.key)}
						<li class="bag-row">
							<div class="bag-photo"><CakePhoto name={l.cake.name} decorative /></div>
							<div>
								<h2>
									<a href={resolve('/(shop)/cakes/[id]', { id: l.cake.id })}>{l.cake.name}</a>
								</h2>
								<p>
									{l.option.flavour}{l.option.recipe
										? ` · ${servingsLabel(l.option.recipe.servings)}`
										: ''}
								</p>
								<p>
									Quantity: 1 · {sizeLabel(l.option.sizeCode)}{l.option.kind === 'ready'
										? ` · Ready-made, collect by ${shortDate(londonDate(l.option.collectBy as string))}`
										: ''}
								</p>
								<button
									class="text-link"
									type="button"
									data-remove
									onclick={() => void remove(l.option.key, i)}>Remove {l.cake.name}</button
								>
							</div>
							<span class="line-price"
								>{formatMoney(l.option.priceMinor as number, l.option.currency)}</span
							>
						</li>
					{/each}
				</ul>
				<a class="text-link" href={resolve('/(shop)/shop')}>← Continue shopping</a>
			</div>
			<aside class="side-card" aria-labelledby="celebration-title">
				<h2 id="celebration-title">Your celebration</h2>
				<p class="small">
					{bagDate
						? `${dateLabel(bagDate)}${bag.state.slot ? ` · ${timeRange(bag.state.slot.starts_at, bag.state.slot.ends_at)}` : ''} · London time`
						: 'Choose your date and time.'}
				</p>
				<button class="text-link" type="button" onclick={openPicker}>
					{bagDate ? 'Change date →' : 'Choose date →'}
				</button>
				{#if tooLate && bagDate && collectBy}
					<div class="notice error" role="status">
						<div>
							<p>
								Your date is after a ready-made cake’s collect-by date ({shortDate(
									londonDate(collectBy)
								)}). Please choose an earlier date.
							</p>
						</div>
					</div>
				{/if}
				<dl>
					<div>
						<dt>Cakes</dt>
						<dd>{lines.length}</dd>
					</div>
					<div>
						<dt>How you’ll get it</dt>
						<dd>
							{bag.state.mode === 'collection' ? 'Collect · Free' : modeLabel(bag.state.mode)}
						</dd>
					</div>
				</dl>
				<p class="small">
					Your total is worked out at checkout from the chef’s current prices. Decoration shown
					included. GK delivery is separately quoted and paid; it is not included in the cake total.
				</p>
				<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- checkout URL with the bag's query -->
				<a class="button primary" href={checkoutHref(bag.state, today)}
					>Review checkout <span class="arrow" aria-hidden="true">→</span></a
				>
				<p class="small">Nothing is reserved until you pay.</p>
			</aside>
		</div>
		<DatePicker
			bind:this={picker}
			{api}
			places={placesNeeded(bag.state.keys)}
			{collectBy}
			onchoose={chooseWhen}
		/>
	{/if}
</div>
