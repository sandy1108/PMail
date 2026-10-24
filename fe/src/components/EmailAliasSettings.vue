<template>
  <section class="email-aliases">
    <h3>{{ lang.email_aliases }}</h3>
    <p class="description">{{ lang.alias_description }}</p>
    <el-form label-position="top" @submit.prevent="createAlias">
      <el-form-item :label="lang.alias_address">
        <div class="address-input">
          <el-input v-model="localPart" maxlength="64" :placeholder="lang.alias_prefix_hint" />
          <span>@</span>
          <el-select v-model="domain" :aria-label="lang.alias_domain">
            <el-option v-for="item in domains" :key="item" :value="item" :label="item" />
          </el-select>
        </div>
        <span class="preview">{{ preview }}</span>
      </el-form-item>
      <el-form-item :label="lang.alias_remark">
        <el-input v-model="remark" maxlength="200" :placeholder="lang.alias_remark_hint" />
      </el-form-item>
      <el-button type="primary" native-type="submit" :loading="busy" :disabled="!validAddress">{{ lang.alias_add }}</el-button>
    </el-form>

    <p v-if="error" role="alert" class="error">{{ error }}</p>
    <div class="search-row">
      <el-input v-model="keyword" clearable maxlength="200" :placeholder="lang.alias_search" @keyup.enter="search" />
      <el-button :loading="loading" @click="search">{{ lang.alias_refresh }}</el-button>
    </div>
    <el-empty v-if="!loading && aliases.length === 0" :description="lang.alias_empty" :image-size="60" />
    <div v-for="alias in aliases" :key="alias.id" class="alias-card">
      <div class="alias-heading">
        <strong>{{ alias.address }}</strong>
        <el-tag :type="alias.enabled ? 'success' : 'info'" size="small">{{ alias.enabled ? lang.enabled : lang.disabled }}</el-tag>
      </div>
      <p v-if="alias.remark">{{ alias.remark }}</p>
      <p class="description">{{ lang.alias_created }}: {{ new Date(alias.created_at).toLocaleString() }}</p>
      <div class="alias-actions">
        <el-button size="small" @click="copy(alias.address)">{{ lang.alias_copy }}</el-button>
        <el-button size="small" :disabled="busy" @click="openEdit(alias)">{{ lang.alias_edit }}</el-button>
        <el-button size="small" :disabled="busy" @click="updateAlias(alias, !alias.enabled)">{{ alias.enabled ? lang.alias_disable : lang.alias_enable }}</el-button>
        <el-button size="small" type="danger" plain :disabled="busy" @click="deleteAlias(alias)">{{ lang.del_btn }}</el-button>
      </div>
    </div>
    <el-pagination v-if="total > pageSize" class="pagination" small layout="prev, pager, next" :current-page="page" :page-size="pageSize" :total="total" :disabled="loading || busy" @current-change="changePage" />
    <el-dialog v-model="editing" :title="lang.alias_edit" width="min(460px, 90vw)" append-to-body destroy-on-close>
      <p class="preview">{{ selected?.address }}</p>
      <el-input v-model="editRemark" maxlength="200" :placeholder="lang.alias_remark_hint" />
      <template #footer>
        <el-button @click="editing = false">{{ lang.access_code_cancel }}</el-button>
        <el-button type="primary" :loading="busy" @click="saveRemark">{{ lang.submit }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import {computed, onMounted, ref} from 'vue'
import {ElMessage, ElMessageBox} from 'element-plus'
import {http} from '@/utils/axios'
import {useGlobalStatusStore} from '@/stores/useGlobalStatusStore'
import lang from '../i18n/i18n'

const store = useGlobalStatusStore()
const domains = computed(() => [...new Set(store.userInfos?.domains || [])])
const domain = ref(domains.value[0] || '')
const localPart = ref('')
const remark = ref('')
const keyword = ref('')
const activeKeyword = ref('')
const aliases = ref([])
const page = ref(1)
const total = ref(0)
const pageSize = 20
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const editing = ref(false)
const selected = ref(null)
const editRemark = ref('')
const preview = computed(() => localPart.value.trim() && domain.value ? `${localPart.value.trim().toLowerCase()}@${domain.value}` : '')
const validAddress = computed(() => !!domain.value && /^[a-z0-9_+\-]+(?:\.[a-z0-9_+\-]+)*$/i.test(localPart.value.trim()) && localPart.value.trim().length <= 64)

// 服务端错误只映射已知业务提示；未知异常不直接展示内部细节。
async function request(action, body) {
  const res = await http.post(`/api/settings/aliases/${action}`, body)
  if (res.errorNo !== 0) {
    const messages = {'Address already reserved': lang.alias_conflict, 'Invalid alias parameters': lang.alias_invalid, 'Alias unavailable': lang.alias_unavailable}
    throw new Error(messages[res.errorMsg] || lang.alias_error)
  }
  return res.data
}
async function loadAliases() {
  if (loading.value) return
  loading.value = true
  error.value = ''
  try {
    const data = await request('list', {current_page: page.value, page_size: pageSize, keyword: activeKeyword.value})
    aliases.value = data.list
    total.value = data.total
  } catch (e) { error.value = e.message || lang.alias_error }
  finally { loading.value = false }
}
async function search() {
  if (loading.value || busy.value) return
  page.value = 1
  activeKeyword.value = keyword.value.trim()
  await loadAliases()
}
async function changePage(value) {
  page.value = value
  await loadAliases()
}
async function createAlias() {
  if (busy.value || loading.value || !validAddress.value) return
  busy.value = true
  error.value = ''
  try {
    await request('create', {local_part: localPart.value.trim(), domain: domain.value, remark: remark.value.trim()})
    localPart.value = ''
    remark.value = ''
    keyword.value = ''
    activeKeyword.value = ''
    page.value = 1
    await loadAliases()
  } catch (e) { error.value = e.message || lang.alias_error }
  finally { busy.value = false }
}
function openEdit(alias) {
  selected.value = alias
  editRemark.value = alias.remark
  editing.value = true
}
async function updateAlias(alias, enabled, newRemark = alias.remark) {
  if (busy.value || loading.value) return false
  busy.value = true
  error.value = ''
  try {
    await request('update', {id: alias.id, remark: newRemark, enabled})
    await loadAliases()
    return true
  } catch (e) { error.value = e.message || lang.alias_error; return false }
  finally { busy.value = false }
}
async function saveRemark() {
  if (selected.value && await updateAlias(selected.value, selected.value.enabled, editRemark.value.trim())) editing.value = false
}
async function deleteAlias(alias) {
  try {
    await ElMessageBox.confirm(lang.alias_delete_confirm, lang.del_btn, {type: 'warning', confirmButtonText: lang.del_btn, cancelButtonText: lang.access_code_cancel})
  } catch { return }
  if (busy.value || loading.value) return
  busy.value = true
  error.value = ''
  try {
    await request('delete', {id: alias.id})
    if (aliases.value.length === 1 && page.value > 1) page.value--
    await loadAliases()
  } catch (e) { error.value = e.message || lang.alias_error }
  finally { busy.value = false }
}
async function copy(address) {
  try { await navigator.clipboard.writeText(address); ElMessage.success(lang.alias_copied) }
  catch { ElMessage.info(lang.alias_copy_manually) }
}
onMounted(loadAliases)
</script>

<style scoped>
.email-aliases { max-width: 100%; }
.description, .preview { color: var(--pm-text-secondary); font-size: 13px; line-height: 1.6; overflow-wrap: anywhere; }
.address-input { display: flex; align-items: center; gap: 8px; width: 100%; }
.address-input .el-input { flex: 1; min-width: 0; }
.address-input .el-select { width: 48%; }
.preview { display: block; margin-top: 6px; }
.search-row { display: flex; gap: 8px; margin-top: 24px; }
.alias-card { margin-top: 14px; padding: 16px; border: 1px solid var(--pm-border-color); border-radius: 12px; }
.alias-heading { display: flex; align-items: flex-start; gap: 10px; justify-content: space-between; }
.alias-heading strong, .alias-card p { overflow-wrap: anywhere; }
.alias-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.alias-actions .el-button { margin-left: 0; }
.error { color: var(--el-color-danger); }
.pagination { margin-top: 20px; justify-content: center; }
</style>
