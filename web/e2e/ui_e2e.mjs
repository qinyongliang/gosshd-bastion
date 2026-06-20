import { createRequire } from "node:module";

const baseURL = mustEnv("GOSSHD_UI_E2E_BASE_URL");
const playwrightPath = mustEnv("PLAYWRIGHT_REQUIRE_PATH");
const browserExecutable = mustEnv("PLAYWRIGHT_CHROMIUM_EXECUTABLE");

const require = createRequire(import.meta.url);
const { chromium } = require(playwrightPath);

const browser = await chromium.launch({ executablePath: browserExecutable, headless: true });
let context;

try {
  await verifyChineseAuth();
  context = await browser.newContext({ locale: "en-US" });
  const page = await context.newPage();
  page.setDefaultTimeout(10_000);
  page.on("pageerror", (error) => console.error(`browser page error: ${error.stack || error.message}`));
  page.on("console", (message) => {
    if (message.type() === "error") console.error(`browser console: ${message.text()}`);
  });

  await assertStatus(page, "/app.js", 404);
  await assertStatus(page, "/unknown-route", 404);

  await page.goto(`${baseURL}/`, { waitUntil: "networkidle" });
  await page.getByRole("tab", { name: "Login" }).waitFor();
  await page.getByRole("button", { name: /Black|黑/ }).click();
  if ((await page.evaluate(() => localStorage.getItem("gosshd_theme"))) !== "dark") throw new Error("dark theme was not persisted");
  await page.getByRole("button", { name: /White|白/ }).click();
  await page.getByRole("button", { name: "中文" }).click();
  await page.getByRole("tab", { name: "登录" }).waitFor();
  if ((await page.evaluate(() => localStorage.getItem("gosshd_locale"))) !== "zh-CN") throw new Error("locale was not persisted");
  await page.getByRole("button", { name: "EN" }).click();
  await page.getByRole("tab", { name: "Login" }).waitFor();

  await page.getByLabel("Email").fill("admin");
  await page.getByLabel("Password").fill("admin-pass");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expectText(page, "SSH ingress online");
  await expectText(page, /Access path|访问链路/);
  await expectText(page, /Private node|私有节点/);

  await page.getByRole("link", { name: /Public keys/ }).click();
  await page.getByRole("button", { name: "添加公钥" }).click();
  await page.getByLabel("标题").fill("React laptop");
  await page.getByRole("textbox", { name: "公钥" }).fill("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDUTThKwa4NlLwH7sntZnYosUoFkNceEce0kEwbE9nNm react-key");
  await page.getByRole("button", { name: "添加公钥" }).last().click();
  await expectText(page, "React laptop");

  await page.getByRole("link", { name: /Organizations/ }).click();
  await page.getByRole("button", { name: "创建组织" }).click();
  await page.getByLabel("组织名称").fill("React Ops");
  await page.getByLabel("组织标识").fill(`react-ops-${Date.now()}`);
  await page.getByRole("button", { name: "创建组织" }).last().click();
  await page.locator("tbody").getByText("React Ops").waitFor();

  await page.getByRole("link", { name: /SSH services/ }).click();
  await page.getByRole("button", { name: "添加服务" }).click();
  await page.getByLabel("服务名称").fill("React Service");
  await page.getByLabel("目标别名").fill("react-test2");
  await page.getByLabel("标签").fill("测试环境, common");
  await page.getByRole("button", { name: "下一步" }).click();
  await page.getByLabel("目标主机").fill("127.0.0.1");
  await page.getByLabel("目标端口").fill("22");
  await page.getByLabel("远程用户名").fill("root");
  await page.getByRole("button", { name: "下一步" }).click();
  await page.getByLabel("密钥或密码").fill("root-pass");
  await page.getByRole("button", { name: "添加服务" }).last().click();
  await expectText(page, "react-test2");
  await page.getByRole("button", { name: /复制连接命令/ }).first().click();
  await expectText(page, "Copied");

  await page.getByRole("button", { name: "添加服务" }).click();
  await page.getByRole("tab", { name: /Private node|私有节点/ }).click();
  await page.getByLabel("服务别名").fill("react-private");
  await page.getByRole("button", { name: "创建安装令牌" }).click();
  await expectText(page, "systemctl");
  await expectText(page, "sc.exe");
  await page.getByRole("button", { name: "Close" }).click();

  await page.getByRole("link", { name: /Command policy/ }).click();
  await page.getByRole("button", { name: "创建安全组" }).click();
  const policyDialog = page.getByRole("dialog", { name: "创建安全组" });
  await page.getByLabel("名称").fill("React readonly");
  await page.getByLabel("IP 白名单或范围").fill("private");
  await page.getByText("允许交互式终端").click();
  await policyDialog.getByRole("button", { name: "创建", exact: true }).click();
  await expectText(page, "React readonly");

  await page.getByRole("link", { name: /^Audit$/ }).click();
  await expectText(page, /No audit rows|暂无审计记录/);
  await expectStaticEmptyState(page);

  await page.getByRole("link", { name: /System admin/ }).click();
  await expectText(page, "账号管理");
  await page.getByRole("button", { name: /账号管理/ }).click();
  await page.getByRole("dialog", { name: "账号管理" }).waitFor();
  await page.getByRole("button", { name: "重置密码" }).first().click();
  await page.getByRole("dialog", { name: "重置用户密码" }).waitFor();
} finally {
  await context?.close().catch(() => {});
  await browser.close();
}

async function verifyChineseAuth() {
  const zh = await browser.newContext({ locale: "zh-CN" });
  try {
    const page = await zh.newPage();
    page.setDefaultTimeout(10_000);
    await page.goto(`${baseURL}/`, { waitUntil: "networkidle" });
    await page.getByRole("tab", { name: "登录" }).waitFor();
    await page.getByLabel("Email").fill("admin");
    await page.getByLabel("Password").fill("wrong-pass");
    await page.getByRole("button", { name: "Sign in" }).click();
    await page.locator(".status.error").filter({ hasText: "邮箱或密码不正确" }).waitFor();
    await expectCount(page.locator(".status.error").filter({ hasText: "invalid credentials" }), 0);
  } finally {
    await zh.close();
  }
}

async function assertStatus(page, route, expected) {
  const response = await page.goto(`${baseURL}${route}`, { waitUntil: "domcontentloaded" });
  if (!response || response.status() !== expected) {
    throw new Error(`${route} status mismatch: got ${response?.status()} want ${expected}`);
  }
}

async function expectText(page, text) {
  await page.getByText(text).first().waitFor();
}

async function expectCount(locator, expected) {
  const actual = await locator.count();
  if (actual !== expected) throw new Error(`count mismatch: got ${actual}, want ${expected}`);
}

async function expectStaticEmptyState(page) {
  const emptyState = page.locator(".empty-state").first();
  await emptyState.waitFor();
  const animationName = await emptyState.locator(".empty-orbit").evaluate((element) => getComputedStyle(element).animationName);
  if (animationName !== "none") throw new Error(`empty state should be static, got ${animationName}`);
}

function mustEnv(name) {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
}
