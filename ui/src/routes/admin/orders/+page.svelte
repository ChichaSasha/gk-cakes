<script lang="ts">
	// Orders (approved GK-018 r03b2 sketch, orders.html): every order by
	// handover date for chef, technical support and fulfilment, each seeing
	// what the API gives their role. Read only for now: no order page or
	// action yet. Search filters the loaded orders in the browser (the API has
	// no text search, K09); nothing personal goes into the URL.
	import { resolve } from '$app/paths';
	import { goto, replaceState } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { ApiError, apiGet } from '../../../lib/api';
	import { leaveOnSignOut, signInConfigured, signOut, type Me } from '../../../lib/auth';
	import { londonToday } from '../../../lib/checkout/format';
	import type { StaffApi } from '../../../lib/admin/catalogue';
	import {
		cakeText,
		matchesSearch,
		matchesStatus,
		MODES,
		moneyText,
		periods,
		productionText,
		shortDay,
		shortId,
		slotText,
		staffOrders,
		statusFilters,
		statusOf,
		viewOf,
		type Period,
		type StaffOrderRow,
		type StaffView,
		type StatusFilter
	} from '../../../lib/admin/orders';
	import { staffApi } from '../../../lib/admin/session';
	import ChefIcon from '../../../lib/admin/ui/ChefIcon.svelte';
	import ChefShell from '../../../lib/admin/ui/ChefShell.svelte';

	const today = londonToday();
	const PERIODS = periods(today);
	const params = page.url.searchParams;
	const isPeriod = (v: string | null): v is Period => v !== null && v in PERIODS;

	let api: StaffApi | null = null;
	let view = $state<StaffView | null>(null);
	let rows = $state<StaffOrderRow[] | null>(null);
	let phase = $state<'loading' | 'ready' | 'error'>('loading');
	let problem = $state('');
	let period = $state<Period>(
		isPeriod(params.get('period')) ? (params.get('period') as Period) : 'upcoming'
	);
	let mode = $state(
		params.get('mode') && MODES[params.get('mode') as string] ? (params.get('mode') as string) : ''
	);
	let filter = $state<StatusFilter>('all');
	let term = $state('');
	let revision = 0;

	const filters = $derived(view ? statusFilters(view) : []);
	const showMoney = $derived(view === 'chef');
	const shown = $derived(
		(rows ?? []).filter(
			(o) =>
				matchesSearch(o, term) && (!mode || o.fulfillment_mode === mode) && matchesStatus(o, filter)
		)
	);
	const now = new Date();

	async function load() {
		if (!api) return;
		const rev = ++revision;
		phase = 'loading';
		try {
			const list = await staffOrders(api, PERIODS[period]);
			if (rev !== revision) return;
			rows = list;
			phase = 'ready';
		} catch (e) {
			if (rev !== revision) return;
			if (await ended(e)) return;
			phase = 'error';
		}
	}

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

	function keepInUrl() {
		const url = new URL(page.url.href);
		url.searchParams.delete('period');
		url.searchParams.delete('mode');
		if (period !== 'upcoming') url.searchParams.set('period', period);
		if (mode) url.searchParams.set('mode', mode);
		// eslint-disable-next-line svelte/no-navigation-without-resolve -- same page, query only
		replaceState(url, {});
	}

	function changePeriod() {
		keepInUrl();
		void load();
	}

	onMount(() => {
		let active = true;
		(async () => {
			api = signInConfigured ? await staffApi() : null;
			if (!api) {
				if (active) await goto(resolve('/sign-in?next=/admin'), { replaceState: true });
				return;
			}
			try {
				const me = await apiGet<Me>('/api/v1/admin/me', {
					baseUrl: api.baseUrl,
					token: api.token
				});
				if (!active) return;
				view = viewOf(me.capabilities);
				if (!view) {
					problem = 'This account has no staff access.';
					return;
				}
			} catch (e) {
				if (!active || (await ended(e))) return;
				phase = 'error';
				return;
			}
			await load();
		})();
		const unsubscribe = leaveOnSignOut(() => {
			rows = null;
			void goto(resolve('/sign-in?next=/admin'), { replaceState: true });
		});
		return () => {
			active = false;
			unsubscribe();
		};
	});

	async function leave() {
		rows = null;
		await signOut();
		await goto(resolve('/'), { replaceState: true });
	}
