const state = { me: null, settings: { systemName: 'ProxyFleet', logoDataUrl: '', telegram: {} }, pendingLogoDataURL: null, platform: { version: '0.5.1' }, overview: {}, analytics: {}, activity: { samples: [] }, nodes: [], proxies: [], events: [], pwa: { supported: false, publicKey: '', subscription: null }, pwaSubscription: null, installPrompt: null, refreshTimer: null, platformTimer: null, view: 'dashboard', editingNodeId: null, selectedProxyId: null, recap: { background: 'gradient', accent: '#64d2ff' } }
const routes = { dashboard: '/', nodes: '/nodes', proxies: '/proxies', events: '/events', settings: '/settings' }
const pageTitles = { dashboard: 'Обзор', nodes: 'Ноды', proxies: 'Прокси', events: 'Журнал', settings: 'Настройки' }

const $ = (selector, root = document) => root.querySelector(selector)
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)]
const tr = (source, variables) => window.I18n?.t(source, variables) || source

async function api(path, options = {}) {
  let response
  try {
    response = await fetch(path, {
      credentials: 'same-origin',
      ...options,
      headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
    })
  } catch (cause) {
    const error = new Error(tr('Нет связи с сервером ProxyFleet. Проверьте, что панель запущена, и повторите попытку.'))
    error.code = 'NETWORK_ERROR'
    error.cause = cause
    throw error
  }
  const data = await response.json().catch(() => ({}))
  if (!response.ok) {
    const error = new Error(tr(data.error || httpErrorMessage(response.status)))
    error.status = response.status
    throw error
  }
  return data
}

function httpErrorMessage(status) {
  if (status === 400) return 'Сервер не смог обработать запрос. Проверьте введённые данные.'
  if (status === 401) return 'Сессия завершилась. Войдите в панель ещё раз.'
  if (status === 403) return 'Недостаточно прав для этого действия.'
  if (status === 404) return 'Запрошенные данные не найдены.'
  if (status === 409) return 'Данные уже изменились. Обновите страницу и попробуйте снова.'
  if (status >= 500) return 'Сервер временно не может выполнить запрос. Попробуйте ещё раз чуть позже.'
  return 'Не удалось выполнить запрос. Попробуйте ещё раз.'
}

async function bootstrap() {
  window.I18n?.apply()
  await registerPWA()
  try {
    const setup = await api('/api/setup/status')
    const branding = await api('/api/branding')
    Object.assign(state.settings, branding)
    applyBranding()
    if (!setup.configured) {
      $('#setup-form [name=publicUrl]').value = window.location.origin
      showSetup()
      return
    }
    state.me = await api('/api/me')
    showApp()
    await Promise.all([loadSettings(), loadPlatform(), refreshAll()])
    startRefreshTimer()
  } catch (error) {
    if (error.status === 401) showLogin()
    else showToast(error.message)
  }
}

async function loadPlatform() {
  state.platform = await api('/api/platform')
  $('#app-version b').textContent = `v${state.platform.version}`
  $('#recap-version').textContent = `v${state.platform.version}`
  const stars = state.platform.githubStars
  $('#github-stars').textContent = Number.isFinite(stars) ? new Intl.NumberFormat(window.I18n?.localeTag() || 'ru-RU').format(stars) : '0'
  $('.utility-github').href = state.platform.githubUrl || 'https://github.com/neluckoff/proxyfleet'
  renderUpdateNotice()
}

function renderUpdateNotice() {
  const notice = $('#update-notice')
  const release = state.platform.latestRelease
  const visible = Boolean(state.platform.updateAvailable && release?.url && release?.version)
  notice.classList.toggle('hidden', !visible)
  if (!visible) return
  notice.href = release.url
  notice.title = `${tr('Доступно обновление')} v${release.version}`
  notice.setAttribute('aria-label', `${tr('Открыть новый релиз ProxyFleet')} v${release.version}`)
  $('.utility-update-copy', notice).textContent = tr('Доступно обновление')
  $('#update-version').textContent = `v${release.version}`
}

async function loadSettings() {
  state.settings = await api('/api/settings')
  await loadPWASettings()
  state.pendingLogoDataURL = null
  applyBranding()
  renderSettings()
}

async function registerPWA() {
  if (!('serviceWorker' in navigator)) return
  try {
    await navigator.serviceWorker.register('/service-worker.js')
    state.pwa.supported = true
  } catch (error) {
    state.pwa.supported = false
  }
}

async function loadPWASettings() {
  if (!state.pwa.supported || !('PushManager' in window) || !('Notification' in window)) {
    state.pwa = { ...state.pwa, supported: false }
    return
  }
  try {
    const registration = await navigator.serviceWorker.ready
    state.pwaSubscription = await registration.pushManager.getSubscription()
    const endpoint = state.pwaSubscription?.endpoint
    const suffix = endpoint ? `?endpoint=${encodeURIComponent(endpoint)}` : ''
    state.pwa = { supported: true, ...(await api(`/api/pwa${suffix}`)) }
  } catch (error) {
    state.pwa = { ...state.pwa, supported: false, error: error.message }
  }
}

function applyBranding() {
  const name = state.settings?.systemName || 'ProxyFleet'
  const logo = state.settings?.logoDataUrl || ''
  const initials = brandInitials(name)
  $$('[data-system-name]').forEach(element => { element.textContent = name })
  $$('[data-brand-mark]').forEach(mark => {
    const text = $('[data-brand-initials]', mark)
    const defaultImage = $('[data-brand-default-logo]', mark)
    const image = $('[data-brand-logo]', mark)
    text.textContent = initials
    text.classList.add('hidden')
    defaultImage.classList.toggle('hidden', Boolean(logo))
    image.classList.toggle('hidden', !logo)
    if (logo) image.src = logo
    else image.removeAttribute('src')
  })
  $('#app-favicon').href = logo || '/favicon.png'
  if (state.view) document.title = `${name} — ${tr(pageTitles[state.view] || 'Панель')}`
}

function renderSettings() {
  const settings = state.settings || {}
  const telegram = settings.telegram || {}
  const brandingForm = $('#branding-form')
  brandingForm.elements.namedItem('systemName').value = settings.systemName || 'ProxyFleet'
  renderBrandingPreview(state.pendingLogoDataURL ?? settings.logoDataUrl ?? '')
  $('#telegram-enabled').checked = Boolean(telegram.enabled)
  const telegramForm = $('#telegram-form')
  telegramForm.elements.namedItem('botToken').value = ''
  telegramForm.elements.namedItem('chatId').value = telegram.chatId || ''
  telegramForm.elements.namedItem('proxyUrl').value = telegram.proxyUrl || ''
  const status = $('#telegram-status')
  status.className = `settings-status ${telegram.enabled ? 'connected' : telegram.configured ? 'disabled' : ''}`
  status.textContent = telegram.enabled ? 'Подключено' : telegram.configured ? 'Выключено' : 'Не настроено'
  $('#telegram-bot-name').textContent = telegram.botUsername ? `@${telegram.botUsername}` : telegram.configured ? 'Токен сохранён' : 'Бот ещё не подключён'
  $('#telegram-connection-copy').textContent = telegram.configured ? 'Настройки можно проверить тестовым сообщением.' : 'Сохраните настройки и отправьте тестовое сообщение.'
  $('#telegram-disconnect').classList.toggle('hidden', !telegram.configured)
  renderPWASettings()
  window.I18n?.apply($('#view-settings'))
}

function renderPWASettings() {
  const supported = Boolean(state.pwa.supported)
  const subscription = state.pwaSubscription
  const connected = Boolean(subscription && state.pwa.subscription)
  const preferences = state.pwa.subscription?.preferences || { nodes: true, proxies: true, system: true }
  const enabled = $('#pwa-notifications-enabled')
  enabled.checked = connected
  enabled.disabled = !supported
  $('#pwa-preferences').disabled = !connected
  $('#pwa-save').disabled = !connected
  $('#pwa-test').disabled = !connected
  ;['nodes', 'proxies', 'system'].forEach(category => {
    $('#pwa-form').elements.namedItem(category).checked = preferences[category] !== false
  })
  const status = $('#pwa-status')
  status.className = `settings-status ${connected ? 'connected' : supported ? 'disabled' : ''}`
  status.textContent = connected ? 'Подключено' : supported ? 'Выключено' : 'Не поддерживается'
  const installed = isPWAInstalled()
  $('#pwa-install-title').textContent = installed ? 'Приложение установлено' : 'Веб-приложение'
  $('#pwa-install-copy').textContent = installed
    ? 'ProxyFleet уже открыт или установлен как отдельное приложение.'
    : state.installPrompt ? 'Браузер готов установить ProxyFleet отдельным приложением.' : 'Установите панель через меню браузера, когда станет доступна установка.'
  $('#pwa-install').classList.toggle('hidden', installed)
  $('#pwa-install').disabled = !state.installPrompt
}

