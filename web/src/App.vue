<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
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
  has_cookies?: boolean;
  has_app_password?: boolean;
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
  unread?: boolean;
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
  warning?: string;
};

type MessageBatchData = {
  account_id: string;
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
  message: false,
  prefetch: false,
});
const inboxMeta = ref({ method: "", count: 0, folder: "all" });
const inboxAlias = ref("");
const prefetchSeq = ref(0);
const apiToken = ref(localStorage.getItem("icloud-prime.api-token") || "");
const tokenDraft = ref(apiToken.value);
const authRequired = ref(false);
const accountFormOpen = ref(false);
const accountActionBusy = ref(false);
const aliasActionBusy = ref(false);
const accountForm = ref({ name: "", real_email: "", host: "icloud.com", proxy: "", cookies: "" });
const cookieInput = ref("");
const passwordForm = ref({ icloud_email: "", app_password: "" });
const loginForm = ref({ password: "", otp_code: "" });
let accountEpoch = 0;
const requestSequences = new Map<string, number>();

function accountContext() {
  const accountID = selectedAccountId.value;
  const epoch = accountEpoch;
  return { accountID, current: () => epoch === accountEpoch && accountID === selectedAccountId.value };
}

function readContext(kind: string) {
  const context = accountContext();
  const sequence = (requestSequences.get(kind) || 0) + 1;
  requestSequences.set(kind, sequence);
  return { ...context, current: () => context.current() && requestSequences.get(kind) === sequence };
}

function resetAccountData() {
  accountEpoch++;
  prefetchSeq.value++;
  aliases.value = [];
  folders.value = defaultFolders;
  messages.value = [];
  createJobs.value = [];
  remainingThisHour.value = 0;
  selectedAlias.value = "";
  selectedFolder.value = "all";
  selectedMessageId.value = "";
  mailModalOpen.value = false;
  inboxMeta.value = { method: "", count: 0, folder: "all" };
  inboxAlias.value = "";
  cookieInput.value = "";
  passwordForm.value = { icloud_email: activeAccount.value?.icloud_email || "", app_password: "" };
  loginForm.value = { password: "", otp_code: "" };
  accountActionBusy.value = false;
  aliasActionBusy.value = false;
  for (const key of Object.keys(busy.value) as (keyof typeof busy.value)[]) busy.value[key] = false;
  clearFeedback();
}

watch(selectedAccountId, resetAccountData, { flush: "sync" });

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
const cookieStatusLabel = computed(() => (activeAccount.value?.has_cookies ? "已配置" : "未配置"));
const inboxStatusLabel = computed(() => (activeAccount.value?.has_app_password || activeAccount.value?.has_cookies ? "已配置" : "未配置"));
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
const modalBody = computed(() => {
  if (busy.value.message) return "正在读取正文...";
  if (modalMessage.value?.uid && busy.value.prefetch && !modalMessage.value.body) return "正文后台加载中...";
  return modalMessage.value?.body || modalMessage.value?.preview || "无正文摘要";
});
const extractedCodes = computed(() =>
  visibleMessages.value
    .map((message) => ({ message, code: extractVerificationCode(message) }))
    .filter((item) => item.code),
);

