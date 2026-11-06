<script lang="ts">
	// One request to add an order (Creator-approved GK-018 r04d-claims sketch,
	// request.html): what the customer sent, who is asking (their profile,
	// not proof) and, once decided, the outcome. The API has no single-request
	// read yet, so the request is found by paging the queue's four lists; a
	// request closed when its customer deleted their account is in none of
	// them. Chef only. Deciding comes in the next slice (GK-018 r04d build).
	import { resolve } from '$app/paths';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { ApiError, apiGet } from '../../../../lib/api';
	import { leaveOnSignOut, signInConfigured, signOut, type Me } from '../../../../lib/auth';
	import type { StaffApi } from '../../../../lib/admin/catalogue';
	import {
		decidedBy,
		findClaim,
		howLabel,
		isFuturePath,
		looksLikeOrderRef,
		monthLabel,
		shortRef,
		STATE_CHEF,
		waitText,
		when,
		whoName,
		type StaffClaim
	} from '../../../../lib/admin/claims';
	import { viewOf, type StaffView } from '../../../../lib/admin/orders';
	import { staffApi } from '../../../../lib/admin/session';
	import ChefIcon from '../../../../lib/admin/ui/ChefIcon.svelte';
	import ChefShell from '../../../../lib/admin/ui/ChefShell.svelte';

	const CUSTOMER_SEES: Record<string, string> = {
		pending: '“Waiting for the chef”',
		approved: '“Added to your account”',
		declined:
			'“Couldn’t add”: “The chef couldn’t add an order from the details given, so nothing was added. Please contact us if you think this is a mistake.”',
		withdrawn: '“Withdrawn”'
	};

	let api: StaffApi | null = null;
	let me = $state<string | null>(null);
	let view = $state<StaffView | null>(null);
	let claim = $state<StaffClaim | null>(null);
	let phase = $state<'loading' | 'ready' | 'missing' | 'capped' | 'error'>('loading');
	let problem = $state('');
	let mounted = true;
	const now = new Date();

	const pending = $derived(claim?.state === 'pending');
	const name = $derived(claim?.account.full_name?.trim() || null);
	const verb = $derived(pending ? 'asks' : 'asked');
	const backHref = $derived(
		claim && claim.state !== 'pending'
			? `${resolve('/admin/requests')}?tab=${claim.state}`
			: resolve('/admin/requests')
	);

	async function load() {
		if (!api) return;
		phase = 'loading';
		try {
			if (!view) {
				const who = await apiGet<Me>('/api/v1/admin/me', {
					baseUrl: api.baseUrl,
					token: api.token
				});
				view = viewOf(who.capabilities);
				me = who.account_id;
				if (!view) {
					problem = 'This account has no staff access.';
					return;
				}
			}
			if (view !== 'chef') return;
			const id = page.params.id ?? '';
			const found = await findClaim(api, id);
			if (!mounted || id !== (page.params.id ?? '')) return;
			claim = found.claim;
			phase = found.claim ? 'ready' : found.capped ? 'capped' : 'missing';
		} catch (e) {
			if (!mounted) return;
			if (e instanceof ApiError && e.signInRequired) {
				await signOut();
				await goto(resolve('/sign-in?next=/admin&ended=1'), { replaceState: true });
				return;
			}
			if (e instanceof ApiError && e.code === 'ACCOUNT_DISABLED') {
				problem = 'This account is disabled.';
				return;
			}
			phase = 'error';
		}
	}

	onMount(() => {
		(async () => {
			api = signInConfigured ? await staffApi() : null;
			if (!mounted) return;
			if (!api) {
				await goto(resolve('/sign-in?next=/admin'), { replaceState: true });
				return;
			}
			loadedId = page.params.id ?? '';
			await load();
		})();
		const unsubscribe = leaveOnSignOut(() => {
			claim = null;
			void goto(resolve('/sign-in?next=/admin'), { replaceState: true });
		});
		return () => {
			mounted = false;
			unsubscribe();
		};
	});

	// Another request in the same page (later: "Next waiting request") loads it.
	let loadedId: string | null = null;
	$effect(() => {
		const id = page.params.id ?? '';
		if (api && loadedId !== null && id !== loadedId) {
			loadedId = id;
			claim = null;
			void load();
		}
	});

	async function leave() {
		claim = null;
		const reached = await signOut();
		if (!reached) {
			problem =
				'You are signed out on this device, but the sign-in service could not be reached to end the session everywhere.';
			return;
		}
		await goto(resolve('/'), { replaceState: true });
	}
