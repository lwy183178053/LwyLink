const tokenKey = "lwylink.site.token";
const page = document.currentScript?.dataset.page || "home";
const toast = document.querySelector("[data-toast]");
let currentUser = null;

function token() {
  return localStorage.getItem(tokenKey) || "";
}

function show(message) {
  if (!toast) return;
  toast.textContent = message;
  toast.hidden = false;
  window.setTimeout(() => {
    toast.hidden = true;
  }, 3200);
}

async function api(path, options = {}) {
  const headers = new Headers(options.headers || {});
  if (!headers.has("Content-Type") && options.body) headers.set("Content-Type", "application/json");
  if (token()) headers.set("Authorization", `Bearer ${token()}`);
  const res = await fetch(path, { ...options, headers });
  const text = await res.text();
  let data = {};
  if (text) {
    try { data = JSON.parse(text); } catch { data = { error: text }; }
  }
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
  return data;
}

function wireAuthForms() {
  const login = document.querySelector("[data-login-form]");
  login?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(login);
    try {
      const result = await api("/api/login", {
        method: "POST",
        body: JSON.stringify({ email: form.get("email"), password: form.get("password") }),
      });
      localStorage.setItem(tokenKey, result.token);
      location.href = "/dashboard";
    } catch (err) {
      show(err.message);
    }
  });

  const register = document.querySelector("[data-register-form]");
  register?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(register);
    try {
      const result = await api("/api/register", {
        method: "POST",
        body: JSON.stringify({
          email: form.get("email"),
          password: form.get("password"),
          displayName: form.get("displayName"),
          emailCode: form.get("emailCode"),
        }),
      });
      localStorage.setItem(tokenKey, result.token);
      location.href = "/dashboard";
    } catch (err) {
      show(err.message);
    }
  });

  const reset = document.querySelector("[data-reset-form]");
  reset?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(reset);
    try {
      await api("/api/password/reset", {
        method: "POST",
        body: JSON.stringify({
          email: form.get("email"),
          emailCode: form.get("emailCode"),
          newPassword: form.get("newPassword"),
        }),
      });
      reset.reset();
      show("密码已重置，请使用新密码登录");
    } catch (err) {
      show(err.message);
    }
  });
}

function wireEmailCodeButtons() {
  document.querySelectorAll("[data-send-code]").forEach((button) => {
    button.addEventListener("click", async () => {
      const purpose = button.dataset.purpose || "register";
      const source = button.dataset.emailSource || "register";
      const form = document.querySelector(`[data-${source}-form]`);
      const input = form?.querySelector('input[name="email"]');
      const email = input?.value || "";
      if (!email.trim()) {
        show("请先填写邮箱");
        return;
      }
      try {
        button.disabled = true;
        await api("/api/email/verification/send", {
          method: "POST",
          body: JSON.stringify({ email, purpose }),
        });
        show("验证码已发送，请查收邮箱");
        startCodeCountdown(button);
      } catch (err) {
        button.disabled = false;
        show(err.message);
      }
    });
  });
}

function wirePasswordChange() {
  const form = document.querySelector("[data-change-password-form]");
  form?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    try {
      await api("/api/password/change", {
        method: "POST",
        body: JSON.stringify({
          oldPassword: data.get("oldPassword"),
          newPassword: data.get("newPassword"),
        }),
      });
      form.reset();
      show("密码已修改");
    } catch (err) {
      show(err.message);
    }
  });
}

function startCodeCountdown(button) {
  let remain = 60;
  const original = button.textContent;
  button.textContent = `${remain}s`;
  const timer = window.setInterval(() => {
    remain -= 1;
    if (remain <= 0) {
      window.clearInterval(timer);
      button.disabled = false;
      button.textContent = original;
      return;
    }
    button.textContent = `${remain}s`;
  }, 1000);
}

async function fetchCurrentUser() {
  if (!token()) return null;
  try {
    currentUser = await api("/api/me");
    return currentUser;
  } catch {
    localStorage.removeItem(tokenKey);
    currentUser = null;
    return null;
  }
}

function renderAuthNav(user) {
  const login = document.querySelector("[data-auth-login]");
  const userLink = document.querySelector("[data-auth-user]");
  if (!login || !userLink) return;
  if (!user) {
    login.hidden = false;
    userLink.hidden = true;
    return;
  }
  login.hidden = true;
  userLink.hidden = false;
  const name = displayName(user);
  setText("[data-auth-avatar]", initial(name));
  setText("[data-auth-name]", name);
  setText("[data-auth-email]", user.email || user.username || "");
  setText("[data-auth-member]", membershipLabel(user.membership));
}

