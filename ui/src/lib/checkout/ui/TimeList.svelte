<script lang="ts">
	// Visible start–end times as a radio list (GK-017 r02 item, sketch
	// def('time-list')): native radios in a fieldset with a legend, so arrow
	// keys move between times and Space selects; the current time is marked
	// and a refused choice stays struck through (Q15).
	import type { MoveOption } from '../changes';

	interface Props {
		legend: string;
		name: string;
		options: MoveOption[];
		value: string;
		/** The whole list can't be used now (for example after the six-hour cutoff). */
		disabled?: boolean;
		describedby?: string;
		id?: string;
	}

	let {
		legend,
		name,
		options,
		value = $bindable(),
		disabled = false,
		describedby,
		id
	}: Props = $props();
</script>

<fieldset class="oc-times" {id} {disabled} aria-describedby={describedby} tabindex="-1">
	<legend>{legend}</legend>
	<div class="oc-time-options">
		{#each options as o (o.id)}
			<label
				class="oc-time"
				class:is-off={o.disabled}
				class:is-gone={o.gone}
				class:is-current={o.current}
			>
				<input type="radio" {name} value={o.id} bind:group={value} disabled={o.disabled} />
				<span><strong>{o.label}</strong><small>{o.note}</small></span>
			</label>
		{/each}
	</div>
</fieldset>
