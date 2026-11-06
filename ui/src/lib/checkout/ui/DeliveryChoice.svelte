<script lang="ts">
	// "What would you like to do?" (GK-017 r02b states 14, 15 pay-by passed
	// and 19 our side): the options the API lists as radio cards; collection
	// and own courier reveal the day's times the API lists (Q10). Nothing
	// changes until "Confirm my choice"; the page then reads the order until
	// the choice is secured or refused (a refusal reopens the choice, with
	// the reason above the options).
	import { tick } from 'svelte';
	import type { ChangeBaseline } from '../changes';
	import type { OrderRecoveryApi } from '../client';
	import type { ActionKeys, Who } from '../flow';
	import {
		choiceBaseline,
		recoveryProblemFor,
		sendChoice,
		type ChoiceCard,
		type ChoiceKind
	} from '../recovery';
	import type { Order } from '../types';
	import TimeList from './TimeList.svelte';

	interface Props {
		order: Order;
		who: Who;
		api: OrderRecoveryApi;
		keys: ActionKeys;
		card: ChoiceCard;
		/** The choice was accepted (202): the page reads the order until its outcome. */
		onsent: (kind: ChoiceKind, baseline: ChangeBaseline) => void;
		/** The order changed: read it again (fresh options and times). */
		onrefresh: () => void;
	}

	let { order, who, api, keys, card, onsent, onrefresh }: Props = $props();

	let choice = $state('');
	let time = $state('');
	let error = $state<{ field: 'choice' | 'time'; message: string } | null>(null);
	let problem = $state('');
	let busy = $state(false);

	const option = $derived(card.options.find((o) => o.value === choice));
	const needsTime = $derived(option?.needsTime === true);

	// A time no longer listed (the order was read again) is not kept chosen.
	$effect(() => {
		if (time && !card.times.some((t) => t.id === time)) time = '';
	});
	// Choosing clears the matching error.
	$effect(() => {
		if (error?.field === 'choice' && choice) error = null;
		else if (error?.field === 'time' && time) error = null;
	});

	async function invalid(field: 'choice' | 'time', message: string, focus: string) {
		error = { field, message };
		await tick();
		document.getElementById(focus)?.focus();
	}

	async function confirm() {
		if (busy) return;
		problem = '';
		if (!option) return invalid('choice', 'Choose one option.', 'choice-options');
		if (option.needsTime && !time) {
			return card.times.length
				? invalid('time', 'Choose a time.', 'choice-times')
				: invalid('time', card.noTimes, 'choice-options');
		}
		error = null;
		busy = true;
		const baseline = choiceBaseline(order, card.kind);
		try {
			await sendChoice(
				api,
				keys,
				order,
				card.kind,
				option.value,
				option.needsTime ? time : undefined,
				who
			);
			onsent(card.kind, baseline);
		} catch (e) {
			const p = recoveryProblemFor(e, 'choice');
			problem = p.message;
			if (p.next === 'refresh') onrefresh();
		}
		busy = false;
	}
</script>

<section class="co-card" aria-labelledby="choice-h">
	<h2 class="co-h" id="choice-h">{card.heading}</h2>
	{#if card.lead}<p class="small">{card.lead}</p>{/if}
	{#if card.refusal}
		<div class="notice warning oc-focus" id="choice-refused" tabindex="-1">
			<p>{card.refusal}</p>
		</div>
	{/if}
	<fieldset
		class="oc-choices"
		id="choice-options"
		tabindex="-1"
		aria-labelledby="choice-h"
		aria-describedby={error?.field === 'choice' ? 'choice-error' : undefined}
	>
		{#each card.options as o (o.value)}
			<label class="mode-card">
				<input type="radio" name="delivery-choice" value={o.value} bind:group={choice} />
				<span><strong>{o.label}</strong><small>{o.note}</small></span>
			</label>
		{/each}
	</fieldset>
	{#if needsTime}
		{#if card.times.length > 0}
			<TimeList
				id="choice-times"
				legend={card.legends[choice] ?? 'Time · London time'}
				name="choice-time"
				options={card.times}
				bind:value={time}
				describedby={error?.field === 'time' ? 'choice-error' : undefined}
			/>
		{:else}
			<div class="notice"><p>{card.noTimes}</p></div>
		{/if}
	{/if}
	{#if card.deadline}<p class="small">{card.deadline}</p>{/if}
	{#if error}<p class="field-error" id="choice-error" role="alert">{error.message}</p>{/if}
	{#if problem}<p class="co-errors" role="alert">{problem}</p>{/if}
	<div class="oc-actions">
		<button
			type="button"
			class="primary co-pay"
			onclick={() => void confirm()}
			aria-disabled={busy ? 'true' : undefined}
			aria-busy={busy}
		>
			<span>{busy ? 'Confirming your choice…' : 'Confirm my choice'}</span>
			<span class="co-arrow" aria-hidden="true">→</span>
		</button>
	</div>
</section>
