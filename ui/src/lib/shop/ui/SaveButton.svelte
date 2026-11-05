<script lang="ts">
	// The heart on a cake card (approved GK-016 r01c sketch): saves the cake on
	// this device for a year, or removes it, and says so. Nothing is sent
	// anywhere; adding favourites to an account comes with the account pages.
	import { favourites } from '../favourites.svelte';
	import { toast } from '../toast.svelte';
	import Icon from './Icon.svelte';

	let {
		id,
		name,
		onremove
	}: {
		id: string;
		name: string;
		/** Called after this cake is removed (the favourites page moves focus). */
		onremove?: (button: HTMLButtonElement) => void;
	} = $props();

	const saved = $derived(favourites.has(id));

	function press(e: MouseEvent) {
		const button = e.currentTarget as HTMLButtonElement;
		const result = favourites.toggle(id);
		if (result === null) {
			toast.show('This browser cannot keep things on this device.');
			return;
		}
		toast.show(result ? 'Saved on this device for a year.' : 'Removed from favourites.');
		if (!result) onremove?.(button);
	}
</script>

<button
	class="save"
	type="button"
	aria-label={`${saved ? 'Unsave' : 'Save'} ${name}`}
	aria-pressed={saved}
	onclick={press}
>
	<Icon name="heart" />
</button>
