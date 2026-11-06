<script lang="ts">
	// Needs a manager (r03b2: the money cases of Needs attention, on the
	// order they belong to; GK-1050, 1083, 1084, 1087): each case with the
	// decision the API offers for it — match or accept an outside refund,
	// review a changed refund result, reconcile a payment mismatch, decide
	// an unknown refund past its window, reserve a failed remainder again or
	// arrange a verified transfer (K05 Q5: preview, reserve, then record what
	// happened, with evidence references only). The chef decides; technical
	// support sees the same cases without amounts or buttons. No decision
	// calls the payment provider.
	import { tick } from 'svelte';
	import { formatMoney } from '../../checkout/format';
	import type { StaffApi } from '../catalogue';
	import {
		managerCases,
		moneyRefusal,
		noteProblem,
		OUTCOME_LABEL,
		outcomeNeeds,
		parseAmount,
		PAYOUT_STATE,
		payoutOutcomes,
		previewPayout,
		referenceProblem,
		refundIdProblem,
		TOOL_LABEL,
		type ManagerCase,
		type MoneyAction,
		type MoneyInput,
		type Payout,
		type PayoutOutcome,
		type PayoutPreview,
		type Tool,
		collectionLabel
	} from '../order-money';
	import { type StaffOrderDetail, when } from '../order-detail';
	import type { StaffView } from '../orders';

	let {
		api,
		order,
		view,
		now,
		busy,
		acting,
		payouts,
		payoutsError,
		act,
		onsession
	}: {
		api: StaffApi;
		order: StaffOrderDetail;
		view: StaffView;
		now: Date;
		busy: boolean;
		acting: MoneyAction | null;
		payouts: Payout[];
		payoutsError: string;
		act: (input: MoneyInput) => Promise<boolean>;
		/** A sign-in or access problem from the preview (the page handles it). */
		onsession: (e: unknown) => Promise<boolean>;
	} = $props();

	const cases = $derived(managerCases(order, view));
	const chef = $derived(view === 'chef');
	const cur = $derived(order.currency ?? 'GBP');
	const m = (n: number) => formatMoney(n, cur);

	// One form open at a time: the case key and the tool, or a payout outcome.
	let open = $state<
		{ key: string; tool: Tool } | { payout: string; outcome: PayoutOutcome } | null
	>(null);
	let choice = $state('');
	let candidate = $state('');
	let note = $state('');
	let refundId = $state('');
	let unavailable = $state('');
	let recipient = $state('');
	let transfer = $state('');
	let evidence = $state('');
	let amount = $state('');
	let errors = $state<Record<string, string>>({});
	let preview = $state<PayoutPreview | null>(null);
	let previewing = $state(false);
	let previewError = $state('');

	function isOpen(c: ManagerCase, tool: Tool) {
		return open !== null && 'key' in open && open.key === c.key && open.tool === tool;
	}

	async function focus(id: string) {
		await tick();
		document.getElementById(id)?.focus();
	}

	function reset() {
		choice = '';
		candidate = '';
		note = '';
		refundId = '';
		unavailable = '';
		recipient = '';
		transfer = '';
		evidence = '';
		amount = '';
		errors = {};
		preview = null;
		previewError = '';
	}

	function start(c: ManagerCase, tool: Tool) {
		const same = isOpen(c, tool);
		reset();
		open = same ? null : { key: c.key, tool };
		if (tool === 'match_or_explain' && c.candidates?.length) candidate = c.candidates[0].id;
		if (!same) void focus(`${c.key}-${tool}-first`);
	}

	async function close(back: string) {
		open = null;
		reset();
		await focus(back);
	}

	function onKey(e: KeyboardEvent, back: string) {
		if (e.key === 'Escape') {
			e.preventDefault();
			void close(back);
		}
	}

	/** Shows the first problem's field. */
	async function invalid(keys: string[]) {
		const first = keys.find((k) => errors[k]);
		if (first) await focus(first);
		return Boolean(first);
	}

	const err = (id: string) => errors[id] ?? '';
	const desc = (id: string, help = '') =>
		[help, err(id) ? `${id}-error` : ''].filter(Boolean).join(' ') || undefined;

	async function submitCase(c: ManagerCase, tool: Tool) {
		const p = c.key;
		const e: Record<string, string> = {};
		if (tool !== 're_reserve') e[`${p}-note`] = noteProblem(note, 'what you checked');
		else if (note.trim()) e[`${p}-note`] = noteProblem(note, 'what you checked');
		if ((tool === 'match_or_explain' || tool === 'review' || tool === 'decide') && !choice) {
			e[`${p}-${tool}-first`] = 'Choose one.';
		}
		if (tool === 'match_or_explain' && choice === 'match' && !candidate) {
			e[`${p}-candidate`] = 'Choose the refund it matches.';
		}
		if (tool === 'decide' && choice === 'succeeded')
			e[`${p}-refund-id`] = refundIdProblem(refundId);
		errors = e;
		if (await invalid([`${p}-${tool}-first`, `${p}-candidate`, `${p}-refund-id`, `${p}-note`]))
			return;
		let input: MoneyInput;
		switch (tool) {
			case 'match_or_explain':
				input =
					choice === 'match'
						? {
								m: 'resolve',
								observationId: c.observationId ?? '',
								resolution: 'match',
								operationId: candidate,
								note
							}
						: {
								m: 'resolve',
								observationId: c.observationId ?? '',
								resolution: 'accept_external',
								note
							};
				break;
			case 'review':
				input = {
					m: 'clear_refund',
					operationId: c.operationId ?? '',
					decision: choice as 'keep' | 'failed' | 'succeeded',
					note
				};
				break;
			case 'reconcile':
				input = { m: 'clear_payment', collectionId: c.collectionId, note };
				break;
			case 'decide':
				input = {
					m: 'decide',
					operationId: c.operationId ?? '',
					decision: choice as 'failed' | 'succeeded',
					refundId,
					note
				};
				break;
			case 're_reserve':
				input = {
					m: 're_reserve',
					operationId: c.operationId ?? '',
					collectionId: c.collectionId,
					note
				};
				break;
			default:
				return;
		}
		if (await act(input)) {
			open = null;
			reset();
		}
	}

	async function previewTransfer(c: ManagerCase) {
		const p = c.key;
		errors = {
			[`${p}-transfer-first`]: referenceProblem(
				unavailable,
				'where the proof that the provider can’t return it is kept'
			),
			[`${p}-recipient`]: referenceProblem(
				recipient,
				'where the customer’s confirmed transfer details are kept'
			)
		};
		if (await invalid([`${p}-transfer-first`, `${p}-recipient`])) return;
		previewing = true;
		previewError = '';
		try {
			preview = await previewPayout(
				api,
				order,
				{ collectionId: c.collectionId, unavailable, recipient },
				`chef-payout-preview-${order.id}-v${order.version}-${crypto.randomUUID().slice(0, 8)}`
			);
			await focus(`${p}-preview-h`);
		} catch (e) {
			if (await onsession(e)) return;
			previewError = moneyRefusal(e, 'preview').text;
			await focus(`${p}-preview-error`);
		} finally {
			previewing = false;
		}
	}

	async function reserve() {
		if (!preview) return;
		if (await act({ m: 'payout', preview })) {
			open = null;
			reset();
		}
	}

	const previewExpired = $derived(preview ? now >= new Date(preview.expires_at) : false);
	const previewOld = $derived(preview ? preview.order_version !== order.version : false);

	function startOutcome(p: Payout, outcome: PayoutOutcome) {
		const same =
			open !== null &&
			'payout' in open &&
			open.payout === p.refund_operation_id &&
			open.outcome === outcome;
		reset();
		if (outcome === 'submitted' || outcome === 'unknown') {
			open = same ? null : { payout: p.refund_operation_id, outcome };
			if (!same) void focus(`po-${p.refund_operation_id}-confirm`);
			return;
		}
		open = same ? null : { payout: p.refund_operation_id, outcome };
		if (!same)
			void focus(
				`po-${p.refund_operation_id}-${outcomeNeeds(outcome).transfer ? 'transfer' : 'evidence'}`
			);
	}

	function outcomeOpen(p: Payout): PayoutOutcome | null {
		return open !== null && 'payout' in open && open.payout === p.refund_operation_id
			? open.outcome
			: null;
	}

	async function submitOutcome(p: Payout, outcome: PayoutOutcome) {
		const id = `po-${p.refund_operation_id}`;
		const need = outcomeNeeds(outcome);
		const e: Record<string, string> = {};
		if (need.transfer) {
			e[`${id}-transfer`] = referenceProblem(transfer, 'the transfer’s reference');
			const a = parseAmount(amount, 'the amount you transferred');
			e[`${id}-amount`] =
				'error' in a
					? a.error
					: a.minor !== p.amount_minor
						? `That isn’t the reserved ${formatMoney(p.amount_minor, p.currency)}. Only exactly the reserved amount can be recorded as arrived; if another amount was sent, record it as failed and arrange it again.`
						: '';
		}
		if (need.evidence)
			e[`${id}-evidence`] = referenceProblem(evidence, 'where the evidence is kept');
		errors = e;
		if (await invalid([`${id}-transfer`, `${id}-amount`, `${id}-evidence`])) return;
		const a = parseAmount(amount);
		const ok = await act({
			m: 'payout_outcome',
			operationId: p.refund_operation_id,
			outcome,
			transferIdentity: transfer,
			evidence,
			amountMinor: 'minor' in a ? a.minor : undefined
		});
		if (ok) {
			open = null;
			reset();
		}
	}

	const REVIEW_CHOICES: [string, string][] = [
		['keep', 'Keep the recorded result: it’s right'],
		[
			'failed',
			'Record it as failed: nothing was returned (what it would return is owed again; nothing is resent)'
		],
		['succeeded', 'Record it as confirmed: the provider shows the same refund and amount succeeded']
	];
