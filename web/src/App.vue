<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import {
  AlertCircle,
  CalendarClock,
  CheckCircle2,
  Copy,
  Folder,
  Inbox,
  KeyRound,
  Loader2,
  Mail,
  Pause,
  Play,
  Plus,
  RefreshCw,
  Settings2,
  ShieldCheck,
  Trash2,
  UserRound,
  X,
} from "lucide-vue-next";

type ApiResponse<T> = {
  success: boolean;
  message?: string;
  data?: T;
};

type Account = {
  id: string;
  name: string;
  real_email?: string;
  icloud_email?: string;
  host?: string;
  status?: string;
  alias_total?: number;
  alias_active?: number;
  last_validated?: string;
};

type Alias = {
  email: string;
  anonymousId: string;
  label: string;
  active: boolean;
  createdAt?: string;
};

type Message = {
  id: string;
  uid?: string;
  folder?: string;
  from: string;
  to: string;
  subject: string;
  date: string;
  preview: string;
  body?: string;
};

type FolderOption = {
  name: string;
  role: string;
};

type AliasesData = {
  account_id: string;
  count: number;
  aliases: Alias[] | null;
};

type MailboxesData = {
  account_id: string;
  folders: FolderOption[] | null;
};

type InboxData = {
  account_id: string;
  alias?: string;
  folder?: string;
  count: number;
  method: string;
  messages: Message[] | null;
};

type CreateData = {
  email: string;
  label: string;
  created_at: string;
  account_id: string;
};

type BatchCreateData = {
  account_id: string;
  requested: number;
  created: CreateData[];
  created_count: number;
  skipped_count: number;
  remaining_this_hour: number;
  message?: string;
  last_error?: string;
};

type CreateJob = {
  id: string;
  account_id: string;
  label_prefix?: string;
  mode: "duration" | "daily_window";
  status: "running" | "paused" | "completed" | "error";
  duration_hours?: number;
  start_time?: string;
  end_time?: string;
  created_count: number;
  last_error?: string;
  started_at?: string;
  ended_at?: string;
  next_run_at?: string;
  created_at: string;
  updated_at: string;
};

type CreateJobsData = {
  jobs: CreateJob[];
  remaining_this_hour?: number;
};

type AppTab = "account" | "create" | "aliases" | "inbox" | "codes" | "settings";

const defaultFolders: FolderOption[] = [
  { name: "all", role: "all" },
  { name: "INBOX", role: "inbox" },
  { name: "Junk", role: "junk" },
];

const navItems: { id: AppTab; label: string }[] = [
  { id: "account", label: "账户" },
  { id: "create", label: "生成邮箱" },
  { id: "aliases", label: "邮箱列表" },
  { id: "inbox", label: "收件箱" },
  { id: "codes", label: "验证码" },
  { id: "settings", label: "设置" },
];

const pageTitles: Record<AppTab, string> = {
  account: "账户",
  create: "生成邮箱",
  aliases: "邮箱列表",
  inbox: "收件箱邮件",
  codes: "验证码",
  settings: "设置",
};

const accounts = ref<Account[]>([]);
const aliases = ref<Alias[]>([]);
const folders = ref<FolderOption[]>(defaultFolders);
const messages = ref<Message[]>([]);
const selectedAccountId = ref("");
const selectedAlias = ref("");
const selectedFolder = ref("all");
const selectedMessageId = ref("");
const newLabel = ref("");
const batchCount = ref(5);
const mailLimit = ref(10);
const onlyUnread = ref(false);
const onlyHideMyEmail = ref(false);
const mailModalOpen = ref(false);
const createJobs = ref<CreateJob[]>([]);
const remainingThisHour = ref(5);
const jobMode = ref<"duration" | "daily_window">("duration");
const durationHours = ref(12);
const dailyStart = ref("09:00");
const dailyEnd = ref("18:00");
const jobLabelPrefix = ref("自动创建");
const activeTab = ref<AppTab>("inbox");
const notice = ref("");
const error = ref("");
const busy = ref({
  accounts: false,
  aliases: false,
  folders: false,
  create: false,
  batch: false,
  jobs: false,
  jobAction: false,
  inbox: false,
});
const inboxMeta = ref({ method: "", count: 0, folder: "all" });

