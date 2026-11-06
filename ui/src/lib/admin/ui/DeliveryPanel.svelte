<script lang="ts">
	// The GK delivery card's state and the chef's delivery decisions (in
	// r03b2's visual language; the customer side is GK-017 r02b states 14,
	// 15 and 19): send or replace a quote (from £100, pay by the original
	// delivery time − 48 h), waive it for a free-delivery code with the
	// measured distance, record that GK can't deliver, and record a failed
	// delivery (our side, or nobody available). Chef only; fulfilment and
	// technical support read the state.
	import { tick } from 'svelte';
	import { formatMoney } from '../../checkout/format';
	import {
		canRecordNotPossible,
		choiceStatus,
		deliveryStatus,
		distanceText,
		failureChoices,
		failureConfirm,
		failureStatus,
		notPossibleConfirm,
		optionalLineProblem,
		parseDistance,
		parsePence,
		quoteChoices,
		quoteFormNotes,
		quoteLines,
		quotePriceProblem,
		lineProblem,
		type Decision,
		type DecisionInput
	} from '../order-decisions';
	import { londonTime } from '../../checkout/format';
	import { when, type StaffOrderDetail } from '../order-detail';
	import type { StaffView } from '../orders';
	import DecisionStatus from './DecisionStatus.svelte';

	let {
		order,
		view,
		now,
		busy,
		deciding,
		status,
		error,
		pending,
		decide,
		oncheck
	}: {
		order: StaffOrderDetail;
		view: StaffView;
		now: Date;
		busy: boolean;
		deciding: Decision | null;
		status: string;
		error: string;
		pending: boolean;
		decide: (input: DecisionInput) => Promise<boolean>;
		oncheck: () => void;
	} = $props();

	type Open = 'quote' | 'waiver' | 'not_possible' | 'failed';
	let open = $state<Open | null>(null);
	let price = $state('');
	let note = $state('');
	let priceError = $state('');
	let noteError = $state('');
	let distance = $state('');
	let unit = $state<'mi' | 'km'>('mi');
	let source = $state('');
	let distanceError = $state('');
	let sourceError = $state('');
	let npReason = $state('');
	let npError = $state('');
	let cause = $state<'' | 'our_side' | 'nobody_available'>('');
	let failReason = $state('');
	let causeError = $state('');
	let failError = $state('');

	const chef = $derived(view === 'chef');
	const lines = $derived([
		...deliveryStatus(order, view),
		...choiceStatus(order, view),
		...failureStatus(order, view)
	]);
	const q = $derived(quoteChoices(order, view, now));
	const notPossible = $derived(canRecordNotPossible(order, view));
	const failure = $derived(failureChoices(order, view, now));
	const history = $derived(quoteLines(order, view));
	const parsed = $derived(parsePence(price));
	const priceLabel = $derived(
		'minor' in parsed && !quotePriceProblem(price) ? ` · ${formatMoney(parsed.minor)}` : ''
	);
	const metres = $derived.by(() => {
		const r = parseDistance(distance, unit);
		return 'metres' in r ? r.metres : null;
	});
	const formNotes = $derived(quoteFormNotes(order, q));

	async function toggle(which: Open, focusId: string) {
		open = open === which ? null : which;
		if (open) {
			await tick();
			document.getElementById(focusId)?.focus();
		}
	}

	async function close(which: Open) {
		open = null;
		await tick();
		document.getElementById(`del-${which}`)?.focus();
	}

	function onKey(e: KeyboardEvent, which: Open) {
		if (e.key === 'Escape') {
			e.preventDefault();
			void close(which);
		}
	}

	async function sendQuote() {
		priceError = quotePriceProblem(price);
		noteError = optionalLineProblem(note, 500);
		if (priceError || noteError || !('minor' in parsed)) {
			await tick();
			document.getElementById(priceError ? 'quote-price' : 'quote-note')?.focus();
			return;
		}
		if (await decide({ d: 'quote', amountMinor: parsed.minor, note })) {
			open = null;
			price = '';
			note = '';
		}
	}

	async function sendWaiver() {
		const d = parseDistance(distance, unit);
		distanceError = 'error' in d ? d.error : '';
		sourceError = lineProblem(source, 200, 'how you measured it');
		if (distanceError || sourceError || !('metres' in d)) {
			await tick();
			document.getElementById(distanceError ? 'waiver-distance' : 'waiver-source')?.focus();
			return;
		}
		if (await decide({ d: 'waiver', distanceM: d.metres, source })) {
			open = null;
			distance = '';
			source = '';
		}
	}

	async function sendNotPossible() {
		npError = optionalLineProblem(npReason, 500);
		if (npError) {
			await tick();
			document.getElementById('np-reason')?.focus();
			return;
		}
		if (await decide({ d: 'not_possible', reason: npReason })) {
			open = null;
			npReason = '';
		}
	}

	async function sendFailure() {
		causeError = cause ? '' : 'Choose what happened.';
		failError = optionalLineProblem(failReason, 500);
		if (causeError || failError || !cause) {
			await tick();
			document.getElementById(causeError ? 'fail-legend' : 'fail-reason')?.focus();
			return;
		}
		const ok = await decide({
			d: cause === 'our_side' ? 'failed_our_side' : 'failed_nobody',
			reason: failReason
		});
		if (ok) {
			open = null;
			failReason = '';
			cause = '';
		}
	}
