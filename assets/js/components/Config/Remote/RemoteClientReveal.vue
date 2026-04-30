<template>
	<div>
		<ol class="text-muted mb-2">
			<li>{{ $t("config.remote.qrScan") }}</li>
		</ol>

		<hr class="my-4" />

		<p>
			<i18n-t keypath="config.remote.manualLogin" scope="global">
				<template #url>
					<a :href="serverUrl" target="_blank" rel="noopener">{{ serverUrl }}</a>
				</template>
			</i18n-t>
		</p>
		<FormRow id="revealUsername" :label="$t('config.remote.username')">
			<input
				id="revealUsername"
				type="text"
				class="form-control border"
				:value="client.username"
				readonly
			/>
		</FormRow>
		<FormRow id="revealPassword" :label="$t('config.remote.password')">
			<input
				id="revealPassword"
				type="text"
				class="form-control border font-monospace"
				:value="client.password"
				readonly
			/>
			<CopyLink :text="client.password" />
		</FormRow>

		<div class="mt-4 small text-muted">
			<strong class="text-evcc">{{ $t("general.note") }}</strong>
			{{ $t("config.remote.passwordOnce") }}
		</div>

		<div class="d-flex justify-content-end mt-3">
			<button type="button" class="btn btn-primary" @click="$emit('done')">
				{{ $t("config.remote.done") }}
			</button>
		</div>
	</div>
</template>

<script lang="ts">
import { defineComponent, type PropType } from "vue";
import FormRow from "../FormRow.vue";
import CopyLink from "../../Helper/CopyLink.vue";
import type { RemoteClientCreated } from "@/types/evcc";

export default defineComponent({
	name: "RemoteClientReveal",
	components: { FormRow, CopyLink },
	props: {
		client: { type: Object as PropType<RemoteClientCreated>, required: true },
		serverUrl: { type: String, required: true },
	},
	emits: ["done"],
});
</script>
