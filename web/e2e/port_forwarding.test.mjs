import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createServer, connect } from "node:net";
import { createSocket } from "node:dgram";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE, headless: true });
const tcpEcho = createServer(socket => socket.pipe(socket));
await new Promise(resolve => tcpEcho.listen(0, "127.0.0.1", resolve));
const destinationPort = tcpEcho.address().port;
const udpEcho = createSocket("udp4");
udpEcho.on("message", (data, address) => udpEcho.send(data, address.port, address.address));
await new Promise(resolve => udpEcho.bind(destinationPort, "127.0.0.1", resolve));
const base = process.env.GOSSHD_UI_E2E_BASE_URL;
const screenshots = process.env.GOSSHD_FORWARD_SCREENSHOTS;
if (screenshots) await mkdir(screenshots, { recursive: true });
const freePort = async () => { const s = createServer(); await new Promise(r => s.listen(0, "127.0.0.1", r)); const p = s.address().port; await new Promise(r => s.close(r)); return p; };
const udpRequest = port => new Promise((resolve, reject) => {
 const s = createSocket("udp4"); const payload = Buffer.from("real port UDP\x00\xff");
 const timer = setTimeout(() => { s.close(); reject(new Error("UDP timeout")); }, 5000);
 s.once("error", e => { clearTimeout(timer); s.close(); reject(e); });
 s.once("message", b => { clearTimeout(timer); s.close(); try { assert.deepEqual(b, payload); resolve(); } catch (e) { reject(e); } });
 s.send(payload, port, "127.0.0.1");
});
const tcpRequest = port => new Promise((resolve, reject) => {
 const s = connect(port, "127.0.0.1"); const payload = Buffer.from("real port TCP");
 s.setTimeout(5000, () => s.destroy(new Error("TCP timeout"))); s.once("error", reject);
 s.once("connect", () => s.write(payload)); s.once("data", b => { s.destroy(); try { assert.deepEqual(b, payload); resolve(); } catch(e) { reject(e); } });
});
try {
 for (const [locale, width] of [["zh-CN", 1440], ["en", 1440], ["zh-CN", 390]]) {
  const zh = locale === "zh-CN";
  const context = await browser.newContext({ locale: zh ? "zh-CN" : "en-US", viewport: { width, height: 1000 }, reducedMotion: "reduce" });
  await context.addInitScript(locale => localStorage.setItem("gosshd_locale", locale), locale);
  const page = await context.newPage(); page.setDefaultTimeout(15000); const errors = []; page.on("pageerror", e => errors.push(e.message));
  await page.goto(base); await page.getByLabel(zh ? "邮箱" : "Email", { exact: true }).fill("admin"); await page.getByLabel(zh ? "密码" : "Password", { exact: true }).fill("admin-pass");
  await page.getByRole("button", { name: zh ? "登录" : "Sign in", exact: true }).click();
  if (width < 600) await page.locator(".mobile-menu-button").click();
  await page.getByRole("link", { name: zh ? "隧道管理" : "Tunnels", exact: true }).click();
  await page.getByRole("tab", { name: zh ? "端口转发" : "Port forwarding", exact: true }).waitFor();
  await page.getByRole("button", { name: zh ? "新建隧道" : "New tunnel", exact: true }).first().click();
  const editor = page.locator(".tunnel-editor"); await editor.waitFor();
  const tcp = editor.getByRole("checkbox", { name: "TCP", exact: true }); const udp = editor.getByRole("checkbox", { name: "UDP", exact: true });
  assert.equal(await tcp.isChecked(), true); assert.equal(await udp.isChecked(), false);
  await tcp.uncheck(); assert.equal(await editor.getByRole("button", { name: zh ? "保存配置" : "Save configuration", exact: true }).isDisabled(), true);
  await udp.check(); assert.equal(await tcp.isChecked(), false); await tcp.check();
  const name = `Port UI ${locale} ${width}`; const listenPort = await freePort();
  await editor.getByLabel(zh ? "隧道名称" : "Tunnel name", { exact: true }).fill(name);
  await editor.getByLabel(zh ? "监听端口" : "Listen port", { exact: true }).fill(String(listenPort));
  await editor.getByLabel(zh ? "目标端口" : "Destination port", { exact: true }).fill(String(destinationPort));
  await editor.getByLabel(zh ? "保存后立即启用" : "Enable after saving", { exact: true }).click();
  if (screenshots) await page.screenshot({ path: join(screenshots, `port-editor-${locale}-${width}.png`), fullPage: true });
  await editor.getByRole("button", { name: zh ? "保存配置" : "Save configuration", exact: true }).click(); await editor.waitFor({ state: "detached" });
  const me = await (await context.request.get(base + "/api/me")).json(); const org = me.active_organization_id || me.organizations?.[0]?.id;
  let configured;
  const waitRun = async networks => {
   for (let i = 0; i < 100; i++) { const result = await (await context.request.get(base + "/api/tunnels?organization_id=" + org)).json(); configured = result.tunnels?.find(t => t.name === name); if (configured?.status === "running" && JSON.stringify(configured.transports) === JSON.stringify(networks)) return; await new Promise(r => setTimeout(r, 50)); }
   throw new Error("port forwarding did not converge: " + JSON.stringify(configured));
  };
  await waitRun(["tcp", "udp"]); assert.equal(configured.protocol, "port"); await tcpRequest(listenPort); await udpRequest(listenPort);
  const card = page.locator(".tunnel-card").filter({ has: page.getByRole("heading", { name, exact: true }) });
  await card.getByText("TCP + UDP", { exact: true }).waitFor();
  await card.getByRole("button", { name: zh ? "编辑隧道" : "Edit tunnel", exact: true }).click(); await editor.waitFor();
  assert.equal(await tcp.isChecked(), true); assert.equal(await udp.isChecked(), true); await tcp.uncheck();
  await editor.getByRole("button", { name: zh ? "保存配置" : "Save configuration", exact: true }).click(); await editor.waitFor({ state: "detached" });
  await waitRun(["udp"]); await udpRequest(listenPort);
  await card.getByRole("button", { name: zh ? "编辑隧道" : "Edit tunnel", exact: true }).click(); await editor.waitFor();
  assert.equal(await tcp.isChecked(), false); assert.equal(await udp.isChecked(), true);
  if (screenshots) await page.screenshot({path:join(screenshots,`port-saved-${locale}-${width}.png`),fullPage:true});
  await editor.getByRole("button", { name: zh ? /取\s*消/ : "Cancel", exact: true }).click();
  await card.getByRole("button",{name:zh?"统计":"Statistics",exact:true}).click();
  const stats=page.getByRole("dialog").filter({hasText:zh?"所选时段流量":"Traffic in range"}).last();await stats.waitFor();
  assert.equal(await page.getByRole("dialog").count(),1);
  assert.equal(await stats.getByRole("tab",{name:zh?"请求日志":"Requests",exact:true}).count(),0);
  assert.equal(await stats.getByRole("tab",{name:zh?"来源统计":"Sources",exact:true}).count(),0);
  assert.equal(await stats.getByText(/保留期内|retained window/).count(),0);
  assert.equal(await stats.locator(".forward-log-filters").count(),0);
  assert.equal(await stats.getByRole("button",{name:zh?"流量趋势 / 历史来源统计":"Traffic trends / historical sources",exact:true}).count(),0);
  await stats.locator(".tunnel-statistics-table").getByRole("button",{name:"127.0.0.1",exact:true}).waitFor();
  const overview=await(await context.request.get(base+"/api/tunnels/"+configured.id+"/logs")).json();
  assert.deepEqual(overview.events,[]);assert.deepEqual(overview.sources,[]);assert.equal(overview.retention_days,0);
  assert.ok(overview.stats.up>0&&overview.stats.down>0);
  if(screenshots)await page.screenshot({path:join(screenshots,`port-statistics-${locale}-${width}.png`),fullPage:true});
  assert.deepEqual(errors, []); await context.request.delete(base + "/api/tunnels/" + configured.id); await context.close();
 }
 console.log("Port forwarding UI passed: Chinese/English, desktop/mobile, TCP default, empty rejection, dual TCP/UDP traffic, UDP-only edit persistence");
} finally { await browser.close(); udpEcho.close(); await new Promise(r => tcpEcho.close(r)); }
