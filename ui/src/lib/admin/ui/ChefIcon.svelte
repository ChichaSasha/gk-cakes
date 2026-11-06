<script lang="ts" module>
	export type ChefIconName =
		| 'today'
		| 'orders'
		| 'plus'
		| 'calendar'
		| 'stock'
		| 'basket'
		| 'mail'
		| 'support'
		| 'money'
		| 'search'
		| 'check';
</script>

<script lang="ts">
	// Line icons of the approved GK-018 r03b2 sketch (decorative; the
	// control or link carries the accessible name).
	let { name, cls = 'icon' }: { name: ChefIconName; cls?: string } = $props();
</script>

<svg class={cls} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
	{#if name === 'today'}
		<path d="M3 10h18M7 3v4M17 3v4" /><rect x="3" y="5" width="18" height="16" rx="3" />
	{:else if name === 'orders'}
		<path d="M5 4h14v16H5z" /><path d="M9 9h6M9 13h6M9 17h3" />
	{:else if name === 'plus'}
		<circle cx="12" cy="12" r="9" /><path d="M12 8v8M8 12h8" />
	{:else if name === 'calendar'}
		<rect x="3" y="5" width="18" height="16" rx="3" /><path d="M3 10h18M8 14h2M14 14h2M8 17h2" />
	{:else if name === 'stock'}
		<path d="M4 8l8-4 8 4-8 4-8-4Z" /><path d="M4 8v8l8 4 8-4V8" />
	{:else if name === 'basket'}
		<path d="M4 9h16l-2 11H6L4 9Z" /><path d="M9 9l3-5 3 5" />
	{:else if name === 'mail'}
		<rect x="3" y="5" width="18" height="14" rx="3" /><path d="m4 7 8 6 8-6" />
	{:else if name === 'support'}
		<circle cx="12" cy="12" r="9" /><circle cx="12" cy="12" r="3.5" /><path
			d="m5.6 5.6 3.9 3.9M14.5 14.5l3.9 3.9M18.4 5.6l-3.9 3.9M9.5 14.5l-3.9 3.9"
		/>
	{:else if name === 'money'}
		<rect x="3" y="6" width="18" height="12" rx="3" /><circle cx="12" cy="12" r="2.5" />
	{:else if name === 'search'}
		<circle cx="11" cy="11" r="7" /><path d="m20 20-4-4" />
	{:else}
		<path d="m5 12 4 4 10-10" />
	{/if}
</svg>
