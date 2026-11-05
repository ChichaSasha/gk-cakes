<script lang="ts">
	// Checkout (GK-025) following the Creator-approved GK-017 r01 sketch: five
	// sections and an order summary with the server's quote totals. Continue
	// creates the order from the reviewed quote with the consented policy,
	// then sends the customer to Stripe's secure payment page (or confirms a
	// £0 order). The basket comes from the URL until GK-016 adds the real one;
	// this page is not linked from navigation yet.
	import '../../lib/checkout/checkout.css';
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { ApiError } from '../../lib/api';
	import { lastOrder } from '../../lib/guest';
	import { madeToOrderQuantity, parseBasket } from '../../lib/checkout/basket';
	import { accountToken, api, checkoutConfigured } from '../../lib/checkout/client';
	import {
		ActionKeys,
		BasketProblem,
		forgetQuote,
		placeOrder,
		requestQuote,
		resolveBasket,
		type BasketItem,
		type QuoteRequest
	} from '../../lib/checkout/flow';
	import {
		addDays,
		dateLabel,
		formatMoney,
		londonToday,
		modeLabel,
		timeRange
	} from '../../lib/checkout/format';
	import { problemFor, type Problem } from '../../lib/checkout/messages';
	import type { Day, FulfillmentMode, Policy, Quote, Slot } from '../../lib/checkout/types';
	import Allergens from '../../lib/checkout/ui/Allergens.svelte';
	import OrderSummary from '../../lib/checkout/ui/OrderSummary.svelte';
	import PaymentMethods from '../../lib/checkout/ui/PaymentMethods.svelte';
	import RefundTerms from '../../lib/checkout/ui/RefundTerms.svelte';

	const basket = parseBasket(page.url.searchParams);
	const dayQuantity = Math.max(1, madeToOrderQuantity(basket.lines));
	const keys = new ActionKeys();
	const previousOrder = lastOrder();
	const WINDOW_DAYS = 31; // the availability API answers at most 31 dates
	const MAX_WINDOWS = 6;

	let phase = $state<'loading' | 'ready' | 'blocked'>('loading');
	let blockedMessage = $state('');
	let items = $state<BasketItem[]>([]);
	let token = $state<string | undefined>(undefined);
	let policy = $state<Policy | null>(null);

	let days = $state<Day[]>([]);
	let nextWindow = $state('');
	let windowsLoaded = $state(0);
	let datesLoading = $state(false);
	let date = $state('');
	let mode = $state<FulfillmentMode>('collection');
	let slots = $state<Slot[]>([]);
	let slotsLoading = $state(false);
	let slotStatus = $state('');
	let slotId = $state('');
	let slotRev = 0;

	let quote = $state<Quote | null>(null);
	let proof: string | undefined;
	let quoteLoading = $state(false);
	let quoteRev = 0;

	let codeInput = $state('');
	let appliedCode = $state<string | null>(null);
	let codeBusy = $state(false);
	let codeStatus = $state<{ text: string; kind: '' | 'success' | 'error' }>({
		text: 'One code per order.',
		kind: ''
	});

	let fullName = $state('');
	let email = $state('');
	let otherPayer = $state(false);
	let payerName = $state('');
	let reviewed = $state(false);

	type Field = 'date' | 'slot' | 'name' | 'email' | 'payer' | 'reviewed' | 'code';
	let fieldErrors = $state<Partial<Record<Field, string>>>({});
	let summaryErrors = $state<{ id: Field; text: string }[]>([]);
	let problem = $state<Problem | null>(null);
	let submitting = $state(false);
	let submitStatus = $state('');

	let errorBox = $state<HTMLElement | null>(null);
	let problemBox = $state<HTMLElement | null>(null);
	let codeField = $state<HTMLInputElement | null>(null);
	let codeChip = $state<HTMLElement | null>(null);

	const slot = $derived(slots.find((s) => s.id === slotId) ?? null);
	const codeDescription = $derived(
		quote?.discount?.kind === 'percentage' && quote.discount.basis_points
			? `${quote.discount.basis_points / 100}% off cakes`
			: ''
	);
	const free = $derived(quote?.total_minor === 0);
	const payLabel = $derived(
		submitting
			? submitStatus
			: !quote
				? 'Continue to secure payment'
				: free
					? 'Continue · confirm your free order'
					: `Continue to secure payment · ${formatMoney(quote.total_minor, quote.currency)}`
	);

	function request(code: string | null = appliedCode): QuoteRequest {
		return { date, slotId, mode, lines: basket.lines, code: code ?? undefined };
	}

	onMount(() => {
		void start();
	});

	async function start() {
		if (basket.problem) {
			block(
				basket.problem === 'too_many'
					? 'An order can have at most 20 cakes. Please go back and remove some.'
					: 'Your basket is empty. Choose a cake to order first.'
			);
			return;
		}
		if (!checkoutConfigured) {
			block('Online ordering isn’t available on this site yet.');
			return;
		}
		phase = 'loading';
		try {
			const [t, resolved, current] = await Promise.all([
				accountToken(),
				resolveBasket(api, basket.lines),
				api.policy()
			]);
			token = t;
			items = resolved;
			policy = current;
			phase = 'ready';
			await loadDates(true);
		} catch (e) {
			if (e instanceof BasketProblem) {
				block(
					'A cake in your basket isn’t available to order online right now. Please go back and choose again.'
				);
			} else if (e instanceof ApiError && e.status === 404) {
				block('Online ordering isn’t open yet. Please contact us to order.');
			} else {
				block(problemFor(e).message);
			}
		}
	}

	function block(message: string) {
		blockedMessage = message;
		phase = 'blocked';
	}

	async function loadDates(initial = false) {
		datesLoading = true;
		try {
			let from = nextWindow || londonToday();
			do {
				const to = addDays(from, WINDOW_DAYS - 1);
				const a = await api.availability(from, to, dayQuantity);
				days = [...days, ...a.days.filter((d) => d.status === 'open')];
				windowsLoaded++;
				from = addDays(to, 1);
				nextWindow = from;
			} while (initial && days.length === 0 && windowsLoaded < 3);
			if (initial && basket.date && days.some((d) => d.date === basket.date)) {
				date = basket.date;
				await loadSlots();
			}
		} catch (e) {
			problem = problemFor(e);
		} finally {
			datesLoading = false;
		}
	}

	async function loadSlots() {
		if (!date) return;
		const rev = ++slotRev;
		const previous = slot;
		slotsLoading = true;
		slotStatus = 'Loading available times…';
		try {
			const c = await api.schedule(date, mode, dayQuantity);
			if (rev !== slotRev) return;
			slots = c.slots.filter((s) => s.availability === 'available');
			const kept = previous && slots.some((s) => s.id === previous.id);
			if (kept) {
				slotStatus = `Your time ${timeRange(previous.starts_at, previous.ends_at)} is still available.`;
			} else {
				slotId = '';
				quote = null;
				slotStatus = previous
					? `${timeRange(previous.starts_at, previous.ends_at)} isn’t available for this option. Choose another time.`
					: slots.length > 0
						? `${slots.length} ${slots.length === 1 ? 'time' : 'times'} available.`
						: 'No times left on this date. Please choose another date.';
			}
		} catch (e) {
			if (rev !== slotRev) return;
			slots = [];
			slotId = '';
			quote = null;
			slotStatus = problemFor(e).message;
		} finally {
			if (rev === slotRev) slotsLoading = false;
		}
		if (rev === slotRev && slotId) await refreshQuote();
	}

	async function refreshQuote() {
		if (!date || !slotId) {
			quote = null;
			return;
		}
		const rev = ++quoteRev;
		quoteLoading = true;
		try {
			const r = await requestQuote(api, keys, request(), token);
			if (rev !== quoteRev) return;
			quote = r.quote;
			proof = r.proof;
		} catch (e) {
			if (rev !== quoteRev) return;
			quote = null;
			const p = problemFor(e);
			if (p.field === 'code' && appliedCode) {
				const was = appliedCode;
				appliedCode = null;
				codeStatus = {
					text: `${was} no longer applies to this order, so we removed it.`,
					kind: 'error'
				};
				quoteLoading = false;
				await refreshQuote();
				return;
			}
			if (p.next === 'reload_slots' || p.next === 'reload_dates') {
				slotId = '';
				await loadSlots();
				slotStatus = `${p.message} ${slotStatus}`;
			} else {
				problem = p;
			}
		} finally {
			if (rev === quoteRev) quoteLoading = false;
		}
	}

	async function chooseDate() {
		clearError('date');
		problem = null;
		quote = null;
		await loadSlots();
	}

	async function chooseMode() {
		await loadSlots();
	}

	async function chooseSlot() {
		clearError('slot');
		slotStatus = '';
		await refreshQuote();
	}

	async function applyCode() {
		const code = codeInput.trim();
		if (!code) {
			codeStatus = { text: 'Enter a code, or continue without one.', kind: 'error' };
			codeField?.focus();
			return;
		}
		if (!slotId) {
			codeStatus = {
				text: 'Choose a date and time first: we check the code against your order.',
				kind: 'error'
			};
			return;
		}
		const rev = ++quoteRev;
		codeBusy = true;
		quoteLoading = true;
		codeStatus = { text: 'Checking your code…', kind: '' };
		try {
			const r = await requestQuote(api, keys, request(code), token);
			if (rev !== quoteRev) return;
			quote = r.quote;
			proof = r.proof;
			appliedCode = r.quote.discount?.code ?? code.toUpperCase();
			codeInput = '';
			const d = r.quote.discount;
			const what =
				d?.kind === 'percentage' && d.basis_points ? `: ${d.basis_points / 100}% off cakes` : '';
			codeStatus = {
				text: `${appliedCode} applied${what}. New total ${formatMoney(r.quote.total_minor, r.quote.currency)}.`,
				kind: 'success'
			};
			await tick();
			codeChip?.focus();
		} catch (e) {
			if (rev !== quoteRev) return;
			const p = problemFor(e);
			codeStatus = { text: p.message, kind: 'error' };
			if (p.next === 'reload_slots') {
				slotId = '';
				await loadSlots();
			}
		} finally {
			codeBusy = false;
			if (rev === quoteRev) quoteLoading = false;
		}
	}

	async function removeCode() {
		const was = appliedCode;
		appliedCode = null;
		await refreshQuote();
		codeStatus = {
			text: `${was} removed.${quote ? ` New total ${formatMoney(quote.total_minor, quote.currency)}.` : ''} One code per order.`,
			kind: ''
		};
		await tick();
		codeField?.focus();
	}

	function clearError(f: Field) {
		if (fieldErrors[f]) fieldErrors = { ...fieldErrors, [f]: undefined };
		if (summaryErrors.some((e) => e.id === f || (f === 'date' && e.id === 'slot'))) {
			summaryErrors = summaryErrors.filter((e) => e.id !== f);
		}
	}

	function validate(): { id: Field; text: string }[] {
		const errs: { id: Field; text: string }[] = [];
		if (!date) errs.push({ id: 'date', text: 'Choose a date' });
		else if (!slotId) errs.push({ id: 'slot', text: 'Choose a time' });
		const n = fullName.trim();
		if (!n || n.length > 120) errs.push({ id: 'name', text: 'Enter your full name' });
		const e = email.trim();
		if (e.length > 254 || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(e))
			errs.push({ id: 'email', text: 'Enter an email address like name@example.com' });
		if (otherPayer && (!payerName.trim() || payerName.trim().length > 120))
			errs.push({ id: 'payer', text: 'Enter the payer’s full name' });
		if (!reviewed) errs.push({ id: 'reviewed', text: 'Tick to confirm you’ve checked your order' });
		return errs;
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (submitting || phase !== 'ready' || !policy) return;
		problem = null;
		const errs = validate();
		fieldErrors = Object.fromEntries(errs.map((x) => [x.id, `${x.text}.`]));
		summaryErrors = errs;
		if (errs.length > 0) {
			await tick();
			errorBox?.focus();
			return;
		}
		if (quoteLoading || !quote) {
			problem = { message: 'Your total is still updating. Please wait a moment.', next: 'none' };
			await tick();
			problemBox?.focus();
			return;
		}
		submitting = true;
		submitStatus = free ? 'Placing your order…' : 'Opening secure payment…';
		try {
			const current = token ? ((await accountToken()) ?? token) : undefined;
			const result = await placeOrder(api, keys, {
				quote,
				proof,
				token: current,
				policyId: policy.id,
				contact: {
					full_name: fullName.trim(),
					email: email.trim(),
					...(otherPayer ? { payer_name: payerName.trim() } : {})
				}
			});
			if (result.kind === 'redirect') {
				// Top-level navigation to Stripe's hosted page (no Stripe script here).
				window.location.assign(result.url);
				return;
			}
			await goto(resolve('/orders/[id]', { id: result.orderId }));
		} catch (e) {
			submitting = false;
			await handle(problemFor(e));
		}
	}

	async function handle(p: Problem) {
		problem = p;
		switch (p.next) {
			case 'requote':
				forgetQuote(keys, request(), token);
				reviewed = false;
				await refreshQuote();
				break;
			case 'remove_code':
				forgetQuote(keys, request(), token);
				appliedCode = null;
				codeStatus = { text: p.message, kind: 'error' };
				reviewed = false;
				await refreshQuote();
				break;
			case 'reload_policy':
				reviewed = false;
				try {
					policy = await api.policy();
				} catch (e) {
					problem = problemFor(e);
				}
				break;
			case 'reload_slots':
				slotId = '';
				quote = null;
				await loadSlots();
				break;
			case 'reload_dates':
				date = '';
				slotId = '';
				quote = null;
				slots = [];
				days = [];
				nextWindow = '';
				windowsLoaded = 0;
				await loadDates(true);
				break;
			case 'fix_input':
				if (p.field) fieldErrors = { ...fieldErrors, [p.field]: p.message };
				break;
		}
		await tick();
		problemBox?.focus();
	}

	function jump(event: MouseEvent, id: string) {
		event.preventDefault();
		document.getElementById(id)?.focus();
	}
