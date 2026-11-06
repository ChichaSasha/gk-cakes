<script lang="ts">
	// Cancel and refund (r03b2 order.html "Cancel and refund…", the chef's
	// branch "I’m cancelling (GK can’t make it)"): a reason, then a review of
	// what goes back (100% of everything paid, from the server's figures),
	// then the cancellation, watched until it's secured (K11). The customer's
	// own cancellation (with their 100%/50% rule and agreement) stays on
	// their order page; the API has no chef route for it.
	import { tick } from 'svelte';
	import { formatMoney } from '../../checkout/format';
	import {
		cancelChoice,
		cancelPreview,
		cancelPreviewLines,
		noteProblem,
		type MoneyAction,
		type MoneyInput
	} from '../order-money';
	import { fulfillmentState, type StaffOrderDetail } from '../order-detail';
	import type { StaffView } from '../orders';

	let {
		order,
		view,
		busy,
		acting,
		act
	}: {
		order: StaffOrderDetail;
		view: StaffView;
		busy: boolean;
		acting: MoneyAction | null;
		act: (input: MoneyInput) => Promise<boolean>;
	} = $props();

	let open = $state<'form' | 'review' | null>(null);
	let reason = $state('');
	let reasonError = $state('');

	const choice = $derived(cancelChoice(order, view));
	const preview = $derived(cancelPreview(order));
	const lines = $derived(cancelPreviewLines(order, preview));
	const total = $derived(preview.paidMinor + preview.strayMinor);
	const out = $derived(fulfillmentState(order) === 'out_for_delivery');

	async function toggle() {
		open = open ? null : 'form';
		if (open) {
			await tick();
			document.getElementById('cancel-reason')?.focus();
		}
	}

	async function review() {
		reasonError = noteProblem(reason, 'why you’re cancelling');
		await tick();
		if (reasonError) {
			document.getElementById('cancel-reason')?.focus();
			return;
		}
		open = 'review';
		await tick();
		document.getElementById('cancel-review-h')?.focus();
	}

	async function back() {
		open = 'form';
		await tick();
		document.getElementById('cancel-reason')?.focus();
	}

	async function close() {
		open = null;
		await tick();
		document.getElementById('cancel-open')?.focus();
	}

	async function confirm() {
		const ok = await act({ m: 'cancel', reason, amountMinor: total });
		if (ok) {
			open = null;
			reason = '';
		}
	}

	function onKey(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			e.preventDefault();
			void close();
		}
	}
</script>

{#if choice.can}
	<div class="actions">
		<button
			id="cancel-open"
			class="btn"
			type="button"
			aria-expanded={open !== null}
			aria-controls="cancel-panel"
			disabled={busy}
			onclick={() => void toggle()}>Cancel and refund…</button
		>
	</div>
	{#if open === 'form'}
		<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
		<form
			id="cancel-panel"
			class="decide-box"
			aria-label="Cancel and refund"
			novalidate
			onkeydown={onKey}
			onsubmit={(e) => {
				e.preventDefault();
				void review();
			}}
		>
			<p class="small">
				You’re cancelling (GK can’t make it): the customer gets 100% of everything paid. If the
				customer asked to cancel, they do it on their order page, where their own rule applies.
			</p>
			<label class="field" for="cancel-reason"
				>Why you’re cancelling (kept with the order)
				<input
					id="cancel-reason"
					autocomplete="off"
					maxlength="500"
					placeholder="e.g. I can’t make the cake on this date"
					bind:value={reason}
					oninput={() => (reasonError = '')}
					aria-invalid={reasonError ? 'true' : undefined}
					aria-describedby={reasonError ? 'cancel-reason-error' : undefined}
				/>
			</label>
			{#if reasonError}<p id="cancel-reason-error" class="small bad-text">{reasonError}</p>{/if}
			<div class="actions">
				<button class="btn btn-primary" type="submit" disabled={busy}
					>Review the cancellation</button
				>
				<button class="btn btn-quiet" type="button" onclick={() => void close()}>Not now</button>
			</div>
		</form>
	{:else if open === 'review'}
		<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
		<div
			id="cancel-panel"
			class="decide-box decide-danger"
			role="group"
			aria-labelledby="cancel-review-h"
			onkeydown={onKey}
		>
			<p id="cancel-review-h" class="strong" tabindex="-1">Cancel this order?</p>
			{#each lines as line, i (i)}<p class="small">{line}</p>{/each}
			{#if out}
				<p class="small warn-text">
					It’s out for delivery now. Cancelling ends the order: the cake isn’t handed over.
				</p>
			{/if}
			<p class="small muted">
				Reason: “{reason.trim()}”. The refund goes to the payment provider first and shows as not
				confirmed until they confirm. A cancelled order can’t be reopened.
			</p>
			<div class="actions">
				<button class="btn btn-danger" type="button" disabled={busy} onclick={() => void confirm()}
					>{acting === 'cancel'
						? 'Cancelling…'
						: total
							? `Cancel order and refund ${formatMoney(total, preview.currency)}`
							: 'Cancel order'}</button
				>
				<button class="btn btn-quiet" type="button" disabled={busy} onclick={() => void back()}
					>Back</button
				>
			</div>
		</div>
	{/if}
{/if}
{#if choice.note}<p class="role-note">{choice.note}</p>{/if}
