<script lang="ts">
	// Sizes and starting prices (approved GK-016 r01c sketch): the owner's
	// confirmed GK-002 base price list, a public guide. The price of each cake
	// is the chef's own, shown on its page and priced by the server at
	// checkout; nothing here is used for a total.
	import { resolve } from '$app/paths';
	import { formatMoney } from '../../../lib/checkout/format';

	// docs/creator/GK-002 base_price_list (confirmed 2026-10-05), GBP pence.
	const single = [
		{ size: 'Mini', diameter: '5 inches', servings: '3–4', weight: '1–1.3 kg', from: 7000 },
		{ size: 'S', diameter: '6 inches', servings: '7–10', weight: '1.5–1.7 kg', from: 9000 },
		{ size: 'M', diameter: '7 inches', servings: '12–14', weight: '2.2–2.7 kg', from: 10500 },
		{ size: 'L', diameter: '8 inches', servings: '17–20', weight: '3–3.8 kg', from: 13000 },
		{ size: 'XL', diameter: '10 inches', servings: '30–32', weight: '5–6 kg', from: 18000 },
		{ size: 'XXL', diameter: '12 inches', servings: '40–45', weight: '8–9 kg', from: 24500 }
	];
	const tiered = [
		{ tiers: '4 + 7 inches', servings: '14–17', from: 18000 },
		{ tiers: '6 + 8 inches', servings: '28–32', from: 25000 },
		{ tiers: '8 + 10 inches', servings: '47–53', from: 32000 },
		{ tiers: '4 + 6 + 8 inches', servings: '35–40', from: 33000 },
		{ tiers: '6 + 9 + 11 inches', servings: '70–80', from: 60000 }
	];
</script>

<svelte:head>
	<title>Sizes and starting prices · GK Cake London</title>
	<meta name="description" content="Cake sizes, servings and starting prices at GK Cake London." />
</svelte:head>

<div class="wrap">
	<div class="page-title">
		<h1>Sizes and starting prices</h1>
		<p>A guide to servings and where prices start.</p>
	</div>
	<div class="narrow">
		<p>
			Every cake is finished with Swiss meringue buttercream. These are the starting prices for each
			size, the same for every flavour. The chef chooses the sizes each cake comes in and sets its
			price, shown on the cake’s page. The decoration in a cake’s photo is included in its price.
		</p>
		<table class="size-table">
			<caption>Single-tier cakes</caption>
			<thead>
				<tr>
					<th scope="col">Size</th>
					<th scope="col">Diameter</th>
					<th scope="col">Servings</th>
					<th scope="col">Weight</th>
					<th scope="col">From</th>
				</tr>
			</thead>
			<tbody>
				{#each single as r (r.size)}
					<tr>
						<th scope="row">{r.size}</th>
						<td>{r.diameter}</td>
						<td>{r.servings}</td>
						<td>{r.weight}</td>
						<td>{formatMoney(r.from)}</td>
					</tr>
				{/each}
			</tbody>
		</table>
		<table class="size-table">
			<caption>Tiered cakes</caption>
			<thead>
				<tr>
					<th scope="col">Tiers</th>
					<th scope="col">Servings</th>
					<th scope="col">From</th>
				</tr>
			</thead>
			<tbody>
				{#each tiered as r (r.tiers)}
					<tr>
						<th scope="row">{r.tiers}</th>
						<td>{r.servings}</td>
						<td>{formatMoney(r.from)}</td>
					</tr>
				{/each}
			</tbody>
		</table>
		<p class="small">
			Larger sizes are available by request. Some decoration details cost extra; the chef agrees the
			price with you before you pay.
		</p>
		<div class="form-actions">
			<a class="button primary" href={resolve('/(shop)/shop')}
				>Shop cakes <span class="arrow" aria-hidden="true">→</span></a
			>
			<a class="button" href="mailto:gk.cake.london@gmail.com">Ask the chef</a>
		</div>
	</div>
</div>
