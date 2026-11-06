<script lang="ts">
	// A date and one of its published times for the chef's offer or time
	// change (r03b2 slot list): the day's starts from the public schedule,
	// those still ahead, never a closed date or the order's own time. The
	// chef's command checks its own rules (shelf life, the day's places for
	// an offer); a refusal is worded by the page.
	import { untrack } from 'svelte';
	import { ApiError } from '../../api';
	import type { StaffApi } from '../catalogue';
	import {
		firstDate,
		lastDate,
		scheduleDay,
		slotOptions,
		type ScheduleChoices,
		type SlotOption
	} from '../order-decisions';
	import type { StaffOrderDetail } from '../order-detail';

	let {
		api,
		order,
		now,
		purpose,
		idPrefix,
		date = $bindable(),
		choice = $bindable(),
		disabled = false,
		dateLabel = 'Date'
	}: {
		api: StaffApi;
		order: StaffOrderDetail;
		now: Date;
		purpose: 'offer' | 'appointment';
		idPrefix: string;
		date: string;
		choice: SlotOption | null;
		disabled?: boolean;
		dateLabel?: string;
	} = $props();

	let phase = $state<'idle' | 'loading' | 'ready' | 'error'>('idle');
	let day = $state.raw<ScheduleChoices | null>(null);
	let loadNote = $state('');
	let passedNote = $state('');
	let seq = 0;

	// The fetched day is filtered against the page's clock as it ticks (no
	// refetch): a start that has begun drops out.
	const shown = $derived(day ? slotOptions(day, order, now, purpose) : null);
	const options = $derived(shown?.options ?? []);
	const note = $derived(phase === 'error' ? loadNote : (shown?.note ?? ''));

	// A chosen time that has started since is cleared, with a short note.
	$effect(() => {
		const c = choice;
		if (c && new Date(c.starts_at) <= now) {
			untrack(() => {
				passedNote = `${c.label} has started, so it was cleared. Choose another time.`;
				choice = null;
			});
		}
	});

	async function load(target: string) {
		const mine = ++seq;
		choice = null;
		day = null;
		passedNote = '';
		if (!/^\d{4}-\d{2}-\d{2}$/.test(target)) {
			phase = 'idle';
			return;
		}
		phase = 'loading';
		try {
			const c = await scheduleDay(api, order, target);
			if (mine !== seq) return;
			day = c;
			phase = 'ready';
		} catch (e) {
			if (mine !== seq) return;
			phase = 'error';
			loadNote =
				e instanceof ApiError && e.status === 422
					? 'Choose a date as day, month and year.'
					: 'The times for that day couldn’t be loaded.';
		}
	}

	// Only the date reloads the times (an order read again keeps the choice).
	$effect(() => {
		const target = date;
		untrack(() => void load(target));
	});
</script>

<div class="slot-picker">
	<label class="field" for="{idPrefix}-date"
		>{dateLabel}
		<input
			id="{idPrefix}-date"
			type="date"
			min={firstDate(now)}
			max={lastDate(now)}
			bind:value={date}
			{disabled}
		/>
	</label>
	{#if phase === 'loading'}
		<p class="small muted" role="status">Loading the times…</p>
	{:else if phase === 'error'}
		<div class="small bad-text" role="alert">
			<p>{note}</p>
			<button class="btn btn-small" type="button" onclick={() => void load(date)}>Try again</button>
		</div>
	{:else if phase === 'ready'}
		{#if note}<p class="small" role="status">{note}</p>{/if}
		{#if passedNote}<p class="small warn-text" role="status">{passedNote}</p>{/if}
		{#if options.length}
			<fieldset class="slot-fieldset" {disabled}>
				<legend>Time (London time)</legend>
				<div class="slot-list">
					{#each options as opt (opt.id)}
						<label class="slot-opt" class:is-off={opt.disabled}>
							<input
								type="radio"
								name="{idPrefix}-slot"
								value={opt.id}
								disabled={opt.disabled}
								checked={choice?.id === opt.id}
								onchange={() => {
									choice = opt;
									passedNote = '';
								}}
							/>
							<span>{opt.label}</span>
							{#if opt.hint}<span class="slot-hint">· {opt.hint}</span>{/if}
						</label>
					{/each}
				</div>
			</fieldset>
		{/if}
	{/if}
</div>