</script>

{#each cases as c (c.key)}
	<article class="m-case" aria-labelledby="{c.key}-h">
		<h3 id="{c.key}-h" class="m-case-h">{c.title}</h3>
		{#each c.lines as line, i (i)}<p class="small">{line}</p>{/each}
		{#if c.tools.length}
			<div class="actions">
				{#each c.tools as tool (tool)}
					<button
						id="{c.key}-{tool}"
						class="btn btn-small"
						type="button"
						aria-expanded={isOpen(c, tool)}
						aria-controls="{c.key}-{tool}-panel"
						disabled={busy}
						onclick={() => start(c, tool)}
						>{tool === 're_reserve' && c.remainderMinor
							? `Reserve ${m(c.remainderMinor)} again…`
							: TOOL_LABEL[tool]}</button
					>
				{/each}
			</div>
		{/if}
		{#each c.tools as tool (tool)}
			{#if isOpen(c, tool)}
				{@const p = c.key}
				{#if tool === 'transfer'}
					<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
					<div
						id="{p}-{tool}-panel"
						class="decide-box"
						role="group"
						aria-label="Arrange a verified transfer"
						onkeydown={(e) => onKey(e, `${p}-${tool}`)}
					>
						{#if !preview}
							<form
								novalidate
								onsubmit={(e) => {
									e.preventDefault();
									void previewTransfer(c);
								}}
							>
								<p class="small">
									When the payment provider can’t return it at all, you pay it back by bank transfer
									with proof (K05 Q5): the same amount, never as well as a refund. Enter only
									<strong>references</strong> to where the evidence is kept (for example “Drive: GK refunds
									/ Noor, card closed”), never bank details.
								</p>
								<label class="field" for="{p}-transfer-first"
									>Proof the provider can’t return it (reference)
									<input
										id="{p}-transfer-first"
										autocomplete="off"
										maxlength="200"
										bind:value={unavailable}
										oninput={() => (errors = { ...errors, [`${p}-transfer-first`]: '' })}
										aria-invalid={err(`${p}-transfer-first`) ? 'true' : undefined}
										aria-describedby={desc(`${p}-transfer-first`, `${p}-ref-help`)}
									/>
								</label>
								{#if err(`${p}-transfer-first`)}<p
										id="{p}-transfer-first-error"
										class="small bad-text"
									>
										{err(`${p}-transfer-first`)}
									</p>{/if}
								<label class="field" for="{p}-recipient"
									>The customer’s confirmed transfer details (reference)
									<input
										id="{p}-recipient"
										autocomplete="off"
										maxlength="200"
										bind:value={recipient}
										oninput={() => (errors = { ...errors, [`${p}-recipient`]: '' })}
										aria-invalid={err(`${p}-recipient`) ? 'true' : undefined}
										aria-describedby={desc(`${p}-recipient`, `${p}-ref-help`)}
									/>
								</label>
								{#if err(`${p}-recipient`)}<p id="{p}-recipient-error" class="small bad-text">
										{err(`${p}-recipient`)}
									</p>{/if}
								<p id="{p}-ref-help" class="small muted">
									Up to 200 plain characters. Six or more digits in a row once spaces, hyphens and
									dots are removed (so also a date like 2026-10-31), or anything like an IBAN, is
									refused.
								</p>
								<div class="actions">
									<button class="btn btn-primary" type="submit" disabled={busy || previewing}
										>{previewing ? 'Working out…' : 'Preview the transfer'}</button
									>
									<button
										class="btn btn-quiet"
										type="button"
										onclick={() => void close(`${p}-${tool}`)}>Not now</button
									>
								</div>
								<div role="alert" class="step-error-wrap">
									{#if previewError}<p id="{p}-preview-error" class="small bad-text" tabindex="-1">
											{previewError}
										</p>{/if}
								</div>
							</form>
						{:else}
							<p id="{p}-preview-h" class="strong" tabindex="-1">
								Reserve {formatMoney(preview.amount_minor, preview.currency)} for a verified transfer?
							</p>
							<dl class="facts small">
								<div>
									<dt>Amount</dt>
									<dd>
										{formatMoney(preview.amount_minor, preview.currency)} · the whole remaining amount
									</dd>
								</div>
								<div>
									<dt>From</dt>
									<dd>the {collectionLabel(order, preview.collection_id)}</dd>
								</div>
								<div>
									<dt>Proof</dt>
									<dd class="wrap">{preview.unavailable_evidence_ref}</dd>
								</div>
								<div>
									<dt>Transfer details</dt>
									<dd class="wrap">{preview.recipient_evidence_ref}</dd>
								</div>
								<div>
									<dt>Preview valid until</dt>
									<dd>{when(preview.expires_at)}</dd>
								</div>
							</dl>
							<p class="small">
								Reserving blocks any other refund or transfer for this payment. Once it’s secured
								you get the transfer details and a payment reference; then make the transfer and
								record what happened here.
							</p>
							{#if previewExpired || previewOld}
								<p class="small warn-text" role="status">
									{previewOld
										? 'The order changed since this preview. Preview the transfer again.'
										: 'This preview expired (they last 10 minutes). Preview the transfer again.'}
								</p>
							{/if}
							<div class="actions">
								<button
									class="btn btn-primary"
									type="button"
									disabled={busy || previewExpired || previewOld}
									onclick={() => void reserve()}
									>{acting === 'payout'
										? 'Reserving…'
										: `Reserve ${formatMoney(preview.amount_minor, preview.currency)}`}</button
								>
								<button
									class="btn btn-quiet"
									type="button"
									disabled={busy}
									onclick={() => {
										preview = null;
										void focus(`${p}-transfer-first`);
									}}>Change the references</button
								>
							</div>
						{/if}
					</div>
				{:else}
					<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
					<form
						id="{p}-{tool}-panel"
						class="decide-box"
						aria-label={TOOL_LABEL[tool].replace('…', '')}
						novalidate
						onkeydown={(e) => onKey(e, `${p}-${tool}`)}
						onsubmit={(e) => {
							e.preventDefault();
							void submitCase(c, tool);
						}}
					>
						{#if tool === 'match_or_explain'}
							<fieldset
								class="choice"
								aria-describedby={err(`${p}-${tool}-first`)
									? `${p}-${tool}-first-error`
									: undefined}
							>
								<legend id="{p}-{tool}-first" tabindex="-1">What was it?</legend>
								{#if c.candidates?.length}
									<label
										><input type="radio" name="{p}-res" value="match" bind:group={choice} /> A refund
										we sent from here: match it to that refund (the provider’s records must prove it’s
										the same one)</label
									>
								{/if}
								<label
									><input type="radio" name="{p}-res" value="accept_external" bind:group={choice} /> Made
									outside GK Cakes (for example in the dashboard): accept it, counted as returned by its
									own status; nothing new is sent</label
								>
							</fieldset>
							{#if err(`${p}-${tool}-first`)}<p id="{p}-{tool}-first-error" class="small bad-text">
									{err(`${p}-${tool}-first`)}
								</p>{/if}
							{#if choice === 'match' && c.candidates?.length}
								<label class="field" for="{p}-candidate"
									>The refund it is
									<select
										id="{p}-candidate"
										bind:value={candidate}
										aria-invalid={err(`${p}-candidate`) ? 'true' : undefined}
										aria-describedby={desc(`${p}-candidate`)}
									>
										{#each c.candidates as k (k.id)}<option value={k.id}>{k.label}</option>{/each}
									</select>
								</label>
								{#if err(`${p}-candidate`)}<p id="{p}-candidate-error" class="small bad-text">
										{err(`${p}-candidate`)}
									</p>{/if}
							{/if}
						{:else if tool === 'review'}
							<fieldset
								class="choice"
								aria-describedby={err(`${p}-${tool}-first`)
									? `${p}-${tool}-first-error`
									: undefined}
							>
								<legend id="{p}-{tool}-first" tabindex="-1"
									>After checking the provider’s records</legend
								>
								{#each REVIEW_CHOICES as [v, label] (v)}
									<label
										><input type="radio" name="{p}-rev" value={v} bind:group={choice} />
										{label}</label
									>
								{/each}
							</fieldset>
							{#if err(`${p}-${tool}-first`)}<p id="{p}-{tool}-first-error" class="small bad-text">
									{err(`${p}-${tool}-first`)}
								</p>{/if}
							<p class="small muted">
								The system accepts “failed” or “confirmed” only when the provider’s own record of
								this refund shows it.
							</p>
						{:else if tool === 'decide'}
							<fieldset
								class="choice"
								aria-describedby={err(`${p}-${tool}-first`)
									? `${p}-${tool}-first-error`
									: undefined}
							>
								<legend id="{p}-{tool}-first" tabindex="-1">What happened to the refund?</legend>
								<label
									><input type="radio" name="{p}-dec" value="failed" bind:group={choice} /> It was never
									made: what it would return is owed again (accepted only when a complete check with the
									provider after the retry window found nothing)</label
								>
								<label
									><input type="radio" name="{p}-dec" value="succeeded" bind:group={choice} /> It went
									through: give the provider’s refund ID, which is checked with them</label
								>
							</fieldset>
							{#if err(`${p}-${tool}-first`)}<p id="{p}-{tool}-first-error" class="small bad-text">
									{err(`${p}-${tool}-first`)}
								</p>{/if}
							{#if choice === 'succeeded'}
								<label class="field" for="{p}-refund-id"
									>The payment provider’s refund ID
									<input
										id="{p}-refund-id"
										autocomplete="off"
										placeholder="re_…"
										bind:value={refundId}
										oninput={() => (errors = { ...errors, [`${p}-refund-id`]: '' })}
										aria-invalid={err(`${p}-refund-id`) ? 'true' : undefined}
										aria-describedby={desc(`${p}-refund-id`)}
									/>
								</label>
								{#if err(`${p}-refund-id`)}<p id="{p}-refund-id-error" class="small bad-text">
										{err(`${p}-refund-id`)}
									</p>{/if}
							{/if}
						{:else if tool === 'reconcile'}
							<p id="{p}-{tool}-first" class="small" tabindex="-1">
								Clear the mismatch once you’ve checked the provider’s records: the recorded result ({order.collections?.find(
									(x) => x.id === c.collectionId
								)?.state === 'failed'
									? 'failed'
									: 'paid'}) stands. Nothing is charged, refunded or confirmed by this.
							</p>
						{:else if tool === 're_reserve'}
							<p id="{p}-{tool}-first" class="small" tabindex="-1">
								Reserve {c.remainderMinor ? m(c.remainderMinor) : 'the remaining amount'} again on the
								{collectionLabel(order, c.collectionId)} for one new attempt through the payment provider.
								It’s never resent automatically after this. If the provider can’t return it at all, arrange
								a verified transfer instead (never both).
							</p>
						{/if}
						<label class="field" for="{p}-note"
							>{tool === 're_reserve'
								? 'Note (optional)'
								: 'What you checked (kept with the decision)'}
							<input
								id="{p}-note"
								autocomplete="off"
								maxlength="500"
								bind:value={note}
								oninput={() => (errors = { ...errors, [`${p}-note`]: '' })}
								aria-invalid={err(`${p}-note`) ? 'true' : undefined}
								aria-describedby={desc(`${p}-note`)}
							/>
						</label>
						{#if err(`${p}-note`)}<p id="{p}-note-error" class="small bad-text">
								{err(`${p}-note`)}
							</p>{/if}
						<div class="actions">
							<button class="btn btn-primary" type="submit" disabled={busy}
								>{acting
									? 'Recording…'
									: tool === 'match_or_explain'
										? choice === 'match'
											? 'Match it'
											: 'Record it'
										: tool === 'review'
											? 'Record the review'
											: tool === 'reconcile'
												? 'Clear the mismatch'
												: tool === 'decide'
													? 'Record the result'
													: c.remainderMinor
														? `Reserve ${m(c.remainderMinor)} again`
														: 'Reserve it again'}</button
							>
							<button class="btn btn-quiet" type="button" onclick={() => void close(`${p}-${tool}`)}
								>Not now</button
							>
						</div>
					</form>
				{/if}
			{/if}
		{/each}
		{#if c.note}<p class="role-note">{c.note}</p>{/if}
	</article>
{/each}

{#if chef && (payouts.length || payoutsError)}
	<h3 class="sub-h">Verified transfers</h3>
	{#if payoutsError}<p class="small bad-text">{payoutsError}</p>{/if}
	<ul class="parts">
		{#each payouts as p (p.refund_operation_id)}
			{@const id = `po-${p.refund_operation_id}`}
			{@const opened = outcomeOpen(p)}
			<li>
				<p class="strong">
					{formatMoney(p.amount_minor, p.currency)} · {PAYOUT_STATE[p.state] ??
						p.state}{p.outcome_pending ? ' · an outcome is being recorded' : ''}
				</p>
				{#if p.instructions}
					<dl class="part-facts small">
						<div>
							<dt>Transfer</dt>
							<dd>{formatMoney(p.instructions.amount_minor, p.instructions.currency)}</dd>
						</div>
						<div>
							<dt>To (details kept at)</dt>
							<dd class="wrap">{p.instructions.recipient_evidence_ref}</dd>
						</div>
						<div>
							<dt>Payment reference</dt>
							<dd class="mono wrap">{p.instructions.payment_reference}</dd>
						</div>
					</dl>
				{/if}
				{#if p.transfer_identity}<p class="small muted">
						Transfer reference: {p.transfer_identity}
					</p>{/if}
				{#if p.decided_at}<p class="small muted">Recorded {when(p.decided_at)}</p>{/if}
				{#if payoutOutcomes(p).length}
					<div class="actions">
						{#each payoutOutcomes(p) as outcome (outcome)}
							<button
								id="{id}-{outcome}"
								class="btn btn-small"
								type="button"
								aria-expanded={opened === outcome}
								disabled={busy}
								onclick={() => startOutcome(p, outcome)}>{OUTCOME_LABEL[outcome]}</button
							>
						{/each}
					</div>
				{/if}
				{#if opened}
					{@const need = outcomeNeeds(opened)}
					<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
					<form
						class="decide-box"
						aria-label="Record the transfer’s outcome"
						novalidate
						onkeydown={(e) => onKey(e, `${id}-${opened}`)}
						onsubmit={(e) => {
							e.preventDefault();
							void submitOutcome(p, opened);
						}}
					>
						<p class="small">
							{opened === 'submitted'
								? `Record that you sent the ${formatMoney(p.amount_minor, p.currency)} transfer with the payment reference above. Record the result once it arrives or fails.`
								: opened === 'unknown'
									? 'Record that the transfer’s result is unknown. Nothing else can be paid back on this payment, by any route, until it’s recorded as arrived or failed.'
									: opened === 'succeeded'
										? 'Record that the transfer arrived: its reference (unique to this transfer), where the proof is kept, and exactly the reserved amount.'
										: opened === 'failed'
											? 'Record that the transfer failed (with where the proof is kept): the amount is owed again, and another verified transfer may follow.'
											: 'Withdraw the reservation because nothing was paid (with where the proof is kept): the amount is owed again.'}
						</p>
						{#if need.transfer}
							<label class="field" for="{id}-transfer"
								>The transfer’s reference
								<input
									id="{id}-transfer"
									autocomplete="off"
									maxlength="200"
									bind:value={transfer}
									oninput={() => (errors = { ...errors, [`${id}-transfer`]: '' })}
									aria-invalid={err(`${id}-transfer`) ? 'true' : undefined}
									aria-describedby={desc(`${id}-transfer`)}
								/>
							</label>
							{#if err(`${id}-transfer`)}<p id="{id}-transfer-error" class="small bad-text">
									{err(`${id}-transfer`)}
								</p>{/if}
							<label class="field" for="{id}-amount"
								>Amount transferred
								<input
									id="{id}-amount"
									inputmode="decimal"
									autocomplete="off"
									placeholder={formatMoney(p.amount_minor, p.currency)}
									bind:value={amount}
									oninput={() => (errors = { ...errors, [`${id}-amount`]: '' })}
									aria-invalid={err(`${id}-amount`) ? 'true' : undefined}
									aria-describedby={desc(`${id}-amount`)}
								/>
							</label>
							{#if err(`${id}-amount`)}<p id="{id}-amount-error" class="small bad-text">
									{err(`${id}-amount`)}
								</p>{/if}
						{/if}
						{#if need.evidence}
							<label class="field" for="{id}-evidence"
								>Where the proof is kept (reference)
								<input
									id="{id}-evidence"
									autocomplete="off"
									maxlength="200"
									bind:value={evidence}
									oninput={() => (errors = { ...errors, [`${id}-evidence`]: '' })}
									aria-invalid={err(`${id}-evidence`) ? 'true' : undefined}
									aria-describedby={desc(`${id}-evidence`)}
								/>
							</label>
							{#if err(`${id}-evidence`)}<p id="{id}-evidence-error" class="small bad-text">
									{err(`${id}-evidence`)}
								</p>{/if}
						{/if}
						<div class="actions">
							<button
								id="{id}-confirm"
								class="btn {opened === 'withdrawn' || opened === 'failed'
									? 'btn-danger'
									: 'btn-primary'}"
								type="submit"
								disabled={busy}
								>{acting === 'payout_outcome'
									? 'Recording…'
									: OUTCOME_LABEL[opened].replace('…', '')}</button
							>
							<button
								class="btn btn-quiet"
								type="button"
								onclick={() => void close(`${id}-${opened}`)}>Not now</button
							>
						</div>
					</form>
				{/if}
			</li>
		{/each}
	</ul>
{/if}
{#if !chef && cases.length}
	<p class="role-note">
		Money decisions stay with Elina. Technical support can look but not change them.
	</p>
{/if}
