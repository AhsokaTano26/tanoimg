<script setup>
import { onMounted, reactive, ref } from 'vue';
import { request, toast } from '../../runtime.js';
import { ask } from '../../dialogs.js';
import FormField from '../../components/FormField.vue';

const health = ref(null);
const tasks = ref([]), taskKind = ref('moderation'), taskPage = ref(1), taskTotal = ref(0);
const integrityKind = ref('images'), integrity = ref(null), integrityFindings = ref([]), integrityScanned = ref(0);
const audit = ref([]), auditPage = ref(1), auditTotal = ref(0);
const maintenance = ref({ enabled:false, message:'', retryAfterSeconds:60 });
const maintenanceLoaded = ref(false);
const reports = ref([]), reportStatus = ref('open'), reportPage = ref(1), reportTotal = ref(0);
const loading = reactive({ health:false, tasks:false, integrity:false, audit:false, maintenance:false, reports:false, saveMaintenance:false, reportId:null });
const errors = reactive({ health:'', tasks:'', integrity:'', audit:'', maintenance:'', reports:'' });
const pageSize = 20;
const issueLabels = {missing:'文件缺失',symlink:'符号链接',not_regular:'不是普通文件',size_mismatch:'文件大小不符',invalid_filename:'文件名无效',orphan:'孤立文件'};
const reasonLabels = {copyright:'版权',privacy:'隐私',abuse:'滥用',spam:'垃圾内容',other:'其他'};
const statusLabels = {open:'待处理',resolved:'已解决',dismissed:'已驳回'};
const mb = value => value == null ? '—' : `${(value / 1048576).toFixed(1)} MB`;

async function fetchSection(name, url, apply) {
  loading[name] = true; errors[name] = '';
  try { apply(await request(url)); }
  catch (error) { errors[name] = error.message; }
  finally { loading[name] = false; }
}
const loadHealth = () => fetchSection('health','/api/admin/health',data => { health.value = data; });
const loadTasks = () => fetchSection('tasks',`/api/admin/tasks?kind=${taskKind.value}&page=${taskPage.value}&limit=${pageSize}`,data => { tasks.value = data.tasks || []; taskTotal.value = data.total || 0; });
const loadAudit = () => fetchSection('audit',`/api/admin/audit?page=${auditPage.value}&limit=${pageSize}`,data => { audit.value = data.events || []; auditTotal.value = data.pagination?.total || 0; });
const loadMaintenance = () => fetchSection('maintenance','/api/admin/maintenance',data => { maintenance.value = data; maintenanceLoaded.value = true; });
const loadReports = () => fetchSection('reports',`/api/admin/reports?status=${reportStatus.value}&page=${reportPage.value}&limit=${pageSize}`,data => { reports.value = data.reports || []; reportTotal.value = data.pagination?.total || 0; });

async function scanIntegrity(reset = false) {
  if (reset) { integrity.value = null; integrityFindings.value = []; integrityScanned.value = 0; }
  loading.integrity = true; errors.integrity = '';
  try {
    const query = new URLSearchParams({kind:integrityKind.value,limit:'100'});
    if (!reset && integrity.value?.nextCursor) query.set('cursor',integrity.value.nextCursor);
    const data = await request(`/api/admin/integrity?${query}`);
    integrity.value = data;
    integrityScanned.value += data.scanned || 0;
    integrityFindings.value.push(...(data.findings || []));
  } catch (error) { errors.integrity = error.message; }
  finally { loading.integrity = false; }
}
function changeIntegrityKind(value) { integrityKind.value = value; scanIntegrity(true); }
function changeTaskKind(value) { taskKind.value = value; taskPage.value = 1; tasks.value = []; loadTasks(); }
function changeReportStatus(value) { reportStatus.value = value; reportPage.value = 1; reports.value = []; loadReports(); }
function taskMove(delta) { taskPage.value += delta; loadTasks(); }
function auditMove(delta) { auditPage.value += delta; loadAudit(); }
function reportMove(delta) { reportPage.value += delta; loadReports(); }

