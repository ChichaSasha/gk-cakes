<script lang="ts">
	// One request to add an order and the chef's decision (Creator-approved
	// GK-018 r04d-claims sketch, request.js, approved "as suggested" on the
	// GK-018 card 2026-11-06). Older-order requests (today): find the order
	// among website orders, confirm it's theirs through the order's contact
	// email before adding it (answer 3: never on a reference alone), then add
	// or decline with an internal note (answer 5). Requests from an order page
	// or email (Future): only that order can be added. Nothing is matched
	// automatically and nothing personal goes into the URL. The API has no
	// single-request read yet, so the request is found by paging the lists.
	import { resolve } from '$app/paths';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount, tick } from 'svelte';
	import { ApiError, apiGet, newIdempotencyKey } from '../../../../lib/api';
	import { leaveOnSignOut, signInConfigured, signOut, type Me } from '../../../../lib/auth';
	import { formatMoney, londonToday } from '../../../../lib/checkout/format';
	import type { StaffApi } from '../../../../lib/admin/catalogue';
	import {
		decideClaim,
		decidedBy,
		findClaim,
		howLabel,
		isFuturePath,
		looksLikeOrderRef,
		monthLabel,
		sameOrderRequests,
		shortRef,
		STATE_CHEF,
		staffClaims,
		waitText,
		when,
		whoName,
		type StaffClaim
	} from '../../../../lib/admin/claims';
	import {
		BUSINESS_EMAIL,
		canMovePickup,
		checkEmail,
		confirmPlaceholder,
		defaultMonths,
		isUpcoming,
		matchOrders,
		MAX_SEARCH_MONTHS,
		monthName,
		monthOptions,
		monthsRange,
		monthsSpan,
		newCheckCode,
		nextWaiting,
		NOTE_MAX,
		noteLength,
		noteProblem,
		orderLine,
		ownerCan,
		refusalOf,
		retrySafe,
		surname,
		type DecisionRefusal
	} from '../../../../lib/admin/claimDecision';
	import { claimsWaiting } from '../../../../lib/admin/claimsWaiting.svelte';
	import {
		MODES,
		staffOrders,
		viewOf,
		type StaffOrderRow,
		type StaffView
	} from '../../../../lib/admin/orders';
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
	const GUEST_GONE =
		'Guest access to this order stops working: today that’s the order page in the tab that placed it, if it’s still open (and, once order emails exist, the links in them). They see the order by signing in.';
	const GUEST_KEPT = 'Their guest access to the order keeps working as before.';
	const FUTURE_NOTE =
		'Arrives once GK-016’s “Add this order to my account” (on the order page) and order emails exist. Customers can’t send this kind of request today.';
	const LIFE: Record<string, [string, string]> = {
		awaiting_payment: ['Waiting for payment', 'info'],
		confirmation_pending: ['Confirming', 'info'],
		paid_awaiting_agreement: ['Paid late', 'warn'],
		confirmed: ['Confirmed', 'ok'],
		in_production: ['Being made', 'ok'],
		ready: ['Ready', 'ok'],
		completed: ['Handed over', 'neutral'],
		cancellation_pending: ['Cancelling', 'warn'],
		cancelled: ['Cancelled', 'neutral']
	};
	const today = londonToday();
	const MONTHS = monthOptions(today);
	const now = new Date();

	let api: StaffApi | null = null;
	let me = $state<string | null>(null);
	let view = $state<StaffView | null>(null);
	let claim = $state<StaffClaim | null>(null);
	let waiting = $state<StaffClaim[]>([]);
	let phase = $state<'loading' | 'ready' | 'missing' | 'capped' | 'error'>('loading');
	let problem = $state('');
	let mounted = true;

	// Find the order (older-order requests).
	let term = $state('');
	let from = $state('');
	let to = $state('');
	let rows = $state<StaffOrderRow[] | null>(null);
	let search = $state<'idle' | 'loading' | 'ready' | 'error' | 'too_many'>('idle');
	let searchRev = 0;
	let pickId = $state('');
	// The order they asked from (Future requests).
	let linkOrder = $state<StaffOrderRow | null>(null);
	let linkLoad = $state<'idle' | 'loading' | 'ready' | 'error'>('idle');

	// The decision.
	let step = $state<'review' | 'approve' | 'decline' | 'busy' | 'done'>('review');
	let decision = $state<'approve' | 'decline'>('approve');
	let result = $state<'ok' | DecisionRefusal | null>(null);
	let confirmed = $state(false);
	let note = $state('');
	let confirmError = $state('');
	let noteError = $state('');
	let attempt: { key: string; signature: string } | null = null;
	// A fresh one-off code for each order chosen on each request (never reused: design Gaps).
	let code = $state(newCheckCode());
	// What was sent: the result and Try again use this, never the live choice.
	let submitted = $state<{ order: StaffOrderRow | null; note: string } | null>(null);
	let linkRev = 0;

	const pending = $derived(claim?.state === 'pending');
	const future = $derived(claim ? isFuturePath(claim) : false);
	const name = $derived(claim?.account.full_name?.trim() || null);
	const theirAccount = $derived(name ? `${name}’s account` : 'their account');
	const verb = $derived(pending || step === 'done' ? 'asks' : 'asked');
	const matches = $derived(rows ? matchOrders(rows, term) : []);
	const picked = $derived<StaffOrderRow | null>(
		future
			? linkOrder && claim && linkOrder.id === claim.order_id
				? linkOrder
				: null
			: (rows?.find((o) => o.id === pickId) ?? null)
	);
	// The search and choice are locked once a decision is being confirmed, sent or answered.
	const locked = $derived(step !== 'review');
	const needsCheck = $derived(!future);
	const same = $derived(claim && future ? sameOrderRequests(claim, waiting) : []);
	const following = $derived(claim ? nextWaiting(claim, waiting) : null);
	const spanOk = $derived(
		from !== '' &&
			to !== '' &&
			monthsSpan(from, to) >= 1 &&
			monthsSpan(from, to) <= MAX_SEARCH_MONTHS
	);
	const backHref = $derived(
		claim && claim.state !== 'pending'
			? `${resolve('/admin/requests')}?tab=${claim.state}`
			: resolve('/admin/requests')
	);

	async function ended(e: unknown): Promise<boolean> {
		if (e instanceof ApiError && e.signInRequired) {
			await signOut();
			await goto(resolve('/sign-in?next=/admin&ended=1'), { replaceState: true });
			return true;
		}
		if (e instanceof ApiError && (e.code === 'ACCOUNT_DISABLED' || e.code === 'FORBIDDEN')) {
			problem =
				e.code === 'FORBIDDEN'
					? 'This account can’t decide requests.'
					: 'This account is disabled.';
			return true;
		}
		return false;
	}

	function resetDecision() {
		step = 'review';
		result = null;
		confirmed = false;
		note = '';
		confirmError = '';
		noteError = '';
		attempt = null;
		submitted = null;
		code = newCheckCode();
		copied = '';
	}

	async function load() {
		if (!api) return;
		const id = page.params.id ?? '';
		phase = 'loading';
		resetDecision();
		linkOrder = null;
		linkLoad = 'idle';
		rows = null;
		search = 'idle';
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
			const [found, firstWaiting] = await Promise.all([
				findClaim(api, id),
				staffClaims(api, 'pending')
			]);
			if (!mounted || id !== (page.params.id ?? '')) return;
			claimsWaiting.set(firstWaiting);
			waiting = firstWaiting.claims;
			claim = found.claim;
			phase = found.claim ? 'ready' : found.capped ? 'capped' : 'missing';
			if (found.claim?.state === 'pending') {
				if (isFuturePath(found.claim)) void loadLinkOrder(found.claim);
				else {
					const m = defaultMonths(found.claim.approximate_date, today);
					from = m.from;
					to = m.to;
					term = '';
					pickId = '';
					void runSearch();
				}
			}
		} catch (e) {
			if (!mounted || id !== (page.params.id ?? '') || (await ended(e))) return;
			phase = 'error';
		}
	}

	/** Every order in the chosen months, then the browser filters (the API has no text search, K09). */
	async function runSearch() {
		if (!api || !spanOk) return;
		const rev = ++searchRev;
		search = 'loading';
		try {
			const list = await staffOrders(api, monthsRange(from, to));
			if (rev !== searchRev || !mounted) return;
			rows = list;
			if (!list.some((o) => o.id === pickId)) pickId = '';
			search = 'ready';
		} catch (e) {
			if (rev !== searchRev || !mounted || (await ended(e))) return;
			search = e instanceof Error && e.message.startsWith('more than') ? 'too_many' : 'error';
		}
	}

	async function loadLinkOrder(c: StaffClaim) {
		if (!api || !c.order_id) return;
		const rev = ++linkRev;
		linkLoad = 'loading';
		try {
			const o = await apiGet<StaffOrderRow>(`/api/v1/admin/orders/${c.order_id}`, {
				baseUrl: api.baseUrl,
				token: api.token
			});
			if (rev !== linkRev || !mounted || claim?.id !== c.id) return;
			linkOrder = o;
			linkLoad = 'ready';
		} catch (e) {
			if (rev !== linkRev || !mounted || (await ended(e))) return;
			linkLoad = 'error';
		}
	}

	async function focusStep() {
		await tick();
		const target =
			step === 'review'
				? document.querySelector<HTMLElement>(future ? '#dec-h' : '#find-h')
				: document.querySelector<HTMLElement>('[data-focus]');
		target?.focus();
	}

	function go(next: 'review' | 'approve' | 'decline') {
		if (next !== 'review' && next !== decision) {
			// A note written for adding isn't a decline's note (and the other way round).
			note = '';
			confirmed = false;
		}
		if (next !== 'review') decision = next;
		if (next === 'review') {
			result = null;
			submitted = null;
		}
		confirmError = '';
		noteError = '';
		step = next;
		void focusStep();
	}

	/** One key per decision attempt, kept for Try again; a new one when the decision, order or note changes. */
	function keyFor(signature: string): string {
		if (!attempt || attempt.signature !== signature)
			attempt = { key: newIdempotencyKey(), signature };
		return attempt.key;
	}

	async function submit(e?: SubmitEvent, retry = false) {
		e?.preventDefault();
		if (!api || !claim) return;
		const c = claim;
		if (retry && submitted) return send(c, submitted.order, submitted.note);
		const order = picked;
		if (decision === 'approve') {
			if (!order) return;
			confirmError =
				needsCheck && !confirmed
					? 'Tick this once a reply from that address quoted your code and their request reference.'
					: '';
			noteError = noteProblem(note, needsCheck) ?? '';
			if (confirmError || noteError) {
				await tick();
				document
					.querySelector<HTMLElement>(confirmError ? '#confirm-check' : '#decision-note')
					?.focus();
				return;
			}
		} else {
			noteError = noteProblem(note, false) ?? '';
			if (noteError) {
				await tick();
				document.querySelector<HTMLElement>('#decision-note')?.focus();
				return;
			}
		}
		await send(c, order, note.trim());
	}

	async function send(c: StaffClaim, order: StaffOrderRow | null, trimmed: string) {
		if (!api) return;
		if (decision === 'approve' && !order) return;
		submitted = { order, note: trimmed };
		const signature = `${decision}|${order?.id ?? ''}|${trimmed}`;
		const key = keyFor(signature);
		step = 'busy';
		void focusStep();
		try {
			await decideClaim(
				api,
				key,
				c.id,
				decision === 'approve' && order
					? {
							expected_version: c.version,
							decision: 'approve',
							order_id: order.id,
							note: trimmed || undefined
						}
					: { expected_version: c.version, decision: 'decline', note: trimmed || undefined }
			);
			if (!mounted) return;
			claim = {
				...c,
				state: decision === 'approve' ? 'approved' : 'declined',
				order_id: decision === 'approve' && order ? order.id : c.order_id,
				decided_at: new Date().toISOString(),
				decided_by: me,
				decision_note: trimmed || null
			};
			waiting = waiting.filter((x) => x.id !== c.id);
			claimsWaiting.set({ claims: waiting });
			result = 'ok';
			attempt = null;
		} catch (err) {
			if (!mounted) return;
			const r = refusalOf(err);
			if (r === 'signed_out' || r === 'forbidden') {
				if (await ended(err)) return;
			}
			result = r;
			if (!retrySafe(r)) attempt = null;
		}
		step = 'done';
		void focusStep();
	}

	// The tick and "how you confirmed" are about one order: choosing another clears them.
	let confirmedFor = '';
	$effect(() => {
		const id = pickId;
		if (id !== confirmedFor) {
			confirmedFor = id;
			confirmed = false;
			note = '';
			confirmError = '';
			noteError = '';
			code = newCheckCode();
			copied = '';
			emailOpen = false;
		}
	});

	// Another request in the same page ("Next waiting request") loads it.
	let loadedId: string | null = null;
	$effect(() => {
		const id = page.params.id ?? '';
		if (api && loadedId !== null && id !== loadedId) {
			loadedId = id;
			claim = null;
			void load();
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
			loadedId = page.params.id ?? '';
			await load();
		})();
		const unsubscribe = leaveOnSignOut(() => {
			claim = null;
			rows = null;
			void goto(resolve('/sign-in?next=/admin'), { replaceState: true });
		});
		return () => {
			mounted = false;
			unsubscribe();
		};
	});

	async function leave() {
		claim = null;
		rows = null;
		const reached = await signOut();
		if (!reached) {
			problem =
				'You are signed out on this device, but the sign-in service could not be reached to end the session everywhere.';
			return;
		}
		await goto(resolve('/'), { replaceState: true });
	}

	let copied = $state('');
	let emailOpen = $state(false);
	async function copyEmail(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			copied = 'Copied. Paste it into an email to that address.';
		} catch {
			copied = 'Couldn’t copy: select the text above and copy it.';
		}
	}

	const savedNow = $derived(step === 'done' && result === 'ok');
