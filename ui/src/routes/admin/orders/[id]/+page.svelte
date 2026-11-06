<script lang="ts">
	// One order (approved GK-018 r03b2 sketch, order.html) for chef,
	// technical support and fulfilment, each seeing what the API gives their
	// role, with the day-to-day steps the API allows: Start making, Mark
	// ready, Out for delivery, a problem, Hand over and (chef) Customer
	// didn't come. Every step sends the version shown and its own key; a
	// changed order is read again and said so. Cancelling, refunds, late
	// payments, delivery quotes and problems and time changes come later.
	import { resolve } from '$app/paths';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount, tick } from 'svelte';
	import { ApiError, apiGet } from '../../../../lib/api';
	import { leaveOnSignOut, signInConfigured, signOut, type Me } from '../../../../lib/auth';
	import { timeRange } from '../../../../lib/checkout/format';
	import type { StaffApi } from '../../../../lib/admin/catalogue';
	import { viewOf, type StaffView } from '../../../../lib/admin/orders';
	import {
		asksFirst,
		availableSteps,
		banners,
		cakeTitle,
		cancelledText,
		commandKey,
		confirmLabel,
		confirmText,
		dayLabel,
		deliveryText,
		detailStatus,
		doneText,
		fulfillmentState,
		historyLines,
		itemDetail,
		latePaymentCard,
		modeText,
		moneyFacts,
		orderIdFrom,
		orderLabel,
		pollStep,
		progressSteps,
		reasonProblem,
		REFUND_WORDS,
		refundFacts,
		refusalOf,
		sendStep,
		staffOrder,
		stepLabel,
		when,
		type StaffOrderDetail,
		type StepAction
	} from '../../../../lib/admin/order-detail';
	import { staffApi } from '../../../../lib/admin/session';
	import ChefIcon from '../../../../lib/admin/ui/ChefIcon.svelte';
	import ChefShell from '../../../../lib/admin/ui/ChefShell.svelte';
	import '../../../../lib/admin/order-detail.css';

	const orderId = orderIdFrom(page.params.id ?? '');

	let api: StaffApi | null = null;
	let view = $state<StaffView | null>(null);
	let order = $state<StaffOrderDetail | null>(null);
	let phase = $state<'loading' | 'ready' | 'error' | 'missing'>('loading');
	let problem = $state('');
	let now = $state(new Date());
	// After a 409 STALE_VERSION: the latest version is shown with a banner.
	let stale = $state(false);
	let busy = $state<StepAction | null>(null);
	let asking = $state<StepAction | null>(null);
	let reason = $state('');
	let reasonError = $state('');
	let stepStatus = $state('');
	let stepError = $state('');
	let stillPending = $state<{
		operation_id: string;
		poll_after_seconds: number;
		action: StepAction;
	} | null>(null);
	// One key per action at a version: a retry of the same action reuses it
	// (not shown, so a plain record).
	const keys: Record<string, string> = {};
	let mounted = true;
	const isActive = () => mounted;

	let statusEl = $state<HTMLElement | null>(null);
	let errorEl = $state<HTMLElement | null>(null);
	let staleEl = $state<HTMLElement | null>(null);

	const steps = $derived(
		order && view ? availableSteps(order, view, now) : { actions: [], note: '' }
	);
	const status = $derived(order && view ? detailStatus(order, view) : null);
	const notices = $derived(order && view ? banners(order, view, now) : []);
	const late = $derived(order && view ? latePaymentCard(order, view) : null);
	const progress = $derived(order && view ? progressSteps(order, view, now) : []);
	const history = $derived(order ? historyLines(order) : []);
	const money = $derived(order && view === 'chef' ? moneyFacts(order) : []);
	const refund = $derived(order && view === 'chef' ? refundFacts(order) : []);
	const ended = $derived(
		order ? ['cancelled', 'cancellation_pending'].includes(order.lifecycle) : false
	);

	async function sessionEnded(e: unknown): Promise<boolean> {
		if (e instanceof ApiError && e.signInRequired) {
			await signOut();
			await goto(resolve('/sign-in?next=/admin&ended=1'), { replaceState: true });
			return true;
		}
		if (e instanceof ApiError && (e.code === 'FORBIDDEN' || e.code === 'ACCOUNT_DISABLED')) {
			problem =
				e.code === 'FORBIDDEN' ? 'This account has no staff access.' : 'This account is disabled.';
			return true;
		}
		return false;
	}

	async function read(): Promise<StaffOrderDetail> {
		if (!api || !orderId) throw new Error('no order');
		return staffOrder(api, orderId);
	}

	/** Reads the order; quiet keeps what is shown while it reloads. */
	async function load(quiet = false): Promise<boolean> {
		if (!quiet) phase = 'loading';
		try {
			const o = await read();
			if (!isActive()) return false;
			order = o;
			now = new Date();
			phase = 'ready';
			return true;
		} catch (e) {
			if (!isActive() || (await sessionEnded(e))) return false;
			if (e instanceof ApiError && e.status === 404) {
				order = null;
				phase = 'missing';
			} else if (!quiet) {
				phase = 'error';
			} else {
				stepError =
					'The latest version of the order couldn’t be read. Reload the page before you act.';
			}
			return false;
		}
	}

	async function start() {
		if (!api) return;
		if (!orderId) {
			phase = 'missing';
			return;
		}
		if (!view) {
			phase = 'loading';
			try {
				const me = await apiGet<Me>('/api/v1/admin/me', { baseUrl: api.baseUrl, token: api.token });
				if (!isActive()) return;
				view = viewOf(me.capabilities);
				if (!view) {
					problem = 'This account has no staff access.';
					return;
				}
			} catch (e) {
				if (!isActive() || (await sessionEnded(e))) return;
				phase = 'error';
				return;
			}
		}
		await load();
	}

	onMount(() => {
		let active = true;
		(async () => {
			api = signInConfigured ? await staffApi() : null;
			if (!active) return;
			if (!api) {
				await goto(resolve('/sign-in?next=/admin'), { replaceState: true });
				return;
			}
			await start();
		})();
		// Eligibility that depends on the time (the no-show) follows the clock.
		const clock = setInterval(() => (now = new Date()), 30_000);
		const unsubscribe = leaveOnSignOut(() => {
			order = null;
			void goto(resolve('/sign-in?next=/admin'), { replaceState: true });
		});
		return () => {
			active = false;
			mounted = false;
			clearInterval(clock);
			unsubscribe();
		};
	});

	async function leave() {
		order = null;
		const reached = await signOut();
		if (!reached) {
			problem =
				'You are signed out on this device, but the sign-in service could not be reached to end the session everywhere.';
			return;
		}
		await goto(resolve('/'), { replaceState: true });
	}

	async function focusLater(el: () => HTMLElement | null) {
		await tick();
		el()?.focus();
	}

	function choose(a: StepAction) {
		stepError = '';
		stepStatus = '';
		if (!asksFirst(a)) {
			void run(a);
			return;
		}
		asking = a;
		reason = '';
		reasonError = '';
		void focusLater(() =>
			document.getElementById(a === 'delivery_exception' ? 'problem-reason' : 'confirm-step')
		);
	}

	function notYet() {
		const a = asking;
		asking = null;
		if (a) void focusLater(() => document.getElementById(`step-${a}`));
	}

	function nonce(): string {
		return crypto.randomUUID().slice(0, 8);
	}

	async function run(a: StepAction) {
		const o = order;
		if (!api || !o || busy) return;
		if (a === 'delivery_exception') {
			reasonError = reasonProblem(reason);
			if (reasonError) {
				void focusLater(() => document.getElementById('problem-reason'));
				return;
			}
		}
		busy = a;
		stale = false;
		stepError = '';
		stillPending = null;
		stepStatus = a === 'handover' ? 'Recording the handover…' : 'Recording…';
		const slot = `${a}:${o.version}`;
		const key = keys[slot] ?? commandKey(a, o, nonce());
		keys[slot] = key;
		try {
			const answer = await sendStep(
				api,
				o,
				a,
				key,
				a === 'delivery_exception' ? reason : undefined
			);
			asking = null;
			if (answer.kind === 'done') {
				await load(true);
				stepStatus = doneText(a);
			} else {
				stepStatus =
					a === 'handover' ? 'Recording the handover…' : 'Recording that the customer didn’t come…';
				await load(true);
				void focusLater(() => statusEl);
				const r = await pollStep(read, answer.accepted, { active: isActive });
				if (!isActive()) return;
				if (r.order) order = r.order;
				now = new Date();
				if (r.outcome === 'done' || r.outcome === 'noshow') {
					stepStatus = doneText(a, r.outcome);
				} else if (r.outcome === 'refused') {
					stepStatus = '';
					stepError =
						'It wasn’t recorded: the order changed before it could be. The latest version is shown; check it and try again if it’s still right.';
				} else {
					stepStatus = 'Still being recorded. This page shows it when it’s done.';
					stillPending = { ...answer.accepted, action: a };
				}
			}
			void focusLater(() => (stepError ? errorEl : statusEl));
		} catch (e) {
			const refusal = refusalOf(e, a);
			if (refusal.signIn) {
				await sessionEnded(e);
				return;
			}
			if (!refusal.retry) delete keys[slot];
			stepStatus = '';
			if (refusal.stale) {
				asking = null;
				await load(true);
				stale = true;
				void focusLater(() => staleEl);
				return;
			}
			stepError = refusal.text;
			if (refusal.reload) {
				asking = null;
				await load(true);
			}
			void focusLater(() => errorEl);
		} finally {
			busy = null;
		}
	}

	/** Reads a still-pending handover or no-show again. */
	async function checkAgain() {
		const p = stillPending;
		if (!p || busy) return;
		busy = p.action;
		stepStatus = 'Checking…';
		try {
			const r = await pollStep(
				read,
				{ order_id: orderId ?? '', operation_id: p.operation_id, state: '', poll_after_seconds: 1 },
				{ tries: 1, active: isActive }
			);
			if (r.order) order = r.order;
			now = new Date();
			if (r.outcome === 'done' || r.outcome === 'noshow') {
				stillPending = null;
				stepStatus = doneText(p.action, r.outcome);
			} else if (r.outcome === 'refused') {
				stillPending = null;
				stepStatus = '';
				stepError =
					'It wasn’t recorded: the order changed before it could be. The latest version is shown; check it and try again if it’s still right.';
			} else {
				stepStatus = 'Still being recorded. This page shows it when it’s done.';
			}
		} catch (e) {
			await sessionEnded(e);
		} finally {
			busy = null;
			void focusLater(() => (stepError ? errorEl : statusEl));
		}
	}

	function onConfirmKey(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			e.preventDefault();
			notYet();
		}
	}
