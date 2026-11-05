<script lang="ts">
	// The shop frame's status message (see toast.svelte.ts). The region is
	// always present so screen readers announce each new message.
	import { toast } from '../toast.svelte';
</script>

<p class="toast" role="status">{toast.message}</p>
