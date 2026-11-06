<script lang="ts">
	// Delete your account (approved GK-016 r02b sketch, states 50–56). The API
	// preview (POST /me/deletion-preview) decides the step: blocked while an
	// order's money is unsettled (the orders and why), a staff account can't
	// be deleted here, a warning with an extra tick for confirmed upcoming
	// orders, or the review (what is deleted and kept, "can't be undone"
	// tick). Then a fresh sign-in by email link (the API needs one within 10
	// minutes): the link returns to /account, which offers to continue here
	// while the marker (an expiry only) is set and the session is fresh. The
	// final step asks the API for a new preview and deletes only with it and
	// the customer's click, one Idempotency-Key per deletion (kept on a lost
	// answer). Deleted: signed out here, Clear this device, Back to the shop.
	// No analytics (K10).
	import { resolve } from '$app/paths';
	import { goto } from '$app/navigation';
	import { onMount, tick } from 'svelte';
	import { newIdempotencyKey } from '../../../lib/api';
	import { auth, callbackUrl, rememberReturnPath, signOut } from '../../../lib/auth';
	import {
		deleteAccount,
		deletionPreview,
		type AccountApi,
		type DeletionPreview
	} from '../../../lib/account/client';
	import {
		accountProblem,
		accountUnusable,
		endedSession,
		leaveWhenSignedOut,
		signOutHere,
		toSignIn
	} from '../../../lib/account/guard';
	import { orderRef } from '../../../lib/account/orders';
	import {
		blockedReason,
		clearDevice,
		deletionFailure,
		deletionMarked,
		deletionStep,
		finalDecision,
		forgetDeletion,
		freshSignIn,
		markDeletion,
		mayMark,
		ordersTickLabel,
		tickErrors,
		type Ticks,
		upcomingLine,
		upcomingWarning
	} from '../../../lib/account/privacy';
	import { SaveKey } from '../../../lib/account/profile';
	import { accountApi, sessionIdentity, sessionSignInAt } from '../../../lib/account/session';
	import AccountShell from '../../../lib/account/ui/AccountShell.svelte';
	import { emptyBag } from '../../../lib/shop/bag';
	import { bag } from '../../../lib/shop/bag.svelte';
	import { emptyFavourites } from '../../../lib/shop/favourites';
	import { favourites } from '../../../lib/shop/favourites.svelte';

	type Step = 'review' | 'warn' | 'blocked' | 'staff' | 'reauth' | 'sent' | 'final' | 'deleted';

	let api: AccountApi | null = null;
	const keys = new SaveKey();
	let unsubscribe: (() => void) | undefined;
	let email = $state<string>();
	/** 'final' once the customer has signed in again (a preview then leads to the final step). */
	let stage: 'start' | 'final' = 'start';
	// The account this page opened for (its marker hash): if another account's
	// session replaces it in this browser, the page starts again from the review.
	let openedFor: string | undefined;
	let preview = $state<DeletionPreview | null>(null);
	let step = $state<Step | null>(null);
	let ticks = $state<Ticks>({ orders: false, undone: false });
	let tickErr = $state<Partial<Record<keyof Ticks, string>>>({});
	let blocked = $state('');
	let loadProblem = $state('');
	let retrying = $state(false);
	let sending = $state(false);
	let sendProblem = $state('');
	let reauthNote = $state(false);
	// The send problem ends with how to reach us (no way forward on this page).
	let sendProblemContact = $state(false);
	// Another account's session took over this browser: its review starts here.
	let swapNote = $state(false);
	let staleNote = $state(false);
	let deleting = $state(false);
	let deleteProblem = $state('');
	let cleared = $state('');
	let signOutProblem = $state('');

	const upcoming = $derived(preview?.upcoming ?? []);

	function local(): Storage | null {
		try {
			return window.localStorage;
		} catch {
			return null;
		}
	}

	function tab(): Storage | null {
		try {
			return window.sessionStorage;
		} catch {
			return null;
		}
	}

	async function focus(id: string) {
		await tick();
		document.getElementById(id)?.focus();
	}

	/**
	 * What the page may do now (privacy.ts finalDecision): the final step
	 * only for the account it opened for, marked and freshly signed in; the
	 * same account whose window ran out signs in again; another account's
	 * session starts again from the review (never a shortcut for anyone else).
	 */
	async function decide(): Promise<'final' | 'reauth' | 'review'> {
		const { email: now_email, who } = await sessionIdentity();
		const now = Date.now();
		const decision = finalDecision({
			openedFor,
			who,
			marked: who !== undefined && deletionMarked(local(), now, who),
			fresh: freshSignIn(await sessionSignInAt(), now)
		});
		if (decision === 'review') {
			stage = 'start';
			if (who !== openedFor) {
				openedFor = who;
				email = now_email;
				swapNote = true;
			}
		}
		return decision;
	}

	/** Back to the review for another account's session, with a note. */
	async function toReview() {
		if (await loadPreview()) await settleFocus();
		else await focus('del-retry');
	}

	/**
	 * After the page reloads its preview, focus what changed: the stale note,
	 * else the swap note, else the step's status, else its heading.
	 */
	async function settleFocus() {
		await tick();
		const target =
			(staleNote && document.getElementById('del-stale')) ||
			(swapNote && document.getElementById('del-swap')) ||
			((step === 'blocked' || step === 'staff' || step === 'final') &&
				document.getElementById('del-status')) ||
			document.getElementById('delete-h');
		target?.focus();
	}

	/** The same account must sign in again (its fresh sign-in or marker ran out). */
	async function toSignInAgain() {
		stage = 'start';
		reauthNote = true;
		sendProblem = '';
		step = 'reauth';
		await focus('del-reauth-note');
	}

	/** Asks the API what deleting would do now and shows the matching step. */
	async function loadPreview(): Promise<boolean> {
		if (!api) return false;
		if (stage === 'final' && (await decide()) !== 'final') stage = 'start';
		loadProblem = '';
		try {
			preview = await deletionPreview(api, newIdempotencyKey());
		} catch (e) {
			if (await endedSession(e)) return false;
			preview = null;
			step = null;
			if (accountUnusable(e)) blocked = accountProblem(e, 'your account');
			else loadProblem = accountProblem(e, 'this page');
			return false;
		}
		const next = deletionStep(preview);
		ticks = { orders: false, undone: false };
		tickErr = {};
		step = next === 'staff' || next === 'blocked' || stage === 'start' ? next : 'final';
		return true;
	}

	async function arrive() {
		if (step === 'blocked' || step === 'staff' || step === 'final') await focus('del-status');
	}

	async function retryLoad() {
		retrying = true;
		const ok = await loadPreview();
		retrying = false;
		if (ok) await arrive();
		else await focus('del-retry');
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
			const identity = await sessionIdentity();
			email = identity.email;
			const at = await sessionSignInAt();
			const who = identity.who;
			openedFor = who;
			if (!active) return;
			// Back from the confirmation link, as the same account that asked
			// for it: straight to the final step.
			const now = Date.now();
			if (deletionMarked(local(), now, who) && freshSignIn(at, now)) stage = 'final';
			if (await loadPreview()) await arrive();
		})();
		unsubscribe = leaveWhenSignedOut(() => {
			preview = null;
			step = null;
		});
		return () => {
			active = false;
			unsubscribe?.();
		};
	});

	async function showTickErrors(found: Partial<Record<keyof Ticks, string>>): Promise<boolean> {
		tickErr = found;
		const first = found.orders ? 'del-order' : found.undone ? 'del-ok' : '';
		if (first) await focus(first);
		return first !== '';
	}

	function ticked(which: keyof Ticks) {
		if (tickErr[which] && ticks[which]) tickErr = { ...tickErr, [which]: undefined };
	}

	async function toReauth(event: SubmitEvent) {
		event.preventDefault();
		if (await showTickErrors(tickErrors(ticks, step === 'warn'))) return;
		reauthNote = false;
		swapNote = false;
		sendProblem = '';
		step = 'reauth';
		await focus('reauth-h');
	}

	/** Emails a sign-in link to the signed-in address; it returns to /account. */
	async function sendLink() {
		if (sending) return;
		sendProblem = '';
		sendProblemContact = false;
		// The link and the marker are for the account signed in now, which
		// must be the one that did the review on this page.
		const identity = await sessionIdentity();
		if (identity.who === undefined && openedFor === undefined) {
			// This browser can't identify the account (no Web Crypto, for example
			// an insecure address): no link, and say why instead of looping.
			sendProblem =
				'We can’t confirm it’s you in this browser. Please open this page over a secure connection in an up-to-date browser.';
			sendProblemContact = true;
			await focus('del-send-problem');
			return;
		}
		if (!mayMark(openedFor, identity.who)) {
			if (identity.who !== openedFor) {
				openedFor = identity.who;
				email = identity.email;
				swapNote = true;
			}
			stage = 'start';
			await toReview();
			return;
		}
		const who = identity.who;
		email = identity.email;
		if (!email) {
			sendProblem =
				'We can’t find this account’s email address. Please sign out and sign in again.';
			await focus('del-send-problem');
			return;
		}
		sending = true;
		let problem = '';
		try {
			rememberReturnPath('/account');
			const { error } = await auth().auth.signInWithOtp({
				email,
				options: { emailRedirectTo: callbackUrl(), shouldCreateUser: false }
			});
			if (error) {
				problem =
					error.status === 429
						? 'You asked for a link a moment ago. Please wait a minute and try again.'
						: 'Sign-in is not available right now. Please try again.';
			}
		} catch {
			problem = 'Sign-in is not available right now. Please try again.';
		}
		sending = false;
		if (problem) {
			sendProblem = problem;
			await focus('del-send-problem');
			return;
		}
		markDeletion(local(), Date.now(), who);
		step = 'sent';
		await focus('del-status');
	}

	async function finishDeleted() {
		forgetDeletion(local());
		unsubscribe?.();
		unsubscribe = undefined;
		await signOut().catch(() => false);
		preview = null;
		email = undefined;
		deleting = false;
		step = 'deleted';
		await focus('del-status');
	}

	async function confirmDelete() {
		if (deleting || !api || !preview) return;
		deleteProblem = '';
		staleNote = false;
		if (step === 'final') {
			const decision = await decide();
			if (decision === 'review') return toReview();
			if (decision === 'reauth') return toSignInAgain();
		}
		if (await showTickErrors(tickErrors(ticks, upcoming.length > 0, false))) return;
		const input = { preview_id: preview.preview_id, preview_hash: preview.preview_hash };
		deleting = true;
		try {
			await deleteAccount(api, keys.for(input), input);
			keys.forget();
			await finishDeleted();
		} catch (e) {
			deleting = false;
			await deleteFailed(e);
		}
	}

	async function deleteFailed(e: unknown) {
		const kind = deletionFailure(e);
		if (kind !== 'retry') keys.forget();
		switch (kind) {
			case 'ended':
				await endedSession(e);
				return;
			case 'reauth':
				reauthNote = true;
				sendProblem = '';
				step = 'reauth';
				await focus('del-reauth-note');
				return;
			case 'deleted':
				await finishDeleted();
				return;
			case 'disabled':
				preview = null;
				step = null;
				blocked = accountProblem(e, 'your account');
				await focus('del-blocked');
				return;
			case 'staff':
				step = 'staff';
				await focus('del-status');
				return;
			case 'blocked':
			case 'stale':
				// The facts changed: show them as they are now.
				if (await loadPreview()) {
					staleNote = kind === 'stale' && step === 'final';
					await settleFocus();
				} else {
					await focus('del-retry');
				}
				return;
			default:
				deleteProblem =
					kind === 'retry'
						? 'We couldn’t finish deleting your account right now. Please try again.'
						: 'We couldn’t delete your account right now. Please try again.';
				await focus('del-problem');
		}
	}

	function keep() {
		forgetDeletion(local());
	}

	function clearThisDevice() {
		const ok = clearDevice(local(), tab());
		favourites.state = emptyFavourites();
		bag.state = emptyBag();
		cleared = ok
			? 'Cleared the favourites saved in this browser and the bag in this tab.'
			: 'This browser didn’t let us clear everything. You can clear this site’s data in your browser’s settings.';
	}

	async function leave() {
		unsubscribe?.();
		const reached = await signOutHere();
		preview = null;
		step = null;
		if (!reached) {
			signOutProblem =
				'You are signed out on this device, but the sign-in service could not be reached to end the session everywhere.';
			return;
		}
		await goto(resolve('/'), { replaceState: true });
	}
