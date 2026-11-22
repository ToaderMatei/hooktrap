/* HookTrap dashboard client. Vanilla JS, no build step. */
'use strict';

const $ = (sel, el = document) => el.querySelector(sel);
const api = {
  async get(path) { return this._json(await fetch(path)); },
  async send(method, path, body) {
    const opts = { method, headers: {} };
    if (body !== undefined) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
    return this._json(await fetch(path, opts));
  },
  async _json(res) {
    const txt = await res.text();
    let data = null;
    try { data = txt ? JSON.parse(txt) : null; } catch { data = txt; }
    if (!res.ok) throw new Error((data && data.error) || res.statusText || 'request failed');
    return data;
  },
};

const state = {
  cfg: null, version: 'dev', bins: [], currentBin: null,
  requests: [], currentReq: null, detailTab: 'pretty', search: '',
  tunnel: { running: false, url: '' }, tunnelAvailable: false,
};

function esc(s) { return String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c])); }
function timeAgo(iso) {
  const d = (Date.now() - new Date(iso).getTime()) / 1000;
  if (d < 2) return 'now';
  if (d < 60) return `${d | 0}s`;
  if (d < 3600) return `${(d / 60) | 0}m`;
  if (d < 86400) return `${(d / 3600) | 0}h`;
  return new Date(iso).toLocaleDateString();
}
function fmtBytes(n) { if (n < 1024) return `${n} B`; if (n < 1048576) return `${(n / 1024).toFixed(1)} KB`; return `${(n / 1048576).toFixed(1)} MB`; }
function b64ToText(b64) { try { return decodeURIComponent(escape(atob(b64))); } catch { return atob(b64 || ''); } }
function methodClass(r) { return r.kind === 'dns' ? 'DNS' : r.kind === 'tcp' ? 'TCP' : (r.method || 'GET'); }

function toast(msg, kind = '') {
  const el = document.createElement('div');
  el.className = `toast ${kind}`;
  el.textContent = msg;
  $('#toasts').appendChild(el);
  setTimeout(() => { el.style.opacity = '0'; setTimeout(() => el.remove(), 300); }, 3200);
}
async function copy(text) {
  try { await navigator.clipboard.writeText(text); toast('Copied to clipboard', 'ok'); }
  catch { toast('Copy failed — select manually', 'err'); }
}

/* ── Capture URL helpers ── */
function captureBase(binId) {
  if (state.cfg && state.cfg.publicUrl) return state.cfg.publicUrl.replace(/\/$/, '') + '/' + binId;
  const host = location.hostname;
  const port = state.cfg ? state.cfg.httpPort : 8080;
  return `http://${host}:${port}/${binId}`;
}

/* ── State / bins ── */
async function loadState() {
  const s = await api.get('/api/state');
  state.cfg = s.config; state.version = s.version; state.bins = s.bins || [];
  state.tunnel = s.tunnel || { running: false, url: '' };
  state.tunnelAvailable = s.tunnelAvailable;
  $('#version').textContent = 'v' + s.version;
  if (!state.currentBin && state.bins.length) selectBin(state.bins[0].id, false);
  renderBins(); renderTunnel();
}

function renderBins() {
  const el = $('#bin-list');
  el.innerHTML = state.bins.map(b => `
    <div class="bin ${b.id === state.currentBin ? 'active' : ''}" data-id="${esc(b.id)}">
      <div class="name"><span>${esc(b.name)}</span><span class="count">${b.count}</span></div>
      <div class="url">/${esc(b.id)}</div>
    </div>`).join('');
  el.querySelectorAll('.bin').forEach(n => n.onclick = () => selectBin(n.dataset.id));
}

async function selectBin(id, reload = true) {
  state.currentBin = id; state.currentReq = null;
  $('#endpoint-url').textContent = captureBase(id);
  try { history.replaceState(null, '', '#' + id); } catch { }
  renderBins();
  if (reload) { await loadRequests(); renderDetail(); }
}

async function loadRequests() {
  if (!state.currentBin) return;
  const q = encodeURIComponent(state.search);
  state.requests = await api.get(`/api/bins/${state.currentBin}/requests?limit=500&q=${q}`);
  renderRequests();
  // Auto-open the latest request so the detail pane is never empty on arrival.
  if (!state.currentReq && state.requests.length) openRequest(state.requests[0].id);
}