const activeAccount = computed(() =>
  accounts.value.find((account) => account.id === selectedAccountId.value),
);
const activeAliases = computed(() => aliases.value.filter((item) => item.active).length);
const inactiveAliases = computed(() => aliases.value.length - activeAliases.value);
const selectedAliasInfo = computed(() =>
  aliases.value.find((item) => item.email === selectedAlias.value),
);
const folderOptions = computed(() => {
  const byName = new Map<string, FolderOption>();
  for (const item of defaultFolders) byName.set(item.name, item);
  for (const item of folders.value) {
    if (item.role === "inbox" || item.role === "junk") {
      byName.set(item.name, item);
    }
  }
  return Array.from(byName.values());
});
const accountDisplayEmail = computed(
  () => activeAccount.value?.icloud_email || activeAccount.value?.real_email || "未设置邮箱",
);
const cookieStatusLabel = computed(() => (selectedAccountId.value ? "已配置" : "未配置"));
const inboxStatusLabel = computed(() => (selectedAccountId.value ? "已配置" : "未配置"));
const pageTitle = computed(() => pageTitles[activeTab.value]);
const pageSubtitle = computed(() => {
  if (activeTab.value === "inbox" && selectedAliasInfo.value) {
    return `Hide My Email · ${selectedAliasInfo.value.email}`;
  }
  if (activeTab.value === "inbox") {
    return `Hide My Email · ${accountDisplayEmail.value}`;
  }
  if (activeTab.value === "create") {
    return `本小时共享额度 ${remainingThisHour.value} / 5`;
  }
  if (activeTab.value === "aliases") {
    return `${aliases.value.length} 个别名 · ${activeAliases.value} 个启用`;
  }
  if (activeTab.value === "codes") {
    return `${extractedCodes.value.length} 个可能验证码`;
  }
  return accountDisplayEmail.value;
});
const visibleMessages = computed(() => {
  let list = messages.value;
  if (onlyHideMyEmail.value) {
    list = list.filter(isHideMyEmailMessage);
  }
  if (onlyUnread.value) {
    list = list.filter(isUnreadMessage);
  }
  const limit = Math.min(100, Math.max(1, Number(mailLimit.value) || 10));
  return list.slice(0, limit);
});
const activeMessage = computed(() => {
  if (visibleMessages.value.length === 0) return undefined;
  return (
    visibleMessages.value.find((message) => message.id === selectedMessageId.value) ||
    visibleMessages.value[0]
  );
});
const modalMessage = computed(() => (mailModalOpen.value ? activeMessage.value : undefined));
const modalCode = computed(() => extractVerificationCode(modalMessage.value));
const modalBody = computed(() => modalMessage.value?.body || modalMessage.value?.preview || "无正文摘要");
const extractedCodes = computed(() =>
  visibleMessages.value
    .map((message) => ({ message, code: extractVerificationCode(message) }))
    .filter((item) => item.code),
);

async function api<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
    ...init,
  });
  const body = (await response.json()) as ApiResponse<T>;
  if (!response.ok || !body.success) {
    throw new Error(body.message || `请求失败: ${response.status}`);
  }
  return body.data as T;
}

function setError(err: unknown) {
  error.value = err instanceof Error ? err.message : String(err);
}

function clearFeedback() {
  error.value = "";
  notice.value = "";
}

async function loadAccounts() {
  busy.value.accounts = true;
  clearFeedback();
  try {
    const data = await api<Account[]>("/api/accounts");
    accounts.value = data;
    if (!selectedAccountId.value && data.length > 0) {
      selectedAccountId.value = data[0].id;
    }
  } catch (err) {
    setError(err);
  } finally {
    busy.value.accounts = false;
  }
}

async function loadMailboxes() {
  if (!selectedAccountId.value) return;
  busy.value.folders = true;
  try {
    const data = await api<MailboxesData>(`/api/mailboxes?account_id=${selectedAccountId.value}`);
    folders.value = data.folders?.length ? data.folders : defaultFolders;
  } catch {
    folders.value = defaultFolders;
  } finally {
    busy.value.folders = false;
  }
}

async function loadAliases() {
  if (!selectedAccountId.value) return;
  busy.value.aliases = true;
  clearFeedback();
  try {
    const data = await api<AliasesData>(`/api/aliases?account_id=${selectedAccountId.value}`);
    aliases.value = data.aliases ?? [];
    if (selectedAlias.value && !aliases.value.some((item) => item.email === selectedAlias.value)) {
      selectedAlias.value = "";
    }
  } catch (err) {
    setError(err);
  } finally {
    busy.value.aliases = false;
  }
}