</script>

{#each lines as line, i (i)}<p class={i ? 'small' : ''}>{line}</p>{/each}
{#if chef}
	{#if q.quote || q.replace || q.waive}
		<div class="actions">
			{#if q.quote || q.replace}
				<button
					id="del-quote"
					class="btn {q.quote ? 'btn-primary' : ''}"
					type="button"
					aria-expanded={open === 'quote'}
					aria-controls="del-quote-panel"
					disabled={busy}
					onclick={() => void toggle('quote', 'quote-price')}
					>{q.quote ? 'Send a delivery price…' : 'Replace the quote…'}</button
				>
			{/if}
			{#if q.waive}
				<button
					id="del-waiver"
					class="btn"
					type="button"
					aria-expanded={open === 'waiver'}
					aria-controls="del-waiver-panel"
					disabled={busy}
					onclick={() => void toggle('waiver', 'waiver-distance')}
					>Waive delivery (free-delivery code)…</button
				>
			{/if}
		</div>
	{/if}
	{#if open === 'quote'}
		<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
		<form
			id="del-quote-panel"
			class="decide-box"
			aria-label={q.quote ? 'Send a delivery price' : 'Replace the quote'}
			novalidate
			onkeydown={(e) => onKey(e, 'quote')}
			onsubmit={(e) => {
				e.preventDefault();
				void sendQuote();
			}}
		>
			<div class="form-grid">
				<label class="field" for="quote-price"
					>Delivery price (from £100)
					<input
						id="quote-price"
						inputmode="decimal"
						autocomplete="off"
						placeholder="e.g. 120 or 120.50"
						bind:value={price}
						oninput={() => (priceError = '')}
						aria-invalid={priceError ? 'true' : undefined}
						aria-describedby="quote-help{priceError ? ' quote-price-error' : ''}"
					/>
				</label>
				<label class="field" for="quote-note"
					>Note for your records (optional, staff only)
					<input
						id="quote-note"
						autocomplete="off"
						maxlength="500"
						bind:value={note}
						oninput={() => (noteError = '')}
						aria-invalid={noteError ? 'true' : undefined}
						aria-describedby={noteError ? 'quote-note-error' : undefined}
					/>
				</label>
			</div>
			{#if priceError}<p id="quote-price-error" class="small bad-text">{priceError}</p>{/if}
			{#if noteError}<p id="quote-note-error" class="small bad-text">{noteError}</p>{/if}
			<div id="quote-help" class="small muted">
				{#each formNotes as line, i (i)}<p>{line}</p>{/each}
			</div>
			<div class="actions">
				<button class="btn btn-primary" type="submit" disabled={busy}
					>{deciding === 'quote' ? 'Sending…' : `Send quote${priceLabel}`}</button
				>
				<button class="btn btn-quiet" type="button" onclick={() => void close('quote')}
					>Not now</button
				>
			</div>
		</form>
	{/if}
	{#if open === 'waiver'}
		<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
		<form
			id="del-waiver-panel"
			class="decide-box"
			aria-label="Waive delivery"
			novalidate
			onkeydown={(e) => onKey(e, 'waiver')}
			onsubmit={(e) => {
				e.preventDefault();
				void sendWaiver();
			}}
		>
			<p class="small">
				Only for an order placed with a free-delivery code. This page can’t see the code: the system
				checks it and refuses otherwise. You record the measured driving distance and how you
				measured it, and approve that it’s eligible; there’s no automatic distance limit. Nothing is
				paid for delivery afterwards.
			</p>
			<div class="form-grid">
				<div class="distance-row">
					<label class="field" for="waiver-distance"
						>Measured driving distance
						<input
							id="waiver-distance"
							inputmode="decimal"
							autocomplete="off"
							placeholder="e.g. 5.1"
							bind:value={distance}
							oninput={() => (distanceError = '')}
							aria-invalid={distanceError ? 'true' : undefined}
							aria-describedby="waiver-metres{distanceError ? ' waiver-distance-error' : ''}"
						/>
					</label>
					<label class="field" for="waiver-unit"
						>Unit
						<select id="waiver-unit" bind:value={unit}>
							<option value="mi">miles</option>
							<option value="km">km</option>
						</select>
					</label>
				</div>
				<label class="field" for="waiver-source"
					>How you measured it
					<input
						id="waiver-source"
						autocomplete="off"
						maxlength="200"
						placeholder="e.g. Google Maps driving route"
						bind:value={source}
						oninput={() => (sourceError = '')}
						aria-invalid={sourceError ? 'true' : undefined}
						aria-describedby={sourceError ? 'waiver-source-error' : undefined}
					/>
				</label>
			</div>
			<p id="waiver-metres" class="small muted">
				{metres !== null ? `Recorded as ${distanceText(metres)}.` : 'Recorded in metres.'}
			</p>
			{#if distanceError}<p id="waiver-distance-error" class="small bad-text">
					{distanceError}
				</p>{/if}
			{#if sourceError}<p id="waiver-source-error" class="small bad-text">{sourceError}</p>{/if}
			<div class="actions">
				<button class="btn btn-primary" type="submit" disabled={busy}
					>{deciding === 'waiver'
						? 'Recording…'
						: `Waive delivery${metres !== null ? ` · ${distanceText(metres)}` : ''}`}</button
				>
				<button class="btn btn-quiet" type="button" onclick={() => void close('waiver')}
					>Not now</button
				>
			</div>
		</form>
	{/if}
	{#if q.note}<p class="role-note">{q.note}</p>{/if}

	{#if failure}
		<div class="actions">
			<button
				id="del-failed"
				class="btn"
				type="button"
				aria-expanded={open === 'failed'}
				aria-controls="del-failed-panel"
				disabled={busy}
				onclick={() => void toggle('failed', 'fail-legend')}>Delivery failed…</button
			>
		</div>
		{#if open === 'failed'}
			<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
			<form
				id="del-failed-panel"
				class="decide-box decide-danger"
				aria-label="Record a failed delivery"
				novalidate
				onkeydown={(e) => onKey(e, 'failed')}
				onsubmit={(e) => {
					e.preventDefault();
					void sendFailure();
				}}
			>
				<fieldset class="choice" aria-describedby={causeError ? 'fail-cause-error' : undefined}>
					<legend id="fail-legend" tabindex="-1">What happened?</legend>
					<label
						><input
							type="radio"
							name="cause"
							value="our_side"
							bind:group={cause}
							onchange={() => (causeError = '')}
						/> It failed on our side (for example a breakdown, or we couldn’t reach the address)</label
					>
					<label class:is-off={!failure.nobody}
						><input
							type="radio"
							name="cause"
							value="nobody_available"
							bind:group={cause}
							onchange={() => (causeError = '')}
							disabled={!failure.nobody}
						/>
						Nobody was available to take it{failure.nobodyFrom
							? ` (possible from ${londonTime(failure.nobodyFrom)}, when the delivery time starts)`
							: ''}</label
					>
				</fieldset>
				<div role="alert">
					{#if causeError}<p id="fail-cause-error" class="small bad-text">{causeError}</p>{/if}
				</div>
				{#if cause}<p class="small"><strong>{failureConfirm(order, cause)}</strong></p>{/if}
				<label class="field" for="fail-reason"
					>What happened, in a line (optional; only you see it)
					<input
						id="fail-reason"
						class="text-input"
						autocomplete="off"
						maxlength="500"
						bind:value={failReason}
						oninput={() => (failError = '')}
						aria-invalid={failError ? 'true' : undefined}
						aria-describedby={failError ? 'fail-reason-error' : undefined}
					/>
				</label>
				{#if failError}<p id="fail-reason-error" class="small bad-text">{failError}</p>{/if}
				<div class="actions">
					<button
						class="btn {cause === 'nobody_available' ? 'btn-danger' : 'btn-primary'}"
						type="submit"
						disabled={busy}
						>{deciding === 'failed_our_side' || deciding === 'failed_nobody'
							? 'Recording…'
							: cause === 'nobody_available'
								? 'Record: nobody was available'
								: cause === 'our_side'
									? 'Record: failed on our side'
									: 'Record what happened'}</button
					>
					<button class="btn btn-quiet" type="button" onclick={() => void close('failed')}
						>Not now</button
					>
				</div>
			</form>
		{/if}
	{/if}

	{#if notPossible}
		<div class="actions">
			<button
				id="del-not_possible"
				class="btn btn-quiet"
				type="button"
				aria-expanded={open === 'not_possible'}
				aria-controls="del-np-panel"
				disabled={busy}
				onclick={() => void toggle('not_possible', 'np-reason')}>We can’t deliver this…</button
			>
		</div>
		{#if open === 'not_possible'}
			<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
			<form
				id="del-np-panel"
				class="decide-box decide-danger"
				aria-label="Record that GK can’t deliver"
				novalidate
				onkeydown={(e) => onKey(e, 'not_possible')}
				onsubmit={(e) => {
					e.preventDefault();
					void sendNotPossible();
				}}
			>
				<p class="small"><strong>{notPossibleConfirm(order)}</strong></p>
				<label class="field" for="np-reason"
					>Reason (optional; only you see it)
					<input
						id="np-reason"
						class="text-input"
						autocomplete="off"
						maxlength="500"
						bind:value={npReason}
						oninput={() => (npError = '')}
						aria-invalid={npError ? 'true' : undefined}
						aria-describedby={npError ? 'np-reason-error' : undefined}
					/>
				</label>
				{#if npError}<p id="np-reason-error" class="small bad-text">{npError}</p>{/if}
				<div class="actions">
					<button class="btn btn-danger" type="submit" disabled={busy}
						>{deciding === 'not_possible' ? 'Recording…' : 'Record: GK can’t deliver'}</button
					>
					<button class="btn btn-quiet" type="button" onclick={() => void close('not_possible')}
						>Not now</button
					>
				</div>
			</form>
		{/if}
	{/if}
	<DecisionStatus area="delivery" {status} {error} {pending} {busy} {oncheck} />
{/if}
{#if history.length}
	<h3 class="sub-h">Quotes</h3>
	<ol class="record small">
		{#each history as line, i (i)}
			<li><span class="muted">{when(line.at)}</span> <span>{line.text}</span></li>
		{/each}
	</ol>
{/if}