function renderRequests(flashId) {
  const el = $('#req-list');
  const list = state.starredOnly ? state.requests.filter(r => r.starred) : state.requests;
  if (!list.length) {
    el.innerHTML = `<div class="empty">${state.starredOnly ? 'No starred requests in this bin.' : 'No requests yet.<br />Fire one at the capture URL above.'}</div>`;
    return;
  }
  el.innerHTML = list.map(r => {
    const m = methodClass(r);
    const label = r.kind === 'http' ? (r.path || '/') + (r.query ? '?' + r.query : '') : (r.summary || r.kind);
    return `<div class="req ${r.id === state.currentReq ? 'active' : ''} ${r.id === flashId ? 'flash' : ''}" data-id="${esc(r.id)}">
      <span class="method m-${m}">${esc(m)}</span>
      <span class="path" title="${esc(label)}">${esc(label)}</span>
      ${r.starred ? '<span class="star">★</span>' : ''}
      <span class="meta">${esc(r.remoteAddr || '')} · ${timeAgo(r.time)}</span>
    </div>`;
  }).join('');
  el.querySelectorAll('.req').forEach(n => n.onclick = () => openRequest(n.dataset.id));
}

async function openRequest(id) {
  state.currentReq = id;
  try { const r = await api.get(`/api/requests/${id}`); state.reqCache = r; renderRequests(); renderDetail(r); }
  catch (e) { toast(e.message, 'err'); }
}

/* ── Detail ── */
function renderDetail(r) {
  const col = $('#detail-col');
  if (!r) {
    col.innerHTML = `<div class="placeholder"><div class="big">&gt;_</div><p>Select a request to inspect it.</p></div>`;
    return;
  }
  const bodyText = r.body ? b64ToText(r.body) : '';
  const m = methodClass(r);
  const tabs = ['pretty', 'raw', 'headers', 'curl'];
  col.innerHTML = `
    <div class="detail">
      <h3><span class="method m-${m}">${esc(m)}</span> ${esc(r.kind === 'http' ? (r.path || '/') : (r.summary || r.kind))}</h3>
      <div class="sub">${esc(r.id)} · ${new Date(r.time).toLocaleString()}</div>
      <div class="actions">
        <button class="btn sm" data-act="star">${r.starred ? '★ starred' : '☆ star'}</button>
        ${r.kind === 'http' ? '<button class="btn sm" data-act="replay">↻ replay</button>' : ''}
        <button class="btn sm ghost" data-act="copy-curl">copy curl</button>
        <button class="btn sm ghost" data-act="copy-fetch">copy fetch</button>
        <button class="btn sm ghost" data-act="copy-python">copy python</button>
        <button class="btn sm ghost" data-act="dl-json">json</button>
        <button class="btn sm ghost" data-act="dl-har">har</button>
        <button class="btn sm danger" data-act="del">delete</button>
      </div>
      <dl class="kv">
        <dt>source</dt><dd>${esc(r.remoteAddr || '—')}</dd>
        <dt>host</dt><dd>${esc(r.host || '—')}</dd>
        ${r.scheme ? `<dt>scheme</dt><dd>${esc(r.scheme)}</dd>` : ''}
        ${r.query ? `<dt>query</dt><dd>${esc(r.query)}</dd>` : ''}
        <dt>size</dt><dd>${fmtBytes(r.bodySize || 0)}${r.truncated ? ' (truncated)' : ''}</dd>
      </dl>
      ${(r.files && r.files.length) ? `<div class="files-note">📎 ${r.files.length} uploaded file(s): ${r.files.map(f => esc(f.filename) + ' (' + fmtBytes(f.size) + ')').join(', ')}</div>` : ''}
      <div class="tabs">${tabs.map(t => `<div class="tab ${t === state.detailTab ? 'active' : ''}" data-tab="${t}">${t}</div>`).join('')}</div>
      <div id="tab-content"></div>
      <div class="section-label">note</div>
      <div class="field"><textarea id="req-note" placeholder="Add a note (saved automatically)…" style="min-height:60px">${esc(r.note || '')}</textarea></div>
    </div>`;

  renderTab(r, bodyText);
  col.querySelectorAll('.tab').forEach(t => t.onclick = () => { state.detailTab = t.dataset.tab; renderDetail(r); });
  col.querySelectorAll('[data-act]').forEach(b => b.onclick = () => detailAction(b.dataset.act, r));
  const note = $('#req-note');
  let noteTimer;
  note.oninput = () => { clearTimeout(noteTimer); noteTimer = setTimeout(async () => {
    try { await api.send('PATCH', `/api/requests/${r.id}`, { note: note.value }); } catch (e) { toast(e.message, 'err'); }
  }, 600); };
}

