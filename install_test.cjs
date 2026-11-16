const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function browser(options = {}) {
  const listeners = {}, nodes = {};
  function element() {
    return {hidden: true, disabled: false, textContent: '', children: [], open: false,
      addEventListener(type, fn) { this[type === 'close' ? 'onclose' : type] = fn; },
      replaceChildren(...items) { this.children = items; },
      showModal() { this.open = true; }, close() { this.open = false; this.onclose?.(); },
      focus() { this.focused = true; }};
  }
  for (const id of ['install-button','install-dialog','install-description','install-steps','close-install']) nodes[id] = element();
  const display = {matches: false, addEventListener(type, fn) { this.change = fn; }};
  const context = {document: {getElementById: id => nodes[id], createElement: element},
    navigator: {userAgent: 'Windows Chrome', maxTouchPoints: 0, ...options.navigator},
    location: {protocol: options.protocol || 'https:'}, isSecureContext: options.secure !== false,
    matchMedia: () => display, addEventListener: (type, fn) => { listeners[type] = fn; },
    fetch() { throw Error('installation must not request business data'); }};
  Object.assign(display, options.display);
  context.window = context;
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, 'internal/web', 'install.js'), 'utf8'), context);
  return {nodes,listeners,display,guide: () => nodes['install-description'].textContent + nodes['install-steps'].children.map(n => n.textContent).join(' ')};
}
function promptEvent(outcome = 'dismissed') {
  return {calls:0, prevented:false, preventDefault() { this.prevented=true; },
    prompt() { this.calls++; return Promise.resolve(); }, userChoice: Promise.resolve({outcome})};
}

for (const [name,options,text] of [
  ['desktop',{},/Chrome.*Edge/],
  ['iPhone',{navigator:{userAgent:'iPhone Safari'}},/分享.*主屏幕/],
  ['iPad desktop UA',{navigator:{userAgent:'Macintosh Safari',maxTouchPoints:5}},/主屏幕/],
  ['Android',{navigator:{userAgent:'Android Chrome'}},/主屏幕/],
  ['Mac Safari',{navigator:{userAgent:'Macintosh Version Safari'}},/程序坞/],
  ['embedded browser',{navigator:{userAgent:'iPhone MicroMessenger'}},/浏览器.*Safari/],
  ['HTTP',{secure:false,protocol:'http:'},/HTTPS/],
  ['file',{protocol:'file:'},/HTTPS/],
]) test('install guidance: '+name, () => {
  const b=browser(options); b.nodes['install-button'].click();
  assert.equal(b.nodes['install-dialog'].open,true); assert.match(b.guide(),text);
  b.nodes['close-install'].click(); assert.equal(b.nodes['install-dialog'].open,false);
  assert.equal(b.nodes['install-button'].focused,true);
});

test('standalone modes hide entry; display changes restore it', () => {
  for (const options of [{display:{matches:true}},{navigator:{standalone:true}}]) {
    assert.equal(browser(options).nodes['install-button'].hidden,true);
  }
  const b=browser(); b.nodes['install-button'].click();
  b.display.matches=true; b.display.change();
  assert.equal(b.nodes['install-button'].hidden,true); assert.equal(b.nodes['install-dialog'].open,false);
  b.display.matches=false; b.display.change(); assert.equal(b.nodes['install-button'].hidden,false);
});
test('native installation is click-triggered, once per event; dismissal guides retry',async () => {
  const b=browser(), e=promptEvent(); b.listeners.beforeinstallprompt(e);
  assert.equal(e.calls,0); assert.equal(e.prevented,true);
  const click=b.nodes['install-button'].click(); b.nodes['install-button'].click();
  await click; assert.equal(e.calls,1); assert.equal(b.nodes['install-button'].disabled,false);
  assert.equal(b.nodes['install-dialog'].open,true); assert.match(b.guide(),/未添加/);
  b.nodes['close-install'].click(); await b.nodes['install-button'].click();
  assert.equal(e.calls,1); assert.equal(b.nodes['install-dialog'].open,true);
});
test('accepted is not installed; appinstalled hides entry and ignores late prompt',async () => {
  const b=browser(), e=promptEvent('accepted'); b.listeners.beforeinstallprompt(e);
  await b.nodes['install-button'].click(); assert.equal(b.nodes['install-button'].hidden,false);
  b.listeners.appinstalled(); assert.equal(b.nodes['install-button'].hidden,true);
  const late=promptEvent(); b.listeners.beforeinstallprompt(late);
  assert.equal(late.prevented,false); assert.equal(b.nodes['install-button'].hidden,true);
});
test('native prompt error restores entry and opens guidance',async () => {
  const b=browser(), e=promptEvent(); e.prompt=()=>Promise.reject(Error('failed'));
  b.listeners.beforeinstallprompt(e); await b.nodes['install-button'].click();
  assert.equal(b.nodes['install-button'].disabled,false); assert.equal(b.nodes['install-dialog'].open,true);
});
test('installation during prompt never reopens guidance',async () => {
  const b=browser(),e=promptEvent(); let resolve;
  e.userChoice=new Promise(r=>{resolve=r;}); b.listeners.beforeinstallprompt(e);
  const click=b.nodes['install-button'].click(); b.listeners.appinstalled(); resolve({outcome:'dismissed'}); await click;
  assert.equal(b.nodes['install-button'].hidden,true); assert.equal(b.nodes['install-dialog'].open,false);
});
test('insecure context cannot retain native prompt', () => {
  const b=browser({secure:false,protocol:'http:'}),e=promptEvent(); b.listeners.beforeinstallprompt(e);
  b.nodes['install-button'].click(); assert.equal(e.calls,0); assert.equal(e.prevented,false); assert.match(b.guide(),/HTTPS/);
});
