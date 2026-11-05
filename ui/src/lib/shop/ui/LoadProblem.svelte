<script lang="ts">
	// A failed catalogue load with its recovery (approved GK-016 r01c sketch:
	// "Try again" reloads; nothing else on the page is lost). Focus then moves
	// to the start of the page's content, as this button goes away.
	import { invalidateAll } from '$app/navigation';
	import { tick } from 'svelte';

	let { what = 'the cakes' }: { what?: string } = $props();
	let busy = $state(false);

	async function retry() {
		busy = true;
		try {
			await invalidateAll();
		} finally {
			busy = false;
		}
		await tick();
		document.getElementById('main')?.focus();
	}
</script>

<div class="empty" role="alert">
	<h2>We couldn’t load {what}</h2>
	<p>Please check your connection and try again. If it keeps happening, contact the chef.</p>
	<button class="btn primary" type="button" disabled={busy} aria-busy={busy} onclick={retry}>
		{busy ? 'Trying again…' : 'Try again'}
	</button>
</div>
