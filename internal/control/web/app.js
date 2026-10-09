const state = { me: null, overview: {}, nodes: [], users: [], events: [], view: 'dashboard', refreshTimer: null }

const $ = (selector, root = document) => root.querySelector(selector)
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)]

async function api(path, options = {}) {
  const response = await fetch(path, {
    credentials: 'same-origin',
    ...options,
    headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
  })
  const data = await response.json().catch(() => ({}))
  if (!response.ok) {
    const error = new Error(data.error || `HTTP ${response.status}`)
    error.status = response.status
    throw error
  }
  return data
}

async function bootstrap() {
  try {
    const setup = await api('/api/setup/status')
    if (!setup.configured) {
      $('#setup-form [name=publicUrl]').value = window.location.origin
      showSetup()
      return
    }
    state.me = await api('/api/me')
    showApp()
    await refreshAll()
    startRefreshTimer()
  } catch (error) {
    if (error.status === 401) showLogin()
    else showToast(error.message)
  }
}

function showSetup() {
  $('#setup-screen').classList.remove('hidden')
  $('#login-screen').classList.add('hidden')
  $('#app').classList.add('hidden')
}

function showLogin() {
  $('#setup-screen').classList.add('hidden')
  $('#login-screen').classList.remove('hidden')
  $('#app').classList.add('hidden')
}

function showApp() {
  $('#setup-screen').classList.add('hidden')
  $('#login-screen').classList.add('hidden')
  $('#app').classList.remove('hidden')
}

function startRefreshTimer() {
  if (!state.refreshTimer) state.refreshTimer = setInterval(refreshAll, 10000)
}

async function refreshAll() {
  try {
    const [overview, nodes, users, events] = await Promise.all([
      api('/api/overview'), api('/api/nodes'), api('/api/users'), api('/api/events'),
    ])
    Object.assign(state, { overview, nodes, users, events })
    render()
  } catch (error) {
    if (error.status === 401) showLogin()
    else showToast(error.message)
  }
}

function render() {
  const o = state.overview
  $('#nodes-badge').textContent = o.nodes || 0
  $('#healthy-count').textContent = o.healthy || 0
  $('#network-summary').textContent = o.nodes
    ? `${o.online} из ${o.nodes} нод на связи. ${o.healthy} успешно проходят проверку Telegram.`
    : 'Подключите первую ноду, чтобы развернуть приватную proxy-сеть.'
  $('#metric-online').textContent = `${o.online || 0} / ${o.nodes || 0}`
  $('#online-trend').textContent = o.nodes ? `${Math.round((o.online / o.nodes) * 100)}%` : '—'
  $('#metric-connections').textContent = o.activeConnections || 0
  $('#metric-traffic').textContent = formatBytes((o.bytesUp || 0) + (o.bytesDown || 0))
  $('#metric-users').textContent = o.users || 0
  renderDashboardNodes()
  renderEvents($('#dashboard-events'), state.events.slice(0, 5), true)
  renderNodes()
  renderUsers()
  renderEvents($('#events-list'), state.events)
}

function renderDashboardNodes() {
  const root = $('#dashboard-nodes')
  if (!state.nodes.length) return setEmpty(root, 'Ноды пока не подключены')
  root.className = 'node-list'
  root.innerHTML = state.nodes.slice(0, 5).map(node => `
    <div class="node-row">
      <div class="node-name"><span class="status-dot ${node.status === 'online' ? (node.telegramOk ? 'online' : 'bad') : ''}"></span><span><b>${escapeHTML(node.name)}</b><small>${escapeHTML(node.region || 'Регион не указан')}</small></span></div>
      <span class="node-meta">${node.telegramLatencyMs ? `${node.telegramLatencyMs} мс` : '—'}</span>
      <span class="node-health ${node.telegramOk ? 'good' : ''}">${node.telegramOk ? 'Telegram OK' : node.status === 'pending' ? 'Ожидает' : 'Нет доступа'}</span>
    </div>`).join('')
}

