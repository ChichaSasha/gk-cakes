<script lang="ts">
	// Cancellation and refund terms for the chosen time (GK-017 r01, K05):
	// cancelling before handover - 120 elapsed hours returns everything paid.
	// The 50% amount is not computed here (server money only), so it is
	// described as a share; the full amount is the server's total.
	import { deadlineLabel, formatMoney, refundDeadline } from '../format';

	interface Props {
		/** The appointment start (server instant), or null before a time is chosen. */
		startsAt: string | null;
		/** The server's total to pay, when known. */
		totalMinor: number | null;
		currency?: string;
		mode: string;
	}

	let { startsAt, totalMinor, currency = 'GBP', mode }: Props = $props();

	const deadline = $derived(startsAt ? deadlineLabel(refundDeadline(startsAt)) : null);
</script>

<div class="co-box" id="refund-terms">
	<p class="co-label">Cancellation and refunds</p>
	{#if !deadline}
		<p>Choose a time to see your exact cancellation deadline.</p>
	{:else}
		<p>
			Cancel <strong>before {deadline}</strong> for a full refund{totalMinor !== null
				? ' of '
				: ''}{#if totalMinor !== null}<strong>{formatMoney(totalMinor, currency)}</strong>{/if}.
		</p>
		<p>
			From then on, or {mode === 'customer_courier'
				? 'if your courier doesn’t collect it'
				: 'if the cake isn’t collected'}, you get <strong>50%</strong> of what you paid, rounded in your
			favour. If we have to cancel, you get 100% back.
		</p>
		<p>
			Once you’ve paid, moving your date or time won’t change this deadline. After handover, contact
			the chef to request your 50% refund or if something is wrong.
		</p>
	{/if}
</div>
