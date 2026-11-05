<script lang="ts">
	// Cake page (approved GK-016 r01c sketch): the chef's sizes and prices for
	// this cake, servings, the exact allergen declaration of the current recipe
	// version (prominent allergens called out, K01), ready-made cakes with the
	// chef's collect-by date, and the date-and-time picker before adding to
	// the bag. Missing disclosure disables buying (K01); a cake that is not
	// active (archived or never published) is a 404 (load and +error.svelte).
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { PUBLIC_API_URL } from '$app/env/public';
	import { onMount } from 'svelte';
	import {
		dateLabel,
		deadlineLabel,
		formatMoney,
		londonToday,
		sizeLabel,
		timeRange
	} from '../../../../lib/checkout/format';
	import {
		earliestCollectBy,
		MAX_BAG,
		placesNeeded,
		withCake,
		withWhen,
		type ChosenSlot,
		type PickerMode
	} from '../../../../lib/shop/bag';
	import { bag } from '../../../../lib/shop/bag.svelte';
	import {
		cakeOptions,
		containsLine,
		servingsLabel,
		type CakeOption
	} from '../../../../lib/shop/catalog';
	import { shopApi } from '../../../../lib/shop/client';
	import { londonDate, shortDate } from '../../../../lib/shop/picker';
	import CakePhoto from '../../../../lib/shop/ui/CakePhoto.svelte';
	import DatePicker from '../../../../lib/shop/ui/DatePicker.svelte';
	import Icon from '../../../../lib/shop/ui/Icon.svelte';
	import LoadProblem from '../../../../lib/shop/ui/LoadProblem.svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const api = shopApi(PUBLIC_API_URL);
	let picker = $state<DatePicker>();

	const cake = $derived(data.result.ok ? data.result.data.cake : undefined);
	const ready = $derived(data.result.ok ? data.result.data.ready : []);
	const options = $derived(cake ? cakeOptions(cake, ready) : []);
	let chosenKey = $state<string | undefined>();
	const option = $derived<CakeOption | undefined>(
		options.find((o) => o.key === chosenKey) ?? options.find((o) => o.purchasable) ?? options[0]
	);
	const allergens = $derived(option?.purchasable ? (option.recipe?.allergens ?? []) : []);
	const prominent = $derived(allergens.filter((a) => a.prominent));

	const today = londonToday();
	const bagDate = $derived(bag.state.date && bag.state.date >= today ? bag.state.date : undefined);
	const keysWithThis = $derived(
		option && !bag.state.keys.includes(option.key)
			? [...bag.state.keys, option.key]
			: bag.state.keys
	);
	const collectBy = $derived(earliestCollectBy(keysWithThis, ready));
	const tooLate = $derived(
		!!bagDate && !!option?.collectBy && bagDate > londonDate(option.collectBy)
	);
	const bagFull = $derived(
		!!option && !bag.state.keys.includes(option.key) && bag.state.keys.length >= MAX_BAG
	);

	onMount(() => bag.load());

	function optionLabel(o: CakeOption, flavours: number): string {
		const parts: string[] = [];
		if (o.kind === 'ready') parts.push('Ready-made');
		if (flavours > 1) parts.push(o.flavour);
		const size = sizeLabel(o.sizeCode).replace(' inches', ' in');
		parts.push(size);
		if (o.recipe) parts.push(servingsLabel(o.recipe.servings).replace(' servings', ''));
		if (o.purchasable && o.priceMinor !== null) parts.push(formatMoney(o.priceMinor, o.currency));
		else parts.push('can’t be bought yet');
		return parts.join(' · ');
	}
	const flavourCount = $derived(new Set(options.map((o) => o.flavour)).size);

	function openPicker() {
		void picker?.show({
			date: bagDate,
			mode: bag.state.mode,
			slotId: bagDate ? bag.state.slot?.id : undefined
		});
	}

	function chooseWhen(date: string, mode: PickerMode, slot: ChosenSlot) {
		bag.set(withWhen(bag.state, date, mode, slot));
	}

	function addToBag() {
		if (!option || !bagDate || tooLate) return;
		bag.set(withCake(bag.state, option.key));
		void goto(resolve('/(shop)/bag'));
	}

	const tag = $derived(
		!option || !option.purchasable
			? 'Not available right now'
			: option.kind === 'ready'
				? `Ready-made · ${option.available} left · collect by ${shortDate(londonDate(option.collectBy as string))}`
				: 'Made to order'
	);
</script>

