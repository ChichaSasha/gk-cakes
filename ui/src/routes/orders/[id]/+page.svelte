<script lang="ts">
	// Order status (GK-025). Reads the order with this tab's guest capability
	// or the signed-in customer's token and shows its lifecycle in customer
	// words (orderView). The return from the payment page
	// (?payment=returned) only changes the wording to "still checking"; the
	// API's order snapshot alone says whether anything was paid or confirmed.
	import '../../../lib/checkout/checkout.css';
	import { onMount } from 'svelte';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { ApiError } from '../../../lib/api';
	import { orderCapability } from '../../../lib/guest';
	import { accountToken, api, checkoutConfigured } from '../../../lib/checkout/client';
	import { ActionKeys, startPaymentFromOrder, type Who } from '../../../lib/checkout/flow';
	import {
		dateLabel,
		formatMoney,
		modeLabel,
		momentLabel,
		sizeLabel,
		timeRange
	} from '../../../lib/checkout/format';
	import { problemFor } from '../../../lib/checkout/messages';
	import {
		isPaymentPageUrl,
		nextPollDelay,
		orderView,
		POLL_LIMIT_MS
	} from '../../../lib/checkout/order';
	import type { Order } from '../../../lib/checkout/types';

	const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
	const orderId = page.params.id ?? '';
	const returned = page.url.searchParams.get('payment') === 'returned';
	const keys = new ActionKeys();

	let order = $state<Order | null>(null);
	let unavailable = $state('');
	let actionProblem = $state('');
	let busy = $state(false);
	let busyText = $state('');
	let pollStopped = $state(false);
	let who: Who | null = null;
	let timer: ReturnType<typeof setTimeout> | undefined;
	let attempt = 0;
	let pollStarted = 0;
	let active = true;

	const view = $derived(order ? orderView(order, { returned }) : null);

	onMount(() => {
		void start();
		return () => {
			active = false;
			clearTimeout(timer);
		};
	});

	async function start() {
		if (!checkoutConfigured || !UUID.test(orderId)) {
			unavailable = 'We can’t find this order.';
			return;
		}
		const capability = orderCapability(orderId);
		if (capability) {
			who = { kind: 'guest', secret: capability };
		} else {
			const token = await accountToken();
			if (token) who = { kind: 'account', token };
		}
		if (!who) {
			unavailable =
				'We can’t show this order in this browser. Open it in the tab where you ordered, or sign in if you ordered with your account.';
			return;
		}
		await load();
	}

	async function load() {
		if (!who || !active) return;
		try {
			const o = await api.order(orderId, who);
			if (!active) return;
			order = o;
			actionProblem = '';
		} catch (e) {
			if (!active) return;
			if (e instanceof ApiError && (e.status === 404 || e.status === 401)) {
				unavailable =
					e.status === 401
						? 'Your session has ended. Please sign in again to see this order.'
						: 'We can’t find this order. If you ordered as a guest, open it in the tab where you ordered.';
				return;
			}
			actionProblem = problemFor(e).message;
		}
		schedule();
	}

	function schedule() {
		clearTimeout(timer);
		if (!active || !order) return;
		const v = orderView(order, { returned });
		if (!v.poll) {
			attempt = 0;
			pollStarted = 0;
			return;
		}
		pollStarted ||= Date.now();
		if (Date.now() - pollStarted > POLL_LIMIT_MS) {
			pollStopped = true;
			return;
		}
		timer = setTimeout(() => void load(), nextPollDelay(attempt++));
	}

	function refresh() {
		pollStopped = false;
		attempt = 0;
		pollStarted = 0;
		void load();
	}

	async function act() {
		if (!order || !view || !who || busy) return;
		actionProblem = '';
		if (view.action === 'open_payment' && isPaymentPageUrl(view.checkoutUrl)) {
			busy = true;
			busyText = 'Opening secure payment…';
			window.location.assign(view.checkoutUrl);
			return;
		}
		busy = true;
		try {
			if (view.action === 'start_payment') {
				busyText = 'Opening secure payment…';
				const url = await startPaymentFromOrder(api, keys, order, who);
				if (url) {
					window.location.assign(url);
					return;
				}
			} else if (view.action === 'confirm_free') {
				busyText = 'Confirming your order…';
				await api.confirmFree(
					order.id,
					order.version,
					keys.for(`free:${order.id}:${order.version}`).key,
					who
				);
			}
		} catch (e) {
			actionProblem = problemFor(e).message;
		}
		busy = false;
		busyText = '';
		refresh();
	}
