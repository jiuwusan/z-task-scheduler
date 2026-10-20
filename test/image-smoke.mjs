// Linux CI only. Uses its own containers and volume; refuses an existing scheduler.
import { execFileSync } from 'node:child_process';
import { statSync } from 'node:fs';
import assert from 'node:assert/strict';
const image=process.argv[2]||'z-task-scheduler:ci';
const docker=(...args)=>execFileSync('docker',args,{encoding:'utf8',stdio:['ignore','pipe','pipe']}).trim();
const ids=[];let volume='';let base='';const jar=new Map();let csrf='';
async function api(path,method='GET',body){const r=await fetch(base+'/api'+path,{method,headers:{'Content-Type':'application/json',Origin:base,Cookie:[...jar].map(([k,v])=>k+'='+v).join('; '),'X-CSRF-Token':csrf},...(body?{body:JSON.stringify(body)}:{})});for(const c of r.headers.getSetCookie()){const [name,value]=c.split(';')[0].split('=');jar.set(name,value);}const v=await r.json();assert.ok(r.ok,JSON.stringify(v));return v.data;}
async function ready(){for(let i=0;i<100;i++){try{if((await fetch(base+'/health')).ok)return;}catch{}await new Promise(r=>setTimeout(r,200));}throw new Error('Container never became healthy');}
function start(){const id=docker('run','-d','--name','z-task-scheduler','--label','io.jiuwusan.z-task-scheduler=true','--group-add',String(statSync('/var/run/docker.sock').gid),'-p','127.0.0.1::3000','-v','/var/run/docker.sock:/var/run/docker.sock','-v',volume+':/app/data',image);ids.push(id);const binding=docker('port',id,'3000/tcp').split('\n')[0];base='http://'+binding;return id;}
async function runTask(task){const run=await api('/tasks/'+task.id+'/run','POST',{});for(let i=0;i<100;i++){const v=await api('/runs/'+run.id);if(v.state!=='running'){assert.equal(v.state,'success',JSON.stringify(v));return v;}await new Promise(r=>setTimeout(r,100));}throw new Error('Execution did not finish');}
try{
 const existing=docker('ps','-a','--filter','name=^/z-task-scheduler$','--format','{{.ID}}');assert.equal(existing,'','Refusing to replace an existing z-task-scheduler container');
 volume=docker('volume','create');const schedulerID=start();await ready();assert.notEqual(docker('exec',schedulerID,'id','-u'),'0');docker('exec',schedulerID,'/app/scheduler','healthcheck');
 const setup=await api('/setup/status');csrf=setup.csrf;const credentials={username:'ci-admin',password:'ci-test-password'};await api('/setup','POST',credentials);csrf=(await api('/auth/login','POST',credentials)).csrf;
 assert.equal((await api('/status')).dockerAvailable,true);
 const target=docker('create','--label','io.jiuwusan.scheduler-test=true','--entrypoint','/bin/sleep',image,'600');ids.push(target);
 for(const action of ['start','restart','stop']){const task=await api('/tasks','POST',{name:'CI '+action,type:'docker',containerId:target,dockerAction:action,enabled:false,timeoutSeconds:30,schedule:{kind:'interval',intervalSeconds:60}});await runTask(task);assert.equal(docker('inspect','--format','{{.State.Running}}',target),action==='stop'?'false':'true');}
 let request,r;for(const config of [{url:'http://127.0.0.1:3000/health'},{url:'http://127.0.0.1:3000/api/auth/login',method:'POST',data:credentials}]){request=await api('/tasks','POST',{name:'CI HTTP request',type:'http',...config,enabled:false,schedule:{kind:'interval',intervalSeconds:60}});r=await runTask(request);assert.equal(r.result.httpStatus,200);}
 const containers=await api('/docker/containers');const self=containers.find(c=>c.self);assert.equal(self.id,schedulerID);
 const reject=await fetch(base+'/api/tasks',{method:'POST',headers:{'Content-Type':'application/json',Origin:base,Cookie:[...jar].map(([k,v])=>k+'='+v).join('; '),'X-CSRF-Token':csrf},body:JSON.stringify({name:'self',type:'docker',containerId:schedulerID,dockerAction:'stop',schedule:{kind:'interval',intervalSeconds:60}})});assert.equal(reject.status,400);
 docker('rm','-f',schedulerID);start();jar.clear();await ready();assert.equal((await api('/setup/status')).initialized,true);csrf=(await api('/auth/login','POST',credentials)).csrf;assert.ok((await api('/tasks')).some(t=>t.id===request.id));assert.ok((await api('/runs')).items.some(x=>x.id===r.id));
 console.log('PASS image: non-root, health, HTTP GET/JSON POST, Unix Docker socket, lifecycle/self guard, recreate persistence');
}catch(e){for(const id of ids){try{console.error(docker('logs','--tail','80',id));}catch{}}throw e;}
finally{for(const id of ids.reverse()){try{docker('rm','-f',id);}catch{}}if(volume){try{docker('volume','rm',volume);}catch{}}}