function isPWAInstalled() {
  return window.matchMedia('(display-mode: standalone)').matches || window.navigator.standalone === true
}

function renderBrandingPreview(logo) {
  const name = $('#branding-form [name=systemName]').value || state.settings.systemName || 'ProxyFleet'
  const initials = brandInitials(name)
  const text = $('[data-preview-initials]')
  const defaultImage = $('[data-preview-default-logo]')
  const image = $('[data-preview-logo]')
  text.textContent = initials
  text.classList.add('hidden')
  defaultImage.classList.toggle('hidden', Boolean(logo))
  image.classList.toggle('hidden', !logo)
  if (logo) image.src = logo
  else image.removeAttribute('src')
}

function brandInitials(name) {
  const capitals = String(name).match(/[A-ZА-ЯЁ]/g) || []
  if (capitals.length >= 2) return capitals.slice(0, 2).join('')
  const words = String(name).trim().split(/\s+/).filter(Boolean)
  if (words.length >= 2) return words.slice(0, 2).map(word => word[0]).join('').toUpperCase()
  return String(name).replace(/\s/g, '').slice(0, 2).toUpperCase() || 'PF'
}

function showSetup() {
  stopRefreshTimer()
  $('#setup-screen').classList.remove('hidden')
  $('#login-screen').classList.add('hidden')
  $('#app').classList.add('hidden')
  document.title = `${state.settings?.systemName || 'ProxyFleet'} — ${tr('Настройка')}`
}

function showLogin() {
  stopRefreshTimer()
  $('#setup-screen').classList.add('hidden')
  $('#login-screen').classList.remove('hidden')
  $('#app').classList.add('hidden')
  document.title = `${state.settings?.systemName || 'ProxyFleet'} — ${tr('Вход')}`
}

function showApp() {
  $('#setup-screen').classList.add('hidden')
  $('#login-screen').classList.add('hidden')
  $('#app').classList.remove('hidden')
  syncLanguageMenu()
  syncRoute()
}

function startRefreshTimer() {
  if (!state.refreshTimer) state.refreshTimer = setInterval(refreshAll, 10000)
  if (!state.platformTimer) state.platformTimer = setInterval(() => loadPlatform().catch(() => {}), 15 * 60 * 1000)
}

function stopRefreshTimer() {
  clearInterval(state.refreshTimer)
  clearInterval(state.platformTimer)
  state.refreshTimer = null
  state.platformTimer = null
}