function renderTab(r, bodyText) {
  const c = $('#tab-content');
  if (state.detailTab === 'headers') {
    c.innerHTML = `<div class="headers">${(r.headers || []).map(h => `<div class="h"><span class="hn">${esc(h.name)}:</span><span class="hv">${esc(h.value)}</span></div>`).join('') || '<em>no headers</em>'}</div>`;
    return;
  }
  if (state.detailTab === 'raw') {
    fetchText(`/api/requests/${r.id}/export?format=raw`, c); return;
  }
  if (state.detailTab === 'curl') {
    fetchText(`/api/requests/${r.id}/export?format=curl`, c, true); return;
  }
  // pretty
  let shown = bodyText, lang = '';
  const ct = (r.headers || []).find(h => h.name.toLowerCase() === 'content-type');
  if (ct && ct.value.includes('json')) { try { shown = JSON.stringify(JSON.parse(bodyText), null, 2); lang = 'json'; } catch { } }
  c.innerHTML = bodyText ? `<pre class="body">${esc(shown)}</pre>` : '<div class="empty">empty body</div>';
}

async function fetchText(url, container, withCopy) {
  try {
    const res = await fetch(url); const txt = await res.text();
    container.innerHTML = `<pre class="body">${esc(txt)}</pre>`;
    if (withCopy) { const b = document.createElement('button'); b.className = 'btn sm'; b.textContent = 'copy'; b.style.marginTop = '.5rem'; b.onclick = () => copy(txt); container.appendChild(b); }
  } catch (e) { container.innerHTML = `<div class="empty">${esc(e.message)}</div>`; }
}

async function detailAction(act, r) {
  try {
    if (act === 'star') { const u = await api.send('PATCH', `/api/requests/${r.id}`, { starred: !r.starred }); state.reqCache = u; await loadRequests(); renderDetail(u); }
    else if (act === 'copy-curl') { const res = await fetch(`/api/requests/${r.id}/export?format=curl`); copy(await res.text()); }
    else if (act === 'copy-fetch') { const res = await fetch(`/api/requests/${r.id}/export?format=fetch`); copy(await res.text()); }
    else if (act === 'copy-python') { const res = await fetch(`/api/requests/${r.id}/export?format=python`); copy(await res.text()); }
    else if (act === 'dl-json') download(`/api/requests/${r.id}/export?format=json`);
    else if (act === 'dl-har') download(`/api/requests/${r.id}/export?format=har`);
    else if (act === 'replay') replayModal(r);
    else if (act === 'del') { if (confirm('Delete this request?')) { await api.send('DELETE', `/api/requests/${r.id}`); state.currentReq = null; await loadRequests(); renderDetail(); } }
  } catch (e) { toast(e.message, 'err'); }
}
function download(url) { const a = document.createElement('a'); a.href = url; a.download = ''; document.body.appendChild(a); a.click(); a.remove(); }

/* ── Tunnel ── */
function renderTunnel() {
  const pill = $('#tunnel-pill'), txt = $('#tunnel-text');
  if (state.tunnel.running && state.tunnel.url) { pill.className = 'pill live'; txt.textContent = 'tunnel: on'; pill.title = state.tunnel.url; }
  else if (state.tunnel.running) { pill.className = 'pill'; txt.textContent = 'tunnel: starting…'; }
  else { pill.className = 'pill off'; txt.textContent = 'tunnel: off'; }
}

