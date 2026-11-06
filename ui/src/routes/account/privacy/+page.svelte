<script lang="ts">
	// Privacy & your data (approved GK-016 r02b sketch, states 48 and 57–59):
	// how long we keep what (K09 and the owner's GK-005 rules, as r02b words
	// them; payment records "being confirmed"), sign out everywhere (the
	// provider's global sign-out, started here; other devices follow when
	// their session next checks, never claimed as instant), delete the account
	// (/account/delete) and clear this device. No analytics run here (K10).
	import { resolve } from '$app/paths';
	import { goto } from '$app/navigation';
	import { onMount, tick } from 'svelte';
	import { auth, signOut } from '../../../lib/auth';
	import { me as readMe, type AccountApi, type Me } from '../../../lib/account/client';
	import {
		accountProblem,
		accountUnusable,
		endedSession,
		leaveWhenSignedOut,
		toSignIn
	} from '../../../lib/account/guard';
	import { clearDevice, RETENTION } from '../../../lib/account/privacy';
	import { accountApi, signedInEmail } from '../../../lib/account/session';
	import AccountShell from '../../../lib/account/ui/AccountShell.svelte';
	import { emptyBag } from '../../../lib/shop/bag';
	import { bag } from '../../../lib/shop/bag.svelte';
	import { emptyFavourites } from '../../../lib/shop/favourites';
	import { favourites } from '../../../lib/shop/favourites.svelte';

	type View = 'overview' | 'everywhere' | 'everywhere-done' | 'everywhere-partial';

	let api: AccountApi | null = null;
	let email = $state<string>();
	let account = $state<Me | null>(null);
	let blocked = $state('');
	let meProblem = $state('');
	let reloading = $state(false);
	let view = $state<View>('overview');
	let signingOut = $state(false);
	let cleared = $state('');
	let signOutProblem = $state('');
	let unsubscribe: (() => void) | undefined;

	const signedOut = $derived(view === 'everywhere-done' || view === 'everywhere-partial');

	async function focus(id: string) {
		await tick();
		document.getElementById(id)?.focus();
	}

	async function loadAccount(): Promise<void> {
		if (!api) return;
		try {
			account = await readMe(api);
			meProblem = '';
		} catch (e) {
			if (await endedSession(e)) return;
			if (accountUnusable(e)) blocked = accountProblem(e, 'your account');
			else meProblem = accountProblem(e, 'your account');
		}
	}

	async function retryAccount() {
		reloading = true;
		await loadAccount();
		reloading = false;
		await tick();
		document.querySelector<HTMLElement>(account ? 'h1' : '#pv-retry')?.focus();
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
			if (active) await loadAccount();
		})();
		unsubscribe = leaveWhenSignedOut(() => {
			account = null;
		});
		return () => {
			active = false;
			unsubscribe?.();
		};
	});

	async function askEverywhere() {
		cleared = '';
		view = 'everywhere';
		await focus('everywhere-h');
	}

	async function cancelEverywhere() {
		view = 'overview';
		await focus('pv-everywhere');
	}

	/**
	 * Ends every session of this account at the provider (scope 'global') and
	 * this device's. The provider SDK always clears this device; when the
	 * provider can't be reached, the other devices may stay signed in until
	 * their sessions expire, and the page says so (the built wording).
	 */
	async function signOutEverywhere() {
		if (signingOut) return;
		signingOut = true;
		// This page reports the result itself: don't leave for sign-in.
		unsubscribe?.();
		unsubscribe = undefined;
		let reached = false;
		try {
			const { error } = await auth().auth.signOut({ scope: 'global' });
			reached = !error;
		} catch {
			await signOut().catch(() => false);
		}
		account = null;
		email = undefined;
		signingOut = false;
		view = reached ? 'everywhere-done' : 'everywhere-partial';
		await focus('pv-signed-out');
	}

	function clearThisDevice() {
		const ok = clearDevice(storage('localStorage'), storage('sessionStorage'));
		favourites.state = emptyFavourites();
		bag.state = emptyBag();
		cleared = ok
			? 'Cleared the favourites and bag saved in this browser.'
			: 'This browser didn’t let us clear everything. You can clear this site’s data in your browser’s settings.';
	}

	function storage(name: 'localStorage' | 'sessionStorage'): Storage | null {
		try {
			return window[name];
		} catch {
			return null;
		}
	}

	async function leave() {
		unsubscribe?.();
		const reached = await signOut();
		account = null;
		if (!reached) {
			signOutProblem =
				'You are signed out on this device, but the sign-in service could not be reached to end the session everywhere.';
			return;
		}
		await goto(resolve('/'), { replaceState: true });
	}
