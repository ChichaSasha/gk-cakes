<script lang="ts">
	// Refund progress (GK-017 r02b state 13): the card refund's tracker as
	// the API's refund state reports it (past tense only for finished steps)
	// and, with a GK delivery, the chef's delivery refund on its own line.
	import { refundProgress } from '../changes';
	import type { Refund } from '../types';

	interface Props {
		refund: Refund;
		currency?: string;
	}

	let { refund, currency = 'GBP' }: Props = $props();

	const view = $derived(refundProgress(refund, currency));
	// Read after the step's label (the leading space is part of the text).
	const SPOKEN = { done: ' · done', current: ' · current step', upcoming: '' } as const;
</script>

{#if view.lines.length > 0}
	<ul class="oc-split">
		{#each view.lines as line (line.label)}
			<li>
				<span>{line.label}<small>{line.status}</small></span>
				{#if line.amount}<span>{line.amount}</span>{/if}
			</li>
		{/each}
	</ul>
{/if}
<ol class="oc-track" aria-label={view.trackLabel}>
	{#each view.steps as step (step.label)}
		<li
			class={step.state === 'upcoming' ? '' : step.state}
			aria-current={step.state === 'current' ? 'step' : undefined}
		>
			<span>{step.label}</span>{#if step.state !== 'upcoming'}<span class="sr-only"
					>{SPOKEN[step.state]}</span
				>{/if}
		</li>
	{/each}
</ol>