async function api<T>(url: string, init?: RequestInit): Promise<T> {
  const token = apiToken.value;
  const epoch = accountEpoch;
  const headers = new Headers(init?.headers);
  headers.set("Content-Type", "application/json");
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const response = await fetch(url, {
    ...init,
    headers,
  });
  if (response.status === 401 && token === apiToken.value && epoch === accountEpoch && url === "/api/accounts") {
    authRequired.value = true;
    activeTab.value = "settings";
  }
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

function parseCookieInput(input: string): Record<string, string> {
  const value = input.trim();
  if (!value) throw new Error("请输入 Cookie。");
  if (value.startsWith("{")) {
    const parsed: unknown = JSON.parse(value);
    if (!parsed || Array.isArray(parsed) || typeof parsed !== "object" ||
        !Object.keys(parsed).length || Object.values(parsed).some((item) => typeof item !== "string")) {
      throw new Error("Cookie JSON 必须是非空的字符串键值对象。");
    }
    return parsed as Record<string, string>;
  }
  const entries = value.split(";").filter((item) => item.trim()).map((item) => {
    const split = item.indexOf("=");
    if (split < 1 || !item.slice(0, split).trim()) throw new Error("Cookie 格式应为 name=value。");
    return [item.slice(0, split).trim(), item.slice(split + 1).trim()];
  });
  return Object.fromEntries(entries);
}

async function saveApiToken() {
  apiToken.value = tokenDraft.value.trim();
  if (apiToken.value) localStorage.setItem("icloud-prime.api-token", apiToken.value);
  else localStorage.removeItem("icloud-prime.api-token");
  accounts.value = [];
  selectedAccountId.value = "";
  resetAccountData();
  authRequired.value = false;
  await refreshAll();
}

async function addAccount() {
  if (accountActionBusy.value) return;
  const context = accountContext();
  accountActionBusy.value = true;
  clearFeedback();
  try {
    const form = accountForm.value;
    if (form.cookies.trim()) parseCookieInput(form.cookies);
    const created = await api<Account>("/api/accounts", {
      method: "POST",
      body: JSON.stringify({ ...form, name: form.name.trim(), real_email: form.real_email.trim() }),
    });
    if (!context.current()) return;
    accounts.value = [...accounts.value, created];
    selectedAccountId.value = created.id;
    accountForm.value = { name: "", real_email: "", host: "icloud.com", proxy: "", cookies: "" };
    accountFormOpen.value = false;
    await loadAccountData();
    if (selectedAccountId.value === created.id) notice.value = "账号已添加";
  } catch (err) {
    if (context.current()) setError(err);
  } finally {
    if (context.current()) accountActionBusy.value = false;
  }
}

async function removeAccount() {
  const context = accountContext();
  if (!context.accountID || !window.confirm(`删除账号“${activeAccount.value?.name || context.accountID}”？`)) return;
  accountActionBusy.value = true;
  clearFeedback();
  try {
    await api(`/api/accounts/${encodeURIComponent(context.accountID)}`, { method: "DELETE" });
    if (!context.current()) return;
    accounts.value = accounts.value.filter((account) => account.id !== context.accountID);
    selectedAccountId.value = accounts.value[0]?.id || "";
    if (!selectedAccountId.value) accountFormOpen.value = true;
    await loadAccountData({ includeInbox: true });
  } catch (err) {
    if (context.current()) setError(err);
  } finally {
    if (context.current()) accountActionBusy.value = false;
  }
}

async function saveCredentials(kind: "cookies" | "password" | "login") {
  const context = accountContext();
  if (!context.accountID || accountActionBusy.value) return;
  accountActionBusy.value = true;
  clearFeedback();
  try {
    const body = kind === "cookies" ? { cookies: parseCookieInput(cookieInput.value) }
      : kind === "password" ? { ...passwordForm.value } : { ...loginForm.value };
    const result = await api<{ warning?: string }>(`/api/accounts/${encodeURIComponent(context.accountID)}/${kind}`, {
      method: kind === "cookies" ? "PUT" : "POST", body: JSON.stringify(body),
    });
    if (!context.current()) return;
    cookieInput.value = "";
    passwordForm.value.app_password = "";
    loginForm.value = { password: "", otp_code: "" };
    await loadAccounts();
    if (!context.current()) return;
    await loadAccountData({ includeInbox: true, force: true });
    if (context.current()) notice.value = result.warning ? `凭据已保存；${result.warning}` : kind === "login" ? "登录成功" : "凭据已更新";
  } catch (err) {
    if (context.current()) setError(err);
  } finally {
    if (context.current()) accountActionBusy.value = false;
  }
}

async function changeAlias(alias: Alias, action: "deactivate" | "reactivate" | "delete") {
  const context = accountContext();
  if (!context.accountID || aliasActionBusy.value || !aliases.value.includes(alias)) return;
  if (action === "delete" && !window.confirm(`永久删除别名 ${alias.email}？`)) return;
  aliasActionBusy.value = true;
  clearFeedback();
  try {
    await api(`/api/aliases/${encodeURIComponent(alias.anonymousId)}${action === "delete" ? "" : `/${action}`}`, {
      method: action === "delete" ? "DELETE" : "POST", body: JSON.stringify({ account_id: context.accountID }),
    });
    if (!context.current()) return;
    await loadAliases({ force: true });
    if (context.current()) notice.value = action === "delete" ? "别名已删除" : action === "deactivate" ? "别名已停用" : "别名已启用";
  } catch (err) {
    if (context.current()) setError(err);
  } finally {
    if (context.current()) aliasActionBusy.value = false;
  }
}

async function loadAccounts() {
  const context = readContext("accounts");
  busy.value.accounts = true;
  clearFeedback();
  try {
    const data = await api<Account[]>("/api/accounts");
    if (!context.current()) return;
    accounts.value = data;
    authRequired.value = false;
    if (!data.some((account) => account.id === selectedAccountId.value)) {
      selectedAccountId.value = data[0]?.id || "";
    }
    if (data.length === 0) {
      activeTab.value = "account";
      accountFormOpen.value = true;
    }
  } catch (err) {
    if (context.current()) setError(err);
  } finally {
    if (context.current()) busy.value.accounts = false;
  }
}

async function loadMailboxes(options: { force?: boolean } = {}) {
  if (!selectedAccountId.value) return;
  const context = readContext("folders");
  busy.value.folders = true;
  try {
    const params = new URLSearchParams({ account_id: context.accountID });
    if (options.force) params.set("refresh", "1");
    const data = await api<MailboxesData>(`/api/mailboxes?${params.toString()}`);
    if (!context.current()) return;
    folders.value = data.folders?.length ? data.folders : defaultFolders;
  } catch {
    if (context.current()) folders.value = defaultFolders;
  } finally {
    if (context.current()) busy.value.folders = false;
  }
}

async function loadAliases(options: { force?: boolean } = {}) {
  if (!selectedAccountId.value) return;
  const context = readContext("aliases");
  busy.value.aliases = true;
  try {
    const params = new URLSearchParams({ account_id: context.accountID });
    if (options.force) params.set("refresh", "1");
    const data = await api<AliasesData>(`/api/aliases?${params.toString()}`);
    if (!context.current()) return;
    aliases.value = data.aliases ?? [];
    if (selectedAlias.value && !aliases.value.some((item) => item.email === selectedAlias.value)) {
      selectedAlias.value = "";
    }
  } catch (err) {
    if (context.current()) setError(err);
  } finally {
    if (context.current()) busy.value.aliases = false;
  }
}

async function loadCreateJobs() {
  if (!selectedAccountId.value) return;
  const context = readContext("jobs");
  busy.value.jobs = true;
  try {
    const data = await api<CreateJobsData>(`/api/create/jobs?account_id=${encodeURIComponent(context.accountID)}`);
    if (!context.current()) return;
    createJobs.value = data.jobs ?? [];
    remainingThisHour.value = data.remaining_this_hour ?? remainingThisHour.value;
  } catch (err) {
    if (context.current()) setError(err);
  } finally {
    if (context.current()) busy.value.jobs = false;
  }
}

async function loadAccountData(options: { includeInbox?: boolean; withBody?: boolean; force?: boolean } = {}) {
  if (!selectedAccountId.value) return;
  await Promise.all([
    loadMailboxes({ force: options.force }),
    loadAliases({ force: options.force }),
    loadCreateJobs(),
    options.includeInbox ? loadInbox(selectedAlias.value, { prefetchBody: Boolean(options.withBody) }) : Promise.resolve(),
  ]);
}

async function createAlias() {
  if (!selectedAccountId.value) return;
  const context = accountContext();
  busy.value.create = true;
  clearFeedback();
  try {
    const label = newLabel.value.trim() || `Web 管理台 ${new Date().toLocaleString()}`;
    const created = await api<CreateData>("/api/create", {
      method: "POST",
      body: JSON.stringify({ account_id: context.accountID, label }),
    });
    if (!context.current()) return;
    notice.value = `已创建 ${created.email}`;
    selectedAlias.value = created.email;
    selectedFolder.value = "all";
    newLabel.value = "";
    await Promise.all([loadAliases({ force: true }), loadCreateJobs(), loadInbox(created.email)]);
    if (context.current()) activeTab.value = "inbox";
  } catch (err) {
    if (context.current()) setError(err);
  } finally {
    if (context.current()) busy.value.create = false;
  }
}

async function createAliasBatch() {
  if (!selectedAccountId.value) return;
  const context = accountContext();
  busy.value.batch = true;
  clearFeedback();
  try {
    const labelPrefix = newLabel.value.trim() || `Web 管理台 ${new Date().toLocaleString()}`;
    const data = await api<BatchCreateData>("/api/create/batch", {
      method: "POST",
      body: JSON.stringify({
        account_id: context.accountID,
        count: Math.min(5, Math.max(1, Number(batchCount.value) || 1)),
        label_prefix: labelPrefix,
      }),
    });
    if (!context.current()) return;
    remainingThisHour.value = data.remaining_this_hour;
    const lastCreated = data.created?.at(-1);
    if (lastCreated) {
      selectedAlias.value = lastCreated.email;
      selectedFolder.value = "all";
    }
    notice.value =
      data.message ||
      `已创建 ${data.created_count} 个别名${data.skipped_count ? `，跳过 ${data.skipped_count} 个` : ""}`;
    if (data.last_error) notice.value += `；${data.last_error}`;
    await Promise.all([loadAliases({ force: true }), loadCreateJobs()]);
    if (lastCreated && context.current()) {
      await loadInbox(lastCreated.email);
      if (context.current()) activeTab.value = "inbox";
    }
  } catch (err) {
    if (context.current()) setError(err);
  } finally {
    if (context.current()) busy.value.batch = false;
  }
}

async function saveCreateJob() {
  if (!selectedAccountId.value) return;
  const context = accountContext();
  busy.value.jobAction = true;
  clearFeedback();
  try {
    const body =
      jobMode.value === "duration"
        ? {
            account_id: context.accountID,
            label_prefix: jobLabelPrefix.value.trim() || "自动创建",
            mode: jobMode.value,
            duration_hours: Math.max(1, Number(durationHours.value) || 1),
          }
        : {
            account_id: context.accountID,
            label_prefix: jobLabelPrefix.value.trim() || "自动创建",
            mode: jobMode.value,
            start_time: dailyStart.value,
            end_time: dailyEnd.value,
          };
    const job = await api<CreateJob>("/api/create/jobs", {
      method: "POST",
      body: JSON.stringify(body),
    });
    if (!context.current()) return;
    notice.value = `任务已保存：${job.id}`;
    await loadCreateJobs();
  } catch (err) {
    if (context.current()) setError(err);
  } finally {
    if (context.current()) busy.value.jobAction = false;
  }
}

async function pauseCreateJob(job: CreateJob) {
  await updateCreateJobStatus(job, "pause");
}

async function resumeCreateJob(job: CreateJob) {
  await updateCreateJobStatus(job, "resume");
}

async function updateCreateJobStatus(job: CreateJob, action: "pause" | "resume") {
  const context = accountContext();
  if (job.account_id !== context.accountID) return;
  busy.value.jobAction = true;
  clearFeedback();
  try {
    await api<CreateJob>(`/api/create/jobs/${job.id}/${action}`, { method: "POST" });
    if (!context.current()) return;
    notice.value = action === "pause" ? "任务已暂停" : "任务已恢复";
    await loadCreateJobs();
  } catch (err) {
    if (context.current()) setError(err);
  } finally {
    if (context.current()) busy.value.jobAction = false;
  }
}

async function deleteCreateJob(job: CreateJob) {
  const context = accountContext();
  if (job.account_id !== context.accountID || !window.confirm(`删除任务“${job.label_prefix || job.id}”？`)) return;
  busy.value.jobAction = true;
  clearFeedback();
  try {
    await api<{ id: string }>(`/api/create/jobs/${job.id}`, { method: "DELETE" });
    if (!context.current()) return;
    notice.value = "任务已删除";
    await loadCreateJobs();
  } catch (err) {
    if (context.current()) setError(err);
  } finally {
    if (context.current()) busy.value.jobAction = false;
  }
}

async function loadInbox(alias = selectedAlias.value, options: { prefetchBody?: boolean } = {}) {
  if (!selectedAccountId.value) return;
  const context = readContext("inbox");
  const folder = selectedFolder.value;
  prefetchSeq.value++;
  busy.value.prefetch = false;
  busy.value.inbox = true;
  messages.value = [];
  inboxAlias.value = "";
  mailModalOpen.value = false;
  error.value = "";
  try {
    const params = new URLSearchParams({
      account_id: context.accountID,
      limit: String(Math.min(100, Math.max(1, Number(mailLimit.value) || 10))),
      days: "30",
      folder,
    });
    if (alias) params.set("alias", alias);
    const data = await api<InboxData>(`/api/inbox?${params.toString()}`);
    if (!context.current()) return;
    messages.value = data.messages ?? [];
    inboxAlias.value = alias;
    if (data.warning) notice.value = data.warning;
    inboxMeta.value = {
      method: data.method || "unknown",
      count: data.count,
      folder: data.folder || folder,
    };
    if (!messages.value.some((message) => message.id === selectedMessageId.value)) {
      selectedMessageId.value = messages.value[0]?.id ?? "";
    }
    void prefetchMessageBodies({ priorityId: selectedMessageId.value, force: Boolean(options.prefetchBody) });
  } catch (err) {
    if (context.current()) setError(err);
  } finally {
    if (context.current()) busy.value.inbox = false;
  }
}

async function refreshAll() {
  const token = apiToken.value;
  await loadAccounts();
  if (token !== apiToken.value || authRequired.value) return;
  await loadAccountData({ includeInbox: true, withBody: activeTab.value === "codes", force: true });
}

async function handleAccountChange() {
  await loadAccountData({ includeInbox: true, withBody: activeTab.value === "codes" });
}

async function chooseAlias(alias: Alias) {
  selectedAlias.value = alias.email;
  selectedFolder.value = "all";
  activeTab.value = "inbox";
  await loadInbox(alias.email);
}

function switchTab(tab: AppTab) {
  activeTab.value = tab;
  if (tab === "codes" && selectedAccountId.value) {
    void ensureCodeBodies();
  }
}

async function selectMessage(message: Message) {
  selectedMessageId.value = message.id;
  mailModalOpen.value = true;
  if (message.body === undefined) {
    void prefetchMessageBodies({ priorityId: message.id, force: true });
  }
}

function closeMailModal() {
  mailModalOpen.value = false;
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    notice.value = "已复制到剪贴板";
  } catch {
    error.value = "无法访问剪贴板，请使用 HTTPS 或本机地址。";
  }
}

