<script lang="ts">
	// Requests to add orders (Creator-approved GK-018 r04d-claims sketch,
	// requests.html): the chef's queue of customers' requests to add an older
	// order to their account, by state, oldest received first, 50 at a time.
	// Chef only (the API refuses others). Profile details are the customer's
	// own typing, labelled as such (Creator answer 2); waiting 3 days or more
	// is amber, a visual cue only (answer 9). Only the tab goes in the URL.
	import { resolve } from '$app/paths';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount, tick } from 'svelte';
	import { ApiError, apiGet } from '../../../lib/api';
	import { leaveOnSignOut, signInConfigured, signOut, type Me } from '../../../lib/auth';
	import type { StaffApi } from '../../../lib/admin/catalogue';
	import {
		CLAIM_TABS,
		daysWaiting,
		decidedBy,
		howLabel,
		isClaimState,
		isFuturePath,
		looksLikeOrderRef,
		monthLabel,
		shortRef,
		STATE_CHEF,
		staffClaims,
		waitText,
		WAITING_AMBER_DAYS,
		when,
		whoContact,
		whoName,
		type ClaimState,
		type StaffClaim
	} from '../../../lib/admin/claims';
	import { claimsWaiting } from '../../../lib/admin/claimsWaiting.svelte';
	import { viewOf, type StaffView } from '../../../lib/admin/orders';
	import { staffApi } from '../../../lib/admin/session';
	import ChefIcon from '../../../lib/admin/ui/ChefIcon.svelte';
	import ChefShell from '../../../lib/admin/ui/ChefShell.svelte';

	const tab = $derived<ClaimState>(
		isClaimState(page.url.searchParams.get('tab'))
			? (page.url.searchParams.get('tab') as ClaimState)
			: 'pending'
	);

	let api: StaffApi | null = null;
	let me = $state<string | null>(null);
	let view = $state<StaffView | null>(null);
	let claims = $state<StaffClaim[]>([]);
	let next = $state<string | undefined>();
	let phase = $state<'loading' | 'ready' | 'error'>('loading');
	let more = $state<'idle' | 'loading' | 'error'>('idle');
	let problem = $state('');
	let rowsEl = $state<HTMLTableSectionElement>();
	let revision = 0;
	let mounted = true;
	const now = new Date();

	const EMPTY: Record<ClaimState, string> = {
		pending:
			'Nothing waiting. When a customer asks to add an order to their account, it shows here, oldest first.',
		approved: 'No orders added to accounts yet.',
		declined: 'No declined requests.',
		withdrawn: 'No withdrawn requests.'
	};

	async function ended(e: unknown): Promise<boolean> {
		if (e instanceof ApiError && e.signInRequired) {
			await signOut();
			await goto(resolve('/sign-in?next=/admin&ended=1'), { replaceState: true });
			return true;
		}
		if (e instanceof ApiError && e.code === 'ACCOUNT_DISABLED') {
			problem = 'This account is disabled.';
			return true;
		}
		return false;
	}

	/** Who is signed in (once): the queue is the chef's only. */
	async function role(): Promise<boolean> {
		if (view) return true;
		if (!api) return false;
		const who = await apiGet<Me>('/api/v1/admin/me', { baseUrl: api.baseUrl, token: api.token });
		view = viewOf(who.capabilities);
		me = who.account_id;
		if (!view) problem = 'This account has no staff access.';
		return view !== null;
	}

	async function load(state: ClaimState) {
		if (!api) return;
		const rev = ++revision;
		phase = 'loading';
		more = 'idle';
		try {
			if (!(await role()) || view !== 'chef') return;
			const first = await staffClaims(api, state);
			if (rev !== revision || !mounted) return;
			claims = first.claims;
			next = first.next_cursor;
			if (state === 'pending') claimsWaiting.set(first);
			phase = 'ready';
		} catch (e) {
			if (rev !== revision || !mounted || (await ended(e))) return;
			phase = 'error';
		}
	}

	async function showMore() {
		if (!api || !next || more === 'loading') return;
		const rev = revision;
		const before = claims.length;
		more = 'loading';
		try {
			const page = await staffClaims(api, tab, next);
			if (rev !== revision || !mounted) return;
			claims = [...claims, ...page.claims];
			next = page.next_cursor;
			more = 'idle';
			await tick();
			rowsEl?.querySelectorAll<HTMLAnchorElement>('tr a.btn')[before]?.focus();
		} catch (e) {
			if (rev !== revision || !mounted || (await ended(e))) return;
			more = 'error';
		}
	}

	// A tab is a link: load whenever the tab in the URL changes.
	let loadedTab: ClaimState | null = null;
	$effect(() => {
		const t = tab;
		if (api && t !== loadedTab) {
			loadedTab = t;
			void load(t);
		}
	});

	onMount(() => {
		(async () => {
			api = signInConfigured ? await staffApi() : null;
			if (!mounted) return;
			if (!api) {
				await goto(resolve('/sign-in?next=/admin'), { replaceState: true });
				return;
			}
			loadedTab = tab;
			await load(tab);
		})();
		const unsubscribe = leaveOnSignOut(() => {
			claims = [];
			void goto(resolve('/sign-in?next=/admin'), { replaceState: true });
		});
		return () => {
			mounted = false;
			unsubscribe();
		};
	});

	async function leave() {
		claims = [];
		const reached = await signOut();
		if (!reached) {
			problem =
				'You are signed out on this device, but the sign-in service could not be reached to end the session everywhere.';
			return;
		}
		await goto(resolve('/'), { replaceState: true });
	}

	const tabHref = (s: ClaimState) =>
		s === 'pending' ? resolve('/admin/requests') : `${resolve('/admin/requests')}?tab=${s}`;
	const tabLabel = $derived(CLAIM_TABS.find(([s]) => s === tab)?.[1] ?? '');