</script>

<svelte:head>
	<title>Delete your account · GK Cake London</title>
	<meta name="robots" content="noindex" />
</svelte:head>

{#snippet swapNotice()}
	{#if swapNote}
		<div class="notice" id="del-swap" role="alert" tabindex="-1">
			<div>
				<p>
					You’re now signed in{email
						? ` as ${email}`
						: ''}.{#if step === 'review' || step === 'warn'}
						Please review what happens before deleting this account.{/if}
				</p>
			</div>
		</div>
	{/if}
{/snippet}

{#snippet lists()}
	<div class="acc-two acc-lists">
		<div>
			<h3 class="acc-h3">We delete</h3>
			<ul class="acc-dots">
				<li>your account and sign-in</li>
				<li>your profile: name, phone and Instagram name</li>
				<li>favourites saved in your account</li>
				<li>your requests and the photos you sent with them</li>
				<li>the link between your orders and this account, and the private links to your orders</li>
			</ul>
		</div>
		<div>
			<h3 class="acc-h3">We keep for your orders</h3>
			<ul class="acc-dots">
				<li>
					details of your orders (name, email, cake, date): unfinished orders until they’re done,
					then 2 years after handover or cancellation, no longer linked to an account
				</li>
				<li>anything needed for an open dispute, until it’s resolved</li>
			</ul>
			<h3 class="acc-h3">We keep because the law requires</h3>
			<ul class="acc-dots">
				<li>
					payment and refund records needed for accounting, for the legal period (being confirmed),
					with as little personal information as possible
				</li>
			</ul>
			<p class="small">Messages with the chef on Instagram or by email also stay in those apps.</p>
		</div>
	</div>
	<p class="small">
		Copies in our backups are removed within 30 days. Favourites saved in this browser stay on this
		device until you clear them.
	</p>
{/snippet}

{#snippet upcomingNotice()}
	<div class="notice" id="del-status" role="alert" tabindex="-1">
		<div>
			{#if upcoming.length === 1}
				<p>
					<strong>You have an upcoming order:</strong>
					<a href={resolve('/orders/[id]', { id: upcoming[0].order_id })}
						>{orderRef({ id: upcoming[0].order_id })}</a
					>{upcomingLine(upcoming[0]) ? `, ${upcomingLine(upcoming[0])}` : ''}.
				</p>
			{:else}
				<p><strong>You have upcoming orders:</strong></p>
				<ul class="acc-dots">
					{#each upcoming as u (u.order_id)}
						<li>
							<a href={resolve('/orders/[id]', { id: u.order_id })}
								>{orderRef({ id: u.order_id })}</a
							>{upcomingLine(u) ? `, ${upcomingLine(u)}` : ''}
						</li>
					{/each}
				</ul>
			{/if}
			<p>{upcomingWarning(upcoming.length)}</p>
		</div>
	</div>
{/snippet}

{#snippet orderTick()}
	<label class="acc-check">
		<input
			type="checkbox"
			id="del-order"
			bind:checked={ticks.orders}
			aria-invalid={tickErr.orders ? 'true' : undefined}
			aria-describedby={tickErr.orders ? 'del-order-error' : undefined}
			onchange={() => ticked('orders')}
		/>
		{ordersTickLabel(upcoming)}
	</label>
	<p class="field-error" id="del-order-error" hidden={!tickErr.orders}>{tickErr.orders ?? ''}</p>
{/snippet}

{#snippet contact()}
	Email <a href="mailto:gk.cake.london@gmail.com">gk.cake.london@gmail.com</a> or message
	<a href="https://www.instagram.com/gk.cake.london/" rel="noopener noreferrer">@gk.cake.london</a> on
	Instagram.
{/snippet}

{#snippet keepButton()}
	<a class="button" href={resolve('/account/privacy')} onclick={keep}>Keep my account</a>
{/snippet}

{#if step === 'deleted'}
	<AccountShell active="delete" nav={false} title="Your account has been deleted">
		<div class="notice success" id="del-status" role="status" tabindex="-1">
			<div>
				<p>We’ve signed you out and deleted your account, profile, favourites and requests.</p>
				<p>
					Details of your orders are kept, no longer linked to an account, until 2 years after
					handover or cancellation (unfinished orders until they’re done); payment records for as
					long as the law requires.
				</p>
				<p>Copies in our backups are removed within 30 days.</p>
			</div>
		</div>
		<div class="small acc-inline">
			<span>Favourites saved in this browser are still here.</span>
			<button class="btn acc-small-btn" type="button" onclick={clearThisDevice}
				>Clear this device</button
			>
		</div>
		<p class="small" role="status">{cleared}</p>
		<div class="acc-actions">
			<a class="button primary" href={resolve('/')}
				>Back to the shop <span class="arrow" aria-hidden="true">→</span></a
			>
		</div>
	</AccountShell>
{:else}
	<AccountShell
		active="delete"
		title="Privacy & your data"
		sub={email ? `Signed in as ${email}` : undefined}
		onsignout={step || blocked || loadProblem ? leave : undefined}
	>
		{#if signOutProblem}
			<div class="notice error" role="alert"><div><p>{signOutProblem}</p></div></div>
		{:else if blocked}
			<div class="notice error" id="del-blocked" role="alert" tabindex="-1">
				<div><p>{blocked}</p></div>
			</div>
			<div class="acc-actions">
				<a class="button" href="mailto:gk.cake.london@gmail.com">Contact us</a>
				<button class="btn" type="button" onclick={leave}>Sign out</button>
			</div>
		{:else if loadProblem}
			<div class="notice error" role="alert"><div><p>{loadProblem}</p></div></div>
			<div class="acc-actions">
				<button
					class="btn"
					id="del-retry"
					type="button"
					disabled={retrying}
					aria-busy={retrying}
					onclick={retryLoad}>{retrying ? 'Trying again…' : 'Try again'}</button
				>
				{@render keepButton()}
			</div>
		{:else if !step || !preview}
			<p role="status" aria-busy="true">Checking your account…</p>
		{:else if step === 'blocked'}
			<section class="acc-card" id="delete" aria-labelledby="delete-h">
				<h2 class="acc-h" id="delete-h" tabindex="-1">Delete your account</h2>
				{@render swapNotice()}
				<div class="notice error" id="del-status" role="alert" tabindex="-1">
					<div>
						{#if preview.blocked.length > 0}
							<p>
								<strong>You can’t delete your account yet.</strong> These orders still have money to settle:
							</p>
							<ul class="acc-dots">
								{#each preview.blocked as b (b.order_id)}
									<li>
										<a href={resolve('/orders/[id]', { id: b.order_id })}
											>{orderRef({ id: b.order_id })}</a
										>: {blockedReason(b.reason)}
									</li>
								{/each}
							</ul>
							<p>
								Once they’re settled you can delete your account. Need help sooner? {@render contact()}
							</p>
						{:else}
							<p><strong>You can’t delete your account yet.</strong></p>
							<p>Need help? {@render contact()}</p>
						{/if}
					</div>
				</div>
				{@render lists()}
				<div class="acc-actions">
					<button class="btn acc-danger" type="button" disabled>Delete my account</button>
					<a class="button" href={resolve('/account')}>Back to your account</a>
				</div>
			</section>
		{:else if step === 'staff'}
			<section class="acc-card" id="delete" aria-labelledby="delete-h">
				<h2 class="acc-h" id="delete-h" tabindex="-1">Delete your account</h2>
				{@render swapNotice()}
				<div class="notice error" id="del-status" role="alert" tabindex="-1">
					<div>
						<p>
							<strong>You can’t delete this account here.</strong> It has staff access. The staff access
							has to be removed first; then you can delete it as a customer.
						</p>
					</div>
				</div>
				<div class="acc-actions">
					<a class="button" href={resolve('/account')}>Back to your account</a>
				</div>
			</section>
		{:else if step === 'review' || step === 'warn'}
			<section class="acc-card" id="delete" aria-labelledby="delete-h">
				<h2 class="acc-h" id="delete-h" tabindex="-1">Delete your account</h2>
				{@render swapNotice()}
				{#if step === 'warn'}
					{@render upcomingNotice()}
				{:else}
					<p>Here’s what happens if you delete your account.</p>
				{/if}
				{@render lists()}
				<form id="del-form" novalidate onsubmit={toReauth}>
					{#if step === 'warn'}{@render orderTick()}{/if}
					<label class="acc-check">
						<input
							type="checkbox"
							id="del-ok"
							bind:checked={ticks.undone}
							aria-invalid={tickErr.undone ? 'true' : undefined}
							aria-describedby={tickErr.undone ? 'del-ok-error' : undefined}
							onchange={() => ticked('undone')}
						/>
						I understand that deleting my account can’t be undone.
					</label>
					<p class="field-error" id="del-ok-error" hidden={!tickErr.undone}>
						{tickErr.undone ?? ''}
					</p>
					<div class="acc-actions">
						<button class="btn primary" type="submit"
							>Continue: confirm it’s you <span class="arrow" aria-hidden="true">→</span></button
						>
						{@render keepButton()}
					</div>
				</form>
				<p class="small">
					Next, we ask you to sign in again with an email link before anything is deleted.
				</p>
				<p class="small quiet">
					You can still order as a guest, and create a new account later with the same email. Your
					earlier orders and favourites won’t be in the new account.
				</p>
			</section>
		{:else if step === 'reauth'}
			<section class="acc-card" id="delete" aria-labelledby="reauth-h">
				<h2 class="acc-h" id="reauth-h" tabindex="-1">Confirm it’s you</h2>
				{#if reauthNote}
					<div class="notice" id="del-reauth-note" role="alert" tabindex="-1">
						<div><p>Please sign in again to confirm it’s you. Nothing has been deleted.</p></div>
					</div>
				{/if}
				<p>
					To delete your account, please sign in again now. This makes sure it’s really you on this
					device. <strong>Nothing is deleted yet.</strong>
				</p>
				<div class="acc-actions">
					<button
						class="btn primary"
						type="button"
						disabled={sending}
						aria-busy={sending}
						onclick={sendLink}
						>{sending ? 'Sending…' : 'Email me a confirmation link'}
						<span class="arrow" aria-hidden="true">→</span></button
					>
				</div>
				{#if sendProblem}
					<div class="notice error" id="del-send-problem" role="alert" tabindex="-1">
						<div>
							<p>{sendProblem}</p>
							{#if sendProblemContact}<p>Need help? {@render contact()}</p>{/if}
						</div>
					</div>
				{/if}
				{#if email}
					<p class="small">
						The email link goes to {email}, works once, in this browser, for 30 minutes.
					</p>
				{/if}
				<div class="acc-actions">{@render keepButton()}</div>
			</section>
		{:else if step === 'sent'}
			<section class="acc-card" id="delete" aria-labelledby="sent-h">
				<h2 class="acc-h" id="sent-h">Check your email</h2>
				<div class="notice" id="del-status" role="status" tabindex="-1">
					<div>
						<p>
							We’ve sent a confirmation link to <strong>{email}</strong>. Open it in this browser
							within 30 minutes; it brings you back to your account to finish.
						</p>
						<p>
							Nothing is deleted yet. If you didn’t mean to delete your account, just ignore the
							email.
						</p>
					</div>
				</div>
				<div class="acc-actions">{@render keepButton()}</div>
			</section>
		{:else if step === 'final'}
			<section class="acc-card" id="delete" aria-labelledby="final-h">
				<h2 class="acc-h" id="final-h">Delete your account now?</h2>
				{#if staleNote}
					<div class="notice" id="del-stale" role="alert" tabindex="-1">
						<div>
							<p>
								Something changed since this page checked your account, so we’ve checked again.
								Please read it and confirm again.
							</p>
						</div>
					</div>
				{/if}
				{#if upcoming.length > 0}
					{@render upcomingNotice()}
				{:else}
					<div class="notice success" id="del-status" role="status" tabindex="-1">
						<div>
							<p><strong>You’ve signed in again.</strong> You can now delete your account.</p>
						</div>
					</div>
				{/if}
				<p>
					We’ll delete your account, profile, favourites and requests, and unlink your orders. We
					keep details of your orders until 2 years after handover or cancellation (unfinished
					orders until they’re done), and payment records for as long as the law requires.
				</p>
				<p>This can’t be undone.</p>
				{#if upcoming.length > 0}{@render orderTick()}{/if}
				<div class="acc-actions">
					<button
						class="btn acc-danger"
						type="button"
						disabled={deleting}
						aria-busy={deleting}
						onclick={confirmDelete}>{deleting ? 'Deleting…' : 'Delete my account'}</button
					>
					{@render keepButton()}
				</div>
				{#if deleteProblem}
					<div class="notice error" id="del-problem" role="alert" tabindex="-1">
						<div><p>{deleteProblem}</p></div>
					</div>
				{/if}
				<p class="small quiet">
					This confirmation stays open for 10 minutes; after that we ask you to sign in again.
				</p>
			</section>
		{/if}
	</AccountShell>
{/if}
