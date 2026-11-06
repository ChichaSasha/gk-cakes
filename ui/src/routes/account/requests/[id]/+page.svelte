<script lang="ts">
	// One request to add an order (approved GK-016 r02b sketch, states 40–42):
	// waiting for the chef (what the customer sent, and Withdraw this request
	// with the version seen and one Idempotency-Key per withdrawal), added
	// (the order, only once approved), couldn't add (nothing about any order,
	// K00) or withdrawn. Never the chef's note. Another account's request, or
	// none, is a plain "not found". No analytics (K10).
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { goto } from '$app/navigation';
	import { onMount, tick } from 'svelte';
	import { ApiError } from '../../../../lib/api';
	import {
		myClaim,
		withdrawClaim,
		type AccountApi,
		type Claim
	} from '../../../../lib/account/client';
	import { approximateLabel, claimRef, claimView } from '../../../../lib/account/claims';
	import {
		accountProblem,
		accountUnusable,
		endedSession,
		leaveWhenSignedOut,
		signOutHere,
		toSignIn
	} from '../../../../lib/account/guard';
	import {
		channelLabel,
		isClaimId,
		maybeArrived,
		WITHDRAW_PROBLEM
	} from '../../../../lib/account/older';
	import { orderRef } from '../../../../lib/account/orders';
	import { SaveKey } from '../../../../lib/account/profile';
	import { accountApi, signedInEmail } from '../../../../lib/account/session';
	import AccountShell from '../../../../lib/account/ui/AccountShell.svelte';

	// The API's IDs are lowercase; a link typed in capitals still works.
	const claimId = (page.params.id ?? '').toLowerCase();
	let api: AccountApi | null = null;
	const keys = new SaveKey();
	let email = $state<string>();
	let claim = $state<Claim | null>(null);
	let missing = $state(false);
	let blocked = $state('');
	let loadProblem = $state('');
	let retrying = $state(false);
	let withdrawing = $state(false);
	let withdrawProblem = $state('');
	/** Withdrawn by this page just now (confirmed in the status region). */
	let withdrawnNow = $state(false);
	/** The withdrawal was refused because the request had changed meanwhile. */
	let changed = $state(false);
	let signOutProblem = $state('');

	const view = $derived(claim ? claimView(claim) : null);
	const ref = $derived(claim ? claimRef(claim) : '');
	const when = $derived(claim ? approximateLabel(claim.approximate_date) : undefined);
	const title = $derived(claim?.path === 'email_link' ? 'Add an order' : 'Add an older order');

	function clear() {
		claim = null;
		withdrawnNow = false;
		changed = false;
	}

	async function focus(id: string) {
		await tick();
		document.getElementById(id)?.focus();
	}

	/** Reads the request; true when it is shown. */
	async function load(): Promise<boolean> {
		if (!api) return false;
		if (!isClaimId(claimId)) {
			missing = true;
			return false;
		}
		try {
			claim = await myClaim(api, claimId);
			loadProblem = '';
			return true;
		} catch (e) {
			if (await endedSession(e)) return false;
			claim = null;
			if (e instanceof ApiError && e.status === 404) missing = true;
			else if (accountUnusable(e)) blocked = accountProblem(e, 'your request');
			else loadProblem = accountProblem(e, 'this request');
			return false;
		}
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
			// The state's notice takes focus, as the sketch does (also after sending).
			if (active && (await load())) await focus('claim-status');
		})();
		const unsubscribe = leaveWhenSignedOut(clear);
		return () => {
			active = false;
			unsubscribe();
		};
	});

	async function retryLoad() {
		retrying = true;
		const ok = await load();
		retrying = false;
		await focus(ok ? 'claim-h' : missing ? 'rq-h' : 'rq-retry');
	}

	async function withdraw() {
		if (!api || !claim || withdrawing) return;
		withdrawProblem = '';
		changed = false;
		const body = { claim: claim.id, expected_version: claim.version };
		withdrawing = true;
		try {
			claim = await withdrawClaim(api, keys.for(body), claim.id, claim.version);
			keys.forget();
			withdrawing = false;
			withdrawnNow = true;
			await focus('rq-done');
		} catch (e) {
			withdrawing = false;
			if (await endedSession(e)) return;
			if (accountUnusable(e)) {
				keys.forget();
				claim = null;
				blocked = accountProblem(e, 'your request');
				// The Withdraw button (focused) is gone: focus the message.
				await focus('rq-blocked');
				return;
			}
			if (e instanceof ApiError && (e.code === 'STALE_VERSION' || e.code === 'CONFLICT')) {
				// The chef answered it (or another tab withdrew it) meanwhile: show the latest.
				keys.forget();
				if (await load()) {
					changed = true;
					await focus('rq-changed');
				}
				return;
			}
			if (e instanceof ApiError && e.status === 404) {
				keys.forget();
				claim = null;
				missing = true;
				await focus('rq-h');
				return;
			}
			// A lost answer may still have arrived: the same withdrawal keeps its key.
			if (!maybeArrived(e)) keys.forget();
			withdrawProblem = WITHDRAW_PROBLEM;
			await focus('rq-problem');
		}
	}

	async function leave() {
		const reached = await signOutHere();
		clear();
		if (!reached) {
			signOutProblem =
				'You are signed out on this device, but the sign-in service could not be reached to end the session everywhere.';
			return;
		}
		await goto(resolve('/'), { replaceState: true });
	}