<svelte:head>
	<title>{cake ? `${cake.name} · GK Cake London` : 'Cake · GK Cake London'}</title>
	{#if !cake}<meta name="robots" content="noindex" />{/if}
</svelte:head>

<div class="wrap">
	{#if !data.result.ok || !cake}
		<LoadProblem what="this cake" />
	{:else}
		<p class="breadcrumb">
			<a href={resolve('/(shop)/shop')}>Shop cakes</a> &nbsp;/&nbsp; <span>{cake.name}</span>
		</p>
		<section class="detail">
			<div class="detail-visual"><CakePhoto name={cake.name} /></div>
			<div class="detail-copy">
				<span class="tag">{tag}</span>
				<h1>{cake.name}</h1>
				{#if option}<p class="flavour">{option.flavour}</p>{/if}
				<p class="detail-price">
					{option?.purchasable && option.priceMinor !== null
						? formatMoney(option.priceMinor, option.currency)
						: 'Not available right now'}
				</p>

				{#each prominent as a (a.code)}
					<div class="peanut-alert" role="note">
						<strong>Contains {a.label}</strong>
						<span>Please check the allergens below before ordering.</span>
					</div>
				{/each}

				{#if options.length > 1}
					<fieldset class="size-choice">
						<legend>Size</legend>
						<div class="size-options">
							{#each options as o (o.key)}
								<label class="choice-chip">
									<input
										type="radio"
										name="option"
										value={o.key}
										checked={o.key === option?.key}
										onchange={() => (chosenKey = o.key)}
									/>
									<span>{optionLabel(o, flavourCount)}</span>
								</label>
							{/each}
						</div>
						<p class="small">
							Sizes chosen by the chef for this cake.
							<a href={resolve('/(shop)/sizes')}>Sizes and starting prices</a>
						</p>
					</fieldset>
				{/if}

				{#if option}
					<div class="facts">
						<span>{sizeLabel(option.sizeCode)}</span>
						{#if option.recipe}<span>{servingsLabel(option.recipe.servings)}</span>{/if}
					</div>
				{/if}
				{#if cake.description}<p class="description">{cake.description}</p>{/if}

				{#if option?.purchasable && option.recipe}
					<p class="contains">
						<strong>Contains: {containsLine(allergens)}.</strong>
						<a href="#allergens">Full allergen notice</a>
					</p>
					<p class="small">
						Finished with {option.recipe.coating}. The decoration shown is included. Changes and
						extras need a price agreed with the chef before you pay.
					</p>
					{#if option.kind === 'ready'}
						<div class="notice" role="note">
							<div>
								<p>
									This cake is already made, so it doesn’t use one of the day’s places. It must be
									collected by {deadlineLabel(option.collectBy as string)}: the chef’s collect-by
									date for this cake. The usual 192-hour notice applies unless the chef has approved
									shorter notice for this cake.
								</p>
							</div>
						</div>
					{/if}
				{:else if option && !option.purchasable}
					<p class="contains"><strong>Allergen information is not available yet.</strong></p>
				{/if}

				{#if !option}
					<div class="notice error">
						<div>
							<p>
								This cake can’t be ordered right now. Please come back later, or contact the chef.
							</p>
						</div>
					</div>
				{:else if !option.purchasable}
					<div class="notice error">
						<div>
							<p>
								Allergen information for this cake is still being checked, so it can’t be bought
								yet. You can still ask the chef about it.
							</p>
						</div>
					</div>
				{:else if tooLate && bagDate}
					<div class="notice error" role="status">
						<div>
							<p>
								Your date, {dateLabel(bagDate)}, is after this ready-made cake’s collect-by date ({shortDate(
									londonDate(option.collectBy as string)
								)}). Please choose an earlier date.
							</p>
						</div>
					</div>
				{:else if bagFull}
					<div class="notice error" role="status">
						<div><p>Your bag is full: one order can have up to {MAX_BAG} cakes.</p></div>
					</div>
				{:else}
					<div class="notice" role="status">
						<div>
							<p>
								{#if bagDate}
									Your date: {dateLabel(bagDate)}{bag.state.slot
										? ` · ${timeRange(bag.state.slot.starts_at, bag.state.slot.ends_at)}`
										: ''}. Nothing is reserved until you pay.
								{:else}
									Choose a date and time to add this cake. Nothing is reserved until you pay.
								{/if}
							</p>
						</div>
					</div>
				{/if}

				<div class="action-stack">
					{#if option?.purchasable}
						<button class="btn" type="button" onclick={openPicker}>
							<Icon name="calendar" />
							{bagDate ? 'Change date or time' : 'Choose your date and time'}
						</button>
						<button
							class="btn primary"
							type="button"
							disabled={!bagDate || tooLate || bagFull}
							onclick={addToBag}
						>
							Add to bag · {formatMoney(option.priceMinor as number, option.currency)}
							<span class="arrow" aria-hidden="true">→</span>
						</button>
					{:else if option}
						<button class="btn" type="button" disabled>Can’t be bought yet</button>
						<a class="button" href="mailto:gk.cake.london@gmail.com">Contact the chef</a>
					{:else}
						<button class="btn" type="button" disabled>Not available right now</button>
					{/if}
				</div>
				<p class="small">
					Collect for free at St Katharine Docks, send your own courier, or ask for GK delivery
					(quoted by the chef from £100 and paid separately).
				</p>

				{#if option?.purchasable && option.recipe}
					<details class="accordion" id="allergens" open>
						<summary>Ingredients &amp; allergens</summary>
						{#if option.recipe.composition.length > 0}
							<p>{option.recipe.composition.join('. ')}. Coating: {option.recipe.coating}.</p>
						{/if}
						{#each prominent as a (a.code)}
							<p class="allergen-alert">Contains {a.label}</p>
						{/each}
						<p><strong>Contains: {containsLine(allergens)}.</strong></p>
						<p class="cross-contact">{option.recipe.allergen_notice.text}</p>
						<p class="small">
							Final product, decoration and supplier-label checks are still needed before launch.
						</p>
					</details>
				{/if}
				<details class="accordion">
					<summary>Sizes &amp; changes</summary>
					<p>
						{options.length > 1
							? 'This cake comes in the sizes above.'
							: 'This cake comes in the size shown.'}
						For another size, colour or flavour, please contact the chef. See
						<a href={resolve('/(shop)/sizes')}>sizes and starting prices</a>.
					</p>
				</details>
				<details class="accordion">
					<summary>Collection &amp; your date</summary>
					<p>
						Collection is from St Katharine Docks, London, Thursday to Sunday. Cakes need at least
						192 hours’ notice from payment to handover. Dates open up to about two months ahead.
					</p>
				</details>
			</div>
		</section>
		<DatePicker
			bind:this={picker}
			{api}
			places={placesNeeded(keysWithThis)}
			{collectBy}
			onchoose={chooseWhen}
		/>
	{/if}
</div>
