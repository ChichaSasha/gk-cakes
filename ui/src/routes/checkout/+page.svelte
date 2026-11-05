<script lang="ts">
	// Checkout (GK-030), following the GK-017 r01 sketch: when and how, your
	// details, one discount code, the checks before paying, and payment. All
	// totals come from server quotes (K03); "Continue to secure payment"
	// refreshes the quote if anything changed, creates the order (202: payment
	// pending, never accepted), starts the payment or the free confirmation
	// and goes to the order page, which opens Stripe's hosted page.
	// The basket comes from the URL until the discovery pages exist:
	// /checkout?item=<variant_id>&item=...&lot=<lot_id>.
	import '../../lib/shop.css';
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { ApiError, type ApiOptions } from '../../lib/api';
	import { customerApi, shopConfigured } from '../../lib/auth';
	import {
		ActionKeys,
		basketItems,
		checkoutProblem,
		confirmFree,
		createCheckout,
		createQuote,
		findCakesForVariants,
		getAvailability,
		getOrder,
		getPolicy,
		getReadyCakes,
		getSchedule,
		newGuestProof,
		parseBasket,
		quoteUsable,
		saveOrderCapability,
		setHandoff,
		startPayment,
		type AvailabilityDay,
		type BasketItem,
		type FulfillmentMode,
		type Policy,
		type Quote,
		type QuoteInput,
		type Slot
	} from '../../lib/checkout';
	import {
		addDays,
		dateLabel,
		deadlineLabel,
		formatMoney,
		londonToday,
		refundDeadline,
		shortDateLabel,
		slotLabel
	} from '../../lib/format';

	type Field = 'date' | 'slot' | 'name' | 'email' | 'payer' | 'reviewed' | 'code-input';

	const WINDOW_DAYS = 31;
	const METHODS = [
		'Cards',
		'Apple Pay',
		'Onelink',
		'Klarna',
		'Revolut Pay',
		'Amazon Pay',
		'Cartes Bancaires',
		'Samsung Pay',
		'Satispay',
		'MB WAY',
		'Bancontact',
		'BLIK',
		'EPS',
		'Pix',
		'Kakao Pay',
		'Naver Pay',
		'PAYCO'
	];
	const DAY_STATUS: Record<string, string> = {
		open: 'Available',
		full: 'Full',
		lead_time: 'Too soon',
		closed: 'Closed',
		outside_horizon: 'Not open yet',
		no_times: 'No times'
	};
	const MODE_SUMMARY: Record<string, string> = {
		collection: 'Collect · St Katharine Docks',
		customer_courier: 'Your courier collects'
	};
	const STATUTORY =
		'Nothing in this cancellation and refund policy affects the customer’s statutory rights where a product is faulty, not as described or otherwise does not meet applicable legal requirements.';

	const basket = parseBasket(page.url.searchParams);

	// Loaded data
	let opts: ApiOptions = { baseUrl: '' };
	let signedIn = $state(false);
	let items = $state<BasketItem[]>([]);
	let policy = $state<Policy | null>(null);
	let loading = $state(true);
	let pageProblem = $state('');

	// When and how
	let windowStart = $state('');
	let days = $state<AvailabilityDay[]>([]);
	let daysLoading = $state(false);
	let daysProblem = $state('');
	let pickerOpen = $state(true);
	let date = $state('');
	let mode = $state<FulfillmentMode>('collection');
	let slots = $state<Slot[]>([]);
	let slotsLoading = $state(false);
	let slotId = $state('');
	let slotStatus = $state('');

	// Your details
	let fullName = $state('');
	let email = $state('');
	let otherPayer = $state(false);
	let payerName = $state('');

	// Discount code and quote
	let codeInput = $state('');
	let appliedCode = $state('');
	let codeStatus = $state<{ text: string; kind: '' | 'success' | 'error' }>({
		text: 'One code per order.',
		kind: ''
	});
	let quote = $state<Quote | null>(null);
	let quoteSig = $state('');
	let quoteLoading = $state(false);
	let quoteProblem = $state('');
	let quoteProof: string | undefined;
	let attemptSig = '';
	let attemptProof: string | undefined;
	let quoteRevision = 0;

	// Before you pay, payment
	let reviewed = $state(false);
	let errors = $state<Partial<Record<Field, string>>>({});
	let summaryErrors = $state<[Field, string][]>([]);
	let submitting = $state(false);
	let payProblem = $state('');
	let payStatus = $state('');

	const keys = new ActionKeys();

	const slot = $derived(slots.find((s) => s.id === slotId));
	const lines = basket.lines;
	const inputs = $derived<QuoteInput | null>(
		date && slotId
			? {
					date,
					slot_id: slotId,
					fulfillment_mode: mode,
					lines,
					...(appliedCode ? { discount_code: appliedCode } : {})
				}
			: null
	);
	const currentQuote = $derived(
		quote && inputs && quoteSig === JSON.stringify(inputs) ? quote : null
	);
	const blocked = $derived(items.some((i) => i.problem));
	const notices = $derived([...new Set(items.map((i) => i.notice).filter(Boolean))]);
	const deadline = $derived(slot ? deadlineLabel(refundDeadline(slot.starts_at)) : '');
	const free = $derived(currentQuote?.total_minor === 0);
	const payLabel = $derived(
		currentQuote
			? free
				? 'Confirm your order · £0'
				: `Continue to secure payment · ${formatMoney(currentQuote.total_minor)}`
			: 'Continue to secure payment'
	);

	// -----------------------------------------------------------------------
	// Loading

	onMount(() => {
		void load();
	});

	async function load(): Promise<void> {
		if (!shopConfigured) {
			pageProblem = 'Checkout isn’t available right now. Please try again later.';
			loading = false;
			return;
		}
		if (basket.problem || lines.length === 0) {
			pageProblem = basket.problem ?? 'Your basket is empty. Choose a cake to check out.';
			loading = false;
			return;
		}
		try {
			opts = await customerApi();
			signedIn = Boolean(opts.token);
			const pub = { baseUrl: opts.baseUrl };
			const variantIds = lines.flatMap((l) => (l.variant_id ? [l.variant_id] : []));
			const needLots = lines.some((l) => l.lot_id);
			const [ready, pol] = await Promise.all([
				needLots ? getReadyCakes(pub) : Promise.resolve([]),
				getPolicy(pub).catch((e) => {
					if (e instanceof ApiError && e.status === 404) return null;
					throw e;
				})
			]);
			const lotVariants = lines.flatMap((l) => {
				const lot = ready.find((r) => r.id === l.lot_id);
				return lot ? [lot.variant_id] : [];
			});
			const cakes = await findCakesForVariants(pub, [...variantIds, ...lotVariants]);
			items = basketItems(lines, cakes, ready);
			policy = pol;
			if (!pol) pageProblem = 'Online checkout isn’t open yet. Please try again later.';
		} catch {
			pageProblem = 'We couldn’t load your order details. Please try again in a moment.';
		} finally {
			loading = false;
		}
		if (!pageProblem) void loadDays(londonToday());
	}

	async function loadDays(from: string): Promise<void> {
		windowStart = from;
		daysLoading = true;
		daysProblem = '';
		try {
			const a = await getAvailability(
				{ baseUrl: opts.baseUrl },
				from,
				addDays(from, WINDOW_DAYS - 1),
				basket.madeToOrder || undefined
			);
			if (windowStart !== from) return;
			days = a.days;
		} catch {
			if (windowStart !== from) return;
			days = [];
			daysProblem = 'We couldn’t load available dates. Please try again.';
		} finally {
			if (windowStart === from) daysLoading = false;
		}
	}

	function chooseDate(d: string): void {
		date = d;
		pickerOpen = false;
		clearError('date');
		void loadSlots();
	}

	function chooseMode(m: FulfillmentMode): void {
		if (m === mode) return;
		mode = m;
		void loadSlots();
	}

	let slotRevision = 0;
	async function loadSlots(): Promise<void> {
		if (!date) return;
		const rev = ++slotRevision;
		const previous = slot;
		slotsLoading = true;
		slotStatus = 'Loading available times…';
		try {
			const c = await getSchedule(
				{ baseUrl: opts.baseUrl },
				date,
				mode,
				basket.madeToOrder || undefined
			);
			if (rev !== slotRevision) return;
			const offered = c.slots.filter((s) => s.availability === 'available');
			slots = offered;
			const kept = previous
				? offered.find((s) => s.id === previous.id || s.starts_at === previous.starts_at)
				: undefined;
			slotId = kept?.id ?? '';
			const label = previous ? slotLabel(previous.starts_at, previous.ends_at) : '';
			slotStatus = kept
				? `Your time ${label} is still available.`
				: previous
					? `${label} isn’t available for this option. Choose another time.`
					: offered.length === 0
						? 'No times are available on this date. Choose another date.'
						: `${offered.length} ${offered.length === 1 ? 'time' : 'times'} available.`;
		} catch {
			if (rev !== slotRevision) return;
			slots = [];
			slotId = '';
			slotStatus = 'We couldn’t load times for this date. Please try again.';
		} finally {
			if (rev === slotRevision) slotsLoading = false;
		}
	}

	// -----------------------------------------------------------------------
	// Quotes: the server prices every change (K03).

	let quoteTimer: ReturnType<typeof setTimeout> | undefined;
	$effect(() => {
		const inp = inputs;
		clearTimeout(quoteTimer);
		if (!inp || blocked || pageProblem) return;
		if (quoteSig === JSON.stringify(inp)) return;
		quoteTimer = setTimeout(() => void refreshQuote(inp).catch(() => {}), 250);
		return () => clearTimeout(quoteTimer);
	});

	/** Requests a quote for inp; a new request (not a retry) uses a fresh guest proof. */
	async function fetchQuote(inp: QuoteInput, fresh = false): Promise<Quote> {
		const sig = JSON.stringify(inp);
		if (fresh || sig !== attemptSig) {
			attemptSig = sig;
			attemptProof = signedIn ? undefined : newGuestProof();
		}
		const body: QuoteInput = { ...inp, ...(attemptProof ? { guest_proof: attemptProof } : {}) };
		const q = await createQuote(opts, body, keys.keyFor('quote', body));
		quoteProof = attemptProof;
		attemptSig = ''; // the next request for the same choice needs a new proof
		return q;
	}

	/** Refreshes the shown quote; returns it, or throws after showing the problem. */
	async function refreshQuote(inp: QuoteInput, fresh = false): Promise<Quote> {
		const rev = ++quoteRevision;
		quoteLoading = true;
		quoteProblem = '';
		try {
			const q = await fetchQuote(inp, fresh);
			if (rev === quoteRevision) {
				quote = q;
				quoteSig = JSON.stringify(inp);
			}
			return q;
		} catch (e) {
			if (rev === quoteRevision) handleQuoteError(e, inp);
			throw e;
		} finally {
			if (rev === quoteRevision) quoteLoading = false;
		}
	}

	function handleQuoteError(e: unknown, inp: QuoteInput): void {
		const p = checkoutProblem(e);
		if (p.field === 'code' && inp.discount_code) {
			appliedCode = '';
			codeStatus = { text: p.message, kind: 'error' };
			return;
		}
		if (p.field === 'slot') {
			setError('slot', p.message);
			slotId = '';
			void loadSlots();
			return;
		}
		quoteProblem = p.message;
	}

	async function applyCode(): Promise<void> {
		const code = codeInput.trim();
		if (!code) {
			codeStatus = { text: 'Enter a code, or continue without one.', kind: 'error' };
			return;
		}
		if (appliedCode) {
			codeStatus = { text: `Only one code per order. Remove ${appliedCode} first.`, kind: 'error' };
			return;
		}
		if (!inputs) {
			codeStatus = {
				text: 'Choose a date and time first: your code is checked with your total.',
				kind: 'error'
			};
			return;
		}
		codeStatus = { text: 'Checking your code…', kind: '' };
		const inp = { ...inputs, discount_code: code };
		try {
			const q = await refreshQuote(inp);
			appliedCode = code;
			codeInput = '';
			codeStatus = {
				text: `${q.discount?.code ?? code.toUpperCase()} applied. New total ${formatMoney(q.total_minor)}.`,
				kind: 'success'
			};
			clearError('code-input');
			await tick();
			document.getElementById('code-chip-text')?.focus();
		} catch (e) {
			const p = checkoutProblem(e);
			if (p.field !== 'code') codeStatus = { text: p.message, kind: 'error' };
		}
	}

	async function removeCode(): Promise<void> {
		const was = quote?.discount?.code ?? appliedCode;
		appliedCode = '';
		clearError('code-input');
		codeStatus = { text: `${was} removed. One code per order.`, kind: '' };
		await tick();
		document.getElementById('code-input')?.focus();
	}

	// -----------------------------------------------------------------------
	// Validation and errors

	function setError(field: Field, message: string): void {
		errors = { ...errors, [field]: message };
	}

	function clearError(field: Field): void {
		if (!errors[field]) return;
		const next = { ...errors };
		delete next[field];
		errors = next;
		summaryErrors = summaryErrors.filter(([f]) => f !== field);
	}

	function validate(): [Field, string][] {
		const list: [Field, string][] = [];
		if (!date) list.push(['date', 'Choose a date']);
		else if (!slotId) list.push(['slot', 'Choose a time']);
		if (!fullName.trim()) list.push(['name', 'Enter your full name']);
		if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim()))
			list.push(['email', 'Enter an email address like name@example.com']);
		if (otherPayer && !payerName.trim()) list.push(['payer', 'Enter the payer’s full name']);
		if (!reviewed) list.push(['reviewed', 'Tick to confirm you’ve checked your order']);
		return list;
	}

	async function showErrors(list: [Field, string][]): Promise<void> {
		errors = Object.fromEntries(list);
		summaryErrors = list;
		await tick();
		document.getElementById('error-summary')?.focus();
	}

	function focusField(field: Field): void {
		if (field === 'date') {
			pickerOpen = true;
			void tick().then(() =>
				(
					document.querySelector<HTMLElement>('#date-picker .day:not(:disabled)') ??
					document.getElementById('change-date')
				)?.focus()
			);
			return;
		}
		document.getElementById(field)?.focus();
	}

	function describedBy(field: Field, extra = ''): string | undefined {
		const ids = [extra, errors[field] ? `${field}-error` : ''].filter(Boolean).join(' ');
		return ids || undefined;
	}

	// -----------------------------------------------------------------------
	// Continue to secure payment

	async function submit(event: SubmitEvent): Promise<void> {
		event.preventDefault();
		if (submitting) return;
		payProblem = '';
		const list = validate();
		if (list.length > 0) {
			await showErrors(list);
			return;
		}
		summaryErrors = [];
		if (!inputs || !policy || blocked) return;
		submitting = true;
		payStatus = 'Opening secure payment…';
		try {
			await placeOrder(inputs, policy);
		} finally {
			submitting = false;
			if (payProblem) payStatus = '';
		}
	}

	async function placeOrder(inp: QuoteInput, pol: Policy): Promise<void> {
		// 1. A current, unexpired quote for exactly these choices.
		let q = currentQuote;
		if (!q || !quoteUsable(q)) {
			const shown = q?.total_minor;
			try {
				q = await refreshQuote(inp, true);
			} catch {
				payProblem = quoteProblem || 'Please check the highlighted details.';
				return;
			}
			if (shown !== undefined && q.total_minor !== shown) {
				payProblem = 'A price has changed. Please check the new total, then continue.';
				return;
			}
		}

		// 2. The order (202: payment pending).
		const contact = {
			full_name: fullName.trim(),
			email: email.trim(),
			...(otherPayer ? { payer_name: payerName.trim() } : {})
		};
		const body = {
			quote_id: q.id,
			quote_version: q.version,
			consented_policy_version: pol.id,
			contact,
			...(quoteProof ? { guest_proof: quoteProof } : {})
		};
		let orderId: string;
		let pollAfter: number;
		try {
			const placed = await createCheckout(opts, body, keys.keyFor('checkout', body));
			orderId = placed.order_id;
			pollAfter = placed.poll_after_seconds || 3;
		} catch (e) {
			await checkoutRefused(e, inp);
			return;
		}
		const guestProof = quoteProof;
		if (guestProof) saveOrderCapability(localStorage, orderId, guestProof);

		// 3. Start the payment (or confirm a free order), then the order page.
		const readOpts = guestProof ? { baseUrl: opts.baseUrl } : opts;
		let message: string | undefined;
		let autopay = false;
		try {
			const order = await getOrder(readOpts, orderId, guestProof);
			const cmd = { order: orderId, expected_version: order.version };
			if (order.total_minor === 0) {
				const res = await confirmFree(
					readOpts,
					orderId,
					order.version,
					keys.keyFor('free', cmd),
					guestProof
				);
				pollAfter = res.poll_after_seconds || pollAfter;
			} else {
				const res = await startPayment(
					readOpts,
					orderId,
					order.version,
					keys.keyFor('payment', cmd),
					guestProof
				);
				pollAfter = res.poll_after_seconds || pollAfter;
				autopay = true;
			}
		} catch (e) {
			const p = checkoutProblem(e);
			message = p.message;
			autopay = p.kind === 'in_progress';
		}
		setHandoff(sessionStorage, orderId, { autopay, message, pollAfter });
		await goto(resolve('/orders/[id]', { id: orderId }));
	}

	async function checkoutRefused(e: unknown, inp: QuoteInput): Promise<void> {
		if (e instanceof ApiError && e.status === 404 && e.code !== 'NOT_FOUND') {
			payProblem = 'Online checkout isn’t open yet. Nothing was charged.';
			return;
		}
		const p = checkoutProblem(e);
		switch (p.kind) {
			case 'price_changed':
			case 'quote_expired':
			case 'not_found':
				// K04: review a new quote; guests need a fresh proof for it.
				payProblem = p.kind === 'not_found' ? 'Your total needed refreshing.' : p.message;
				try {
					const q = await refreshQuote(inp, true);
					payProblem += ` To pay now: ${formatMoney(q.total_minor)}.`;
				} catch {
					// refreshQuote showed the problem
				}
				break;
			case 'slot':
			case 'lead_time':
				setError('slot', p.message);
				payProblem = p.message;
				slotId = '';
				void loadSlots();
				break;
			case 'code':
				setError('code-input', p.message);
				codeStatus = { text: p.message, kind: 'error' };
				payProblem = p.message;
				break;
			case 'policy':
				reviewed = false;
				setError('reviewed', p.message);
				payProblem = p.message;
				try {
					policy = await getPolicy({ baseUrl: opts.baseUrl });
				} catch {
					// keep the old text; the tick stays required
				}
				break;
			default:
				payProblem = p.message;
		}
		await tick();
		document.getElementById('pay-problem')?.focus();
	}