/* ── Live SSE ── */
function connectLive() {
  const es = new EventSource('/api/events');
  const pill = $('#live-pill'), txt = $('#live-text');
  es.addEventListener('ready', () => { pill.className = 'pill live'; txt.textContent = 'live'; });
  es.addEventListener('request', e => {
    const ev = JSON.parse(e.data);
    if (ev.request && ev.binId === state.currentBin) {
      if (state.search) { loadRequests(); }
      else { state.requests.unshift(ev.request); if (state.requests.length > 500) state.requests.pop(); renderRequests(ev.request.id); }
    }
    bumpBinCount(ev.binId);
  });
  es.addEventListener('cleared', e => { const ev = JSON.parse(e.data); if (ev.binId === state.currentBin) { state.requests = []; state.currentReq = null; renderRequests(); renderDetail(); } loadState(); });
  es.addEventListener('bins', () => loadState());
  es.addEventListener('tunnel', e => { const ev = JSON.parse(e.data); state.tunnel = { running: true, url: ev.message }; renderTunnel(); toast('Tunnel ready: ' + ev.message, 'ok'); $('#endpoint-url').textContent = captureBase(state.currentBin); });
  es.onerror = () => { pill.className = 'pill off'; txt.textContent = 'reconnecting…'; };
}
function bumpBinCount(binId) {
  const b = state.bins.find(x => x.id === binId); if (b) { b.count++; renderBins(); }
}

/* ── Modals ── */
function modal(title, bodyHTML, footHTML) {
  const root = $('#modal-root');
  root.innerHTML = `<div class="modal-backdrop"><div class="modal">
    <div class="modal-head"><h3>${title}</h3><button class="icon-btn" data-close>✕</button></div>
    <div class="modal-body">${bodyHTML}</div>
    ${footHTML ? `<div class="modal-foot">${footHTML}</div>` : ''}
  </div></div>`;
  const backdrop = $('.modal-backdrop', root);
  backdrop.onclick = e => { if (e.target === backdrop) closeModal(); };
  root.querySelectorAll('[data-close]').forEach(b => b.onclick = closeModal);
  return root;
}
function closeModal() { $('#modal-root').innerHTML = ''; }

function newBinModal() {
  modal('New capture bin', `
    <div class="field"><label>ID (optional — a random one is generated)</label><input id="nb-id" placeholder="e.g. oast1" /></div>
    <div class="field"><label>Name</label><input id="nb-name" placeholder="Friendly label" /></div>
    <div class="hint">The bin becomes reachable at /&lt;id&gt; on the capture listener (and as &lt;id&gt;.host if you use subdomains).</div>`,
    `<button class="btn ghost" data-close>cancel</button><button class="btn" id="nb-create">create</button>`);
  $('#nb-create').onclick = async () => {
    try { const b = await api.send('POST', '/api/bins', { id: $('#nb-id').value.trim(), name: $('#nb-name').value.trim() }); closeModal(); await loadState(); selectBin(b.id); toast('Bin created', 'ok'); }
    catch (e) { toast(e.message, 'err'); }
  };
}

function respondModal() {
  const bin = state.bins.find(b => b.id === state.currentBin);
  if (!bin) return;
  const r = bin.response || {};
  const hdrs = (r.headers || []).map(h => `${h.name}: ${h.value}`).join('\n');
  modal('Response for /' + esc(bin.id), `
    <div class="banner-note">Craft what this bin answers. Use {{ip}}, {{method}}, {{path}}, {{query}}, {{body}}, {{q:name}}, {{request_id}} as placeholders.</div>
    <div class="row">
      <div class="field"><label>Mode</label><select id="r-mode">
        <option value="static">static</option><option value="redirect">redirect</option><option value="echo">echo (reflect request as JSON)</option>
      </select></div>
      <div class="field"><label>Status</label><input id="r-status" type="number" value="${r.status || 200}" /></div>
      <div class="field"><label>Delay (ms)</label><input id="r-delay" type="number" value="${r.delayMs || 0}" /></div>
    </div>
    <div class="field"><label>Content-Type</label><input id="r-ct" value="${esc(r.contentType || 'text/plain; charset=utf-8')}" /></div>
    <div class="field" id="redir-field" style="display:none"><label>Redirect Location</label><input id="r-redir" value="${esc(r.redirectUrl || '')}" placeholder="https://internal.target/ or gopher://…" /></div>
    <div class="field"><label>Extra headers (one per line, Name: value)</label><textarea id="r-headers" placeholder="X-Powered-By: HookTrap">${esc(hdrs)}</textarea></div>
    <div class="field"><label>Body</label><textarea id="r-body" style="min-height:120px">${esc(r.body || '')}</textarea></div>
    <label class="checkbox"><input type="checkbox" id="r-cors" ${r.cors ? 'checked' : ''} /> Send permissive CORS headers (reply to pre-flight)</label>`,
    `<button class="btn ghost" data-close>cancel</button><button class="btn" id="r-save">save response</button>`);
  const modeSel = $('#r-mode'); modeSel.value = r.mode || 'static';
  const toggleRedir = () => { $('#redir-field').style.display = modeSel.value === 'redirect' ? 'block' : 'none'; };
  modeSel.onchange = toggleRedir; toggleRedir();
  $('#r-save').onclick = async () => {
    const headers = $('#r-headers').value.split('\n').map(l => l.trim()).filter(Boolean).map(l => { const i = l.indexOf(':'); return i < 0 ? { name: l, value: '' } : { name: l.slice(0, i).trim(), value: l.slice(i + 1).trim() }; });
    const resp = { mode: modeSel.value, status: +$('#r-status').value || 200, contentType: $('#r-ct').value, redirectUrl: $('#r-redir').value, delayMs: +$('#r-delay').value || 0, body: $('#r-body').value, cors: $('#r-cors').checked, headers };
    try { await api.send('PUT', `/api/bins/${bin.id}`, { response: resp }); bin.response = resp; closeModal(); toast('Response saved', 'ok'); }
    catch (e) { toast(e.message, 'err'); }
  };
}

