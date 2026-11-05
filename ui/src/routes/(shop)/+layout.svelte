<script lang="ts">
	// Discovery pages (GK-028) share the shop frame; checkout, orders, account
	// and admin keep their own layout. The home page (routes/+page.svelte)
	// uses the same frame.
	import ShopShell from '../../lib/shop/ui/ShopShell.svelte';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();
</script>

<ShopShell>
	{@render children()}
</ShopShell>
