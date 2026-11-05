<script lang="ts">
	// Line icons from the approved GK-016 r01c sketch (decorative: hidden from
	// assistive technology; the control carries the accessible name).
	let { name }: { name: 'bag' | 'user' | 'menu' | 'calendar' | 'cake' } = $props();
</script>

<svg class="icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
	{#if name === 'bag'}
		<path d="M5 7h14l1 14H4L5 7Z" /><path d="M8 8V6a4 4 0 0 1 8 0v2" />
	{:else if name === 'user'}
		<circle cx="12" cy="7" r="3.5" /><path d="M4 21v-2a8 8 0 0 1 16 0v2Z" />
	{:else if name === 'menu'}
		<path d="M3 6h18M3 12h18M3 18h18" />
	{:else if name === 'calendar'}
		<rect x="3" y="5" width="18" height="16" rx="2" /><path d="M7 2v6M17 2v6M3 10h18" />
	{:else}
		<path d="M4 20h16M5 20v-7h14v7M7 13v-3h10v3M12 10V7M12 4.5v.5" />
	{/if}
</svg>