async function loadCreateJobs() {
  if (!selectedAccountId.value) return;
  busy.value.jobs = true;
  try {
    const data = await api<CreateJobsData>(`/api/create/jobs?account_id=${selectedAccountId.value}`);
    createJobs.value = data.jobs ?? [];
    remainingThisHour.value = data.remaining_this_hour ?? remainingThisHour.value;
  } catch (err) {
    setError(err);
  } finally {
    busy.value.jobs = false;
  }
}

async function createAlias() {
  if (!selectedAccountId.value) return;
  busy.value.create = true;
  clearFeedback();
  try {
    const label = newLabel.value.trim() || `Web 管理台 ${new Date().toLocaleString()}`;
    const created = await api<CreateData>("/api/create", {
      method: "POST",
      body: JSON.stringify({ account_id: selectedAccountId.value, label }),
    });
    notice.value = `已创建 ${created.email}`;
    selectedAlias.value = created.email;
    selectedFolder.value = "all";
    newLabel.value = "";
    await loadAliases();
    await loadCreateJobs();
    await loadInbox(created.email);
    activeTab.value = "inbox";
  } catch (err) {
    setError(err);
  } finally {
    busy.value.create = false;
  }
}

async function createAliasBatch() {
  if (!selectedAccountId.value) return;
  busy.value.batch = true;
  clearFeedback();
  try {
    const labelPrefix = newLabel.value.trim() || `Web 管理台 ${new Date().toLocaleString()}`;
    const data = await api<BatchCreateData>("/api/create/batch", {
      method: "POST",
      body: JSON.stringify({
        account_id: selectedAccountId.value,
        count: Math.min(5, Math.max(1, Number(batchCount.value) || 1)),
        label_prefix: labelPrefix,
      }),
    });
    remainingThisHour.value = data.remaining_this_hour;
    const lastCreated = data.created.at(-1);
    if (lastCreated) {
      selectedAlias.value = lastCreated.email;
      selectedFolder.value = "all";
    }
    notice.value =
      data.message ||
      `已创建 ${data.created_count} 个别名${data.skipped_count ? `，跳过 ${data.skipped_count} 个` : ""}`;
    await loadAliases();
    await loadCreateJobs();
    if (lastCreated) {
      await loadInbox(lastCreated.email);
      activeTab.value = "inbox";
    }
  } catch (err) {
    setError(err);
  } finally {
    busy.value.batch = false;
  }
}

async function saveCreateJob() {
  if (!selectedAccountId.value) return;
  busy.value.jobAction = true;
  clearFeedback();
  try {
    const body =
      jobMode.value === "duration"
        ? {
            account_id: selectedAccountId.value,
            label_prefix: jobLabelPrefix.value.trim() || "自动创建",
            mode: jobMode.value,
            duration_hours: Math.max(1, Number(durationHours.value) || 1),
          }
        : {
            account_id: selectedAccountId.value,
            label_prefix: jobLabelPrefix.value.trim() || "自动创建",
            mode: jobMode.value,
            start_time: dailyStart.value,
            end_time: dailyEnd.value,
          };
    const job = await api<CreateJob>("/api/create/jobs", {
      method: "POST",
      body: JSON.stringify(body),
    });
    notice.value = `任务已保存：${job.id}`;
    await loadCreateJobs();
  } catch (err) {
    setError(err);
  } finally {
    busy.value.jobAction = false;
  }
}

async function pauseCreateJob(job: CreateJob) {
  await updateCreateJobStatus(job, "pause");
}

async function resumeCreateJob(job: CreateJob) {
  await updateCreateJobStatus(job, "resume");
}

async function updateCreateJobStatus(job: CreateJob, action: "pause" | "resume") {
  busy.value.jobAction = true;
  clearFeedback();
  try {
    await api<CreateJob>(`/api/create/jobs/${job.id}/${action}`, { method: "POST" });
    notice.value = action === "pause" ? "任务已暂停" : "任务已恢复";
    await loadCreateJobs();
  } catch (err) {
    setError(err);
  } finally {
    busy.value.jobAction = false;
  }
}