async function refreshAll() {
  try {
    const [overview, analytics, activity, nodes, proxies, events] = await Promise.all([
      api('/api/overview'), api('/api/analytics'), api('/api/activity'), api('/api/nodes'), api('/api/proxies'), api('/api/events'),
    ])
    Object.assign(state, { overview, analytics, activity, nodes, proxies, events })
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
    ? tr('{online} из {nodes} нод на связи. {healthy} успешно проходят проверку Telegram.', { online: o.online, nodes: o.nodes, healthy: o.healthy })
    : tr('Подключите первую ноду, чтобы развернуть приватную proxy-сеть.')
  $('#metric-online').textContent = `${o.online || 0} / ${o.nodes || 0}`
  $('#online-trend').textContent = o.nodes ? `${Math.round((o.online / o.nodes) * 100)}%` : '—'
  $('#metric-connections').textContent = o.activeConnections || 0
  $('#metric-traffic').textContent = formatBytes((o.bytesUp || 0) + (o.bytesDown || 0))
  $('#metric-users').textContent = o.proxies || o.users || 0
  renderActivityCharts()
  renderAnalytics()
  renderNodes()
  renderProxies()
  renderEvents($('#events-list'), state.events)
  renderRecap()
  window.I18n?.apply()
}

function renderAnalytics() {
  const traffic = state.analytics?.traffic || {}
  const availability = state.analytics?.availability || {}
  const users = state.analytics?.proxies || state.analytics?.users || {}
  const system = state.analytics?.system || {}
  const lifetime = traffic.lifetime || {}

  $('#analytics-traffic-total').textContent = formatBytes(lifetime.bytesTotal || 0)
  $('#analytics-traffic-today').textContent = formatBytes(traffic.today?.bytesTotal || 0)
  $('#analytics-traffic-week').textContent = formatBytes(traffic.sevenDays?.bytesTotal || 0)
  $('#analytics-traffic-month').textContent = formatBytes(traffic.thirtyDays?.bytesTotal || 0)
  $('#analytics-traffic-calendar').textContent = formatBytes(traffic.month?.bytesTotal || 0)
  $('#analytics-traffic-year').textContent = formatBytes(traffic.year?.bytesTotal || 0)
  $('#analytics-traffic-up').textContent = formatBytes(lifetime.bytesUp || 0)
  $('#analytics-traffic-down').textContent = formatBytes(lifetime.bytesDown || 0)
  renderTrafficTrend($('#analytics-traffic-today-trend'), traffic.today, tr('со вчера'))
  renderTrafficTrend($('#analytics-traffic-week-trend'), traffic.sevenDays, tr('к прошлой неделе'))

  $('#analytics-nodes-online').textContent = availability.online || 0
  $('#analytics-nodes-offline').textContent = availability.offline || 0
  $('#analytics-nodes-pending').textContent = availability.pending || 0
  $('#analytics-telegram-healthy').textContent = `${availability.telegramHealthy || 0} / ${availability.online || 0}`
  $('#analytics-telegram-latency').textContent = availability.averageTelegramLatencyMs
    ? tr('средняя задержка {latency} мс', { latency: availability.averageTelegramLatencyMs })
    : tr('задержка —')

  $('#analytics-users-total').textContent = users.total || 0
  $('#analytics-users-enabled').textContent = users.enabled || 0
  $('#analytics-users-disabled').textContent = users.disabled || 0
  $('#analytics-users-unassigned').textContent = users.external || 0

  $('#analytics-telemetry-nodes').textContent = formatAgents(system.telemetryNodes || 0)
  $('#analytics-heap').textContent = formatBytes(system.heapBytes || 0)
  $('#analytics-runtime-memory').textContent = formatBytes(system.runtimeMemoryBytes || 0)
  $('#analytics-host-memory').textContent = formatBytes(system.hostMemoryUsedBytes || 0)
  $('#analytics-host-memory-total').textContent = tr('из {value}', { value: formatBytes(system.hostMemoryTotalBytes || 0) })
  $('#analytics-goroutines').textContent = system.goroutines || 0
  $('#analytics-load').textContent = Number(system.averageLoad || 0).toFixed(2)
  $('#analytics-uptime').textContent = formatDuration(system.averageUptimeSeconds || 0)
}

function renderTrafficTrend(root, period, label) {
  const difference = Number(period?.differenceBytes || 0)
  const previous = Number(period?.previousTotal || 0)
  root.className = difference > 0 ? 'trend-up' : difference < 0 ? 'trend-down' : ''
  if (!previous && !difference) {
    root.textContent = tr('собираем данные')
    return
  }
  root.textContent = `${difference >= 0 ? '↗' : '↘'} ${difference >= 0 ? '+' : '−'}${formatBytes(Math.abs(difference))} ${label}`
}

function renderRecap() {
  const analytics = state.analytics || {}
  const traffic = analytics.traffic || {}
  const system = analytics.system || {}
  const availability = analytics.availability || {}
  const name = state.settings?.systemName || 'ProxyFleet'
  $('[data-recap-name]').textContent = name
  $('[data-recap-mark]').textContent = brandInitials(name)
  $('[data-recap-logo]').src = state.settings?.logoDataUrl || '/logo.png'
  $('#recap-users').textContent = analytics.proxies?.total || analytics.users?.total || 0
  $('#recap-nodes').textContent = availability.total || 0
  $('#recap-traffic').textContent = formatBytes(traffic.lifetime?.bytesTotal || 0)
  $('#recap-online').textContent = availability.online || 0
  $('#recap-month-traffic').textContent = formatBytes(traffic.month?.bytesTotal || 0)
  $('#recap-agents').textContent = system.telemetryNodes || 0
  $('#recap-connections').textContent = availability.activeConnections || 0
  $('#recap-memory').textContent = formatBytes(system.hostMemoryUsedBytes || 0)
  $('#recap-month').textContent = new Intl.DateTimeFormat(window.I18n?.localeTag() || 'ru-RU', { month: 'long', year: 'numeric' }).format(new Date()).toUpperCase()
  $('#recap-note').textContent = $('#recap-note-input').value.trim() || tr('Private infrastructure')
  $('#recap-version').textContent = `v${state.platform.version || state.me?.version || '0.5.1'}`
  const card = $('#recap-card')
  card.className = `recap-card background-${state.recap.background}`
  card.style.setProperty('--recap-accent', state.recap.accent)
  $$('[data-recap-toggle]').forEach(input => {
    $(`[data-recap-section="${input.dataset.recapToggle}"]`, card)?.classList.toggle('hidden', !input.checked)
  })
}

function drawRecapCanvas() {
  const canvas = document.createElement('canvas')
  canvas.width = 960
  canvas.height = 1200
  const ctx = canvas.getContext('2d')
  const accent = state.recap.accent
  const analytics = state.analytics || {}
  const traffic = analytics.traffic || {}
  const availability = analytics.availability || {}
  const system = analytics.system || {}
  const sections = Object.fromEntries($$('[data-recap-toggle]').map(input => [input.dataset.recapToggle, input.checked]))

  ctx.fillStyle = '#05070c'
  ctx.fillRect(0, 0, canvas.width, canvas.height)
  if (state.recap.background === 'gradient') {
    const gradient = ctx.createRadialGradient(900, 0, 0, 900, 0, 760)
    gradient.addColorStop(0, `${accent}38`)
    gradient.addColorStop(1, 'transparent')
    ctx.fillStyle = gradient
    ctx.fillRect(0, 0, canvas.width, canvas.height)
  }
  if (state.recap.background === 'grid') {
    ctx.strokeStyle = `${accent}20`
    ctx.lineWidth = 1
    for (let x = 0; x < canvas.width; x += 48) { ctx.beginPath(); ctx.moveTo(x, 0); ctx.lineTo(x, canvas.height); ctx.stroke() }
    for (let y = 0; y < canvas.height; y += 48) { ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(canvas.width, y); ctx.stroke() }
  }
  ctx.strokeStyle = `${accent}80`
  ctx.lineWidth = 6
  ctx.strokeRect(3, 3, canvas.width - 6, canvas.height - 6)

  const name = state.settings?.systemName || 'ProxyFleet'
  ctx.textAlign = 'left'
  ctx.fillStyle = accent
  ctx.font = '700 24px system-ui, sans-serif'
  ctx.fillText(brandInitials(name), 76, 86)
  ctx.fillStyle = '#f5f5f7'
  ctx.font = '700 31px system-ui, sans-serif'
  ctx.fillText(name.toUpperCase(), 132, 86)

  ctx.textAlign = 'center'
  ctx.fillStyle = accent
  ctx.font = '700 176px system-ui, sans-serif'
  ctx.fillText(String(analytics.proxies?.total || analytics.users?.total || 0), 480, 320)
  drawCanvasLabel(ctx, tr('ПРОКСИ'), 480, 365)

  let y = 480
  if (sections.network) {
    drawCanvasMetric(ctx, String(availability.total || 0), tr('НОД'), 310, y)
    drawCanvasMetric(ctx, formatBytes(traffic.lifetime?.bytesTotal || 0), tr('ТРАФИКА'), 650, y)
    y += 130
    drawCanvasDivider(ctx, accent, y)
    y += 72
  }
  if (sections.period) {
    ctx.textAlign = 'left'
    drawCanvasLabel(ctx, new Intl.DateTimeFormat(window.I18n?.localeTag() || 'ru-RU', { month: 'long', year: 'numeric' }).format(new Date()).toUpperCase(), 92, y)
    y += 88
    drawCanvasMetric(ctx, String(availability.online || 0), tr('НОД ОНЛАЙН'), 310, y)
    drawCanvasMetric(ctx, formatBytes(traffic.month?.bytesTotal || 0), tr('ЗА МЕСЯЦ'), 650, y)
    y += 130
  }
  if (sections.system) {
    ctx.textAlign = 'left'
    drawCanvasLabel(ctx, tr('ИНФРАСТРУКТУРА'), 92, y)
    y += 88
    drawCanvasMetric(ctx, String(system.telemetryNodes || 0), tr('АГЕНТОВ'), 220, y, 42)
    drawCanvasMetric(ctx, String(availability.activeConnections || 0), tr('СОЕДИНЕНИЙ'), 480, y, 42)
    drawCanvasMetric(ctx, formatBytes(system.hostMemoryUsedBytes || 0), 'RAM', 740, y, 42)
  }

  drawCanvasDivider(ctx, accent, 1070)
  ctx.textAlign = 'left'
  ctx.fillStyle = accent
  ctx.font = '600 25px system-ui, sans-serif'
  ctx.fillText(($('#recap-note-input').value.trim() || tr('Private infrastructure')).slice(0, 42), 76, 1137)
  ctx.textAlign = 'right'
  ctx.fillText(`v${state.platform.version || state.me?.version || '0.5.1'}`, 884, 1137)
  return canvas
}

function drawCanvasMetric(ctx, value, label, x, y, size = 54) {
  ctx.textAlign = 'center'
  ctx.fillStyle = '#f5f5f7'
  ctx.font = `650 ${size}px system-ui, sans-serif`
  ctx.fillText(value, x, y)
  drawCanvasLabel(ctx, label, x, y + 38)
}

function drawCanvasLabel(ctx, label, x, y) {
  ctx.textAlign = x < 150 ? 'left' : 'center'
  ctx.fillStyle = '#6e6e73'
  ctx.font = '700 18px system-ui, sans-serif'
  ctx.fillText(label, x, y)
}

function drawCanvasDivider(ctx, accent, y) {
  ctx.strokeStyle = `${accent}55`
  ctx.lineWidth = 2
  ctx.beginPath()
  ctx.moveTo(76, y)
  ctx.lineTo(884, y)
  ctx.stroke()
}

function recapBlob() {
  return new Promise((resolve, reject) => drawRecapCanvas().toBlob(blob => blob ? resolve(blob) : reject(new Error('PNG export failed')), 'image/png'))
}

async function downloadRecap() {
  const blob = await recapBlob()
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `proxyfleet-recap-${new Date().toISOString().slice(0, 10)}.png`
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

function renderActivityCharts() {
  const samples = state.activity?.samples || []
  $('#activity-users-now').textContent = state.overview.activeConnections || 0
  $('#activity-nodes-now').textContent = state.overview.online || 0
  renderActivityChart($('#activity-users-chart'), samples, 'activeConnections', 'users', tr('активных подключений'))
  renderActivityChart($('#activity-nodes-chart'), samples, 'onlineNodes', 'nodes', tr('нод онлайн'))
}

function renderActivityChart(root, samples, valueKey, tone, unit) {
  const now = Date.now()
  const start = now - 24 * 60 * 60 * 1000
  const width = 640
  const height = 180
  const top = 14
  const bottom = 12
  const usableHeight = height - top - bottom
  const points = samples
    .map(sample => ({ time: new Date(sample.recordedAt).getTime(), value: Number(sample[valueKey]) || 0 }))
    .filter(point => Number.isFinite(point.time) && point.time >= start)
  const maxValue = Math.max(1, ...points.map(point => point.value))
  const coordinates = points.map(point => ({
    x: Math.max(0, Math.min(width, ((point.time - start) / (now - start)) * width)),
    y: top + usableHeight - (point.value / maxValue) * usableHeight,
    ...point,
  }))
  const path = coordinates.map((point, index) => `${index ? 'L' : 'M'} ${point.x.toFixed(1)} ${point.y.toFixed(1)}`).join(' ')
  const area = coordinates.length
    ? `${path} L ${coordinates.at(-1).x.toFixed(1)} ${height} L ${coordinates[0].x.toFixed(1)} ${height} Z`
    : ''
  const last = coordinates.at(-1)
  const grid = [0, 1, 2, 3].map(index => {
    const y = top + (usableHeight / 3) * index
    return `<line class="chart-grid-line" x1="0" y1="${y}" x2="${width}" y2="${y}"></line>`
  }).join('')

  root.innerHTML = `<svg viewBox="0 0 ${width} ${height}" role="img" aria-label="${escapeHTML(unit)} ${tr('за последние 24 часа')}">
    ${grid}
    ${area ? `<path class="chart-area ${tone}" d="${area}"></path><path class="chart-line ${tone}" d="${path}"></path>` : ''}
    ${last ? `<circle class="chart-point ${tone}" cx="${last.x.toFixed(1)}" cy="${last.y.toFixed(1)}" r="5"><title>${last.value} ${escapeHTML(unit)}</title></circle>` : ''}
    ${coordinates.length < 2 ? '<text class="chart-empty-label" x="320" y="88" text-anchor="middle">Собираем данные</text>' : ''}
  </svg>`
}

function renderNodes() {
  const root = $('#nodes-grid')
  if (!state.nodes.length) {
    root.innerHTML = '<article class="panel empty-state">Нод пока нет. Создайте первую и выполните Docker-команду на VPS.</article>'
    return
  }
  root.innerHTML = state.nodes.map(node => {
    const host = node.advertisedHost || node.publicIp || 'адрес определится после подключения'
    const agentVersion = String(node.agentVersion || '').replace(/^v/i, '')
    const agentBadge = agentVersion
      ? `<span class="node-agent-version" title="Версия агента"><span class="node-agent-signal" aria-hidden="true"><i></i><i></i><i></i></span><span><small>АГЕНТ</small><b>v${escapeHTML(agentVersion)}</b></span></span>`
      : `<span class="node-agent-version disconnected"><span class="node-agent-signal" aria-hidden="true"><i></i><i></i><i></i></span><span><small>АГЕНТ</small><b>Не подключён</b></span></span>`
    return `<article class="node-card">
      <div class="node-card-head"><div><h3>${escapeHTML(node.name)}</h3><p>${escapeHTML(node.region || 'Регион не указан')}</p></div><span class="status-pill ${node.status}">${statusText(node.status)}</span></div>
      <div class="node-address">HTTP&nbsp;&nbsp;&nbsp; ${escapeHTML(host)}:${node.httpPort}<br>SOCKS5&nbsp; ${escapeHTML(host)}:${node.socksPort}<br>WEB&nbsp;&nbsp;&nbsp;&nbsp; ${escapeHTML(node.advertisedHost || 'нужен домен')}:443 → 127.0.0.1:${node.webProxyPort || 8443}</div>
      <div class="node-stats">
        <div class="node-stat"><small>TELEGRAM</small><b class="${node.telegramOk ? 'good' : 'bad'}">${node.telegramOk ? `${node.telegramLatencyMs} мс` : 'ошибка'}</b></div>
        <div class="node-stat"><small>СОЕДИНЕНИЯ</small><b>${node.activeConnections || 0}</b></div>
        <div class="node-stat"><small>ТРАФИК</small><b>${formatBytes((node.bytesUp || 0) + (node.bytesDown || 0))}</b></div>
      </div>
      <div class="node-card-footer">${agentBadge}<div class="node-card-actions"><span>${node.lastSeenAt && !node.lastSeenAt.startsWith('0001-') ? timeAgo(node.lastSeenAt) : '—'}</span><button class="text-button" type="button" data-edit-node="${node.id}">Изменить</button></div></div>
    </article>`
  }).join('')
}

function renderProxies() {
  const root = $('#proxies-table')
  if (!state.proxies.length) {
    root.innerHTML = '<tr><td colspan="8" class="empty-state">Прокси пока нет. Создайте первый доступ к одной из нод.</td></tr>'
    return
  }
  root.innerHTML = state.proxies.map(proxy => `<tr class="proxy-row" data-open-proxy="${proxy.id}" tabindex="0">
    <td class="table-user"><b>${escapeHTML(proxy.name)}</b><small>${escapeHTML(proxy.id.slice(0, 8))}</small></td>
    <td><span class="protocol-chip ${proxy.protocol}">${proxyProtocolLabel(proxy.protocol)}</span></td>
    <td class="proxy-endpoint">${proxy.endpoint ? `<code>${escapeHTML(proxy.endpoint)}</code>` : '<span class="muted">Адрес не задан</span>'}</td>
    <td><code>${escapeHTML(proxy.username)}</code></td>
    <td class="proxy-node-cell"><b>${escapeHTML(proxy.source === 'external' ? proxy.provider || 'Внешний прокси' : proxy.nodeName || 'Нода не назначена')}</b><small>${escapeHTML(proxy.source === 'external' ? 'внешний источник' : proxy.nodeRegion || '')}</small></td>
    <td><span class="status-pill ${proxy.status}">${proxyStatusText(proxy.status)}</span></td>
    <td class="muted proxy-created-cell">${formatCompactDate(proxy.createdAt)}</td>
    <td><button class="text-button" type="button" data-manage-proxy="${proxy.id}">Управлять</button></td>
  </tr>`).join('')
}

function renderEvents(root, events, compact = false) {
  if (!events.length) return setEmpty(root, 'Событий пока нет')
  root.className = compact ? 'timeline' : 'event-log'
  root.innerHTML = events.map(event => compact
    ? `<div class="timeline-item"><span class="timeline-marker"></span><div><p>${escapeHTML(tr(event.message))}</p><small>${escapeHTML(event.type)}</small></div><small>${timeAgo(event.createdAt)}</small></div>`
    : `<div class="event-row"><span class="event-type">${escapeHTML(event.type)}</span><p>${escapeHTML(tr(event.message))}</p><time>${formatDateTime(event.createdAt)}</time></div>`
  ).join('')
}

function setView(view, historyMode = 'push') {
  if (!routes[view]) view = 'dashboard'
  state.view = view
  $$('.view').forEach(element => element.classList.toggle('active', element.id === `view-${view}`))
  $$('.nav-item').forEach(element => {
    const active = element.dataset.view === view
    element.classList.toggle('active', active)
    if (active) element.setAttribute('aria-current', 'page')
    else element.removeAttribute('aria-current')
  })
  const path = routes[view]
  if (historyMode === 'push' && window.location.pathname !== path) window.history.pushState({ view }, '', path)
  if (historyMode === 'replace') window.history.replaceState({ view }, '', path)
  document.title = `${state.settings?.systemName || 'ProxyFleet'} — ${tr(pageTitles[view])}`
  window.scrollTo(0, 0)
}

function syncRoute() {
  const path = window.location.pathname.replace(/\/+$/, '') || '/'
  if (path === '/users') {
    setView('proxies', 'replace')
    return
  }
  const view = Object.keys(routes).find(key => routes[key] === path)
  setView(view || 'dashboard', view ? 'none' : 'replace')
}

function openNodeDialog() {
  state.editingNodeId = null
  $('#node-form').reset()
  $('[data-form-error]', $('#node-form')).textContent = ''
  $('#node-form [name=httpPort]').value = 3128
  $('#node-form [name=socksPort]').value = 1080
  $('#node-form [name=webProxyPort]').value = 8443
  $('#node-dialog-eyebrow').textContent = 'НОВАЯ НОДА'
  $('#node-dialog-title').textContent = 'Подключить сервер'
  $('#node-dialog-copy').textContent = 'После создания появится одноразовая Docker-команда. Выполните её на новом VPS.'
  $('#node-submit-button').textContent = 'Создать ноду'
  window.I18n?.apply($('#node-dialog'))
  $('#node-dialog').showModal()
}

function openEditNodeDialog(node) {
  state.editingNodeId = node.id
  const form = $('#node-form')
  form.reset()
  $('[data-form-error]', form).textContent = ''
  form.elements.namedItem('name').value = node.name || ''
  form.elements.namedItem('region').value = node.region || ''
  form.elements.namedItem('advertisedHost').value = node.advertisedHost || ''
  form.elements.namedItem('httpPort').value = node.httpPort || 3128
  form.elements.namedItem('socksPort').value = node.socksPort || 1080
  form.elements.namedItem('webProxyPort').value = node.webProxyPort || 8443
  $('#node-dialog-eyebrow').textContent = 'РЕДАКТИРОВАНИЕ НОДЫ'
  $('#node-dialog-title').textContent = node.name
  $('#node-dialog-copy').textContent = 'Изменения конфигурации применятся после следующего обращения агента к панели.'
  $('#node-submit-button').textContent = 'Сохранить изменения'
  window.I18n?.apply($('#node-dialog'))
  $('#node-dialog').showModal()
}

function openProxyDialog() {
  const form = $('#proxy-form')
  form.reset()
  $('[data-form-error]', form).textContent = ''
  $('#proxy-node-select').innerHTML = state.nodes.length ? state.nodes.map(node => {
    const host = node.advertisedHost || node.publicIp
    const suffix = host ? `${host} · ${statusText(node.status)}` : tr('нет публичного адреса')
    return `<option value="${node.id}">${escapeHTML(node.name)} — ${escapeHTML(suffix)}</option>`
  }).join('') : '<option value="">Сначала добавьте ноду</option>'
  if (!state.nodes.length) form.elements.namedItem('source').value = 'external'
  syncProxySourceFields()
  updateProxyPreview()
  window.I18n?.apply($('#proxy-dialog'))
  $('#proxy-dialog').showModal()
}

function updateProxyPreview() {
  const form = $('#proxy-form')
  if (form.elements.namedItem('source').value === 'external') {
    $('#proxy-create-preview').textContent = maskProxyInput(form.elements.namedItem('externalUrl').value) || tr('Вставьте строку подключения внешнего прокси')
    return
  }
  const node = state.nodes.find(item => item.id === form.elements.namedItem('nodeId').value)
  const protocol = form.elements.namedItem('protocol').value || 'http'
  const username = form.elements.namedItem('username').value.trim() || 'login'
  const host = protocol === 'webproxy' ? node?.advertisedHost : node?.advertisedHost || node?.publicIp
  const port = protocol === 'webproxy' ? 443 : protocol === 'socks5' ? node?.socksPort : node?.httpPort
  $('#proxy-create-preview').textContent = host
    ? protocol === 'webproxy'
      ? `tg://webproxy?server=${host}&secret=••••`
      : `${protocol}://${username}:••••@${host}:${port}`
    : tr('Сначала укажите публичный адрес выбранной ноды')
}

function syncManagedProtocolFields() {
  const form = $('#proxy-form')
  const webproxy = form.elements.namedItem('protocol').value === 'webproxy'
  const credentials = $('#proxy-credential-fields')
  const note = $('#webproxy-create-note')
  credentials.classList.toggle('hidden', webproxy)
  note.classList.toggle('hidden', !webproxy)
  form.elements.namedItem('name').closest('label').classList.toggle('span-2', webproxy)
  $$('input, button', credentials).forEach(control => { control.disabled = webproxy })
  const username = form.elements.namedItem('username')
  username.required = !webproxy
}

function syncProxySourceFields() {
  const form = $('#proxy-form')
  const external = form.elements.namedItem('source').value === 'external'
  const managedFields = $('#managed-proxy-fields')
  const externalFields = $('#external-proxy-fields')
  managedFields.classList.toggle('hidden', external)
  externalFields.classList.toggle('hidden', !external)
  $$('input, select, button', managedFields).forEach(control => { control.disabled = external })
  $$('input, select, button', externalFields).forEach(control => { control.disabled = !external })
  if (!external && !state.nodes.length) {
    form.elements.namedItem('source').value = 'external'
    syncProxySourceFields()
  }
  if (!external) syncManagedProtocolFields()
}

function maskProxyInput(value) {
  const input = String(value || '').trim()
  if (!input) return ''
  try {
    const parsed = new URL(input)
    if (parsed.username) parsed.password = parsed.password ? '••••' : ''
    return parsed.toString().replace(/\/$/, '')
  } catch (error) {
    return input.replace(/(\/\/[^:@/]+:)[^@/]+@/, '$1••••@')
  }
}

function openProxyDetail(proxy) {
  state.selectedProxyId = proxy.id
  $('#proxy-detail-protocol').textContent = `${proxyProtocolLabel(proxy.protocol)} ${tr('ПРОКСИ')}`
  $('#proxy-detail-name').textContent = proxy.name
  const status = $('#proxy-detail-status')
  status.className = `status-pill ${proxy.status}`
  status.textContent = proxyStatusText(proxy.status)
  $('#proxy-detail-status-copy').textContent = proxyStatusDescription(proxy.status)
  $('#proxy-detail-url').textContent = proxy.maskedUrl || tr('Адрес появится после настройки ноды')
  const external = proxy.source === 'external'
  $('#proxy-detail-source-label').textContent = external ? tr('Источник') : tr('Нода')
  $('#proxy-detail-node').textContent = external ? proxy.provider || tr('Внешний прокси') : proxy.nodeName || tr('Не назначена')
  $('#proxy-detail-auth-label').textContent = proxy.protocol === 'webproxy' ? tr('Secret') : tr('Логин')
  $('#proxy-detail-login').textContent = proxy.protocol === 'webproxy' ? tr('Скрыт — можно перевыпустить') : proxy.username
  $('#proxy-detail-endpoint').textContent = proxy.endpoint || '—'
  $('#proxy-detail-time-label').textContent = external ? tr('Последняя проверка') : tr('Создан')
  $('#proxy-detail-created').textContent = external ? proxy.lastCheckedAt && !proxy.lastCheckedAt.startsWith('0001-') ? `${formatDateTime(proxy.lastCheckedAt)}${proxy.latencyMs ? ` · ${proxy.latencyMs} мс` : ''}` : tr('Ещё не проверялся') : formatDateTime(proxy.createdAt)
  $('#proxy-detail-url-copy').textContent = external
    ? tr('Реквизиты зашифрованы. Полную ссылку можно показать по отдельному действию.')
    : tr('Пароль скрыт. Полная ссылка повторно выдаётся при смене пароля.')
  $('#proxy-managed-config').classList.toggle('hidden', external)
  $('#proxy-external-config').classList.toggle('hidden', !external)
  $('#proxy-external-actions').classList.toggle('hidden', !external)
  $('#proxy-reset-password').classList.toggle('hidden', external)
  $('#proxy-edit-provider').value = proxy.provider || ''
  $('#proxy-edit-external-url').value = ''
  $('#proxy-edit-protocol').value = ['http', 'socks5', 'webproxy'].includes(proxy.protocol) ? proxy.protocol : 'http'
  $('#proxy-edit-protocol').disabled = proxy.protocol === 'webproxy'
  const nodeSelect = $('#proxy-edit-node')
  nodeSelect.innerHTML = state.nodes.map(node => {
    const host = node.advertisedHost || node.publicIp
    const suffix = host ? host : tr('нет публичного адреса')
    return `<option value="${node.id}">${escapeHTML(node.name)} — ${escapeHTML(suffix)}</option>`
  }).join('')
  nodeSelect.value = state.nodes.some(node => node.id === proxy.nodeId) ? proxy.nodeId : state.nodes[0]?.id || ''
  const toggle = $('#proxy-toggle')
  toggle.textContent = external
    ? proxy.enabled ? tr('Исключить из использования') : tr('Вернуть в использование')
    : proxy.enabled ? tr('Отключить прокси') : tr('Включить прокси')
  toggle.classList.toggle('danger', proxy.enabled)
  $('#proxy-reset-password').textContent = proxy.protocol === 'webproxy' ? tr('Перевыпустить secret') : tr('Сменить пароль')
  window.I18n?.apply($('#proxy-detail-dialog'))
  $('#proxy-detail-dialog').showModal()
}

function showSecret(title, copy, value) {
  resetCopyButton()
  $('#secret-title').textContent = tr(title)
  $('#secret-copy').textContent = tr(copy)
  $('#secret-value').textContent = value
  window.I18n?.apply($('#secret-dialog'))
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
    await Promise.all([loadSettings(), loadPlatform(), refreshAll()])
    startRefreshTimer()
  } catch (error) { errorNode.textContent = error.message }
})

