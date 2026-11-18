"use strict";

(() => {
  const byId = (id) => document.getElementById(id);
  const key = "jellyhere-managed-owner-token";
  let ownerToken = "";
  let githubSession = false;
  let pairingKnownDevices = null;
  let setupTouched = false;
  let refreshing = false;

  function notice(message) { byId("notice").textContent = message || ""; }
  function element(tag, className, value) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (value !== undefined) node.textContent = value;
    return node;
  }
  function showSetup(open) {
    setupTouched = true;
    byId("add-device").hidden = !open;
    byId("add-device-trigger").setAttribute("aria-expanded", String(open));
    if (open) byId("add-device").scrollIntoView({ behavior: "smooth", block: "start" });
  }
  async function request(path, method = "GET", body) {
    const response = await fetch(path, {
      method,
      headers: { ...(ownerToken ? { Authorization: "Bearer " + ownerToken } : {}), ...(body ? { "Content-Type": "application/json" } : {}) },
      body: body ? JSON.stringify(body) : undefined,
      cache: "no-store",
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) {
      const error = new Error(response.status === 401 ? "登录已失效，请重新登录" : (data.error?.message || "请求失败 (" + response.status + ")"));
      error.status = response.status;
      throw error;
    }
    return data;
  }
  function showSecret(title, token, details) {
    byId("issued").hidden = false;
    byId("issued").querySelector("h2").textContent = title;
    byId("issued-details").textContent = details;
    byId("issued-token").value = token;
    byId("issued").scrollIntoView({ behavior: "smooth", block: "start" });
  }
  function deviceTitle(device) {
    return device.model || (device.manufacturer ? device.manufacturer + " Android 手机" : "Android 手机");
  }
  function deviceState(device) {
    if (!device.online) return ["离线", "offline"];
    if (device.paused) return ["已暂停", "paused"];
    return ["在线", "online"];
  }
  function lastSeen(device) {
    const value = device.lastSeenAt || device.connectedAt;
    if (!value) return "尚未连接";
    const date = new Date(value);
    if (Number.isNaN(date.getTime()) || date.getFullYear() < 2020) return "尚未连接";
    const label = device.online ? "最近响应" : "最近在线";
    return label + "：" + date.toLocaleString("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" });
  }
  function renderDevice(device) {
    const card = element("article", "device-card");
    card.setAttribute("role", "listitem");
    const head = element("div", "device-card-head");
    const icon = element("span", "phone-icon");
    icon.setAttribute("aria-hidden", "true");
    const copy = element("div");
    copy.style.minWidth = "0";
    copy.style.flex = "1";
    copy.append(element("h3", "device-name", deviceTitle(device)), element("p", "device-maker", device.manufacturer || "Android 设备"));
    const state = deviceState(device);
    head.append(icon, copy, element("span", "pill " + state[1], state[0]));
    const meta = element("div", "device-meta");
    meta.append(document.createTextNode("设备 ID："), element("code", "", device.id), element("br"), document.createTextNode(lastSeen(device)));
    const footer = element("div", "device-footer");
    footer.append(element("span", "form-note", device.online ? "可在下方为智能体授权" : "检查手机端果冻连接"));
    const revoke = element("button", "ghost danger", "移除设备");
    revoke.type = "button";
    revoke.setAttribute("aria-label", "移除设备 " + device.id);
    revoke.addEventListener("click", async () => {
      if (!confirm("移除设备 " + device.id + "？它将从当前空间断开，关联的智能体授权也会被撤销。")) return;
      try {
        await request("/tenant/devices/" + encodeURIComponent(device.id), "DELETE");
        await refresh();
        notice("设备及关联授权已撤销");
      } catch (error) { notice(error.message); }
    });
    footer.append(revoke);
    card.append(head, meta, footer);
    return card;
  }
  function renderClient(client) {
    const card = element("div", "agent-card");
    const summary = element("div");
    summary.append(element("strong", "", client.name || client.id));
    summary.append(element("span", "", (client.scope === "viewer" ? "只读" : "开发测试控制") + " · " + (client.device_ids || []).length + " 台设备"));
    const revoke = element("button", "ghost danger", "撤销");
    revoke.type = "button";
    revoke.setAttribute("aria-label", "撤销 " + (client.name || client.id) + " 的授权");
    revoke.addEventListener("click", async () => {
      if (!confirm("撤销 " + (client.name || client.id) + " 的 MCP 令牌？")) return;
      try {
        await request("/tenant/clients/" + encodeURIComponent(client.id), "DELETE");
        await refresh();
        notice("智能体授权已撤销");
      } catch (error) { notice(error.message); }
    });
    card.append(summary, revoke);
    return card;
  }
  function renderChoices(devices) {
    const choices = byId("device-choices");
    const previousChecks = [...choices.querySelectorAll("input")];
    const selected = new Set(previousChecks.filter((input) => input.checked).map((input) => input.value));
    const hadChoices = previousChecks.length > 0;
    choices.replaceChildren();
    for (const device of devices) {
      const label = element("label", "choice");
      const check = element("input");
      check.type = "checkbox";
      check.value = device.id;
      check.checked = selected.has(device.id) || (!hadChoices && devices.length === 1);
      const name = element("span");
      name.append(element("strong", "", deviceTitle(device)), element("small", "", device.id + " · " + deviceState(device)[0]));
      label.append(check, name);
      choices.append(label);
    }
    if (!devices.length) choices.append(element("p", "form-note", "请先添加手机设备"));
  }
  async function refresh() {
    if (refreshing) return;
    refreshing = true;
    try {
      const data = await request("/tenant");
      const devices = data.devices || [];
      const clients = data.clients || [];
      byId("login").hidden = true;
      byId("dashboard").hidden = false;
      byId("tenant-title").textContent = "空间 ID：" + data.tenant_id;
      byId("device-url").textContent = "wss://" + location.host + "/device";
      byId("mcp-url").textContent = location.origin + "/mcp";
      byId("device-count").textContent = String(devices.length);
      byId("online-count").textContent = String(devices.filter((device) => device.online).length);
      byId("client-count").textContent = String(clients.length);
      byId("device-heading-count").textContent = "（" + devices.length + "）";
      byId("devices").replaceChildren(...devices.map(renderDevice));
      byId("empty-devices").hidden = devices.length > 0;
      byId("clients").replaceChildren(...clients.map(renderClient));
      if (!clients.length) byId("clients").append(element("p", "form-note", "尚未授权智能体。添加设备后，在右侧创建专属 MCP 令牌。"));
      renderChoices(devices);
      if (!setupTouched && !devices.length) {
        byId("add-device").hidden = false;
        byId("add-device-trigger").setAttribute("aria-expanded", "true");
      }
      byId("sync-status").textContent = "更新于 " + new Date().toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
      if (pairingKnownDevices && devices.some((device) => !pairingKnownDevices.has(device.id))) {
        pairingKnownDevices = null;
        byId("pairing").hidden = true;
        byId("pair-qr").removeAttribute("src");
        notice("手机已添加。请确认上方设备显示在线，再授权智能体。");
      }
    } catch (error) {
      if (error.status === 401) {
        ownerToken = "";
        githubSession = false;
        try { sessionStorage.removeItem(key); } catch (_) { /* Storage may be disabled. */ }
        byId("dashboard").hidden = true;
        byId("login").hidden = false;
        notice(error.message);
      }
      throw error;
    } finally { refreshing = false; }
  }
  async function login(token) {
    if (!/^[a-f0-9]{64}$/i.test(token)) throw new Error("请输入邀请凭据中的 64 位账户管理令牌");
    ownerToken = token;
    try { await refresh(); }
    catch (error) { ownerToken = ""; throw error; }
    try { sessionStorage.setItem(key, token); } catch (_) { /* Storage may be disabled. */ }
    byId("owner-token").value = "";
    notice("");
  }
  byId("login-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    try { await login(byId("owner-token").value.trim()); }
    catch (error) { notice(error.message); }
  });
  byId("add-device-trigger").addEventListener("click", () => showSetup(byId("add-device").hidden));
  byId("close-add-device").addEventListener("click", () => showSetup(false));
  byId("device-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    try {
      const id = byId("device-id").value.trim();
      const data = await request("/tenant/devices", "POST", { device_id: id });
      await refresh();
      showSecret("手机令牌", data.device_token, "设备 ID：" + id + "。App 地址：wss://" + location.host + "/device");
      byId("device-id").value = "";
      notice("");
    } catch (error) { notice(error.message); }
  });
  byId("client-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    try {
      const ids = [...byId("device-choices").querySelectorAll("input:checked")].map((input) => input.value);
      if (!ids.length) throw new Error("请选择至少一台设备");
      const data = await request("/tenant/clients", "POST", {
        client_name: byId("client-name").value.trim(), scope: byId("client-scope").value, device_ids: ids,
      });
      await refresh();
      showSecret("MCP 令牌", data.mcp_token, "智能体：" + data.client_name + "。MCP 地址：" + location.origin + "/mcp。权限：" + data.scope + "。设备：" + ids.join(", "));
      byId("client-name").value = "";
      notice("");
    } catch (error) { notice(error.message); }
  });
  byId("copy-token").addEventListener("click", async () => {
    try { await navigator.clipboard.writeText(byId("issued-token").value); notice("令牌已复制"); }
    catch (_) { byId("issued-token").select(); notice("请手动复制选中的令牌"); }
  });
  byId("refresh").addEventListener("click", () => refresh().then(() => notice("设备状态已更新")).catch((error) => notice(error.message)));
  byId("create-pairing").addEventListener("click", async () => {
    try {
      const data = await request("/tenant/pairings", "POST", {});
      pairingKnownDevices = new Set([...byId("device-choices").querySelectorAll("input")].map((input) => input.value));
      byId("pair-qr").src = data.qr_data_url;
      byId("pair-expiry").textContent = new Date(data.expires_at).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
      byId("pairing").hidden = false;
      notice("");
    } catch (error) { notice(error.message); }
  });
  setInterval(() => {
    if (!byId("dashboard").hidden) refresh().catch((error) => { byId("sync-status").textContent = "刷新失败：" + error.message; });
  }, 10000);
  byId("logout").addEventListener("click", async () => {
    if (githubSession) await request("/auth/logout", "POST").catch(() => {});
    githubSession = false;
    ownerToken = "";
    try { sessionStorage.removeItem(key); } catch (_) { /* Storage may be disabled. */ }
    byId("issued-token").value = "";
    byId("issued").hidden = true;
    byId("dashboard").hidden = true;
    byId("login").hidden = false;
    notice("");
  });
  const fragment = new URLSearchParams(location.hash.slice(1));
  const invite = fragment.get("owner");
  if (invite) history.replaceState(null, "", location.pathname + location.search);
  let saved = "";
  try { saved = sessionStorage.getItem(key) || ""; } catch (_) { /* Storage may be disabled. */ }
  fetch("/auth/config", { cache: "no-store" }).then((r) => r.json()).then((data) => {
    byId("github-login").hidden = !data.github_enabled;
    byId("github-unavailable").hidden = !!data.github_enabled;
  }).catch(() => {});
  fetch("/auth/session", { cache: "no-store" }).then((r) => {
    if (r.ok) { githubSession = true; return refresh(); }
    if (invite || saved) return login(invite || saved);
  }).catch((error) => { ownerToken = ""; notice(error.message); });
})();