async function saveMaintenance() {
  const data = {enabled:!!maintenance.value.enabled,message:maintenance.value.message || '',retryAfterSeconds:Number(maintenance.value.retryAfterSeconds)};
  if (!Number.isInteger(data.retryAfterSeconds) || data.retryAfterSeconds < 1 || data.retryAfterSeconds > 3600) { errors.maintenance = '重试间隔须为 1–3600 秒'; return; }
  if ([...data.message].length > 500) { errors.maintenance = '维护公告不能超过 500 字'; return; }
  if (data.enabled && !await ask('开启后访客的写入请求会暂时被拒绝。确定保存吗？',{title:'开启维护模式',accept:'开启维护模式'})) return;
  loading.saveMaintenance = true; errors.maintenance = '';
  try {
    maintenance.value = await request('/api/admin/maintenance',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(data)});
    toast('维护设置已保存');
  } catch (error) { errors.maintenance = error.message; }
  finally { loading.saveMaintenance = false; }
}
async function setReportStatus(report, status) {
  loading.reportId = report.id; errors.reports = '';
  try {
    await request(`/api/admin/reports/${report.id}`,{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({status})});
    toast('举报状态已更新'); await loadReports();
  } catch (error) { errors.reports = error.message; }
  finally { loading.reportId = null; }
}
function refreshAll() { return Promise.all([loadHealth(),loadTasks(),scanIntegrity(true),loadAudit(),loadMaintenance(),loadReports()]); }
onMounted(refreshAll);
</script>

