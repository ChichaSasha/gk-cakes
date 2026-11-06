<script lang="ts">
	// Order timeline (GK-017 r02b state 19): Money and Your cake side by
	// side (one column on narrow screens), each step ticked, ringed (now),
	// marked as a problem or still to come, as the API's order reports it.
	// The state is also spoken after each step's words.
	import type { LaneStep, Timeline } from '../timeline';

	interface Props {
		timeline: Timeline;
	}

	let { timeline }: Props = $props();

	const lanes = $derived<{ id: string; title: string; steps: LaneStep[] }[]>([
		{ id: 'lane-money', title: 'Money', steps: timeline.money },
		{ id: 'lane-cake', title: 'Your cake', steps: timeline.cake }
	]);
	// Read after the step's words (the leading space is part of the text).
	const SPOKEN = {
		done: ' · done',
		current: ' · now',
		problem: ' · problem',
		upcoming: ' · not yet'
	} as const;
</script>

<section class="co-card oc-timeline" aria-labelledby="timeline-h">
	<h2 class="co-h" id="timeline-h">Order timeline</h2>
	<div class="oc-lanes">
		{#each lanes as lane (lane.id)}
			<section class="oc-lane" aria-labelledby={lane.id}>
				<h3 id={lane.id}>{lane.title}</h3>
				<ol class="oc-steps">
					{#each lane.steps as step, i (i)}
						<li
							class={step.state === 'upcoming' ? '' : step.state}
							aria-current={step.state === 'current' ? 'step' : undefined}
						>
							<strong>{step.label}</strong>{#if step.detail}<span>{step.detail}</span>{/if}<span
								class="sr-only">{SPOKEN[step.state]}</span
							>
						</li>
					{/each}
				</ol>
			</section>
		{/each}
	</div>
</section>