function clearAliasSelection() {
  selectedAlias.value = "";
  selectedMessageId.value = "";
  mailModalOpen.value = false;
  activeTab.value = "inbox";
  void loadInbox();
}

async function ensureCodeBodies() {
  if (messages.value.length === 0) {
    await loadInbox(selectedAlias.value, { prefetchBody: true });
    return;
  }
  void prefetchMessageBodies({ force: true });
}

async function prefetchMessageBodies(options: { priorityId?: string; force?: boolean } = {}) {
  if (!selectedAccountId.value) return;
  const context = accountContext();
  const folder = selectedFolder.value;
  const candidates = [...messages.value]
    .sort((a, b) => {
      if (a.id === options.priorityId) return -1;
      if (b.id === options.priorityId) return 1;
      return 0;
    })
    .filter((message) => message.uid && message.body === undefined);
  if (candidates.length === 0) return;

  const seq = prefetchSeq.value + 1;
  prefetchSeq.value = seq;
  busy.value.prefetch = true;
  try {
    for (let offset = 0; offset < candidates.length; offset += 50) {
      if (!context.current() || seq !== prefetchSeq.value) return;
      const data = await api<MessageBatchData>("/api/messages", {
        method: "POST",
        body: JSON.stringify({
          account_id: context.accountID,
          messages: candidates.slice(offset, offset + 50).map((message) => ({
            uid: message.uid,
            folder: message.folder || folder || "INBOX",
          })),
        }),
      });
      if (!context.current() || seq !== prefetchSeq.value) return;
      const byKey = new Map<string, Message>();
      for (const message of data.messages ?? []) {
        if (message.uid) byKey.set(`${message.folder || "INBOX"}:${message.uid}`, message);
      }
      messages.value = messages.value.map((item) => {
        const full = item.uid ? byKey.get(`${item.folder || folder || "INBOX"}:${item.uid}`) : undefined;
        return full ? { ...item, ...full, id: item.id, uid: item.uid, folder: item.folder, unread: item.unread ?? full.unread } : item;
      });
    }
  } catch (err) {
    if (context.current() && seq === prefetchSeq.value && options.force) setError(err);
  } finally {
    if (context.current() && seq === prefetchSeq.value) {
      busy.value.prefetch = false;
    }
  }
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
  const recipients = new Set((message.to || "").toLowerCase().match(/[a-z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-z0-9.-]+/g) || []);
  return aliases.value.some((alias) => recipients.has(alias.email.toLowerCase()) || alias.email.toLowerCase() === inboxAlias.value.toLowerCase());
}

