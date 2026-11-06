<script lang="ts">
	// Profile & contact (approved GK-016 r02b sketch, states 43–46): the
	// optional name, phone and Instagram name (GET/PATCH /me/profile with the
	// version the customer saw), how they sign in, and the emails we send.
	// Errors are checked here with the API's rules and the sketch's words; a
	// 409 STALE_VERSION loads the latest details and keeps the customer's own
	// changes. Changing the sign-in email waits for GK-021. No analytics (K10).
	import { resolve } from '$app/paths';
	import { goto } from '$app/navigation';
	import { onMount, tick } from 'svelte';
	import { ApiError } from '../../../lib/api';
	import {
		profile as readProfile,
		updateProfile,
		type AccountApi,
		type Profile
	} from '../../../lib/account/client';
	import {
		accountProblem,
		endedSession,
		leaveWhenSignedOut,
		signOutHere,
		toSignIn
	} from '../../../lib/account/guard';
	import {
		buildPatch,
		conflictLine,
		fieldError,
		formValues,
		invalidField,
		keptNote,
		mergeConflict,
		MESSAGES,
		NAME_MAX,
		PHONE_MAX,
		INSTAGRAM_MAX,
		SaveKey,
		validate,
		type Field,
		type FieldErrors,
		type FormValues,
		type Merge,
		type ProfilePatch
	} from '../../../lib/account/profile';
	import { accountApi, signedInEmail } from '../../../lib/account/session';
	import AccountShell from '../../../lib/account/ui/AccountShell.svelte';

	interface Input {
		field: Field;
		id: string;
		label: string;
		hint?: string;
		type: 'text' | 'tel';
		autocomplete: 'name' | 'tel' | 'off';
		maxlength: number;
	}

	// The sketch's fields and hints. The name hint ("Filled in from Google…
	// also filled in for you at checkout") is left out until Google sign-in
	// (GK-021) and the checkout prefill exist.
	const INPUTS: Input[] = [
		{
			field: 'full_name',
			id: 'pf-name',
			label: 'Full name',
			type: 'text',
			autocomplete: 'name',
			maxlength: NAME_MAX
		},
		{
			field: 'phone',
			id: 'pf-phone',
			label: 'Phone',
			hint: 'Only used if the chef needs to reach you about an order.',
			type: 'tel',
			autocomplete: 'tel',
			maxlength: PHONE_MAX
		},
		{
			field: 'instagram',
			id: 'pf-ig',
			label: 'Instagram name',
			hint: 'Private. Helps the chef recognise your messages. It doesn’t prove who you are and isn’t used to sign in.',
			type: 'text',
			autocomplete: 'off',
			// One more for a typed @ (removed before saving).
			maxlength: INSTAGRAM_MAX + 1
		}
	];
	const SERVER_WORDS: Record<Field, string> = {
		full_name: MESSAGES.nameLong,
		phone: MESSAGES.phone,
		instagram: MESSAGES.instagram
	};

	let api: AccountApi | null = null;
	const keys = new SaveKey();
	let email = $state<string>();
	/** The latest saved profile this page knows (edits are measured from it). */
	let saved = $state<Profile | null>(null);
	let values = $state<FormValues>({ full_name: '', phone: '', instagram: '' });
	let errors = $state<FieldErrors>({});
	/** The error summary as of the last save attempt. */
	let summary = $state<{ field: Field; id: string; message: string }[]>([]);
	let kept = $state<Merge['kept']>({});
	let conflict = $state('');
	let problem = $state('');
	let done = $state(false);
	let saving = $state(false);
	let blocked = $state('');
	let loadProblem = $state('');
	let retrying = $state(false);
	let signOutProblem = $state('');

	function clear() {
		saved = null;
		values = { full_name: '', phone: '', instagram: '' };
		errors = {};
		summary = [];
		kept = {};
		conflict = '';
	}

	async function focus(id: string) {
		await tick();
		document.getElementById(id)?.focus();
	}

	function show(p: Profile) {
		saved = p;
		values = formValues(p);
	}

	async function load(): Promise<boolean> {
		if (!api) return false;
		loadProblem = '';
		try {
			show(await readProfile(api));
			return true;
		} catch (e) {
			if (await endedSession(e)) return false;
			if (e instanceof ApiError && /^ACCOUNT_(DISABLED|DELETED)$/.test(e.code)) {
				blocked = accountProblem(e, 'your details');
			} else {
				loadProblem = accountProblem(e, 'your details');
			}
			return false;
		}
	}

	async function retryLoad() {
		retrying = true;
		const ok = await load();
		retrying = false;
		await focus(ok ? 'details-h' : 'pf-retry');
	}

	onMount(() => {
		let active = true;
		(async () => {
			api = await accountApi();
			if (!active) return;
			if (!api) {
				await toSignIn();
				return;
			}
			email = await signedInEmail();
			if (active) await load();
		})();
		const unsubscribe = leaveWhenSignedOut(clear);
		return () => {
			active = false;
			unsubscribe();
		};
	});

	function edited(field: Field) {
		// Once an error shows, it follows what the customer types (r02b state 45).
		if (errors[field]) errors[field] = fieldError(field, values[field]);
		// "Saved." describes the last save, not new unsaved edits.
		done = false;
	}

	function describedBy(i: Input): string | undefined {
		const ids = [
			i.hint ? `${i.id}-hint` : '',
			i.field in kept ? `${i.id}-kept` : '',
			errors[i.field] ? `${i.id}-error` : ''
		].filter(Boolean);
		return ids.length ? ids.join(' ') : undefined;
	}

	function toField(event: MouseEvent, id: string) {
		event.preventDefault();
		document.getElementById(id)?.focus();
	}

	async function showErrors(found: FieldErrors) {
		errors = found;
		summary = INPUTS.filter((i) => found[i.field]).map((i) => ({
			field: i.field,
			id: i.id,
			message: found[i.field] as string
		}));
		await focus('pf-errors');
	}

	async function savedNow() {
		kept = {};
		conflict = '';
		done = true;
		await focus('pf-status');
	}

	async function stale() {
		if (!api || !saved) return;
		const started = saved;
		try {
			const latest = await readProfile(api);
			const merged = mergeConflict(started, latest, values);
			saved = latest;
			values = merged.values;
			kept = merged.kept;
			conflict = conflictLine(merged.kept);
			await focus('pf-conflict');
		} catch (e) {
			if (await endedSession(e)) return;
			problem = accountProblem(e, 'your latest details');
			await focus('pf-problem');
		}
	}

	async function failed(e: unknown) {
		if (await endedSession(e)) return;
		if (!(e instanceof ApiError)) {
			keys.forget();
			problem = 'We couldn’t save your details right now. Please try again.';
			await focus('pf-problem');
			return;
		}
		if (/^ACCOUNT_(DISABLED|DELETED)$/.test(e.code)) {
			keys.forget();
			clear();
			blocked = accountProblem(e, 'your details');
			// The form (and its focused button) is gone: focus the message.
			await focus('pf-blocked');
			return;
		}
		if (e.code === 'STALE_VERSION') {
			keys.forget();
			await stale();
			return;
		}
		const field = e.code === 'INVALID_INPUT' ? invalidField(e.message) : undefined;
		if (field) {
			keys.forget();
			await showErrors({ [field]: fieldError(field, values[field]) ?? SERVER_WORDS[field] });
			return;
		}
		// A lost answer may still have saved: the same save keeps its key.
		if (!(e.status === 0 || e.retryable)) keys.forget();
		problem = 'We couldn’t save your details right now. Please try again.';
		await focus('pf-problem');
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (saving || !api || !saved) return;
		done = false;
		problem = '';
		const found = validate(values);
		if (Object.keys(found).length > 0) {
			await showErrors(found);
			return;
		}
		errors = {};
		summary = [];
		const patch: ProfilePatch | null = buildPatch(saved, values);
		if (!patch) {
			// Nothing differs from the saved details: they are what is saved.
			await savedNow();
			return;
		}
		saving = true;
		try {
			const next = await updateProfile(api, keys.for(patch), patch);
			keys.forget();
			show(next);
			saving = false;
			await savedNow();
		} catch (e) {
			saving = false;
			await failed(e);
		}
	}

	async function leave() {
		const reached = await signOutHere();
		clear();
		if (!reached) {
			signOutProblem =
				'You are signed out on this device, but the sign-in service could not be reached to end the session everywhere.';
			return;
		}
		await goto(resolve('/'), { replaceState: true });
	}
