'use strict';
const $ = id => document.getElementById(id);
let user = null, links = [], page = 1, pages = 1, listController, listVersion = 0, searchTimer, editing, editTrigger;
let createdURL = '', resumeEdit = false;
const initialParams = new URLSearchParams(location.search);
$('search').value = initialParams.get('q') || '';
const requestedPage = Number(initialParams.get('page'));
page = Number.isSafeInteger(requestedPage) && requestedPage > 0 ? requestedPage : 1;

function notice(message, error = false) { $('notice').textContent = message; $('notice').classList.toggle('error', error); }
function busy(button, value) { button.disabled = value; button.setAttribute('aria-busy', String(value)); }
function fieldError(id, message, errorId) { const descriptions = new Set(($(id).getAttribute('aria-describedby') || '').split(' ').filter(Boolean)); descriptions.add(errorId); $(id).setAttribute('aria-describedby', [...descriptions].join(' ')); $(id).setAttribute('aria-invalid', 'true'); $(errorId).textContent = message; $(id).focus(); }
function clearErrors(form, errorId) { form.querySelectorAll('[aria-invalid]').forEach(el => el.removeAttribute('aria-invalid')); $(errorId).textContent = ''; }
function isHTTPURL(value) { try { const u = new URL(value); return ['http:', 'https:'].includes(u.protocol) && !u.username && !u.password && value.length <= 4096; } catch { return false; } }
function showUser(username) {
 user = username;
 $('login-panel').hidden = Boolean(user); $('workspace').hidden = !user; $('account').hidden = !user;
 $('account-name').textContent = user || ''; document.title = user ? 'Your links — Waymark' : 'Sign in — Waymark';
 if (!user) { if ($('edit-dialog').open) { resumeEdit = true; $('edit-dialog').close(); } listController?.abort(); listVersion++; links = []; $('link-list').replaceChildren(); }
}
async function api(path, options = {}) {
 let response;
 try { response = await fetch(path, { credentials: 'same-origin', ...options, signal: options.signal ? AbortSignal.any([options.signal, AbortSignal.timeout(15000)]) : AbortSignal.timeout(15000) }); }
 catch (err) { if (err.name === 'AbortError') throw err; throw new Error(options.method ? 'Connection lost. Check your links before trying the change again.' : 'Unable to connect. Check your connection and retry.'); }
 if (!response.ok) {
  const message = (await response.text()).trim();
  if (response.status === 401 && !path.endsWith('/login')) { showUser(null); notice('Your session ended. Sign in again; your form entries are still here.', true); }
  const requestID = response.headers.get('X-Request-ID');
  const err = new Error(response.status >= 500 ? `Server error. Try again later.${requestID ? ' Reference: ' + requestID : ''}` : message || 'Request failed.');
  err.status = response.status; throw err;
 }
 if (response.status === 204) return null;
 return response.json();
}
function jsonRequest(method, data) { return { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) }; }
function shortURL(code) { return `${location.origin}/${encodeURIComponent(code)}`; }
function formatDate(value) { return new Date(value).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }); }
function localDate(value) { if (!value) return ''; const d = new Date(value); d.setMinutes(d.getMinutes() - d.getTimezoneOffset()); return d.toISOString().slice(0, 16); }
function displayURL(raw) { try { const u = new URL(raw); if (u.username || u.password) { u.username = ''; u.password = ''; return u.href; } } catch {} return raw; }
function status(link) { if (!isHTTPURL(link.long_url)) return 'Needs update'; return link.disabled ? 'Disabled' : link.expires_at && new Date(link.expires_at) <= new Date() ? 'Expired' : 'Active'; }
function makeButton(label, action) { const button = document.createElement('button'); button.type = 'button'; button.className = 'quiet'; button.textContent = label; button.addEventListener('click', action); return button; }
function makeAnchor(text, href, className) { const a = document.createElement('a'); a.textContent = text; a.href = href; a.className = className; a.target = '_blank'; a.rel = 'noopener noreferrer'; return a; }
async function copy(text) {
 try { await navigator.clipboard.writeText(text); notice('Link copied to clipboard.'); }
 catch { notice('Clipboard unavailable. Select and copy the short link above.', true); }
}
function renderLinks() {
 const tbody = $('link-list'); tbody.replaceChildren();
 for (const link of links) {
  const row = document.createElement('tr'); const destinationCell = document.createElement('td');
  if (link.title) { const title = document.createElement('span'); title.className = 'link-title'; title.textContent = link.title; destinationCell.append(title); }
  destinationCell.append(makeAnchor(shortURL(link.short_code), shortURL(link.short_code), 'short-link'));
  // Records are validated by the server; refuse unsafe navigation even for manually edited legacy data.
  if (isHTTPURL(link.long_url)) destinationCell.append(makeAnchor(link.long_url, link.long_url, 'destination'));
  else { const text = document.createElement('span'); text.textContent = displayURL(link.long_url); destinationCell.append(text); }
  const date = document.createElement('span'); date.className = 'link-date'; date.textContent = `Created ${formatDate(link.created_at)}${link.expires_at ? ' · Expires ' + formatDate(link.expires_at) : ''}`; destinationCell.append(date);
  const clicks = document.createElement('td'); clicks.textContent = Number(link.clicks).toLocaleString();
  const state = document.createElement('td'); const badge = document.createElement('span'); badge.textContent = status(link); badge.className = 'badge ' + (status(link) === 'Active' ? 'active' : 'inactive'); state.append(badge);
  const actions = document.createElement('td'); const group = document.createElement('div'); group.className = 'row-actions';
  group.append(makeButton('Copy', () => copy(shortURL(link.short_code))), makeButton('Edit', event => openEdit(link, event.currentTarget))); actions.append(group);
  row.append(destinationCell, clicks, state, actions); tbody.append(row);
 }
 $('empty').hidden = links.length > 0;
 const heading = $('empty').querySelector('h3'); const hint = $('empty').querySelector('p');
 heading.textContent = $('search').value ? 'No matching links' : 'No links yet';
 hint.textContent = $('search').value ? 'Try another title, URL or code, or clear the search.' : 'Create your first short link above. It will appear here.';
 $('previous').disabled = page <= 1; $('next').disabled = page >= pages;
}
async function loadLinks() {
 const version = ++listVersion; listController?.abort(); listController = new AbortController();
 const params = new URLSearchParams({ page: String(page) }); const query = $('search').value.trim(); if (query) params.set('q', query);
 $('clear-search').hidden = !query; $('list-status').textContent = 'Loading links…'; $('link-list').setAttribute('aria-busy', 'true');
 try {
  const data = await api('/api/urls?' + params, { signal: listController.signal });
  if (version !== listVersion || !user) return;
  links = data.links; page = data.page; pages = data.pages;
  $('page-status').textContent = `${data.total.toLocaleString()} ${data.total === 1 ? 'link' : 'links'} · Page ${page} of ${pages}`;
  $('list-status').textContent = ''; renderLinks();
  const state = new URLSearchParams(); if (query) state.set('q', query); if (page > 1) state.set('page', page);
  history.replaceState(null, '', location.pathname + (state.size ? '?' + state : ''));
 } catch (err) { if (err.name !== 'AbortError' && version === listVersion) $('list-status').textContent = err.message; }
 finally { if (version === listVersion) $('link-list').removeAttribute('aria-busy'); }
}
async function loadStats() {
 try {
  const data = await api('/api/stats'); if (!user) return;
  $('stat-links').textContent = data.links.toLocaleString(); $('stat-active').textContent = data.active.toLocaleString(); $('stat-clicks').textContent = data.redirects.toLocaleString(); $('stats-error').textContent = '';
 } catch (err) { $('stats-error').textContent = err.message; }
}
function refresh() { return Promise.allSettled([loadLinks(), loadStats()]); }
$('login-form').addEventListener('submit', async event => {
 event.preventDefault(); if ($('login-submit').disabled) return; clearErrors(event.currentTarget, 'login-error');
 const username = $('username').value.trim(), password = $('password').value;
 if (!username) return fieldError('username', 'Enter your username.', 'login-error');
 if (!password) return fieldError('password', 'Enter your password.', 'login-error');
 busy($('login-submit'), true);
 try { const data = await api('/api/login', jsonRequest('POST', { username, password })); $('password').value = ''; showUser(data.username); notice('Signed in.'); await refresh(); if (resumeEdit) { resumeEdit = false; $('edit-dialog').showModal(); $('edit-url').focus(); } else $('destination').focus(); }
 catch (err) { fieldError('password', err.status === 401 ? 'Username or password was not recognised. Try again.' : err.message, 'login-error'); }
 finally { busy($('login-submit'), false); }
});
$('show-password').addEventListener('click', () => { const show = $('password').type === 'password'; $('password').type = show ? 'text' : 'password'; $('show-password').textContent = show ? 'Hide' : 'Show'; $('show-password').setAttribute('aria-label', show ? 'Hide password' : 'Show password'); $('show-password').setAttribute('aria-pressed', String(show)); });
$('logout').addEventListener('click', async () => {
 busy($('logout'), true);
 try { await api('/api/logout', { method: 'POST' }); showUser(null); resumeEdit = false; editing = null; $('create-form').reset(); $('created-result').hidden = true; $('search').value = ''; history.replaceState(null, '', '/'); page = 1; notice('Signed out.'); $('username').focus(); }
 catch (err) { notice(err.message, true); } finally { busy($('logout'), false); }
});
function readInput(prefix) {
 const urlID = prefix === 'create' ? 'destination' : 'edit-url'; const titleID = prefix === 'create' ? 'title' : 'edit-title'; const expiresID = prefix === 'create' ? 'expires' : 'edit-expires';
 const url = $(urlID).value.trim();
 if (!isHTTPURL(url)) { fieldError(urlID, 'Enter a complete HTTP or HTTPS URL without a username or password.', prefix + '-error'); return null; }
 const expiresValue = $(expiresID).value;
 if (expiresValue && (!Number.isFinite(new Date(expiresValue).getTime()) || new Date(expiresValue) <= new Date())) { fieldError(expiresID, 'Choose an expiration in the future, or leave it empty.', prefix + '-error'); return null; }
 return { url, title: $(titleID).value.trim(), expires_at: expiresValue ? new Date(expiresValue).toISOString() : null };
}
$('create-form').addEventListener('submit', async event => {
 event.preventDefault(); if ($('create-submit').disabled) return; clearErrors(event.currentTarget, 'create-error');
 const input = readInput('create'); if (!input) return;
 input.code = $('code').value.trim();
 if (input.code && !/^[A-Za-z0-9_-]{4,64}$/.test(input.code)) return fieldError('code', 'Use 4–64 letters, numbers, underscores or hyphens.', 'create-error');
 busy($('create-submit'), true);
 try { const link = await api('/api/shorten', jsonRequest('POST', input)); createdURL = shortURL(link.short_code); $('created-link').href = createdURL; $('created-link').textContent = createdURL; $('created-result').hidden = false; event.target.reset(); notice('Short link created.'); page = 1; $('search').value = ''; await refresh(); $('created-link').focus(); }
 catch (err) { fieldError(err.status === 409 ? 'code' : 'destination', err.message, 'create-error'); }
 finally { busy($('create-submit'), false); }
});
$('copy-created').addEventListener('click', () => copy(createdURL));
function openEdit(link, trigger) {
 editing = link; editTrigger = trigger; clearErrors($('edit-form'), 'edit-error'); $('edit-code').textContent = link.short_code;
 $('edit-url').value = displayURL(link.long_url); $('edit-title').value = link.title; $('edit-expires').value = localDate(link.expires_at); $('edit-disabled').checked = link.disabled; $('edit-dialog').showModal(); $('edit-url').focus();
}
$('close-edit').addEventListener('click', () => { if (!$('edit-submit').disabled) $('edit-dialog').close(); });
$('edit-dialog').addEventListener('cancel', event => { if ($('edit-submit').disabled) event.preventDefault(); });
$('edit-dialog').addEventListener('close', () => { if (editTrigger?.isConnected) editTrigger.focus(); else $('refresh').focus(); });
$('edit-form').addEventListener('submit', async event => {
 event.preventDefault(); if ($('edit-submit').disabled) return; clearErrors(event.currentTarget, 'edit-error');
 const input = readInput('edit'); if (!input) return;
 input.disabled = $('edit-disabled').checked; input.revision = editing.revision;
 busy($('edit-submit'), true); $('close-edit').disabled = true;
 try { await api('/api/urls/' + encodeURIComponent(editing.short_code), jsonRequest('PATCH', input)); $('edit-dialog').close(); notice(input.disabled ? 'Link disabled. Edit it again to re-enable it.' : 'Link updated.'); await refresh(); }
 catch (err) { $('edit-error').textContent = err.message; }
 finally { busy($('edit-submit'), false); $('close-edit').disabled = false; }
});
$('refresh').addEventListener('click', async () => { busy($('refresh'), true); await refresh(); busy($('refresh'), false); });
function queueSearch(event) { if (event?.isComposing) return; clearTimeout(searchTimer); $('clear-search').hidden = !$('search').value; searchTimer = setTimeout(() => { page = 1; loadLinks(); }, 300); }
$('search').addEventListener('input', queueSearch); $('search').addEventListener('compositionend', queueSearch);
$('search-form').addEventListener('submit', event => { event.preventDefault(); clearTimeout(searchTimer); page = 1; loadLinks(); });
$('clear-search').addEventListener('click', () => { clearTimeout(searchTimer); $('search').value = ''; page = 1; loadLinks(); $('search').focus(); });
$('previous').addEventListener('click', () => { if (page > 1) { page--; loadLinks(); } });
$('next').addEventListener('click', () => { if (page < pages) { page++; loadLinks(); } });
window.addEventListener('offline', () => notice('You are offline. Your form entries are preserved.', true));
window.addEventListener('online', () => { notice('Connection restored.'); if (user) refresh(); });
window.addEventListener('beforeunload', event => { if ($('destination').value || $('edit-dialog').open) { event.preventDefault(); event.returnValue = ''; } });
// Activate controls only after their event handlers are installed.
document.querySelectorAll('button[type="button"]').forEach(button => { button.disabled = false; });
(async () => { try { const session = await api('/api/session'); showUser(session.username); await refresh(); } catch (err) { showUser(null); if (err.status !== 401) notice(err.message, true); else notice(''); } })();