</script>

<svelte:head>
	<title>Request to add an order · GK Cake London chef workspace</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<ChefShell active="claims" {view} onsignout={leave}>
	<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- backHref resolve()s the route -->
	<a class="back" href={backHref}>← Requests to add orders</a>
	{#if problem}
		<div class="banner banner-bad" role="alert"><p>{problem}</p></div>
	{:else if view && view !== 'chef'}
		<header class="page-head">
			<div>
				<p class="eyebrow">Account requests</p>
				<h1>Request to add an order</h1>
			</div>
		</header>
		<div class="card">
			<p class="role-note">
				Requests to add orders to customer accounts are the chef’s only. Fulfilment and technical
				support don’t see them: the workspace refuses them, as the API does.
			</p>
		</div>
	{:else if phase === 'loading'}
		<div class="loading" role="status">
			<span class="spinner" aria-hidden="true"></span>Loading the request…
		</div>
	{:else if phase === 'error'}
		<header class="page-head"><div><h1>Request to add an order</h1></div></header>
		<div class="banner banner-bad" role="alert">
			<ChefIcon name="alert" cls="banner-icon" />
			<div>
				<p>
					<strong>The request couldn’t be loaded right now.</strong> Nothing was changed. Check your connection
					and try again.
				</p>
				<button class="btn btn-small" type="button" onclick={() => void load()}>Try again</button>
			</div>
		</div>
	{:else if phase === 'capped'}
		<div class="card empty">
			<h1>Request not found in the lists</h1>
			<p>
				It may be further down a long list than this page reads. Open it from the list of requests
				instead.
			</p>
			<a class="btn" href={resolve('/admin/requests')}>Back to requests</a>
		</div>
	{:else if phase === 'missing' || !claim}
		<div class="card empty">
			<h1>Request not found</h1>
			<p>
				It isn’t in any list. A request is closed and taken off the lists when the customer deletes
				their account.
			</p>
			<a class="btn" href={resolve('/admin/requests')}>Back to requests</a>
		</div>
	{:else}
		{@const c = claim}
		{@const state = STATE_CHEF[c.state] ?? [c.state, 'neutral']}
		<header class="page-head">
			<div>
				<p class="eyebrow">
					Request <span class="mono">{shortRef(c.id)}</span> · {pending
						? waitText(c.created_at, now).toLowerCase()
						: state[0].toLowerCase()}
				</p>
				<h1>
					{name ? `${name} ${verb}` : `A customer ${verb}`} to add {isFuturePath(c)
						? 'an order'
						: 'an older order'}
				</h1>
			</div>
			<span class="chip-s chip-{state[1]}">{state[0]}</span>
		</header>
		<div class="claim-grid">
			<div>
				{#if !pending}
					<section class="card" aria-labelledby="out-h">
						<h2 id="out-h" class="card-h">Outcome</h2>
						<dl class="facts">
							<div>
								<dt>{c.state === 'withdrawn' ? 'Withdrawn' : 'Decided'}</dt>
								<dd>
									{c.decided_at ? when(c.decided_at) : ''}{c.state === 'withdrawn'
										? ' by the customer'
										: ` by ${decidedBy(c, me)}`}
								</dd>
							</div>
							{#if c.state === 'approved' && c.order_id}
								<div>
									<dt>Order added</dt>
									<dd><span class="mono">{shortRef(c.order_id)}</span></dd>
								</div>
							{/if}
							{#if c.state === 'approved' || c.state === 'declined'}
								<div>
									<dt>Your internal note</dt>
									<dd>
										{#if c.decision_note}{c.decision_note}{:else}<span class="muted">None</span
											>{/if}
									</dd>
								</div>
							{/if}
							<div>
								<dt>The customer sees</dt>
								<dd>{CUSTOMER_SEES[c.state] ?? ''}</dd>
							</div>
						</dl>
						{#if c.state === 'approved'}
							<p class="small muted">
								When it was added, guest access to that order stopped working; they see it by
								signing in. Removing an order from an account isn’t built.
							</p>
						{:else if c.state === 'withdrawn'}
							<p class="small muted">Nothing was added. A withdrawn request can’t be decided.</p>
						{/if}
					</section>
				{/if}
				<section class="card" aria-labelledby="sent-h">
					<h2 id="sent-h" class="card-h">What they sent</h2>
					<dl class="facts">
						<div>
							<dt>Received</dt>
							<dd>{when(c.created_at)}</dd>
						</div>
						{#if isFuturePath(c)}
							<div>
								<dt>How they asked</dt>
								<dd>
									<span class="how-chip">From the order page or email</span><span class="tag-future"
										>Future</span
									>
								</dd>
							</div>
							{#if c.order_id}
								<div>
									<dt>The order they asked from</dt>
									<dd><span class="mono">{shortRef(c.order_id)}</span></dd>
								</div>
							{/if}
						{:else}
							<div>
								<dt>How they ordered</dt>
								<dd><span class="how-chip">{howLabel(c)}</span></dd>
							</div>
							<div>
								<dt>Their reference</dt>
								<dd>
									{#if c.reference}<span class="mono strong">{c.reference}</span
										>{#if pending && looksLikeOrderRef(c.reference)}<span
												class="block small info-text">Looks like a website order reference</span
											>{/if}{:else}<span class="muted">Not available</span>{/if}
								</dd>
							</div>
							<div>
								<dt>Roughly when</dt>
								<dd>
									{#if monthLabel(c.approximate_date)}{monthLabel(c.approximate_date)}{:else}<span
											class="muted">Not given</span
										>{/if}
								</dd>
							</div>
						{/if}
						<div>
							<dt>Their note</dt>
							<dd>
								{#if c.note}<q>{c.note}</q>{:else}<span class="muted">No note</span>{/if}
							</dd>
						</div>
					</dl>
					{#if isFuturePath(c)}
						<p class="small muted">
							They asked while signed in, from the order’s guest page. Having guest access shows
							they can see the order; it doesn’t prove the order is theirs: anyone with the link or
							that browser could ask.
						</p>
					{:else}
						<p class="small muted">
							The reference is whatever they typed: yours from a message or receipt, or the “Order
							reference” from a website order’s page. It isn’t checked against anything.
						</p>
					{/if}
				</section>
				<section class="card" aria-labelledby="who-h">
					<h2 id="who-h" class="card-h">Who’s asking · from their profile</h2>
					<dl class="facts">
						<div>
							<dt>Name</dt>
							<dd>
								{#if name}{whoName(c)}{:else}<span class="muted">Not given</span>{/if}
							</dd>
						</div>
						<div>
							<dt>Instagram</dt>
							<dd>
								{#if c.account.instagram}<span class="mono">{c.account.instagram}</span>{:else}<span
										class="muted">Not given</span
									>{/if}
							</dd>
						</div>
						<div>
							<dt>Phone</dt>
							<dd>
								{#if c.account.phone}<span class="mono">{c.account.phone}</span>{:else}<span
										class="muted">Not given</span
									>{/if}
							</dd>
						</div>
					</dl>
					<p class="small muted">
						Typed by the customer in their profile. Not checked and not proof of who they are. Their
						email address isn’t shown: the workspace doesn’t store it.
					</p>
				</section>
			</div>
			<div>
				<section class="card" aria-labelledby="dec-h">
					<h2 id="dec-h" class="card-h">Your decision</h2>
					{#if pending}
						<p>Adding or declining requests from the workspace comes in the next update.</p>
						<p class="small muted">
							Until then the customer keeps seeing “Waiting for the chef”. Nothing has been added.
						</p>
					{:else}
						<p>
							{c.state === 'withdrawn'
								? 'The customer withdrew this request.'
								: `Already decided: “${state[0]}”.`} Nothing to decide.
						</p>
						<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- backHref resolve()s the route -->
						<a class="btn btn-small" href={backHref}>Back to the list</a>
					{/if}
				</section>
			</div>
		</div>
	{/if}
</ChefShell>
