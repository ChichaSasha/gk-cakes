<script lang="ts">
	// The chef workspace frame (approved GK-018 r03b2 sketch): sidebar with
	// the workspace's sections and who is signed in, and the main area.
	// Sections not built yet are shown as "later", as in the sketch. Account
	// requests (GK-018 r04d-claims) is the chef's only, with a waiting count.
	// Needs attention (GK-031 slice 3) is the API's attention list.
	import '../chef.css';
	import type { Snippet } from 'svelte';
	import { resolve } from '$app/paths';
	import { claimsWaiting } from '../claimsWaiting.svelte';
	import type { StaffView } from '../orders';
	import ChefIcon, { type ChefIconName } from './ChefIcon.svelte';

	let {
		active,
		view,
		onsignout,
		children
	}: {
		active: 'orders' | 'claims' | 'attention';
		view: StaffView | null;
		onsignout: () => void;
		children: Snippet;
	} = $props();

	const LABELS: Record<StaffView, string> = {
		chef: 'Chef',
		fulfillment: 'Fulfilment',
		technical_support: 'Technical support'
	};

	const sections: {
		key: string;
		icon: ChefIconName;
		label: string;
		href?: string;
		chefOnly?: boolean;
	}[] = [
		{ key: 'today', icon: 'today', label: 'Today' },
		{ key: 'orders', icon: 'orders', label: 'Orders', href: resolve('/admin/orders') },
		{ key: 'new', icon: 'plus', label: 'New order' },
		{ key: 'calendar', icon: 'calendar', label: 'Calendar & capacity' },
		{ key: 'stock', icon: 'stock', label: 'Ingredients' },
		{ key: 'mail', icon: 'mail', label: 'Messages' },
		{
			key: 'attention',
			icon: 'support',
			label: 'Needs attention',
			href: resolve('/admin/attention')
		},
		{
			key: 'claims',
			icon: 'claims',
			label: 'Account requests',
			href: resolve('/admin/requests'),
			chefOnly: true
		},
		{ key: 'basket', icon: 'basket', label: 'Groceries' },
		{ key: 'money', icon: 'money', label: 'Money' }
	];
	const shown = $derived(sections.filter((s) => !s.chefOnly || view === 'chef'));

	$effect(() => {
		if (view === 'chef') void claimsWaiting.load();
	});

	// On narrow screens the sidebar is a horizontal bar: bring the current section into view.
	let sidebar = $state<HTMLElement>();
	$effect(() => {
		void shown.length; // the chef-only section appears once the role is known
		const current = sidebar?.querySelector<HTMLElement>('a[aria-current]');
		if (sidebar && current && sidebar.scrollWidth > sidebar.clientWidth) {
			sidebar.scrollLeft = Math.max(
				0,
				current.offsetLeft - sidebar.clientWidth / 2 + current.offsetWidth / 2
			);
		}
	});
</script>

<div class="chef">
	<a class="skip" href="#main">Skip to content</a>
	<div class="layout">
		<aside class="sidebar" aria-label="Chef workspace" bind:this={sidebar}>
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
					{#each shown as s (s.key)}
						<li>
							{#if s.href}
								<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- s.href is resolve()d above -->
								<a href={s.href} aria-current={active === s.key ? 'page' : undefined}
									><ChefIcon name={s.icon} /><span>{s.label}</span
									>{#if s.key === 'claims' && claimsWaiting.label && claimsWaiting.label !== '0'}<span
											class="nav-count"
											aria-hidden="true">{claimsWaiting.label}</span
										><span class="sr-only"> ({claimsWaiting.label} waiting)</span>{/if}</a
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
			<div class="who">
				{#if view}<span class="avatar" aria-hidden="true">{LABELS[view].charAt(0)}</span>{/if}
				<span
					>{#if view}<strong>{LABELS[view]}</strong>{/if}<small
						><button class="link-button" type="button" onclick={onsignout}>Sign out</button></small
					></span
				>
			</div>
		</aside>
		<main id="main" class="main" tabindex="-1">
			{@render children()}
		</main>
	</div>
</div>
