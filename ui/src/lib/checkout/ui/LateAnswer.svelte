<script lang="ts">
	// The answer to the chef's offer after a late payment (GK-017 r02b state
	// 10, Q6): "Yes, confirm this date" (or "Accept <the other date>") and
	// "No, refund everything · £90". The press is the agreement evidence the
	// API records; nothing changes until the API reports the outcome, which
	// the page reads (polled). Accepting is offered only while the API says
	// the offer is acceptable; declining has no limit.
	import type { ChangeBaseline } from '../changes';
	import type { OrderRecoveryApi } from '../client';
	import type { ActionKeys, Who } from '../flow';
	import type { AnswerView } from '../order';
	import { lateBaseline, recoveryProblemFor, sendAnswer } from '../recovery';
	import type { Order } from '../types';

	interface Props {
		order: Order;
		who: Who;
		api: OrderRecoveryApi;
		keys: ActionKeys;
		answer: AnswerView;
		/** The answer was accepted (202): the page reads the order until its outcome. */
		onsent: (baseline: ChangeBaseline, answer: 'accept' | 'decline') => void;
		/** A refusal (nothing changed); refresh: read the order again. */
		onproblem: (message: string, refresh: boolean) => void;
	}

	let { order, who, api, keys, answer, onsent, onproblem }: Props = $props();

	let busy = $state<'' | 'accept' | 'decline'>('');

	async function send(kind: 'accept' | 'decline') {
		if (busy) return;
		busy = kind;
		const baseline = lateBaseline(order);
		try {
			await sendAnswer(api, keys, order, answer.offerId, kind, who);
			onsent(baseline, kind);
		} catch (e) {
			const p = recoveryProblemFor(e, 'answer');
			onproblem(p.message, p.next === 'refresh');
		}
		busy = '';
	}
</script>

<div class="oc-actions">
	{#if answer.acceptLabel}
		<button
			type="button"
			class="primary co-pay"
			onclick={() => void send('accept')}
			aria-disabled={busy ? 'true' : undefined}
			aria-busy={busy === 'accept'}
		>
			<span>{busy === 'accept' ? 'Sending your answer…' : answer.acceptLabel}</span>
			<span class="co-arrow" aria-hidden="true">→</span>
		</button>
	{/if}
	<button
		type="button"
		onclick={() => void send('decline')}
		aria-disabled={busy ? 'true' : undefined}
		aria-busy={busy === 'decline'}
		>{busy === 'decline' ? 'Sending your answer…' : answer.declineLabel}</button
	>
</div>
<p class="small">{answer.note}</p>