async function payloadsModal() {
  modal('Payload / OAST generator', '<div class="empty">loading…</div>');
  try {
    const d = await api.get(`/api/payloads?bin=${state.currentBin || 'default'}`);
    const body = d.groups.map(g => `
      <div class="pl-group">
        <h4>${esc(g.category)}</h4>
        <p class="blurb">${esc(g.blurb)}</p>
        ${g.snippets.map((s, i) => `
          <div class="snippet">
            <div class="s-head"><span>${esc(s.title)} · ${esc(s.lang)}</span><button class="btn sm" data-copy="${esc(g.category)}-${i}">copy</button></div>
            <pre data-code="${esc(g.category)}-${i}">${esc(s.code)}</pre>
            ${s.note ? `<div class="s-note">${esc(s.note)}</div>` : ''}
          </div>`).join('')}
      </div>`).join('');
    modal('Payload / OAST generator for /' + esc(d.bin), `
      <div class="banner-note">These are out-of-band callback templates (the webhook.site / Collaborator idea) wired to your capture URL. Use them only against targets you are authorised to test.</div>${body}`,
      `<button class="btn" data-close>done</button>`);
    $('#modal-root').querySelectorAll('[data-copy]').forEach(b => b.onclick = () => { const code = $(`[data-code="${b.dataset.copy}"]`).textContent; copy(code); });
  } catch (e) { toast(e.message, 'err'); }
}

async function filesModal() {
  const render = async () => {
    const list = await api.get('/api/files');
    const items = list.length ? list.map(f => `
      <div class="file-item">
        <span class="fname">/f/${esc(f.name)}</span>
        <span class="fmeta">${fmtBytes(f.size)} · ${esc(f.contentType)}</span>
        <button class="btn sm ghost" data-copy="${esc(f.name)}">url</button>
        <button class="btn sm danger" data-del="${esc(f.name)}">del</button>
      </div>`).join('') : '<div class="empty">No hosted files.</div>';
    modal('Hosted files', `
      <div class="banner-note">Files served at <code>/f/&lt;name&gt;</code> on the capture listener. Useful for OOB-XXE DTDs, JS includes and fixtures. Each fetch is logged into the default bin.</div>
      ${items}
      <div class="section-label">add a file</div>
      <div class="field"><label>Name</label><input id="f-name" placeholder="oob.dtd" /></div>
      <div class="field"><label>Content</label><textarea id="f-content" style="min-height:120px" placeholder="paste text content, or use upload below"></textarea></div>
      <div class="field"><label>…or upload a file</label><input id="f-upload" type="file" /></div>`,
      `<button class="btn ghost" data-close>close</button><button class="btn" id="f-save">add file</button>`);
    $('#modal-root').querySelectorAll('[data-del]').forEach(b => b.onclick = async () => { try { await api.send('DELETE', `/api/files/${b.dataset.del}`); render(); } catch (e) { toast(e.message, 'err'); } });
    $('#modal-root').querySelectorAll('[data-copy]').forEach(b => b.onclick = () => copy(captureBaseRoot() + '/f/' + b.dataset.copy));
    $('#f-save').onclick = async () => {
      const file = $('#f-upload').files[0];
      try {
        if (file) { const fd = new FormData(); fd.append('file', file); if ($('#f-name').value.trim()) fd.append('name', $('#f-name').value.trim()); await fetch('/api/files', { method: 'POST', body: fd }).then(r => { if (!r.ok) throw new Error('upload failed'); }); }
        else { const name = $('#f-name').value.trim(); if (!name) return toast('Name required', 'err'); await fetch(`/api/files?name=${encodeURIComponent(name)}`, { method: 'POST', body: $('#f-content').value }).then(async r => { if (!r.ok) throw new Error((await r.json()).error); }); }
        toast('File saved', 'ok'); render();
      } catch (e) { toast(e.message, 'err'); }
    };
  };
  render();
}
function captureBaseRoot() {
  if (state.cfg && state.cfg.publicUrl) return state.cfg.publicUrl.replace(/\/$/, '');
  return `http://${location.hostname}:${state.cfg ? state.cfg.httpPort : 8080}`;
}

