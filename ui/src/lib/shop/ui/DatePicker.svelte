<script lang="ts">
	// The shared date-and-time picker (approved GK-016 r01c sketch): how the
	// cake is handed over, a month of days with the places left, and the
	// day's times, loaded automatically (no "Check" button). Loading, no times
	// and errors are distinct; obsolete answers are ignored; a time taken
	// while choosing is answered with fresh choices and nothing is picked for
	// the customer. Nothing is reserved: checkout asks the server again.
	import { tick } from 'svelte';
	import type { Day, Slot } from '../../checkout/types';
	import { dateLabel, londonToday } from '../../checkout/format';
	import type { ChosenSlot, PickerMode } from '../bag';
	import type { ShopApi } from '../client';
	import {
		addMonths,
		calendarNote,
		dayView,
		londonDate,
		monthGrid,
		monthLabel,
		monthOf,
		monthRange,
		nextOpenDates,
		restoreChoice,
		shortDate,
		slotLabel,
		timeChoices,
		timesStatus,
		type DayView
	} from '../picker';
	import Icon from './Icon.svelte';

	let {
		api,
		places,
		collectBy,
		onchoose
	}: {
		api: ShopApi;
		/** Made-to-order cakes in the bag (with the one being added). */
		places: number;
		/** Earliest collect-by instant of the ready-made cakes, if any. */
		collectBy?: string;
		onchoose: (date: string, mode: PickerMode, slot: ChosenSlot) => void;
	} = $props();

	const quantity = $derived(Math.max(1, places));
	const collectByDate = $derived(collectBy ? londonDate(collectBy) : undefined);
	const WEEKDAYS = ['M', 'T', 'W', 'T', 'F', 'S', 'S'];
	const MAX_MONTHS_AHEAD = 12;

	let dialog = $state<HTMLDialogElement>();
	let alertBox = $state<HTMLDivElement>();
	let today = $state(londonToday());
	let mode = $state<PickerMode>('collection');
	let month = $state(monthOf(londonToday()));
	let days = $state<Record<string, Day>>({});
	let monthState = $state<'loading' | 'ready' | 'error'>('loading');
	let date = $state<string | undefined>();
	// The time the customer chose ("13:00–14:00"), kept across dates and handovers.
	let wanted = $state<string | undefined>();
	let chosen = $state<string | undefined>();
	let times = $state<{
		status: 'idle' | 'loading' | 'list' | 'empty' | 'error';
		slots: Slot[];
		hiddenLead: number;
		hiddenLate: number;
		dayStatus?: string;
		/** The earlier time, and whether it was still offered, when these times arrived. */
		wanted?: string;
		kept?: boolean;
	}>({ status: 'idle', slots: [], hiddenLead: 0, hiddenLate: 0 });
	let alertText = $state<{ strong: string; rest: string } | undefined>();
	let checking = $state(false);
	let checkFailed = $state(false);
	let monthRevision = 0;
	let timesRevision = 0;
	// Each opening of the dialog; closing it ends that opening. A time check
	// that comes back after the customer closed the picker (a cancel) or
	// reopened it saves nothing.
	let opening = 0;

	const grid = $derived(monthGrid(month));
	const views = $derived<DayView[]>(
		grid.dates.map((d) => dayView(d, today, days[d], { places, collectBy: collectByDate }))
	);
	const focusDay = $derived(
		views.find((v) => v.date === date && v.enabled)?.date ?? views.find((v) => v.enabled)?.date
	);
	const lastDay = $derived(days[grid.dates[grid.dates.length - 1]]);
	const canPrev = $derived(month > monthOf(today));
	// No later month when this one already reaches the horizon (or a ready-made
	// cake's collect-by date), nor beyond a year.
	const canNext = $derived(
		month < addMonths(monthOf(today), MAX_MONTHS_AHEAD) &&
			lastDay?.status !== 'outside_horizon' &&
			!(collectByDate && collectByDate < `${addMonths(month, 1)}-01`)
	);
	const chosenSlot = $derived(times.slots.find((s) => s.id === chosen));
	const alternatives = $derived(date ? nextOpenDates(date, views) : []);
	const note = $derived(calendarNote(places, collectByDate));

	/** Opens the picker with the bag's current choice. */
	export async function show(initial: { date?: string; mode: PickerMode; slot?: ChosenSlot }) {
		opening++;
		checking = false;
		today = londonToday();
		mode = initial.mode;
		date = initial.date && initial.date >= today ? initial.date : undefined;
		wanted = date && initial.slot ? slotLabel(initial.slot) : undefined;
		chosen = undefined;
		alertText = undefined;
		checkFailed = false;
		month = monthOf(date ?? today);
		dialog?.showModal();
		await loadMonth();
		void loadTimes();
		await tick();
		focusCalendar();
	}

	function focusCalendar() {
		const el =
			dialog?.querySelector<HTMLButtonElement>('.calendar-days [tabindex="0"]') ??
			dialog?.querySelector<HTMLButtonElement>('.calendar-heading button:not(:disabled)');
		el?.focus();
	}

	async function loadMonth() {
		const rev = ++monthRevision;
		const range = monthRange(month, today);
		if (!range) {
			monthState = 'ready';
			return;
		}
		monthState = 'loading';
		try {
			const res = await api.availability(range.from, range.to, quantity);
			if (rev !== monthRevision) return;
			const next = { ...days };
			for (const d of res.days) next[d.date] = d;
			days = next;
			monthState = 'ready';
		} catch {
			if (rev === monthRevision) monthState = 'error';
		}
	}

	async function changeMonth(n: number, focusAfter = false) {
		month = addMonths(month, n);
		await loadMonth();
		if (focusAfter) {
			await tick();
			focusCalendar();
		}
	}

	async function loadTimes() {
		const rev = ++timesRevision;
		chosen = undefined;
		if (!date) {
			times = { status: 'idle', slots: [], hiddenLead: 0, hiddenLate: 0 };
			return;
		}
		times = { status: 'loading', slots: [], hiddenLead: 0, hiddenLate: 0 };
		try {
			const res = await api.schedule(date, mode, quantity);
			if (rev !== timesRevision) return;
			const tc = timeChoices(res.slots, collectBy);
			chosen = restoreChoice(tc.slots, wanted);
			times = {
				status: tc.slots.length > 0 ? 'list' : 'empty',
				...tc,
				dayStatus: res.day_status,
				wanted,
				kept: chosen !== undefined
			};
		} catch {
			if (rev === timesRevision)
				times = { status: 'error', slots: [], hiddenLead: 0, hiddenLate: 0 };
		}
	}

	function pickDate(d: string) {
		date = d;
		alertText = undefined;
		checkFailed = false;
		if (monthOf(d) !== month) {
			month = monthOf(d);
			void loadMonth();
		}
		void loadTimes();
	}

	function onDayKey(e: KeyboardEvent, d: string) {
		const delta: Record<string, number> = {
			ArrowLeft: -1,
			ArrowRight: 1,
			ArrowUp: -7,
			ArrowDown: 7
		};
		if (e.key === 'PageUp' || e.key === 'PageDown') {
			e.preventDefault();
			const n = e.key === 'PageUp' ? -1 : 1;
			if ((n < 0 && canPrev) || (n > 0 && canNext)) void changeMonth(n, true);
			return;
		}
		const step = delta[e.key];
		if (step === undefined) return;
		e.preventDefault();
		const dir = Math.sign(step);
		let i = grid.dates.indexOf(d) + step;
		while (i >= 0 && i < views.length && !views[i].enabled) i += dir;
		if (i >= 0 && i < views.length) {
			dialog?.querySelector<HTMLButtonElement>(`[data-day="${views[i].date}"]`)?.focus();
		} else if ((dir < 0 && canPrev) || (dir > 0 && canNext)) {
			void changeMonth(dir, true);
		}
	}

	async function use() {
		const d = date;
		const s = chosenSlot;
		if (!d || !s || checking) return;
		checking = true;
		checkFailed = false;
		const m = mode;
		const at = opening;
		try {
			const res = await api.schedule(d, m, quantity);
			// The customer closed the picker or moved on while we were
			// checking: closing is a cancel, and their new choice wins.
			if (at !== opening || date !== d || mode !== m || chosen !== s.id) return;
			const tc = timeChoices(res.slots, collectBy);
			if (tc.slots.some((x) => x.id === s.id)) {
				dialog?.close();
				onchoose(d, m, { id: s.id, starts_at: s.starts_at, ends_at: s.ends_at });
				return;
			}
			wanted = undefined;
			chosen = undefined;
			timesRevision++;
			times = {
				status: tc.slots.length > 0 ? 'list' : 'empty',
				...tc,
				dayStatus: res.day_status
			};
			if (res.day_status === 'full' || tc.slots.length === 0) {
				days = {
					...days,
					[d]: { date: d, status: 'full', remaining_places: 0 }
				};
				alertText = {
					strong: `Sorry, ${shortDate(d)} has just filled up.`,
					rest: 'Someone took the last place while you were choosing. Your cake and how you’ll get it are kept. Please choose another date; we haven’t picked one for you.'
				};
			} else {
				alertText = {
					strong: `${slotLabel(s)} on ${shortDate(d)} can no longer be booked.`,
					rest: 'Here are the times still open. Your date and other choices are kept.'
				};
			}
			await tick();
			alertBox?.focus();
		} catch {
			if (at === opening) checkFailed = true;
		} finally {
			if (at === opening) checking = false;
		}
	}

	const useLabel = $derived(
		checking
			? 'Checking your time…'
			: times.status === 'loading'
				? 'Loading times…'
				: date && chosenSlot
					? `Use ${shortDate(date)} · ${slotLabel(chosenSlot)}`
					: date
						? 'Choose a time'
						: 'Choose a date and time'
	);

	const statusText = $derived(
		timesStatus(
			date,
			{ ...times, count: times.slots.length },
			{ label: times.wanted, kept: times.kept ?? false }
		)
	);
