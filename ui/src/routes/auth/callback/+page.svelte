<script lang="ts">
	import { resolve } from '$app/paths';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth, signInConfigured, takeReturnPath } from '../../../lib/auth';

	let failed = $state(false);

	onMount(async () => {
		const params = new URL(location.href).searchParams;
		const code = params.get('code');
		// Remove the one-time code (or the provider's error) from the URL and
		// history before anything else is requested.
		history.replaceState(history.state, '', '/auth/callback');
		if (!signInConfigured || !code || params.has('error')) {
			failed = true;
			return;
		}
		// The code is useless without the verifier this browser stored when
		// the link was requested (PKCE).
		const { error } = await auth().auth.exchangeCodeForSession(code);
		if (error) {
			failed = true;
			return;
		}
		await goto(resolve(takeReturnPath()), { replaceState: true });
	});
</script>

<svelte:head>
	<title>Signing in · GK Cakes</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<main class="narrow">
	{#if failed}
		<h1>This sign-in link didn't work</h1>
		<p role="alert">
			It may have expired, been used already, or been opened in a different browser from the one
			that asked for it.
		</p>
		<p><a href={resolve('/sign-in')}>Sign in again</a></p>
	{:else}
		<p role="status">Signing you in…</p>
	{/if}
</main>