function settingsModal() {
  const c = state.cfg;
  modal('Settings', `
    <div class="banner-note">Port and extra-listener changes apply after restarting HookTrap. Capture behaviour (catch-all, body limit) applies live.</div>
    <div class="row">
      <div class="field"><label>Bind address</label><input id="s-bind" value="${esc(c.bindAddr)}" /><div class="hint">0.0.0.0 = reachable on the LAN; 127.0.0.1 = local only</div></div>
      <div class="field"><label>Capture HTTP port</label><input id="s-http" type="number" value="${c.httpPort}" /></div>
      <div class="field"><label>Dashboard port</label><input id="s-admin" type="number" value="${c.adminPort}" /></div>
    </div>
    <div class="row">
      <label class="checkbox"><input type="checkbox" id="s-https" ${c.httpsEnabled ? 'checked' : ''} /> HTTPS capture (self-signed)</label>
      <div class="field"><label>HTTPS port</label><input id="s-httpsport" type="number" value="${c.httpsPort}" /></div>
    </div>
    <div class="field"><label>Public URL (e.g. your tunnel URL — used by the payload generator)</label><input id="s-public" value="${esc(c.publicUrl || '')}" placeholder="https://abc.trycloudflare.com" /></div>
    <div class="row">
      <div class="field"><label>Max requests kept per bin</label><input id="s-max" type="number" value="${c.maxPerBin}" /></div>
      <div class="field"><label>Max body captured (KB)</label><input id="s-body" type="number" value="${c.maxBodyKb}" /></div>
    </div>
    <label class="checkbox"><input type="checkbox" id="s-catch" ${c.catchAll ? 'checked' : ''} /> Catch-all: log requests to unknown paths into the default bin</label>
    <div class="section-label">extra listeners</div>
    <div class="row">
      <label class="checkbox"><input type="checkbox" id="s-dns" ${c.dnsEnabled ? 'checked' : ''} /> DNS capture</label>
      <div class="field"><label>DNS port</label><input id="s-dnsport" type="number" value="${c.dnsPort}" /></div>
      <div class="field"><label>DNS answer IP</label><input id="s-dnsip" value="${esc(c.dnsAnswerIp)}" /></div>
    </div>
    <div class="row">
      <label class="checkbox"><input type="checkbox" id="s-tcp" ${c.tcpEnabled ? 'checked' : ''} /> Raw TCP capture</label>
      <div class="field"><label>TCP port</label><input id="s-tcpport" type="number" value="${c.tcpPort}" /></div>
    </div>
    <div class="field"><label>TCP banner (sent on connect, optional)</label><input id="s-banner" value="${esc(c.tcpBanner || '')}" /></div>
    <label class="checkbox"><input type="checkbox" id="s-open" ${c.openBrowser ? 'checked' : ''} /> Open dashboard in browser on start</label>`,
    `<button class="btn ghost" data-close>cancel</button><button class="btn" id="s-save">save</button>`);
  $('#s-save').onclick = async () => {
    const next = { ...c,
      bindAddr: $('#s-bind').value.trim(), httpPort: +$('#s-http').value, adminPort: +$('#s-admin').value,
      httpsEnabled: $('#s-https').checked, httpsPort: +$('#s-httpsport').value, publicUrl: $('#s-public').value.trim(),
      maxPerBin: +$('#s-max').value, maxBodyKb: +$('#s-body').value, catchAll: $('#s-catch').checked,
      dnsEnabled: $('#s-dns').checked, dnsPort: +$('#s-dnsport').value, dnsAnswerIp: $('#s-dnsip').value.trim(),
      tcpEnabled: $('#s-tcp').checked, tcpPort: +$('#s-tcpport').value, tcpBanner: $('#s-banner').value, openBrowser: $('#s-open').checked };
    try { const res = await api.send('POST', '/api/config', next); state.cfg = res.config; closeModal(); toast(res.message || 'Saved', 'ok'); await loadState(); selectBin(state.currentBin); }
    catch (e) { toast(e.message, 'err'); }
  };
}

