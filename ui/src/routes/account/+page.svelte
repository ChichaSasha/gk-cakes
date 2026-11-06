<script lang="ts">
	// Your account (approved GK-016 r02b sketch, account home): your orders
	// (In progress, and Past behind "Show past orders"), your requests to add
	// older orders (each opens its own page), favourites on this device and
	// "Ordered before?" (Add an older order). Everything comes from the
	// account API with this browser's session (K00); a failed orders read
	// leaves the other cards working. No analytics run here (K10).
	import { resolve } from '$app/paths';
	import { goto } from '$app/navigation';
	import { onMount, tick } from 'svelte';
	import { signOut } from '../../lib/auth';
	import {
		me as readMe,
		myClaims,
		myOrders,
		type AccountApi,
		type AccountOrder,
		type Claim,
		type Me
	} from '../../lib/account/client';
	import { claimView } from '../../lib/account/claims';
	import {
		accountProblem,
		accountUnusable,
		endedSession,
		leaveWhenSignedOut,
		toSignIn
	} from '../../lib/account/guard';
	import { groupOrders } from '../../lib/account/orders';
	import { accountApi, signedInEmail } from '../../lib/account/session';
	import AccountShell from '../../lib/account/ui/AccountShell.svelte';
	import OrderRow from '../../lib/account/ui/OrderRow.svelte';
	import { favourites } from '../../lib/shop/favourites.svelte';

	let api: AccountApi | null = null;
	let email = $state<string>();
	let account = $state<Me | null>(null);
	let blocked = $state('');
	// The account read failed for a reason that may pass (not disabled or deleted).
	let meProblem = $state('');
	let reloading = $state(false);
	let orders = $state<AccountOrder[] | null>(null);
	let ordersProblem = $state('');
	let claims = $state<Claim[] | null>(null);
	let claimsProblem = $state('');
	let showPast = $state(false);
	let retrying = $state(false);
	let signOutProblem = $state('');

	const grouped = $derived(orders ? groupOrders(orders) : null);
	const saved = $derived(favourites.state.ids.length);

	async function loadOrders() {
		if (!api) return;
		try {
			orders = await myOrders(api);
			ordersProblem = '';
		} catch (e) {
			if (await endedSession(e)) return;
			orders = null;
			ordersProblem = accountProblem(e, 'your orders');
		}
	}

	async function loadClaims() {
		if (!api) return;
		try {
			claims = await myClaims(api);
			claimsProblem = '';
		} catch (e) {
			if (await endedSession(e)) return;
			claims = null;
			claimsProblem = accountProblem(e, 'your requests');
		}
	}

	// The Try again button stays while it works ("Trying again…"); focus
	// returns to the card when the orders arrive, or to the button.
	async function retryOrders() {
		retrying = true;
		await loadOrders();
		retrying = false;
		await tick();
		document.getElementById(orders ? 'orders-h' : 'orders-retry')?.focus();
	}

	let retryingClaims = $state(false);
	async function retryClaims() {
		retryingClaims = true;
		await loadClaims();
		retryingClaims = false;
		await tick();
		// No requests left to show: the card goes away, so focus the next card.
		document
			.getElementById(
				claims ? (claims.length > 0 ? 'requests-h' : 'favourites-h') : 'requests-retry'
			)
			?.focus();
	}

	/** Reads the account, then its orders and requests. */
	async function loadAccount(): Promise<void> {
		if (!api) return;
		try {
			account = await readMe(api);
			meProblem = '';
		} catch (e) {
			if (await endedSession(e)) return;
			if (accountUnusable(e)) blocked = accountProblem(e, 'your account');
			else meProblem = accountProblem(e, 'your account');
			return;
		}
		await Promise.all([loadOrders(), loadClaims()]);
	}

	async function retryAccount() {
		reloading = true;
		await loadAccount();
		reloading = false;
		await tick();
		document.querySelector<HTMLElement>(account ? 'h1' : '#account-retry')?.focus();
	}

	onMount(() => {
		let active = true;
		(async () => {
			api = await accountApi();
			if (!active) return;
			if (!api) {
				await toSignIn();
				return;
			}
			email = await signedInEmail();
			if (!active) return;
			await loadAccount();
		})();
		const unsubscribe = leaveWhenSignedOut(() => {
			account = null;
			orders = null;
			claims = null;
		});
		return () => {
			active = false;
			unsubscribe();
		};
	});

	async function leave() {
		const reached = await signOut();
		account = null;
		orders = null;
		claims = null;
		if (!reached) {
			signOutProblem =
				'You are signed out on this device, but the sign-in service could not be reached to end the session everywhere.';
			return;
		}
		await goto(resolve('/'), { replaceState: true });
	}

	// A disclosure: focus stays on the button (aria-expanded tells the state).
	function togglePast() {
		showPast = !showPast;
	}
</script>

<svelte:head>
	<title>Your account · GK Cake London</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<AccountShell
	active="orders"
	sub={email ? `Signed in as ${email}` : undefined}
	onsignout={account || blocked || meProblem ? leave : undefined}
