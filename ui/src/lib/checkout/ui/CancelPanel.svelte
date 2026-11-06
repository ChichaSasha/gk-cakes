<script lang="ts">
	// "Cancel your order?" (GK-017 r02b state 12): the API's preview at its
	// clock (amount, side of the deadline, time left as of the preview's
	// computed_at), confirmed with the preview's ID and hash. When the
	// full-refund deadline passes while the panel is open (a timer, or the
	// API's PREVIEW_STALE), a new preview is shown with the boundary alert and
	// the relabelled button: the customer confirms again. Money is the
	// preview's, never computed here.
	import { onMount, tick } from 'svelte';
	import { newIdempotencyKey } from '../../api';
	import {
		amountChanged,
		boundaryAlert,
		cancelProblemFor,
		previewRefreshDelay,
		previewView
	} from '../changes';
	import type { OrderChangesApi } from '../client';
	import type { ActionKeys, Who } from '../flow';
	import type { CancellationPreview } from '../types';

	interface Props {
		orderId: string;
		who: Who;
		api: OrderChangesApi;
		keys: ActionKeys;
		/** The order has a paid GK delivery (worded "cakes and delivery"). */
		delivery: boolean;
		/** Keep my order, or the order can't be cancelled now (with the reason). */
		onclose: (problem?: string) => void;
		/** The cancellation was accepted (202) for this amount. */
		oncancelled: (amountMinor: number) => void;
	}

	let { orderId, who, api, keys, delivery, onclose, oncancelled }: Props = $props();

	let preview = $state<CancellationPreview | null>(null);
	let alert = $state<{ deadline: string; amount: string; half: boolean } | null>(null);
	let problem = $state('');
	let canRetryPreview = $state(false);
	let busy = $state(false);
	let heading = $state<HTMLElement | null>(null);
	let alertHeading = $state<HTMLElement | null>(null);
	let timer: ReturnType<typeof setTimeout> | undefined;
	let active = true;

	const view = $derived(preview ? previewView(preview, { delivery }) : null);

	onMount(() => {
		heading?.focus();
		void load(false);
		return () => {
			active = false;
			clearTimeout(timer);
		};
	});

	/** Asks for a (new) preview; fresh: compare with the one shown for the boundary alert. */
	async function load(compare: boolean): Promise<boolean> {
		clearTimeout(timer);
		try {
			const p = await api.cancellationPreview(orderId, newIdempotencyKey(), who);
			if (!active) return false;
			const receivedAt = Date.now();
			const crossed = compare ? boundaryAlert(preview, p) : undefined;
			const changed = compare && amountChanged(preview, p);
			preview = p;
			canRetryPreview = false;
			if (crossed) {
				alert = { ...crossed, half: p.rate === 50 };
				problem = '';
				await tick();
				alertHeading?.focus();
			} else if (changed) {
				// Same side of the deadline, another amount (for example the
				// order changed): never confirm a different amount silently.
				problem = 'We’ve refreshed your refund amount. Please check it and confirm again.';
			}
			const delay = previewRefreshDelay(p, receivedAt, receivedAt);
			if (delay !== undefined) timer = setTimeout(() => void load(true), delay);
			return true;
		} catch (e) {
			if (!active) return false;
			const p = cancelProblemFor(e);
			if (p.next === 'close') {
				onclose(p.message);
				return false;
			}
			problem = p.message;
			canRetryPreview = !preview;
			return false;
		}
	}

	/** Keep my order: not while a confirmation is in flight (its answer must reach the page). */
	function keep() {
		if (busy) return;
		onclose();
	}

	async function confirm() {
		if (!preview || busy) return;
		busy = true;
		problem = '';
		const p = preview;
		try {
			await api.cancel(
				orderId,
				{
					expected_version: p.order_version,
					preview_id: p.preview_id,
					preview_hash: p.preview_hash
				},
				keys.for(`cancel:${p.preview_id}`).key,
				who
			);
			if (!active) return;
			oncancelled(p.amount_minor);
		} catch (e) {
			if (!active) return;
			const pr = cancelProblemFor(e);
			if (pr.next === 'close') {
				busy = false;
				onclose(pr.message);
				return;
			}
			if (pr.next === 'repreview') {
				const ok = await load(true);
				// The boundary alert says it all when the deadline was crossed.
				if (ok && !alert) problem = pr.message;
			} else {
				problem = pr.message;
			}
			busy = false;
		}
	}
</script>

<section class="co-card oc-cancel" aria-labelledby="cancel-h">
	<h2 class="co-h oc-focus" id="cancel-h" tabindex="-1" bind:this={heading}>Cancel your order?</h2>
	{#if alert}
		<div class="oc-problem" role="alert">
			<h3 class="oc-focus" tabindex="-1" bind:this={alertHeading}>
				The full-refund deadline has just passed
			</h3>
			<p>
				It was {alert.deadline}. Your refund is now <strong>{alert.amount}</strong>{alert.half
					? ' (50% of what you paid, rounded in your favour)'
					: ''}. Please confirm again if you still want to cancel.
			</p>
		</div>
	{/if}
	{#if !view}
		{#if problem}
			<p class="co-errors" role="alert">{problem}</p>
		{:else}
			<p class="notice" role="status">Checking your refund amount…</p>
		{/if}
		<div class="oc-actions">
			{#if canRetryPreview}
				<button type="button" onclick={() => void load(false)}>Try again</button>
			{/if}
			<button type="button" onclick={() => onclose()}>Keep my order</button>
		</div>
	{:else}
		<div class="oc-refund-box">
			<p class="co-label">{view.side}</p>
			{#if !view.nothingPaid}
				<p class="oc-amount">You get back <strong>{view.amount}</strong></p>
			{/if}
			<p>{view.basis}</p>
			{#if view.split.length > 0}
				<ul class="oc-split">
					{#each view.split as line, i (i)}
						<li><span>{line.label}</span><span>{line.amount}</span></li>
					{/each}
				</ul>
			{/if}
			{#if view.unmatched}<p>{view.unmatched}</p>{/if}
			{#if view.before && !view.nothingPaid}
				<p class="small">
					Full refund until {view.deadline} · <strong>{view.timeLeft}</strong> left (checked at {view.checkedAt}).
					If the deadline passes while this is open, we’ll show the new amount and ask you to
					confirm again.
				</p>
			{/if}
		</div>
		{#if view.roundingExample}
			<div class="oc-example">
				<p class="co-label">Example · how rounding works</p>
				<p>
					If you had paid £85.85, 50% would be £42.925, so you’d get <strong>£42.93</strong>: half
					pennies are rounded in your favour.
				</p>
			</div>
		{/if}
		{#if !view.nothingPaid}
			<p class="small">
				If we have to cancel, you always get 100% back. The refund goes to the payment method you
				used, and this page shows its progress.
			</p>
		{/if}
		{#if problem}
			<p class="co-errors" role="alert">{problem}</p>
		{/if}
		<div class="oc-actions">
			<button
				type="button"
				class="primary co-pay"
				onclick={confirm}
				aria-disabled={busy ? 'true' : undefined}
				aria-busy={busy}
			>
				<span>{busy ? 'Cancelling your order…' : view.buttonLabel}</span>
				<span class="co-arrow" aria-hidden="true">→</span>
			</button>
			<button type="button" onclick={keep} aria-disabled={busy ? 'true' : undefined}
				>Keep my order</button
			>
		</div>
	{/if}
</section>