function replayModal(r) {
  const guess = (r.scheme || 'http') + '://' + (r.host || 'target.example') + (r.path || '/') + (r.query ? '?' + r.query : '');
  modal('Replay request', `
    <div class="banner-note">Re-send this captured request (its method, headers and body) to a target of your choice — like Burp Repeater. Outbound request made by HookTrap; use only against systems you are authorised to test.</div>
    <div class="field"><label>Target URL</label><input id="rp-url" value="${esc(guess)}" placeholder="http://127.0.0.1:8000/path" /></div>
    <div class="field"><label>Method (blank = keep ${esc(r.method || 'GET')})</label><input id="rp-method" placeholder="${esc(r.method || 'GET')}" /></div>
    <div id="rp-result"></div>`,
    `<button class="btn ghost" data-close>close</button><button class="btn" id="rp-send">↻ send</button>`);
  $('#rp-send').onclick = async () => {
    const out = $('#rp-result');
    out.innerHTML = '<div class="empty">sending…</div>';
    try {
      const res = await api.send('POST', `/api/requests/${r.id}/replay`, { url: $('#rp-url').value.trim(), method: $('#rp-method').value.trim() });
      if (!res.ok) { out.innerHTML = `<div class="section-label">error</div><pre class="body">${esc(res.error)}</pre>`; return; }
      out.innerHTML = `<div class="section-label">response · ${res.status} · ${res.ms} ms · ${fmtBytes(res.bodySize)}</div>
        <div class="headers">${(res.headers || []).slice(0, 20).map(h => `<div class="h"><span class="hn">${esc(h.name)}:</span><span class="hv">${esc(h.value)}</span></div>`).join('')}</div>
        <pre class="body" style="margin-top:.6rem">${esc(res.body || '')}</pre>`;
    } catch (e) { out.innerHTML = `<div class="section-label">error</div><pre class="body">${esc(e.message)}</pre>`; }
  };
}

function helpModal() {
  modal('Help &amp; safety', `
    <div class="banner-note">HookTrap is for authorised security testing, lab exercises and CTFs. Capturing traffic from systems you do not own or have no permission to test may be illegal. You are responsible for how you use it.</div>
    <p style="font-size:.85rem;color:var(--text-secondary)">
      <b>Bins</b> are capture endpoints. Point any client at <code>${esc(captureBaseRoot())}/&lt;bin&gt;</code> and every request is logged live.<br><br>
      <b>Respond</b> lets a bin reply with a chosen status, headers, body (with {{placeholders}}), a redirect, an echo, or an artificial delay.<br><br>
      <b>Payload generator</b> builds out-of-band callback snippets (blind XSS, SSRF, OOB-XXE, DNS/TCP) wired to your URL.<br><br>
      <b>Hosted files</b> serve small payloads/fixtures at <code>/f/&lt;name&gt;</code>.<br><br>
      <b>Tunnel</b> exposes the capture port publicly via <code>cloudflared</code> (install it separately and it appears here).<br><br>
      <b>Replay</b> re-sends a captured request to a target you choose (Burp-Repeater style).<br><br>
      <b>Export</b> any request as curl, fetch, python, raw HTTP, JSON or HAR.
    </p>
    <div class="hint">Keyboard: / focus search · j/k next/prev request · n new bin · r respond editor</div>`,
    `<button class="btn" data-close>got it</button>`);
}

/* ── Menus (export/respond dropdowns as simple prompts) ── */
function exportBinMenu() {
  if (!state.currentBin) return;
  modal('Export bin /' + esc(state.currentBin), `<p style="font-size:.85rem;color:var(--text-secondary)">Download all captured requests in this bin.</p>`,
    `<button class="btn ghost" data-close>cancel</button><button class="btn" id="ex-json">JSON</button><button class="btn" id="ex-har">HAR</button>`);
  $('#ex-json').onclick = () => { download(`/api/bins/${state.currentBin}/export?format=json`); closeModal(); };
  $('#ex-har').onclick = () => { download(`/api/bins/${state.currentBin}/export?format=har`); closeModal(); };
}

