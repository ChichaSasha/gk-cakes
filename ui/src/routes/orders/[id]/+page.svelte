<script lang="ts">
	// Order status (GK-025). Reads the order with this tab's guest capability
	// or the signed-in customer's token and shows its lifecycle in customer
	// words (orderView). The return from the payment page
	// (?payment=returned) only changes the wording to "still checking"; the
	// API's order snapshot alone says whether anything was paid or confirmed.
	// GK-030: cancellation with the API's preview, refund progress and the
	// same-day time change (GK-017 r02b states 12, 13 and 18).
	import '../../../lib/checkout/checkout.css';
	import '../../../lib/checkout/changes.css';
	import { onMount, tick } from 'svelte';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { ApiError } from '../../../lib/api';
	import { orderCapability, takeOrderProblem } from '../../../lib/guest';
	import { accountToken, api, changesApi, checkoutConfigured } from '../../../lib/checkout/client';
	import {
		changePoll,
		deliveryPaid,
		mayCancel,
		refundProgress,
		timeChange
	} from '../../../lib/checkout/changes';
	import CancelPanel from '../../../lib/checkout/ui/CancelPanel.svelte';
	import ChangeTime from '../../../lib/checkout/ui/ChangeTime.svelte';
	import RefundProgress from '../../../lib/checkout/ui/RefundProgress.svelte';
	import { ActionKeys, startPaymentFromOrder, type Who } from '../../../lib/checkout/flow';
	import {
		dateLabel,
		formatMoney,
		modeLabel,
		momentLabel,
		sizeLabel,
		timeRange
	} from '../../../lib/checkout/format';
	import { orderProblemFor, type Problem } from '../../../lib/checkout/messages';
	import {
		isPaymentPageUrl,
		newOrderQuery,
		nextPollDelay,
		orderView,
		POLL_LIMIT_MS,
		withOrderProblem
	} from '../../../lib/checkout/order';
	import type { Order } from '../../../lib/checkout/types';

	const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
	const orderId = page.params.id ?? '';
	const returned = page.url.searchParams.get('payment') === 'returned';
	const keys = new ActionKeys();

	let order = $state<Order | null>(null);
	let unavailable = $state('');
	/** A refusal of this page's own action (kept until the next action). */
	let actionProblem = $state('');
	/** A failed read of the order (cleared by the next successful read). */
	let loadProblem = $state('');
	/**
	 * A problem after this order was created: handed over from checkout, or a
	 * dead end (lead time missed, price expired) answered to this page's own
	 * action. Shown until the next action.
	 */
	let handoff = $state<Problem | null>(null);
	let busy = $state(false);
	let busyText = $state('');
	let pollStopped = $state(false);
	let who = $state<Who | null>(null);
	/** The cancellation panel is open. */
	let cancelOpen = $state(false);
	/** The amount of the preview this page confirmed, while its cancellation is secured. */
	let cancelledMinor = $state<number | undefined>(undefined);
	/** This page asked for a cancellation and waits for its outcome. */
	let awaitingCancel = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;
	let attempt = 0;
	let pollStarted = 0;
	let active = true;

	// A dead end (lead time missed, price expired) offers a new order
	// instead of paying or confirming this one.
	const view = $derived(
		order ? withOrderProblem(orderView(order, { returned, cancelledMinor }), handoff?.next) : null
	);
	const canCancel = $derived(order ? mayCancel(order, who) : false);
	const change = $derived(order ? timeChange(order, who) : 'none');
	const refundSummary = $derived(
		order?.refund ? refundProgress(order.refund, order.currency) : null
	);

	// The outcome of this page's own cancellation: a refusal (the order
	// changed meanwhile) leaves the order as it was, and says so.
	$effect(() => {
		if (!awaitingCancel || !order) return;
		const state = order.cancellation?.state;
		if (state === 'refused') {
			awaitingCancel = false;
			cancelledMinor = undefined;
			actionProblem =
				'We couldn’t cancel your order because it changed while we were cancelling it. It hasn’t been cancelled: you can try again.';
		} else if (state === 'cancelled' || order.lifecycle === 'cancelled') {
			awaitingCancel = false;
		}
	});

	onMount(() => {
		void start();
		// Back from the payment page via the browser's back button: the page
		// may come from the back/forward cache with a button still busy.
		const shown = (e: PageTransitionEvent) => {
			if (!e.persisted) return;
			busy = false;
			busyText = '';
			refresh();
		};
		window.addEventListener('pageshow', shown);
		return () => {
			active = false;
			clearTimeout(timer);
			window.removeEventListener('pageshow', shown);
		};
	});

	async function start() {
		const handed = UUID.test(orderId) ? takeOrderProblem(orderId) : undefined;
		if (handed) {
			handoff = orderProblemFor(
				new ApiError(handed.status, {
					code: handed.code,
					retryable: handed.retryable,
					request_id: handed.request_id
				})
			);
		}
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
			loadProblem = '';
		} catch (e) {
			if (!active) return;
			if (e instanceof ApiError && (e.status === 404 || e.status === 401)) {
				unavailable =
					e.status === 401
						? 'Your session has ended. Please sign in again to see this order.'
						: 'We can’t find this order. If you ordered as a guest, open it in the tab where you ordered.';
				return;
			}
			loadProblem = orderProblemFor(e).message;
		}
		schedule();
	}

	function schedule() {
		clearTimeout(timer);
		if (!active || !order) return;
		const v = orderView(order, { returned, cancelledMinor });
		if (!v.poll && !changePoll(order) && !awaitingCancel) {
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

	function openCancel() {
		actionProblem = '';
		cancelOpen = true;
	}

	async function closeCancel(problem?: string) {
		cancelOpen = false;
		if (problem) {
			actionProblem = problem;
			refresh();
		}
		await tick();
		document.getElementById('preview-cancel')?.focus();
	}

	async function cancelled(amountMinor: number) {
		cancelOpen = false;
		cancelledMinor = amountMinor;
		awaitingCancel = true;
		actionProblem = '';
		refresh();
		await tick();
		document.getElementById('status-h')?.focus();
	}

	async function act() {
		if (!order || !view || !who || busy) return;
		if (view.action === 'new_order') {
			const query = newOrderQuery(order);
			busy = true;
			busyText = 'Starting a new order…';
			window.location.assign(query ? `${resolve('/checkout')}?${query}` : resolve('/'));
			return;
		}
		actionProblem = '';
		handoff = null;
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
			const problem = orderProblemFor(e);
			// A dead end stays shown and swaps the action for a new order.
			if (problem.next === 'new_order') handoff = problem;
			else actionProblem = problem.message;
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
					<h2 class="co-h oc-focus" id="status-h" tabindex="-1">{view.title}</h2>
					<div class="notice {view.tone}">
						{#each view.lines as line (line)}<p>{line}</p>{/each}
						{#if view.refund}<p>{view.refund}</p>{/if}
					</div>
				</div>
				{#if order.lifecycle === 'cancelled' && order.refund}
					<RefundProgress refund={order.refund} currency={order.currency} />
					<div class="oc-actions">
						<a class="text-link" href="mailto:gk.cake.london@gmail.com">Contact us</a>
					</div>
				{/if}
				{#if handoff}
					<p class="co-errors" role="alert">{handoff.message}</p>
				{/if}
				{#if actionProblem}
					<p class="co-errors" role="alert">{actionProblem}</p>
				{/if}
				{#if loadProblem}
					<p class="co-errors" role="alert">{loadProblem}</p>
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
						We’re still waiting for a result.{view.doNotPayAgain ? ' Please don’t pay again.' : ''}
						Refresh to check, or come back later.
					</p>
				{:else if view.poll}
					<p class="small">This page updates by itself.</p>
				{/if}
			</section>

			{#if cancelOpen && who}
				<CancelPanel
					orderId={order.id}
					{who}
					api={changesApi}
					{keys}
					delivery={deliveryPaid(order)}
					onclose={(p) => void closeCancel(p)}
					oncancelled={(amount) => void cancelled(amount)}
				/>
			{:else if who && (change !== 'none' || canCancel)}
				<section class="co-card" aria-labelledby="change-h">
					{#if change === 'move' || change === 'moving'}
						<h2 class="co-h" id="change-h">Change your time</h2>
						<ChangeTime {order} {who} api={changesApi} {keys} onchanged={refresh} />
					{:else}
						<h2 class="co-h" id="change-h">Change or cancel</h2>
						{#if change === 'contact_chef'}
							<p>
								Need a different delivery time? Please <a href="mailto:gk.cake.london@gmail.com"
									>contact the chef</a
								>: a GK delivery time can only change with the chef’s agreement.
							</p>
						{/if}
					{/if}
					{#if canCancel}
						<div class="oc-actions">
							<button type="button" id="preview-cancel" onclick={openCancel}
								>Preview cancellation</button
							>
						</div>
					{/if}
				</section>
			{/if}

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
						<dt>{view.promise ? 'Date' : 'Requested date'}</dt>
						<dd>{dateLabel(order.date)}{view.promise ? '' : ' (not confirmed yet)'}</dd>
					</div>
					<div>
						<dt>{view.promise ? 'Time' : 'Requested time'}</dt>
						<dd>
							{timeRange(order.slot.starts_at, order.slot.ends_at)} · London time{view.promise
								? ''
								: ' (not confirmed yet)'}
						</dd>
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
					<span>{order.paid_at ? (deliveryPaid(order) ? 'Paid for cakes' : 'Paid') : 'Total'}</span
					><span>{formatMoney(order.total_minor, order.currency)}</span>
				</div>
				{#if deliveryPaid(order) && order.delivery?.paid_minor}
					<div class="totals">
						<span>GK delivery · paid separately</span><span
							>{formatMoney(order.delivery.paid_minor, order.currency)}</span
						>
					</div>
				{/if}
				{#if refundSummary}
					<div class="totals">
						<span>Refund due</span><span>{refundSummary.due}</span>
					</div>
					<div class="totals">
						<span>Returned so far</span><span>{refundSummary.returnedSoFar}</span>
					</div>
				{/if}
				{#if view.refundTerms === 'standard'}
					<p class="small" id="refund-terms">
						Cancel before {momentLabel(order.full_refund_deadline_at)} for a full refund of what you paid.
						From then on, or if the cake isn’t collected, 50% of what you paid, rounded in your favour;
						100% if we have to cancel.
					</p>
				{:else if view.refundTerms === 'agreement'}
					<p class="small" id="refund-terms">
						If the date can’t be agreed, you get everything you paid back.
					</p>
				{/if}
				<p class="small">Order reference: <code>{order.id}</code></p>
			</section>
		{/if}
	</div>
</main>
