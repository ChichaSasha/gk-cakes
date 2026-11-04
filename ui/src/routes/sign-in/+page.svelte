<script lang="ts">
	import { resolve } from '$app/paths';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { safeReturnPath } from '../../lib/api';
	import { auth, callbackUrl, rememberReturnPath, signInConfigured } from '../../lib/auth';

	const returnPath = safeReturnPath(page.url.searchParams.get('next'));

	let email = $state('');
	let password = $state('');
	let busy = $state(false);
	let message = $state('');
	let linkSent = $state(false);

	async function signInWithPassword(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		message = '';
		try {
			const { error } = await auth().auth.signInWithPassword({ email, password });
			if (error) {
				// One message for every refusal: it never reveals whether an account exists.
				message =
					error.status === 400 || error.status === 401 || error.status === 422
						? 'The email or password is not right.'
						: 'Sign-in is not available right now. Please try again.';
				return;
			}
			password = '';
			await goto(resolve(returnPath), { replaceState: true });
		} finally {
			busy = false;
		}
	}

	async function sendLink() {
		if (!email) {
			message = 'Enter your email address first.';
			return;
		}
		busy = true;
		message = '';
		try {
			rememberReturnPath(returnPath);
			const { error } = await auth().auth.signInWithOtp({
				email,
				options: { emailRedirectTo: callbackUrl(), shouldCreateUser: false }
			});
			// The same answer whether or not the address has an account.
			if (error && error.status !== undefined && error.status >= 500) {
				message = 'Sign-in is not available right now. Please try again.';
				return;
			}
			linkSent = true;
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head>
	<title>Sign in · GK Cakes</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<main class="narrow">
	<h1>Sign in</h1>
	{#if !signInConfigured}
		<p role="alert">Sign-in is not available on this site yet.</p>
	{:else if linkSent}
		<p role="status">
			If an account exists for {email}, we have sent it a sign-in link. It works once, in this
			browser, for 30 minutes.
		</p>
		<p><button type="button" onclick={() => (linkSent = false)}>Use a password instead</button></p>
	{:else}
		<form onsubmit={signInWithPassword}>
			<label>
				Email
				<input type="email" autocomplete="email" required bind:value={email} disabled={busy} />
			</label>
			<label>
				Password
				<input
					type="password"
					autocomplete="current-password"
					required
					bind:value={password}
					disabled={busy}
				/>
			</label>
			<button type="submit" disabled={busy}>Sign in</button>
		</form>
		<p class="or">or</p>
		<button type="button" onclick={sendLink} disabled={busy}>Email me a sign-in link</button>
		{#if message}<p role="alert">{message}</p>{/if}
		<p class="small">You can order without an account.</p>
	{/if}
</main>
