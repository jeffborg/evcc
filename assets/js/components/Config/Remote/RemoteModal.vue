<template>
	<GenericModal
		id="remoteModal"
		:title="`${$t('config.remote.title')} 🧪`"
		config-modal-name="remote"
		data-testid="remote-modal"
	>
		<div class="alert alert-warning">
			Development preview. Not ready for general use. Use with caution and monitor your system
			closely. Feedback welcome!
		</div>
		<ErrorMessage :error="error" />

		<p>{{ $t("config.remote.description") }}</p>

		<div class="form-check form-switch my-3">
			<input
				id="remoteEnabled"
				:checked="config.enabled"
				class="form-check-input"
				type="checkbox"
				role="switch"
				@change="changeEnabled"
			/>
			<div class="form-check-label">
				<label for="remoteEnabled">{{ $t("config.remote.enableLabel") }}</label>
				<div v-if="status.url">
					<span v-if="status.connected" class="text-primary">
						{{ $t("config.remote.connected") }}
					</span>
					<span v-else class="text-muted small">
						{{ $t("config.remote.disconnected") }}
					</span>
				</div>
			</div>
		</div>

		<template v-if="config.enabled">
			<div v-if="status.url" class="mt-4">
				<FormRow id="remoteTailnetUrl" :label="$t('config.remote.url')">
					<input
						id="remoteTailnetUrl"
						type="text"
						class="form-control border"
						:value="status.url"
						readonly
					/>
				</FormRow>
			</div>

			<div v-if="status.authUrl" class="mt-4">
				<p class="text-muted small">
					{{ $t("config.remote.authRequired") }}
				</p>
				<a
					:href="status.authUrl"
					target="_blank"
					rel="noopener"
					class="btn btn-primary btn-sm"
				>
					{{ $t("config.remote.authenticateTailscale") }}
				</a>
			</div>

			<hr class="my-4" />

			<FormRow id="remoteHostname" :label="$t('config.remote.hostname')">
				<div class="input-group">
					<input
						id="remoteHostname"
						v-model="hostname"
						type="text"
						class="form-control border"
						:placeholder="$t('config.remote.hostnamePlaceholder')"
					/>
					<button
						type="button"
						class="btn btn-outline-secondary"
						@click="saveHostname"
					>
						{{ $t("config.general.save") }}
					</button>
				</div>
			</FormRow>

			<FormRow id="remoteAuthKey" :label="$t('config.remote.authKey')">
				<div class="input-group">
					<input
						id="remoteAuthKey"
						v-model="authKey"
						type="password"
						class="form-control border"
						:placeholder="$t('config.remote.authKeyPlaceholder')"
						autocomplete="off"
					/>
					<button
						type="button"
						class="btn btn-outline-secondary"
						@click="saveAuthKey"
					>
						{{ $t("config.general.save") }}
					</button>
				</div>
				<div class="form-text">{{ $t("config.remote.authKeyHint") }}</div>
			</FormRow>
		</template>
	</GenericModal>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import GenericModal from "../../Helper/GenericModal.vue";
import ErrorMessage from "../../Helper/ErrorMessage.vue";
import FormRow from "../FormRow.vue";
import api from "@/api";
import type { Remote, RemoteConfig, RemoteStatus } from "@/types/evcc";
import type { AxiosError } from "axios";

export default defineComponent({
	name: "RemoteModal",
	components: {
		GenericModal,
		ErrorMessage,
		FormRow,
	},
	props: {
		remote: { type: Object as () => Remote | undefined, default: undefined },
	},
	data() {
		return {
			error: null as string | null,
			authKey: "",
			hostname: "",
		};
	},
	computed: {
		config(): RemoteConfig {
			return this.remote?.config ?? { enabled: false, hostname: "evcc" };
		},
		status(): RemoteStatus {
			return this.remote?.status ?? { connected: false };
		},
	},
	watch: {
		"config.hostname": {
			immediate: true,
			handler(val: string) {
				this.hostname = val || "evcc";
			},
		},
	},
	methods: {
		async changeEnabled(e: Event) {
			const target = e.target as HTMLInputElement;
			const checked = target.checked;
			try {
				this.error = null;
				await api.post(`config/remote/${checked}`);
			} catch (err) {
				target.checked = !checked;
				this.handleError(err);
			}
		},
		async saveAuthKey() {
			try {
				this.error = null;
				await api.post("config/remote/authkey", { authKey: this.authKey });
				this.authKey = "";
			} catch (err) {
				this.handleError(err);
			}
		},
		async saveHostname() {
			try {
				this.error = null;
				await api.post("config/remote/hostname", { hostname: this.hostname });
			} catch (err) {
				this.handleError(err);
			}
		},
		handleError(err: unknown) {
			const axiosErr = err as AxiosError<{ error: string }>;
			this.error = axiosErr.response?.data?.error || axiosErr.message || String(err);
		},
	},
});
</script>
