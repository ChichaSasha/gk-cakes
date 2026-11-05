<script lang="ts">
	// Site header of the discovery pages (approved GK-016 r01c sketch): shop
	// navigation, the GK Cake London wordmark, account and bag. Only pages
	// that exist are linked.
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { bag } from '../bag.svelte';
	import Icon from './Icon.svelte';

	let open = $state(false);
	const count = $derived(bag.state.keys.length);
	const path = $derived(page.url.pathname);
	const current = (href: string) =>
		(href === '/' ? path === '/' : path === href || path.startsWith(`${href}/`)) ||
		(href === '/shop' && path.startsWith('/cakes/'))
			? 'page'
			: undefined;

	const links = [
		{ path: '/', href: resolve('/'), label: 'Home' },
		{ path: '/shop', href: resolve('/(shop)/shop'), label: 'Shop cakes' },
		{ path: '/sizes', href: resolve('/(shop)/sizes'), label: 'Sizes & prices' }
	];

	$effect(() => {
		// Close the mobile menu after navigating.
		void path;
		open = false;
	});
</script>

<a class="skip" href="#main">Skip to content</a>
<header class="shop-header">
	<div class="header-inner">
		<nav class="nav-left" aria-label="Shop navigation">
			<button
				class="icon-button menu-toggle"
				type="button"
				aria-label={open ? 'Close menu' : 'Open menu'}
				aria-expanded={open}
				aria-controls="mobile-menu"
				onclick={() => (open = !open)}
			>
				<Icon name="menu" />
			</button>
			{#each links as l (l.path)}
				<a href={l.href} aria-current={current(l.path)}>{l.label}</a>
			{/each}
		</nav>
		<a class="wordmark" href={resolve('/')} aria-label="GK Cake London home">
			<svg viewBox="0 0 150 105" aria-hidden="true" focusable="false">
				<text
					x="77"
					y="51"
					text-anchor="middle"
					font-family="Helvetica Neue,Arial,sans-serif"
					font-weight="500"
					font-size="51"
					letter-spacing="7">GK</text
				>
				<path id="brand-curve" d="M25 57 Q75 111 125 57" fill="none" />
				<text font-family="Helvetica Neue,Arial,sans-serif" font-size="15" letter-spacing="1.8"
					><textPath href="#brand-curve" startOffset="50%" text-anchor="middle"
						>Cake London</textPath
					></text
				>
			</svg>
		</a>
		<nav class="nav-right" aria-label="Account and bag">
			<a class="icon-button" href={resolve('/account')} aria-label="Your account">
				<Icon name="user" />
			</a>
			<a
				class="icon-button"
				href={resolve('/(shop)/bag')}
				aria-label={`Bag, ${count} ${count === 1 ? 'cake' : 'cakes'}`}
				aria-current={current('/bag')}
			>
				<Icon name="bag" />
				{#if count > 0}<span class="bag-count" aria-hidden="true">{count}</span>{/if}
			</a>
		</nav>
	</div>
	<nav id="mobile-menu" class="mobile-nav" class:open aria-label="Mobile navigation">
		{#each links as l (l.path)}
			<a href={l.href} aria-current={current(l.path)}>{l.label}</a>
		{/each}
		<a href={resolve('/(shop)/bag')} aria-current={current('/bag')}>Bag</a>
		<a href={resolve('/account')}>Your account</a>
	</nav>
</header>