</script>

<svelte:head>
	<title>Profile &amp; contact · GK Cake London</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<AccountShell
	active="profile"
	title="Profile & contact"
	sub={email ? `Signed in as ${email}` : undefined}
	onsignout={saved || blocked || loadProblem ? leave : undefined}
>
	{#if signOutProblem}
		<div class="notice error" role="alert"><div><p>{signOutProblem}</p></div></div>
	{:else if blocked}
		<div class="notice error" role="alert" id="pf-blocked" tabindex="-1">
			<div><p>{blocked}</p></div>
		</div>
		<div class="acc-actions">
			<a class="button" href="mailto:gk.cake.london@gmail.com">Contact us</a>
			<button class="btn" type="button" onclick={leave}>Sign out</button>
		</div>
	{:else if loadProblem}
		<div class="notice error" role="alert"><div><p>{loadProblem}</p></div></div>
		<div class="acc-actions">
			<button
				class="btn"
				id="pf-retry"
				type="button"
				disabled={retrying}
				aria-busy={retrying}
				onclick={retryLoad}>{retrying ? 'Trying again…' : 'Try again'}</button
			>
		</div>
	{:else if !saved}
		<p role="status" aria-busy="true">Loading your details…</p>
	{:else}
		<div role="status">
			{#if done}
				<div class="notice success" id="pf-status" tabindex="-1"><div><p>Saved.</p></div></div>
			{/if}
		</div>
		{#if summary.length > 0}
			<div class="acc-errsum" id="pf-errors" role="alert" tabindex="-1">
				<h2>There’s a problem</h2>
				<ul>
					{#each summary as s (s.field)}
						<li><a href="#{s.id}" onclick={(e) => toField(e, s.id)}>{s.message}</a></li>
					{/each}
				</ul>
			</div>
		{/if}
		{#if conflict}
			<div class="notice" id="pf-conflict" role="alert" tabindex="-1">
				<div>
					<p>
						<strong>These details were changed somewhere else</strong> (perhaps in another tab)
						after you opened this page. {conflict}
					</p>
				</div>
			</div>
		{/if}
		{#if problem}
			<div class="notice error" id="pf-problem" role="alert" tabindex="-1">
				<div><p>{problem}</p></div>
			</div>
		{/if}

		<section class="acc-card" id="details" aria-labelledby="details-h">
			<h2 class="acc-h" id="details-h" tabindex="-1">Your details</h2>
			<form id="pf-form" novalidate onsubmit={save}>
				{#each INPUTS as i (i.field)}
					<div class="field">
						<label for={i.id}>{i.label} <span class="quiet">(optional)</span></label>
						{#if i.hint}<span class="small quiet" id="{i.id}-hint">{i.hint}</span>{/if}
						<input
							id={i.id}
							name={i.field}
							type={i.type}
							autocomplete={i.autocomplete}
							autocapitalize={i.field === 'instagram' ? 'none' : undefined}
							spellcheck={i.field === 'full_name' ? undefined : false}
							maxlength={i.maxlength}
							readonly={saving}
							class:acc-changed={i.field in kept}
							aria-invalid={errors[i.field] ? 'true' : undefined}
							aria-describedby={describedBy(i)}
							bind:value={values[i.field]}
							oninput={() => edited(i.field)}
						/>
					</div>
					{#if i.field in kept}
						<p class="small acc-kept" id="{i.id}-kept">{keptNote(kept[i.field] ?? null)}</p>
					{/if}
					<p class="field-error" id="{i.id}-error" hidden={!errors[i.field]}>
						{errors[i.field] ?? ''}
					</p>
				{/each}
				<div class="acc-actions">
					<button class="btn primary" type="submit" disabled={saving} aria-busy={saving}
						>{saving ? 'Saving…' : 'Save changes'}</button
					>
				</div>
			</form>
		</section>

		<section class="acc-card" id="signing" aria-labelledby="signing-h">
			<h2 class="acc-h" id="signing-h">Signing in</h2>
			{#if email}
				<dl class="acc-dl">
					<div>
						<dt>Email</dt>
						<dd>{email}<small>Sign-in links go here.</small></dd>
					</div>
					<div>
						<dt>Email link</dt>
						<dd>On</dd>
					</div>
				</dl>
				<p class="small quiet">
					One account per verified email address: email links for {email} open this account.
				</p>
			{:else}
				<p class="small quiet">One account per verified email address.</p>
			{/if}
		</section>

		<section class="acc-card" id="emails" aria-labelledby="emails-h">
			<h2 class="acc-h" id="emails-h">Emails we send</h2>
			<p>
				Sign-in links come from our sign-in service. Emails about your orders and your account, such
				as the inactivity warning, will be needed for your orders, so they can’t be switched off
				once we send them.
			</p>
			<p>We don’t send marketing emails.</p>
		</section>
	{/if}
</AccountShell>
