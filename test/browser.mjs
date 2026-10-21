import { createRequire } from 'node:module';
import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { createServer } from 'node:http';
import assert from 'node:assert/strict';
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const data = await mkdtemp(join(tmpdir(), 'scheduler-browser-'));
const port = Number(process.env.TEST_PORT || 18310);
const requests=[];
const mock = createServer(async(req,res) => {let body='';for await(const part of req)body+=part;requests.push({method:req.method,headers:req.headers,body});res.setHeader('Content-Type','application/json');setTimeout(()=>res.end('{"ok":true}'),1200); });
await new Promise(r=>mock.listen(0,'127.0.0.1',r));
const child = spawn(resolve(process.env.SCHEDULER_BINARY || 'bin/scheduler.exe'), ['-listen',`127.0.0.1:${port}`,'-data-dir',data], {stdio:['ignore','pipe','pipe'],windowsHide:true});
let logs='';child.stdout.on('data',b=>logs+=b);child.stderr.on('data',b=>logs+=b);
let browser;
try {
 for(let i=0;i<100;i++){try{if((await fetch(`http://127.0.0.1:${port}/health`)).ok)break;}catch{}if(i===99)throw new Error(logs);await new Promise(r=>setTimeout(r,100));}
 browser=await chromium.launch({headless:true,...(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE?{executablePath:process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE}:{})});
 const page=await browser.newPage({viewport:{width:1440,height:1000},timezoneId:'Asia/Shanghai'});
 page.setDefaultTimeout(15000);
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(`http://127.0.0.1:${port}`);
 await page.locator('#username').fill('admin');await page.locator('#password').fill('test-password');await page.locator('#auth-submit').click();
 await page.getByText('管理员已创建，请登录').waitFor();
 await page.locator('#password').fill('test-password');await page.locator('#auth-submit').click();
 await page.locator('#app-view').waitFor({state:'visible'});await page.locator('#add-task').click();
 await page.locator('#task-name').fill('浏览器测试任务');
 const requestURL=`http://127.0.0.1:${mock.address().port}/`;
 for(const [invalid,message] of [['{','JSON 格式无效'],['[]','JSON 对象'],['null','JSON 对象'],['{"url":"http://localhost","type":"docker"}','不支持的字段'],['{}','url'],['{"url":"http://localhost","":0,"type":"docker"}','不支持的字段'],['{"url":"http://localhost","method":1}','method 必须'],['{"url":"http://localhost","headers":[]}','headers 必须'],['{"url":"http://localhost","headers":{"X-Test":1}}','headers 必须'],['{"url":"http://localhost","data":true}','data 必须']]){
  await page.locator('#http-config').fill(invalid);await page.locator('#save-task').click();await page.locator('#task-error').filter({hasText:message}).waitFor();
 }
 await page.locator('#http-config').fill('{"url":'+JSON.stringify(requestURL)+',"method":"POST","headers":{"X-Test":"browser-value"},"data":{"enabled":true,"message":"中文\\nline","id":9007199254740993,"large":1e400}}');
 await page.locator('#schedule-kind').selectOption('interval');await page.locator('#interval-seconds').fill('60');await page.locator('#save-task').click();
 await page.locator('#task-dialog').waitFor({state:'hidden'});let row=page.locator('tr').filter({hasText:'浏览器测试任务'});await row.waitFor();
 await row.getByRole('button',{name:'立即执行'}).click();
 await row.getByRole('button',{name:'执行记录'}).click();await page.getByRole('button',{name:'查看详情'}).first().click();
 await page.locator('#run-output').filter({hasText:'"ok":true'}).waitFor();await page.locator('#close-run').click();
 assert.equal(requests[0].method,'POST');assert.equal(requests[0].headers['x-test'],'browser-value');assert.match(requests[0].headers['content-type'],/application\/json/);assert.equal(JSON.parse(requests[0].body).message,'中文\nline');assert.match(requests[0].body,/"id"\s*:\s*9007199254740993/);
 await page.locator('[data-view="tasks"]').click();row=page.locator('tr').filter({hasText:'浏览器测试任务'});await row.getByRole('button',{name:'编辑',exact:true}).click();await page.locator('#task-dialog').waitFor({state:'visible'});
 const editedConfig=await page.locator('#http-config').inputValue();assert.equal(JSON.parse(editedConfig).method,'POST');assert.match(editedConfig,/"id"\s*:\s*9007199254740993/);assert.equal(JSON.parse(editedConfig).headers['X-Test'],'browser-value');
 assert.match(editedConfig,/"large"\s*:\s*1e400/);
 await page.locator('#save-task').click();await page.locator('#task-dialog').waitFor({state:'hidden'});
 await row.getByRole('button',{name:'暂停',exact:true}).click();await row.getByText('已暂停',{exact:true}).waitFor();
 await row.getByRole('button',{name:'启用',exact:true}).click();await row.getByText('已启用',{exact:true}).waitFor();
 await row.getByRole('button',{name:'编辑',exact:true}).click();await page.locator('#task-dialog').waitFor({state:'visible'});
 assert.match(await page.locator('#http-config').inputValue(),/"id"\s*:\s*9007199254740993/);assert.match(await page.locator('#http-config').inputValue(),/"large"\s*:\s*1e400/);
 await mkdir('.test-artifacts',{recursive:true});await page.screenshot({path:'.test-artifacts/http-json-editor.png',fullPage:true});
 await page.setViewportSize({width:390,height:844});await page.screenshot({path:'.test-artifacts/http-json-editor-mobile.png',fullPage:true});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>window.innerWidth),false);await page.setViewportSize({width:1440,height:1000});
 await page.locator('#http-config').fill(JSON.stringify({url:requestURL,data:'raw text\nliteral -: & 中文'},null,2));
 await page.locator('#task-name').fill('已编辑的任务');await page.locator('#task-enabled').uncheck();await page.locator('#save-task').click();await page.locator('#task-dialog').waitFor({state:'hidden'});
 await page.locator('tr').filter({hasText:'已编辑的任务'}).getByText('已暂停',{exact:true}).waitFor();
 await page.locator('tr').filter({hasText:'已编辑的任务'}).getByRole('button',{name:'立即执行'}).click();
 for(let i=0;requests.length<2&&i<100;i++)await new Promise(r=>setTimeout(r,100));assert.equal(requests[1]?.method,'GET');assert.equal(requests[1]?.body,'raw text\nliteral -: & 中文');
 // Wait for the second request to finish before deleting its task.
 await new Promise(r=>setTimeout(r,1600));
 await mkdir('.test-artifacts',{recursive:true});await page.screenshot({path:'.test-artifacts/dashboard.png',fullPage:true});
 await page.setViewportSize({width:390,height:844});await page.screenshot({path:'.test-artifacts/mobile.png',fullPage:true});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>window.innerWidth),false);
 await page.setViewportSize({width:1440,height:1000});page.once('dialog',d=>d.accept());await page.locator('tr').filter({hasText:'已编辑的任务'}).getByRole('button',{name:'删除',exact:true}).click();await page.getByText('暂无任务',{exact:true}).waitFor();
 await page.locator('[data-view="settings"]').click();await page.getByText('Docker 未连接',{exact:true}).waitFor();
 // Browser contract test: an ID-only legacy task must not silently bind the
 // first available container. The API fixture keeps this independent of Docker.
 let dockerTask={id:'legacy-docker',name:'旧 Docker 任务',type:'docker',containerId:'old-media-id',dockerAction:'stop',enabled:false,timeoutSeconds:60,schedule:{kind:'interval',intervalSeconds:60,timezone:'Asia/Shanghai'}};
 let dockerBody;let mediaID='new-media-id';
 await page.route('**/api/docker/containers',route=>route.fulfill({json:{data:[{id:'database-id',name:'database',state:'running'},{id:mediaID,name:'media',state:'running'}]}}));
 await page.route('**/api/tasks',route=>route.request().method()==='GET'?route.fulfill({json:{data:[dockerTask]}}):route.continue());
 await page.route('**/api/tasks/legacy-docker',route=>{if(route.request().method()==='PUT'){dockerBody=route.request().postDataJSON();dockerTask={...dockerBody,id:'legacy-docker'};}return route.fulfill({json:{data:dockerTask}});});
 await page.reload();await page.locator('#app-view').waitFor({state:'visible'});
 const dockerRow=page.locator('tr').filter({hasText:'旧 Docker 任务'});await dockerRow.getByRole('button',{name:'编辑',exact:true}).click();
 await page.locator('#task-dialog').waitFor({state:'visible'});assert.equal(await page.locator('#container-name').inputValue(),'');
 await page.locator('#save-task').click();assert.equal(dockerBody,undefined);assert.equal(await page.locator('#task-dialog').isVisible(),true);
 await page.locator('#container-name').selectOption('media');await page.locator('#save-task').click();await page.locator('#task-dialog').waitFor({state:'hidden'});
 assert.equal(dockerBody.containerName,'media');assert.equal(dockerBody.containerId,undefined);
 mediaID='recreated-media-id';await dockerRow.getByRole('button',{name:'编辑',exact:true}).click();await page.locator('#task-dialog').waitFor({state:'visible'});
 assert.equal(await page.locator('#container-name').inputValue(),'media');await page.locator('#cancel-task').click();
 assert.deepEqual(errors,[]);console.log('PASS browser: setup/login, HTTP JSON config/validation/precision/text CRUD/run/history, pause, mobile, Docker status/name binding/legacy selection');
} finally {await browser?.close();child.kill();await new Promise(r=>child.once('exit',r));await new Promise(r=>mock.close(r));await rm(data,{recursive:true,force:true,maxRetries:10,retryDelay:100});}