</script>

<svelte:head>
	<title>{title} · GK Cake London</title>
	<meta name="robots" content="noindex" />
</svelte:head>

{#snippet sent(c: Claim)}
	<h3 class="acc-h3">What you sent</h3>
	<dl class="acc-dl">
		{#if c.reference}
			<div>
				<dt>Chef’s reference</dt>
				<dd>{c.reference}</dd>
			</div>
		{/if}
		<div>
			<dt>How you ordered</dt>
			<dd>{channelLabel(c.channel)}</dd>
		</div>
		{#if when}
			<div>
				<dt>When the cake was for</dt>
				<dd>{when}</dd>
			</div>
		{/if}
		{#if c.note}
			<div>
				<dt>Your note</dt>
				<dd class="acc-note">“{c.note}”</dd>
			</div>
		{/if}
	</dl>
{/snippet}

{#snippet back()}
	<a class="button" href={resolve('/account')}>Back to your account</a>
{/snippet}

<AccountShell
	active="request"
	{title}
	sub={email ? `Signed in as ${email}` : undefined}
	onsignout={claim || missing || blocked || loadProblem ? leave : undefined}
>
	{#if signOutProblem}
		<div class="notice error" role="alert"><div><p>{signOutProblem}</p></div></div>
	{:else if blocked}
		<div class="notice error" role="alert" id="rq-blocked" tabindex="-1">
			<div><p>{blocked}</p></div>
		</div>
		<div class="acc-actions">
			<a class="button" href="mailto:gk.cake.london@gmail.com">Contact us</a>
			<button class="btn" type="button" onclick={leave}>Sign out</button>
		</div>
	{:else if missing}
		<section class="acc-card" aria-labelledby="rq-h">
			<h2 class="acc-h" id="rq-h" tabindex="-1">Request not found</h2>
			<p>We can’t find this request in your account.</p>
			<div class="acc-actions">{@render back()}</div>
		</section>
	{:else if loadProblem}
		<div class="notice error" role="alert"><div><p>{loadProblem}</p></div></div>
		<div class="acc-actions">
			<button
				class="btn"
				id="rq-retry"
				type="button"
				disabled={retrying}
				aria-busy={retrying}
				onclick={retryLoad}>{retrying ? 'Trying again…' : 'Try again'}</button
			>
		</div>
	{:else if !claim || !view}
		<p role="status" aria-busy="true">Loading your request…</p>
	{:else}
		<div role="status">
			{#if withdrawnNow}
				<div class="notice success" id="rq-done" tabindex="-1">
					<div><p>Request withdrawn. Nothing was added to your account.</p></div>
				</div>
			{/if}
		</div>
		{#if changed}
			<div class="notice" id="rq-changed" role="alert" tabindex="-1">
				<div>
					<p>
						This request changed before it could be withdrawn (the chef may have answered it, or it
						was withdrawn in another tab). Its latest state is shown below.
					</p>
				</div>
			</div>
		{/if}
		<section class="acc-card" id="claim" aria-labelledby="claim-h">
			{#if claim.state === 'pending'}
				<h2 class="acc-h" id="claim-h" tabindex="-1">Sent to the chef</h2>
				<div class="notice" id="claim-status" role="status" tabindex="-1">
					<div>
						<p>
							<strong>Your request {ref} is waiting for the chef.</strong>
							{#if claim.path === 'email_link'}
								{view.line}
							{:else}
								Nothing has been added to your account yet, and we won’t show any order details
								until she has checked it.
							{/if}
						</p>
						<p>You’ll see the result here and under “Your requests” in your account.</p>
					</div>
				</div>
				{@render sent(claim)}
				<div class="acc-actions">
					{@render back()}
					<button
						class="btn"
						id="rq-withdraw"
						type="button"
						disabled={withdrawing}
						aria-busy={withdrawing}
						onclick={withdraw}>{withdrawing ? 'Withdrawing…' : 'Withdraw this request'}</button
					>
				</div>
				{#if withdrawProblem}
					<div class="notice error" id="rq-problem" role="alert" tabindex="-1">
						<div><p>{withdrawProblem}</p></div>
					</div>
				{/if}
			{:else if claim.state === 'approved' && claim.order_id}
				<h2 class="acc-h" id="claim-h" tabindex="-1">Added to your account</h2>
				<div class="notice success" id="claim-status" role="status" tabindex="-1">
					<div>
						<p>
							<strong>The chef matched your request {ref}</strong> and added order {orderRef({
								id: claim.order_id
							})} to your account.
						</p>
					</div>
				</div>
				<div class="acc-actions">
					<a class="button primary" href={resolve('/orders/[id]', { id: claim.order_id })}
						>View order {orderRef({ id: claim.order_id })}
						<span class="arrow" aria-hidden="true">→</span></a
					>
					{@render back()}
				</div>
			{:else if claim.state === 'declined'}
				<h2 class="acc-h" id="claim-h" tabindex="-1">We couldn’t add this order</h2>
				<div class="notice error" id="claim-status" role="alert" tabindex="-1">
					<div>
						<p>
							The chef couldn’t match request {ref} from the details given, so nothing was added.
						</p>
						<p>
							Please contact us and we’ll help. Email
							<a href="mailto:gk.cake.london@gmail.com">gk.cake.london@gmail.com</a> or message
							<a href="https://www.instagram.com/gk.cake.london/" rel="noopener noreferrer"
								>@gk.cake.london</a
							> on Instagram.
						</p>
					</div>
				</div>
				<div class="acc-actions">
					<a class="button" href="mailto:gk.cake.london@gmail.com">Contact us</a>
					{@render back()}
				</div>
			{:else}
				<!-- Withdrawn, or a state this page doesn't know: the account list's words. -->
				<h2 class="acc-h" id="claim-h" tabindex="-1">
					{claim.state === 'withdrawn' ? 'Withdrawn' : 'Your request'}
				</h2>
				<p class="acc-ref">Request {ref}</p>
				<p><span class="acc-chip {view.chip.tone}">{view.chip.text}</span></p>
				<p class="small">{view.line}</p>
				{#if claim.state === 'withdrawn'}{@render sent(claim)}{/if}
				<div class="acc-actions">{@render back()}</div>
			{/if}
		</section>
	{/if}
</AccountShell>
