<script lang="ts">
	// The account pages' frame (approved GK-016 r02b sketch): the shop frame,
	// the page title with who is signed in, and the account navigation (a
	// labelled nav with aria-current; a horizontal row on narrow screens).
	// Only pages that exist are linked.
	import '../account.css';
	import { resolve } from '$app/paths';
	import type { Snippet } from 'svelte';
	import ShopShell from '../../shop/ui/ShopShell.svelte';

	let {
		active,
		title = 'Your account',
		sub,
		onsignout,
		children
	}: {
		active: 'orders' | 'favourites';
		title?: string;
		sub?: string;
		onsignout?: () => void;
		children: Snippet;
	} = $props();
</script>

<ShopShell>
	<div class="wrap acc-page">
		<div class="page-title">
			<h1 tabindex="-1">{title}</h1>
			{#if sub}<p>{sub}</p>{/if}
		</div>
		<div class="acc-layout">
			<nav class="acc-nav" aria-label="Your account">
				<a href={resolve('/account')} aria-current={active === 'orders' ? 'page' : undefined}
					>Orders &amp; requests</a
				>
				<a href={resolve('/(shop)/favourites')}>Favourites</a>
				{#if onsignout}
					<button class="acc-nav-out" type="button" onclick={onsignout}>Sign out</button>
				{/if}
			</nav>
			<div class="acc-main">
				{@render children()}
			</div>
		</div>
	</div>
</ShopShell>
