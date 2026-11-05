<script lang="ts">
	// A failed catalogue load with its recovery (approved GK-016 r01c sketch:
	// "Try again" reloads; nothing else on the page is lost). When the retry
	// works, focus moves to the start of the page's content, as this button
	// goes away; when it fails again, focus stays here and the alert says so.
	import { invalidateAll } from '$app/navigation';
	import { tick } from 'svelte';

	let { what = 'the cakes' }: { what?: string } = $props();
	let busy = $state(false);
	let failedAgain = $state(false);
	let box = $state<HTMLDivElement>();
	let button = $state<HTMLButtonElement>();

	async function retry() {
		busy = true;
		try {
			await invalidateAll();
		} finally {
			busy = false;
		}
		await tick();
		if (box?.isConnected) {
			// Disabled while busy, the button lost focus; give it back.
			failedAgain = true;
			button?.focus();
		} else document.getElementById('main')?.focus();
	}
</script>

<div class="empty" role="alert" bind:this={box}>
	<h2>{failedAgain ? `We still couldn’t load ${what}` : `We couldn’t load ${what}`}</h2>
	<p>Please check your connection and try again. If it keeps happening, contact the chef.</p>
	<button
		bind:this={button}
		class="btn primary"
		type="button"
		disabled={busy}
		aria-busy={busy}
		onclick={retry}
	>
		{busy ? 'Trying again…' : 'Try again'}
	</button>
</div>