$('#login-form').addEventListener('submit', async event => {
  event.preventDefault()
  const form = new FormData(event.currentTarget)
  $('#login-error').textContent = ''
  try {
    await api('/api/login', { method: 'POST', body: JSON.stringify(Object.fromEntries(form)) })
    state.me = await api('/api/me')
    showApp()
    await Promise.all([loadSettings(), loadPlatform(), refreshAll()])
    startRefreshTimer()
  } catch (error) { $('#login-error').textContent = error.message }
})

async function logout() {
  await api('/api/logout', { method: 'POST' })
  showLogin()
}

$('#utility-logout').addEventListener('click', logout)
$('#open-recap').addEventListener('click', () => {
  renderRecap()
  window.I18n?.apply($('#recap-dialog'))
  $('#recap-dialog').showModal()
})
$('#language-toggle').addEventListener('click', event => {
  event.stopPropagation()
  const menu = $('#language-menu')
  menu.classList.toggle('hidden')
  event.currentTarget.setAttribute('aria-expanded', String(!menu.classList.contains('hidden')))
})
$('#language-menu').addEventListener('click', event => {
  const button = event.target.closest('[data-locale]')
  if (!button) return
  window.I18n?.setLocale(button.dataset.locale)
  $('#language-menu').classList.add('hidden')
  $('#language-toggle').setAttribute('aria-expanded', 'false')
})
document.addEventListener('click', event => {
  if (!event.target.closest('.language-control')) {
    $('#language-menu').classList.add('hidden')
    $('#language-toggle').setAttribute('aria-expanded', 'false')
  }
})
document.addEventListener('proxyfleet:localechange', () => {
  syncLanguageMenu()
  if (!$('#app').classList.contains('hidden')) {
    renderSettings()
    render()
    setView(state.view, 'none')
    loadPlatform().catch(() => {})
  }
  window.I18n?.apply()
})
$$('.nav-item').forEach(link => link.addEventListener('click', event => {
  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
  event.preventDefault()
  setView(link.dataset.view)
}))
$$('[data-view-link]').forEach(button => button.addEventListener('click', () => setView(button.dataset.viewLink)))
$$('[data-action=new-node]').forEach(button => button.addEventListener('click', openNodeDialog))
$$('[data-action=new-proxy]').forEach(button => button.addEventListener('click', openProxyDialog))
$$('[data-close-dialog]').forEach(button => button.addEventListener('click', () => button.closest('dialog').close()))
$$('dialog').forEach(dialog => dialog.addEventListener('click', event => {
  const bounds = dialog.getBoundingClientRect()
  const outside = event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom
  if (outside) dialog.close()
}))