</script>

<svelte:head>
	<title>Requests to add orders · GK Cake London chef workspace</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<ChefShell active="claims" {view} onsignout={leave}>
	<header class="page-head">
		<div>
			<p class="eyebrow">Account requests · chef only · London time</p>
			<h1>Requests to add orders</h1>
			{#if view === 'chef'}
				<p class="muted lead">
					Customers ask to add an order to their account from “Add an older order”. Nothing is added
					until you add it, and nothing is ever matched automatically by email, payer, name or
					Instagram.
				</p>
			{/if}
		</div>
	</header>
	{#if problem}
		<div class="banner banner-bad" role="alert"><p>{problem}</p></div>
	{:else if view && view !== 'chef'}
		<div class="card">
			<p class="role-note">
				Requests to add orders to customer accounts are the chef’s only. Fulfilment and technical
				support don’t see them: the workspace refuses them, as the API does.
			</p>
		</div>
	{:else}
		<div class="banner banner-info">
			<ChefIcon name="info" cls="banner-icon" />
			<div>
				<p>
					<strong>Only website orders can be added for now.</strong> Instagram, email, phone and in-person
					orders aren’t in the system yet: recording older orders by hand comes later. Customers also
					send guest website orders through “Add an older order”, with the order’s reference; those you
					can find and add.
				</p>
			</div>
		</div>
		<section class="card" aria-labelledby="list-h">
			<h2 id="list-h" class="sr-only">Requests</h2>
			<nav class="chips tabs" aria-label="Request status">
				{#each CLAIM_TABS as [s, label] (s)}
					<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- tabHref resolve()s the route -->
					<a class="filter-chip tab" href={tabHref(s)} aria-current={s === tab ? 'page' : undefined}
						>{label}{#if s === 'pending' && claimsWaiting.label && claimsWaiting.label !== '0'}<span
								class="tab-count">{claimsWaiting.label}</span
							>{/if}</a
					>
				{/each}
			</nav>
			{#if phase === 'loading'}
				<div class="loading" role="status">
					<span class="spinner" aria-hidden="true"></span>Loading requests…
				</div>
				<div class="skeleton" aria-hidden="true"><span></span><span></span><span></span></div>
			{:else if phase === 'error'}
				<div class="banner banner-bad" role="alert">
					<ChefIcon name="alert" cls="banner-icon" />
					<div>
						<p>
							<strong>The requests couldn’t be loaded right now.</strong> Nothing was changed. Check your
							connection and try again.
						</p>
						<button class="btn btn-small" type="button" onclick={() => void load(tab)}
							>Try again</button
						>
					</div>
				</div>
			{:else if claims.length === 0}
				<div class="empty">
					<p class="strong">{EMPTY[tab]}</p>
					{#if tab === 'pending'}
						<p class="small muted">
							Today customers ask from their account with “Add an older order”: your reference for
							an Instagram, email, phone or in-person order, or the reference of a website order
							they placed as a guest. Asking from the order page comes later.
						</p>
					{/if}
				</div>
			{:else}
				<p class="small strong" aria-live="polite">
					{next
						? `Showing the oldest ${claims.length} · more below`
						: `${claims.length} ${claims.length === 1 ? 'request' : 'requests'}`} · {tab ===
					'pending'
						? 'oldest first'
						: 'oldest received first'}
				</p>
				<div class="table-wrap">
					<table class="table orders-table claims-table">
						<caption class="sr-only">{tabLabel} requests to add orders</caption>
						<thead>
							<tr>
								<th scope="col">Received</th>
								<th scope="col">Who <span class="th-note">(their profile)</span></th>
								<th scope="col">How</th>
								<th scope="col">Their note</th>
								<th scope="col">{tab === 'pending' ? 'Waiting' : 'Outcome'}</th>
								<th scope="col"><span class="sr-only">Action</span></th>
							</tr>
						</thead>
						<tbody bind:this={rowsEl}>
							{#each claims as c (c.id)}
								{@const outcome = STATE_CHEF[c.state] ?? [c.state, 'neutral']}
								<tr>
									<td data-label="Received"
										><div>
											{when(c.created_at)}<span class="block mono small muted"
												>Request {shortRef(c.id)}</span
											>
										</div></td
									>
									<th scope="row" data-label="Who"
										><div>
											{whoName(c)}<span class="block small muted">{whoContact(c)}</span>
										</div></th
									>
									<td data-label="How"
										><div>
											<span class="how-chip">{howLabel(c)}</span>{#if isFuturePath(c)}<span
													class="tag-future">Future</span
												>{#if c.order_id}<span class="block"
														>Website order <span class="mono">{shortRef(c.order_id)}</span></span
													>{/if}{:else}<span class="block"
													>Their reference: {#if c.reference}<span class="mono">{c.reference}</span
														>{:else}<span class="muted">not available</span>{/if}</span
												><span class="block muted small"
													>{monthLabel(c.approximate_date)
														? `Roughly ${monthLabel(c.approximate_date)}`
														: 'No month given'}</span
												>{#if c.state === 'pending' && looksLikeOrderRef(c.reference)}<span
														class="block small info-text">Looks like a website order reference</span
													>{/if}{/if}{#if c.state === 'approved' && c.order_id && !isFuturePath(c)}<span
													class="block small"
													>Added as order <span class="mono">{shortRef(c.order_id)}</span></span
												>{/if}
										</div></td
									>
									<td data-label="Their note"
										><div>
											{#if c.note}<p class="clamp">“{c.note}”</p>{:else}<span class="muted"
													>No note</span
												>{/if}
										</div></td
									>
									<td data-label={tab === 'pending' ? 'Waiting' : 'Outcome'}
										><div>
											{#if c.state === 'pending'}<span
													class="chip-s chip-{daysWaiting(c.created_at, now) >= WAITING_AMBER_DAYS
														? 'warn'
														: 'info'}">{waitText(c.created_at, now)}</span
												>{:else}<span class="chip-s chip-{outcome[1]}">{outcome[0]}</span><span
													class="block small muted"
													>{c.decided_at ? when(c.decided_at) : ''}{c.state === 'withdrawn'
														? ''
														: ` · by ${decidedBy(c, me)}`}</span
												>{#if c.decision_note}<span class="block small internal-note"
														><ChefIcon name="info" cls="note-icon" /><span class="sr-only"
															>Your internal note:
														</span>{c.decision_note}</span
													>{/if}{/if}
										</div></td
									>
									<td class="td-action"
										><a
											class="btn btn-small"
											class:btn-primary={c.state === 'pending'}
											href={resolve('/admin/requests/[id]', { id: c.id })}
											aria-label="{c.state === 'pending' ? 'Review' : 'View'} request {shortRef(
												c.id
											)} from {whoName(c)}">{c.state === 'pending' ? 'Review' : 'View'}</a
										></td
									>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
				{#if next}
					<div class="more-row">
						<button
							class="btn"
							type="button"
							disabled={more === 'loading'}
							onclick={() => void showMore()}
							>{more === 'loading' ? 'Loading…' : 'Show the next requests'}</button
						>
						{#if more === 'error'}<p class="small bad-text" role="alert">
								The next requests couldn’t be loaded. Try again.
							</p>{/if}
					</div>
				{/if}
			{/if}
			<p class="muted small foot">
				Name, Instagram and phone come from the customer’s profile: they typed them, nobody checked
				them, and they aren’t proof. The workspace doesn’t store the customer’s email address.
			</p>
		</section>
	{/if}
</ChefShell>
