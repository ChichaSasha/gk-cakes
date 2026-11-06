<script lang="ts">
	// Needs attention (Creator-approved GK-018 r03b2 sketch, support.html):
	// the money cases and stopped jobs the API lists (GET
	// /api/v1/admin/attention), grouped as in the sketch, each linking to its
	// order where the chef decides. Read only. The chef sees amounts;
	// technical support the same list without amounts (the API audits its
	// read); fulfilment gets the sketch's role note and no request.
	import { resolve } from '$app/paths';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { ApiError, apiGet } from '../../../lib/api';
	import { leaveOnSignOut, signInConfigured, signOut, type Me } from '../../../lib/auth';
	import type { StaffApi } from '../../../lib/admin/catalogue';
	import { viewOf, type StaffView } from '../../../lib/admin/orders';
	import {
		attentionGroups,
		attentionList,
		type AttentionGroup
	} from '../../../lib/admin/attention';
	import { staffApi } from '../../../lib/admin/session';
	import ChefShell from '../../../lib/admin/ui/ChefShell.svelte';
	import '../../../lib/admin/attention.css';

	let api: StaffApi | null = null;
	let view = $state<StaffView | null>(null);
	let groups = $state<AttentionGroup[] | null>(null);
	let phase = $state<'loading' | 'ready' | 'error' | 'unavailable'>('loading');
	let problem = $state('');
	let mounted = true;
	const isActive = () => mounted;

	async function ended(e: unknown): Promise<boolean> {
		if (e instanceof ApiError && e.signInRequired) {
			await signOut();
			await goto(resolve('/sign-in?next=/admin&ended=1'), { replaceState: true });
			return true;
		}
		if (e instanceof ApiError && (e.code === 'FORBIDDEN' || e.code === 'ACCOUNT_DISABLED')) {
			problem =
				e.code === 'FORBIDDEN' ? 'This account has no staff access.' : 'This account is disabled.';
			return true;
		}
		return false;
	}

	async function start() {
		if (!api) return;
		phase = 'loading';
		try {
			if (!view) {
				const me = await apiGet<Me>('/api/v1/admin/me', { baseUrl: api.baseUrl, token: api.token });
				if (!isActive()) return;
				view = viewOf(me.capabilities);
				if (!view) {
					problem = 'This account has no staff access.';
					return;
				}
			}
			if (view === 'fulfillment') {
				phase = 'ready';
				return;
			}
			const items = await attentionList(api);
			if (!isActive()) return;
			groups = attentionGroups(items, view);
			phase = 'ready';
		} catch (e) {
			if (!isActive() || (await ended(e))) return;
			// Without new checkouts the API doesn't serve the list (404).
			phase = e instanceof ApiError && e.status === 404 ? 'unavailable' : 'error';
		}
	}

	onMount(() => {
		let active = true;
		(async () => {
			api = signInConfigured ? await staffApi() : null;
			if (!active) return;
			if (!api) {
				await goto(resolve('/sign-in?next=/admin'), { replaceState: true });
				return;
			}
			await start();
		})();
		const unsubscribe = leaveOnSignOut(() => {
			groups = null;
			void goto(resolve('/sign-in?next=/admin'), { replaceState: true });
		});
		return () => {
			active = false;
			mounted = false;
			unsubscribe();
		};
	});

	async function leave() {
		groups = null;
		const reached = await signOut();
		if (!reached) {
			problem =
				'You are signed out on this device, but the sign-in service could not be reached to end the session everywhere.';
			return;
		}
		await goto(resolve('/'), { replaceState: true });
	}

	const total = $derived(groups ? groups.reduce((n, g) => n + g.items.length, 0) : 0);
</script>

<svelte:head>
	<title>Needs attention · GK Cake London chef workspace</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<ChefShell active="attention" {view} onsignout={leave}>
	<div class="attention-page">
		<header class="page-head">
			<div>
				<p class="eyebrow">Needs attention</p>
				<h1>Needs attention</h1>
				{#if view && view !== 'fulfillment'}
					<p class="muted">
						Problems behind the scenes: money cases that need a manager, results we’re still
						checking and jobs that stopped. {view === 'technical_support'
							? 'You can look; money decisions stay with Elina, on each order’s page.'
							: 'You decide each money case on its order’s page.'} Every view here is recorded.
					</p>
				{/if}
			</div>
		</header>
		{#if problem}
			<div class="banner banner-bad" role="alert"><p>{problem}</p></div>
		{:else if view === 'fulfillment'}
			<div class="card">
				<p class="role-note">
					Needs attention is for Elina and technical support: it covers payments and refunds.
					Fulfilment helpers start, finish and hand over cakes from the order pages.
				</p>
				<a class="btn" href={resolve('/admin/orders')}>Back to orders</a>
			</div>
		{:else if phase === 'loading'}
			<p class="muted" role="status">Loading…</p>
		{:else if phase === 'unavailable'}
			<div class="card"><p>The attention list isn’t available on this service right now.</p></div>
		{:else if phase === 'error'}
			<div class="banner banner-bad" role="alert">
				<div>
					<p>The list could not be loaded right now.</p>
					<button class="btn" type="button" onclick={() => void start()}>Try again</button>
				</div>
			</div>
		{:else if groups}
			<p class="sr-only" role="status">
				{total === 0 ? 'Nothing needs attention.' : `${total} ${total === 1 ? 'item' : 'items'}.`}
			</p>
			{#if !groups.length}
				<div class="card"><p>Nothing needs attention right now.</p></div>
			{/if}
			{#each groups as g (g.key)}
				<h2 class="q-section" id="q-{g.key}">
					{g.label}<span class="count"
						>{g.items.length}<span class="sr-only">
							{g.items.length === 1 ? 'item' : 'items'}</span
						></span
					>
				</h2>
				{#if g.note}<p class="small muted">{g.note}</p>{/if}
				<ul class="queue" aria-labelledby="q-{g.key}">
					{#each g.items as it (it.key)}
						<li class="card q-item">
							<div class="q-row">
								<h3 class="q-title">
									<span class="need-dot need-{it.tone}" aria-hidden="true"
									></span>{it.title}{#if it.order}
										<span class="need-id">{it.order}</span>{/if}
								</h3>
								{#if it.orderId}
									<a class="small" href={resolve('/admin/orders/[id]', { id: it.orderId })}
										>View order<span class="sr-only"> {it.order}</span></a
									>
								{/if}
							</div>
							{#if !it.orderId}<p class="small muted">Not tied to one order</p>{/if}
							{#each it.lines as line, i (i)}<p class="small">{line}</p>{/each}
						</li>
					{/each}
				</ul>
			{/each}
			{#if view === 'technical_support' && groups.length}
				<p class="role-note">
					Money decisions stay with Elina. Amounts aren’t shown to technical support.
				</p>
			{/if}
		{/if}
	</div>
</ChefShell>
