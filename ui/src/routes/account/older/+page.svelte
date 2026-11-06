<script lang="ts">
	// Add an older order (approved GK-016 r02b sketch, states 38–39): the
	// chef's reference, how the customer ordered, roughly when and a short
	// note, checked here with the API's rules and the sketch's words, then
	// POST /me/order-claims (path c) with one Idempotency-Key per request
	// (kept while the same request is retried). Nothing is added until the
	// chef checks it; the request page shows the result. No analytics (K10).
	import { resolve } from '$app/paths';
	import { goto } from '$app/navigation';
	import { onMount, tick } from 'svelte';
	import { ApiError } from '../../../lib/api';
	import { signOut } from '../../../lib/auth';
	import { claimOlderOrder, type AccountApi } from '../../../lib/account/client';
	import {
		accountProblem,
		accountUnusable,
		endedSession,
		leaveWhenSignedOut,
		toSignIn
	} from '../../../lib/account/guard';
	import {
		CHANNELS,
		claimBody,
		fieldError,
		FIELDS,
		invalidField,
		maybeArrived,
		MONTHS,
		REFERENCE_MAX,
		refusal,
		SEND_PROBLEM,
		SERVER_WORDS,
		validate,
		yearOptions,
		type OlderErrors,
		type OlderField,
		type OlderValues
	} from '../../../lib/account/older';
	import { SaveKey } from '../../../lib/account/profile';
	import { accountApi, signedInEmail } from '../../../lib/account/session';
	import AccountShell from '../../../lib/account/ui/AccountShell.svelte';

	/** Where each field's error links to (the radio group: its first radio). */
	const IDS: Record<OlderField, string> = {
		reference: 'old-ref',
		channel: `old-how-${CHANNELS[0].value}`,
		month: 'old-month',
		year: 'old-year',
		note: 'old-note'
	};
	const HINTS: Partial<Record<OlderField, string>> = {
		reference: 'old-ref-hint',
		note: 'old-note-hint'
	};
	const YEARS = yearOptions();
	const EMPTY: OlderValues = { reference: '', channel: 'instagram', month: '', year: '', note: '' };

	let api = $state.raw<AccountApi | null>(null);
	const keys = new SaveKey();
	let email = $state<string>();
	let values = $state<OlderValues>({ ...EMPTY });
	let errors = $state<OlderErrors>({});
	/** The error summary as of the last send attempt. */
	let summary = $state<{ field: OlderField; id: string; message: string }[]>([]);
	let problem = $state('');
	let sending = $state(false);
	let blocked = $state('');
	let signOutProblem = $state('');

	function clear() {
		api = null;
		values = { ...EMPTY };
		errors = {};
		summary = [];
		problem = '';
	}

	async function focus(id: string) {
		await tick();
		document.getElementById(id)?.focus();
	}

	onMount(() => {
		let active = true;
		(async () => {
			const found = await accountApi();
			if (!active) return;
			if (!found) {
				await toSignIn();
				return;
			}
			email = await signedInEmail();
			if (active) api = found;
		})();
		const unsubscribe = leaveWhenSignedOut(clear);
		return () => {
			active = false;
			unsubscribe();
		};
	});

	// Once an error shows, it follows what the customer types or picks (r02b
	// state 39); the month and year are checked together.
	function edited(field: OlderField) {
		const linked: OlderField[] =
			field === 'month' || field === 'year' ? ['month', 'year'] : [field];
		for (const f of linked) if (errors[f]) errors[f] = fieldError(f, values);
	}

	function describedBy(field: OlderField): string | undefined {
		const ids = [HINTS[field] ?? '', errors[field] ? `${IDS[field]}-error` : ''].filter(Boolean);
		return ids.length ? ids.join(' ') : undefined;
	}

	function toField(event: MouseEvent, id: string) {
		event.preventDefault();
		document.getElementById(id)?.focus();
	}

	async function showErrors(found: OlderErrors) {
		errors = found;
		summary = FIELDS.filter((f) => found[f]).map((f) => ({
			field: f,
			id: IDS[f],
			message: found[f] as string
		}));
		await focus('old-errors');
	}

	async function failed(e: unknown) {
		if (await endedSession(e)) return;
		if (!(e instanceof ApiError)) {
			keys.forget();
			problem = SEND_PROBLEM;
			await focus('old-problem');
			return;
		}
		if (accountUnusable(e)) {
			keys.forget();
			blocked = accountProblem(e, 'your account');
			// The form (and its focused button) is gone: focus the message.
			await focus('old-blocked');
			return;
		}
		const field = e.code === 'INVALID_INPUT' ? invalidField(e.message) : undefined;
		if (field) {
			keys.forget();
			await showErrors({ [field]: fieldError(field, values) ?? SERVER_WORDS[field] });
			return;
		}
		// A lost answer may still have arrived: the same request keeps its key.
		if (!maybeArrived(e)) keys.forget();
		problem = refusal(e);
		await focus('old-problem');
	}

	async function send(event: SubmitEvent) {
		event.preventDefault();
		if (sending || !api) return;
		problem = '';
		const found = validate(values);
		if (Object.keys(found).length > 0) {
			await showErrors(found);
			return;
		}
		errors = {};
		summary = [];
		const body = claimBody(values);
		sending = true;
		try {
			const claim = await claimOlderOrder(api, keys.for(body), body);
			keys.forget();
			// The request page shows "waiting for the chef" and takes focus.
			await goto(resolve('/account/requests/[id]', { id: claim.id }));
		} catch (e) {
			sending = false;
			await failed(e);
		}
	}

	async function leave() {
		const reached = await signOut();
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
	<title>Add an older order · GK Cake London</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<AccountShell
	active="older"
	title="Add an older order"
	sub={email ? `Signed in as ${email}` : undefined}
	onsignout={api || blocked ? leave : undefined}
>
	{#if signOutProblem}
		<div class="notice error" role="alert"><div><p>{signOutProblem}</p></div></div>
	{:else if blocked}
		<div class="notice error" role="alert" id="old-blocked" tabindex="-1">
			<div><p>{blocked}</p></div>
		</div>
		<div class="acc-actions">
			<a class="button" href="mailto:gk.cake.london@gmail.com">Contact us</a>
			<button class="btn" type="button" onclick={leave}>Sign out</button>
		</div>
	{:else if !api}
		<p role="status" aria-busy="true">Loading…</p>
	{:else}
		<div class="acc-two">
			<section class="acc-card" id="old" aria-labelledby="old-h">
				<h2 class="acc-h" id="old-h">Instagram, email or in-person order</h2>
				{#if summary.length > 0}
					<div class="acc-errsum" id="old-errors" role="alert" tabindex="-1">
						<h3>There’s a problem</h3>
						<ul>
							{#each summary as s (s.field)}
								<li><a href="#{s.id}" onclick={(e) => toField(e, s.id)}>{s.message}</a></li>
							{/each}
						</ul>
					</div>
				{/if}
				{#if problem}
					<div class="notice error" id="old-problem" role="alert" tabindex="-1">
						<div><p>{problem}</p></div>
					</div>
				{/if}
				<p class="small">
					For orders made before this website, or by message or in person. The chef checks her
					records before adding anything.
				</p>
				<form id="old-form" novalidate onsubmit={send}>
					<div class="field">
						<label for="old-ref">Order reference from the chef</label>
						<span class="small quiet" id="old-ref-hint"
							>As written in the chef’s message or receipt, for example IG-0613.</span
						>
						<input
							id="old-ref"
							name="reference"
							type="text"
							autocomplete="off"
							autocapitalize="characters"
							spellcheck="false"
							maxlength={REFERENCE_MAX}
							readonly={sending}
							aria-invalid={errors.reference ? 'true' : undefined}
							aria-describedby={describedBy('reference')}
							bind:value={values.reference}
							oninput={() => edited('reference')}
						/>
					</div>
					<p class="field-error" id="old-ref-error" hidden={!errors.reference}>
						{errors.reference ?? ''}
					</p>

					<fieldset class="field" aria-describedby={describedBy('channel')} disabled={sending}>
						<legend>How did you order?</legend>
						<div class="acc-radios">
							{#each CHANNELS as c (c.value)}
								<label
									><input
										type="radio"
										id="old-how-{c.value}"
										name="channel"
										value={c.value}
										bind:group={values.channel}
										onchange={() => edited('channel')}
									/>
									{c.label}</label
								>
							{/each}
						</div>
					</fieldset>
					<p class="field-error" id="{IDS.channel}-error" hidden={!errors.channel}>
						{errors.channel ?? ''}
					</p>

					<fieldset class="field" disabled={sending}>
						<legend
							>When was the cake for? <span class="quiet">(optional, roughly is fine)</span></legend
						>
						<div class="acc-when">
							<div class="field">
								<label for="old-month">Month</label>
								<select
									id="old-month"
									name="month"
									aria-invalid={errors.month ? 'true' : undefined}
									aria-describedby={describedBy('month')}
									bind:value={values.month}
									onchange={() => edited('month')}
								>
									<option value="">Month</option>
									{#each MONTHS as m, i (m)}
										<option value={String(i + 1)}>{m}</option>
									{/each}
								</select>
							</div>
							<div class="field">
								<label for="old-year">Year</label>
								<select
									id="old-year"
									name="year"
									aria-invalid={errors.year ? 'true' : undefined}
									aria-describedby={describedBy('year')}
									bind:value={values.year}
									onchange={() => edited('year')}
								>
									<option value="">Year</option>
									{#each YEARS as y (y)}
										<option value={y}>{y}</option>
									{/each}
								</select>
							</div>
						</div>
					</fieldset>
					<p class="field-error" id="old-month-error" hidden={!errors.month}>
						{errors.month ?? ''}
					</p>
					<p class="field-error" id="old-year-error" hidden={!errors.year}>
						{errors.year ?? ''}
					</p>

					<div class="field">
						<label for="old-note">A short note for the chef</label>
						<span class="small quiet" id="old-note-hint"
							>For example the cake, the occasion, and the Instagram name or email you used. Please
							don’t include card details or health information.</span
						>
						<textarea
							id="old-note"
							name="note"
							rows="4"
							readonly={sending}
							aria-invalid={errors.note ? 'true' : undefined}
							aria-describedby={describedBy('note')}
							bind:value={values.note}
							oninput={() => edited('note')}></textarea>
					</div>
					<p class="field-error" id="old-note-error" hidden={!errors.note}>
						{errors.note ?? ''}
					</p>

					<div class="acc-actions">
						<button class="btn primary" type="submit" disabled={sending} aria-busy={sending}
							>{#if sending}Sending…{:else}Send to the chef <span class="arrow" aria-hidden="true"
									>→</span
								>{/if}</button
						>
						<a class="button" href={resolve('/account')}>Cancel</a>
					</div>
				</form>
			</section>

			<aside class="acc-card acc-why" aria-labelledby="how-h">
				<h2 class="acc-h" id="how-h">How it works</h2>
				<ol class="acc-steps">
					<li>You send the chef’s reference and a short note.</li>
					<li>The chef checks her records.</li>
					<li>
						If it matches, she adds the order to your account. If she can’t match it, we’ll ask you
						to contact us. Either way, you’ll see the result under “Your requests”.
					</li>
				</ol>
				<p class="small">We don’t show any order details until the chef has added the order.</p>
				<h3 class="acc-h3">Ordered on this website as a guest?</h3>
				<p class="small">
					Adding an order from the private link in its order email isn’t available yet. Until it is,
					ask the chef to add it for you.
				</p>
				<h3 class="acc-h3">Rather ask the chef?</h3>
				<p class="small">
					She can add an order for you. Email
					<a href="mailto:gk.cake.london@gmail.com">gk.cake.london@gmail.com</a> or message
					<a href="https://www.instagram.com/gk.cake.london/" rel="noopener noreferrer"
						>@gk.cake.london</a
					> on Instagram.
				</p>
			</aside>
		</div>
	{/if}
</AccountShell>
