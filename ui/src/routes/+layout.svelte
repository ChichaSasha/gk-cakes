<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();

	// The home and discovery pages (GK-028) and the account pages (GK-029)
	// bring their own header.
	const shopPage = $derived(
		page.route.id === '/' ||
			(page.route.id?.startsWith('/(shop)') ?? false) ||
			page.route.id === '/account' ||
			(page.route.id?.startsWith('/account/') ?? false)
	);
	// So do the chef workspace pages (GK-018 r03b2).
	const chefPage = $derived(page.route.id?.startsWith('/admin/orders') ?? false);
</script>

{#if !shopPage && !chefPage}
	<header>
		<a class="brand" href={resolve('/')}>GK Cakes</a>
		<nav aria-label="Account"><a href={resolve('/account')}>Your account</a></nav>
	</header>
{/if}

{@render children()}

<style>
	/* Functional styling only; the approved visual foundation (GK-015) and the
	   account designs (GK-016) replace this. */
	:global(.app) {
		display: contents;
	}
	:global(body) {
		margin: 0;
		font-family: system-ui, sans-serif;
		line-height: 1.5;
		color: #1d1d1f;
	}
	header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 0.75rem 1.25rem;
		border-bottom: 1px solid #ddd;
	}
	.brand {
		font-weight: 600;
		text-decoration: none;
		color: inherit;
	}
	:global(main) {
		padding: 1.25rem;
	}
	:global(main.narrow) {
		max-width: 32rem;
		margin: 0 auto;
	}
	:global(form label) {
		display: block;
		margin-bottom: 0.75rem;
	}
	:global(form input) {
		display: block;
		width: 100%;
		box-sizing: border-box;
		padding: 0.5rem;
		font: inherit;
	}
	:global(button) {
		font: inherit;
		padding: 0.5rem 1rem;
		cursor: pointer;
	}
	:global([role='alert']) {
		color: #a00;
	}
	:global(.small) {
		font-size: 0.875rem;
		color: #555;
	}
	:global(table) {
		border-collapse: collapse;
	}
	:global(th),
	:global(td) {
		text-align: left;
		padding: 0.25rem 0.75rem 0.25rem 0;
	}
</style>
