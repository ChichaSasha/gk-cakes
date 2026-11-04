<script lang="ts">
	import { resolve } from '$app/paths';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth, signInConfigured, takeReturnPath, verifyTokenHash } from '../../../lib/auth';

	let failed = $state(false);

	onMount(() => {
		let active = true;
		(async () => {
			const params = new URL(location.href).searchParams;
			const code = params.get('code');
			const tokenHash = params.get('token_hash');
			const type = params.get('type') ?? '';
			// Remove the one-time code, token hash or provider error from the URL
			// and history before anything else is requested.
			history.replaceState(history.state, '', '/auth/callback');
			let ok = false;
			if (signInConfigured && !params.has('error')) {
				if (code) {
					// Useless without the verifier this browser stored when the link
					// was requested (PKCE).
					ok = !(await auth().auth.exchangeCodeForSession(code)).error;
				} else if (tokenHash) {
					ok = await verifyTokenHash(tokenHash, type); // invitation links
				}
			}
			if (!active) return;
			if (!ok) {
				failed = true;
				return;
			}
			await goto(resolve(takeReturnPath()), { replaceState: true });
		})();
		return () => {
			active = false;
		};
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
		<h1>Signing in</h1>
		<p role="status">Signing you in…</p>
	{/if}
</main>
