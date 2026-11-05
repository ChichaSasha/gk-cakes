<script lang="ts">
	// GK-018 groundwork: a plain, read-only view of the catalogue as the staff
	// API returns it (cakes in every state, their sizes and recipe prices, and
	// the colour list). Not the chef's catalogue screens: those follow the
	// GK-018 design once Sasha approves it. Chef and technical support can
	// read; nothing here changes anything.
	import { resolve } from '$app/paths';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { ApiError } from '../../../lib/api';
	import { leaveOnSignOut, signInConfigured, signOut } from '../../../lib/auth';
	import { formatMoney } from '../../../lib/checkout/format';
	import {
		byState,
		colours,
		liveVariants,
		staffCakes,
		type Colour,
		type StaffCake
	} from '../../../lib/admin/catalogue';
	import { staffApi } from '../../../lib/admin/session';

	let cakes = $state<StaffCake[] | null>(null);
	let colourList = $state<Colour[] | null>(null);
	let problem = $state('');

	const groups = $derived(cakes ? byState(cakes) : null);
	const headings = { draft: 'Drafts', active: 'Live', archived: 'Archived' } as const;

	onMount(() => {
		let active = true;
		(async () => {
			const api = signInConfigured ? await staffApi() : null;
			if (!api) {
				if (active) await goto(resolve('/sign-in?next=/admin'), { replaceState: true });
				return;
			}
			try {
				const [c, k] = await Promise.all([staffCakes(api), colours(api)]);
				if (!active) return;
				cakes = c;
				colourList = k;
			} catch (e) {
				if (!active) return;
				if (e instanceof ApiError && e.signInRequired) {
					await signOut();
					if (active) await goto(resolve('/sign-in?next=/admin&ended=1'), { replaceState: true });
					return;
				}
				problem =
					e instanceof ApiError && e.code === 'FORBIDDEN'
						? 'This account cannot see the catalogue.'
						: 'The catalogue could not be loaded right now. Please try again.';
			}
		})();
		const unsubscribe = leaveOnSignOut(() => {
			cakes = null;
			colourList = null;
			void goto(resolve('/sign-in?next=/admin'), { replaceState: true });
		});
		return () => {
			active = false;
			unsubscribe();
		};
	});
</script>

<svelte:head>
	<title>Catalogue · Staff · GK Cakes</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<main class="narrow">
	<p><a href={resolve('/admin')}>Staff</a></p>
	<h1>Catalogue</h1>
	<p class="small">Read only. The chef’s catalogue screens will follow the approved design.</p>
	{#if problem}
		<p role="alert">{problem}</p>
	{:else if !groups || !colourList}
		<p role="status">Loading…</p>
	{:else}
		{#each ['draft', 'active', 'archived'] as const as state (state)}
			<h2>{headings[state]} ({groups[state].length})</h2>
			{#if groups[state].length === 0}
				<p>None.</p>
			{:else}
				<table>
					<thead>
						<tr><th>Cake</th><th>Colour</th><th>Sizes offered</th></tr>
					</thead>
					<tbody>
						{#each groups[state] as cake (cake.id)}
							<tr>
								<td>{cake.name}</td>
								<td
									>{cake.colour
										? `${cake.colour.label}${cake.colour.archived_at ? ' (archived)' : ''}`
										: '—'}</td
								>
								<td>
									{#each liveVariants(cake) as v (v.id)}
										<div>
											{v.flavour} · {v.size.label} · {v.stock_mode === 'ready_stock'
												? 'Ready-made'
												: 'Made to order'} · {v.recipe
												? formatMoney(v.recipe.price_minor, v.recipe.currency)
												: 'no recipe yet'}{v.purchasable ? '' : ' · not buyable'}
										</div>
									{:else}
										None
									{/each}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}
		{/each}
		<h2>Colours ({colourList.length})</h2>
		{#if colourList.length === 0}
			<p>None yet.</p>
		{:else}
			<table>
				<thead><tr><th>Order</th><th>Colour</th><th>Code</th><th>State</th></tr></thead>
				<tbody>
					{#each colourList as c (c.id)}
						<tr>
							<td>{c.position}</td>
							<td>{c.label}</td>
							<td><code>{c.code}</code></td>
							<td>{c.archived_at ? 'Archived' : 'Active'}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	{/if}
</main>