<template>
  <section class="page-view operations-view">
    <div class="section-title"><div><span class="eyebrow">OPERATIONS</span><h1>运行与审计</h1><p>查看服务状态、后台任务、文件完整性与管理员操作。</p></div><UiButton icon="refresh-cw" @click="refreshAll">刷新全部</UiButton></div>

    <section class="settings-panel ops-panel" aria-labelledby="ops-health"><div class="ops-heading"><h2 id="ops-health">服务状态</h2><UiButton :loading="loading.health" @click="loadHealth">刷新</UiButton></div>
      <p v-if="errors.health" role="alert" class="form-error">{{ errors.health }}</p><p v-else-if="loading.health && !health" role="status">正在读取服务状态…</p>
      <template v-else-if="health"><div class="stats-row"><div><strong>{{ health.status==='ready'?'正常':'需关注' }}</strong><span>运行状态</span></div><div><strong>{{ health.database }}</strong><span>数据库</span></div><div><strong>{{ mb(health.disk?.availableBytes) }}</strong><span>磁盘可用空间</span></div></div>
        <p v-if="health.alerts?.length" class="form-error">告警：{{ health.alerts.join('、') }}</p><p v-else class="field-help">暂无告警。</p>
        <div class="ops-queue"><div v-for="(counts,kind) in health.queues" :key="kind"><strong>{{ kind==='moderation'?'内容审核':'通知推送' }}</strong><span v-if="!Object.keys(counts || {}).length">暂无任务</span><span v-for="(count,status) in counts" :key="status">{{ status }} {{ count }}</span></div></div>
      </template>
    </section>

    <section class="settings-panel ops-panel" aria-labelledby="ops-tasks"><div class="ops-heading"><h2 id="ops-tasks">后台任务</h2><div class="inline-actions"><UiSelect :model-value="taskKind" :options="[{label:'内容审核',value:'moderation'},{label:'通知推送',value:'notification'},{label:'远程迁移',value:'migration'}]" aria-label="任务类型" @update:model-value="changeTaskKind" /><UiButton :loading="loading.tasks" @click="loadTasks">刷新</UiButton></div></div>
      <p v-if="errors.tasks" role="alert" class="form-error">{{ errors.tasks }}</p><p v-else-if="loading.tasks && !tasks.length" role="status">正在读取任务…</p><p v-else-if="!tasks.length" class="empty">暂无任务。</p>
      <div v-else class="ops-list"><article v-for="task in tasks" :key="task.id"><strong>{{ task.id }}</strong><span>{{ task.status || task.phase || '—' }}</span><small v-if="task.imageId">图片 {{ task.imageId }}</small><small v-if="task.eventKind">事件 {{ task.eventKind }}</small><small v-if="task.retryCount">已重试 {{ task.retryCount }} 次</small><small v-if="task.hasError" class="form-error">执行出现错误</small></article></div>
      <div class="ops-pages"><span>第 {{ taskPage }} 页 · 共 {{ taskTotal }} 项</span><UiButton :disabled="loading.tasks || taskPage<=1" @click="taskMove(-1)">上一页</UiButton><UiButton :disabled="loading.tasks || taskPage*pageSize>=taskTotal || taskPage>=100" @click="taskMove(1)">下一页</UiButton></div>
    </section>

    <section class="settings-panel ops-panel" aria-labelledby="ops-integrity"><div class="ops-heading"><h2 id="ops-integrity">文件完整性</h2><div class="inline-actions"><UiSelect :model-value="integrityKind" :options="[{label:'图片记录',value:'images'},{label:'上传目录',value:'uploads'}]" aria-label="扫描类型" @update:model-value="changeIntegrityKind" /><UiButton :loading="loading.integrity" @click="scanIntegrity(true)">重新扫描</UiButton></div></div>
      <p class="field-help">只读扫描，每次检查最多 100 项。</p><p v-if="errors.integrity" role="alert" class="form-error">{{ errors.integrity }}</p><p v-else-if="loading.integrity && !integrity" role="status">正在扫描…</p>
      <template v-if="integrity"><p>已扫描 {{ integrityScanned }} 项，发现 {{ integrityFindings.length }} 个问题。{{ integrity.done?'扫描完成。':'可继续扫描下一批。' }}</p><p v-if="!integrityFindings.length" class="empty">当前扫描范围暂无问题。</p><div v-else class="ops-list"><article v-for="(item,index) in integrityFindings" :key="`${item.id || item.filename}-${index}`"><strong>{{ item.filename || item.id }}</strong><span>{{ issueLabels[item.issue] || item.issue }}</span><small v-if="item.expectedSize!=null">预期 {{ mb(item.expectedSize) }}，实际 {{ mb(item.actualSize) }}</small></article></div><UiButton v-if="!integrity.done" :loading="loading.integrity" @click="scanIntegrity()">扫描下一批</UiButton></template>
    </section>

    <section class="settings-panel ops-panel" aria-labelledby="ops-maintenance"><div class="ops-heading"><h2 id="ops-maintenance">维护模式</h2><UiButton :loading="loading.maintenance" @click="loadMaintenance">重新读取</UiButton></div>
      <p v-if="errors.maintenance" role="alert" class="form-error">{{ errors.maintenance }}</p><p v-if="loading.maintenance" role="status">正在读取维护设置…</p>
      <form v-else-if="maintenanceLoaded" class="ops-form" @submit.prevent="saveMaintenance"><FormField v-model="maintenance.enabled" type="checkbox" label="开启维护模式" /><FormField v-model="maintenance.message" label="维护公告" :max="500" /><FormField v-model="maintenance.retryAfterSeconds" type="number" label="重试间隔（秒）" :min="1" :max="3600" /><UiButton type="submit" variant="primary" :loading="loading.saveMaintenance">保存维护设置</UiButton></form>
    </section>

    <section class="settings-panel ops-panel" aria-labelledby="ops-reports"><div class="ops-heading"><h2 id="ops-reports">图片举报</h2><div class="inline-actions"><UiSelect :model-value="reportStatus" :options="[{label:'待处理',value:'open'},{label:'已解决',value:'resolved'},{label:'已驳回',value:'dismissed'}]" aria-label="举报状态" @update:model-value="changeReportStatus" /><UiButton :loading="loading.reports" @click="loadReports">刷新</UiButton></div></div>
      <p v-if="errors.reports" role="alert" class="form-error">{{ errors.reports }}</p><p v-else-if="loading.reports && !reports.length" role="status">正在读取举报…</p><p v-else-if="!reports.length" class="empty">当前状态下暂无举报。</p>
      <div v-else class="ops-list"><article v-for="report in reports" :key="report.id"><strong>图片 {{ report.imageId }}</strong><span>{{ reasonLabels[report.reason] || report.reason }} · {{ statusLabels[report.status] || report.status }}</span><p v-if="report.details">{{ report.details }}</p><small>{{ report.createdAt }} · {{ report.ip }}</small><div class="inline-actions"><UiButton v-if="report.status!=='resolved'" :disabled="loading.reportId!==null" @click="setReportStatus(report,'resolved')">标记已解决</UiButton><UiButton v-if="report.status!=='dismissed'" :disabled="loading.reportId!==null" @click="setReportStatus(report,'dismissed')">驳回</UiButton><UiButton v-if="report.status!=='open'" :disabled="loading.reportId!==null" @click="setReportStatus(report,'open')">重新打开</UiButton></div></article></div>
      <div class="ops-pages"><span>第 {{ reportPage }} 页 · 共 {{ reportTotal }} 项</span><UiButton :disabled="loading.reports || reportPage<=1" @click="reportMove(-1)">上一页</UiButton><UiButton :disabled="loading.reports || reportPage*pageSize>=reportTotal" @click="reportMove(1)">下一页</UiButton></div>
    </section>

    <section class="settings-panel ops-panel" aria-labelledby="ops-audit"><div class="ops-heading"><h2 id="ops-audit">管理员操作记录</h2><UiButton :loading="loading.audit" @click="loadAudit">刷新</UiButton></div>
      <p v-if="errors.audit" role="alert" class="form-error">{{ errors.audit }}</p><p v-else-if="loading.audit && !audit.length" role="status">正在读取操作记录…</p><p v-else-if="!audit.length" class="empty">暂无操作记录。</p>
      <div v-else class="ops-list"><article v-for="event in audit" :key="event.id"><strong>{{ event.method }} {{ event.path }}</strong><span>HTTP {{ event.status }}</span><small>{{ event.createdAt }} · {{ event.actorId }} · {{ event.ip }}</small></article></div>
      <div class="ops-pages"><span>第 {{ auditPage }} 页 · 共 {{ auditTotal }} 项</span><UiButton :disabled="loading.audit || auditPage<=1" @click="auditMove(-1)">上一页</UiButton><UiButton :disabled="loading.audit || auditPage*pageSize>=auditTotal" @click="auditMove(1)">下一页</UiButton></div>
    </section>
  </section>
