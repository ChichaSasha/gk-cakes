<script lang="ts">
	// The cake's photo area. The public catalogue has no images yet (GK-022
	// Milestone A2: image_revision_id, url, alt, width, height), so this shows
	// a labelled placeholder instead of guessing a photo for a listing. In a
	// card or bag row the placeholder is decorative (the link names the cake).
	import Icon from './Icon.svelte';

	let { name, decorative = false }: { name: string; decorative?: boolean } = $props();
</script>

{#if decorative}
	<span class="photo-pending" aria-hidden="true">
		<Icon name="cake" />
		<span>Photo coming soon</span>
	</span>
{:else}
	<span class="photo-pending" role="img" aria-label={`${name}: photo coming soon`}>
		<Icon name="cake" />
		<span aria-hidden="true">Photo coming soon</span>
	</span>
{/if}