</script>

<svelte:head>
	<title>Checkout · GK Cakes</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<main class="co-page" id="main">
	<div class="page-title">
		<h1>Checkout</h1>
		<p>{token ? 'Signed in · this order is saved to your account' : 'Guest checkout'}</p>
	</div>
	<ol class="co-steps">
		<li>01 · Bag</li>
		<li aria-current="step">02 · Checkout</li>
		<li>03 · Secure payment</li>
		<li>04 · Confirmation</li>
	</ol>
	<p class="co-step-short">Step 2 of 4 · Checkout</p>

	{#if previousOrder && phase !== 'loading'}
		<div class="notice">
			<p>
				You started an order in this tab.
				<a href={resolve('/orders/[id]', { id: previousOrder })}>View its status</a> before paying again.
			</p>
		</div>
	{/if}

	{#if phase === 'loading'}
		<p class="notice" role="status">Loading your order…</p>
	{:else if phase === 'blocked'}
		<div class="notice error" role="alert">
			<p>{blockedMessage}</p>
		</div>
		<p><a href={resolve('/')}>Back to GK Cakes</a></p>
	{:else}
		{#if quote}
			<a class="co-mobile-total" href="#sum-title">
				<span>To pay now</span>
				<strong>{formatMoney(quote.total_minor, quote.currency)}</strong>
				<span class="co-mobile-link">View order<span aria-hidden="true"> ↓</span></span>
			</a>
		{/if}
		<div class="checkout-layout">
			<form id="checkout-form" novalidate onsubmit={submit}>
				{#if summaryErrors.length > 0}
					<div class="co-errors" role="alert" tabindex="-1" bind:this={errorBox}>
						<p>
							Please check {summaryErrors.length === 1
								? '1 thing'
								: `${summaryErrors.length} things`}
							before paying
						</p>
						<ul>
							{#each summaryErrors as err (err.id)}
								<li><a href={`#${err.id}`} onclick={(e) => jump(e, err.id)}>{err.text}</a></li>
							{/each}
						</ul>
					</div>
				{/if}
				{#if problem}
					<div class="co-errors" role="alert" tabindex="-1" bind:this={problemBox}>
						<p>{problem.message}</p>
						{#if problem.next === 'sign_in'}
							<a href={resolve('/sign-in')}>Sign in</a>
						{/if}
						{#if problem.reference}
							<span class="small">Reference: {problem.reference}</span>
						{/if}
					</div>
				{/if}

				<section class="co-card" aria-labelledby="when-h">
					<h2 class="co-h" id="when-h">
						<span class="co-num" aria-hidden="true">1</span>When and how
					</h2>
					<label class="field" for="date">
						Date · London time
						<select
							id="date"
							bind:value={date}
							onchange={chooseDate}
							disabled={datesLoading && days.length === 0}
							aria-invalid={fieldErrors.date ? 'true' : undefined}
							aria-describedby={fieldErrors.date ? 'date-error' : 'date-help'}
						>
							<option value=""
								>{datesLoading && days.length === 0 ? 'Loading dates…' : 'Choose a date'}</option
							>
							{#each days as d (d.date)}
								<option value={d.date}>{dateLabel(d.date)}</option>
							{/each}
						</select>
					</label>
					{#if fieldErrors.date}<p id="date-error" class="field-error">{fieldErrors.date}</p>{/if}
					<div class="date-row">
						<p id="date-help" class="small co-status" role="status">
							{#if datesLoading}Loading available dates…{:else if days.length === 0}No dates are
								available online right now. Please contact us.{/if}
						</p>
						{#if nextWindow && windowsLoaded < MAX_WINDOWS}
							<button
								type="button"
								class="text-link"
								onclick={() => loadDates()}
								disabled={datesLoading}
							>
								Show later dates
							</button>
						{/if}
					</div>

					<fieldset class="co-modes">
						<legend class="sr-only">How you will get your cake</legend>
						<label class="mode-card">
							<input
								type="radio"
								name="fulfilment"
								value="collection"
								bind:group={mode}
								onchange={chooseMode}
							/>
							<span><strong>Collect · Free</strong><small>St Katharine Docks, London.</small></span>
						</label>
						<label class="mode-card">
							<input
								type="radio"
								name="fulfilment"
								value="customer_courier"
								bind:group={mode}
								onchange={chooseMode}
							/>
							<span>
								<strong>Your own courier</strong>
								<small>
									You book and pay your courier to collect at your time. Responsibility passes to
									them at handover.
								</small>
							</span>
						</label>
						<label class="mode-card unavailable">
							<input
								type="radio"
								name="fulfilment"
								value="gk_delivery"
								disabled
								aria-describedby="delivery-note"
							/>
							<span>
								<strong>GK delivery · from £100</strong>
								<small id="delivery-note">
									The chef quotes for your address. Delivery is paid separately. Not available
									online yet: please contact us to arrange it.
								</small>
							</span>
						</label>
					</fieldset>

					<label class="field" for="slot">
						{mode === 'customer_courier'
							? 'Courier collection time · London time'
							: 'Collection time · London time'}
						<select
							id="slot"
							bind:value={slotId}
							onchange={chooseSlot}
							disabled={!date || slotsLoading}
							aria-busy={slotsLoading}
							aria-invalid={fieldErrors.slot ? 'true' : undefined}
							aria-describedby={fieldErrors.slot ? 'slot-status slot-error' : 'slot-status'}
						>
							<option value="">
								{slotsLoading
									? 'Loading available times…'
									: date
										? 'Choose a time'
										: 'Choose a date first'}
							</option>
							{#each slots as s (s.id)}
								<option value={s.id}>{timeRange(s.starts_at, s.ends_at)}</option>
							{/each}
						</select>
					</label>
					{#if fieldErrors.slot}<p id="slot-error" class="field-error">{fieldErrors.slot}</p>{/if}
					<p id="slot-status" class="small co-status" role="status">{slotStatus}</p>
				</section>

				<section class="co-card" aria-labelledby="details-h">
					<h2 class="co-h" id="details-h">
						<span class="co-num" aria-hidden="true">2</span>Your details
					</h2>
					<div class="two-fields">
						<div>
							<label class="field" for="name">
								Full name
								<input
									id="name"
									autocomplete="name"
									maxlength="120"
									bind:value={fullName}
									oninput={() => clearError('name')}
									aria-invalid={fieldErrors.name ? 'true' : undefined}
									aria-describedby={fieldErrors.name ? 'name-error' : undefined}
								/>
							</label>
							{#if fieldErrors.name}<p id="name-error" class="field-error">
									{fieldErrors.name}
								</p>{/if}
						</div>
						<div>
							<label class="field" for="email">
								Email for order updates
								<input
									id="email"
									type="email"
									autocomplete="email"
									maxlength="254"
									bind:value={email}
									oninput={() => clearError('email')}
									aria-invalid={fieldErrors.email ? 'true' : undefined}
									aria-describedby={fieldErrors.email ? 'email-error' : undefined}
								/>
							</label>
							{#if fieldErrors.email}<p id="email-error" class="field-error">
									{fieldErrors.email}
								</p>{/if}
						</div>
					</div>
					<label class="check-line">
						<input type="checkbox" bind:checked={otherPayer} onchange={() => clearError('payer')} />
						<span>Someone else is paying <span class="small">(we record who paid)</span></span>
					</label>
					{#if otherPayer}
						<label class="field" for="payer">
							Payer’s full name
							<input
								id="payer"
								autocomplete="off"
								maxlength="120"
								bind:value={payerName}
								oninput={() => clearError('payer')}
								aria-invalid={fieldErrors.payer ? 'true' : undefined}
								aria-describedby={fieldErrors.payer ? 'payer-error' : undefined}
							/>
						</label>
						{#if fieldErrors.payer}<p id="payer-error" class="field-error">
								{fieldErrors.payer}
							</p>{/if}
					{/if}
					{#if !token}
						<p class="small">
							You can follow this order in this browser tab. <a href={resolve('/sign-in')}
								>Sign in</a
							> first to save it to your account.
						</p>
					{/if}
				</section>

				<section class="co-card" aria-labelledby="code-h">
					<h2 class="co-h" id="code-h">
						<span class="co-num" aria-hidden="true">3</span>Discount code
						<span class="co-optional">optional</span>
					</h2>
					{#if appliedCode}
						<div class="code-chip">
							<span tabindex="-1" bind:this={codeChip}>
								<strong>{appliedCode}</strong>{codeDescription ? ` · ${codeDescription}` : ''}
							</span>
							<button
								type="button"
								class="text-link"
								onclick={removeCode}
								disabled={quoteLoading}
								aria-label={`Remove code ${appliedCode}`}>Remove</button
							>
						</div>
					{:else}
						<div class="code-row">
							<label class="field" for="code">
								<span class="sr-only">Discount code</span>
								<input
									id="code"
									autocomplete="off"
									maxlength="40"
									placeholder="Enter a code"
									bind:value={codeInput}
									bind:this={codeField}
									aria-describedby="code-status"
									aria-invalid={codeStatus.kind === 'error' ? 'true' : undefined}
									onkeydown={(e) => {
										if (e.key === 'Enter') {
											e.preventDefault();
											void applyCode();
										}
									}}
								/>
							</label>
							<button type="button" onclick={applyCode} disabled={codeBusy}>Apply</button>
						</div>
					{/if}
					<p id="code-status" class="code-status {codeStatus.kind}" role="status">
						{codeStatus.text}
					</p>
				</section>

				<section class="co-card" aria-labelledby="check-h">
					<h2 class="co-h" id="check-h">
						<span class="co-num" aria-hidden="true">4</span>Before you pay
					</h2>
					<Allergens {items} />
					<RefundTerms
						startsAt={slot?.starts_at ?? null}
						totalMinor={quote?.total_minor ?? null}
						currency={quote?.currency}
						{mode}
					/>
					<p class="small">
						Nothing in this cancellation and refund policy affects the customer’s statutory rights
						where a product is faulty, not as described or otherwise does not meet applicable legal
						requirements.
					</p>
					{#if policy}
						<p class="small">Our current terms: {policy.summary}</p>
					{/if}
					<label class="check-line">
						<input
							id="reviewed"
							type="checkbox"
							bind:checked={reviewed}
							onchange={() => clearError('reviewed')}
							aria-invalid={fieldErrors.reviewed ? 'true' : undefined}
							aria-describedby={fieldErrors.reviewed ? 'reviewed-error' : undefined}
						/>
						<span>I’ve checked the cake, allergens, date and the refund terms above.</span>
					</label>
					{#if fieldErrors.reviewed}<p id="reviewed-error" class="field-error">
							{fieldErrors.reviewed}
						</p>{/if}
				</section>

				<PaymentMethods />
			</form>

			<OrderSummary
				{items}
				{quote}
				{quoteLoading}
				dateText={date ? dateLabel(date) : 'Choose a date'}
				timeText={slot
					? `${timeRange(slot.starts_at, slot.ends_at)} · London time`
					: 'Choose a time'}
				modeText={modeLabel(mode)}
				fulfilLabel={mode === 'customer_courier' ? 'Your courier' : 'Collection'}
				fulfilValue={mode === 'customer_courier' ? 'Arranged by you' : 'Free'}
			>
				<button
					type="submit"
					form="checkout-form"
					class="primary co-pay"
					aria-disabled={submitting ? 'true' : undefined}
					aria-busy={submitting}
				>
					<span>{payLabel}</span>
					<span class="co-arrow" aria-hidden="true">→</span>
				</button>
				<p class="sr-only" role="status">{submitting ? submitStatus : ''}</p>
			</OrderSummary>
		</div>
	{/if}
</main>