async function deleteCreateJob(job: CreateJob) {
  busy.value.jobAction = true;
  clearFeedback();
  try {
    await api<{ id: string }>(`/api/create/jobs/${job.id}`, { method: "DELETE" });
    notice.value = "任务已删除";
    await loadCreateJobs();
  } catch (err) {
    setError(err);
  } finally {
    busy.value.jobAction = false;
  }
}

async function loadInbox(alias = selectedAlias.value) {
  if (!selectedAccountId.value) return;
  busy.value.inbox = true;
  mailModalOpen.value = false;
  clearFeedback();
  try {
    const params = new URLSearchParams({
      account_id: selectedAccountId.value,
      limit: String(Math.min(100, Math.max(1, Number(mailLimit.value) || 10))),
      days: "30",
      folder: selectedFolder.value,
    });
    if (alias) params.set("alias", alias);
    const data = await api<InboxData>(`/api/inbox?${params.toString()}`);
    messages.value = data.messages ?? [];
    inboxMeta.value = {
      method: data.method || "unknown",
      count: data.count,
      folder: data.folder || selectedFolder.value,
    };
    if (!messages.value.some((message) => message.id === selectedMessageId.value)) {
      selectedMessageId.value = messages.value[0]?.id ?? "";
    }
  } catch (err) {
    setError(err);
  } finally {
    busy.value.inbox = false;
  }
}

async function refreshAll() {
  await loadAccounts();
  await loadMailboxes();
  await loadAliases();
  await loadCreateJobs();
  await loadInbox(selectedAlias.value);
}

async function handleAccountChange() {
  selectedAlias.value = "";
  selectedMessageId.value = "";
  mailModalOpen.value = false;
  await loadMailboxes();
  await loadAliases();
  await loadCreateJobs();
  await loadInbox();
}

async function chooseAlias(alias: Alias) {
  selectedAlias.value = alias.email;
  selectedFolder.value = "all";
  activeTab.value = "inbox";
  await loadInbox(alias.email);
}

function selectMessage(message: Message) {
  selectedMessageId.value = message.id;
  mailModalOpen.value = true;
}

function closeMailModal() {
  mailModalOpen.value = false;
}

async function copyText(text: string) {
  await navigator.clipboard.writeText(text);
  notice.value = "已复制到剪贴板";
}

function clearAliasSelection() {
  selectedAlias.value = "";
  selectedMessageId.value = "";
  mailModalOpen.value = false;
  activeTab.value = "inbox";
  void loadInbox();
}