</script>

<svelte:head>
	<title
		>{order ? `${cakeTitle(order)} · ${orderLabel(order)}` : 'Order'} · GK Cake London chef workspace</title
	>
	<meta name="robots" content="noindex" />
</svelte:head>

<ChefShell active="orders" {view} onsignout={leave}>
	<div class="order-page">
		<a class="back" href={resolve('/admin/orders')}><ChefIcon name="back" /> Orders</a>
		{#if problem}
			<div class="banner banner-bad" role="alert"><p>{problem}</p></div>
		{:else if phase === 'loading' && !order}
			<p class="muted" role="status">Loading the order…</p>
		{:else if phase === 'missing'}
			<div class="card empty-card">
				<h1>Order not found</h1>
				<p>This order doesn’t exist, or it isn’t one your role can see.</p>
				<a class="btn" href={resolve('/admin/orders')}>Back to orders</a>
			</div>
		{:else if phase === 'error'}
			<div class="banner banner-bad" role="alert">
				<div>
					<p>The order could not be loaded right now.</p>
					<button class="btn" type="button" onclick={() => void start()}>Try again</button>
				</div>
			</div>
		{:else if order && view && status}
			{@const o = order}
			{@const v = view}
			{#if stale}
				<div class="banner banner-warn" role="alert" tabindex="-1" bind:this={staleEl}>
					<ChefIcon name="alert" cls="banner-icon" />
					<div>
						<p>
							<strong>This order changed while you were looking.</strong> The latest version is shown
							below. Review it before you act.
						</p>
					</div>
				</div>
			{/if}
			{#each notices as b (b.key)}
				<div class="banner banner-{b.tone}">
					<ChefIcon name={b.tone === 'info' ? 'clock' : 'alert'} cls="banner-icon" />
					<div>
						<p><strong>{b.title}</strong> {b.text}</p>
						{#each b.small as line (line)}<p class="small">{line}</p>{/each}
					</div>
				</div>
			{/each}

			<header class="page-head">
				<div>
					<p class="eyebrow">{orderLabel(o)} · placed {when(o.placed_at)}</p>
					<h1>{cakeTitle(o)}</h1>
					<p class="muted sub">
						{o.customer_full_name} · {dayLabel(o.date)}, {timeRange(
							o.slot.starts_at,
							o.slot.ends_at
						)}
					</p>
				</div>
				<span class="pill pill-{status.stage}" class:pill-released={status.released}
					>{#if status.stage === 'ready' || status.stage === 'done'}<ChefIcon
							name="check"
							cls="pill-icon"
						/>{/if}{status.text}</span
				>
			</header>

			{#if late}
				<section class="card card-alert" aria-labelledby="late-h">
					<h2 id="late-h" class="card-h"><ChefIcon name="alert" />{late.title}</h2>
					{#each late.text as line, i (i)}<p class={i ? 'small muted' : ''}>{line}</p>{/each}
					{#if late.note}<p class="role-note">{late.note}</p>{/if}
				</section>
			{/if}

			<div class="detail-grid">
				<div class="detail-main">
					<section class="card" aria-labelledby="cake-h">
						<h2 id="cake-h" class="card-h">Cake</h2>
						<ul class="items">
							{#each o.items ?? [] as item (item.line_no)}
								<li>
									<p class="strong">{item.cake_name}</p>
									<p class="muted small">{itemDetail(item)}</p>
								</li>
							{:else}
								<li class="muted small">No cakes on this order.</li>
							{/each}
						</ul>
						<p class="muted small">Allergens and the recipe aren’t shown on the order page yet.</p>
					</section>

					<section class="card" aria-labelledby="hand-h">
						<h2 id="hand-h" class="card-h">Handover</h2>
						<dl class="facts">
							<div>
								<dt>Date</dt>
								<dd>{dayLabel(o.date)}</dd>
							</div>
							<div>
								<dt>Time</dt>
								<dd>{timeRange(o.slot.starts_at, o.slot.ends_at)} · London time</dd>
							</div>
							<div>
								<dt>How</dt>
								<dd>{modeText(o.fulfillment_mode)}</dd>
							</div>
							<div>
								<dt>Customer</dt>
								<dd>{o.customer_full_name}</dd>
							</div>
							{#if o.contact_email}<div>
									<dt>Email</dt>
									<dd class="wrap">{o.contact_email}</dd>
								</div>{/if}
							<div>
								<dt>Order ID</dt>
								<dd class="mono wrap">{o.id}</dd>
							</div>
						</dl>
						{#if o.appointment_change?.state === 'processing'}
							<p class="small info-text">
								Changing to {dayLabel(o.appointment_change.to.date)}, {timeRange(
									o.appointment_change.to.starts_at,
									o.appointment_change.to.ends_at
								)} · being confirmed.
							</p>
						{/if}
					</section>

					{#if o.delivery || o.fulfillment_mode === 'gk_delivery'}
						<section class="card" aria-labelledby="del-h">
							<h2 id="del-h" class="card-h"><ChefIcon name="truck" />GK delivery</h2>
							<dl class="facts">
								<div>
									<dt>Delivery</dt>
									<dd>{deliveryText(o) || 'Not recorded yet'}</dd>
								</div>
							</dl>
							{#if fulfillmentState(o) === 'out_for_delivery' && o.lifecycle === 'ready'}
								<p>
									<strong
										>Out for delivery{o.fulfillment?.at
											? ` since ${when(o.fulfillment.at)}`
											: ''}.</strong
									>
								</p>
								<p class="small muted">
									GK is responsible until the cake is handed to the customer. A failed delivery is a
									problem to resolve, never “completed”.
								</p>
							{/if}
							{#if o.delivery_choice?.open}
								<p class="small">
									The customer can now choose another way to get the cake: collection, their own
									courier or a full refund.
								</p>
							{:else if o.delivery_choice?.reason === 'not_possible' || o.delivery_choice?.not_possible}
								<p class="small">
									GK can’t deliver this order (recorded). The customer chooses another way.
								</p>
							{/if}
							{#if o.delivery_failure}
								<p class="small">
									{o.delivery_failure.cause === 'nobody_available'
										? 'Nobody was available to take the delivery'
										: 'The delivery failed on our side'}
									({when(o.delivery_failure.at)}).{o.delivery_failure.open
										? ' The customer is choosing what happens next.'
										: ''}
								</p>
							{/if}
							{#if v === 'chef'}
								<p class="small muted">
									Quotes, waivers and recording a delivery that can’t happen or failed aren’t on
									this page yet.
								</p>
							{/if}
						</section>
					{/if}

					{#if history.length}
						<section class="card" aria-labelledby="rec-h">
							<h2 id="rec-h" class="card-h">Record</h2>
							<ol class="record">
								{#each history as line, i (i)}
									<li><span class="muted small">{when(line.at)}</span> <span>{line.text}</span></li>
								{/each}
							</ol>
						</section>
					{/if}
				</div>

				<div class="detail-side">
					<section class="card" aria-labelledby="prog-h">
						<h2 id="prog-h" class="card-h">Progress</h2>
						<ol class="progress">
							{#each progress as s, i (i)}
								<li class:done={s.done}>
									<span class="p-dot" aria-hidden="true"></span>
									<div>
										<p>{s.label}<span class="sr-only">{s.done ? ' (done)' : ''}</span></p>
										<p class="muted small">
											{s.at
												? `${s.done ? '' : 'Scheduled '}${when(s.at)}`
												: s.done
													? ''
													: 'Not yet'}{s.note ? `${s.at || !s.done ? ' · ' : ''}${s.note}` : ''}
										</p>
									</div>
								</li>
							{/each}
						</ol>
						{#if v === 'technical_support'}
							<p class="role-note">Technical support can look but not change orders.</p>
						{:else if steps.actions.length}
							<div class="actions stack">
								{#each steps.actions as a, i (a)}
									{#if asking === a}
										{#if a === 'delivery_exception'}
											<form
												class="confirm-row"
												aria-label="Record a problem"
												novalidate
												onsubmit={(e) => {
													e.preventDefault();
													void run(a);
												}}
											>
												<label class="field" for="problem-reason">{confirmText(a, o, v)}</label>
												<input
													id="problem-reason"
													class="text-input"
													autocomplete="off"
													maxlength="500"
													bind:value={reason}
													onkeydown={onConfirmKey}
													aria-invalid={reasonError ? 'true' : undefined}
													aria-describedby={reasonError ? 'problem-error' : undefined}
												/>
												{#if reasonError}<p id="problem-error" class="small bad-text">
														{reasonError}
													</p>{/if}
												<button class="btn btn-primary" type="submit" disabled={busy !== null}
													>{busy === a ? 'Recording…' : confirmLabel(a)}</button
												>
												<button class="btn btn-quiet" type="button" onclick={notYet}>Not now</button
												>
											</form>
										{:else}
											<div
												class="confirm-row"
												role="group"
												aria-label={a === 'handover' ? 'Confirm handover' : 'Confirm not collected'}
											>
												<p class="small"><strong>{confirmText(a, o, v)}</strong></p>
												<button
													id="confirm-step"
													class="btn {a === 'no_show' ? 'btn-danger' : 'btn-primary'}"
													type="button"
													disabled={busy !== null}
													onkeydown={onConfirmKey}
													onclick={() => void run(a)}
													>{busy === a ? 'Recording…' : confirmLabel(a)}</button
												>
												<button
													class="btn btn-quiet"
													type="button"
													onkeydown={onConfirmKey}
													onclick={notYet}>Not yet</button
												>
											</div>
										{/if}
									{:else}
										<button
											id="step-{a}"
											class="btn {i === 0 || a === 'handover'
												? 'btn-primary'
												: a === 'delivery_exception'
													? 'btn-quiet'
													: ''}"
											type="button"
											disabled={busy !== null || (asking !== null && asking !== a)}
											onclick={() => choose(a)}
											>{busy === a && !asksFirst(a) ? 'Recording…' : stepLabel(a, o)}</button
										>
									{/if}
								{/each}
							</div>
						{/if}
						{#if steps.note && v !== 'technical_support'}<p class="role-note">{steps.note}</p>{/if}
						<p class="small step-status" role="status" tabindex="-1" bind:this={statusEl}>
							{stepStatus}
						</p>
						{#if stillPending}
							<button
								class="btn btn-small"
								type="button"
								disabled={busy !== null}
								onclick={() => void checkAgain()}>Check again</button
							>
						{/if}
						<div role="alert" class="step-error-wrap">
							{#if stepError}<p class="small bad-text step-error" tabindex="-1" bind:this={errorEl}>
									{stepError}
								</p>{/if}
						</div>
					</section>

					{#if ended || o.refund}
						<section class="card" aria-labelledby="ref-h">
							<div class="card-row">
								<h2 id="ref-h" class="card-h">
									{o.lifecycle === 'cancellation_pending'
										? 'Cancelling'
										: status.released
											? status.text
											: o.refund && !ended
												? 'Refund'
												: 'Cancelled'}
								</h2>
								{#if o.refund?.state && REFUND_WORDS[o.refund.state]}
									<span class="chip-s chip-{REFUND_WORDS[o.refund.state][1]}"
										>{REFUND_WORDS[o.refund.state][0]}</span
									>
								{/if}
							</div>
							{#if o.lifecycle === 'cancelled'}<p class="small">{cancelledText(o, v)}</p>{/if}
							{#if refund.length}
								<dl class="ledger">
									{#each refund as f (f.label)}
										<div>
											<dt>{f.label}</dt>
											<dd class={f.tone ? `${f.tone}-text` : ''}>{f.value}</dd>
										</div>
									{/each}
								</dl>
								{#if o.refund?.in_progress_minor}
									<p class="small muted">
										Money sent but not confirmed still counts: if the provider later says it failed,
										it becomes owed again. We never send a second refund while one may still
										succeed.
									</p>
								{/if}
							{/if}
							{#if v === 'chef'}
								<p class="role-note">Sending refunds from this page comes in a later update.</p>
							{/if}
						</section>
					{/if}

					{#if money.length && !ended}
						<section class="card" aria-labelledby="money-h">
							<h2 id="money-h" class="card-h">Money</h2>
							<dl class="facts">
								{#each money as f (f.label)}
									<div>
										<dt>{f.label}</dt>
										<dd class={f.tone ? `${f.tone}-text` : 'strong'}>{f.value}</dd>
									</div>
								{/each}
							</dl>
							{#if o.lifecycle === 'awaiting_payment'}
								<p class="role-note">
									Nothing can be cancelled or refunded until the payment result is known.
								</p>
							{:else if o.lifecycle === 'confirmation_pending'}
								<p class="role-note">Cancelling is available once the order is confirmed.</p>
							{/if}
						</section>
					{/if}
				</div>
			</div>
			{#if v === 'chef'}
				<p class="muted small foot">
					Not on this page yet: cancelling and refunds, late-payment decisions, delivery quotes and
					problems, and moving the time or date.
				</p>
			{/if}
		{/if}
	</div>
</ChefShell>