</script>

<svelte:head>
	<title>Checkout · GK Cakes</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="shop">
	<main class="wrap checkout" id="main">
		<div class="page-title">
			<h1>Checkout</h1>
			{#if !loading && !pageProblem}
				<p>{signedIn ? 'Signed in' : 'Guest checkout'} · You’ll pay on our secure payment page.</p>
			{/if}
		</div>

		{#if loading}
			<p role="status">Loading your order…</p>
		{:else if pageProblem}
			<div class="narrow">
				<div class="notice error" role="alert"><div><p>{pageProblem}</p></div></div>
			</div>
		{:else}
			<ol class="steps" aria-label="Checkout steps">
				<li>01 · Bag</li>
				<li class="active" aria-current="step">02 · Checkout</li>
				<li>03 · Secure payment</li>
				<li>04 · Confirmation</li>
			</ol>
			<p class="step-short">Step 2 of 4 · Checkout</p>
			<a class="mobile-total" href="#sum-title">
				<span>To pay now</span>
				<strong>{currentQuote ? formatMoney(currentQuote.total_minor) : '—'}</strong>
				<span class="mobile-link">View order<span aria-hidden="true"> ↓</span></span>
			</a>

			<div class="layout">
				<form id="checkout-form" novalidate onsubmit={submit}>
					{#if summaryErrors.length > 0}
						<div id="error-summary" class="errors" role="alert" tabindex="-1">
							<p>
								<strong>
									Please check {summaryErrors.length === 1
										? '1 thing'
										: `${summaryErrors.length} things`} before paying
								</strong>
							</p>
							<ul>
								{#each summaryErrors as [field, text] (field)}
									<li>
										<a
											href={`#${field}`}
											onclick={(e) => {
												e.preventDefault();
												focusField(field);
											}}>{text}</a
										>
									</li>
								{/each}
							</ul>
						</div>
					{/if}

					<!-- 1. When and how -->
					<section class="card" aria-labelledby="h-when">
						<h2 class="h" id="h-when"><span class="num" aria-hidden="true">1</span>When and how</h2>
						{#if date}
							<div class="date-row">
								<span><strong>{dateLabel(date)}</strong> · London time</span>
								<button
									type="button"
									class="text-link"
									id="change-date"
									aria-expanded={pickerOpen}
									aria-controls="date-picker"
									onclick={() => (pickerOpen = !pickerOpen)}
								>
									Change date
								</button>
							</div>
						{/if}
						{#if pickerOpen || !date}
							<div id="date-picker" class="picker">
								<div class="picker-head">
									<button
										type="button"
										class="text-link"
										disabled={daysLoading || windowStart <= londonToday()}
										onclick={() => loadDays(addDays(windowStart, -WINDOW_DAYS))}
									>
										‹ Earlier dates
									</button>
									<p class="small quiet" role="status">
										{daysLoading
											? 'Loading dates…'
											: days.length > 0
												? `${shortDateLabel(days[0].date)} – ${shortDateLabel(days[days.length - 1].date)}`
												: ''}
									</p>
									<button
										type="button"
										class="text-link"
										disabled={daysLoading}
										onclick={() => loadDays(addDays(windowStart, WINDOW_DAYS))}
									>
										Later dates ›
									</button>
								</div>
								{#if daysProblem}
									<p class="field-error" role="alert">
										{daysProblem}
										<button type="button" class="text-link" onclick={() => loadDays(windowStart)}>
											Retry
										</button>
									</p>
								{/if}
								<div
									class="days"
									role="group"
									aria-label="Choose a date"
									aria-describedby={errors.date ? 'date-error' : undefined}
									aria-busy={daysLoading}
								>
									{#each days as d (d.date)}
										<button
											type="button"
											class="day"
											disabled={d.status !== 'open'}
											aria-pressed={d.date === date}
											aria-label={`${dateLabel(d.date)}, ${DAY_STATUS[d.status] ?? d.status}`}
											onclick={() => chooseDate(d.date)}
										>
											<span>{shortDateLabel(d.date)}</span>
											<small>{DAY_STATUS[d.status] ?? d.status}</small>
										</button>
									{/each}
								</div>
								{#if !daysLoading && days.length > 0 && !days.some((d) => d.status === 'open')}
									<p class="small quiet">No dates are available here. Try later dates.</p>
								{/if}
							</div>
						{/if}
						{#if errors.date}<p id="date-error" class="field-error">{errors.date}</p>{/if}

						<fieldset class="modes">
							<legend class="sr-only">How you will get your cake</legend>
							<label class="mode-card">
								<input
									type="radio"
									name="fulfilment"
									value="collection"
									checked={mode === 'collection'}
									onchange={() => chooseMode('collection')}
								/>
								<span>
									<strong>Collect · Free</strong>
									<small>St Katharine Docks, London.</small>
								</span>
							</label>
							<label class="mode-card">
								<input
									type="radio"
									name="fulfilment"
									value="customer_courier"
									checked={mode === 'customer_courier'}
									onchange={() => chooseMode('customer_courier')}
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
								<input type="radio" name="fulfilment" value="gk_delivery" disabled />
								<span>
									<strong>GK delivery · from £100</strong>
									<small>Not available online yet. Please contact us to arrange delivery.</small>
								</span>
							</label>
						</fieldset>

						<label class="field" for="slot">Collection time · London time</label>
						<select
							id="slot"
							bind:value={slotId}
							disabled={!date || slotsLoading}
							aria-busy={slotsLoading}
							aria-invalid={errors.slot ? 'true' : undefined}
							aria-describedby={describedBy('slot', 'slot-status')}
							onchange={() => {
								clearError('slot');
								slotStatus = '';
							}}
						>
							<option value="">
								{!date
									? 'Choose a date first'
									: slotsLoading
										? 'Loading available times…'
										: 'Choose a time'}
							</option>
							{#each slots as s (s.id)}
								<option value={s.id}>{slotLabel(s.starts_at, s.ends_at)}</option>
							{/each}
						</select>
						<p id="slot-status" class="small quiet status" role="status">{slotStatus}</p>
						{#if errors.slot}<p id="slot-error" class="field-error">{errors.slot}</p>{/if}
					</section>

					<!-- 2. Your details -->
					<section class="card" aria-labelledby="h-details">
						<h2 class="h" id="h-details">
							<span class="num" aria-hidden="true">2</span>Your details
						</h2>
						<div class="two">
							<div>
								<label class="field" for="name">Full name</label>
								<input
									id="name"
									autocomplete="name"
									maxlength="80"
									bind:value={fullName}
									aria-invalid={errors.name ? 'true' : undefined}
									aria-describedby={describedBy('name')}
									oninput={() => clearError('name')}
								/>
								{#if errors.name}<p id="name-error" class="field-error">{errors.name}</p>{/if}
							</div>
							<div>
								<label class="field" for="email">Email for order updates</label>
								<input
									id="email"
									type="email"
									autocomplete="email"
									maxlength="120"
									bind:value={email}
									aria-invalid={errors.email ? 'true' : undefined}
									aria-describedby={describedBy('email')}
									oninput={() => clearError('email')}
								/>
								{#if errors.email}<p id="email-error" class="field-error">{errors.email}</p>{/if}
							</div>
						</div>
						<label class="check-line payer-toggle">
							<input
								type="checkbox"
								bind:checked={otherPayer}
								onchange={() => {
									if (!otherPayer) {
										payerName = '';
										clearError('payer');
									}
								}}
							/>
							<span>Someone else is paying <span class="quiet">(we record who paid)</span></span>
						</label>
						{#if otherPayer}
							<label class="field" for="payer">Payer’s full name</label>
							<input
								id="payer"
								autocomplete="off"
								maxlength="80"
								bind:value={payerName}
								aria-invalid={errors.payer ? 'true' : undefined}
								aria-describedby={describedBy('payer')}
								oninput={() => clearError('payer')}
							/>
							{#if errors.payer}<p id="payer-error" class="field-error">{errors.payer}</p>{/if}
						{/if}
					</section>

					<!-- 3. Discount code -->
					<section class="card" aria-labelledby="h-code">
						<h2 class="h" id="h-code">
							<span class="num" aria-hidden="true">3</span>Discount code
							<span class="optional">optional</span>
						</h2>
						{#if appliedCode}
							<div class="code-chip">
								<span id="code-chip-text" tabindex="-1">
									<strong>{currentQuote?.discount?.code ?? appliedCode.toUpperCase()}</strong>
									{#if currentQuote?.discount?.basis_points}
										· {currentQuote.discount.basis_points / 100}% off cakes
									{/if}
								</span>
								<button
									type="button"
									class="text-link"
									aria-label={`Remove code ${appliedCode}`}
									onclick={removeCode}>Remove</button
								>
							</div>
						{:else}
							<div class="code-row">
								<label class="sr-only" for="code-input">Discount code</label>
								<input
									id="code-input"
									autocomplete="off"
									maxlength="40"
									placeholder="Enter a code"
									bind:value={codeInput}
									aria-invalid={codeStatus.kind === 'error' ? 'true' : undefined}
									aria-describedby="code-status"
									onkeydown={(e) => {
										if (e.key === 'Enter') {
											e.preventDefault();
											void applyCode();
										}
									}}
								/>
								<button type="button" onclick={applyCode} disabled={quoteLoading}>Apply</button>
							</div>
						{/if}
						<p id="code-status" class="small code-status {codeStatus.kind}" role="status">
							{codeStatus.text}
						</p>
					</section>

					<!-- 4. Before you pay -->
					<section class="card" aria-labelledby="h-checks">
						<h2 class="h" id="h-checks">
							<span class="num" aria-hidden="true">4</span>Before you pay
						</h2>
						{#each items as item (item.key)}
							<div class="inset">
								<p class="label">{item.name} · allergens</p>
								{#each item.allergens.filter((a) => a.prominent) as a (a.code)}
									<p class="allergen-alert">Contains {a.label}</p>
								{/each}
								{#if item.allergens.length > 0}
									<p>
										<strong>Contains: {item.allergens.map((a) => a.label).join(', ')}.</strong>
									</p>
								{/if}
								{#if item.composition}<p class="small">{item.composition}</p>{/if}
								{#if item.problem}<p class="field-error" role="alert">{item.problem}</p>{/if}
							</div>
						{/each}
						{#each notices as notice (notice)}
							<p class="small">{notice}</p>
						{/each}
						<div class="inset policy" id="refund-terms">
							<p class="label">Cancellation and refunds</p>
							{#if slot}
								<p>
									Cancel before <strong>{deadline}</strong> for a full refund of
									<strong
										>{currentQuote ? formatMoney(currentQuote.total_minor) : 'what you pay'}</strong
									>.
								</p>
							{:else}
								<p>Choose a time to see your exact cancellation deadline.</p>
							{/if}
							{#each (policy?.summary ?? '').split(/\n\s*\n/).filter(Boolean) as para, i (i)}
								<p>{para}</p>
							{/each}
						</div>
						<p class="small quiet statutory">{STATUTORY}</p>
						<label class="check-line confirm">
							<input
								type="checkbox"
								id="reviewed"
								bind:checked={reviewed}
								aria-invalid={errors.reviewed ? 'true' : undefined}
								aria-describedby={describedBy('reviewed')}
								onchange={() => clearError('reviewed')}
							/>
							<span>I’ve checked the cake, allergens, date and the refund terms above.</span>
						</label>
						{#if errors.reviewed}<p id="reviewed-error" class="field-error">
								{errors.reviewed}
							</p>{/if}
					</section>

					<!-- 5. Payment -->
					<section class="card" aria-labelledby="h-pay">
						<h2 class="h" id="h-pay"><span class="num" aria-hidden="true">5</span>Payment</h2>
						<p class="lead">
							You’ll pay on our secure payment page, which shows the options available to you.
						</p>
						<p class="methods-label">Ways to pay include:</p>
						<p class="pay-list">{METHODS.slice(0, 6).join(' · ')}</p>
						<details class="pay-all">
							<summary>Show all {METHODS.length} methods</summary>
							<p class="pay-list">{METHODS.join(' · ')}</p>
						</details>
						<p class="preview-note small">
							The method list is still being confirmed with our payment provider.
						</p>
						<p class="small quiet">
							Prices and refunds are in pounds (GBP). Some options charge in another currency; the
							payment page shows the converted amount and any fees before you pay.
						</p>
					</section>
				</form>

				<aside class="summary" aria-labelledby="sum-title">
					<h2 id="sum-title">Your order</h2>
					{#each items as item (item.key)}
						{@const line = currentQuote?.lines.find(
							(l) =>
								(item.line.lot_id && l.lot_id === item.line.lot_id) ||
								(!item.line.lot_id && !l.lot_id && l.variant_id === item.line.variant_id)
						)}
						<div class="item">
							<div>
								<strong>{item.name}</strong>
								<span>
									{[item.flavour, item.size, item.servings].filter(Boolean).join(' · ')}{item.line
										.quantity > 1
										? ` · ${item.line.quantity} cakes`
										: ''}
								</span>
							</div>
							<span class="price">
								{formatMoney(line?.unit_price_minor ?? item.unitPriceMinor)}{item.line.quantity > 1
									? ' each'
									: ''}
							</span>
						</div>
					{/each}
					<dl class="meta">
						<div>
							<dt>Date</dt>
							<dd>{date ? dateLabel(date) : 'Choose a date'}</dd>
						</div>
						<div>
							<dt>Time</dt>
							<dd>
								{slot
									? `${slotLabel(slot.starts_at, slot.ends_at)} · London time`
									: 'Choose a time'}
							</dd>
						</div>
						<div>
							<dt>How you’ll get it</dt>
							<dd>{MODE_SUMMARY[mode]}</dd>
						</div>
					</dl>
					{#if currentQuote}
						<div class="totals">
							<span>Cakes</span><span>{formatMoney(currentQuote.subtotal_minor)}</span>
						</div>
						{#if currentQuote.discount_minor > 0}
							<div class="totals">
								<span>
									Discount{currentQuote.discount
										? ` · ${currentQuote.discount.code}`
										: ''}{currentQuote.discount?.basis_points
										? ` (${currentQuote.discount.basis_points / 100}%)`
										: ''}
								</span>
								<span>{formatMoney(-currentQuote.discount_minor)}</span>
							</div>
						{/if}
						<div class="totals">
							<span>{mode === 'collection' ? 'Collection' : 'Your courier'}</span>
							<span>{mode === 'collection' ? 'Free' : 'Arranged by you'}</span>
						</div>
					{/if}
					<div class="totals total">
						<span>To pay now</span>
						<span>{currentQuote ? formatMoney(currentQuote.total_minor) : '—'}</span>
					</div>
					<p class="small quiet" role="status">
						{#if quoteLoading}
							Updating your total…
						{:else if quoteProblem}
							{quoteProblem}
						{:else if !currentQuote}
							Choose a date and time to see your total.
						{:else}
							In pounds (GBP). Decoration shown is included.
						{/if}
					</p>
					{#if quoteProblem && inputs}
						<button
							type="button"
							class="text-link"
							onclick={() => inputs && refreshQuote(inputs).catch(() => {})}>Try again</button
						>
					{/if}
					<ul class="assure">
						<li>
							Your place on this date is held for 15 minutes while you pay. If your payment is still
							processing after that, we’ll wait for the result.
						</li>
						<li>Your order is confirmed only when we receive the payment. We’ll email you.</li>
						<li>If a payment result is unclear, we check it for you. Please don’t pay twice.</li>
					</ul>
					{#if payProblem}
						<p id="pay-problem" class="pay-problem" role="alert" tabindex="-1">{payProblem}</p>
					{/if}
					<button
						type="submit"
						form="checkout-form"
						class="primary pay"
						class:is-busy={submitting}
						aria-disabled={submitting || quoteLoading || blocked}
						disabled={blocked}
					>
						<span>{submitting ? 'Opening secure payment…' : payLabel}</span>
						{#if submitting}
							<span class="spinner" aria-hidden="true"></span>
						{:else}
							<span class="arrow" aria-hidden="true">→</span>
						{/if}
					</button>
					<p class="sr-only" role="status">{payStatus}</p>
				</aside>
			</div>
		{/if}
	</main>
</div>

<style>
	.checkout :global(.page-title) {
		margin-bottom: 22px;
	}
	.steps {
		display: flex;
		flex-wrap: nowrap;
		gap: 8px;
		overflow-x: auto;
		margin: 0 0 26px;
		padding: 0 0 2px;
		list-style: none;
		font: 500 11px/1 var(--mono);
		letter-spacing: 0.08em;
	}
	.steps li {
		flex-shrink: 0;
		white-space: nowrap;
		padding: 10px 14px;
		border-radius: 999px;
		background: #ffffff80;
		box-shadow: inset 0 0 0 1px #ffffffe6;
		color: var(--muted);
	}
	.steps .active {
		background: var(--ink);
		color: #fff;
	}
	.step-short,
	.mobile-total {
		display: none;
	}
	.layout {
		display: grid;
		grid-template-columns: minmax(0, 1.4fr) minmax(320px, 0.9fr);
		gap: 28px;
		align-items: start;
	}
	.h {
		display: flex;
		align-items: center;
		gap: 12px;
		margin: 0 0 20px;
		font: 500 22px/1.2 var(--sans);
		letter-spacing: -0.02em;
	}
	.num {
		display: grid;
		place-items: center;
		flex-shrink: 0;
		width: 28px;
		height: 28px;
		border-radius: 50%;
		background: var(--ink);
		color: #fff;
		font: 500 12px/1 var(--mono);
	}
	.optional {
		margin-left: auto;
		font: 500 11px/1 var(--mono);
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.date-row {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		align-items: center;
		gap: 12px;
		margin: 0 0 16px;
		padding: 10px 18px;
		border-radius: 18px;
		background: #fff;
		box-shadow: inset 0 0 0 1px #14141614;
		font-size: 14px;
	}
	.picker {
		margin: 0 0 18px;
	}
	.picker-head {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		align-items: center;
		gap: 8px;
	}
	.picker-head p {
		margin: 0;
	}
	.days {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(92px, 1fr));
		gap: 8px;
		margin: 8px 0 0;
	}
	.day {
		display: grid;
		gap: 2px;
		min-height: 56px;
		padding: 8px;
		border: 1px solid var(--line);
		border-radius: 14px;
		background: #fff;
		font: 500 13px/1.2 var(--sans);
		text-align: center;
	}
	.day small {
		font: 400 11.5px/1.2 var(--sans);
		color: var(--muted);
	}
	.day:disabled {
		border-color: #14141621;
		background: #ffffff80;
		color: #5a5d62;
		opacity: 1;
		text-decoration: line-through;
	}
	.day:disabled small {
		text-decoration: none;
	}
	.day[aria-pressed='true'] {
		border: 2px solid var(--ink);
		background: var(--ink);
		color: #fff;
	}
	.day[aria-pressed='true'] small {
		color: #fff;
	}
	.modes {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: 10px;
		margin: 0 0 18px;
		padding: 0;
		border: 0;
		min-width: 0;
	}
	.mode-card {
		position: relative;
		cursor: pointer;
		min-width: 0;
	}
	.mode-card input {
		position: absolute;
		opacity: 0;
		width: 1px;
		height: 1px;
		margin: 0;
	}
	.mode-card span {
		display: grid;
		align-content: start;
		gap: 6px;
		height: 100%;
		padding: 16px 40px 16px 16px;
		border: 1px solid var(--line);
		border-radius: 18px;
		background: #fff;
	}
	.mode-card strong {
		font: 600 14px/1.3 var(--sans);
	}
	.mode-card small {
		font: 400 12.5px/1.45 var(--sans);
		color: var(--muted);
	}
	.mode-card:hover span {
		border-color: var(--ink);
	}
	.mode-card input:checked + span {
		border: 2px solid var(--ink);
		padding: 15px 39px 15px 15px;
	}
	.mode-card span::after {
		content: '';
		position: absolute;
		top: 16px;
		right: 16px;
		width: 18px;
		height: 18px;
		border-radius: 50%;
		border: 1.5px solid var(--line);
	}
	.mode-card input:checked + span::after {
		border: 5px solid var(--ink);
	}
	.mode-card input:focus-visible + span {
		outline: 3px solid var(--focus);
		outline-offset: 3px;
	}
	.mode-card.unavailable {
		cursor: not-allowed;
	}
	.mode-card.unavailable span {
		border-style: dashed;
		background: #ffffff80;
	}
	@media (forced-colors: active) {
		.mode-card input:checked + span {
			border: 3px solid Highlight;
		}
	}
	#slot {
		width: 100%;
	}
	.status {
		margin: 8px 0 0;
		min-height: 1.4em;
	}
	.two {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 16px;
	}
	.two > div {
		margin: 0 0 14px;
	}
	.payer-toggle {
		margin: 4px 0 14px;
	}
	.code-row {
		display: flex;
		gap: 10px;
		align-items: stretch;
	}
	.code-row button {
		flex-shrink: 0;
	}
	.code-chip {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 6px 10px 6px 18px;
		border-radius: 18px;
		background: #fff;
		border: 1.5px solid var(--ok);
		font-size: 14px;
	}
	.code-chip .text-link {
		padding: 0 14px;
	}
	#code-chip-text:focus {
		outline: 3px solid var(--focus);
		outline-offset: 3px;
		border-radius: 6px;
	}
	.code-status {
		margin: 12px 0 0;
		color: var(--muted);
	}
	.code-status.success {
		color: var(--ok);
	}
	.code-status.error {
		color: var(--error);
	}
	.allergen-alert {
		display: inline-block;
		margin: 2px 0 10px;
		padding: 6px 12px;
		border-radius: 10px;
		background: var(--ink);
		color: #fff;
		font: 700 13px/1.2 var(--sans);
		letter-spacing: 0.06em;
		text-transform: uppercase;
	}
	.policy {
		margin-top: 16px;
		white-space: pre-line;
	}
	.statutory {
		margin: 10px 0 0;
	}
	.confirm {
		margin: 18px 0 0;
	}
	.lead {
		margin: 0;
		font-size: 14.5px;
	}
	.methods-label {
		margin: 12px 0 4px;
	}
	.pay-list {
		margin: 0 0 10px;
		font-size: 14.5px;
		line-height: 1.7;
	}
	.pay-all {
		margin: 0 0 12px;
	}
	.pay-all summary {
		display: inline-flex;
		align-items: center;
		min-height: 44px;
		cursor: pointer;
		font-weight: 500;
		text-decoration: underline;
		text-underline-offset: 4px;
	}
	.preview-note {
		margin: 0 0 10px;
		padding: 8px 12px;
		border: 1px dashed var(--line);
		border-radius: 12px;
		color: var(--muted);
	}
	.errors {
		margin: 0 0 16px;
		padding: 16px 20px;
		border-radius: 22px;
		background: #fff;
		border: 2px solid var(--error);
		font-size: 14px;
	}
	.errors p {
		margin: 0 0 8px;
		color: var(--error);
	}
	.errors ul {
		margin: 0;
		padding-left: 18px;
	}
	.errors a {
		color: var(--error);
		font-weight: 500;
	}
	.summary {
		position: sticky;
		top: 24px;
		max-height: calc(100vh - 48px);
		overflow-y: auto;
		padding: 26px 26px 22px;
		border-radius: var(--r-xl);
		background: #ffffffd6;
		box-shadow: var(--glass-ring), var(--sh-2);
	}
	.summary h2 {
		margin: 0 0 14px;
		font: 500 22px/1.2 var(--sans);
		letter-spacing: -0.02em;
	}
	.pay {
		position: sticky;
		bottom: 0;
		width: 100%;
		justify-content: space-between;
		text-align: left;
		box-shadow:
			0 -18px 22px 2px #fff,
			0 16px 0 10px #fff;
	}
	.pay.is-busy {
		cursor: progress;
	}
	.pay-problem {
		margin: 0 0 12px;
		padding: 12px 14px;
		border: 2px solid var(--error);
		border-radius: 14px;
		background: #fff;
		color: var(--error);
		font-size: 14px;
		font-weight: 500;
	}
	@media (max-width: 1050px) {
		.layout {
			grid-template-columns: 1fr;
		}
		.summary {
			position: static;
			max-height: none;
			overflow: visible;
		}
		.pay {
			position: static;
			box-shadow: none;
		}
		.mobile-total {
			display: flex;
			align-items: baseline;
			gap: 10px;
			margin: 0 0 16px;
			padding: 14px 20px;
			border-radius: 999px;
			background: #ffffffd6;
			box-shadow: var(--glass-ring), var(--sh-1);
			color: inherit;
			text-decoration: none;
			font-size: 14px;
		}
		.mobile-total strong {
			font: 600 18px/1 var(--sans);
		}
		.mobile-link {
			margin-left: auto;
			font-size: 12.5px;
			color: var(--muted);
		}
	}
	@media (max-width: 760px) {
		.steps {
			display: none;
		}
		.step-short {
			display: block;
			margin: 0 0 16px;
			font: 500 12px/1.4 var(--mono);
			letter-spacing: 0.06em;
			text-transform: uppercase;
			color: var(--muted);
		}
		.modes,
		.two {
			grid-template-columns: 1fr;
		}
		.h {
			font-size: 20px;
		}
		.summary {
			padding: 22px 18px;
			border-radius: 28px;
		}
	}
	@media (max-width: 420px) {
		.code-row {
			flex-direction: column;
		}
		.code-row button {
			width: 100%;
		}
		.days {
			grid-template-columns: repeat(auto-fill, minmax(84px, 1fr));
		}
	}
</style>
