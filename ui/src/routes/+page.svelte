<script lang="ts">
	// Home (approved GK-016 r01c sketch on GK-015 r12): the hero, studio
	// notes, the first cakes of the collection and the welcome story.
	import { resolve } from '$app/paths';
	import CakeCard from '../lib/shop/ui/CakeCard.svelte';
	import ShopShell from '../lib/shop/ui/ShopShell.svelte';
	import LoadProblem from '../lib/shop/ui/LoadProblem.svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const featured = $derived(data.listings.ok ? data.listings.data.listings.slice(0, 4) : []);
	const notes = [
		'Handcrafted in London',
		'Swiss meringue buttercream',
		'Bespoke designs on request',
		'Collection at St Katharine Docks',
		'GK delivery quoted individually'
	];
</script>

<svelte:head>
	<title>GK Cake London · Handcrafted cakes</title>
	<meta
		name="description"
		content="Handcrafted cakes in London. Collection at St Katharine Docks, Thursday to Sunday."
	/>
</svelte:head>

<ShopShell>
	<section class="hero">
		<div class="wrap hero-inner">
			<div class="hero-copy">
				<p class="kicker"><i class="dot" aria-hidden="true"></i>Handcrafted cakes · London</p>
				<h1>Cakes that make moments <em>unforgettable</em></h1>
				<p class="intro">Handcrafted in London. Designed for your celebration.</p>
				<div class="hero-buttons">
					<a class="button primary" href={resolve('/(shop)/shop')}
						>Shop cakes <span class="arrow" aria-hidden="true">→</span></a
					>
					<a class="button" href={resolve('/(shop)/sizes')}>Sizes &amp; starting prices</a>
				</div>
				<ul class="hero-facts" aria-label="At a glance">
					<li>Swiss meringue buttercream</li>
					<li>St Katharine Docks collection</li>
					<li>Bespoke on request</li>
				</ul>
			</div>
			<div class="hero-art">
				<img
					src="/shop/hero-cake-stand.png"
					width="653"
					height="720"
					fetchpriority="high"
					alt="Reference cake with dark flowers on its polished silver turntable"
				/>
			</div>
		</div>
	</section>

	<section class="ticker" aria-label="Studio notes">
		<ul>
			{#each notes as n (n)}<li>{n}</li>{/each}
		</ul>
	</section>

	<section class="wrap" aria-labelledby="collection-title">
		<div class="heading-row">
			<div>
				<p class="kicker"><i class="dot" aria-hidden="true"></i>The collection</p>
				<h2 class="section-title" id="collection-title">Find your <em>celebration</em></h2>
			</div>
			<a class="text-link" href={resolve('/(shop)/shop')}
				>Shop all cakes <span aria-hidden="true">→</span></a
			>
		</div>
		{#if !data.listings.ok}
			<LoadProblem />
		{:else if featured.length === 0}
			<div class="empty">
				<h2>No cakes to show yet</h2>
				<p>The chef is preparing the collection. Please come back soon.</p>
			</div>
		{:else}
			<ul class="grid featured">
				{#each featured as item, i (item.id)}
					<CakeCard {item} index={i} heading="h3" />
				{/each}
			</ul>
		{/if}
	</section>

	<section class="wrap home-story" aria-labelledby="story-title">
		<div>
			<p class="kicker"><i class="dot" aria-hidden="true"></i>Welcome to</p>
			<h2 id="story-title">GK Cake <em>London</em></h2>
			<p>
				A cake should feel as special as the moment it marks. From delicate piping to bold
				sculptural details, find a design that feels like you.
			</p>
			<p>
				Choose from the collection, or share your own idea with Elina. We’ll talk through the
				flavour, size and finishing touches together.
			</p>
			<p class="signature">Love, GK</p>
		</div>
	</section>
</ShopShell>
