"use strict";

(() => {
  const byId = (id) => document.getElementById(id);
  const key = "jellyhere-managed-owner-token";
  let ownerToken = "";
  let githubSession = false;
  let pairingKnownDevices = null;

  function notice(message) { byId("notice").textContent = message || ""; }

  async function request(path, method = "GET", body) {
    const response = await fetch(path, {
      method,
      headers: { ...(ownerToken ? { Authorization: `Bearer ${ownerToken}` } : {}), ...(body ? { "Content-Type": "application/json" } : {}) },
      body: body ? JSON.stringify(body) : undefined,
      cache: "no-store",
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(data.error?.message || `请求失败 (${response.status})`);
    return data;
  }

  function showSecret(title, token, details) {
    byId("issued").hidden = false;
    byId("issued").querySelector("h2").textContent = title;
    byId("issued-details").textContent = details;
    byId("issued-token").value = token;
    byId("issued").scrollIntoView({ behavior: "smooth", block: "start" });
  }

  function item(label, onRevoke) {
    const row = document.createElement("li");
    const name = document.createElement("span");
    name.textContent = label;
    const button = document.createElement("button");
    button.type = "button";
    button.className = "small danger";
    button.textContent = "撤销";
    button.addEventListener("click", onRevoke);
    row.append(name, button);
    return row;
  }

  async function refresh() {
    const data = await request("/tenant");
    byId("login").hidden = true;
    byId("dashboard").hidden = false;
    byId("tenant-title").textContent = `设备空间：${data.tenant_id}`;
    byId("device-url").textContent = `wss://${location.host}/device`;
    byId("mcp-url").textContent = `${location.origin}/mcp`;
    const devices = byId("devices");
    const choices = byId("device-choices");
    const previousChecks = [...choices.querySelectorAll("input")];
    const selected = new Set(previousChecks.filter((input) => input.checked).map((input) => input.value));
    const hadChoices = previousChecks.length > 0;
    devices.replaceChildren();
    choices.replaceChildren();
    for (const device of data.devices || []) {
      devices.append(item(`${device.id} · ${device.online ? "在线" : "离线"}${device.model ? ` · ${device.model}` : ""}`, async () => {
        if (!confirm(`撤销设备 ${device.id}？它将断线，关联的智能体授权也会被撤销。`)) return;
        try { await request(`/tenant/devices/${encodeURIComponent(device.id)}`, "DELETE"); await refresh(); notice("设备及关联授权已撤销"); }
        catch (error) { notice(error.message); }
      }));
      const label = document.createElement("label");
      label.className = "choice";
      const check = document.createElement("input");
      check.type = "checkbox";
      check.value = device.id;
      check.checked = selected.has(device.id) || (!hadChoices && data.devices.length === 1);
      label.append(check, document.createTextNode(device.id));
      choices.append(label);
    }
    if (!data.devices?.length) choices.textContent = "请先添加手机设备";
    if (pairingKnownDevices && data.devices?.some((device) => !pairingKnownDevices.has(device.id))) {
      pairingKnownDevices = null;
      byId("pairing").hidden = true;
      byId("pair-qr").removeAttribute("src");
      notice("手机已登记；请确认设备显示在线，然后再授权智能体");
    }
    const clients = byId("clients");
    clients.replaceChildren();
    for (const client of data.clients || []) {
      clients.append(item(`${client.name || client.id} · ${client.scope === "viewer" ? "只读" : "控制"} · ${(client.device_ids || []).join(", ")}`, async () => {
        if (!confirm(`撤销客户端 ${client.id} 的 MCP 令牌？`)) return;
        try { await request(`/tenant/clients/${encodeURIComponent(client.id)}`, "DELETE"); await refresh(); notice("客户端授权已撤销"); }
        catch (error) { notice(error.message); }
      }));
    }
  }

  async function login(token) {
    if (!/^[a-f0-9]{64}$/i.test(token)) throw new Error("请输入邀请凭据中的 64 位账户管理令牌");
    ownerToken = token;
    await refresh();
    try { sessionStorage.setItem(key, token); } catch (_) { /* Storage may be disabled. */ }
    byId("owner-token").value = "";
    notice("");
  }

  byId("login-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    try { await login(byId("owner-token").value.trim()); }
    catch (error) { notice(error.message); }
  });
  byId("device-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    try {
      const id = byId("device-id").value.trim();
      const data = await request("/tenant/devices", "POST", { device_id: id });
      await refresh();
      showSecret("手机令牌", data.device_token, `设备 ID：${id}。App 地址：wss://${location.host}/device`);
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
      showSecret("MCP 令牌", data.mcp_token, `智能体：${data.client_name}。MCP 地址：${location.origin}/mcp。权限：${data.scope}。设备：${ids.join(", ")}`);
      byId("client-name").value = "";
      notice("");
    } catch (error) { notice(error.message); }
  });
  byId("copy-token").addEventListener("click", async () => {
    try { await navigator.clipboard.writeText(byId("issued-token").value); notice("令牌已复制"); }
    catch (_) { byId("issued-token").select(); notice("请手动复制选中的令牌"); }
  });
  byId("refresh").addEventListener("click", () => refresh().catch((error) => notice(error.message)));
  byId("create-pairing").addEventListener("click", async () => {
    try {
      const data = await request("/tenant/pairings", "POST", {});
      pairingKnownDevices = new Set([...byId("device-choices").querySelectorAll("input")].map((input) => input.value));
      byId("pair-qr").src = data.qr_data_url;
      byId("pair-expiry").textContent = new Date(data.expires_at).toLocaleTimeString();
      byId("pairing").hidden = false;
      notice("");
    } catch (error) { notice(error.message); }
  });
  setInterval(() => {
    if (!byId("dashboard").hidden) refresh().catch(() => {});
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
