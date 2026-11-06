<script lang="ts">
	// A decision's status line (polite live region) and error (alert), as the
	// steps' in the Progress card; the page moves focus to them by ID.
	let {
		area,
		status,
		error,
		pending,
		busy,
		oncheck
	}: {
		area: string;
		status: string;
		error: string;
		pending: boolean;
		busy: boolean;
		oncheck: () => void;
	} = $props();
</script>

<p id="{area}-status" class="small step-status" role="status" tabindex="-1">{status}</p>
{#if pending}
	<button class="btn btn-small" type="button" disabled={busy} onclick={oncheck}>Check again</button>
{/if}
<div role="alert" class="step-error-wrap">
	{#if error}<p id="{area}-error" class="small bad-text step-error" tabindex="-1">{error}</p>{/if}
</div>