function formatDate(value?: string) {
  if (!value) return "未知";
  const asNumber = Number(value);
  const date = Number.isFinite(asNumber) && value.length > 10 ? new Date(asNumber) : new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function formatMessageTime(value?: string) {
  if (!value) return "未知";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return formatDate(value);
  return new Intl.DateTimeFormat("en-GB", {
    day: "2-digit",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

function folderLabel(optionOrName?: FolderOption | string) {
  const option =
    typeof optionOrName === "string"
      ? folderOptions.value.find((item) => item.name === optionOrName || item.role === optionOrName)
      : optionOrName;
  const role = option?.role || optionOrName;
  if (role === "all") return "全部";
  if (role === "inbox") return "收件箱";
  if (role === "junk") return "垃圾邮件";
  return option?.name || String(optionOrName || "未知");
}

function jobModeLabel(job: CreateJob) {
  return job.mode === "duration" ? `${job.duration_hours || 0} 小时` : `${job.start_time} - ${job.end_time}`;
}

function jobStatusLabel(status: CreateJob["status"]) {
  if (status === "running") return "运行中";
  if (status === "paused") return "已暂停";
  if (status === "completed") return "已完成";
  if (status === "error") return "异常";
  return status;
}

function senderName(message?: Message) {
  if (!message?.from) return "未知发件人";
  const quoted = message.from.match(/"([^"]+)"/);
  if (quoted?.[1]) return quoted[1];
  return message.from.split("<")[0].trim() || message.from;
}

function isHideMyEmailMessage(message: Message) {
  const text = `${message.to || ""} ${message.from || ""}`.toLowerCase();
  return text.includes("@icloud.com") || Boolean(selectedAlias.value && text.includes(selectedAlias.value.toLowerCase()));
}

function isUnreadMessage(message: Message) {
  const extra = message as Message & { unread?: boolean; read?: boolean; seen?: boolean };
  if (typeof extra.unread === "boolean") return extra.unread;
  if (typeof extra.read === "boolean") return !extra.read;
  if (typeof extra.seen === "boolean") return !extra.seen;
  return true;
}

function extractVerificationCode(message?: Message) {
  if (!message) return "";
  const text = [message.subject, message.preview].filter(Boolean).join("\n");
  const sixDigit = text.match(/(?:^|\D)(\d{6})(?!\d)/);
  if (sixDigit?.[1]) return sixDigit[1];
  const shortCode = text.match(/(?:^|\D)(\d{4,8})(?!\d)/);
  return shortCode?.[1] ?? "";
}

onMounted(async () => {
  await loadAccounts();
  await loadMailboxes();
  await loadAliases();
  await loadCreateJobs();
  await loadInbox();
});
</script>

<template>
  <main class="app-shell">
    <div class="shell-inner">
      <header class="masthead">
        <div class="brand">
          <span class="brand-mark" aria-hidden="true"><span></span></span>
          <h1>iCloud+ 隐藏邮箱</h1>
        </div>
        <div class="status-cluster">
          <span class="status-pill" :class="{ ok: cookieStatusLabel === '已配置' }">
            <span class="pill-dot"></span>
            Cookie: {{ cookieStatusLabel }}
          </span>
          <span class="status-pill" :class="{ ok: inboxStatusLabel === '已配置' }">
            <span class="pill-dot"></span>
            收件箱: {{ inboxStatusLabel }}
          </span>
          <span class="status-pill muted">已生成: {{ aliases.length }}</span>
          <button class="icon-button" type="button" :disabled="busy.accounts" aria-label="刷新" title="刷新" @click="refreshAll">
            <RefreshCw :class="{ spin: busy.accounts || busy.aliases || busy.inbox }" :size="17" />
          </button>
        </div>
      </header>

      <nav class="tabbar" aria-label="主导航" role="tablist">
        <button
          v-for="item in navItems"
          :key="item.id"
          class="app-tab"
          :class="{ active: activeTab === item.id, quiet: item.id === 'aliases' && activeTab !== item.id }"
          type="button"
          role="tab"
          :aria-selected="activeTab === item.id"
          @click="activeTab = item.id"
        >
          {{ item.label }}
        </button>
      </nav>

      <section v-if="error || notice" class="feedback" :class="{ danger: error }">
        <AlertCircle v-if="error" :size="18" />
        <CheckCircle2 v-else :size="18" />
        <span>{{ error || notice }}</span>
      </section>

      <section class="page-head">
        <div class="title-group">
          <h2>{{ pageTitle }}</h2>
          <p>{{ pageSubtitle }}</p>
        </div>
        <span class="quota-pill">本小时可创建 <strong>{{ remainingThisHour }}</strong> / 5</span>
      </section>

      <section v-if="activeTab === 'account'" class="account-view">
        <article class="surface account-selector">
          <div class="surface-title">
            <UserRound :size="19" />
            <span>当前账号</span>
          </div>
          <select id="account" v-model="selectedAccountId" class="select" @change="handleAccountChange">
            <option v-for="account in accounts" :key="account.id" :value="account.id">
              {{ account.name || account.id }}
            </option>
          </select>
          <div v-if="activeAccount" class="account-details">
            <span class="status-line">
              <span class="status-dot" :class="{ active: activeAccount.status === 'active' }"></span>
              {{ activeAccount.status || "unknown" }}
            </span>
            <strong>{{ accountDisplayEmail }}</strong>
            <small>{{ activeAccount.host || "iCloud" }}</small>
          </div>
        </article>

        <article class="stat-tile">
          <strong>{{ aliases.length }}</strong>
          <span>全部别名</span>
        </article>
        <article class="stat-tile">
          <strong>{{ activeAliases }}</strong>
          <span>启用中</span>
        </article>
        <article class="stat-tile">
          <strong>{{ inactiveAliases }}</strong>
          <span>已停用</span>
        </article>
      </section>

      <section v-if="activeTab === 'create'" class="create-view">
        <article class="surface create-surface">
          <div class="surface-heading">
            <div class="surface-title">
              <Mail :size="19" />
              <span>生成邮箱</span>
            </div>
            <span class="soft-tag">共享额度 {{ remainingThisHour }} / 5</span>
          </div>

          <form class="create-row" @submit.prevent="createAlias">
            <input
              v-model="newLabel"
              class="input"
              placeholder="新别名标签，例如 GitHub 注册"
              aria-label="新别名标签"
            />
            <button class="primary-button" type="submit" :disabled="busy.create || !selectedAccountId">
              <Loader2 v-if="busy.create" class="spin" :size="17" />
              <Plus v-else :size="17" />
              创建别名
            </button>
          </form>

          <div class="batch-row">
            <label class="compact-field">
              <span>数量</span>
              <input v-model.number="batchCount" class="input compact-input" type="number" min="1" max="5" />
            </label>
            <button class="secondary-button" type="button" :disabled="busy.batch || !selectedAccountId" @click="createAliasBatch">
              <Loader2 v-if="busy.batch" class="spin" :size="16" />
              <Plus v-else :size="16" />
              批量创建
            </button>
          </div>
        </article>

        <article class="surface schedule-surface">
          <div class="surface-heading">
            <div class="surface-title">
              <CalendarClock :size="19" />
              <span>自动创建</span>
            </div>
            <button class="secondary-button small-button" type="button" :disabled="busy.jobs" @click="loadCreateJobs">
              <RefreshCw :class="{ spin: busy.jobs }" :size="15" />
              更新任务
            </button>
          </div>

          <div class="job-form">
            <input v-model="jobLabelPrefix" class="input" placeholder="任务标签前缀" aria-label="任务标签前缀" />
            <div class="segmented-control" aria-label="任务模式">
              <button
                type="button"
                :class="{ active: jobMode === 'duration' }"
                @click="jobMode = 'duration'"
              >
                运行时长
              </button>
              <button
                type="button"
                :class="{ active: jobMode === 'daily_window' }"
                @click="jobMode = 'daily_window'"
              >
                每日时段
              </button>
            </div>

            <div v-if="jobMode === 'duration'" class="time-row">
              <label class="compact-field">
                <span>小时</span>
                <input v-model.number="durationHours" class="input compact-input" type="number" min="1" />
              </label>
            </div>
            <div v-else class="time-row">
              <label class="compact-field grow">
                <span>开始</span>
                <input v-model="dailyStart" class="input" type="time" />
              </label>
              <label class="compact-field grow">
                <span>结束</span>
                <input v-model="dailyEnd" class="input" type="time" />
              </label>
            </div>

            <button class="primary-button full-button" type="button" :disabled="busy.jobAction || !selectedAccountId" @click="saveCreateJob">
              <Loader2 v-if="busy.jobAction" class="spin" :size="17" />
              <Plus v-else :size="17" />
              保存任务
            </button>
          </div>

          <div v-if="busy.jobs" class="empty-state compact-empty">正在读取自动任务...</div>
          <div v-else-if="createJobs.length === 0" class="empty-state compact-empty">当前账号没有自动创建任务。</div>
          <div v-else class="job-list">
            <article v-for="job in createJobs" :key="job.id" class="job-row">
              <div class="job-main">
                <strong>{{ job.label_prefix || "自动创建" }}</strong>
                <small>{{ jobModeLabel(job) }}</small>
                <small v-if="job.next_run_at">下次：{{ formatDate(job.next_run_at) }}</small>
                <small v-if="job.last_error" class="danger-text">{{ job.last_error }}</small>
              </div>
              <div class="job-meta">
                <span class="soft-tag" :class="{ muted: job.status !== 'running' }">{{ jobStatusLabel(job.status) }}</span>
                <small>{{ job.created_count }} 个</small>
                <div class="job-actions">
                  <button
                    v-if="job.status === 'running'"
                    class="mini-button"
                    type="button"
                    :disabled="busy.jobAction"
                    title="暂停"
                    @click="pauseCreateJob(job)"
                  >
                    <Pause :size="14" />
                  </button>
                  <button
                    v-else-if="job.status === 'paused' || job.status === 'error'"
                    class="mini-button"
                    type="button"
                    :disabled="busy.jobAction"
                    title="恢复"
                    @click="resumeCreateJob(job)"
                  >
                    <Play :size="14" />
                  </button>
                  <button
                    class="mini-button danger-button"
                    type="button"
                    :disabled="busy.jobAction"
                    title="删除"
                    @click="deleteCreateJob(job)"
                  >
                    <Trash2 :size="14" />
                  </button>
                </div>
              </div>
            </article>
          </div>
        </article>
      </section>

      <section v-if="activeTab === 'aliases'" class="alias-view">
        <div class="view-toolbar">
          <span class="toolbar-note">{{ aliases.length }} 个别名</span>
          <button class="secondary-button" type="button" :disabled="busy.aliases" @click="loadAliases">
            <RefreshCw :class="{ spin: busy.aliases }" :size="16" />
            更新列表
          </button>
        </div>

        <div v-if="busy.aliases" class="empty-state">正在读取别名列表...</div>
        <div v-else-if="aliases.length === 0" class="empty-state">当前账号还没有隐私邮箱别名。</div>
        <div v-else class="alias-list">
          <button
            v-for="alias in aliases"
            :key="alias.anonymousId || alias.email"
            class="alias-row"
            :class="{ selected: selectedAlias === alias.email }"
            type="button"
            @click="chooseAlias(alias)"
          >
            <span class="alias-main">
              <strong>{{ alias.email }}</strong>
              <small>{{ alias.label || "未命名" }}</small>
            </span>
            <span class="alias-meta">
              <span class="soft-tag" :class="{ muted: !alias.active }">
                {{ alias.active ? "启用" : "停用" }}
              </span>
              <small>{{ formatDate(alias.createdAt) }}</small>
            </span>
          </button>
        </div>
      </section>

      <section v-if="activeTab === 'inbox'" class="inbox-view">
        <div class="mail-toolbar">
          <label class="compact-field limit-field">
            <span>数量</span>
            <input v-model.number="mailLimit" class="input compact-input" type="number" min="1" max="100" />
          </label>
          <label class="check-field">
            <input v-model="onlyUnread" type="checkbox" />
            <span>只看未读</span>
          </label>
          <label class="check-field">
            <input v-model="onlyHideMyEmail" type="checkbox" />
            <span>只看隐藏邮箱</span>
          </label>
          <button class="primary-button fetch-button" type="button" :disabled="busy.inbox" @click="loadInbox()">
            <Loader2 v-if="busy.inbox" class="spin" :size="17" />
            <Inbox v-else :size="17" />
            拉取邮件
          </button>
        </div>

        <div v-if="selectedAliasInfo" class="selected-alias">
          <span>{{ selectedAliasInfo.email }}</span>
          <button class="mini-text-button" type="button" @click="copyText(selectedAliasInfo.email)">
            <Copy :size="14" />
            复制
          </button>
          <button class="mini-text-button" type="button" @click="clearAliasSelection">查看全部邮件</button>
        </div>

        <div v-if="busy.inbox" class="empty-state">正在读取邮件...</div>
        <div v-else-if="visibleMessages.length === 0" class="empty-state">
          {{ selectedAlias ? "这个别名在当前文件夹范围内未读取到邮件。" : "当前范围暂无可显示邮件。" }}
        </div>
        <section v-else class="mail-list">
          <button
            v-for="message in visibleMessages"
            :key="message.id"
            class="mail-row"
            :class="{ selected: activeMessage?.id === message.id }"
            type="button"
            @click="selectMessage(message)"
          >
            <span class="avatar">”</span>
            <span class="mail-copy">
              <span class="mail-title">
                <strong>{{ senderName(message) }}</strong>
                <span v-if="isHideMyEmailMessage(message)" class="soft-tag blue">隐藏邮箱</span>
                <span class="mail-subject">{{ message.subject || "无主题" }}</span>
              </span>
              <span class="preview">{{ message.preview || "无正文摘要" }}</span>
            </span>
            <time>{{ formatMessageTime(message.date) }}</time>
          </button>
        </section>
      </section>

      <section v-if="activeTab === 'codes'" class="code-view">
        <div v-if="visibleMessages.length === 0" class="empty-state">当前没有可提取验证码的邮件。</div>
        <div v-else-if="extractedCodes.length === 0" class="empty-state">当前邮件没有识别到验证码。</div>
        <div v-else class="surface code-panel">
          <div class="code-grid">
            <article v-for="item in extractedCodes" :key="item.message.id" class="code-card">
              <div class="code-card-head">
                <span class="soft-tag blue">验证码</span>
                <strong>{{ item.code }}</strong>
              </div>
              <p class="code-subject">{{ item.message.subject || "无主题" }}</p>
              <small class="code-meta">{{ senderName(item.message) }} · {{ formatMessageTime(item.message.date) }}</small>
              <button class="mini-text-button code-copy" type="button" @click="copyText(item.code)">
                <Copy :size="14" />
                复制
              </button>
            </article>
          </div>
        </div>
      </section>

      <section v-if="activeTab === 'settings'" class="settings-view">
        <article class="surface settings-panel">
          <div class="surface-title">
            <Settings2 :size="19" />
            <span>服务状态</span>
          </div>
          <label class="settings-field">
            <Folder :size="16" />
            <select v-model="selectedFolder" class="select" :disabled="busy.folders" @change="loadInbox()">
              <option v-for="folder in folderOptions" :key="folder.name" :value="folder.name">
                {{ folderLabel(folder) }}
              </option>
            </select>
          </label>
          <div class="settings-grid">
            <div>
              <span>Cookie</span>
              <strong>{{ cookieStatusLabel }}</strong>
            </div>
            <div>
              <span>收件箱</span>
              <strong>{{ inboxStatusLabel }}</strong>
            </div>
            <div>
              <span>邮件来源</span>
              <strong>{{ inboxMeta.method || "未读取" }}</strong>
            </div>
            <div>
              <span>自动任务</span>
              <strong>{{ createJobs.length }}</strong>
            </div>
          </div>
          <button class="primary-button settings-refresh" type="button" :disabled="busy.accounts" @click="refreshAll">
            <RefreshCw :class="{ spin: busy.accounts || busy.aliases || busy.inbox }" :size="17" />
            刷新状态
          </button>
        </article>

        <article class="surface settings-panel">
          <div class="surface-title">
            <ShieldCheck :size="19" />
            <span>账号信息</span>
          </div>
          <dl class="settings-list">
            <dt>账号</dt>
            <dd>{{ activeAccount?.name || selectedAccountId || "未选择" }}</dd>
            <dt>邮箱</dt>
            <dd>{{ accountDisplayEmail }}</dd>
            <dt>主机</dt>
            <dd>{{ activeAccount?.host || "iCloud" }}</dd>
          </dl>
        </article>

        <article class="surface settings-panel">
          <div class="surface-title">
            <KeyRound :size="19" />
            <span>额度</span>
          </div>
          <div class="quota-large">
            <strong>{{ remainingThisHour }}</strong>
            <span>本小时剩余</span>
          </div>
        </article>
      </section>

      <div
        v-if="mailModalOpen && modalMessage"
        class="mail-modal-backdrop"
        role="presentation"
        @click.self="closeMailModal"
      >
        <section class="mail-modal" role="dialog" aria-modal="true" aria-labelledby="mail-modal-title">
          <header class="mail-modal-header">
            <div class="mail-modal-title-group">
              <span class="soft-tag blue">邮件详情</span>
              <h3 id="mail-modal-title">{{ modalMessage.subject || "无主题" }}</h3>
              <p>{{ senderName(modalMessage) }} · {{ formatMessageTime(modalMessage.date) }}</p>
            </div>
            <button class="icon-button mail-modal-close" type="button" aria-label="关闭邮件详情" title="关闭" @click="closeMailModal">
              <X :size="18" />
            </button>
          </header>

          <div class="mail-modal-scroll">
            <dl class="message-meta mail-modal-meta">
              <dt>主题:</dt>
              <dd>{{ modalMessage.subject || "无主题" }}</dd>
              <dt>发件人:</dt>
              <dd>{{ modalMessage.from || "未知" }}</dd>
              <dt>收件人:</dt>
              <dd>{{ modalMessage.to || "未知" }}</dd>
              <dt>时间:</dt>
              <dd>{{ formatDate(modalMessage.date) }}</dd>
            </dl>

            <div v-if="modalCode" class="code-line mail-modal-code">
              <strong>可能验证码:</strong>
              <span>{{ modalCode }}</span>
            </div>

            <div class="mail-modal-body">
              <p>{{ modalBody }}</p>
            </div>
          </div>
        </section>
      </div>
    </div>
  </main>
</template>
