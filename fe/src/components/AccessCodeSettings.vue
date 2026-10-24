<template>
  <section class="access-codes">
    <h3>{{ lang.access_codes }}</h3>
    <p class="description">{{ lang.access_codes_desc }}</p>
    <p class="description">{{ lang.access_codes_password_notice }}</p>
    <el-form label-position="top" @submit.prevent="createCode">
      <el-form-item :label="lang.access_code_name">
        <el-input v-model="name" maxlength="80" :placeholder="lang.access_code_name_hint" />
      </el-form-item>
      <el-form-item :label="lang.access_code_expiry">
        <el-select v-model="days" style="width: 100%">
          <el-option :value="30" :label="lang.access_code_days_30" />
          <el-option :value="90" :label="lang.access_code_days_90" />
          <el-option :value="365" :label="lang.access_code_days_365" />
          <el-option :value="0" :label="lang.access_code_no_expiry" />
        </el-select>
      </el-form-item>
      <el-form-item :label="lang.access_code_purposes">
        <el-select v-model="httpPermission" style="width: 100%">
          <el-option value="" :label="lang.access_code_http_none" />
          <el-option value="http.read" :label="lang.access_code_http_read" />
          <el-option value="http.write" :label="lang.access_code_http_write" />
        </el-select>
        <el-checkbox v-model="imap">IMAP (TLS)</el-checkbox>
        <el-checkbox v-model="pop3">POP3 (TLS)</el-checkbox>
      </el-form-item>
      <p class="description">{{ lang.access_code_purpose_notice }}</p>
      <el-button type="primary" native-type="submit" :loading="creating" :disabled="!name.trim() || !selectedPermissions().length">{{ lang.access_code_create }}</el-button>
      <el-button :loading="loading" @click="loadCodes">{{ lang.access_code_refresh }}</el-button>
    </el-form>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <el-empty v-if="!loading && !error && codes.length === 0" :description="lang.access_code_empty" :image-size="60" />
    <div v-for="code in codes" :key="code.id" class="code-card">
      <div class="code-heading">
        <strong>{{ code.name }}</strong>
        <el-tag :type="code.revoked || code.expired ? 'info' : 'success'" size="small">
          {{ code.revoked ? lang.access_code_revoked : code.expired ? lang.access_code_expired : lang.access_code_active }}
        </el-tag>
      </div>
      <p>{{ permissionLabel(code.permissions) }}</p>
      <p>{{ lang.access_code_created }}: {{ formatTime(code.created_at) }}</p>
      <p>{{ lang.access_code_expiry }}: {{ formatTime(code.expires_at) }}</p>
      <el-button v-if="!code.revoked && !code.expired" size="small" @click="editCode(code)">{{ lang.access_code_edit }}</el-button>
      <el-button v-if="!code.revoked" type="danger" plain size="small" :loading="revoking === code.id" @click="revokeCode(code)">{{ lang.access_code_revoke }}</el-button>
    </div>
    <el-dialog v-model="showEdit" :title="lang.access_code_edit" width="min(460px, 90vw)" append-to-body destroy-on-close>
      <el-select v-model="editHTTP" style="width: 100%">
        <el-option value="" :label="lang.access_code_http_none" />
        <el-option value="http.read" :label="lang.access_code_http_read" />
        <el-option value="http.write" :label="lang.access_code_http_write" />
      </el-select>
      <el-checkbox v-model="editIMAP">IMAP (TLS)</el-checkbox>
      <el-checkbox v-model="editPOP3">POP3 (TLS)</el-checkbox>
      <p class="description">{{ lang.access_code_purpose_notice }}</p>
      <p v-if="editError" class="error" role="alert">{{ editError }}</p>
      <template #footer>
        <el-button @click="showEdit = false">{{ lang.access_code_cancel }}</el-button>
        <el-button type="primary" :loading="saving" :disabled="!selectedPermissions(true).length" @click="savePermissions">{{ lang.access_code_apply }}</el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="showSecret" :title="lang.access_code_create" width="min(460px, 90vw)" append-to-body destroy-on-close @closed="secret = ''">
      <el-alert :title="lang.access_code_once" type="warning" :closable="false" show-icon />
      <pre class="secret" tabindex="0">{{ secret }}</pre>
      <template #footer>
        <el-button @click="copySecret">{{ lang.access_code_copy }}</el-button>
        <el-button type="primary" @click="showSecret = false">{{ lang.access_code_saved }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import {onMounted, ref} from 'vue'
import {ElMessageBox, ElNotification} from 'element-plus'
import {http} from '@/utils/axios'
import lang from '../i18n/i18n'

const codes = ref([])
const name = ref('')
const days = ref(30)
const secret = ref('')
const showSecret = ref(false)
const creating = ref(false)
const loading = ref(false)
const revoking = ref(null)
const error = ref('')
const httpPermission = ref('http.read')
const imap = ref(false)
const pop3 = ref(false)
const showEdit = ref(false)
const editID = ref(null)
const editHTTP = ref('')
const editIMAP = ref(false)
const editPOP3 = ref(false)
const saving = ref(false)
const editError = ref('')
const selectedPermissions = (edit = false) => [edit ? editHTTP.value : httpPermission.value, (edit ? editIMAP.value : imap.value) && 'imap', (edit ? editPOP3.value : pop3.value) && 'pop3'].filter(Boolean)
const permissionLabel = permissions => (permissions || ['http.read']).map(value => ({'http.read': lang.access_code_http_read, 'http.write': lang.access_code_http_write, imap: 'IMAP', pop3: 'POP3'})[value] || value).join(' · ')
function editCode(code) {
  const permissions = code.permissions || ['http.read']
  editID.value = code.id
  editHTTP.value = permissions.find(p => p.startsWith('http.')) || ''
  editIMAP.value = permissions.includes('imap')
  editPOP3.value = permissions.includes('pop3')
  editError.value = ''
  showEdit.value = true
}
async function savePermissions() {
  if (saving.value || !selectedPermissions(true).length) return
  saving.value = true
  editError.value = ''
  try {
    const res = await http.post('/api/settings/access_codes/update', {id: editID.value, permissions: selectedPermissions(true)})
    if (res.errorNo !== 0) throw new Error()
    showEdit.value = false
    await loadCodes()
  } catch { editError.value = lang.access_code_error }
  finally { saving.value = false }
}
const formatTime = value => value ? new Date(value * 1000).toLocaleString() : lang.access_code_no_expiry

async function loadCodes() {
  loading.value = true
  error.value = ''
  try {
    const res = await http.post('/api/settings/access_codes/list', {})
    if (res.errorNo !== 0) throw new Error()
    codes.value = res.data
  } catch {
    error.value = lang.access_code_error
  } finally {
    loading.value = false
  }
}

// 明文只保存在当前弹窗内存，关闭后清除，不写本地存储或通知。
async function createCode() {
  if (creating.value || !name.value.trim() || !selectedPermissions().length) return
  creating.value = true
  error.value = ''
  try {
    const res = await http.post('/api/settings/access_codes/create', {
      name: name.value.trim(),
      permissions: selectedPermissions(),
      expires_at: days.value ? Math.floor(Date.now() / 1000) + days.value * 86400 : 0
    })
    if (res.errorNo !== 0) throw new Error()
    secret.value = res.data.token
    showSecret.value = true
    name.value = ''
    await loadCodes()
  } catch {
    error.value = lang.access_code_error
  } finally {
    creating.value = false
  }
}

async function revokeCode(code) {
  try {
    await ElMessageBox.confirm(lang.access_code_revoke_confirm, lang.access_code_revoke, {type: 'warning', confirmButtonText: lang.access_code_revoke, cancelButtonText: lang.access_code_cancel})
  } catch { return }
  revoking.value = code.id
  error.value = ''
  try {
    const res = await http.post('/api/settings/access_codes/revoke', {id: code.id})
    if (res.errorNo !== 0) throw new Error()
    await loadCodes()
  } catch {
    error.value = lang.access_code_error
  } finally {
    revoking.value = null
  }
}

async function copySecret() {
  try {
    await navigator.clipboard.writeText(secret.value)
    ElNotification({message: lang.access_code_copied, type: 'success'})
  } catch {
    ElNotification({message: lang.access_code_copy_manually, type: 'warning'})
  }
}
onMounted(loadCodes)
</script>

<style scoped>
h3 { font-size: 18px; margin: 0 0 8px; color: var(--pm-text-primary); }
.description, .code-card p { color: var(--pm-text-secondary); font-size: 13px; line-height: 1.6; }
.code-card { margin-top: 16px; padding: 16px; border: 1px solid var(--pm-border-color); border-radius: var(--pm-radius-sm); }
.code-heading { display: flex; justify-content: space-between; gap: 12px; overflow-wrap: anywhere; }
.code-card p { margin: 6px 0; }
.secret { white-space: pre-wrap; overflow-wrap: anywhere; padding: 16px; background: var(--pm-bg-secondary); user-select: all; }
.error { color: var(--el-color-danger); font-size: 13px; }
</style>