</script>

<dialog
	bind:this={dialog}
	class="picker-dialog"
	aria-labelledby="picker-title"
	onclose={() => {
		opening++;
		checking = false;
	}}
>
	<button
		class="icon-button picker-close"
		type="button"
		aria-label="Close date and time picker"
		onclick={() => dialog?.close()}>×</button
	>
	<h2 id="picker-title">Choose your date and time</h2>
	<p class="small">London time. Nothing is reserved until you pay.</p>

	<fieldset class="picker-modes">
		<legend class="picker-legend">How you’ll get your cake</legend>
		<label class="mode-card">
			<input
				type="radio"
				name="picker-mode"
				value="collection"
				bind:group={mode}
				onchange={() => void loadTimes()}
			/>
			<span><strong>Collect · Free</strong><small>St Katharine Docks, London.</small></span>
		</label>
		<label class="mode-card">
			<input
				type="radio"
				name="picker-mode"
				value="customer_courier"
				bind:group={mode}
				onchange={() => void loadTimes()}
			/>
			<span
				><strong>Own courier</strong><small>You book and pay a courier to collect from us.</small
				></span
			>
		</label>
		<label class="mode-card">
			<input type="radio" name="picker-mode" value="gk_delivery" disabled />
			<span
				><strong>GK delivery · from £100</strong><small
					>Not available online yet: please contact us to arrange it.</small
				></span
			>
		</label>
	</fieldset>

	<div class="calendar">
		<div class="calendar-heading">
			<button
				type="button"
				aria-label="Previous month"
				disabled={!canPrev}
				onclick={() => void changeMonth(-1)}>‹</button
			>
			<h3 aria-live="polite">{monthLabel(month)}</h3>
			<button
				type="button"
				aria-label="Next month"
				disabled={!canNext}
				onclick={() => void changeMonth(1)}>›</button
			>
		</div>
		<div class="weekdays" aria-hidden="true">
			{#each WEEKDAYS as w, i (i)}<span>{w}</span>{/each}
		</div>
		<div
			class="calendar-days"
			role="group"
			aria-label="Choose a day"
			aria-busy={monthState === 'loading'}
		>
			{#each { length: grid.offset }, i (i)}<span></span>{/each}
			{#each views as v (v.date)}
				<button
					type="button"
					class={`day s-${v.status}`}
					data-day={v.date}
					disabled={!v.enabled}
					aria-pressed={date === v.date}
					aria-label={`${dateLabel(v.date)}${v.aria}`}
					tabindex={v.date === focusDay ? 0 : -1}
					onclick={() => pickDate(v.date)}
					onkeydown={(e) => onDayKey(e, v.date)}
				>
					<span>{v.day}</span>
					{#if v.label}<small>{v.label}</small>{/if}
				</button>
			{/each}
		</div>
		{#if monthState === 'loading'}
			<p class="calendar-note" role="status">Loading dates…</p>
		{:else if monthState === 'error'}
			<div class="calendar-note">
				<p role="alert">We couldn’t load the dates. Your other choices are kept.</p>
				<button class="btn" type="button" onclick={() => void loadMonth()}>Try again</button>
			</div>
		{:else}
			<p class="calendar-note">
				{note}
			</p>
		{/if}
	</div>

	{#if alertText}
		<div class="notice error" role="alert" tabindex="-1" bind:this={alertBox}>
			<p><strong>{alertText.strong}</strong> {alertText.rest}</p>
		</div>
	{/if}

	<fieldset
		class="slot-field"
		aria-describedby="picker-slot-status"
		aria-busy={times.status === 'loading'}
	>
		<legend>Times · London time</legend>
		<p class="slot-status" id="picker-slot-status" role="status">{statusText}</p>
		<div class="slot-list">
			{#if times.status === 'loading'}
				{#each { length: 4 }, i (i)}<span class="slot-skeleton" aria-hidden="true"></span>{/each}
			{:else if times.status === 'list'}
				{#each times.slots as s (s.id)}
					<label class="slot-option">
						<input
							type="radio"
							name="picker-slot"
							value={s.id}
							bind:group={chosen}
							onchange={() => (wanted = slotLabel(s))}
						/>
						<span
							><strong>{slotLabel(s)}</strong><small
								>{s.availability === 'requestable'
									? 'Requested, the chef confirms'
									: 'Available'}</small
							></span
						>
					</label>
				{/each}
			{/if}
		</div>
		{#if times.status === 'error'}
			<div class="slot-extra">
				<button class="btn" type="button" onclick={() => void loadTimes()}>Try again</button>
			</div>
		{:else if times.status === 'empty'}
			<div class="slot-extra">
				{#each alternatives as a (a)}
					<button class="btn" type="button" onclick={() => pickDate(a)}>Try {shortDate(a)}</button>
				{/each}
				<a class="text-link" href="mailto:gk.cake.london@gmail.com">Contact the chef →</a>
			</div>
		{/if}
	</fieldset>

	<button
		class="btn primary picker-use"
		type="button"
		disabled={!chosenSlot || checking || times.status === 'loading'}
		onclick={() => void use()}
	>
		<Icon name="calendar" />
		{useLabel}
	</button>
	{#if checkFailed}
		<p class="small" role="alert">
			We couldn’t check your time. Your choices are kept; please try again.
		</p>
	{/if}
	<p class="small">Times are 60 minutes, London time.</p>
</dialog>
