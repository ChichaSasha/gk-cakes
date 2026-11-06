<script lang="ts">
	// Refunds the chef sends from the order (K05; r03b2 refund ledger "By
	// payment" and the GK-1029 "Refund delivery" step): the delivery share
	// that waits for the chef ("Delivery refund · your step"), an unmatched
	// delivery payment returned in full (0035), and an agreed partial refund
	// split over the order's payments. Each goes to the payment provider once
	// and shows as not confirmed until it's confirmed. Chef only.
	import { tick } from 'svelte';
	import { formatMoney } from '../../checkout/format';
	import {
		allocationsFrom,
		deliveryShare,
		noteProblem,
		refundChoice,
		STRAY_REASON,
		strayReturn,
		type MoneyAction,
		type MoneyInput
	} from '../order-money';
	import type { StaffOrderDetail } from '../order-detail';
	import type { StaffView } from '../orders';

	let {
		order,
		view,
		busy,
		acting,
		act
	}: {
		order: StaffOrderDetail;
		view: StaffView;
		busy: boolean;
		acting: MoneyAction | null;
		act: (input: MoneyInput) => Promise<boolean>;
	} = $props();

	type Open = { what: 'delivery' | 'stray'; id: string } | { what: 'part' };
	let open = $state<Open | null>(null);
	let typed = $state<Record<string, string>>({});
	let reason = $state('');
	let reasonError = $state('');
	let checked = $state(false);
	let strayReason = $state(STRAY_REASON);
	let strayError = $state('');

	const cur = $derived(order.currency ?? 'GBP');
	const m = (n: number) => formatMoney(n, cur);
	const part = $derived(refundChoice(order, view));
	const share = $derived(deliveryShare(order, view));
	const strays = $derived(strayReturn(order, view));
	const alloc = $derived(allocationsFrom(part.sources, typed, cur));
	const shown = $derived(checked ? alloc : { ...alloc, fields: {}, total: '' });
	const notes = $derived([...new Set([share.note, strays.note, part.note].filter(Boolean))]);
	const single = $derived(part.sources.length === 1);

	function isOpen(what: 'delivery' | 'stray', id: string) {
		return open !== null && open.what === what && 'id' in open && open.id === id;
	}

	async function focus(id: string) {
		await tick();
		document.getElementById(id)?.focus();
	}

	function openShare(id: string) {
		open = { what: 'delivery', id };
		void focus(`share-send-${id}`);
	}

	function openStray(id: string) {
		open = { what: 'stray', id };
		strayError = '';
		void focus(`stray-reason-${id}`);
	}

	function openPart() {
		open = open?.what === 'part' ? null : { what: 'part' };
		checked = false;
		if (open) void focus(`part-${part.sources[0]?.id}`);
	}

	async function close(back: string) {
		open = null;
		await focus(back);
	}

	async function sendShare(id: string, amountMinor: number) {
		if (await act({ m: 'delivery_refund', collectionId: id, amountMinor })) open = null;
	}

	async function sendStray(id: string, amountMinor: number) {
		strayError = noteProblem(strayReason, 'why it’s returned');
		if (strayError) {
			await focus(`stray-reason-${id}`);
			return;
		}
		const ok = await act({
			m: 'stray',
			allocations: [{ collection_id: id, amount_minor: amountMinor }],
			reason: strayReason
		});
		if (ok) {
			open = null;
			strayReason = STRAY_REASON;
		}
	}

	async function sendPart() {
		checked = true;
		reasonError = noteProblem(reason, 'why you’re refunding');
		const bad = part.sources.find((s) => alloc.fields[s.id]);
		if (bad || alloc.total || reasonError) {
			await focus(
				bad ? `part-${bad.id}` : alloc.total ? `part-${part.sources[0].id}` : 'part-reason'
			);
			return;
		}
		const ok = await act({ m: 'refund', allocations: alloc.allocations, reason });
		if (ok) {
			open = null;
			typed = {};
			reason = '';
			checked = false;
		}
	}

	function onKey(e: KeyboardEvent, back: string) {
		if (e.key === 'Escape') {
			e.preventDefault();
			void close(back);
		}
	}

	function describedBy(id: string) {
		const ids = [`part-help-${id}`];
		if (shown.fields[id]) ids.push(`part-${id}-error`);
		if (shown.total) ids.push('part-total-error');
		return ids.join(' ');
	}
</script>