async function renderDashboard() {
  if (!token()) {
    location.href = "/login";
    return;
  }
  const user = currentUser || await fetchCurrentUser();
  if (!user) {
    location.href = "/login";
    return;
  }
  const name = displayName(user);
  setText("[data-profile-avatar]", initial(name));
  setText("[data-profile-name]", name);
  setText("[data-profile-email]", user.email || user.username || "");
  setText("[data-profile-membership]", membershipLabel(user.membership));
  setText("[data-profile-member-note]", membershipNote(user.membership));
  setText("[data-profile-traffic-used]", formatBytes(user.relayTrafficUsedBytes));
  setText("[data-profile-traffic]", formatBytes(user.relayTrafficRemainingBytes));
  setText("[data-profile-traffic-quota]", formatBytes(user.relayTrafficQuotaBytes));
  const usedPercent = trafficPercent(user);
  setText("[data-profile-traffic-percent]", `${usedPercent}%`);
  const bar = document.querySelector("[data-profile-traffic-bar]");
  if (bar) bar.style.width = `${usedPercent}%`;
  setText("[data-profile-period]", user.relayTrafficPeriod ? `周期: ${user.relayTrafficPeriod}` : "当前周期");
  setText("[data-profile-id]", `#${user.id}`);
  setText("[data-profile-account]", user.email || user.username || "");
  setText("[data-profile-role]", roleLabel(user.role));
  setText("[data-profile-created]", formatDate(user.createdAt));
}

async function renderDownload() {
  const button = document.querySelector("[data-download-button]");
  if (!button) return;
  try {
    const release = await api("/api/client/releases/latest?platform=windows-amd64");
    setText("[data-release-version]", `版本 ${release.version}`);
    setText("[data-release-platform]", release.platform || "windows-amd64");
    setText("[data-release-file]", release.fileName || "LwyLink.exe");
    setText("[data-release-size]", formatBytes(release.sizeBytes));
    setText("[data-release-sha]", shortSHA(release.sha256));
    setText("[data-release-notes]", release.notes || "当前版本暂无额外更新说明。");
  } catch (err) {
    setText("[data-release-notes]", err.message);
  }
  button.addEventListener("click", async () => {
    if (!token()) {
      location.href = "/login";
      return;
    }
    try {
      const ticket = await api("/api/client/releases/ticket", {
        method: "POST",
        body: JSON.stringify({ platform: "windows-amd64" }),
      });
      location.href = ticket.downloadURL;
    } catch (err) {
      show(err.message);
    }
  });
}

function formatBytes(value) {
  const n = Number(value || 0);
  if (n >= 1024 ** 3) return `${(n / 1024 ** 3).toFixed(1)} GB`;
  if (n >= 1024 ** 2) return `${(n / 1024 ** 2).toFixed(1)} MB`;
  if (n >= 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${n} B`;
}

function setLines(target, lines) {
  target.replaceChildren();
  lines.filter(Boolean).forEach((line, index) => {
    const node = index === 0 ? document.createElement("strong") : document.createElement("span");
    node.textContent = line;
    target.appendChild(node);
    if (index < lines.length - 1) target.appendChild(document.createElement("br"));
  });
}

function setText(selector, text) {
  const node = document.querySelector(selector);
  if (node) node.textContent = text;
}

function displayName(user) {
  return user.displayName || user.email || user.username || "LwyLink 玩家";
}

function initial(value) {
  const trimmed = String(value || "L").trim();
  return trimmed.slice(0, 1).toUpperCase();
}

function membershipLabel(value) {
  return value === "vip" ? "VIP" : "Free";
}

function membershipNote(value) {
  return value === "vip" ? "月卡权益已生效，拥有更高的中继流量额度。" : "当前为免费账号，可正常创建房间和使用 P2P。";
}

function roleLabel(value) {
  return value === "admin" ? "管理员" : "玩家";
}

function trafficPercent(user) {
  const used = Number(user.relayTrafficUsedBytes || 0);
  const quota = Number(user.relayTrafficQuotaBytes || 0);
  if (quota <= 0) return 0;
  return Math.max(0, Math.min(100, Math.round((used / quota) * 100)));
}

function formatDate(value) {
  if (!value) return "--";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "--";
  return date.toLocaleDateString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" });
}

function shortSHA(value) {
  const text = String(value || "");
  if (text.length <= 18) return text || "--";
  return `${text.slice(0, 12)}...${text.slice(-8)}`;
}

document.querySelector("[data-logout-button]")?.addEventListener("click", () => {
  localStorage.removeItem(tokenKey);
  location.href = "/";
});

wireAuthForms();
wireEmailCodeButtons();
wirePasswordChange();

(async function init() {
  renderAuthNav(await fetchCurrentUser());
  if (page === "dashboard") renderDashboard();
  if (page === "download") renderDownload();
})();