function renderNodes() {
  const root = $('#nodes-grid')
  if (!state.nodes.length) {
    root.innerHTML = '<article class="panel empty-state">Нод пока нет. Создайте первую и выполните Docker-команду на VPS.</article>'
    return
  }
  root.innerHTML = state.nodes.map(node => {
    const host = node.advertisedHost || node.publicIp || 'адрес определится после подключения'
    return `<article class="node-card">
      <div class="node-card-head"><div><h3>${escapeHTML(node.name)}</h3><p>${escapeHTML(node.region || 'Регион не указан')}</p></div><span class="status-pill ${node.status}">${statusText(node.status)}</span></div>
      <div class="node-address">HTTP&nbsp; ${escapeHTML(host)}:${node.httpPort}<br>SOCKS ${escapeHTML(host)}:${node.socksPort}</div>
      <div class="node-stats">
        <div class="node-stat"><small>TELEGRAM</small><b>${node.telegramOk ? `${node.telegramLatencyMs} мс` : 'ошибка'}</b></div>
        <div class="node-stat"><small>СОЕДИНЕНИЯ</small><b>${node.activeConnections || 0}</b></div>
        <div class="node-stat"><small>ТРАФИК</small><b>${formatBytes((node.bytesUp || 0) + (node.bytesDown || 0))}</b></div>
      </div>
      <div class="node-card-footer"><span>${node.agentVersion ? `agent ${escapeHTML(node.agentVersion)}` : 'агент не подключён'}</span><span>${node.lastSeenAt ? timeAgo(node.lastSeenAt) : '—'}</span></div>
    </article>`
  }).join('')
}

function renderUsers() {
  const root = $('#users-table')
  if (!state.users.length) {
    root.innerHTML = '<tr><td colspan="6" class="empty-state">Пользователей пока нет</td></tr>'
    return
  }
  root.innerHTML = state.users.map(user => {
    const nodeNames = user.nodeIds.map(id => state.nodes.find(node => node.id === id)?.name).filter(Boolean)
    return `<tr>
      <td class="table-user"><b>${escapeHTML(user.name)}</b><small>${user.id.slice(0, 8)}</small></td>
      <td><code>${escapeHTML(user.username)}</code></td>
      <td>${nodeNames.length ? nodeNames.map(name => `<span class="chip neutral">${escapeHTML(name)}</span>`).join(' ') : '<span class="muted">Не назначены</span>'}</td>
      <td><button class="toggle ${user.enabled ? 'on' : ''}" data-toggle-user="${user.id}" aria-label="Переключить доступ"></button></td>
      <td class="muted">${formatDate(user.createdAt)}</td>
      <td><button class="text-button" data-reset-user="${user.id}">Новый пароль</button></td>
    </tr>`
  }).join('')
}

function renderEvents(root, events, compact = false) {
  if (!events.length) return setEmpty(root, 'Событий пока нет')
  root.className = compact ? 'timeline' : 'event-log'
  root.innerHTML = events.map(event => compact
    ? `<div class="timeline-item"><span class="timeline-marker"></span><div><p>${escapeHTML(event.message)}</p><small>${escapeHTML(event.type)}</small></div><small>${timeAgo(event.createdAt)}</small></div>`
    : `<div class="event-row"><span class="event-type">${escapeHTML(event.type)}</span><p>${escapeHTML(event.message)}</p><time>${formatDateTime(event.createdAt)}</time></div>`
  ).join('')
}

function setView(view) {
  state.view = view
  $$('.view').forEach(element => element.classList.toggle('active', element.id === `view-${view}`))
  $$('.nav-item').forEach(element => element.classList.toggle('active', element.dataset.view === view))
  const names = { dashboard: ['СЕТЬ', 'Обзор'], nodes: ['ИНФРАСТРУКТУРА', 'Ноды'], users: ['ДОСТУП', 'Пользователи'], events: ['АУДИТ', 'События'] }
  $('#section-kicker').textContent = names[view][0]
  $('#section-title').textContent = names[view][1]
}

function openNodeDialog() {
  $('#node-form').reset()
  $('#node-form [name=httpPort]').value = 3128
  $('#node-form [name=socksPort]').value = 1080
  $('#node-dialog').showModal()
}

function openUserDialog() {
  if (!state.nodes.length) return showToast('Сначала добавьте хотя бы одну ноду')
  $('#user-form').reset()
  $('#user-node-options').innerHTML = state.nodes.map(node => `<label class="check-option"><input type="checkbox" name="nodeIds" value="${node.id}" checked><span>${escapeHTML(node.name)}</span></label>`).join('')
  $('#user-dialog').showModal()
}

function showSecret(title, copy, value) {
  $('#secret-title').textContent = title
  $('#secret-copy').textContent = copy
  $('#secret-value').textContent = value
  $('#secret-dialog').showModal()
}

$('#setup-form').addEventListener('submit', async event => {
  event.preventDefault()
  const form = new FormData(event.currentTarget)
  const errorNode = $('#setup-error')
  errorNode.textContent = ''
  if (form.get('password') !== form.get('passwordConfirm')) {
    errorNode.textContent = 'Пароли не совпадают'
    return
  }
  const payload = { username: form.get('username'), password: form.get('password'), publicUrl: form.get('publicUrl') }
  try {
    state.me = await api('/api/setup', { method: 'POST', body: JSON.stringify(payload) })
    showApp()
    await refreshAll()
    startRefreshTimer()
  } catch (error) { errorNode.textContent = error.message }
})