$$('[data-recap-background] button').forEach(button => button.addEventListener('click', () => {
  state.recap.background = button.dataset.background
  $$('[data-recap-background] button').forEach(item => item.classList.toggle('active', item === button))
  renderRecap()
}))
$$('[data-recap-toggle]').forEach(input => input.addEventListener('change', renderRecap))
$('#recap-note-input').addEventListener('input', renderRecap)
$$('[data-recap-palette] button').forEach(button => button.addEventListener('click', () => {
  state.recap.accent = button.dataset.color
  $$('[data-recap-palette] button').forEach(item => item.classList.toggle('active', item === button))
  renderRecap()
}))
$('#download-recap').addEventListener('click', async () => {
  try { await downloadRecap(); showToast(tr('PNG-карточка скачана')) }
  catch (error) { showToast(tr('Не удалось создать PNG')) }
})
$('#copy-recap').addEventListener('click', async event => {
  const button = event.currentTarget
  try {
    const blob = await recapBlob()
    if (!window.ClipboardItem || !navigator.clipboard?.write) throw new Error('Clipboard images are unavailable')
    await navigator.clipboard.write([new ClipboardItem({ 'image/png': blob })])
    const previous = button.textContent
    button.textContent = tr('✓ Скопировано')
    setTimeout(() => { button.textContent = previous; window.I18n?.apply(button) }, 2200)
  } catch (error) {
    await downloadRecap()
    showToast(tr('Буфер недоступен — PNG скачан'))
  }
})

