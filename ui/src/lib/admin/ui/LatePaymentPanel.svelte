<script lang="ts">
	// Paid late · your decision (r03b2 GK-1039 card) with the Q6 offer flow
	// as the main path: offer the customer's requested time, or another date,
	// which they accept or decline on their order page (GK-017 r02b state 10).
	// The r03b2 paid-late form stays as the second path (the chef's
	// /late-payment route): make it with the customer's agreement recorded
	// elsewhere, or return everything.
	import { tick } from 'svelte';
	import { formatMoney, timeRange } from '../../checkout/format';
	import type { StaffApi } from '../catalogue';
	import {
		agreementMissing,
		latePaymentChoices,
		latePaymentStatus,
		londonLocalToIso,
		type AgreementInput,
		type Decision,
		type DecisionInput
	} from '../order-decisions';
	import { dayShort, type StaffOrderDetail } from '../order-detail';
	import type { StaffView } from '../orders';
	import type { SlotOption } from '../order-decisions';
	import AgreementFields from './AgreementFields.svelte';
	import DecisionStatus from './DecisionStatus.svelte';
	import SlotPicker from './SlotPicker.svelte';

	let {
		api,
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
		api: StaffApi;
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

	let open = $state<'other' | 'decide' | null>(null);
	let date = $state('');
	let choice = $state<SlotOption | null>(null);
	let decision = $state<'' | 'honour' | 'full_return'>('');
	let feasible = $state(false);
	let agreement = $state<AgreementInput>({ channel: '', when: '', evidence: '', exact: false });

	const choices = $derived(latePaymentChoices(order, view, now));
	const lines = $derived(latePaymentStatus(order, view, now));
	const requested = $derived(
		`${dayShort(order.date)}, ${timeRange(order.slot.starts_at, order.slot.ends_at)}`
	);
	const paid = $derived(
		order.late_payment?.paid_minor != null
			? formatMoney(order.late_payment.paid_minor, order.late_payment.currency ?? 'GBP')
			: ''
	);
	const missing = $derived([
		...(feasible ? [] : ['a tick that you can make it']),
		...agreementMissing(
			agreement,
			now,
			order.paid_at ? { at: order.paid_at, label: 'the payment' } : null,
			requested
		)
	]);

	const whenWrong = $derived(
		missing.some((m) => m === 'a time that isn’t in the future' || m.startsWith('a time after'))
	);
	const evidenceWrong = $derived(missing.some((m) => m.startsWith('evidence on one line')));

	function toggle(which: 'other' | 'decide') {
		open = open === which ? null : which;
		if (open === 'other' && !date) date = order.date;
		if (open) {
			void tick().then(() =>
				document.getElementById(which === 'other' ? 'offer-date' : 'late-decide-legend')?.focus()
			);
		}
	}

	async function offerOther() {
		if (!choice) return;
		const ok = await decide({ d: 'offer', kind: 'other', date, slotId: choice.id });
		if (ok) {
			open = null;
			choice = null;
		}
	}

	async function honour() {
		const at = londonLocalToIso(agreement.when);
		if (missing.length || !at) return;
		const ok = await decide({
			d: 'honour',
			channel: agreement.channel,
			agreedAt: at,
			evidence: agreement.evidence
		});
		if (ok) open = null;
	}

	async function fullReturn() {
		const ok = await decide({ d: 'full_return' });
		if (ok) open = null;
	}

	async function close(which: 'other' | 'decide') {
		open = null;
		await tick();
		document.getElementById(`late-${which}`)?.focus();
	}

	function onKey(e: KeyboardEvent, which: 'other' | 'decide') {
		if (e.key === 'Escape') {
			e.preventDefault();
			void close(which);
		}
	}
</script>

{#each lines as line, i (i)}<p class={i ? 'small' : ''}>{line}</p>{/each}
{#if view === 'chef'}
	{#if choices.requested || choices.other || choices.honour || choices.fullReturn}
		<div class="actions">
			{#if choices.requested}
				<button
					class="btn btn-primary"
					type="button"
					disabled={busy}
					onclick={() => void decide({ d: 'offer', kind: 'requested' })}
					>{deciding === 'offer' && !open
						? 'Sending…'
						: `Offer their requested time · ${requested}`}</button
				>
			{/if}
			{#if choices.other}
				<button
					id="late-other"
					class="btn {choices.requested ? '' : 'btn-primary'}"
					type="button"
					aria-expanded={open === 'other'}
					aria-controls="late-other-panel"
					disabled={busy}
					onclick={() => toggle('other')}
					>{order.late_payment?.state === 'offered'
						? 'Offer another date instead…'
						: 'Offer another date…'}</button
				>
			{/if}
		</div>
		{#if open === 'other'}
			<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
			<form
				id="late-other-panel"
				class="decide-box"
				aria-label="Offer another date"
				novalidate
				onkeydown={(e) => onKey(e, 'other')}
				onsubmit={(e) => {
					e.preventDefault();
					void offerOther();
				}}
			>
				<p class="small">
					An offer holds nothing and changes nothing until the customer accepts it on their order
					page. It must fit the day’s standard places (it can’t use an extra place) and the cakes’
					shelf life; the system checks. A new offer replaces the open one.
				</p>
				<SlotPicker
					{api}
					{order}
					{now}
					purpose="offer"
					idPrefix="offer"
					bind:date
					bind:choice
					disabled={busy}
					dateLabel="Date to offer"
				/>
				<div class="actions">
					<button class="btn btn-primary" type="submit" disabled={busy || !choice}
						>{deciding === 'offer'
							? 'Sending…'
							: choice
								? `Send offer · ${dayShort(date)}, ${choice.label}`
								: 'Choose a time to offer'}</button
					>
					<button class="btn btn-quiet" type="button" onclick={() => void close('other')}
						>Not now</button
					>
				</div>
			</form>
		{/if}
		{#if choices.honour || choices.fullReturn}
			<div class="actions">
				<button
					id="late-decide"
					class="btn btn-quiet"
					type="button"
					aria-expanded={open === 'decide'}
					aria-controls="late-decide-panel"
					disabled={busy}
					onclick={() => toggle('decide')}
					>Decide without an offer (agreed elsewhere, or return everything)…</button
				>
			</div>
		{/if}
		{#if open === 'decide'}
			<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
			<div
				id="late-decide-panel"
				class="decide-box"
				role="group"
				aria-label="Decide without an offer"
				onkeydown={(e) => onKey(e, 'decide')}
			>
				<fieldset class="choice">
					<legend id="late-decide-legend" tabindex="-1">What happens?</legend>
					{#if choices.honour}
						<label
							><input type="radio" name="late" value="honour" bind:group={decision} /> Make it on {requested}
							(you can, and the customer agreed outside the order page)</label
						>
					{/if}
					<label
						><input type="radio" name="late" value="full_return" bind:group={decision} /> Return the
						full {paid || 'amount paid'} (never the 50% cancellation charge)</label
					>
				</fieldset>
				{#if decision === 'honour' && choices.honour}
					<form
						novalidate
						onsubmit={(e) => {
							e.preventDefault();
							void honour();
						}}
					>
						<p class="small">
							Make it only when you record that you can <em>and</em> the customer explicitly agreed to
							this handover. Silence never counts.
						</p>
						<label class="check">
							<input type="checkbox" bind:checked={feasible} aria-describedby="honour-missing" />
							<span
								>I can make it for {requested} (time, ingredients, workload) · recorded as you</span
							>
						</label>
						<AgreementFields
							idPrefix="honour"
							bind:value={agreement}
							{now}
							exactLabel={requested}
							describedBy="honour-missing"
							invalidWhen={whenWrong}
							invalidEvidence={evidenceWrong}
						/>
						<p id="honour-missing" class="small" aria-live="polite">
							{#if missing.length}Still needed: {missing.join(', ')}.{:else}<span class="ok-text"
									>Ready: your check and the customer’s agreement are saved with the order.</span
								>{/if}
						</p>
						<p class="small muted">
							Before confirming, the system rechecks the original date and time, the day place and
							the cakes. No date is moved automatically. Its day place is already counted.
						</p>
						<button
							class="btn btn-primary"
							type="submit"
							disabled={busy || missing.length > 0}
							aria-describedby="honour-missing"
							>{deciding === 'honour'
								? 'Confirming…'
								: `Confirm the order for ${requested}`}</button
						>
					</form>
				{:else if decision === 'full_return'}
					<p class="small">
						Everything paid{paid ? ` (${paid})` : ''} goes back and the order is cancelled; its day place
						is released. This can’t be undone.
					</p>
					<button
						class="btn btn-danger"
						type="button"
						disabled={busy}
						onclick={() => void fullReturn()}
						>{deciding === 'full_return'
							? 'Returning…'
							: `Return ${paid || 'everything paid'}`}</button
					>
				{/if}
				<button class="btn btn-quiet" type="button" onclick={() => void close('decide')}
					>Not now</button
				>
			</div>
		{/if}
	{/if}
	{#if choices.note}<p class="role-note">{choices.note}</p>{/if}
	<DecisionStatus area="late" {status} {error} {pending} {busy} {oncheck} />
{/if}