function isUnreadMessage(message: Message) {
  return message.unread === true;
}

function extractVerificationCode(message?: Message) {
  if (!message) return "";
  const text = [message.subject, message.preview, message.body].filter(Boolean).join("\n");
  const sixDigit = text.match(/(?:^|\D)(\d{6})(?!\d)/);
  if (sixDigit?.[1]) return sixDigit[1];
  const shortCode = text.match(/(?:^|\D)(\d{4,8})(?!\d)/);
  return shortCode?.[1] ?? "";
}

onMounted(async () => {
  await loadAccounts();
  await loadAccountData({ includeInbox: true });
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
            <RefreshCw :class="{ spin: busy.accounts || busy.aliases || busy.inbox || busy.prefetch }" :size="17" />
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
          @click="switchTab(item.id)"
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
            <option v-if="accounts.length === 0" value="">暂无账号</option>
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
          <div class="account-buttons">
            <button class="secondary-button" type="button" @click="accountFormOpen = !accountFormOpen"><Plus :size="16" />添加账号</button>
            <button v-if="activeAccount" class="mini-button danger-button" type="button" title="删除账号" aria-label="删除账号" :disabled="accountActionBusy" @click="removeAccount"><Trash2 :size="16" /></button>
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

        <form v-if="accountFormOpen" class="account-form account-wide" @submit.prevent="addAccount">
          <h3>添加账号</h3>
          <div class="account-fields">
            <label class="form-field">显示名称<input v-model="accountForm.name" class="input" required autocomplete="off" /></label>
            <label class="form-field">Apple ID<input v-model="accountForm.real_email" class="input" type="email" autocomplete="username" /></label>
            <label class="form-field">地区<select v-model="accountForm.host" class="select"><option value="icloud.com">全球 · icloud.com</option><option value="icloud.com.cn">中国大陆 · icloud.com.cn</option></select></label>
            <label class="form-field">代理地址<input v-model="accountForm.proxy" class="input" placeholder="http:// 或 socks5://" autocomplete="off" /></label>
            <label class="form-field account-wide">Cookie<textarea v-model="accountForm.cookies" class="input cookie-input" rows="3" placeholder="name=value; name2=value2 或 JSON" autocomplete="off"></textarea></label>
          </div>
          <button class="primary-button" type="submit" :disabled="accountActionBusy || authRequired"><Loader2 v-if="accountActionBusy" class="spin" :size="16" /><Plus v-else :size="16" />添加账号</button>
        </form>

        <div v-if="activeAccount" class="credential-grid account-wide">
          <form class="account-form" @submit.prevent="saveCredentials('cookies')">
            <h3>Cookie</h3>
            <label class="form-field">Cookie 值<textarea v-model="cookieInput" class="input cookie-input" rows="4" required autocomplete="off" placeholder="name=value; name2=value2 或 JSON"></textarea></label>
            <button class="secondary-button" type="submit" :disabled="accountActionBusy"><RefreshCw :size="16" />更新 Cookie</button>
          </form>
          <form class="account-form" @submit.prevent="saveCredentials('password')">
            <h3>IMAP 凭据</h3>
            <label class="form-field">iCloud 邮箱<input v-model="passwordForm.icloud_email" class="input" type="email" required autocomplete="username" /></label>
            <label class="form-field">应用专用密码<input v-model="passwordForm.app_password" class="input" type="password" required autocomplete="new-password" /></label>
            <button class="secondary-button" type="submit" :disabled="accountActionBusy"><KeyRound :size="16" />保存密码</button>
          </form>
          <form class="account-form" @submit.prevent="saveCredentials('login')">
            <h3>Apple ID 登录</h3>
            <span class="credential-identity">{{ activeAccount.real_email || activeAccount.name }}</span>
            <label class="form-field">Apple ID 密码<input v-model="loginForm.password" class="input" type="password" required autocomplete="current-password" /></label>
            <label class="form-field">双重认证验证码<input v-model="loginForm.otp_code" class="input" inputmode="numeric" pattern="[0-9]{6}" maxlength="6" autocomplete="one-time-code" /></label>
            <button class="secondary-button" type="submit" :disabled="accountActionBusy"><ShieldCheck :size="16" />登录</button>
          </form>
        </div>
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
          <button class="secondary-button" type="button" :disabled="busy.aliases" @click="loadAliases({ force: true })">
            <RefreshCw :class="{ spin: busy.aliases }" :size="16" />
            更新列表
          </button>
        </div>

        <div v-if="busy.aliases" class="empty-state">正在读取别名列表...</div>
        <div v-else-if="aliases.length === 0" class="empty-state">当前账号还没有隐私邮箱别名。</div>
        <div v-else class="alias-list">
          <article
            v-for="alias in aliases"
            :key="alias.anonymousId || alias.email"
            class="alias-row"
            :class="{ selected: selectedAlias === alias.email }"
          >
            <button class="alias-main alias-open" type="button" @click="chooseAlias(alias)">
              <strong>{{ alias.email }}</strong>
              <small>{{ alias.label || "未命名" }}</small>
            </button>
            <span class="alias-meta">
              <span class="soft-tag" :class="{ muted: !alias.active }">
                {{ alias.active ? "启用" : "停用" }}
              </span>
              <small>{{ formatDate(alias.createdAt) }}</small>
              <span class="job-actions">
                <button class="mini-button" type="button" title="复制别名" aria-label="复制别名" @click="copyText(alias.email)"><Copy :size="14" /></button>
                <button class="mini-button" type="button" :title="alias.active ? '停用别名' : '启用别名'" :aria-label="alias.active ? '停用别名' : '启用别名'" :disabled="aliasActionBusy" @click="changeAlias(alias, alias.active ? 'deactivate' : 'reactivate')"><Pause v-if="alias.active" :size="14" /><Play v-else :size="14" /></button>
                <button class="mini-button danger-button" type="button" title="永久删除已停用别名" aria-label="删除别名" :disabled="aliasActionBusy || alias.active" @click="changeAlias(alias, 'delete')"><Trash2 :size="14" /></button>
              </span>
            </span>
          </article>
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
        <div v-else-if="busy.prefetch && extractedCodes.length === 0" class="empty-state">正在解析邮件正文...</div>
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
        <form class="account-form account-wide" @submit.prevent="saveApiToken">
          <h3>API 访问令牌</h3>
          <label class="form-field">Bearer Token<input v-model="tokenDraft" class="input" type="password" autocomplete="off" /></label>
          <span v-if="authRequired" class="danger-text">访问被拒绝，请输入服务端配置的访问令牌。</span>
          <button class="secondary-button" type="submit" :disabled="busy.accounts"><KeyRound :size="16" />保存并连接</button>
        </form>
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
            <RefreshCw :class="{ spin: busy.accounts || busy.aliases || busy.inbox || busy.prefetch }" :size="17" />
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

            <div class="mail-modal-body" :class="{ loading: busy.message }">
              <p>{{ modalBody }}</p>
            </div>
          </div>
        </section>
      </div>
    </div>
  </main>
</template>