$('#branding-form [name=systemName]').addEventListener('input', () => renderBrandingPreview(state.pendingLogoDataURL ?? state.settings.logoDataUrl ?? ''))
$('#branding-logo').addEventListener('change', event => {
  const file = event.currentTarget.files?.[0]
  if (!file) return
  if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type) || file.size > 512 * 1024) {
    event.currentTarget.value = ''
    showToast('Нужен PNG, JPEG или WebP до 512 КБ')
    return
  }
  const reader = new FileReader()
  reader.addEventListener('load', () => {
    state.pendingLogoDataURL = String(reader.result || '')
    renderBrandingPreview(state.pendingLogoDataURL)
  })
  reader.readAsDataURL(file)
})

$('#branding-logo-remove').addEventListener('click', () => {
  state.pendingLogoDataURL = ''
  $('#branding-logo').value = ''
  renderBrandingPreview('')
})

$('#branding-form').addEventListener('submit', async event => {
  event.preventDefault()
  const form = new FormData(event.currentTarget)
  const payload = {
    systemName: form.get('systemName'),
    logoDataUrl: state.pendingLogoDataURL ?? state.settings.logoDataUrl ?? '',
  }
  try {
    state.settings = await api('/api/settings/branding', { method: 'PATCH', body: JSON.stringify(payload) })
    state.pendingLogoDataURL = null
    applyBranding()
    renderSettings()
    showToast('Оформление сохранено')
  } catch (error) { showToast(error.message) }
})

$('#telegram-form').addEventListener('submit', async event => {
  event.preventDefault()
  const form = new FormData(event.currentTarget)
  const payload = {
    enabled: form.get('enabled') === 'on',
    chatId: form.get('chatId'),
    proxyUrl: form.get('proxyUrl'),
  }
  if (String(form.get('botToken') || '').trim()) payload.botToken = form.get('botToken')
  try {
    state.settings = await api('/api/settings/telegram', { method: 'PATCH', body: JSON.stringify(payload) })
    renderSettings()
    showToast('Настройки Telegram сохранены')
  } catch (error) { showToast(error.message) }
})

$('#telegram-test').addEventListener('click', async event => {
  const button = event.currentTarget
  const previous = button.textContent
  button.disabled = true
  button.textContent = tr('Проверяем…')
  try {
    const result = await api('/api/settings/telegram/test', { method: 'POST' })
    await loadSettings()
    showToast(result.botUsername ? tr('Сообщение отправлено через @{username}', { username: result.botUsername }) : 'Тестовое сообщение отправлено')
  } catch (error) { showToast(error.message) }
  finally { button.disabled = false; button.textContent = previous }
})

$('#telegram-disconnect').addEventListener('click', async () => {
  if (!window.confirm(tr('Удалить токен Telegram-бота из панели?'))) return
  try {
    state.settings = await api('/api/settings/telegram', { method: 'PATCH', body: JSON.stringify({ clearToken: true }) })
    renderSettings()
    showToast('Telegram-бот отключён')
  } catch (error) { showToast(error.message) }
})

window.addEventListener('beforeinstallprompt', event => {
  event.preventDefault()
  state.installPrompt = event
  if (!$('#view-settings').classList.contains('hidden')) renderPWASettings()
})

window.addEventListener('appinstalled', () => {
  state.installPrompt = null
  renderPWASettings()
  showToast('ProxyFleet установлен как приложение')
})

$('#pwa-install').addEventListener('click', async () => {
  if (!state.installPrompt) return
  await state.installPrompt.prompt()
  await state.installPrompt.userChoice
  state.installPrompt = null
  renderPWASettings()
})

$('#pwa-notifications-enabled').addEventListener('change', async event => {
  event.currentTarget.disabled = true
  try {
    if (event.currentTarget.checked) await enablePWANotifications()
    else await disablePWANotifications()
    await loadPWASettings()
    renderPWASettings()
  } catch (error) {
    event.currentTarget.checked = Boolean(state.pwaSubscription && state.pwa.subscription)
    showToast(error.message)
  } finally {
    event.currentTarget.disabled = !state.pwa.supported
  }
})

$('#pwa-form').addEventListener('submit', async event => {
  event.preventDefault()
  if (!state.pwaSubscription) return
  try {
    await savePWASubscription(state.pwaSubscription)
    await loadPWASettings()
    renderPWASettings()
    showToast('Настройки push-уведомлений сохранены')
  } catch (error) { showToast(error.message) }
})

