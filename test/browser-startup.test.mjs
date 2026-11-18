import test from 'node:test';
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {createServer} from 'node:http';
import {once} from 'node:events';
import {resolve} from 'node:path';

async function attempt(port) {
 const child=spawn(process.execPath,['test/browser.mjs'],{cwd:resolve('.'),windowsHide:true,stdio:['ignore','pipe','pipe'],env:{...process.env,TEST_PORT:String(port),TEST_BASE_PATH:'',SCHEDULER_BINARY:process.execPath,PLAYWRIGHT_MODULE:resolve('.test-artifacts/browser-tools/node_modules/playwright')}});
 let output='';child.stdout.on('data',data=>output+=data);child.stderr.on('data',data=>output+=data);
 const exited=once(child,'exit');
 let timer;
 const timeout=new Promise(resolve=>{timer=setTimeout(()=>resolve('timeout'),6000);});
 const result=await Promise.race([exited,timeout]);clearTimeout(timer);
 if(result==='timeout'){child.kill();await exited;}
 return {result,output};
}

test('browser harness rejects an occupied port without requesting the existing service',async t=>{
 let requests=0;
 const existing=createServer((_request,response)=>{requests++;response.setHeader('content-type','application/json');response.end('{"data":{"status":"ok"}}');});
 existing.listen(0,'127.0.0.1');await once(existing,'listening');
 t.after(()=>new Promise(resolve=>{existing.close(resolve);existing.closeAllConnections();}));
 const {result,output}=await attempt(existing.address().port);
 assert.notEqual(result,'timeout','Occupied-port failure must exit without hanging');
 assert.notEqual(result[0],0);assert.match(output,/EADDRINUSE/);
 assert.equal(requests,0,'Must not probe or mutate an existing service');
});

test('browser harness observes an early child exit and completes cleanup',async()=>{
 const {result,output}=await attempt(0);
 assert.notEqual(result,'timeout','Early child exit must not wait for a lost exit event');
 assert.notEqual(result[0],0);assert.match(output,/bad option|exited|启动/);
});
