<template>
  <el-dialog :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" :title="lang.client_help_title" width="min(660px, 94vw)" append-to-body destroy-on-close class="client-connection-help">
    <div class="help-content">
      <p>{{ lang.client_help_intro }}</p>
      <p v-if="error" role="alert">{{ lang.client_help_load_error }}</p>
      <template v-else-if="settings">
        <div class="table-scroll">
          <table>
            <thead><tr><th>{{ lang.client_help_protocol }}</th><th>{{ lang.client_help_server }}</th><th>{{ lang.client_help_port }}</th><th>{{ lang.client_help_state }}</th></tr></thead>
            <tbody><tr v-for="protocol in protocols" :key="protocol"><td>{{ protocol.toUpperCase() }}<br>SSL/TLS</td><td>{{ settings.host }}</td><td>{{ settings[protocol].port }}</td><td>{{ settings[protocol].enabled ? lang.client_help_enabled : lang.client_help_disabled }}</td></tr></tbody>
          </table>
        </div>
        <p><strong>{{ lang.client_help_username }}：</strong><code>{{ settings.username }}</code></p>
        <p>{{ lang.client_help_password }}</p>
        <p>{{ lang.client_help_scope }}</p>
        <p>{{ lang.client_help_inbound }}</p>
        <details><summary>{{ lang.client_help_local }}</summary><p v-for="protocol in protocols" :key="protocol">{{ protocol.toUpperCase() }}：{{ settings[protocol].local_port }} · SSL/TLS</p></details>
        <p>{{ lang.client_help_delivery }}</p>
      </template>
    </div>
    <template #footer><el-button @click="$emit('update:modelValue', false)">{{ lang.client_help_close }}</el-button></template>
  </el-dialog>
</template>
<script setup>
import {ref, watch} from 'vue'
import {http} from '@/utils/axios'
import lang from '../i18n/i18n'
const props = defineProps({modelValue: Boolean})
defineEmits(['update:modelValue'])
const settings = ref(null)
const error = ref(false)
const protocols = ['imap', 'pop3', 'smtp']
// 每次打开重新读取，不把部署状态或账号硬编码进教程，也不读取授权码明文。
watch(() => props.modelValue, async open => {
  if (!open) return
  settings.value = null
  error.value = false
  try {
    const response = await http.post('/api/user/info', {})
    if (response.errorNo !== 0 || !response.data?.client_settings) throw new Error()
    settings.value = response.data.client_settings
  } catch { error.value = true }
})
</script>
<style scoped>
.help-content { max-height: 65vh; overflow-y: auto; overflow-wrap: anywhere; color: var(--pm-text-primary); line-height: 1.7; }
.table-scroll { overflow-x: auto; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th, td { padding: 8px; text-align: left; border-bottom: 1px solid var(--pm-border-color); }
summary { cursor: pointer; }
</style>
