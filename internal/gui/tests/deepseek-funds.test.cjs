// Node test runner + Playwright. All API and asset requests are intercepted.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const assets = process.env.MAGPIE_FUNDS_ASSETS || path.resolve(__dirname, "../assets");
function serve(lang, panel, data) {
  const settings = { theme: "dark", lang, tray: "panel", quotaLeft: false, currency: "usd" };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"dark",web:${!panel}};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json(data);
    if (url.pathname === "/api/usage/quotas/history") return json([]);
    if (url.pathname === "/api/usage") return json({calls:0,errors:0,input:0,output:0,cache_read:0,cache_write:0,reasoning:0,unpriced:0,cost:0,bucket:"day",series:[],agents:[],models:[]});
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const words = {
 en: ["Granted balance", "Topped-up balance", "Total available", "Used first"],
 zh: ["赠金剩余", "充值余额", "合计可用", "优先消耗"],
 "zh-TW": ["贈金剩餘", "儲值餘額", "合計可用", "優先使用"],
 ja: ["付与残高", "チャージ残高", "利用可能な合計", "優先使用"],
 de: ["Bonusguthaben", "Aufgeladenes Guthaben", "Insgesamt verfügbar", "Zuerst verwendet"],
};
function fixtures(state) {
 const now = Date.now();
 const q = {provider:"my-deepseek",name:"DeepSeek",icon:"deepseek",windows:[],balance:"¥110.00",readAt:new Date(now).toISOString(),
  balanceFunds:[{currency:"CNY",granted:10,toppedUp:100}],
  balanceTrend:{points:[{at:new Date(now-86400000).toISOString(),amount:120},{at:new Date(now).toISOString(),amount:110}]}};
 if(state===1) {q.balance="¥100.00";q.balanceFunds[0].granted=0;}
 if(state===2) {q.balanceFunds[0].granted=null;q.balanceFunds[0].toppedUp=null;}
 if(state===3) {q.balance="$5.00 · ¥-0.05";q.balanceFunds=[{currency:"USD",granted:5,toppedUp:null},{currency:"CNY",granted:0,toppedUp:-.05}];q.balanceTrend.currency="$";}
 if(state===4) {q.asOf=new Date(now-3600000).toISOString();q.readAt=q.asOf;}
 return [q,{provider:"other",name:"OpenRouter",icon:"generic",windows:[],balance:"$3.11"}];
}
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
 for (const [lang,w] of Object.entries(words)) {
  test(`${engine} ${lang}: grants before top-ups, unknowns, currencies, stale data and total trend`, async (t) => {
   const browser=await (engine==="webkit"?webkit.launch():chromium.launch({channel:"chromium"}));
   t.after(()=>browser.close());
   const context=await browser.newContext({viewport:{width:1100,height:850},reducedMotion:"reduce"});
   const errors=[];let data=fixtures(0);const page=await context.newPage();page.setDefaultTimeout(8000);
   page.on("pageerror",e=>errors.push(e.message));
   await page.route("**/*",route=>serve(lang,false,data)(route));
   const card=page.locator(".subscription-card",{hasText:"DeepSeek"});
   const check=async(state)=>{
    await card.locator(".funds-row").first().waitFor();
    assert.equal(await card.locator(".funds-row").count(),3);
    assert.equal(await card.locator(".funds-grant .funds-label").textContent(),w[0]+([0,3,4].includes(state)?w[3]:""));
    assert.equal(await card.locator(".funds-cash .funds-label").textContent(),w[1]);
    assert.equal(await card.locator(".funds-total .funds-label").textContent(),w[2]);
    assert.equal(await card.locator(".funds-total b").textContent(),data[0].balance);
    assert.equal(await card.locator(".quota-track").count(),0,"no invented grant percentage");
    if(state===2) assert.deepEqual(await card.locator(".funds-grant b, .funds-cash b").allTextContents(),["—","—"],"unknown is not zero or old funds");
    if(state===1) assert.match(await card.locator(".funds-grant b").textContent(),/[¥￥]0[.,]00/);
    if(state===3) {assert.match(await card.locator(".funds-grant b").textContent(),/\$5[.,]00 · [¥￥]0[.,]00/);assert.match(await card.locator(".funds-cash b").textContent(),/\$— · [¥￥]-0[.,]05/);}
    assert.equal(await card.locator(".balance-curve path.qc-line").count(),1,"total history survives breakdown");
    assert((await card.locator(".balance-curve").getAttribute("title")).includes("\n"),"grant expiry caveat survives the normal curve tooltip");
    assert((await card.locator(".balance-curve .qc-key").first().textContent()).includes(w[2]),"legend identifies the total, not only top-ups");
    if(state===4) assert.equal(await card.locator(".quota-read.stale").count(),1,"retained amounts are dated");
    assert.equal(await page.locator(".subscription-card",{hasText:"OpenRouter"}).locator(".funds-row").count(),0,"neighbor's balance unchanged");
    const overflow=await card.evaluate(c=>[c,...c.querySelectorAll(".funds-row,.funds-label")].filter(e=>e.scrollWidth>e.clientWidth+1).map(e=>e.className));
    assert.deepEqual(overflow,[],"no clipped labels or amounts");
   };
   for(const state of [0,1,2,3,4,0]) {
    data=fixtures(state);await page.goto("http://magpie.test/?view=usage");await check(state);
    await page.setViewportSize({width:375,height:850});await check(state);
    await page.setViewportSize({width:1100,height:850});
   }
   if(process.env.ARTIFACT_DIR) {await fs.mkdir(process.env.ARTIFACT_DIR,{recursive:true});await page.screenshot({path:path.join(process.env.ARTIFACT_DIR,`${engine}-${lang}-funds.png`),fullPage:true});}
   // Light mode uses the same semantic rows and must not hide text.
   await page.evaluate(()=>document.documentElement.dataset.theme="light");await check(0);
   const panel=await context.newPage();panel.setDefaultTimeout(8000);panel.on("pageerror",e=>errors.push(e.message));
   await panel.setViewportSize({width:440,height:700});await panel.route("**/*",serve(lang,true,fixtures(0)));
   await panel.goto("http://magpie.test/?mode=panel");await panel.locator('#ptabs [data-ptab="usage"]').click();
   const pc=panel.locator(".pq-card.bal",{hasText:"DeepSeek"});await pc.locator(".funds-grant").waitFor();
   assert.equal(await pc.locator(".funds-total b").textContent(),"¥110.00");
   assert.equal(await pc.locator(".funds-row").count(),3);
   assert.equal(await pc.evaluate(e=>e.scrollWidth>e.clientWidth+1),false,"panel has no overflow");
   assert.deepEqual(errors,[]);
  });
 }
}
