<script lang="ts">
	// The discovery pages' frame (GK-028): header, main and footer from the
	// approved GK-016 r01c sketch, with its styles. Used by the (shop) layout
	// and the home page.
	import '../shop.css';
	import type { Snippet } from 'svelte';
	import { onMount } from 'svelte';
	import { bag } from '../bag.svelte';
	import ShopFooter from './ShopFooter.svelte';
	import ShopHeader from './ShopHeader.svelte';

	let { children }: { children: Snippet } = $props();

	onMount(() => bag.load());
</script>

<div class="shop">
	<ShopHeader />
	<main id="main" tabindex="-1">
		{@render children()}
	</main>
	<ShopFooter />
</div>