</script>

<svelte:head>
	<title>Your order · GK Cakes</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<main class="co-page" id="main">
	<div class="order-wrap">
		<div class="page-title">
			<h1>Your order</h1>
		</div>

		{#if unavailable}
			<div class="notice error" role="alert"><p>{unavailable}</p></div>
			<p><a href={resolve('/')}>Back to GK Cakes</a></p>
		{:else if !order || !view}
			<p class="notice" role="status">Loading your order…</p>
		{:else}
			<section class="co-card" aria-labelledby="status-h">
				<div aria-live="polite">
					<h2 class="co-h" id="status-h">{view.title}</h2>
					<div class="notice {view.tone}">
						{#each view.lines as line (line)}<p>{line}</p>{/each}
						{#if view.refund}<p>{view.refund}</p>{/if}
					</div>
				</div>
				{#if actionProblem}
					<p class="co-errors" role="alert">{actionProblem}</p>
				{/if}
				{#if view.action !== 'none' || pollStopped}
					<div class="order-actions">
						{#if view.action !== 'none'}
							<button
								type="button"
								class={view.doNotPayAgain ? '' : 'primary co-pay'}
								onclick={act}
								aria-disabled={busy ? 'true' : undefined}
								aria-busy={busy}
							>
								<span>{busy ? busyText : view.actionLabel}</span>
								{#if !view.doNotPayAgain}<span class="co-arrow" aria-hidden="true">→</span>{/if}
							</button>
						{/if}
						{#if pollStopped}
							<button type="button" onclick={refresh}>Refresh status</button>
						{/if}
					</div>
				{/if}
				{#if pollStopped}
					<p class="small" role="status">
						We’re still waiting for a result. Please don’t pay again; refresh to check, or come back
						later.
					</p>
				{:else if view.poll}
					<p class="small">This page updates by itself.</p>
				{/if}
			</section>

			<section class="co-card order-details" aria-labelledby="details-h">
				<h2 class="co-h" id="details-h">Order details</h2>
				<ul class="sum-items">
					{#each order.items ?? [] as item (item.line_no)}
						<li class="sum-item">
							<div>
								<strong>{item.cake_name}</strong>
								<span
									>{item.flavour} · {sizeLabel(item.size_code)}{item.lot_id
										? ' · ready-made'
										: ''}</span
								>
							</div>
							<span class="sum-price"
								>{item.quantity > 1 ? `${item.quantity} × ` : ''}{formatMoney(
									item.unit_price_minor,
									order.currency
								)}</span
							>
						</li>
					{/each}
				</ul>
				<dl class="sum-meta">
					<div>
						<dt>Date</dt>
						<dd>{dateLabel(order.date)}</dd>
					</div>
					<div>
						<dt>Time</dt>
						<dd>{timeRange(order.slot.starts_at, order.slot.ends_at)} · London time</dd>
					</div>
					<div>
						<dt>How you’ll get it</dt>
						<dd>{modeLabel(order.fulfillment_mode)}</dd>
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
					<div>
						<dt>Email</dt>
						<dd>{order.contact_email}</dd>
					</div>
				</dl>
				<div class="totals">
					<span>Cakes</span><span>{formatMoney(order.subtotal_minor, order.currency)}</span>
				</div>
				{#if order.discount_minor > 0}
					<div class="totals">
						<span>Discount</span><span>−{formatMoney(order.discount_minor, order.currency)}</span>
					</div>
				{/if}
				<div class="totals total">
					<span>{order.paid_at ? 'Paid' : 'Total'}</span><span
						>{formatMoney(order.total_minor, order.currency)}</span
					>
				</div>
				{#if order.lifecycle !== 'cancelled'}
					<p class="small">
						Full refund if you cancel before {momentLabel(order.full_refund_deadline_at)}. From then
						on, or if the cake isn’t collected, 50% of what you paid, rounded in your favour; 100%
						if we have to cancel.
					</p>
				{/if}
				<p class="small">Order reference: <code>{order.id}</code></p>
			</section>
		{/if}
	</div>
</main>
