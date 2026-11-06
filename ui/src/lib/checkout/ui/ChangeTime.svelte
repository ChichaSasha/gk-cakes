<script lang="ts">
	// "Change your time" (GK-017 r02b state 18) for collection and own-courier
	// orders: the same day only, until 6 hours before the current time (the
	// API's move choices and cutoff; equality is still allowed). The list
	// shows the day's start–end times with the current one marked; a refused
	// choice keeps the old booking and shows fresh choices. Moving never
	// changes the refund deadline.
	import { tick } from 'svelte';
	import { ApiError } from '../../api';
	import {
		moveIntro,
		moveOutcome,
		moveProblemFor,
		moveView,
		shortDate,
		timeChange,
		type MoveView
	} from '../changes';
	import type { OrderChangesApi } from '../client';
	import type { ActionKeys, Who } from '../flow';
	import { timeRange } from '../format';
	import type { Order } from '../types';
	import TimeList from './TimeList.svelte';

	interface Props {
		order: Order;
		who: Who;
		api: OrderChangesApi;
		keys: ActionKeys;
		/** A move was accepted (202) or the order changed: read the order again (and poll). */
		onchanged: () => void;
	}

	let { order, who, api, keys, onchanged }: Props = $props();

	let open = $state(false);
	let loading = $state(false);
	let loadFailed = $state(false);
	let view = $state<MoveView | null>(null);
	let chosen = $state('');
	let status = $state('');
	let problem = $state('');
	let busy = $state(false);
	/** This page asked for a move and waits for its outcome. */
	let awaiting = $state(false);

	const mode = $derived(timeChange(order, who));
	const moving = $derived(mode === 'moving' ? order.appointment_change : null);

	// The outcome of this page's own move, once the API reports it.
	$effect(() => {
		if (!awaiting) return;
		const outcome = moveOutcome(order);
		if (!outcome) return;
		awaiting = false;
		if (outcome.ok) {
			status = outcome.message;
			problem = '';
			open = false;
			view = null;
		} else {
			problem = outcome.message;
			status = '';
			const gone =
				order.appointment_change?.reason === 'slot_unavailable'
					? order.appointment_change.to.id
					: undefined;
			void load(gone);
		}
	});

	async function load(gone?: string) {
		open = true;
		loading = true;
		loadFailed = false;
		try {
			const c = await api.moveChoices(order.id, who);
			view = moveView(c, gone);
			chosen = view.currentId;
		} catch {
			view = null;
			loadFailed = true;
		}
		loading = false;
	}

	async function toggle() {
		if (open) {
			open = false;
			return;
		}
		status = '';
		problem = '';
		await load();
		await tick();
		document.getElementById('move-times')?.focus();
	}

	async function move() {
		if (!view || busy) return;
		problem = '';
		if (chosen === view.currentId || !chosen) {
			status = 'That’s your current time. Choose a different time to move.';
			return;
		}
		const target = view.options.find((o) => o.id === chosen);
		busy = true;
		status = '';
		try {
			const res = await api.movePickup(
				order.id,
				{ target_slot_id: chosen, expected_version: order.version },
				keys.for(`move:${order.id}:${order.version}:${chosen}`).key,
				who
			);
			if (res.state === 'unchanged') {
				status = 'That’s already your time.';
			} else {
				awaiting = true;
				status = target ? `Moving your time to ${target.label}…` : 'Moving your time…';
				open = false;
				view = null;
			}
			onchanged();
		} catch (e) {
			const p = moveProblemFor(e);
			problem = p.message;
			if (p.next === 'reload') await load(p.gone ? chosen : undefined);
			else if (p.next === 'refresh') {
				onchanged();
				await load();
			} else if (e instanceof ApiError && e.status === 401) open = false;
		}
		busy = false;
	}
</script>

{#if moving}
	<p class="notice" role="status">
		Moving your time to {timeRange(moving.to.starts_at, moving.to.ends_at)} on {shortDate(
			moving.to.date
		)}… This page updates by itself.
	</p>
{:else}
	<p class="small">{view?.intro ?? moveIntro(order.pickup_move_until)}</p>
	<div class="oc-actions">
		<button
			type="button"
			aria-expanded={open}
			aria-controls="move-panel"
			onclick={() => void toggle()}>Change your time</button
		>
	</div>
	<div id="move-panel" hidden={!open}>
		{#if loading}
			<p class="small" role="status">Loading available times…</p>
		{:else if loadFailed}
			<div class="notice error">
				<p>We couldn’t load the times just now. Nothing was changed.</p>
			</div>
			<div class="oc-actions">
				<button type="button" onclick={() => void load()}>Try again</button>
			</div>
		{:else if view}
			<div>
				<TimeList
					id="move-times"
					legend={view.legend}
					name="move"
					options={view.options}
					bind:value={chosen}
					disabled={view.status !== 'open'}
					describedby={view.note ? 'move-note' : undefined}
				/>
				{#if view.note}<p class="small" id="move-note">{view.note}</p>{/if}
				{#if view.canMove}
					<div class="oc-actions">
						<button
							type="button"
							onclick={() => void move()}
							aria-disabled={busy ? 'true' : undefined}
							aria-busy={busy}>{busy ? 'Moving your time…' : 'Move to the selected time'}</button
						>
					</div>
				{/if}
				<p class="small">Moving your time doesn’t change your refund deadline.</p>
			</div>
		{/if}
	</div>
{/if}
{#if problem}
	<p class="co-errors" role="alert">{problem}</p>
{/if}
<p class="oc-status" role="status">{status}</p>