</template>

<style scoped>
.operations-view {display:grid;gap:20px}.ops-panel {min-width:0}.ops-heading {display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:16px}.ops-heading h2 {margin:0}.ops-heading .ui-select {width:170px}.ops-list {display:grid;gap:8px}.ops-list article {display:flex;align-items:center;gap:10px;flex-wrap:wrap;padding:12px;border:1px solid var(--border);border-radius:8px;overflow-wrap:anywhere}.ops-list strong {font-size:13px}.ops-list span,.ops-list small {font-size:12px;color:var(--secondary)}.ops-list p {width:100%;margin:0;font-size:13px}.ops-list .inline-actions {width:100%}.ops-queue {display:grid;gap:8px}.ops-queue>div {display:flex;gap:12px;flex-wrap:wrap;font-size:13px}.ops-queue span {color:var(--secondary)}.ops-pages {display:flex;align-items:center;gap:8px;justify-content:flex-end;margin-top:16px;font-size:12px;color:var(--secondary)}.ops-form {display:grid;gap:14px;max-width:560px}.ops-form>.ui-button {justify-self:start}@media(max-width:640px){.ops-heading,.ops-pages {align-items:flex-start;flex-wrap:wrap}.ops-heading .inline-actions {flex-wrap:wrap}.ops-heading .ui-select {width:100%}}
</style>
