<script lang="ts">
	// The cake's photo area. The public catalogue has no images yet (GK-022
	// Milestone A2: image_revision_id, url, alt, width, height), so this shows
	// a labelled placeholder instead of guessing a photo for a listing. In a
	// card or bag row the placeholder is decorative (the link names the cake).
	//
	// TODO(GK-028 photos): the shop went live with these placeholders by
	// Sasha's decision (2026-11-05). Replace them with the approved photo once
	// GET /api/v1/cakes and /cakes/{id} send it (K01 images; storage is
	// SETUP.md provider row 04; owner photos in docs/creator/GK-002): render
	// the url with its alt, width and height, keep the photo version for
	// "Request changes to this design", and keep this placeholder only for a
	// cake without an approved photo. Tracked in ui/README "Discovery pages"
	// and on the GK-028 card.
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