$('#pwa-test').addEventListener('click', async event => {
  if (!state.pwaSubscription) return
  const button = event.currentTarget
  const previous = button.textContent
  button.disabled = true
  button.textContent = tr('Отправляем…')
  try {
    await api('/api/pwa/test', { method: 'POST', body: JSON.stringify({ endpoint: state.pwaSubscription.endpoint }) })
    showToast('Тестовое push-уведомление отправлено')
  } catch (error) { showToast(error.message) }
  finally { button.disabled = false; button.textContent = previous }
})

async function enablePWANotifications() {
  if (!state.pwa.supported) throw new Error('Этот браузер не поддерживает push-уведомления')
  const permission = await Notification.requestPermission()
  if (permission !== 'granted') throw new Error('Разрешите уведомления в настройках браузера')
  const registration = await navigator.serviceWorker.ready
  const existing = await registration.pushManager.getSubscription()
  const subscription = existing || await registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: urlBase64ToUint8Array(state.pwa.publicKey) })
  state.pwaSubscription = subscription
  try {
    await savePWASubscription(subscription)
  } catch (error) {
    if (!existing) await subscription.unsubscribe()
    state.pwaSubscription = existing
    throw error
  }
}

async function disablePWANotifications() {
  const subscription = state.pwaSubscription
  if (!subscription) return
  await api('/api/pwa/subscriptions', { method: 'DELETE', body: JSON.stringify({ endpoint: subscription.endpoint }) })
  await subscription.unsubscribe()
  state.pwaSubscription = null
}

async function savePWASubscription(subscription) {
  const serialized = subscription.toJSON()
  const form = $('#pwa-form')
  return api('/api/pwa/subscriptions', {
    method: 'POST',
    body: JSON.stringify({
      endpoint: serialized.endpoint,
      keys: serialized.keys,
      preferences: {
        nodes: form.elements.namedItem('nodes').checked,
        proxies: form.elements.namedItem('proxies').checked,
        system: form.elements.namedItem('system').checked,
      },
    }),
  })
}

function urlBase64ToUint8Array(value) {
  const padding = '='.repeat((4 - value.length % 4) % 4)
  const base64 = (value + padding).replace(/-/g, '+').replace(/_/g, '/')
  const raw = window.atob(base64)
  return Uint8Array.from([...raw].map(character => character.charCodeAt(0)))
}

$('#password-form').addEventListener('submit', async event => {
  event.preventDefault()
  const form = new FormData(event.currentTarget)
  if (form.get('newPassword') !== form.get('passwordConfirm')) {
    showToast('Новые пароли не совпадают')
    return
  }
  try {
    await api('/api/settings/password', { method: 'POST', body: JSON.stringify({ currentPassword: form.get('currentPassword'), newPassword: form.get('newPassword') }) })
    event.currentTarget.reset()
    showToast('Пароль изменён')
  } catch (error) { showToast(error.message) }
})

$('#node-form').addEventListener('submit', async event => {
  event.preventDefault()
  const form = new FormData(event.currentTarget)
  const payload = Object.fromEntries(form)
  payload.httpPort = Number(payload.httpPort)
  payload.socksPort = Number(payload.socksPort)
  payload.webProxyPort = Number(payload.webProxyPort)
  const errorNode = $('[data-form-error]', event.currentTarget)
  try {
    if (state.editingNodeId) {
      await api(`/api/nodes/${state.editingNodeId}`, { method: 'PATCH', body: JSON.stringify(payload) })
      $('#node-dialog').close()
      state.editingNodeId = null
      await refreshAll()
      showToast('Изменения сохранены')
    } else {
      const result = await api('/api/nodes', { method: 'POST', body: JSON.stringify(payload) })
      $('#node-dialog').close()
      showSecret('Нода создана', 'Выполните эту команду на новом VPS с установленным Docker:', result.installCommand)
      await refreshAll()
    }
  } catch (error) { errorNode.textContent = error.message }
})

$('#nodes-grid').addEventListener('click', event => {
  const editButton = event.target.closest('[data-edit-node]')
  if (!editButton) return
  const node = state.nodes.find(item => item.id === editButton.dataset.editNode)
  if (node) openEditNodeDialog(node)
})

$('#proxy-form').addEventListener('input', updateProxyPreview)
$('#proxy-form').addEventListener('change', event => {
  if (event.target.name === 'source') syncProxySourceFields()
  if (event.target.name === 'protocol') syncManagedProtocolFields()
  updateProxyPreview()
})
$('#generate-proxy-password').addEventListener('click', () => {
  const alphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789'
  const values = new Uint32Array(20)
  crypto.getRandomValues(values)
  $('#proxy-form [name=password]').value = [...values].map(value => alphabet[value % alphabet.length]).join('')
})

$('#proxy-form').addEventListener('submit', async event => {
  event.preventDefault()
  const form = new FormData(event.currentTarget)
  const errorNode = $('[data-form-error]', event.currentTarget)
  errorNode.textContent = ''
  const source = form.get('source') || 'managed'
  let payload
  if (source === 'external') {
    payload = { source, name: form.get('externalName'), provider: form.get('provider'), externalUrl: form.get('externalUrl') }
  } else {
    const node = state.nodes.find(item => item.id === form.get('nodeId'))
    if (form.get('protocol') === 'webproxy' && (!node?.advertisedHost || !node?.publicIp)) {
      errorNode.textContent = tr('Для Telegram WEB укажите домен ноды и дождитесь определения публичного IP.')
      return
    }
    if (!node?.advertisedHost && !node?.publicIp) {
      errorNode.textContent = tr('У выбранной ноды нет публичного адреса. Добавьте его в настройках ноды.')
      return
    }
    payload = { source, name: form.get('name'), username: form.get('username'), password: form.get('password'), protocol: form.get('protocol'), nodeId: form.get('nodeId') }
  }
  try {
    const result = await api('/api/proxies', { method: 'POST', body: JSON.stringify(payload) })
    $('#proxy-dialog').close()
    if (source === 'external') showToast('Внешний прокси добавлен и сохранён в зашифрованном виде')
    else showSecret(form.get('protocol') === 'webproxy' ? 'Telegram WEB готов' : 'Прокси готов', form.get('protocol') === 'webproxy' ? 'Скопируйте ссылку — она сразу откроется в Telegram:' : 'Скопируйте готовую ссылку подключения. Она показывается только один раз:', result.proxyUrl || result.password)
    await refreshAll()
  } catch (error) { errorNode.textContent = error.message }
})

$('#proxies-table').addEventListener('click', event => {
  const target = event.target.closest('[data-manage-proxy], [data-open-proxy]')
  if (!target) return
  const id = target.dataset.manageProxy || target.dataset.openProxy
  const proxy = state.proxies.find(item => item.id === id)
  if (proxy) openProxyDetail(proxy)
})

$('#proxies-table').addEventListener('keydown', event => {
  if (!['Enter', ' '].includes(event.key)) return
  const row = event.target.closest('[data-open-proxy]')
  if (!row) return
  event.preventDefault()
  const proxy = state.proxies.find(item => item.id === row.dataset.openProxy)
  if (proxy) openProxyDetail(proxy)
})

$('#proxy-toggle').addEventListener('click', async () => {
  const proxy = state.proxies.find(item => item.id === state.selectedProxyId)
  if (!proxy) return
  try {
    await api(`/api/proxies/${proxy.id}`, { method: 'PATCH', body: JSON.stringify({ enabled: !proxy.enabled }) })
    $('#proxy-detail-dialog').close()
    await refreshAll()
    showToast(proxy.source === 'external'
      ? proxy.enabled ? 'Внешний прокси исключён из использования' : 'Внешний прокси возвращён в использование'
      : proxy.enabled ? 'Прокси отключён' : 'Прокси включён')
  } catch (error) { showToast(error.message) }
})

$('#proxy-save-external').addEventListener('click', async () => {
  const proxy = state.proxies.find(item => item.id === state.selectedProxyId)
  if (!proxy || proxy.source !== 'external') return
  const payload = { provider: $('#proxy-edit-provider').value }
  const externalUrl = $('#proxy-edit-external-url').value.trim()
  if (externalUrl) payload.externalUrl = externalUrl
  try {
    await api(`/api/proxies/${proxy.id}`, { method: 'PATCH', body: JSON.stringify(payload) })
    $('#proxy-detail-dialog').close()
    await refreshAll()
    showToast('Внешний прокси обновлён')
  } catch (error) { showToast(error.message) }
})