</script>

<svelte:head>
	<title>Request to add an order · GK Cake London chef workspace</title>
	<meta name="robots" content="noindex" />
</svelte:head>

{#snippet orderSummary(o: StaffOrderRow)}
	{@const life = LIFE[o.lifecycle] ?? [o.lifecycle, 'neutral']}
	<div class="order-sum">
		<div>
			<p class="strong">
				Order <span class="mono">{shortRef(o.id)}</span> · {o.items
					.map((i) => i.cake_name)
					.join(', ')}
				<span class="chip-s chip-{life[1]}">{life[0]}</span>
			</p>
			<p class="small">{orderLine(o)} · {MODES[o.fulfillment_mode] ?? o.fulfillment_mode}</p>
			{#if o.customer_full_name}<p class="small">
					Name on the order: <strong>{o.customer_full_name}</strong
					>{#if o.total_minor !== undefined}&nbsp;· {formatMoney(
							o.total_minor,
							o.currency ?? 'GBP'
						)}{/if}
				</p>{/if}
		</div>
	</div>
{/snippet}

{#snippet noteField(placeholder: string, label: string)}
	<label class="field"
		>{label}<input
			id="decision-note"
			bind:value={note}
			autocomplete="off"
			{placeholder}
			aria-describedby="note-count note-err"
			aria-invalid={noteError ? 'true' : undefined}
		/></label
	>
	<p class="small muted counter" id="note-count">
		{noteLength(note.trim()) > NOTE_MAX
			? `${noteLength(note.trim()) - NOTE_MAX} characters too many`
			: `${NOTE_MAX - noteLength(note.trim())} characters left`} · one line
	</p>
	<p class="small bad-text" id="note-err" role="alert">{noteError}</p>
{/snippet}

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
					Request <span class="mono">{shortRef(c.id)}</span> · {c.state === 'pending'
						? waitText(c.created_at, now).toLowerCase()
						: state[0].toLowerCase()}
				</p>
				<h1>
					{name ? `${name} ${verb}` : `A customer ${verb}`} to add {future
						? 'an order'
						: 'an older order'}
				</h1>
			</div>
			<span class="chip-s chip-{state[1]}">{state[0]}</span>
		</header>
		{#if future}
			<div class="banner banner-warn future-banner">
				<ChefIcon name="clock" cls="banner-icon" />
				<div>
					<p><strong>Future state.</strong> {FUTURE_NOTE}</p>
				</div>
			</div>
		{/if}

		{#snippet sentCard(later: boolean)}
			<section class="card" class:card-later={later} aria-labelledby="sent-h">
				<h2 id="sent-h" class="card-h">What they sent</h2>
				<dl class="facts">
					<div>
						<dt>Received</dt>
						<dd>{when(c.created_at)}</dd>
					</div>
					{#if future}
						<div>
							<dt>How they asked</dt>
							<dd>
								<span class="how-chip">From the order page or email</span><span class="tag-future"
									>Future</span
								>
							</dd>
						</div>
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
				{#if future}
					<p class="small muted">
						They asked while signed in, from the order’s guest page. Having guest access shows they
						can see the order; it doesn’t prove the order is theirs: anyone with the link or that
						browser could ask.
					</p>
				{:else}
					<p class="small muted">
						The reference is whatever they typed: yours from a message or receipt, or the “Order
						reference” from a website order’s page. It isn’t checked against anything.
					</p>
				{/if}
			</section>
		{/snippet}

		{#snippet whoCard()}
			<section class="card card-later" aria-labelledby="who-h">
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
		{/snippet}

		<div class="claim-grid">
			<div class="claim-main">
				{#if c.state !== 'pending' && !savedNow}
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
					{@render sentCard(true)}
					{@render whoCard()}
				{:else if savedNow}
					{#if decision === 'approve' && submitted?.order}
						<section class="card" aria-labelledby="ad-h">
							<h2 id="ad-h" class="card-h">The order you added</h2>
							{@render orderSummary(submitted.order)}
						</section>
					{/if}
					{@render sentCard(true)}
					{@render whoCard()}
				{:else if future}
					<section class="card" aria-labelledby="lo-h">
						<h2 id="lo-h" class="card-h">The order they asked from</h2>
						{#if linkLoad === 'loading'}
							<p class="loading" role="status">
								<span class="spinner" aria-hidden="true"></span>Loading the order…
							</p>
						{:else if linkLoad === 'error' || !linkOrder}
							<p class="bad-text small" role="alert">
								Order <span class="mono">{c.order_id ? shortRef(c.order_id) : ''}</span> couldn’t be
								loaded.
								<button class="btn btn-small" type="button" onclick={() => void loadLinkOrder(c)}
									>Try again</button
								>
							</p>
						{:else}
							{@render orderSummary(linkOrder)}
						{/if}
						{#if same.length}
							<div class="banner banner-warn small">
								<ChefIcon name="alert" cls="banner-icon" />
								<div>
									<p>
										<strong>Another request asks for this same order.</strong>
										{#each same as x, i (x.id)}{i ? '; ' : ''}Request
											<a class="mono" href={resolve('/admin/requests/[id]', { id: x.id })}
												>{shortRef(x.id)}</a
											>
											from {whoName(x)} ({when(x.created_at)}){/each}. Only one account can have the
										order: if you add it here, adding it for the other request is refused.
									</p>
								</div>
							</div>
						{/if}
						<p class="small muted">
							For a request from the order page, only this order can be added.
						</p>
					</section>
					{@render sentCard(true)}
					{@render whoCard()}
				{:else}
					{@render sentCard(false)}
					{@render whoCard()}
					<section class="card" aria-labelledby="find-h">
						<h2 id="find-h" class="card-h" tabindex="-1">
							<ChefIcon name="search" />Find the order
						</h2>
						<p class="small muted">
							Looks only at orders already in the system (website orders). Choosing one is your
							judgement: a matching name or reference isn’t proof.
						</p>
						<form class="find-form" role="search" onsubmit={(e) => e.preventDefault()}>
							<fieldset class="find-fields" disabled={locked}>
								<legend class="sr-only">Search for the order</legend>
								<label class="field find-term"
									><span>Start of the order ID, or the name on the order</span><span
										class="search-box"
										><ChefIcon name="search" /><input
											type="search"
											bind:value={term}
											placeholder="e.g. 8b20 or Becker"
											autocomplete="off"
											aria-describedby="find-hint"
										/></span
									></label
								>
								<label class="field"
									>Handover from<select bind:value={from} onchange={() => void runSearch()}>
										{#each MONTHS as m (m)}<option value={m}>{monthName(m)}</option>{/each}
									</select></label
								>
								<label class="field"
									>to<select bind:value={to} onchange={() => void runSearch()}>
										{#each MONTHS as m (m)}<option value={m}>{monthName(m)}</option>{/each}
									</select></label
								>
							</fieldset>
						</form>
						<p id="find-hint" class="small muted">
							Up to 3 months at a time (the order list reads at most 93 days); every order in those
							months is loaded before filtering. {monthLabel(c.approximate_date)
								? `Starts around their month, ${monthLabel(c.approximate_date)}.`
								: 'They gave no month: starts with the last 3 months.'}
						</p>
						<div class="quick">
							<span class="small">Search for:</span>
							{#if c.reference}<button
									class="btn btn-small"
									type="button"
									disabled={locked}
									onclick={() => (term = c.reference ?? '')}>their reference “{c.reference}”</button
								>{/if}
							{#if surname(name)}<button
									class="btn btn-small"
									type="button"
									disabled={locked}
									onclick={() => (term = surname(name) ?? '')}
									>their surname “{surname(name)}”</button
								>{/if}
						</div>
						{#if !spanOk}
							<p class="bad-text small" role="alert">
								Choose up to 3 months, with “to” on or after “from”.
							</p>
						{:else if search === 'loading'}
							<p class="loading" role="status">
								<span class="spinner" aria-hidden="true"></span>Loading orders…
							</p>
						{:else if search === 'error' || search === 'too_many'}
							<div class="banner banner-bad" role="alert">
								<div>
									<p>
										{search === 'too_many'
											? 'There are too many orders in those months to load at once. Choose fewer months.'
											: 'The orders couldn’t be loaded right now. Nothing was changed.'}
									</p>
									{#if search === 'error'}<button
											class="btn btn-small"
											type="button"
											disabled={locked}
											onclick={() => void runSearch()}>Try again</button
										>{/if}
								</div>
							</div>
						{:else if search === 'ready'}
							{@const range = `${monthName(from)}${from === to ? '' : ` – ${monthName(to)}`}`}
							{#if matches.length === 0}
								<p class="small strong" aria-live="polite">
									No orders found{term.trim() ? ` for “${term.trim()}”` : ''} · handover {range}
								</p>
								<div class="banner banner-info no-match">
									<ChefIcon name="info" cls="banner-icon" />
									<div>
										<p>
											<strong
												>No matching order in the system{rows && rows.length ? '' : ' yet'}.</strong
											>
											{looksLikeOrderRef(c.reference)
												? 'Their reference looks like a website order’s: check the months or try its first characters.'
												: `${c.channel === 'instagram' ? 'Instagram orders' : 'Orders made by email, phone or in person'} from before the website aren’t in the system, so they can’t be added to an account yet. Recording older orders by hand comes later.`}
										</p>
										<p class="small">What you can do now:</p>
										<ul class="small list-tight">
											<li>
												<strong>Decline.</strong> The customer sees only “Couldn’t add … from the details
												given” and is asked to contact us, which can read as if their details were wrong.
												Once older orders can be recorded they’d have to ask again. You can add an internal
												note for yourself.
											</li>
											<li>
												<strong>Leave it waiting.</strong> They keep seeing “Waiting for the chef”, and
												it counts towards their limit of 5 waiting requests.
											</li>
											{#if rows && rows.length}<li>Try another name, ID or months.</li>{/if}
										</ul>
									</div>
								</div>
							{:else}
								<p class="small strong" aria-live="polite">
									{matches.length}
									{matches.length === 1 ? 'order' : 'orders'} · handover {range}{term.trim()
										? ` · matching “${term.trim()}”`
										: ''}
								</p>
								<fieldset class="pick-list" disabled={locked}>
									<legend class="sr-only">Choose the order to add</legend>
									{#each matches as o (o.id)}
										{@const life = LIFE[o.lifecycle] ?? [o.lifecycle, 'neutral']}
										<label class="pick"
											><input type="radio" name="pick" value={o.id} bind:group={pickId} /><span
												><span class="strong"
													><span class="mono">{shortRef(o.id)}</span> · {o.customer_full_name}</span
												>
												<span class="chip-s chip-{life[1]}">{life[0]}</span><span
													class="block small muted"
													>{orderLine(o)}{#if o.total_minor !== undefined}&nbsp;·
														{formatMoney(o.total_minor, o.currency ?? 'GBP')}{/if}</span
												></span
											></label
										>
									{/each}
								</fieldset>
								<p class="small muted">
									This list can’t show whether an order is already in someone’s account. If it is,
									adding it is refused and nothing changes.
								</p>
							{/if}
						{/if}
					</section>
					{#if picked && picked.contact_email}
						{@const o = picked}
						{@const email = checkEmail(o, code)}
						<section class="card card-alert" aria-labelledby="ev-h">
							<h2 id="ev-h" class="card-h">
								<ChefIcon name="alert" />Before adding: confirm it’s theirs
							</h2>
							<p class="small">
								A website order’s reference isn’t secret: it’s on the order page, in screenshots and
								anywhere the buyer shared it. Their profile isn’t proof either. Confirm through the
								order’s contact email before you add it.
							</p>
							<dl class="facts">
								<div>
									<dt>Contact email on this order</dt>
									<dd>
										<span class="mono">{o.contact_email}</span>
										<span class="chip-s chip-neutral">chef only</span>
									</dd>
								</div>
								<div>
									<dt>A one-off code for this check</dt>
									<dd>
										<span class="mono">{code}</span>
										<span class="small muted"
											>(for a new email; use it once). If you already emailed this address, check
											the reply against the code in the email you sent.</span
										>
									</dd>
								</div>
							</dl>
							<ol class="steps small">
								<li>
									<strong>You</strong> email <span class="mono">{o.contact_email}</span> from
									<span class="mono">{BUSINESS_EMAIL}</span> with the code, and
									<strong>without</strong> the request reference (message below).
								</li>
								<li>
									Wait for a reply <strong>from that address</strong> that quotes your code
									<em>and</em> the request reference <span class="mono">{shortRef(c.id)}</span>.
									Only someone who reads that mailbox has the code; only the person signed in to
									this account can see the reference.
								</li>
								<li>
									If nobody replies, the reply lacks either, or the buyer says it wasn’t them, don’t
									add it: decline, or leave it waiting.
								</li>
							</ol>
							{#if isUpcoming(o)}
								<p class="small wrong-person">
									<ChefIcon name="alert" cls="note-icon" /><span
										>This order is still upcoming: whoever it’s added to {ownerCan(o)}. Take extra
										care, or wait for the order-page route.</span
									>
								</p>
							{/if}
							<details class="draft" bind:open={emailOpen}>
								<summary class="btn btn-small">Show the email to the order’s address</summary>
								<div class="draft-body">
									<p class="small">To: <span class="mono">{o.contact_email}</span></p>
									<p class="mail-body">{email}</p>
									<button class="btn btn-small" type="button" onclick={() => void copyEmail(email)}
										>Copy the email</button
									>
									<p class="small" role="status">{copied}</p>
									<p class="small muted">
										Never include the request reference in this email: the real buyer could reply
										with it and confirm someone else’s request.
									</p>
								</div>
							</details>
							<p class="small muted">
								Why not ask the requester to email you from that address? A sender address can be
								forged, and email warnings catch only some of it. Your email to the address, with a
								code, can’t be answered without reading that mailbox, and it also tells the real
								buyer if someone else asked. Matching the address is your check, never automatic.
							</p>
						</section>
					{/if}
				{/if}
			</div>

			<div class="claim-side">
				<section class="card decision sticky" aria-labelledby="dec-h">
					<h2 id="dec-h" class="card-h" tabindex="-1">
						{c.state === 'pending' || savedNow ? 'Your decision' : 'Decision'}
					</h2>
					{#if c.state !== 'pending' && !savedNow}
						<p class="small">
							{c.state === 'withdrawn'
								? 'The customer withdrew this request.'
								: `Already decided: “${state[0]}”.`} Nothing to decide.
						</p>
						<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- backHref resolve()s the route -->
						<a class="btn btn-small" href={backHref}>Back to the list</a>
					{:else if step === 'review'}
						{#if !future && picked}
							<p class="small">
								Chosen: {orderLine(picked)} · name on the order
								<strong>{picked.customer_full_name}</strong>{#if picked.contact_email}&nbsp;·
									contact email <span class="mono">{picked.contact_email}</span>{/if}
							</p>
							<p class="small warn-text">
								Confirm through that address before adding (see “Before adding”).
							</p>
							{#if isUpcoming(picked)}<p class="small bad-text">
									Still upcoming: whoever it’s added to {ownerCan(picked)}. Take extra care (see
									“Before adding”).
								</p>{/if}
						{/if}
						<div class="actions stack">
							{#if picked && (future || picked.contact_email)}
								<button class="btn btn-primary" type="button" onclick={() => go('approve')}
									>{future
										? 'Add to their account'
										: `Add order ${shortRef(picked.id)} to their account`}</button
								>
							{:else}
								<button class="btn btn-primary" type="button" disabled aria-describedby="need-pick"
									>Add to their account</button
								>
								<p id="need-pick" class="small muted">
									{future
										? 'The order they asked from is loading.'
										: 'Choose an order under “Find the order” first.'}
								</p>
							{/if}
							<button class="btn" type="button" onclick={() => go('decline')}>Decline</button>
						</div>
						<p class="small muted">
							Not sure? Leave it waiting and <a href={resolve('/admin/requests')}>go back</a>;
							nothing changes.
						</p>
					{:else if step === 'approve' && picked}
						{@const o = picked}
						<form class="confirm-box" novalidate aria-labelledby="ca-h" onsubmit={submit}>
							<p id="ca-h" class="strong" tabindex="-1" data-focus>
								Add order <span class="mono">{shortRef(o.id)}</span> to {theirAccount}?
							</p>
							<ul class="small list-tight">
								<li>The order appears in their account, in their order list.</li>
								<li>{GUEST_GONE}</li>
								<li>Their request shows “Added to your account”.</li>
								<li>
									If the order is already in their account, it’s just marked added; nothing else
									changes.
								</li>
							</ul>
							<p class="small wrong-person">
								<ChefIcon name="alert" cls="note-icon" />
								{#if isUpcoming(o)}
									<span
										>If this is the wrong person, they see the order in their account, including the
										name and contact email{o.contact_email ? ` (${o.contact_email})` : ''} on it and the
										handover details. Because it’s still upcoming, they can also
										<strong>cancel it</strong> (the refund, 100% or 50% under the cancellation
										policy, goes back to the order’s own payment, not to them){#if canMovePickup(o)},
											may be able to <strong>move its pickup time</strong> to another start that day (until
											6 hours before the current start){/if}, or answer a late-payment question if
										there is one. The real buyer’s guest access stops, so they couldn’t step in, and
										there’s no undo here (removing an order from an account isn’t built).</span
									>
								{:else}
									<span
										>If this is the wrong person, they see the order in their account, including the
										name and contact email{o.contact_email ? ` (${o.contact_email})` : ''} on it and the
										handover details; the real buyer’s guest access stops; and there’s no undo here (removing
										an order from an account isn’t built). The order is {o.lifecycle === 'cancelled'
											? 'cancelled'
											: 'handed over'}, so they can’t change it.</span
									>
								{/if}
							</p>
							{#if needsCheck}
								<label class="check"
									><input
										id="confirm-check"
										type="checkbox"
										bind:checked={confirmed}
										aria-describedby="conf-err"
										aria-invalid={confirmError ? 'true' : undefined}
									/><span
										>I emailed the order’s contact address (<span class="mono"
											>{o.contact_email}</span
										>) and got a reply from it quoting my code and their request reference</span
									></label
								>
								<p class="small bad-text" id="conf-err" role="alert">{confirmError}</p>
								{@render noteField(
									confirmPlaceholder(o.contact_email ?? '', code, c),
									'How you confirmed (required) · internal note, never shown to the customer'
								)}
							{:else}
								<p class="small muted">
									They asked from the order’s guest page, which shows they had guest access.
								</p>
								{@render noteField(
									'e.g. Asked from the order page; name matches',
									'Internal note (optional) · only you see it, never the customer'
								)}
							{/if}
							<div class="actions">
								<button class="btn btn-primary" type="submit">Yes, add to their account</button>
								<button class="btn btn-quiet" type="button" onclick={() => go('review')}
									>Not yet</button
								>
							</div>
						</form>
					{:else if step === 'decline'}
						<form class="confirm-box" novalidate aria-labelledby="cd-h" onsubmit={submit}>
							<p id="cd-h" class="strong" tabindex="-1" data-focus>
								Decline request <span class="mono">{shortRef(c.id)}</span>?
							</p>
							<ul class="small list-tight">
								<li>Nothing is added to their account.</li>
								<li>
									They see only “Couldn’t add”, with “from the details given” and a request to
									contact us: no reason, and nothing about any order.
								</li>
								{#if future}<li>{GUEST_KEPT}</li>{/if}
							</ul>
							{@render noteField(
								'e.g. No historical Instagram orders in the system yet',
								'Internal note (optional) · only you see it, never the customer'
							)}
							<div class="actions">
								<button class="btn btn-danger" type="submit">Decline request</button>
								<button class="btn btn-quiet" type="button" onclick={() => go('review')}
									>Not yet</button
								>
							</div>
						</form>
					{:else if step === 'busy'}
						<div class="actions stack">
							<button
								class="btn {decision === 'approve' ? 'btn-primary' : 'btn-danger'}"
								type="button"
								disabled
								aria-busy="true"
								><span class="spinner light" aria-hidden="true"></span>{decision === 'approve'
									? 'Adding…'
									: 'Declining…'}</button
							>
						</div>
						<p class="small" role="status" tabindex="-1" data-focus>
							{decision === 'approve'
								? 'Adding the order to their account…'
								: 'Declining the request…'}
						</p>
					{:else if step === 'done'}
						{#if result === 'ok'}
							<div class="banner banner-ok result" role="status" tabindex="-1" data-focus>
								<ChefIcon name="check" cls="banner-icon" />
								<div>
									{#if decision === 'approve'}
										<p>
											<strong>Added to {theirAccount} ✓</strong> Order
											<span class="mono"
												>{submitted?.order ? shortRef(submitted.order.id) : ''}</span
											> is now in their account.
										</p>
										<ul class="small list-tight">
											<li>Guest access to the order no longer works; they see it by signing in.</li>
											<li>They see “Added to your account”.</li>
										</ul>
									{:else}
										<p>
											<strong>Declined ✓</strong> Nothing was added. They see “Couldn’t add” and are asked
											to contact us.
										</p>
									{/if}
									{#if submitted?.note}<p class="small">
											Your internal note (only you see it): “{submitted.note}”
										</p>{/if}
									<div class="actions">
										{#if following}<a
												class="btn btn-small btn-primary"
												href={resolve('/admin/requests/[id]', { id: following.id })}
												>Next waiting request</a
											>{/if}
										<a class="btn btn-small btn-quiet" href={resolve('/admin/requests')}
											>Back to requests</a
										>
									</div>
								</div>
							</div>
						{:else if result === 'order_owned'}
							<div class="banner banner-bad result" role="alert" tabindex="-1" data-focus>
								<ChefIcon name="alert" cls="banner-icon" />
								<div>
									<p>
										<strong>This order is already in another customer’s account,</strong> so it can’t
										be added to this one. Nothing changed, and the request is still waiting.
									</p>
									<p class="small">
										The workspace doesn’t say which account.{future
											? ' You can decline this request.'
											: ' Choose another order, or decline.'}
									</p>
									<div class="actions">
										{#if !future}<button
												class="btn btn-small"
												type="button"
												onclick={() => {
													pickId = '';
													go('review');
												}}>Choose another order</button
											>{/if}
										<button class="btn btn-small" type="button" onclick={() => go('decline')}
											>Decline</button
										>
									</div>
								</div>
							</div>
						{:else if result === 'stale'}
							<div class="banner banner-warn result" role="alert" tabindex="-1" data-focus>
								<ChefIcon name="alert" cls="banner-icon" />
								<div>
									<p>
										<strong>This request changed while you were looking.</strong> It was decided on another
										device, withdrawn by the customer, or closed because they deleted their account. Nothing
										was saved.
									</p>
									<div class="actions">
										<button class="btn btn-small" type="button" onclick={() => void load()}
											>Show the latest</button
										>
									</div>
								</div>
							</div>
						{:else if result === 'not_found_order' || result === 'unexpected'}
							<div class="banner banner-bad result" role="alert" tabindex="-1" data-focus>
								<ChefIcon name="alert" cls="banner-icon" />
								<div>
									{#if result === 'unexpected' || decision === 'decline'}
										<p>
											<strong>The decision wasn’t accepted.</strong> Nothing changed, and the
											request is still waiting.{decision === 'decline'
												? ' Check the note and try again.'
												: ''}
										</p>
										<div class="actions">
											<button class="btn btn-small" type="button" onclick={() => go(decision)}
												>Back to the decision</button
											>
											<button
												class="btn btn-small btn-quiet"
												type="button"
												onclick={() => void load()}>Show the latest</button
											>
										</div>
									{:else if future}
										<p>
											<strong>That isn’t the order they asked from.</strong> For this request only
											order
											<span class="mono">{c.order_id ? shortRef(c.order_id) : ''}</span> can be added.
											Nothing changed.
										</p>
										<div class="actions">
											<button class="btn btn-small" type="button" onclick={() => void load()}
												>Reload the request</button
											>
										</div>
									{:else}
										<p>
											<strong>That order can’t be found any more.</strong> Nothing changed. Search again
											and choose an order that’s in the list.
										</p>
										<div class="actions">
											<button
												class="btn btn-small"
												type="button"
												onclick={() => {
													pickId = '';
													go('review');
													void runSearch();
												}}>Search again</button
											>
										</div>
									{/if}
								</div>
							</div>
						{:else if result === 'account_unavailable'}
							<div class="banner banner-bad result" role="alert" tabindex="-1" data-focus>
								<ChefIcon name="alert" cls="banner-icon" />
								<div>
									<p>
										<strong>Their account is disabled,</strong> so nothing can be added to it. Nothing
										changed.
									</p>
									<div class="actions">
										<button class="btn btn-small" type="button" onclick={() => go('decline')}
											>Decline</button
										>
										<a class="btn btn-small btn-quiet" href={resolve('/admin/requests')}
											>Back to requests</a
										>
									</div>
								</div>
							</div>
						{:else if result === 'paused'}
							<div class="banner banner-warn result" role="alert" tabindex="-1" data-focus>
								<ChefIcon name="clock" cls="banner-icon" />
								<div>
									<p>
										<strong>Adding orders to accounts is paused for a moment</strong> while the data is
										being restored. Nothing changed.
									</p>
									<p class="small">Try again in a few minutes.</p>
									<div class="actions">
										<button
											class="btn btn-small"
											type="button"
											onclick={() => void submit(undefined, true)}>Try again</button
										>
									</div>
								</div>
							</div>
						{:else}
							<div class="banner banner-warn result" role="alert" tabindex="-1" data-focus>
								<ChefIcon name="alert" cls="banner-icon" />
								<div>
									<p>
										<strong>We couldn’t tell whether it was saved:</strong> the connection dropped before
										the answer arrived.
									</p>
									<p class="small">
										Try again. It’s safe: the same decision is never applied twice.
									</p>
									<div class="actions">
										<button
											class="btn btn-small"
											type="button"
											onclick={() => void submit(undefined, true)}>Try again</button
										>
									</div>
								</div>
							</div>
						{/if}
					{/if}
					<p class="small muted foot">
						Only you (chef) can decide. Every decision is recorded with your staff account and the
						time.
					</p>
				</section>
			</div>
		</div>
	{/if}
</ChefShell>
