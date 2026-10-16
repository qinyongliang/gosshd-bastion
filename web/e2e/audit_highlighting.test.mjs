import assert from "node:assert/strict";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_REQUIRE_PATH);
const browser = await chromium.launch(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE ? {executablePath:process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE,headless:true} : {channel:"chrome",headless:true});
const baseURL = process.env.GOSSHD_UI_E2E_BASE_URL;
assert.ok(baseURL);
const command = "python3 - <<'PY'\n" + '# <script> never execute </script>\nprint("example")\n'.repeat(200) + "PY";
const shortCommand = "const answer = 42;";
const record = (id, command) => ({id,user_id:"test-user",target_id:"test-target",command,policy_decision:"allow",policy_reason:"llm: safe",request_type:"exec",started_at:"2026-01-01T00:00:00Z",ended_at:"2026-01-01T00:00:01Z",exit_code:0});
try {
  const page = await browser.newPage({locale:"en-US"});
  const errors = [];
  page.on("pageerror", e => errors.push(e.message));
  await page.route("**/api/**", route => {
    const path = new URL(route.request().url()).pathname;
    const body = path === "/api/me" ? {user:{id:"test-user",email:"reader@example.com",display_name:"Reader",is_system_admin:false},organizations:[{id:"test-org",name:"Example",slug:"example",role:"owner"}],runtime:{ssh_host:"ssh.example.com",ssh_port:22,app_name:"Example",app_description:"Test"}}
    : path === "/api/audit" ? {logs:[record("long",command),record("short",shortCommand)],total:2,page:1,page_size:20}
    : path === "/api/audit/running" ? {logs:[]}
    : {keys:[],targets:[],credentials:[],folders:[],members:[],groups:[],policies:[],configs:[],prompts:[],reviews:[]};
    return route.fulfill({json:body});
  });
  await page.goto(`${baseURL}/audit`, {waitUntil:"domcontentloaded"});
  const preview = page.locator(".audit-cell-command");
  await preview.first().waitFor();
  assert.equal(await preview.count(),2);
  assert.equal(await preview.locator("span").count(),0,"list preview must not be highlighted");
  assert.ok((await preview.first().textContent()).length<=513);
  await preview.first().click();
  const detail=page.locator(".audit-detail-content");
  await detail.waitFor();
  assert.equal(await detail.textContent(),command,"expanded details must preserve full script");
  assert.equal(await detail.locator("script").count(),0);
  await page.keyboard.press("Escape");
  await preview.nth(1).click();
  await detail.locator("span").first().waitFor();
  assert.equal(await detail.textContent(),shortCommand);
  assert.deepEqual(errors,[]);
  console.log("Audit browser regression passed: plain bounded previews, full expanded scripts, on-demand multi-language highlighting, no script execution.");
} finally {await browser.close();}