$('#proxy-check-external').addEventListener('click', async event => {
  const proxy = state.proxies.find(item => item.id === state.selectedProxyId)
  if (!proxy) return
  const button = event.currentTarget
  const previous = button.textContent
  button.disabled = true
  button.textContent = tr('Проверяем…')
  try {
    const result = await api(`/api/proxies/${proxy.id}/check`, { method: 'POST' })
    $('#proxy-detail-dialog').close()
    await refreshAll()
    showToast(result.reachable ? 'Внешний прокси доступен' : 'Внешний прокси сейчас недоступен')
  } catch (error) { showToast(error.message) }
  finally { button.disabled = false; button.textContent = previous }
})

$('#proxy-reveal-external').addEventListener('click', async () => {
  const proxy = state.proxies.find(item => item.id === state.selectedProxyId)
  if (!proxy) return
  try {
    const result = await api(`/api/proxies/${proxy.id}/reveal`, { method: 'POST' })
    $('#proxy-detail-dialog').close()
    showSecret('Подключение внешнего прокси', 'Скопируйте строку подключения и храните её в безопасном месте:', result.proxyUrl)
  } catch (error) { showToast(error.message) }
})

$('#proxy-save-settings').addEventListener('click', async () => {
  const proxy = state.proxies.find(item => item.id === state.selectedProxyId)
  const node = state.nodes.find(item => item.id === $('#proxy-edit-node').value)
  if (!proxy || !node) return
  if (!node.advertisedHost && !node.publicIp) {
    showToast('У выбранной ноды нет публичного адреса. Добавьте его в настройках ноды.')
    return
  }
  try {
    await api(`/api/proxies/${proxy.id}`, { method: 'PATCH', body: JSON.stringify({ protocol: $('#proxy-edit-protocol').value, nodeId: node.id }) })
    $('#proxy-detail-dialog').close()
    await refreshAll()
    showToast('Настройки прокси сохранены')
  } catch (error) { showToast(error.message) }
})

$('#proxy-reset-password').addEventListener('click', async () => {
  const proxy = state.proxies.find(item => item.id === state.selectedProxyId)
  if (!proxy) return
  try {
    const result = await api(`/api/proxies/${proxy.id}`, { method: 'PATCH', body: JSON.stringify({ password: '' }) })
    $('#proxy-detail-dialog').close()
    showSecret(proxy.protocol === 'webproxy' ? 'Secret обновлён' : 'Пароль обновлён', 'Старая ссылка больше не работает. Скопируйте новую:', result.proxyUrl || result.password)
    await refreshAll()
  } catch (error) { showToast(error.message) }
})

$('#proxy-delete').addEventListener('click', () => {
  const proxy = state.proxies.find(item => item.id === state.selectedProxyId)
  if (!proxy) return
  $('#delete-proxy-name').textContent = proxy.name
  window.I18n?.apply($('#delete-proxy-dialog'))
  $('#delete-proxy-dialog').showModal()
})

$('#confirm-delete-proxy').addEventListener('click', async event => {
  const proxy = state.proxies.find(item => item.id === state.selectedProxyId)
  if (!proxy) return
  const button = event.currentTarget
  button.disabled = true
  button.textContent = tr('Удаление…')
  try {
    await api(`/api/proxies/${proxy.id}`, { method: 'DELETE' })
    $('#delete-proxy-dialog').close()
    $('#proxy-detail-dialog').close()
    state.selectedProxyId = ''
    await refreshAll()
    showToast('Прокси удалён')
  } catch (error) { showToast(error.message) }
  finally {
    button.disabled = false
    button.textContent = tr('Удалить прокси')
  }
})

$('#copy-secret').addEventListener('click', async () => {
  const button = $('#copy-secret')
  try {
    await navigator.clipboard.writeText($('#secret-value').textContent)
    button.textContent = '✓ Скопировано'
    button.classList.add('copied')
    clearTimeout(button.resetTimer)
    button.resetTimer = setTimeout(resetCopyButton, 2500)
  } catch (error) {
    showToast('Не удалось скопировать')
  }
})

window.addEventListener('popstate', syncRoute)

function resetCopyButton() {
  const button = $('#copy-secret')
  clearTimeout(button.resetTimer)
  button.textContent = tr('Копировать')
  button.classList.remove('copied')
}

function showToast(message) {
  const toast = $('#toast')
  toast.textContent = tr(message)
  toast.classList.add('show')
  clearTimeout(showToast.timer)
  showToast.timer = setTimeout(() => toast.classList.remove('show'), 2500)
}

function setEmpty(root, text) { root.className = 'empty-state'; root.textContent = text }
function statusText(status) { return tr(({ online: 'Онлайн', offline: 'Офлайн', pending: 'Ожидает' })[status] || status) }
function proxyStatusText(status) { return tr(({ online: 'Готов', offline: 'Нода офлайн', pending: 'Ожидает ноду', disabled: 'Отключён', unassigned: 'Без ноды', unchecked: 'Не проверен', unavailable: 'Недоступен' })[status] || status) }
function proxyStatusDescription(status) { return tr(({ online: 'Прокси готов принимать подключения', offline: 'Нода не отвечает — подключения временно недоступны', pending: 'Агент ноды ещё не подключился к панели', disabled: 'Доступ выключен вручную', unassigned: 'Выберите ноду для этого прокси', unchecked: 'Внешний прокси ещё не проверялся', unavailable: 'Адрес внешнего прокси не отвечает' })[status] || '') }
function proxyProtocolLabel(protocol) { return protocol === 'webproxy' ? 'TELEGRAM WEB' : protocol === 'socks5' ? 'SOCKS5' : 'HTTP' }
function formatBytes(value) { const english = window.I18n?.locale() === 'en'; if (!value) return english ? '0 B' : '0 Б'; const units = english ? ['B','KB','MB','GB','TB'] : ['Б','КБ','МБ','ГБ','ТБ']; const i = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1); const number = new Intl.NumberFormat(window.I18n?.localeTag() || 'ru-RU', { maximumFractionDigits: i ? 1 : 0 }).format(value / 1024 ** i); return `${number} ${units[i]}` }
function formatDuration(value) { const seconds = Number(value) || 0; if (!seconds) return '—'; const days = Math.floor(seconds / 86400); const hours = Math.floor((seconds % 86400) / 3600); const minutes = Math.floor((seconds % 3600) / 60); if (window.I18n?.locale() === 'en') { if (days) return `${days}d ${hours}h`; return hours ? `${hours}h ${minutes}m` : `${minutes}m` } if (days) return `${days} д ${hours} ч`; return hours ? `${hours} ч ${minutes} мин` : `${minutes} мин` }
function formatAgents(value) { const number = Number(value) || 0; if (window.I18n?.locale() === 'en') return `${number} ${number === 1 ? 'agent' : 'agents'}`; const mod100 = number % 100; const mod10 = number % 10; const word = mod100 >= 11 && mod100 <= 19 ? 'агентов' : mod10 === 1 ? 'агент' : mod10 >= 2 && mod10 <= 4 ? 'агента' : 'агентов'; return `${number} ${word}` }
function formatDate(value) { return new Intl.DateTimeFormat(window.I18n?.localeTag() || 'ru-RU', { day: '2-digit', month: 'short', year: 'numeric' }).format(new Date(value)) }
function formatCompactDate(value) { return new Intl.DateTimeFormat(window.I18n?.localeTag() || 'ru-RU', { day: '2-digit', month: '2-digit', year: '2-digit' }).format(new Date(value)) }
function formatDateTime(value) { return new Intl.DateTimeFormat(window.I18n?.localeTag() || 'ru-RU', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' }).format(new Date(value)) }
function timeAgo(value) { const sec = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 1000)); if (sec < 15) return tr('только что'); const relative = new Intl.RelativeTimeFormat(window.I18n?.localeTag() || 'ru-RU', { numeric: 'always' }); if (sec < 60) return relative.format(-sec, 'second'); if (sec < 3600) return relative.format(-Math.floor(sec / 60), 'minute'); return formatDateTime(value) }
function syncLanguageMenu() { const locale = window.I18n?.locale() || 'ru'; $$('[data-locale]').forEach(button => button.classList.toggle('active', button.dataset.locale === locale)) }
function escapeHTML(value = '') { return String(value).replace(/[&<>'"]/g, char => ({ '&':'&amp;', '<':'&lt;', '>':'&gt;', "'":'&#39;', '"':'&quot;' })[char]) }

bootstrap()
