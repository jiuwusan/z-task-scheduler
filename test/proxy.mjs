import {createServer, request} from 'node:http';

// Test-only proxy: strip a mount, rewrite Cookie Path and use the internal Host.
export async function startPrefixProxy(target, prefix) {
 if(!/^\/(?:[a-zA-Z0-9_-]+\/)*[a-zA-Z0-9_-]+$/.test(prefix))throw Error('Invalid test prefix');
 const upstreamURL=new URL(target);
 const server=createServer((req,res)=>{
  const url=new URL(req.url,'http://localhost');
  if(url.pathname===prefix){res.writeHead(308,{location:prefix+'/'+url.search});res.end();return;}
  if(!url.pathname.startsWith(prefix+'/')){res.writeHead(404);res.end();return;}
  const upstream=request(upstreamURL,{method:req.method,path:req.url.slice(prefix.length),headers:{...req.headers,host:upstreamURL.host,'x-forwarded-proto':'http'}},response=>{
   const headers={...response.headers};
   if(headers['set-cookie'])headers['set-cookie']=headers['set-cookie'].map(cookie=>cookie.replace(/Path=\//i,`Path=${prefix}/`));
   res.writeHead(response.statusCode,headers);response.pipe(res);
  });
  upstream.on('error',()=>{res.writeHead(502);res.end();});req.pipe(upstream);
 });
 await new Promise((resolve,reject)=>{server.once('error',reject);server.listen(0,'127.0.0.1',resolve);});
 return {baseURL:`http://127.0.0.1:${server.address().port}${prefix}/`,close:()=>new Promise(resolve=>{server.close(resolve);server.closeAllConnections();})};
}