$('#login-form').addEventListener('submit', async event => {
  event.preventDefault()
  const form = new FormData(event.currentTarget)
  $('#login-error').textContent = ''
  try {
    await api('/api/login', { method: 'POST', body: JSON.stringify(Object.fromEntries(form)) })
    showApp()
    await refreshAll()
    startRefreshTimer()
  } catch (error) { $('#login-error').textContent = error.message }
})

$('#logout').addEventListener('click', async () => { await api('/api/logout', { method: 'POST' }); showLogin() })
$$('.nav-item').forEach(button => button.addEventListener('click', () => setView(button.dataset.view)))
$$('[data-view-link]').forEach(button => button.addEventListener('click', () => setView(button.dataset.viewLink)))
$$('[data-action=new-node]').forEach(button => button.addEventListener('click', openNodeDialog))
$$('[data-action=new-user]').forEach(button => button.addEventListener('click', openUserDialog))
$$('[data-close-dialog]').forEach(button => button.addEventListener('click', () => button.closest('dialog').close()))

$('#node-form').addEventListener('submit', async event => {
  event.preventDefault()
  const form = new FormData(event.currentTarget)
  const payload = Object.fromEntries(form)
  payload.httpPort = Number(payload.httpPort)
  payload.socksPort = Number(payload.socksPort)
  const errorNode = $('[data-form-error]', event.currentTarget)
  try {
    const result = await api('/api/nodes', { method: 'POST', body: JSON.stringify(payload) })
    $('#node-dialog').close()
    showSecret('Нода создана', 'Выполните эту команду на новом VPS с установленным Docker:', result.installCommand)
    await refreshAll()
  } catch (error) { errorNode.textContent = error.message }
})

$('#user-form').addEventListener('submit', async event => {
  event.preventDefault()
  const form = new FormData(event.currentTarget)
  const payload = { name: form.get('name'), username: form.get('username'), password: form.get('password'), nodeIds: form.getAll('nodeIds') }
  const errorNode = $('[data-form-error]', event.currentTarget)
  try {
    const result = await api('/api/users', { method: 'POST', body: JSON.stringify(payload) })
    $('#user-dialog').close()
    showSecret('Доступ создан', `Логин: ${result.user.username}. Пароль показывается только один раз:`, result.password)
    await refreshAll()
  } catch (error) { errorNode.textContent = error.message }
})

$('#users-table').addEventListener('click', async event => {
  const toggle = event.target.closest('[data-toggle-user]')
  const reset = event.target.closest('[data-reset-user]')
  try {
    if (toggle) {
      const user = state.users.find(item => item.id === toggle.dataset.toggleUser)
      await api(`/api/users/${user.id}`, { method: 'PATCH', body: JSON.stringify({ enabled: !user.enabled }) })
      await refreshAll()
    }
    if (reset) {
      const result = await api(`/api/users/${reset.dataset.resetUser}`, { method: 'PATCH', body: JSON.stringify({ password: '' }) })
      showSecret('Пароль обновлён', `Новый пароль для ${result.user.username}:`, result.password)
      await refreshAll()
    }
  } catch (error) { showToast(error.message) }
})

$('#copy-secret').addEventListener('click', async () => {
  await navigator.clipboard.writeText($('#secret-value').textContent)
  showToast('Скопировано')
})

function showToast(message) {
  const toast = $('#toast')
  toast.textContent = message
  toast.classList.add('show')
  clearTimeout(showToast.timer)
  showToast.timer = setTimeout(() => toast.classList.remove('show'), 2500)
}

function setEmpty(root, text) { root.className = 'empty-state'; root.textContent = text }
function statusText(status) { return ({ online: 'Онлайн', offline: 'Офлайн', pending: 'Ожидает' })[status] || status }
function formatBytes(value) { if (!value) return '0 Б'; const units = ['Б','КБ','МБ','ГБ','ТБ']; const i = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1); return `${(value / 1024 ** i).toFixed(i ? 1 : 0)} ${units[i]}` }
function formatDate(value) { return new Intl.DateTimeFormat('ru-RU', { day: '2-digit', month: 'short', year: 'numeric' }).format(new Date(value)) }
function formatDateTime(value) { return new Intl.DateTimeFormat('ru-RU', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' }).format(new Date(value)) }
function timeAgo(value) { const sec = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 1000)); if (sec < 15) return 'только что'; if (sec < 60) return `${sec} сек назад`; if (sec < 3600) return `${Math.floor(sec / 60)} мин назад`; return formatDateTime(value) }
function escapeHTML(value = '') { return String(value).replace(/[&<>'"]/g, char => ({ '&':'&amp;', '<':'&lt;', '>':'&gt;', "'":'&#39;', '"':'&quot;' })[char]) }

bootstrap()