>
	{#if signOutProblem}
		<div class="notice error" role="alert"><div><p>{signOutProblem}</p></div></div>
	{:else if blocked}
		<div class="notice error" role="alert"><div><p>{blocked}</p></div></div>
		<div class="acc-actions">
			<a class="button" href="mailto:gk.cake.london@gmail.com">Contact us</a>
			<button class="btn" type="button" onclick={leave}>Sign out</button>
		</div>
	{:else if meProblem}
		<div class="notice error" role="alert"><div><p>{meProblem}</p></div></div>
		<div class="acc-actions">
			<button
				class="btn"
				id="account-retry"
				type="button"
				disabled={reloading}
				aria-busy={reloading}
				onclick={retryAccount}>{reloading ? 'Trying again…' : 'Try again'}</button
			>
		</div>
	{:else if !account}
		<p role="status" aria-busy="true">Loading your account…</p>
	{:else}
		{#if account.capabilities.length > 0}
			<section class="acc-card" aria-labelledby="staff-h">
				<div class="acc-cta">
					<div>
						<h2 class="acc-h" id="staff-h">Staff access</h2>
						<p class="small">This account can also use the staff area.</p>
					</div>
					<a class="button acc-small-btn" href={resolve('/admin')}>Open staff area</a>
				</div>
			</section>
		{/if}

		<section class="acc-card" id="orders" aria-labelledby="orders-h">
			<h2 class="acc-h" id="orders-h" tabindex="-1">Your orders</h2>
			{#if ordersProblem}
				<div class="notice error" role="alert"><div><p>{ordersProblem}</p></div></div>
				<div class="acc-actions">
					<button
						class="btn"
						id="orders-retry"
						type="button"
						disabled={retrying}
						aria-busy={retrying}
						onclick={retryOrders}>{retrying ? 'Trying again…' : 'Try again'}</button
					>
				</div>
			{:else if !grouped}
				<p role="status" aria-busy="true">Loading your orders…</p>
			{:else if grouped.current.length + grouped.past.length === 0}
				<div class="acc-empty">
					<h3>No orders yet</h3>
					<p class="small">
						When you order while signed in, your orders appear here with their status.
					</p>
					<div class="acc-actions">
						<a class="button primary" href={resolve('/(shop)/shop')}
							>Shop cakes <span class="arrow" aria-hidden="true">→</span></a
						>
					</div>
				</div>
			{:else}
				<h3 class="acc-sub">In progress</h3>
				{#each grouped.current as row (row.order.id)}
					<OrderRow order={row.order} item={row.item} />
				{:else}
					<p class="small">Nothing in progress.</p>
				{/each}
				{#if grouped.past.length > 0}
					<h3 class="acc-sub">Past orders</h3>
					<button
						class="btn acc-small-btn acc-past-toggle"
						type="button"
						aria-expanded={showPast}
						aria-controls="past-orders"
						onclick={togglePast}
						>{showPast ? 'Hide past orders' : `Show past orders (${grouped.past.length})`}</button
					>
					<div id="past-orders" hidden={!showPast}>
						{#each grouped.past as row (row.order.id)}
							<OrderRow order={row.order} item={row.item} />
						{/each}
					</div>
				{/if}
			{/if}
			<p class="small acc-foot">
				Can’t see an order? <a href={resolve('/account/older')}>Add an older order</a>. We never add
				orders to your account automatically, even ones made with this email address.
			</p>
		</section>

		{#if claimsProblem || (claims && claims.length > 0)}
			<section class="acc-card" id="requests" aria-labelledby="requests-h">
				<h2 class="acc-h" id="requests-h" tabindex="-1">Your requests</h2>
				{#if claimsProblem}
					<div class="notice error" role="alert"><div><p>{claimsProblem}</p></div></div>
					<div class="acc-actions">
						<button
							class="btn"
							id="requests-retry"
							type="button"
							disabled={retryingClaims}
							aria-busy={retryingClaims}
							onclick={retryClaims}>{retryingClaims ? 'Trying again…' : 'Try again'}</button
						>
					</div>
				{:else if claims}
					{#each claims.map((c) => ({ id: c.id, ...claimView(c) })) as c (c.id)}
						<article class="acc-req">
							<p class="acc-ref">Request {c.ref}</p>
							<h3>{c.title}</h3>
							<p><span class="acc-chip {c.chip.tone}">{c.chip.text}</span></p>
							<p class="small">
								{c.line}
								<a
									href={resolve('/account/requests/[id]', { id: c.id })}
									aria-label={`View request ${c.ref}`}>View →</a
								>
							</p>
						</article>
					{/each}
				{/if}
			</section>
		{/if}

		<section class="acc-card" id="favourites" aria-labelledby="favourites-h">
			<h2 class="acc-h" id="favourites-h" tabindex="-1">Your favourites</h2>
			{#if !favourites.loaded}
				<p role="status">Loading…</p>
			{:else if saved > 0}
				<p class="small">
					{saved}
					{saved === 1 ? 'cake is' : 'cakes are'} saved on this device. Favourites stay on this device
					for a year.
				</p>
				<a class="text-link" href={resolve('/(shop)/favourites')}>View your favourites →</a>
			{:else}
				<p class="small">Nothing saved yet. Tap the heart on a cake to save it on this device.</p>
				<a class="text-link" href={resolve('/(shop)/shop')}>Browse cakes →</a>
			{/if}
		</section>

		<section class="acc-card" id="older" aria-labelledby="older-h">
			<div class="acc-cta">
				<div>
					<h2 class="acc-h" id="older-h">Ordered before?</h2>
					<p class="small">
						Add an order you made on Instagram, by email or in person, with the chef’s reference.
						The chef checks it first.
					</p>
				</div>
				<a class="button acc-small-btn" href={resolve('/account/older')}>Add an older order</a>
			</div>
		</section>
	{/if}
</AccountShell>