{#if view === 'chef'}
	{#each share.collections as c (c.id)}
		{@const amount = Math.min(share.pendingMinor, c.roomMinor)}
		{#if isOpen('delivery', c.id)}
			<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
			<div
				class="confirm-row"
				role="group"
				aria-label="Confirm delivery refund"
				onkeydown={(e) => onKey(e, `share-${c.id}`)}
			>
				<p class="small">
					<strong>Send {m(amount)} back to the customer’s original delivery payment?</strong> It’s the
					delivery share owed, sent once. It shows as “not confirmed” until the payment provider confirms.
				</p>
				<button
					id="share-send-{c.id}"
					class="btn btn-primary"
					type="button"
					disabled={busy}
					onclick={() => void sendShare(c.id, amount)}
					>{acting === 'delivery_refund' ? 'Sending…' : `Send refund ${m(amount)}`}</button
				>
				<button class="btn btn-quiet" type="button" onclick={() => void close(`share-${c.id}`)}
					>Not yet</button
				>
			</div>
		{:else}
			<div class="actions">
				<button
					id="share-{c.id}"
					class="btn btn-primary"
					type="button"
					disabled={busy}
					onclick={() => openShare(c.id)}>Refund delivery · {m(amount)}</button
				>
			</div>
		{/if}
	{/each}

	{#each strays.collections as c (c.id)}
		{#if isOpen('stray', c.id)}
			<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
			<form
				class="decide-box"
				aria-label="Return the delivery payment"
				novalidate
				onkeydown={(e) => onKey(e, `stray-${c.id}`)}
				onsubmit={(e) => {
					e.preventDefault();
					void sendStray(c.id, c.amountMinor);
				}}
			>
				<p class="small">
					<strong>Return the {m(c.amountMinor)} delivery payment that couldn’t be used?</strong> It goes
					back in full to the payment it came from, once, and doesn’t count against what the cake’s refund
					rules return. It shows as “not confirmed” until the payment provider confirms.
				</p>
				<label class="field" for="stray-reason-{c.id}"
					>Reason (kept with the refund)
					<input
						id="stray-reason-{c.id}"
						autocomplete="off"
						maxlength="500"
						bind:value={strayReason}
						oninput={() => (strayError = '')}
						aria-invalid={strayError ? 'true' : undefined}
						aria-describedby={strayError ? `stray-reason-${c.id}-error` : undefined}
					/>
				</label>
				{#if strayError}<p id="stray-reason-{c.id}-error" class="small bad-text">
						{strayError}
					</p>{/if}
				<div class="actions">
					<button class="btn btn-primary" type="submit" disabled={busy}
						>{acting === 'stray' ? 'Returning…' : `Return ${m(c.amountMinor)}`}</button
					>
					<button class="btn btn-quiet" type="button" onclick={() => void close(`stray-${c.id}`)}
						>Not yet</button
					>
				</div>
			</form>
		{:else}
			<div class="actions">
				<button
					id="stray-{c.id}"
					class="btn btn-primary"
					type="button"
					disabled={busy}
					onclick={() => openStray(c.id)}
					>Return the unmatched delivery payment · {m(c.amountMinor)}…</button
				>
			</div>
		{/if}
	{/each}

	{#if part.can}
		<div class="actions">
			<button
				id="part-open"
				class="btn btn-quiet"
				type="button"
				aria-expanded={open?.what === 'part'}
				aria-controls="part-panel"
				disabled={busy}
				onclick={openPart}>Refund part of the payment…</button
			>
		</div>
		{#if open?.what === 'part'}
			<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
			<form
				id="part-panel"
				class="decide-box"
				aria-label="Refund part of the payment"
				novalidate
				onkeydown={(e) => onKey(e, 'part-open')}
				onsubmit={(e) => {
					e.preventDefault();
					void sendPart();
				}}
			>
				<p class="small">
					An agreed refund on top of anything already owed (for example goodwill you agreed with the
					customer). It goes back to the payment you choose, once, and shows as “not confirmed”
					until the payment provider confirms. Up to {m(part.limitMinor)} can still come back.
				</p>
				{#each part.sources as s (s.id)}
					<label class="field" for="part-{s.id}"
						>{single ? 'Amount to refund' : `From the ${s.label.toLowerCase()}`}
						<input
							id="part-{s.id}"
							inputmode="decimal"
							autocomplete="off"
							placeholder="e.g. 20 or 20.50"
							bind:value={typed[s.id]}
							oninput={() => (checked = false)}
							aria-invalid={shown.fields[s.id] || shown.total ? 'true' : undefined}
							aria-describedby={describedBy(s.id)}
						/>
					</label>
					<p id="part-help-{s.id}" class="small muted">
						{s.label}: {m(s.paidMinor)} paid · up to {m(s.roomMinor)} can still come back.
					</p>
					{#if shown.fields[s.id]}<p id="part-{s.id}-error" class="small bad-text">
							{shown.fields[s.id]}
						</p>{/if}
				{/each}
				{#if shown.total}<p id="part-total-error" class="small bad-text">{shown.total}</p>{/if}
				<label class="field" for="part-reason"
					>Why (kept with the refund)
					<input
						id="part-reason"
						autocomplete="off"
						maxlength="500"
						placeholder="e.g. Goodwill: the decoration wasn’t as agreed"
						bind:value={reason}
						oninput={() => (reasonError = '')}
						aria-invalid={reasonError ? 'true' : undefined}
						aria-describedby={reasonError ? 'part-reason-error' : undefined}
					/>
				</label>
				{#if reasonError}<p id="part-reason-error" class="small bad-text">{reasonError}</p>{/if}
				<div class="actions">
					<button class="btn btn-primary" type="submit" disabled={busy}
						>{acting === 'refund'
							? 'Sending…'
							: alloc.totalMinor
								? `Send refund · ${m(alloc.totalMinor)}`
								: 'Send refund'}</button
					>
					<button class="btn btn-quiet" type="button" onclick={() => void close('part-open')}
						>Not now</button
					>
				</div>
			</form>
		{/if}
	{/if}
	{#each notes as note (note)}<p class="role-note">{note}</p>{/each}
{/if}
