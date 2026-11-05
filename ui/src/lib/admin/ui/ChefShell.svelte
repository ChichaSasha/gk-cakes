<script lang="ts">
	// The chef workspace frame (approved GK-018 r03b2 sketch): sidebar with
	// the workspace's sections and who is signed in, and the main area.
	// Sections not built yet are shown as "later", as in the sketch.
	import '../chef.css';
	import type { Snippet } from 'svelte';
	import { resolve } from '$app/paths';
	import type { StaffView } from '../orders';
	import ChefIcon, { type ChefIconName } from './ChefIcon.svelte';

	let {
		active,
		view,
		onsignout,
		children
	}: {
		active: 'orders';
		view: StaffView | null;
		onsignout: () => void;
		children: Snippet;
	} = $props();

	const LABELS: Record<StaffView, string> = {
		chef: 'Chef',
		fulfillment: 'Fulfilment',
		technical_support: 'Technical support'
	};

	const sections: { key: string; icon: ChefIconName; label: string; href?: string }[] = [
		{ key: 'today', icon: 'today', label: 'Today' },
		{ key: 'orders', icon: 'orders', label: 'Orders', href: resolve('/admin/orders') },
		{ key: 'new', icon: 'plus', label: 'New order' },
		{ key: 'calendar', icon: 'calendar', label: 'Calendar & capacity' },
		{ key: 'stock', icon: 'stock', label: 'Ingredients' },
		{ key: 'mail', icon: 'mail', label: 'Messages' },
		{ key: 'support', icon: 'support', label: 'Needs attention' },
		{ key: 'basket', icon: 'basket', label: 'Groceries' },
		{ key: 'money', icon: 'money', label: 'Money' }
	];
</script>

<div class="chef">
	<a class="skip" href="#main">Skip to content</a>
	<div class="layout">
		<aside class="sidebar" aria-label="Chef workspace">
			<a
				class="brand"
				href={resolve('/admin/orders')}
				aria-label="GK Cake London chef workspace, Orders"
			>
				<img src="/admin/chrome-emblem.png" alt="" width="40" height="40" />
				<span><strong>GK Cake London</strong><small>Chef workspace</small></span>
			</a>
			<nav aria-label="Sections">
				<ul>
					{#each sections as s (s.key)}
						<li>
							{#if s.href}
								<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- s.href is resolve()d above -->
								<a href={s.href} aria-current={active === s.key ? 'page' : undefined}
									><ChefIcon name={s.icon} /><span>{s.label}</span></a
								>
							{:else}
								<span class="nav-later" title="Not built yet"
									><ChefIcon name={s.icon} /><span>{s.label}</span><small>later</small></span
								>
							{/if}
						</li>
					{/each}
				</ul>
			</nav>
			{#if view}
				<div class="who">
					<span class="avatar" aria-hidden="true">{LABELS[view].charAt(0)}</span>
					<span
						><strong>{LABELS[view]}</strong><small
							><button class="link-button" type="button" onclick={onsignout}>Sign out</button
							></small
						></span
					>
				</div>
			{/if}
		</aside>
		<main id="main" class="main" tabindex="-1">
			{@render children()}
		</main>
	</div>
</div>
