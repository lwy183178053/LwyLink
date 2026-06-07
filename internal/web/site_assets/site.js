const tokenKey = "lwylink.site.token";
const page = document.currentScript?.dataset.page || "home";
const toast = document.querySelector("[data-toast]");

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
        }),
      });
      localStorage.setItem(tokenKey, result.token);
      location.href = "/dashboard";
    } catch (err) {
      show(err.message);
    }
  });
}

async function renderDashboard() {
  const box = document.querySelector("[data-profile]");
  if (!box) return;
  if (!token()) {
    location.href = "/login";
    return;
  }
  try {
    const user = await api("/api/me");
    setLines(box, [
      user.displayName || user.email || user.username,
      `邮箱: ${user.email || user.username}`,
      `会员: ${user.membership}`,
      `剩余中继流量: ${formatBytes(user.relayTrafficRemainingBytes)}`,
    ]);
  } catch {
    localStorage.removeItem(tokenKey);
    location.href = "/login";
  }
}

async function renderDownload() {
  const box = document.querySelector("[data-release-box]");
  const button = document.querySelector("[data-download-button]");
  if (!box || !button) return;
  try {
    const release = await api("/api/client/releases/latest?platform=windows-amd64");
    setLines(box, [
      `Windows 客户端 ${release.version}`,
      `${release.fileName || "LwyLink.exe"} · ${formatBytes(release.sizeBytes)}`,
      release.notes || "",
    ]);
  } catch (err) {
    box.textContent = err.message;
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

document.querySelector("[data-logout-button]")?.addEventListener("click", () => {
  localStorage.removeItem(tokenKey);
  location.href = "/";
});

const authLink = document.querySelector("[data-auth-link]");
if (authLink && token()) {
  authLink.textContent = "用户中心";
  authLink.setAttribute("href", "/dashboard");
}

wireAuthForms();
if (page === "dashboard") renderDashboard();
if (page === "download") renderDownload();