/* ── Wiring ── */
function wire() {
  $('#new-bin-btn').onclick = newBinModal;
  $('#respond-btn').onclick = respondModal;
  $('#payloads-btn').onclick = payloadsModal;
  $('#files-btn').onclick = filesModal;
  $('#settings-btn').onclick = settingsModal;
  $('#settings-link').onclick = settingsModal;
  $('#help-btn').onclick = helpModal;
  $('#clear-bin-btn').onclick = async () => { if (state.currentBin && confirm('Clear all requests in this bin?')) { try { await api.send('POST', `/api/bins/${state.currentBin}/clear`); } catch (e) { toast(e.message, 'err'); } } };
  $('#export-bin-btn').onclick = exportBinMenu;
  $('#starred-btn').onclick = e => { state.starredOnly = !state.starredOnly; e.currentTarget.classList.toggle('ghost', !state.starredOnly); e.currentTarget.style.color = state.starredOnly ? 'var(--yellow)' : ''; renderRequests(); };
  $('#copy-endpoint').onclick = () => copy(captureBase(state.currentBin));
  $('#endpoint-url').onclick = () => copy(captureBase(state.currentBin));
  $('#theme-btn').onclick = toggleTheme;
  $('#tunnel-pill').onclick = toggleTunnel;
  let searchTimer;
  $('#search').oninput = e => { state.search = e.target.value; clearTimeout(searchTimer); searchTimer = setTimeout(loadRequests, 250); };

  document.addEventListener('keydown', e => {
    if (e.target.matches('input, textarea')) { if (e.key === 'Escape') e.target.blur(); return; }
    if ($('#modal-root').innerHTML && e.key === 'Escape') return closeModal();
    if (e.key === '/') { e.preventDefault(); $('#search').focus(); }
    else if (e.key === 'n') newBinModal();
    else if (e.key === 'r') respondModal();
    else if (e.key === 'j' || e.key === 'k') moveSelection(e.key === 'j' ? 1 : -1);
  });
}
function moveSelection(dir) {
  if (!state.requests.length) return;
  let i = state.requests.findIndex(r => r.id === state.currentReq);
  i = Math.max(0, Math.min(state.requests.length - 1, i + dir));
  openRequest(state.requests[i].id);
}

function toggleTheme() {
  const cur = document.documentElement.getAttribute('data-theme');
  const next = cur === 'light' ? 'dark' : 'light';
  document.documentElement.setAttribute('data-theme', next);
  try { localStorage.setItem('hooktrap-theme', next); } catch { }
}
async function toggleTunnel() {
  if (!state.tunnelAvailable) { toast('Install cloudflared and restart to use tunnels', 'err'); return; }
  try {
    if (state.tunnel.running) { await api.send('POST', '/api/tunnel/stop'); state.tunnel = { running: false, url: '' }; toast('Tunnel stopped'); }
    else { await api.send('POST', '/api/tunnel/start'); state.tunnel.running = true; toast('Starting tunnel…'); }
    renderTunnel();
  } catch (e) { toast(e.message, 'err'); }
}

async function init() {
  try { const t = localStorage.getItem('hooktrap-theme'); if (t) document.documentElement.setAttribute('data-theme', t); } catch { }
  wire();
  // Capture the requested bin from the URL before loadState() auto-selects one.
  const hashBin = decodeURIComponent((location.hash || '').replace(/^#\/?/, ''));
  try {
    await loadState();
    if (hashBin && state.bins.some(b => b.id === hashBin)) await selectBin(hashBin);
    else if (state.currentBin) await loadRequests();
  } catch (e) { toast('Failed to load: ' + e.message, 'err'); }
  // ?live=0 disables the SSE stream (used for static screenshots / embedding).
  if (!/[?&]live=0\b/.test(location.search)) connectLive();
  else { const p = $('#live-pill'); p.className = 'pill'; $('#live-text').textContent = 'static'; }
  // ?shot=<tool> opens a panel on load (screenshots / demos).
  const shot = new URLSearchParams(location.search).get('shot');
  if (shot === 'payloads') payloadsModal();
  else if (shot === 'files') filesModal();
  else if (shot === 'settings') settingsModal();
}
init();