</script>

<svelte:head>
	<title>Privacy &amp; your data · GK Cake London</title>
	<meta name="robots" content="noindex" />
</svelte:head>

{#snippet signInAgain()}
	<div class="acc-actions">
		<a class="button primary" href={resolve('/sign-in?next=/account')}
			>Sign in again <span class="arrow" aria-hidden="true">→</span></a
		>
	</div>
{/snippet}

{#if signedOut}
	<AccountShell
		active="privacy"
		nav={false}
		title={view === 'everywhere-done' ? 'Signed out everywhere' : 'Signed out on this device'}
	>
		{#if view === 'everywhere-done'}
			<div class="notice success" id="pv-signed-out" role="status" tabindex="-1">
				<div>
					<p>You’re signed out on all your devices.</p>
					<p>
						Other devices are asked to sign in again when they next check, usually within an hour.
					</p>
				</div>
			</div>
		{:else}
			<div class="notice error" id="pv-signed-out" role="alert" tabindex="-1">
				<div>
					<p>
						You are signed out on this device, but the sign-in service could not be reached to end
						the session everywhere.
					</p>
					<p>Sign in again and try “Sign out everywhere” later.</p>
				</div>
			</div>
		{/if}
		{@render signInAgain()}
	</AccountShell>
{:else}
	<AccountShell
		active="privacy"
		title="Privacy & your data"
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
					id="pv-retry"
					type="button"
					disabled={reloading}
					aria-busy={reloading}
					onclick={retryAccount}>{reloading ? 'Trying again…' : 'Try again'}</button
				>
			</div>
		{:else if !account}
			<p role="status" aria-busy="true">Loading your account…</p>
		{:else if view === 'everywhere'}
			<section class="acc-card" id="everywhere" aria-labelledby="everywhere-h">
				<h2 class="acc-h" id="everywhere-h" tabindex="-1">Sign out everywhere?</h2>
				<p>
					You’ll be signed out on this device and every other one. Orders and favourites in your
					account aren’t affected.
				</p>
				<div class="acc-actions">
					<button
						class="btn primary"
						type="button"
						disabled={signingOut}
						aria-busy={signingOut}
						onclick={signOutEverywhere}
						>{signingOut ? 'Signing out…' : 'Sign out everywhere'}
						<span class="arrow" aria-hidden="true">→</span></button
					>
					<button class="btn" type="button" disabled={signingOut} onclick={cancelEverywhere}
						>Cancel</button
					>
				</div>
			</section>
		{:else}
			<section class="acc-card" id="data" aria-labelledby="data-h">
				<h2 class="acc-h" id="data-h">Your data and how long we keep it</h2>
				<p class="small">Your account is active. We keep it while you use it.</p>
				<table class="acc-table">
					<caption class="sr-only">How long we keep your data</caption>
					<thead>
						<tr><th scope="col">What</th><th scope="col">How long we keep it</th></tr>
					</thead>
					<tbody>
						{#each RETENTION as [what, how] (what)}
							<tr><th scope="row">{what}</th><td>{how}</td></tr>
						{/each}
					</tbody>
				</table>
				<p class="small">
					No analytics run on account pages: we don’t track what you look at here.
				</p>
			</section>

			<section class="acc-card" id="everywhere" aria-labelledby="everywhere-h">
				<h2 class="acc-h" id="everywhere-h">Sign out everywhere</h2>
				<p class="small">Signs you out on every phone, tablet and computer, including this one.</p>
				<div class="acc-actions">
					<button class="btn" id="pv-everywhere" type="button" onclick={askEverywhere}
						>Sign out everywhere…</button
					>
				</div>
			</section>

			<section class="acc-card" id="delete" aria-labelledby="delete-h">
				<h2 class="acc-h" id="delete-h">Delete your account</h2>
				<p class="small">
					Removes your account and the personal details we don’t need for your orders or by law.
					You’ll see exactly what before you confirm.
				</p>
				<div class="acc-actions">
					<a class="button acc-danger-link" href={resolve('/account/delete')}>Delete my account…</a>
				</div>
			</section>

			<section class="acc-card" id="device" aria-labelledby="device-h">
				<h2 class="acc-h" id="device-h">This device</h2>
				<p class="small">
					Favourites and your bag can be saved in this browser without an account.
				</p>
				<div class="acc-actions">
					<button class="btn" type="button" onclick={clearThisDevice}>Clear this device</button>
				</div>
				<p class="small" role="status">{cleared}</p>
			</section>
		{/if}
	</AccountShell>
{/if}
