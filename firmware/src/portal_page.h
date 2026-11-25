#pragma once

#include "portal_logo_inc.h"

namespace portalpage {

constexpr char kPage[] = R"HTML(<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<meta name="color-scheme" content="light dark">
<title>连接 Wi-Fi · today</title>
<style>
:root{--bg:#f4f4f6;--card:#fff;--fg:#1c1c21;--muted:#63636b;--line:#e3e3e8;--soft:#eeedfd;--accent:#4f46e5;--error:#b83232;--button:#1c1c21;--button-fg:#fff;font:15px/1.5 -apple-system,BlinkMacSystemFont,'PingFang SC',system-ui,sans-serif;color:var(--fg);background:var(--bg);color-scheme:light dark}
*{box-sizing:border-box}body{margin:0}main{max-width:440px;margin:0 auto;padding:28px 20px 32px}.brand{display:flex;align-items:center;gap:5px;margin-bottom:22px;font-weight:600;font-size:17px}.brand svg{width:28px;height:28px}h1{font-size:25px;line-height:1.25;font-weight:600;letter-spacing:-.03em;margin:0}.heading{display:flex;align-items:baseline;justify-content:space-between;gap:12px;margin-bottom:20px}.band{font-size:12px;color:var(--muted);white-space:nowrap}.saved{color:var(--muted);font-size:12px;margin:-8px 0 16px;overflow-wrap:anywhere}.saved strong{font-weight:500;color:var(--fg)}.saved span{display:block;margin-top:3px}button,input{font:inherit}input[type=text],input[type=password]{cursor:text}input[type=checkbox],.check-row{cursor:pointer}button{cursor:pointer}button:disabled{cursor:default;opacity:.5}button:focus-visible,summary:focus-visible{outline:2px solid var(--accent);outline-offset:3px}input{font-size:16px}.list-heading{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:8px}h2{font-size:13px;font-weight:500;margin:0}.rescan{display:flex;align-items:center;gap:5px;min-height:36px;border:0;border-radius:6px;background:transparent;color:var(--muted);padding:0 4px;font-size:12px}.rescan svg{width:15px;height:15px;fill:none;stroke:currentColor;stroke-width:1.6;stroke-linecap:round;stroke-linejoin:round}.nets{border:1px solid var(--line);background:var(--card);border-radius:12px;overflow:auto;max-height:248px}.net{display:flex;align-items:center;gap:10px;width:100%;min-height:48px;padding:10px 12px;border:0;border-bottom:1px solid var(--line);color:var(--fg);background:transparent;text-align:left}.net:last-child{border-bottom:0}.net:focus-visible{outline-offset:-3px;border-radius:4px}.net[aria-pressed=true]{background:var(--soft)}.name{min-width:0;flex:1;overflow:hidden;white-space:nowrap;text-overflow:ellipsis;font-size:14px}.net svg{width:18px;height:18px;flex:none;fill:none;stroke:currentColor;stroke-width:1.6;stroke-linecap:round;stroke-linejoin:round}.net .lock{width:14px;height:14px;color:var(--muted)}.net .check{color:var(--accent)}.empty{margin:0;padding:22px 14px;color:var(--muted);font-size:13px;line-height:1.6}.scan-state{font-size:12px;color:var(--muted);margin:8px 2px 0;min-height:0}.scan-state:empty{display:none}.scan-state.error,.error{color:var(--error)}details{margin:12px 0 18px}summary{list-style:none;display:inline-block;min-height:36px;padding:7px 0;font-size:13px;color:var(--muted);cursor:pointer}summary{border-radius:4px}summary::-webkit-details-marker{display:none}details[open] summary{margin-bottom:5px}label.field-label{display:block;font-size:13px;font-weight:500;margin:0 0 7px}.entry{position:relative}input[type=text],input[type=password]{display:block;width:100%;height:46px;border:1px solid var(--line);border-radius:9px;padding:0 12px;background:var(--card);color:var(--fg);min-width:0}input:focus{outline:2px solid var(--accent);outline-offset:1px}input::placeholder{color:var(--muted)}.check-row{display:flex;align-items:center;gap:7px;margin-top:10px;font-size:13px;color:var(--muted)}.check-row input{width:16px;height:16px;accent-color:var(--accent);margin:0}.password{margin-top:14px}.password input{padding-right:62px}.reveal{position:absolute;top:5px;right:6px;height:36px;min-width:46px;border:0;border-radius:5px;color:var(--muted);background:transparent;font-size:12px}.primary{width:100%;min-height:46px;padding:10px 16px;margin-top:18px;border:0;border-radius:9px;background:var(--button);color:var(--button-fg);font-size:15px;font-weight:500}.primary:disabled{opacity:.35}.error{font-size:13px;margin:10px 0 0;overflow-wrap:anywhere}.error:empty{display:none}.result{padding-top:12px}.result h2{font-size:19px;font-weight:600}.result p{font-size:14px;line-height:1.7;color:var(--muted);margin:10px 0 0}[hidden]{display:none!important}@media(hover:hover){.rescan:not(:disabled):hover,.reveal:not(:disabled):hover{background:var(--line)}.net:not(:disabled):not([aria-pressed=true]):hover{background:var(--bg)}summary:hover{color:var(--fg)}}@media(prefers-color-scheme:dark){:root{--bg:#1a1a1e;--card:#232327;--fg:#ededf1;--muted:#a1a1aa;--line:#333339;--soft:#2b2c47;--accent:#a5b4fc;--error:#f28b82;--button:#ededf1;--button-fg:#1a1a1e}}@media(max-width:350px){main{padding-left:16px;padding-right:16px}.heading{gap:8px}h1{font-size:23px}}
</style>
</head>
<body>
<main>
  <div class="brand">{{LOGO}}<span>today</span></div>
  <div class="heading"><h1>连接 Wi-Fi</h1><span class="band">仅支持 2.4 GHz</span></div>
  {{SAVED_NOTE}}
  <section id="networks" aria-labelledby="network-heading">
    <div class="list-heading"><h2 id="network-heading">附近的网络</h2><button id="rescan" class="rescan" type="button"><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M16 10a6 6 0 1 1-1.8-4.3M16 3v4h-4"/></svg><span>刷新</span></button></div>
    <div id="nets" class="nets" aria-label="附近的 Wi-Fi"><p class="empty">正在查找网络…</p></div>
    <p id="scan-state" class="scan-state" role="status" aria-live="polite"></p>
  </section>
  <form id="wifi-form" method="post" action="/save">
    <details id="manual"><summary>手动输入网络</summary>
      <label class="field-label" for="ssid">网络名称</label>
      <input id="ssid" type="text" name="ssid" maxlength="32" required autocomplete="off" autocapitalize="off" autocorrect="off" spellcheck="false" placeholder="Wi-Fi 名称">
      <label class="check-row"><input id="open-network" type="checkbox">无需密码</label>
    </details>
    <div id="passrow" class="password" hidden>
      <label class="field-label" for="pass">Wi-Fi 密码</label>
      <div class="entry"><input id="pass" name="pass" type="password" maxlength="63" autocomplete="off"><button id="reveal" class="reveal" type="button" aria-label="显示密码">显示</button></div>
    </div>
    <button id="connect" class="primary" type="submit" disabled>保存并连接</button>
    <p id="form-error" class="error" role="alert"></p>
  </form>
  <section id="result" class="result" role="status" hidden><h2>已保存</h2><p>显示屏正在重启连接 Wi-Fi。<br>连接后，在 Mac App 的「设备」中输入屏幕配对码。</p></section>
</main>
<script>
(function(){
  var ssid=document.getElementById('ssid'),pass=document.getElementById('pass'),passrow=document.getElementById('passrow'),manual=document.getElementById('manual'),open=document.getElementById('open-network'),list=document.getElementById('nets'),rescan=document.getElementById('rescan'),status=document.getElementById('scan-state'),connect=document.getElementById('connect'),error=document.getElementById('form-error'),reveal=document.getElementById('reveal'),form=document.getElementById('wifi-form');
  var selected='',scanning=false,saving=false,haveList=false;
  function update(){var hasName=ssid.value.length>0;passrow.hidden=!hasName||open.checked;pass.disabled=open.checked;pass.required=hasName&&!open.checked;connect.disabled=!hasName||saving;if(open.checked)pass.value='';}
  function selection(){list.querySelectorAll('.net').forEach(function(b){b.setAttribute('aria-pressed',String(b.dataset.ssid===selected));var c=b.querySelector('.check');if(c)c.hidden=b.dataset.ssid!==selected;});}
  function pick(n){if(ssid.value!==n.ssid){pass.value='';pass.type='password';reveal.textContent='显示';reveal.setAttribute('aria-label','显示密码');}ssid.value=n.ssid;selected=n.ssid;open.checked=!n.secure;manual.open=false;error.textContent='';update();selection();if(n.secure)pass.focus();}
  var signal='<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3 16v-3M8 16V9M13 16V5M18 16V2"/></svg>';
  var lock='<svg class="lock" viewBox="0 0 16 16" aria-hidden="true"><rect x="3" y="7" width="10" height="7" rx="2"/><path d="M5 7V4a3 3 0 0 1 6 0v3"/></svg>';
  var check='<svg class="check" viewBox="0 0 20 20" aria-hidden="true" hidden><path d="m4 10 4 4 8-8"/></svg>';
  function render(networks){list.replaceChildren();haveList=networks.length>0;if(!haveList){var empty=document.createElement('p');empty.className='empty';empty.textContent='未发现附近的 2.4 GHz 网络。也可以手动输入网络名称。';list.appendChild(empty);return;}networks.forEach(function(n){var b=document.createElement('button');b.type='button';b.className='net';b.dataset.ssid=n.ssid;b.setAttribute('aria-pressed','false');b.setAttribute('aria-label',n.ssid+(n.secure?'，需要密码':'，开放网络')+(n.saved?'，已保存':''));b.innerHTML=signal;var bars=b.querySelectorAll('path')[0];var level=n.rssi>-65?4:n.rssi>-80?3:2;var segs=['M3 16v-3','M8 16V9','M13 16V5','M18 16V2'];bars.remove();segs.forEach(function(d,i){var p=document.createElementNS('http://www.w3.org/2000/svg','path');p.setAttribute('d',d);p.setAttribute('opacity',i<level?'1':'.18');b.querySelector('svg').appendChild(p);});var name=document.createElement('span');name.className='name';name.textContent=n.ssid;b.appendChild(name);if(n.secure)b.insertAdjacentHTML('beforeend',lock);b.insertAdjacentHTML('beforeend',check);b.onclick=function(){pick(n);};list.appendChild(b);});selection();}
  async function request(url,options){var controller=new AbortController(),timer=setTimeout(function(){controller.abort();},5000);try{return await fetch(url,Object.assign({cache:'no-store',signal:controller.signal},options));}finally{clearTimeout(timer);}}
  async function scan(refresh){if(scanning)return;scanning=true;rescan.disabled=true;status.className='scan-state';status.textContent='正在查找网络…';var started=Date.now();try{for(;;){var response=await request(refresh?'/scan?refresh=1':'/scan');refresh=false;if(!response.ok)throw new Error('扫描失败，请重试。');var data=await response.json();if(data.failed)throw new Error('扫描失败，请重新刷新。');if(!data.scanning&&data.complete){render(data.networks);status.textContent='';break;}if(data.networks.length&&!haveList)render(data.networks);if(Date.now()-started>45000)throw new Error('扫描用时较长，请重新刷新。');await new Promise(function(resolve){setTimeout(resolve,700);});}}catch(e){status.className='scan-state error';status.textContent=e.name==='AbortError'||e instanceof TypeError?'与显示屏的连接中断，请确认仍连着 Today 热点后刷新。':e.message;if(!haveList){list.replaceChildren();var p=document.createElement('p');p.className='empty';p.textContent='暂时无法显示网络列表。';list.appendChild(p);}}finally{scanning=false;rescan.disabled=false;}}
  rescan.onclick=function(){scan(true);};
  ssid.oninput=function(){selected='';error.textContent='';selection();update();};
  open.onchange=function(){error.textContent='';update();};
  reveal.onclick=function(){var showing=pass.type==='password';pass.type=showing?'text':'password';reveal.textContent=showing?'隐藏':'显示';reveal.setAttribute('aria-label',showing?'隐藏密码':'显示密码');};
  form.onsubmit=async function(event){event.preventDefault();if(saving||!form.reportValidity())return;saving=true;error.textContent='';connect.textContent='正在保存…';update();try{var response=await request('/save',{method:'POST',body:new URLSearchParams(new FormData(form))});if(!response.ok)throw new Error(await response.text());form.hidden=true;var saved=document.querySelector('.saved');if(saved)saved.hidden=true;document.getElementById('networks').hidden=true;document.getElementById('result').hidden=false;}catch(e){error.textContent=e.name==='AbortError'||e instanceof TypeError?'暂时无法保存，请确认仍连着 Today 热点后重试。':e.message;}finally{saving=false;connect.textContent='保存并连接';update();}};
  scan(false);
})();
</script>
</body>
</html>)HTML";

}  // namespace portalpage
