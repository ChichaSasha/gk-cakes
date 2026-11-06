<script lang="ts">
	// Move time / Change date… (r03b2 Handover card; r02b "agreed date
	// change"): the chef's appointment change with the customer's recorded
	// agreement and her feasibility note (api/README 0031). After a failed
	// delivery and the customer's redelivery choice it sets the new delivery
	// time (0037). Chef only.
	import { tick } from 'svelte';
	import type { StaffApi } from '../catalogue';
	import {
		agreementMissing,
		appointmentChoice,
		lineProblem,
		londonLocalToIso,
		type AgreementInput,
		type Decision,
		type DecisionInput,
		type SlotOption
	} from '../order-decisions';
	import { dayShort, type StaffOrderDetail } from '../order-detail';
	import type { StaffView } from '../orders';
	import AgreementFields from './AgreementFields.svelte';
	import DecisionStatus from './DecisionStatus.svelte';
	import SlotPicker from './SlotPicker.svelte';

	let {
		api,
		order,
		view,
		busy,
		now,
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

	let open = $state<'time' | 'date' | null>(null);
	let date = $state('');
	let choice = $state<SlotOption | null>(null);
	let feasibility = $state('');
	let agreement = $state<AgreementInput>({ channel: '', when: '', evidence: '', exact: false });

	const can = $derived(appointmentChoice(order, view));
	const target = $derived(choice ? `${dayShort(date)}, ${choice.label}` : '');
	const missing = $derived([
		...(choice ? [] : ['the new time']),
		...(lineProblem(feasibility, 500, 'x')
			? [
					feasibility.trim()
						? 'your note on one line, up to 500 characters'
						: 'your check that you can still make it'
				]
			: []),
		...agreementMissing(
			agreement,
			now,
			{ at: order.placed_at, label: 'the order was placed' },
			target || 'the new time'
		)
	]);
	const noteWrong = $derived(
		feasibility.trim() !== '' && lineProblem(feasibility, 500, 'x') !== ''
	);
	const whenWrong = $derived(
		missing.some((m) => m === 'a time that isn’t in the future' || m.startsWith('a time after'))
	);
	const evidenceWrong = $derived(missing.some((m) => m.startsWith('evidence on one line')));
	const active = $derived(['confirmed', 'in_production', 'ready'].includes(order.lifecycle));
	const selfMove = $derived(
		order.fulfillment_mode === 'collection' || order.fulfillment_mode === 'customer_courier'
	);

	// A different time is a different agreement: the "exactly" tick clears.
	let lastTarget = '';
	$effect(() => {
		const t = target;
		if (t !== lastTarget) {
			lastTarget = t;
			agreement.exact = false;
		}
	});

	async function show(which: 'time' | 'date') {
		open = which;
		date = order.date;
		choice = null;
		await tick();
		document.getElementById('appt-date')?.focus();
	}

	async function close() {
		const was = open;
		open = null;
		await tick();
		document
			.getElementById(
				can.redelivery ? 'appt-redelivery' : was === 'date' ? 'appt-change-date' : 'appt-move-time'
			)
			?.focus();
	}

	async function submit() {
		const at = londonLocalToIso(agreement.when);
		if (missing.length || !choice || !at) return;
		const ok = await decide({
			d: 'appointment',
			date,
			slotId: choice.id,
			channel: agreement.channel,
			agreedAt: at,
			evidence: agreement.evidence,
			feasibility
		});
		if (ok) {
			open = null;
			choice = null;
			feasibility = '';
			agreement = { channel: '', when: '', evidence: '', exact: false };
		}
	}
</script>

{#if view === 'chef' && can.allowed}
	{#if !open}
		<div class="actions">
			{#if can.redelivery}
				<button
					id="appt-redelivery"
					class="btn btn-primary"
					type="button"
					disabled={busy}
					aria-controls="appt-panel"
					aria-expanded="false"
					onclick={() => void show('date')}>Set the new delivery time…</button
				>
			{:else}
				<button
					id="appt-move-time"
					class="btn"
					type="button"
					disabled={busy}
					aria-controls="appt-panel"
					aria-expanded="false"
					onclick={() => void show('time')}>Move time</button
				>
				<button
					id="appt-change-date"
					class="btn"
					type="button"
					disabled={busy}
					aria-controls="appt-panel"
					aria-expanded="false"
					onclick={() => void show('date')}>Change date…</button
				>
			{/if}
		</div>
		<p class="muted small">
			{#if can.redelivery}The customer chose a new delivery after it failed. Agree the time with
				them; it can go out again once this change is confirmed. Nothing more to pay.
			{:else}{selfMove
					? 'The customer can move to another time on the same day up to 6 hours before. '
					: ''}A time or date change needs their agreement and your check that you can still make
				it. Moves never change the refund deadline.{/if}
		</p>
	{:else}
		<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
		<form
			id="appt-panel"
			class="decide-box"
			aria-label={can.redelivery ? 'Set the new delivery time' : 'Change the time or date'}
			novalidate
			onkeydown={(e) => {
				if (e.key === 'Escape') {
					e.preventDefault();
					void close();
				}
			}}
			onsubmit={(e) => {
				e.preventDefault();
				void submit();
			}}
		>
			<p class="small">
				{can.redelivery
					? 'Set the time you agreed with the customer for the new delivery (any published time; another day is your decision).'
					: 'Choose the time you agreed with the customer. Another day is your decision: if the day is full online, the order takes an extra place with your note as the reason.'}
				The refund deadline and the preparation start don’t move. The customer is emailed the new time
				once it’s confirmed.
			</p>
			<SlotPicker
				{api}
				{order}
				{now}
				purpose="appointment"
				idPrefix="appt"
				bind:date
				bind:choice
				disabled={busy}
				dateLabel={open === 'time' ? 'Date (the same day unless you change it)' : 'New date'}
			/>
			<label class="field" for="appt-note"
				>Your check that you can still make it (time, ingredients, shelf life)
				<input
					id="appt-note"
					class="text-input"
					autocomplete="off"
					maxlength="500"
					bind:value={feasibility}
					aria-invalid={noteWrong ? 'true' : undefined}
					aria-describedby="appt-missing"
				/>
			</label>
			<AgreementFields
				idPrefix="appt"
				bind:value={agreement}
				{now}
				exactLabel={target || 'the new time'}
				describedBy="appt-missing"
				invalidWhen={whenWrong}
				invalidEvidence={evidenceWrong}
			/>
			<p id="appt-missing" class="small" aria-live="polite">
				{#if missing.length}Still needed: {missing.join(', ')}.{:else}<span class="ok-text"
						>Ready: the customer’s agreement and your note are saved with the change.</span
					>{/if}
			</p>
			<div class="actions">
				<button
					class="btn btn-primary"
					type="submit"
					disabled={busy || missing.length > 0}
					aria-describedby="appt-missing"
					>{deciding === 'appointment'
						? 'Changing…'
						: target
							? `${can.redelivery ? 'Set delivery for' : 'Change to'} ${target}`
							: 'Choose the new time'}</button
				>
				<button class="btn btn-quiet" type="button" onclick={() => void close()}>Not now</button>
			</div>
		</form>
	{/if}
{:else if view !== 'chef' && active}
	<p class="role-note">Moving an order: Only Elina (chef) can do this.</p>
{:else if view === 'chef' && can.note}
	<p class="role-note">{can.note}</p>
{/if}
{#if view === 'chef'}
	<DecisionStatus area="time" {status} {error} {pending} {busy} {oncheck} />
{/if}