</script>

<svelte:head>
	<title>Orders · GK Cake London chef workspace</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<ChefShell active="orders" {view} onsignout={leave}>
	<header class="page-head">
		<div>
			<p class="eyebrow">Orders · London time</p>
			<h1>Orders</h1>
		</div>
	</header>
	{#if problem}
		<div class="banner banner-bad" role="alert"><p>{problem}</p></div>
	{:else}
		<section class="card" aria-label="Find orders">
			<form class="filters" role="search" onsubmit={(e) => e.preventDefault()}>
				<label class="field search-field"
					><span>Search by name or order number</span><span class="search-box"
						><ChefIcon name="search" /><input
							type="search"
							bind:value={term}
							placeholder="e.g. Mia or the order's first characters"
							autocomplete="off"
						/></span
					></label
				>
				<label class="field"
					>When<select bind:value={period} onchange={changePeriod}>
						{#each Object.entries(PERIODS) as [k, p] (k)}<option value={k}>{p.label}</option>{/each}
					</select></label
				>
				<label class="field"
					>How<select bind:value={mode} onchange={keepInUrl}>
						<option value="">Any</option>
						{#each Object.entries(MODES) as [k, l] (k)}<option value={k}>{l}</option>{/each}
					</select></label
				>
				<div class="chips" role="group" aria-label="Status">
					{#each filters as [k, l] (k)}
						<button
							type="button"
							class="filter-chip"
							aria-pressed={filter === k}
							onclick={() => (filter = k)}>{l}</button
						>
					{/each}
				</div>
			</form>
			<p class="small strong" aria-live="polite">
				{phase === 'ready'
					? `${shown.length} ${shown.length === 1 ? 'order' : 'orders'} · sorted by handover date and time`
					: phase === 'loading'
						? 'Loading orders…'
						: ''}
			</p>
			{#if phase === 'error'}
				<div class="banner banner-bad" role="alert">
					<div>
						<p>The orders could not be loaded right now.</p>
						<button class="btn" type="button" onclick={() => void load()}>Try again</button>
					</div>
				</div>
			{:else if phase === 'ready'}
				<div class="table-wrap">
					<table class="table orders-table">
						<caption class="sr-only">Orders matching the search</caption>
						<thead>
							<tr>
								<th scope="col">Order</th>
								<th scope="col">Customer</th>
								<th scope="col">Cake</th>
								<th scope="col">Handover</th>
								<th scope="col">Production</th>
								<th scope="col">Status</th>
								{#if showMoney}<th scope="col">Money</th>{/if}
							</tr>
						</thead>
						<tbody>
							{#each shown as o (o.id)}
								{@const status = statusOf(o, view ?? 'fulfillment')}
								<tr>
									<td data-label="Order"
										><span class="order-id strong" title={o.id}>{shortId(o.id)}</span></td
									>
									<td data-label="Customer">{o.customer_full_name}</td>
									<td data-label="Cake"
										>{cakeText(o)}<span class="block muted small"
											>{MODES[o.fulfillment_mode] ?? o.fulfillment_mode}</span
										></td
									>
									<td data-label="Handover"
										>{shortDay(o.date)}<span class="block muted small">{slotText(o)}</span></td
									>
									<td data-label="Production">{productionText(o, view ?? 'fulfillment', now)}</td>
									<td data-label="Status"
										><span class="pill pill-{status.stage}" class:pill-released={status.released}
											>{#if status.stage === 'ready' || status.stage === 'done'}<ChefIcon
													name="check"
													cls="pill-icon"
												/>{/if}{status.text}</span
										></td
									>
									{#if showMoney}<td data-label="Money">{moneyText(o)}</td>{/if}
								</tr>
							{:else}
								<tr>
									<td colspan={showMoney ? 7 : 6} class="empty-row"
										>No orders match{term.trim() ? ` “${term.trim()}”` : ''}. Try another name,
										order number or date range.</td
									>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
			<p class="muted small foot">
				Production dates show when you started, or the automatic start 48 hours after payment (which
				uses no ingredients). Technical support sees this list read only.
			</p>
		</section>
	{/if}
</ChefShell>
