<script lang="ts">
	import { resolve } from '$app/paths';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { ApiError } from '../../lib/api';
	import {
		authedGet,
		currentSession,
		leaveOnSignOut,
		signInConfigured,
		signOut,
		type Me,
		type StaffMember
	} from '../../lib/auth';

	let me = $state<Me | null>(null);
	let staff = $state<StaffMember[] | null>(null);
	let problem = $state('');

	const labels: Record<string, string> = {
		chef: 'Chef',
		fulfillment: 'Fulfilment',
		technical_support: 'Technical support'
	};

	onMount(() => {
		let active = true;
		(async () => {
			if (!signInConfigured || !(await currentSession())) {
				if (!active) return;
				await goto(resolve('/sign-in?next=/admin'), { replaceState: true });
				return;
			}
			try {
				me = await authedGet<Me>('/api/v1/admin/me');
				if (me.capabilities.includes('chef')) {
					staff = (await authedGet<{ staff: StaffMember[] }>('/api/v1/admin/staff')).staff;
				}
			} catch (e) {
				if (!active) return;
				if (e instanceof ApiError && e.signInRequired) {
					await signOut();
					if (!active) return;
					await goto(resolve('/sign-in?next=/admin&ended=1'), { replaceState: true });
					return;
				}
				if (e instanceof ApiError && e.code === 'FORBIDDEN') {
					problem = 'This account has no staff access.';
				} else if (e instanceof ApiError && e.code === 'ACCOUNT_DISABLED') {
					problem = 'This account is disabled.';
				} else {
					problem = 'The staff area could not be loaded right now. Please try again.';
				}
			}
		})();
		// Signed out in another tab: leave, so no private data stays on screen.
		const unsubscribe = leaveOnSignOut(() => {
			me = null;
			staff = null;
			void goto(resolve('/sign-in?next=/admin'), { replaceState: true });
		});
		return () => {
			active = false;
			unsubscribe();
		};
	});

	async function leave() {
		const reached = await signOut();
		me = null;
		staff = null;
		if (!reached) {
			problem =
				'You are signed out on this device, but the sign-in service could not be reached to end the session everywhere.';
			return;
		}
		await goto(resolve('/'), { replaceState: true });
	}
</script>

<svelte:head>
	<title>Staff · GK Cakes</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<main class="narrow">
	<h1>Staff</h1>
	{#if problem}
		<p role="alert">{problem}</p>
		<p><button type="button" onclick={leave}>Sign out</button></p>
	{:else if me}
		<p>
			Signed in with: {me.capabilities.map((c) => labels[c] ?? c).join(', ')}.
		</p>
		{#if staff}
			<h2>Active staff</h2>
			<table>
				<thead><tr><th>Account</th><th>Access</th><th>Since</th></tr></thead>
				<tbody>
					{#each staff as member (member.account_id + member.capability)}
						<tr>
							<td
								><code>{member.account_id}</code
								>{#if member.account_id === me.account_id}&nbsp;(you){/if}</td
							>
							<td>{labels[member.capability] ?? member.capability}</td>
							<td
								>{new Date(member.granted_at).toLocaleDateString('en-GB', {
									timeZone: 'Europe/London'
								})}</td
							>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
		<p><a href={resolve('/admin/catalogue')}>Catalogue</a> (read only)</p>
		<p class="small">Daily orders and the schedule will appear here.</p>
		<p><button type="button" onclick={leave}>Sign out</button></p>
	{:else}
		<p role="status">Loading…</p>
	{/if}
</main>
