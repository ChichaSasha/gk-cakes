<script lang="ts">
	// Order status (GK-030): the authoritative order snapshot from the API,
	// read with the customer's session or this browser's guest capability.
	// It polls until the order reaches a settled state. Returning from the
	// payment page (?payment=returned) never claims success by itself.
	import '../../../lib/shop.css';
	import { onMount } from 'svelte';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { ApiError, type ApiOptions } from '../../../lib/api';
	import { customerApi, shopConfigured, signInConfigured } from '../../../lib/auth';
	import {
		ActionKeys,
		checkoutProblem,
		confirmFree,
		getOrder,
		isId,
		nextPollSeconds,
		orderCapability,
		orderStatus,
		refundView,
		restartQuery,
		startPayment,
		takeHandoff,
		type Order
	} from '../../../lib/checkout';
	import { dateLabel, deadlineLabel, formatMoney, slotLabel } from '../../../lib/format';

	const id = $derived(page.params.id ?? '');
	const returned = $derived(page.url.searchParams.get('payment') === 'returned');

	let order = $state<Order | null>(null);
	let loadProblem = $state('');
	let notFound = $state(false);
	let actionProblem = $state('');
	let flash = $state('');
	let busy = $state(false);
	let checking = $state(false);
	let redirecting = $state(false);
	let announcement = $state('');

	const status = $derived(order ? orderStatus(order) : null);
	const refund = $derived(order ? refundView(order.refund) : undefined);

	const MODES: Record<string, string> = {
		collection: 'Collect · St Katharine Docks',
		customer_courier: 'Your courier collects',
		gk_delivery: 'GK delivery'
	};

	const keys = new ActionKeys();
	let opts: ApiOptions = { baseUrl: '' };
	let guest = false;
	let capability: string | undefined;
	let autopay = false;
	let pollHint = 3;
	let attempt = 0;
	let timer: ReturnType<typeof setTimeout> | undefined;
	let active = true;

	async function read(): Promise<Order> {
		opts = await customerApi();
		capability = orderCapability(localStorage, id);
		if (opts.token) {
			try {
				guest = false;
				return await getOrder(opts, id);
			} catch (e) {
				// A guest order placed in this browser before signing in, or an
				// ended session: fall back to the stored capability.
				const fallback =
					e instanceof ApiError && (e.status === 404 || e.status === 401) && capability;
				if (!fallback) throw e;
			}
		}
		if (!capability) throw new ApiError(404, { code: 'NOT_FOUND' });
		guest = true;
		return getOrder({ baseUrl: opts.baseUrl }, id, capability);
	}

	function commandOpts(): ApiOptions {
		return guest ? { baseUrl: opts.baseUrl } : opts;
	}

	async function refresh(): Promise<void> {
		clearTimeout(timer);
		try {
			const next = await read();
			if (!active) return;
			const before = order ? orderStatus(order).kind : null;
			order = next;
			loadProblem = '';
			const now = orderStatus(next);
			if (now.kind !== before) {
				announcement = `${now.title}. ${now.message}`;
				attempt = 0;
			}
			if (autopay && now.action.kind === 'open') {
				autopay = false;
				redirecting = true;
				announcement = 'Opening secure payment…';
				location.assign(now.action.url);
				return;
			}
		} catch (e) {
			if (!active) return;
			if (e instanceof ApiError && e.status === 404) {
				notFound = true;
				return;
			}
			loadProblem = order
				? 'We couldn’t refresh this order just now. The details below may be out of date.'
				: 'We couldn’t load this order right now. Please try again in a moment.';
		}
		schedule();
	}

	function schedule(): void {
		clearTimeout(timer);
		if (!active || notFound || document.hidden) return;
		if (order && orderStatus(order).terminal) return;
		timer = setTimeout(() => void refresh(), nextPollSeconds(attempt++, pollHint) * 1000);
	}

	async function checkNow(): Promise<void> {
		checking = true;
		attempt = 0;
		await refresh();
		checking = false;
	}

	async function pay(kind: 'start' | 'free'): Promise<void> {
		if (!order || busy) return;
		busy = true;
		actionProblem = '';
		const action = kind === 'start' ? 'payment' : 'free-confirmation';
		const key = keys.keyFor(action, { order: order.id, expected_version: order.version });
		try {
			const res =
				kind === 'start'
					? await startPayment(commandOpts(), order.id, order.version, key, capability)
					: await confirmFree(commandOpts(), order.id, order.version, key, capability);
			pollHint = res.poll_after_seconds || pollHint;
			autopay = kind === 'start';
			attempt = 0;
			await refresh();
		} catch (e) {
			const p = checkoutProblem(e);
			actionProblem = p.message;
			if (p.kind === 'in_progress' || p.kind === 'stale' || p.kind === 'quote_expired') {
				await refresh();
			}
		} finally {
			busy = false;
		}
	}

	onMount(() => {
		if (!shopConfigured || !isId(id)) {
			notFound = isId(id) ? false : true;
			if (!shopConfigured) loadProblem = 'Orders can’t be shown right now. Please try again later.';
			return;
		}
		const handoff = takeHandoff(sessionStorage, id);
		autopay = handoff.autopay === true;
		flash = handoff.message ?? '';
		pollHint = handoff.pollAfter ?? 3;
		void refresh();
		const onVisible = () => {
			if (!document.hidden) {
				attempt = 0;
				void refresh();
			} else clearTimeout(timer);
		};
		document.addEventListener('visibilitychange', onVisible);
		return () => {
			active = false;
			clearTimeout(timer);
			document.removeEventListener('visibilitychange', onVisible);
		};
	});
