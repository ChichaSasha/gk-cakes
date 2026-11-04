<script lang="ts">
	import { resolve } from '$app/paths';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { ApiError } from '../../lib/api';
	import { authedGet, currentSession, signInConfigured, signOut, type Me } from '../../lib/auth';

	let me = $state<Me | null>(null);
	let problem = $state('');

	onMount(async () => {
		if (!signInConfigured || !(await currentSession())) {
			await goto(resolve('/sign-in?next=/account'), { replaceState: true });
			return;
		}
		try {
			me = await authedGet<Me>('/api/v1/me');
		} catch (e) {
			if (e instanceof ApiError && e.signInRequired) {
				await signOut();
				await goto(resolve('/sign-in?next=/account'), { replaceState: true });
				return;
			}
			problem =
				e instanceof ApiError && e.code === 'ACCOUNT_DISABLED'
					? 'This account is disabled. Please contact us if you think this is a mistake.'
					: 'We could not load your account right now. Please try again.';
		}
	});

	async function leave() {
		await signOut();
		me = null;
		await goto(resolve('/'), { replaceState: true });
	}
</script>

<svelte:head>
	<title>Your account · GK Cakes</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<main class="narrow">
	<h1>Your account</h1>
	{#if problem}
		<p role="alert">{problem}</p>
		<p><button type="button" onclick={leave}>Sign out</button></p>
	{:else if me}
		<dl>
			<dt>Account</dt>
			<dd><code>{me.account_id}</code></dd>
			{#if me.capabilities.length > 0}
				<dt>Staff access</dt>
				<dd>{me.capabilities.join(', ')} · <a href={resolve('/admin')}>Open staff area</a></dd>
			{/if}
		</dl>
		<p class="small">Order history and favourites will appear here.</p>
		<p><button type="button" onclick={leave}>Sign out</button></p>
	{:else}
		<p role="status">Loading…</p>
	{/if}
</main>