</script>

<svelte:head>
	<title>{status ? status.title : 'Your order'} · GK Cakes</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="shop">
	<main class="wrap" id="main">
		<p class="sr-only" role="status" aria-live="polite">{announcement}</p>
		{#if notFound}
			<div class="page-title"><h1>Order not found</h1></div>
			<div class="narrow">
				<div class="notice error">
					<div>
						<p>We couldn’t find this order from this browser.</p>
						<p class="small">
							If you ordered as a guest, open this page in the browser you ordered in.
							{#if signInConfigured}If you were signed in, sign in again.{/if}
						</p>
					</div>
				</div>
				{#if signInConfigured}
					<a class="button" href={resolve('/sign-in')}>Sign in</a>
				{/if}
			</div>
		{:else if !order}
			<div class="page-title"><h1>Your order</h1></div>
			<div class="narrow">
				{#if loadProblem}
					<div class="notice error" role="alert"><div><p>{loadProblem}</p></div></div>
					{#if shopConfigured}
						<button type="button" onclick={checkNow} disabled={checking}>Try again</button>
					{/if}
				{:else}
					<p role="status">Loading your order…</p>
				{/if}
			</div>
		{:else if status}
			<div class="page-title"><h1>{status.title}</h1></div>
			<div class="narrow">
				{#if returned && !status.terminal}
					<p class="small quiet">
						You’re back from the secure payment page. This page shows what we know about your
						payment; it updates by itself.
					</p>
				{/if}
				{#if flash}
					<div class="notice error"><div><p>{flash}</p></div></div>
				{/if}
				<div class="notice {status.tone}">
					<div>
						<p>{status.message}</p>
						{#if status.doNotPayAgain && !status.message.includes('do not pay again')}
							<p><strong>Please don’t pay again.</strong></p>
						{/if}
					</div>
				</div>
				{#if loadProblem}
					<p class="small" role="alert">{loadProblem}</p>
				{/if}

				{#if redirecting}
					<p class="small quiet">Opening secure payment…</p>
				{:else if status.action.kind === 'open'}
					<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- Stripe's hosted page, checked by safeCheckoutUrl -->
					<a class="button primary" href={status.action.url} rel="noreferrer">
						Continue to secure payment · {formatMoney(order.total_minor)}
						<span class="arrow" aria-hidden="true">→</span>
					</a>
				{:else if status.action.kind === 'start' || status.action.kind === 'free'}
					<button
						type="button"
						class="primary"
						aria-disabled={busy}
						onclick={() => pay(status.action.kind === 'free' ? 'free' : 'start')}
					>
						{#if busy}
							<span
								>{status.action.kind === 'free' ? 'Confirming…' : 'Opening secure payment…'}</span
							>
							<span class="spinner" aria-hidden="true"></span>
						{:else if status.action.kind === 'free'}
							Confirm my order <span class="arrow" aria-hidden="true">→</span>
						{:else}
							Continue to secure payment · {formatMoney(order.total_minor)}
							<span class="arrow" aria-hidden="true">→</span>
						{/if}
					</button>
				{/if}
				{#if actionProblem}
					<p class="field-error" role="alert">{actionProblem}</p>
				{/if}
				{#if !status.terminal}
					<p>
						<button type="button" class="text-link" onclick={checkNow} disabled={checking}>
							{checking ? 'Checking…' : 'Check again'}
						</button>
					</p>
				{/if}

				<section class="card" aria-labelledby="order-heading">
					<h2 id="order-heading" class="label">Your order</h2>
					{#each order.items ?? [] as item (item.line_no)}
						<div class="item">
							<div>
								<strong>{item.cake_name}</strong>
								<span>
									{item.flavour} · {item.size_code.toUpperCase()}{item.quantity > 1
										? ` · ${item.quantity} cakes`
										: ''}
								</span>
							</div>
							<span class="price">
								{formatMoney(item.unit_price_minor)}{item.quantity > 1 ? ' each' : ''}
							</span>
						</div>
					{/each}
					<dl class="meta">
						<div>
							<dt>Date</dt>
							<dd>{dateLabel(order.date)}</dd>
						</div>
						<div>
							<dt>Time</dt>
							<dd>{slotLabel(order.slot.starts_at, order.slot.ends_at)} · London time</dd>
						</div>
						<div>
							<dt>How you’ll get it</dt>
							<dd>{MODES[order.fulfillment_mode] ?? order.fulfillment_mode}</dd>
						</div>
						<div>
							<dt>Name</dt>
							<dd>{order.customer_full_name}</dd>
						</div>
						{#if order.different_payer_name}
							<div>
								<dt>Paid by</dt>
								<dd>{order.different_payer_name}</dd>
							</div>
						{/if}
					</dl>
					<div class="totals">
						<span>Cakes</span><span>{formatMoney(order.subtotal_minor)}</span>
					</div>
					{#if order.discount_minor > 0}
						<div class="totals">
							<span>Discount</span><span>{formatMoney(-order.discount_minor)}</span>
						</div>
					{/if}
					<div class="totals">
						<span
							>{order.fulfillment_mode === 'customer_courier' ? 'Your courier' : 'Collection'}</span
						>
						<span>{order.fulfillment_mode === 'customer_courier' ? 'Arranged by you' : 'Free'}</span
						>
					</div>
					<div class="totals total">
						<span>{order.paid_at ? 'Paid' : 'Total'}</span><span
							>{formatMoney(order.total_minor)}</span
						>
					</div>
					<p class="small quiet">In pounds (GBP).</p>
				</section>

				{#if status.kind === 'confirmed'}
					<section class="inset" aria-labelledby="refund-heading">
						<h2 id="refund-heading" class="label">Cancellation and refunds</h2>
						<p>
							Cancel before <strong>{deadlineLabel(order.full_refund_deadline_at)}</strong> for a
							full refund of <strong>{formatMoney(order.total_minor)}</strong>.
						</p>
						<p class="small quiet">
							Moving your date or time doesn’t change this deadline. To cancel, please contact us.
						</p>
					</section>
				{/if}

				{#if refund}
					<section class="inset" aria-labelledby="refund-state-heading">
						<h2 id="refund-state-heading" class="label">Refund</h2>
						{#if refund.done}
							<p>Refunded: <strong>{formatMoney(refund.returnedMinor)}</strong>.</p>
						{:else}
							<p>Refund requested: <strong>{formatMoney(refund.requestedMinor)}</strong>.</p>
							<p>
								Returned so far: <strong>{formatMoney(refund.returnedMinor)}</strong>. We’ll update
								this page when the rest is returned.
							</p>
						{/if}
					</section>
				{/if}

				{#if status.kind === 'cancelled'}
					<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- resolve() plus the basket query -->
					<a class="button" href={resolve('/checkout') + restartQuery(order.items)}
						>Start a new order</a
					>
				{/if}
				<p class="small quiet">Order reference: <code>{order.id}</code></p>
			</div>
		{/if}
	</main>
</div>
