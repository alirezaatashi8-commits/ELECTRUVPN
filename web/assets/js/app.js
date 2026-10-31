function electruApp() {
  return {
    tab: 'dashboard',
    routeReady: false,
    overview: {},
    inbounds: [],
    inboundSearch: '', inboundProtocolFilter: 'all', inboundStatusFilter: 'all', selectedInboundIds: [], inboundSearchActive: false, inboundSearchLoading: false, inboundSearchPage: 1, inboundSearchPageSize: 50, inboundSearchTotal: 0, inboundSearchHasMore: false, inboundPage: 1, inboundPageSize: 50, inboundTotal: 0, inboundHasMore: false, inboundClientPages: {}, inboundClientLoading: {},
    inboundPreview: {show:false, title:'', engine:'', config:'', valid:false, msg:''}, inboundAdvanced: {show:false, title:'', id:null, json:'', error:'', mode:'edit'}, inboundHealth: {show:false, title:'', data:null}, inboundLive:{show:false,title:'',data:{},id:null}, inboundLiveTimer:null, inboundAnalytics:{show:false,title:'',days:7,points:[],health:null},
    inboundStep: 1,
    inboundSaving: false,
    inboundValidating: false,
    clientSaving: false,
    clientWizard: { show: false, inboundId: 0, step: 1, expiryDays: 0 },
    selectedInboundPreset: '',
    inboundTemplates: [],
    inboundTemplateName: '',
    logs: [],
    me: { username: '', role: '', mustChangePassword: false },
    enterprise: { metrics: {}, alerts: [], search: { q: '', obj: null } },
    polling: true,
    _overviewLoading: false,
    createMenuOpen: false,
    showInboundModal: false,
    showQuickGrpcModal: false,
    showQuickTransportModal: false,
    showMassProvisionModal: false,
    massProvision: { sourceInboundId: 0, count: 10, namePrefix: 'bulk-inbound', startPort: 0, includeClients: false, dryRun: false, busy: false, result: null },
    quickGrpc: { remark:'', clientName:'', totalGB:0, days:30, inboundTotalGB:0, inboundDays:0, port:0, ipMode:'ipv4', addressType:'ipv4', saving:false, result:null },
    quickTransport: { network:'ws', label:'WebSocket', remark:'', clientName:'', totalGB:0, days:30, inboundTotalGB:0, inboundDays:0, port:0, ipMode:'ipv4', addressType:'ipv4', saving:false, result:null },
    showQrModal: false,
    showEditClientModal: false,
    showClientLiveModal: false,
    clientLive: { client: {}, inbound: {}, live: {}, link: '' },
    clientLiveTimer: null,
    expandedInboundId: null,
    selectedClientIds: [],
    clientGroups: [],          // {name, count} from /clients/groups
    clientGroupFilter: '',     // filter clients inside expanded inbound by group name
    bulkOpenFor: null,
    editingInboundId: null,
    qrLink: '', qrLinks: {}, qrSubscription: '', qrSubscriptionLinks: {}, qrFamily: 'ipv4', qrClientId: null, qrClientEmail: '',
    mobileDelivery: { show: false, platform: 'android', family: 'ipv4', client: {}, inbound: {}, link: '', subscription: '', links: {}, showQr: false },
    subscriptionClinic: { show:false, client:{}, data:null, loading:false },
    pw: { current: '', next: '' },
    credentials: { current: '', username: '', password: '' },
    panelSettings: { port: 2087, tlsEnabled: false, tlsHost: '', certificateConfigured: false, privateKeyConfigured: false, certificatePEM: '', privateKeyPEM: '', clearCertificate: false },
    settingsCenter: {
      publicHost:'', timezone:'Europe/Berlin', language:'fa', theme:'dark', trafficUnit:'GB', speedUnit:'Mbps', dashboardRefreshSec:10,
      sessionTimeoutHours:168, loginMaxAttempts:5, loginBlockMinutes:5, fail2banEnabled:true, securityHeaders:true, hsts:true, csrf:true, cookieSecure:true,
      apiEnabled:true, apiTokenTTL:0, apiIPAllowlist:'', apiIPBlocklist:'', panelIPAllowlist:'', panelIPBlocklist:'',
      xrayLogLevel:'warning', dnsMode:'system', dnsProvider:'auto', dnsCustomResolvers:'', ipv6:true, udp:true, keepaliveSec:30, fdLimit:0,
      bbrEnabled:true, qdisc:'fq', tcpMtuProbing:true, networkOptimizer:true,
      backupEnabled:true, backupIntervalMin:1440, backupRetention:14, backupRemoteDir:'', backupIncludeXray:true, backupIncludeSingbox:true,
      notifyTelegram:false, notifyWebhook:false, notifyEmail:false, notifyOnNodeOffline:true, notifyOnCertExpiry:true, notifyOnLoginFailures:true,
      nodeHealthIntervalSec:15, nodeTimeoutSec:10, failoverEnabled:true, loadBalanceEnabled:true, nodeMaintenance:false, nodeOfflineThresholdSec:45,
      clientDefaultTrafficGB:0, clientDefaultDeviceLimit:0, clientDefaultExpiryDays:30, clientDefaultSubscription:true, clientDefaultAutoExtend:false,
      autoUpdate:false, updateCheckDaily:true, logRetentionDays:14, auditRetentionDays:90,
      templateProtocol:'vless', templateNetwork:'tcp', templateSecurity:'reality',
      cpuSoftLimit:90, memorySoftLimit:90, updateChannel:'beta', maintenanceStart:'02:00', maintenanceEnd:'04:00', autoRollback:true,
      logLevel:'info', cacheTTL:60, cacheMaxMB:128, workerCount:0, queueSize:1000, proxyHTTP:'', proxySOCKS:'', dnsPrimary:'1.1.1.1', dnsSecondary:'8.8.8.8',
      backupEncryption:false, backupEncryptionKey:'', backupRemoteType:'local', backupRemoteEndpoint:'', backupVerifyAfterCreate:true, backupRestoreTest:false,
      alertCooldownMin:30, alertNodeOffline:true, alertNodeRecovered:true, alertDisk:true, alertCPU:true, alertMemory:true, alertBackup:true, alertXray:true,
      firewallManaged:false, firewallSSHPort:22, firewallPanelPort:443, autoCleanupEnabled:true, cleanupSessionsDays:30, cleanupAuditDays:90, cleanupLogsDays:14,
      flag_clientCommandCenter:true, flag_nodeFailover:true, flag_smartClient:true, flag_trafficAnalytics:true, flag_configurationSafety:true, flag_automaticRollback:true, flag_experimentalLoadBalancer:false, flag_experimentalUpdateEngine:false, flag_liveUserApply:false
    },
    settingsSaving:false,
    advancedSettings: { security: {}, sessions: [], runtime: {}, logs: '', emergency: {} },
    newInbound: inboundDefaults(),
    portStatus: { state: 'idle', message: '', port: 0 },
    newClient: { email: '', totalGB: 0, limitIp: 0, expiryTime: 0 },
    editClient: { id: null, email: '', totalGB: 0, limitIp: 0, expiryDate: '', days: '', enable: true, group: '', autoExtendGB: 0, autoExtendDays: 0, subscriptionEnabled: true, subscriptionName: '', subscriptionGroup: '', subscriptionRefreshMin: 15, subscriptionAddressType:'ipv4', subscriptionAutoUpdate:true, subscriptionSmart:false },
    bulk: { prefix: '', count: 10, totalGB: 0, days: 0 },
    toasts: [],
    toastSeq: 0,
    confirmModal: { show: false, title: '', message: '', action: null, cancelAction: null, confirmLabel: 'Confirm', cancelLabel: 'Cancel' },
    actionMenu: { type: '', id: null },
    netHistory: { labels: [], up: [], down: [] },
    pollTimers: [],
    resellers: [], resellerSummary: {}, resellerMe: {}, resellerStats: [], resellerTopups: [], resellerWholesale: {}, resellerTopupAmount: 0, resellerTopupNote: '', resellerFormOpen: false, resellerForm: { username:'', password:'', parentUserId:0, initialCredit:0, maxTotalGB:0, maxGBPerClient:0, maxClients:0, allowSubResellers:false, maxSubResellers:0, commissionPercent:0, wholesalePricePerGB:0, notes:'' }, resellerLedger: [], resellerEdit: null,
    pro: { auditFilter: { username: '', role: '', action: '', source: '' }, system: {}, audit: [], nodes: [], apiKeys: [], backups: [], users: [], subscriptionGroups: [], subscriptionPresets: [], subscriptionOverview: {}, subscriptionLogs: [], node: { name: '', address: '', port: 22, agentEndpoint: '', role: 'node' }, agentBootstrap: null },
    ops: { health: {}, system: {}, publicHostStatus: null, newDomain: '', newIPv4: '', settings: { publicHost: '', timezone: '' }, templates: [], notifications: [], rules: [], tasks: [], diag: { host: '', port: 443, result: null }, commandCenter: {}, preflight: {}, recovery: {} },
    ultra: { clients: [], devices: [], plans: [], snapshots: [], selectedClient: null, plan: { name: '', description: '', totalGB: 0, limitIp: 0, durationDay: 30 } },
    onlineUsers: { users: [], count: 0 },
    honeypot: { stats: null, settings: null, report: null, banIP: '', banHours: 24, loading: false },
    replay: { q: '', results: [], client: null, events: [], audits: [], note: '', loading: false },
    onlineAuto: true,
    logsAuto: true,
    logsFollow: true,
    _logsLoading: false,
    _onlineLoading: false,
    onlineTimer: null,
    clientDevices: { show: false, clientId: 0, email: '', limitIp: 0, list: [] },

    tickets: { list: [], selected: null, messages: [], reply: '', filter: 'all', newSubject: '', newBody: '', loading: false },
    telegram: { adminBotToken: '', adminBotTokenHint: '', adminIds: '', adminRoles: '', userBotToken: '', userBotTokenHint: '' },
    tunnelChain: { id: 0, enabled: false, selectionMode: 'manual', autoStrategy: 'least_load', midNodeId: 0, exitNodeId: 0, midAddress: '', midPort: 443, midProtocol: 'vless', midUUID: '', exitAddress: '', exitPort: 443, exitProtocol: 'vless', exitUUID: '', exitPublicKey: '', exitShortId: '', exitSni: '', rotateAddresses: '', rotateOnFail: true, confirmBeforeFailover: true, activeExitHost: '', activeMidHost: '', hopNodes: [] },
    finance: {
      from: '', to: '', loading: false,
      summary: null, planSales: [], monthly: [], churn: null, transactions: [],
      chartMonthly: null, chartPlans: null
    },
    shop: { plans: [], orders: [], orderFilter: 'pending_review', settings: { cardNumber: '', cardHolder: '', instructions: '', enabled: true }, planForm: { name: '', totalGB: 30, durationDays: 30, priceToman: 0, inboundId: 0 }, broadcast: '', broadcastTarget: 'all', trial: { enabled: false, mb: 500, hours: 2, inboundId: 0 }, winback: { enabled: false, daysAfter: 3, discountPct: 30, planIds: '', message: '' }, customGB: { enabled: false, pricePerGB: 0, minGB: 5, maxGB: 500, days: 30, inboundId: 0 } },
    platform: { summary: {}, security: {}, jobs: [], billingPlans: [], orders: [], webhooks: [], certificates: [], plugins: [], operations: [], domains: [], nodeGroups: [] },
    bbr: { status: {}, test: {} },
    fabric: { performance: [], routing: [], advisor: [], deployments: [], selected: null },
    dashboard: { nodesHealthy: 0, nodesTotal: 0, nodeRows: [], protocols: [], protocolTotal: 0, alerts: [] },
    dashboardWidgets: [
      { key: 'resources', label: 'Server Resources', enabled: true },
      {key:'kpi',label:'KPI cards',enabled:true},{key:'traffic',label:'Traffic',enabled:true},{key:'alerts',label:'Alerts',enabled:true},{key:'nodes',label:'Nodes',enabled:true},{key:'users',label:'User activity',enabled:true},{key:'protocols',label:'Protocols',enabled:true},{key:'performance',label:'Advisor',enabled:true},{key:'system',label:'System health',enabled:true},{key:'map',label:'Topology',enabled:true}
    ],
    dashboardCompact: false, showWidgetManager: false, dashboardLastUpdate: 'now', liveClock: new Date().toLocaleTimeString(), liveClockTimer: null, commandPalette: false, commandQuery: '',
    panelUpdate: { localVersion: '', remoteVersion: '', updateAvailable: false, message: '', checking: false, applying: false },
    // Subscription command center (tab state) — must exist before any template access
    subscription: {
      clients: [],
      devices: [],
      groups: [],
      presets: [],
      overview: {},
      logs: [],
      plans: [],
      inboundMap: {},
      selected: null,
      search: '',
      filter: 'all',
      refreshing: false,
      chart: null,
    },

    init() {
      // Alpine 3 calls init() automatically. Guard against a second call so
      // timers, popstate and visibility listeners are never registered twice.
      if (this._booted) return;
      this._booted = true;
      this.applyUiLanguageOnBoot();

      // Live Control Center clock. It is fully independent from API polling and
      // is driven by a timer stored on window (outside Alpine's reactive proxy),
      // so it keeps ticking even if the dashboard template is re-created by x-if.
      const updateLiveClock = () => {
        let value;
        try {
          value = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
        } catch (_) {
          value = new Date().toTimeString().slice(0, 8);
        }
        this.liveClock = value;
        // Direct DOM write as a safety net: x-if recreates the dashboard node
        // each time the tab is opened, and the new node must show the time at once.
        document.querySelectorAll('[data-live-clock]').forEach((el) => {
          if (el.textContent !== value) el.textContent = value;
        });
      };
      updateLiveClock();
      // A fixed setInterval(…, 1000) is not aligned with the wall-clock second, so
      // the display can lag by up to ~1s. Re-arm a timeout for the NEXT second
      // boundary on every tick (self-correcting, no drift).
      if (window.__electruClockTimer) { clearTimeout(window.__electruClockTimer); clearInterval(window.__electruClockTimer); }
      const armClock = () => {
        window.__electruClockTimer = setTimeout(() => { updateLiveClock(); armClock(); }, 1000 - (Date.now() % 1000) + 3);
      };
      armClock();
      this._tickLiveClock = updateLiveClock;

      this.routeReady = true;
      this.applyRoute(false);
      window.addEventListener('popstate', () => this.applyRoute(false));
      try { this.inboundTemplates = JSON.parse(localStorage.getItem('electruvpn.inboundTemplates') || '[]'); } catch (_) { this.inboundTemplates = []; }
      this.loadDashboardWidgets();
      // applyRoute() already loads the selected tab, including its overview;
      // do not immediately request the overview a second time.
      // applyRoute() already loads the selected tab. Only preload inbounds for
      // the Dashboard protocol summary; other tabs should not trigger unrelated
      // client/inbound queries during startup.
      if (this.tab === 'dashboard') this.loadInbounds();
      // Role/session data is needed globally, but Pro and Enterprise datasets are
      // loaded by their own tabs instead of eagerly on every page visit.
      this.loadMe();

      // Pausing polling while the tab is hidden is a real, measurable win:
      // no wasted CPU/gopsutil calls or xray API hits for a backgrounded
      // panel, and traffic still catches up the moment it's visible again.
      document.addEventListener('visibilitychange', () => {
        this.polling = document.visibilityState === 'visible';
        if (!this.polling) return;
        updateLiveClock();
        // Refresh only the data for the tab the user can currently see.
        if (this.tab === 'dashboard') { this.refreshOverview(); this.loadDashboardPro(); }
        if (this.tab === 'pro' || this.tab === 'ops') this.loadEnterprise();
      });

      // Server-side traffic accounting is independent of browser sessions.
      // Poll dashboard data only while its tab is visible, and less frequently
      // to avoid unnecessary API/DB work on small VPSs with an open admin panel.
      this.pollTimers.push(setInterval(() => {
        if (!this.polling) return;
        if (this.tab === 'dashboard') { this.refreshOverview(); this.loadDashboardPro(); }
        if (this.tab === 'pro' || this.tab === 'ops') this.loadEnterprise();
      }, 90000));

      // Live dashboard values (network speed, online users, CPU/RAM) refresh every
      // 5s while the dashboard is visible. The heavier multi-request batch above
      // stays on its slower 90s cadence.
      this.pollTimers.push(setInterval(() => {
        if (this.polling && this.tab === 'dashboard') this.refreshOverview();
      }, 5000));

      // Xray log viewer follows the tail while the Logs tab is visible.
      this.pollTimers.push(setInterval(() => {
        if (this.polling && this.tab === 'logs' && this.logsAuto) this.loadLogs();
      }, 3000));

      this.$nextTick(() => this.ensureNetChart());
    },

    // ---------- client-side panel routing ----------
    routeMap() {
      return { dashboard:'dashboard', inbounds:'inbounds', online:'online', subscription:'subscription', finance:'finance', pro:'pro', control:'control', platform:'platform', fabric:'fabric', ops:'ops', logs:'logs', settings:'settings', telegram:'telegram', resellers:'resellers' };
    },
    applyRoute(replace=true) {
      const map=this.routeMap();
      let raw=(window.location.pathname||'').replace(/\/+$/,'').split('/').pop() || 'dashboard';
      if (window.location.hash) raw=window.location.hash.slice(1).split('?')[0];
      if (!map[raw]) raw='dashboard';
      this.tab=raw;
      this.loadTab(raw);
      if (replace) this.updateRoute(raw,true);
    },
    updateRoute(tab, replace=false) {
      const map=this.routeMap();
      if (!map[tab]) tab='dashboard';
      const base=window.__ELECTRU_BASE_PATH__ || '';
      const target=(base||'')+'/dashboard/'+tab;
      if (replace) history.replaceState({tab},'',target);
      else history.pushState({tab},'',target);
    },
    goTab(tab) {
      const known = this.routeMap();
      const loadable = ['dashboard','inbounds','online','subscription','finance','pro','control','platform','fabric','ops','logs','settings','telegram','resellers'];
      if (!known[tab] && !loadable.includes(tab)) tab = 'dashboard';
      this.tab = tab;
      // Always update URL when possible so refresh stays on the same page
      if (known[tab] || loadable.includes(tab)) this.updateRoute(tab, false);
      this.loadTab(tab);
      window.scrollTo({top:0,behavior:'smooth'});
    },

    uiLang: (typeof localStorage !== 'undefined' && localStorage.getItem('electru_lang')) || 'fa',
    i18n: {
      fa: {
        'nav.sectionOverview':'نمای کلی',
        'nav.sectionNetwork':'شبکه و کاربران',
        'nav.sectionAccount':'حساب',
        'nav.sectionControl':'مرکز کنترل',
        'nav.sectionOps':'عملیات',
        'nav.sectionSystem':'سیستم',
        'nav.dashboard':'داشبورد',
        'nav.inbounds':'اینباندها',
        'nav.online':'کاربران آنلاین',
        'nav.subscription':'سابسکریپشن',
        'nav.settings':'تنظیمات پنل',
        'nav.telegram':'تلگرام',
        'nav.signout':'خروج',
        'nav.pro':'مرکز Pro',
        'nav.control':'مرکز کنترل',
        'nav.platform':'پلتفرم',
        'nav.finance':'مالی',
        'nav.resellers':'نمایندگان',
        'nav.fabric':'نودها',
        'nav.ops':'عملیات',
        'nav.logs':'لاگ Xray',
        'nav.language':'زبان',
        'nav.lang.fa':'فارسی',
        'nav.lang.en':'English',
        'common.refresh':'بروزرسانی',
        'common.save':'ذخیره',
        'common.cancel':'انصراف',
        'common.delete':'حذف',
        'common.edit':'ویرایش',
        'common.create':'ایجاد',
        'common.back':'بازگشت',
        'common.next':'بعدی',
        'common.search':'جستجو',
        'common.loading':'در حال بارگذاری…',
        'common.success':'انجام شد',
        'common.error':'خطا',
        'xray.running':'در حال اجرا',
        'xray.stopped':'متوقف',
        'online.title':'کاربران آنلاین',
        'online.hint':'بر اساس ترافیک اخیر و دستگاه‌های متصل (Xray و sing-box)؛ اتصال کاملاً بیکار ممکن است دیده نشود.',
        'online.refresh':'بروزرسانی',
        'online.auto':'خودکار (۵ ثانیه)',
        'online.count':'تعداد',
        'online.client':'کلاینت',
        'online.inbound':'اینباند',
        'online.speed':'سرعت',
        'online.actions':'عملیات',
        'online.kick':'قطع جلسه',
        'online.kickDisable':'قطع + غیرفعال',
        'online.devices':'دستگاه‌ها',
        'online.liveDevices':'دستگاه متصل',
        'online.empty':'کسی آنلاین نیست یا هنوز ترافیک نمونه نشده است.',
        'online.kicked':'جلسه قطع شد',
        'online.disabled':'کلاینت غیرفعال شد',
        'online.kickFail':'قطع جلسه ناموفق بود',
        'devices.title':'دستگاه‌ها',
        'devices.active':'فعال',
        'devices.revoked':'لغو شده',
        'devices.revoke':'لغو',
        'devices.revokeAll':'لغو همه',
        'devices.revokedOk':'دستگاه لغو شد',
        'devices.revokedAllOk':'همه دستگاه‌ها لغو شدند',
        'devices.loadFail':'بارگذاری دستگاه‌ها ناموفق بود',
        'lang.changed':'زبان تغییر کرد',
        'dash.title':'نمای زیرساخت',
        'dash.live':'مرکز کنترل زنده',
        'dash.compact':'نمای فشرده',
        'dash.expanded':'نمای گسترده',
        'dash.widgets':'ویجت‌ها',
        'dash.resetLayout':'بازنشانی چیدمان',
        'dash.nodes':'نودها',
        'dash.users':'کاربران',
        'dash.devices':'دستگاه‌ها',
        'dash.traffic':'ترافیک',
        'dash.jobs':'کارهای فعال',
        'dash.security':'امنیت',
        'dash.orders':'سفارش‌ها',
        'dash.disk':'دیسک',
        'dash.healthy':'سالم',
        'dash.clients':'کلاینت‌ها',
        'dash.registered':'ثبت‌شده',
        'dash.total':'مجموع',
        'dash.runningQueued':'در حال اجرا / صف',
        'dash.auditEvents':'رویدادهای ممیزی',
        'dash.pending':'در انتظار',
        'dash.used':'مصرف‌شده',
        'top.restartXray':'ری‌استارت Xray',
        'top.command':'دستور',
        'top.stop':'توقف',
        'top.start':'شروع',
        'common.close':'بستن',
        'common.port':'پورت',
        'common.protocol':'پروتکل',
        'common.status':'وضعیت',
        'common.clients':'کلاینت‌ها',
        'common.security':'امنیت',
        'common.traffic':'ترافیک',
        'common.download':'دانلود',
        'common.upload':'آپلود',
        'common.domain':'دامنه',
        'common.address':'آدرس',
        'common.name':'نام',
        'common.remark':'عنوان',
        'common.expiry':'انقضا',
        'common.expires':'منقضی می‌شود',
        'common.all':'همه',
        'common.approve':'تأیید',
        'common.restore':'بازیابی',
        'common.rollback':'بازگردانی',
        'common.restart':'ری‌استارت',
        'common.live':'زنده',
        'common.copy':'کپی',
        'common.copyConfig':'کپی کانفیگ',
        'common.resetTraffic':'ریست ترافیک',
        'common.presets':'پیش‌فرض‌ها',
        'common.transport':'ترنسپورت',
        'common.ipMode':'حالت IP',
        'inbound.title':'اینباندها',
        'inbound.new':'اینباند جدید',
        'inbound.clients':'کلاینت‌ها',
        'inbound.edit':'ویرایش',
        'inbound.enable':'فعال',
        'inbound.disable':'غیرفعال',
        'inbound.wizard.basic':'پایه',
        'inbound.wizard.transport':'ترنسپورت',
        'inbound.wizard.security':'امنیت',
        'inbound.wizard.review':'بازبینی',
        'sub.title':'سابسکریپشن',
        'sub.clinic':'کلینیک سابسکریپشن',
        'sub.groups':'گروه‌های سابسکریپشن',
        'sub.noGroups':'هنوز گروهی نیست.',
        'sub.rotateToken':'چرخش توکن',
        'settings.title':'تنظیمات',
        'telegram.title':'تلگرام',
        'telegram.subtitle':'ربات‌ها، فروشگاه و تنظیمات تلگرام',
        'settings.general':'عمومی',
        'settings.saved':'تنظیمات ذخیره شد',
        'settings.saveFail':'ذخیره تنظیمات ناموفق بود',
        'logs.title':'لاگ اخیر Xray',
        'ops.title':'عملیات',
        'finance.title':'مالی',
        'platform.title':'پلتفرم',
        'platform.orders':'سفارش‌ها',
        'platform.certs':'گواهی‌ها',
        'platform.webhooks':'وب‌هوک‌ها',
        'platform.plugins':'افزونه‌ها',
        'platform.backups':'بکاپ‌ها',
        'platform.apiKeys':'کلیدهای API',
      },
      en: {
        'nav.sectionOverview':'Overview',
        'nav.sectionNetwork':'Network & Users',
        'nav.sectionAccount':'Account',
        'nav.sectionControl':'Control Plane',
        'nav.sectionOps':'Operations',
        'nav.sectionSystem':'System',
        'nav.dashboard':'Dashboard',
        'nav.inbounds':'Inbounds',
        'nav.online':'Online Users',
        'nav.subscription':'Subscription',
        'nav.settings':'Panel Settings',
        'nav.telegram':'Telegram',
        'nav.signout':'Sign out',
        'nav.pro':'Pro Center',
        'nav.control':'Control Center',
        'nav.platform':'Platform',
        'nav.finance':'Finance',
        'nav.resellers':'Resellers',
        'nav.fabric':'Node Fabric',
        'nav.ops':'Operations',
        'nav.logs':'Xray Logs',
        'nav.language':'Language',
        'nav.lang.fa':'فارسی',
        'nav.lang.en':'English',
        'common.refresh':'Refresh',
        'common.save':'Save',
        'common.cancel':'Cancel',
        'common.delete':'Delete',
        'common.edit':'Edit',
        'common.create':'Create',
        'common.back':'Back',
        'common.next':'Next',
        'common.search':'Search',
        'common.loading':'Loading…',
        'common.success':'Done',
        'common.error':'Error',
        'xray.running':'Running',
        'xray.stopped':'Stopped',
        'online.title':'Online Users',
        'online.hint':'Based on recent traffic and connected devices (Xray and sing-box); fully idle sessions may not appear.',
        'online.refresh':'Refresh',
        'online.auto':'Auto (5s)',
        'online.count':'Count',
        'online.client':'Client',
        'online.inbound':'Inbound',
        'online.speed':'Speed',
        'online.actions':'Actions',
        'online.kick':'Kick session',
        'online.kickDisable':'Kick + disable',
        'online.devices':'Devices',
        'online.liveDevices':'Live devices',
        'online.empty':'No one online (or no traffic sample yet).',
        'online.kicked':'Session kicked',
        'online.disabled':'Client disabled',
        'online.kickFail':'Kick failed',
        'devices.title':'Devices',
        'devices.active':'Active',
        'devices.revoked':'Revoked',
        'devices.revoke':'Revoke',
        'devices.revokeAll':'Revoke all',
        'devices.revokedOk':'Device revoked',
        'devices.revokedAllOk':'All devices revoked',
        'devices.loadFail':'Could not load devices',
        'lang.changed':'Language updated',
        'dash.title':'Infrastructure Overview',
        'dash.live':'Live Control Center',
        'dash.compact':'Compact view',
        'dash.expanded':'Expanded view',
        'dash.widgets':'Widgets',
        'dash.resetLayout':'Reset layout',
        'dash.nodes':'Nodes',
        'dash.users':'Users',
        'dash.devices':'Devices',
        'dash.traffic':'Traffic',
        'dash.jobs':'Active Jobs',
        'dash.security':'Security',
        'dash.orders':'Orders',
        'dash.disk':'Disk',
        'dash.healthy':'healthy',
        'dash.clients':'clients',
        'dash.registered':'registered',
        'dash.total':'total',
        'dash.runningQueued':'running / queued',
        'dash.auditEvents':'audit events',
        'dash.pending':'pending',
        'dash.used':'used',
        'top.restartXray':'Restart Xray',
        'top.command':'Command',
        'top.stop':'Stop',
        'top.start':'Start',
        'common.close':'Close',
        'common.port':'Port',
        'common.protocol':'Protocol',
        'common.status':'Status',
        'common.clients':'Clients',
        'common.security':'Security',
        'common.traffic':'Traffic',
        'common.download':'Download',
        'common.upload':'Upload',
        'common.domain':'Domain',
        'common.address':'Address',
        'common.name':'Name',
        'common.remark':'Remark',
        'common.expiry':'Expiry',
        'common.expires':'Expires',
        'common.all':'All',
        'common.approve':'Approve',
        'common.restore':'Restore',
        'common.rollback':'Rollback',
        'common.restart':'Restart',
        'common.live':'Live',
        'common.copy':'Copy',
        'common.copyConfig':'Copy config',
        'common.resetTraffic':'Reset traffic',
        'common.presets':'Presets',
        'common.transport':'Transport',
        'common.ipMode':'IP mode',
        'inbound.title':'Inbounds',
        'inbound.new':'New inbound',
        'inbound.clients':'Clients',
        'inbound.edit':'Edit',
        'inbound.enable':'Enable',
        'inbound.disable':'Disable',
        'inbound.wizard.basic':'Basic',
        'inbound.wizard.transport':'Transport',
        'inbound.wizard.security':'Security',
        'inbound.wizard.review':'Review',
        'sub.title':'Subscription',
        'sub.clinic':'Subscription Clinic',
        'sub.groups':'Subscription Groups',
        'sub.noGroups':'No groups yet.',
        'sub.rotateToken':'Rotate Token',
        'settings.title':'Settings',
        'telegram.title':'Telegram',
        'telegram.subtitle':'Bots, shop and Telegram settings',
        'settings.general':'General',
        'settings.saved':'Settings saved',
        'settings.saveFail':'Could not save settings',
        'logs.title':'Recent Xray Log',
        'ops.title':'Operations',
        'finance.title':'Finance',
        'platform.title':'Platform',
        'platform.orders':'Orders',
        'platform.certs':'Certificates',
        'platform.webhooks':'Webhooks',
        'platform.plugins':'Plugins',
        'platform.backups':'Backups',
        'platform.apiKeys':'API keys',
      }
    },
    t(key) {
      const lang = (this.uiLang === 'en' || this.uiLang === 'fa') ? this.uiLang : 'fa';
      const pack = (this.i18n && this.i18n[lang]) || {};
      if (pack[key] != null) return pack[key];
      const fallback = (this.i18n && this.i18n.en) || {};
      return fallback[key] != null ? fallback[key] : key;
    },

    applyUiLanguageOnBoot() {
      let lang = 'fa';
      try {
        const stored = localStorage.getItem('electru_lang');
        if (stored === 'en' || stored === 'fa') lang = stored;
        else if (this.settings && (this.settings.language === 'en' || this.settings.language === 'fa')) lang = this.settings.language;
      } catch (e) {}
      this.uiLang = lang;
      try {
        document.documentElement.setAttribute('lang', lang);
        document.documentElement.setAttribute('dir', lang === 'fa' ? 'rtl' : 'ltr');
        document.body.setAttribute('dir', lang === 'fa' ? 'rtl' : 'ltr');
        document.body.setAttribute('lang', lang);
      } catch (e) {}
    },
    setLanguage(lang) {
      lang = (lang === 'en') ? 'en' : 'fa';
      this.uiLang = lang;
      try { localStorage.setItem('electru_lang', lang); } catch (e) {}
      if (this.settings) this.settings.language = lang;
      try {
        document.documentElement.setAttribute('lang', lang);
        document.documentElement.setAttribute('dir', lang === 'fa' ? 'rtl' : 'ltr');
        document.body.setAttribute('dir', lang === 'fa' ? 'rtl' : 'ltr');
        document.body.setAttribute('lang', lang);
      } catch (e) {}
      this.toast('success', this.t('lang.changed'));
    },
    // Single speed formatter for every Live view (B/s .. TB/s).
    formatSpeed(bps) {
      return this.fmtSpeed(bps);
    },
    
    
    openReplayStudio(cl) {
      const id = typeof cl === 'object' ? cl.id : cl;
      this.goTab('pro');
      this.$nextTick(() => { this.replay.q = String(id); this.replayOpen(id); });
    },
    async replaySearch() {
      const q = (this.replay.q || '').trim();
      if (!q) return;
      this.replay.loading = true;
      const d = await this.api('/pro/replay/search?q=' + encodeURIComponent(q));
      this.replay.loading = false;
      if (d && d.success) this.replay.results = d.obj || [];
      else this.toast('error', (d && d.msg) || 'Search failed');
    },
    async replayOpen(id) {
      if (!id) return;
      this.replay.loading = true;
      const d = await this.api('/pro/replay/client/' + id);
      this.replay.loading = false;
      if (d && d.success) {
        this.replay.client = d.obj.client || null;
        this.replay.events = d.obj.events || [];
        this.replay.audits = d.obj.audits || [];
      } else this.toast('error', (d && d.msg) || 'Could not load timeline');
    },
    async replayAddNote() {
      if (!this.replay.client || !this.replay.note.trim()) return;
      const d = await this.api('/pro/replay/client/' + this.replay.client.id + '/note', {
        method: 'POST', body: JSON.stringify({ summary: this.replay.note.trim() })
      });
      if (d && d.success) {
        this.toast('success', 'Note added');
        this.replay.note = '';
        this.replayOpen(this.replay.client.id);
      } else this.toast('error', (d && d.msg) || 'Note failed');
    },
    replayParseSnap(ev) {
      try { return JSON.parse(ev.snapshotJson || '{}'); } catch (e) { return {}; }
    },

    async loadHoneypot() {
      this.honeypot.loading = true;
      try {
        const d = await this.api('/pro/honeypot/stats');
        if (d && d.success) {
          this.honeypot.stats = d.obj || {};
          this.honeypot.settings = (d.obj && d.obj.settings) || this.honeypot.settings;
        }
      } catch (e) {}
      this.honeypot.loading = false;
    },
    async honeypotDailyReport(sendTg) {
      const q = sendTg ? '?telegram=1' : '';
      const d = await this.api('/pro/honeypot/daily-report' + q);
      if (d && d.success) {
        this.honeypot.report = d.obj || null;
        this.toast('success', sendTg ? 'گزارش به تلگرام ارسال شد' : 'گزارش آماده است');
      } else this.toast('error', (d && d.msg) || 'گزارش ناموفق');
    },
    async honeypotUnban(ip) {
      if (!ip) return;
      const d = await this.api('/pro/honeypot/unban/' + encodeURIComponent(ip), { method: 'POST' });
      if (d && d.success) { this.toast('success', 'Unban: ' + ip); this.loadHoneypot(); }
      else this.toast('error', (d && d.msg) || 'Unban failed');
    },
    async honeypotManualBan() {
      const ip = (this.honeypot.banIP || '').trim();
      if (!ip) return this.toast('error', 'IP required');
      const d = await this.api('/pro/honeypot/ban', { method: 'POST', body: JSON.stringify({ ip, hours: Number(this.honeypot.banHours)||24, reason: 'manual-panel' }) });
      if (d && d.success) { this.toast('success', 'Banned ' + ip); this.honeypot.banIP=''; this.loadHoneypot(); }
      else this.toast('error', (d && d.msg) || 'Ban failed');
    },
    async saveHoneypotSettings(patch) {
      const d = await this.api('/pro/honeypot/settings', { method: 'POST', body: JSON.stringify(patch || this.honeypot.settings || {}) });
      if (d && d.success) { this.toast('success', 'Honeypot settings saved'); this.honeypot.settings = d.obj || this.honeypot.settings; }
      else this.toast('error', (d && d.msg) || 'ذخیره ناموفق');
    },

    async loadOnlineUsers() {
      if (this._onlineLoading) return;
      this._onlineLoading = true;
      try {
        const d = await this.api('/online-users');
        if (d && d.success) this.onlineUsers = d.obj || { users: [], count: 0 };
      } catch (e) {
      } finally {
        this._onlineLoading = false;
      }
    },
    onlineAutoChanged() {
      clearInterval(this.onlineTimer);
      this.onlineTimer = null;
      if (this.onlineAuto) {
        this.loadOnlineUsers();
        this.onlineTimer = setInterval(() => { if (this.polling && this.tab === 'online') this.loadOnlineUsers(); }, 5000);
      }
    },
    async kickClient(u, disable) {
      if (!u || !u.id) return;
      const q = disable ? '?disable=1' : '';
      const d = await this.api('/clients/' + u.id + '/kick' + q, { method: 'POST' });
      if (d && d.success && d.obj && d.obj.kicked) {
        this.toast('success', disable ? this.t('online.disabled') : this.t('online.kicked'));
        this.loadOnlineUsers();
      } else {
        this.toast('error', (d && d.obj && d.obj.msg) || (d && d.msg) || this.t('online.kickFail'));
      }
    },
    async openClientDevices(u) {
      if (!u || !u.id) return;
      const d = await this.api('/clients/' + u.id + '/devices');
      if (d && d.success) {
        this.clientDevices = {
          show: true,
          clientId: u.id,
          email: u.email || (d.obj && d.obj.email) || '',
          limitIp: (d.obj && d.obj.limitIp) || u.limitIp || 0,
          list: (d.obj && d.obj.devices) || []
        };
      } else {
        this.toast('error', (d && d.msg) || this.t('devices.loadFail'));
      }
    },
    async revokeDevice(dev) {
      if (!dev || !this.clientDevices.clientId) return;
      const d = await this.api('/clients/' + this.clientDevices.clientId + '/devices/' + dev.id + '/revoke', { method: 'POST' });
      if (d && d.success) {
        this.toast('success', this.t('devices.revokedOk'));
        this.openClientDevices({ id: this.clientDevices.clientId, email: this.clientDevices.email });
      } else this.toast('error', (d && d.msg) || this.t('common.error'));
    },
    async revokeAllDevices() {
      if (!this.clientDevices.clientId) return;
      const d = await this.api('/clients/' + this.clientDevices.clientId + '/devices/revoke-all', { method: 'POST' });
      if (d && d.success) {
        this.toast('success', this.t('devices.revokedAllOk'));
        this.openClientDevices({ id: this.clientDevices.clientId, email: this.clientDevices.email });
      } else this.toast('error', (d && d.msg) || this.t('common.error'));
    },

    loadTab(tab) {
      if (typeof this._tickLiveClock === 'function') this.$nextTick(() => this._tickLiveClock());
      if (tab === 'dashboard') this.$nextTick(() => this.ensureNetChart());
      switch(tab) {
        case 'dashboard': this.refreshOverview(); this.loadDashboardPro(true); break;
        case 'inbounds': this.loadInbounds(); break;
        case 'online': if (this.onlineAuto) this.onlineAutoChanged(); else this.loadOnlineUsers(); break;
        case 'finance': this.loadFinance(); break;
        case 'subscription':
          this.loadSubscriptionDashboard(true);
          try { this.loadTickets(); } catch (e) {}
          break;
        case 'pro': this.loadPro(); this.loadHoneypot(); break;
        case 'control':
          this.loadUltra();
          try { this.loadShop(); } catch (e) {}
          break;
        case 'platform': this.loadPlatform(); break;
        case 'resellers': this.loadResellers(); break;
        case 'fabric': this.loadFabric(); break;
        case 'ops': this.loadOps(); break;
        case 'logs': this.logsFollow = true; this.loadLogs().then(() => this.scrollLogsToEnd()); break;
        case 'telegram':
          try { this.loadTelegramConfig(); } catch (e) {}
          try { this.loadShop(); } catch (e) {}
          break;
        case 'settings':
          this.loadMe();
          this.loadPanelSettings();
          this.loadSettingsCenter();
          this.loadDNSProvider();
          this.loadPublicHostStatus();
          break;
      }
    },

    // ---------- toast + confirm (replace native alert/confirm) ----------
    toggleActionMenu(type, id) {
      if (this.actionMenu.type === type && this.actionMenu.id === id) this.actionMenu = { type: '', id: null };
      else this.actionMenu = { type, id };
    },
    closeActionMenu() { this.actionMenu = { type: '', id: null }; },

    toast(type, text) {
      const id = ++this.toastSeq;
      const icon = type === 'success' ? '✓' : type === 'error' ? '✕' : 'ℹ';
      this.toasts.push({ id, type, text, icon });
      setTimeout(() => { this.toasts = this.toasts.filter(t => t.id !== id); }, 4000);
    },
    confirmAction(title, message, action, cancelAction = null, confirmLabel = 'Confirm', cancelLabel = 'Cancel') {
      this.confirmModal = { show: true, title, message, action, cancelAction, confirmLabel, cancelLabel };
    },
    cancelConfirmed() {
      const action = this.confirmModal.cancelAction;
      this.confirmModal.show = false;
      this.confirmModal.cancelAction = null;
      if (action) action();
    },
    runConfirmed() {
      const action = this.confirmModal.action;
      this.confirmModal.show = false;
      this.confirmModal.action = null;
      if (action) action();
    },

    // ---------- helpers ----------
    fmtSpeed(n) {
      n = Number(n) || 0;
      if (n < 1024) return n.toFixed(0) + ' B/s';
      const units = ['KB/s', 'MB/s', 'GB/s', 'TB/s'];
      let v = n / 1024, i = 0;
      while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
      return v.toFixed(v < 10 ? 2 : 1) + ' ' + units[i];
    },
    fmtBytes(n) {
      n = Number(n) || 0;
      const units = ['B', 'KB', 'MB', 'GB', 'TB'];
      let i = 0;
      while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
      return n.toFixed(i === 0 ? 0 : 1) + ' ' + units[i];
    },
    fmtUptime(sec) {
      sec = Number(sec) || 0;
      const d = Math.floor(sec / 86400);
      const h = Math.floor((sec % 86400) / 3600);
      const m = Math.floor((sec % 3600) / 60);
      if (d > 0) return `${d}d ${h}h`;
      if (h > 0) return `${h}h ${m}m`;
      return `${m}m`;
    },
    fmtGB(n) {
      n = Number(n) || 0;
      const gb = n / (1024 * 1024 * 1024);
      if (gb < 10) return gb.toFixed(2);
      if (gb < 100) return gb.toFixed(1);
      return gb.toFixed(0);
    },
    cpuPct() {
      return Math.min(100, Math.max(0, Number((this.overview || {}).cpuPercent) || 0));
    },
    resourceBarClass(pct) {
      pct = Number(pct) || 0;
      if (pct >= 90) return 'res-bar high';
      if (pct >= 70) return 'res-bar mid';
      return 'res-bar ok';
    },
    gaugeStyle(pct) {
      pct = Math.min(100, Math.max(0, Number(pct) || 0));
      let c = '#34d399'; // green
      if (pct >= 90) c = '#fb7185';
      else if (pct >= 70) c = '#fbbf24';
      else if (pct >= 40) c = '#22d3ee';
      return { '--p': pct, '--c': c };
    },
    onlineGaugePct() {
      const online = this.onlineNow();
      const total = Math.max(1, Number(this.overview.clientCount || 0));
      return Math.min(100, (online / total) * 100);
    },
    clientGaugePct() {
      const n = Number(this.overview.clientCount || 0);
      // soft scale so small counts still show arc
      return Math.min(100, n === 0 ? 0 : Math.max(8, Math.min(100, n * 2)));
    },
    deviceGaugePct() {
      const n = Number((this.platform && this.platform.security && this.platform.security.devices) || 0);
      return Math.min(100, n === 0 ? 0 : Math.max(8, Math.min(100, n * 3)));
    },
    nodeHealthPct() {
      const t = Number(this.dashboard.nodesTotal || 0);
      if (t <= 0) return 0;
      return Math.min(100, (Number(this.dashboard.nodesHealthy || 0) / t) * 100);
    },
    memPct() {
      if (!this.overview.memTotalBytes) return 0;
      return Math.min(100, (this.overview.memUsedBytes / this.overview.memTotalBytes) * 100);
    },
    diskPct() {
      if (!this.overview.diskTotalBytes) return 0;
      return Math.min(100, (this.overview.diskUsedBytes / this.overview.diskTotalBytes) * 100);
    },
    async api(path, opts = {}) {
      try {
        const res = await fetch((window.__ELECTRU_BASE_PATH__ || '') + '/api' + path, {
          credentials: 'same-origin',
          headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) },
          ...opts,
        });
        if (res.status === 401) { window.location.replace((window.__ELECTRU_BASE_PATH__ || '') + '/login'); return null; }
        const text = await res.text();
        try { return text ? JSON.parse(text) : { success: res.ok }; }
        catch (_) { return { success: false, msg: text || `HTTP ${res.status}` }; }
      } catch (err) {
        this.toast('error', 'Could not reach the server');
        return null;
      }
    },

    // ---------- dashboard ----------
    async checkPanelUpdate() {
      this.panelUpdate.checking = true;
      this.panelUpdate.message = '';
      try {
        const d = await this.api('/panel/update/check');
        if (d && d.obj) {
          this.panelUpdate.localVersion = d.obj.localVersion || '';
          this.panelUpdate.remoteVersion = d.obj.remoteVersion || '';
          this.panelUpdate.updateAvailable = !!d.obj.updateAvailable;
        }
        this.panelUpdate.message = (d && d.msg) || '';
        if (d && d.success) {
          this.toast(this.panelUpdate.updateAvailable ? 'success' : 'success', this.panelUpdate.message);
        } else {
          this.toast('error', this.panelUpdate.message || 'بررسی ناموفق');
        }
      } catch (e) {
        this.panelUpdate.message = 'خطا در اتصال';
        this.toast('error', 'بررسی بروزرسانی ناموفق');
      } finally {
        this.panelUpdate.checking = false;
      }
    },
    async applyPanelUpdate() {
      if (!this.panelUpdate.updateAvailable) return;
      if (!confirm('نسخه جدید از گیت‌هاب نصب شود؟ پنل چند دقیقه قطع می‌شود.')) return;
      this.panelUpdate.applying = true;
      this.panelUpdate.message = 'بروزرسانی شروع شد…';
      try {
        const d = await this.api('/panel/update/apply', { method: 'POST', body: '{}' });
        this.panelUpdate.message = (d && d.msg) || '';
        if (d && d.success) {
          this.toast('success', this.panelUpdate.message);
          if (d.obj && d.obj.started) {
            this.panelUpdate.updateAvailable = false;
            setTimeout(() => { try { location.reload(); } catch (_) {} }, 25000);
          }
        } else {
          this.toast('error', this.panelUpdate.message || 'نصب ناموفق');
        }
      } catch (e) {
        // Panel may restart mid-request — treat as likely success.
        this.panelUpdate.message = 'پنل در حال بروزرسانی است؛ چند ثانیه صبر کنید و صفحه را رفرش کنید.';
        this.toast('success', this.panelUpdate.message);
        setTimeout(() => { try { location.reload(); } catch (_) {} }, 30000);
      } finally {
        this.panelUpdate.applying = false;
      }
    },
    async refreshOverview() {
      // Prevent overlapping overview requests when startup, visibility changes,
      // and a timer tick happen close together. This endpoint aggregates system
      // metrics and DB totals, so duplicate in-flight calls have no value.
      if (this._overviewLoading || !this.polling) return;
      this._overviewLoading = true;
      try {
        const data = await this.api('/dashboard/overview');
        if (this.polling && data && data.success && data.obj) {
          this.overview = data.obj;
          this.pushNetPoint(data.obj.netUpSpeed, data.obj.netDownSpeed);
        }
      } finally {
        this._overviewLoading = false;
      }
    },
    widgetOn(key) { const w=this.dashboardWidgets.find(x=>x.key===key); return !w || w.enabled; },
    loadDashboardWidgets() { try { const v=JSON.parse(localStorage.getItem('electru.dashboard.widgets')||'null'); if(Array.isArray(v)) this.dashboardWidgets=v; } catch(_){} },
    saveDashboardWidgets() { localStorage.setItem('electru.dashboard.widgets', JSON.stringify(this.dashboardWidgets)); },
    resetDashboardWidgets() { this.dashboardWidgets.forEach(w=>w.enabled=true); this.saveDashboardWidgets(); },
    openCommandPalette() { this.commandPalette=true; this.commandQuery=''; this.$nextTick(()=>this.$refs.commandInput&&this.$refs.commandInput.focus()); },
    filteredCommands() { const q=(this.commandQuery||'').toLowerCase(); return this.commands().filter(c=>!q || c.label.toLowerCase().includes(q) || c.hint.toLowerCase().includes(q)).slice(0,10); },
    commands() { return [
      {id:'dashboard',icon:'📊',label:'Open Dashboard',hint:'dashboard',run:()=>this.goTab('dashboard')},
      {id:'fabric',icon:'🌐',label:'Open Node Fabric',hint:'nodes / routing',run:()=>this.goTab('fabric')},
      {id:'users',icon:'👥',label:'Open Users',hint:'clients',run:()=>this.goTab('inbounds')},
      {id:'subscription',icon:'🔗',label:'Open Subscription',hint:'clients / links / traffic / devices',run:()=>this.goTab('subscription')},
      {id:'ops',icon:'🛠️',label:'Open Operations Center',hint:'health / diagnostics',run:()=>this.goTab('ops')},
      {id:'refresh',icon:'↻',label:'Refresh Dashboard',hint:'live data',run:()=>this.loadDashboardPro(true)},
      {id:'restart',icon:'⚡',label:'Restart Xray',hint:'service action',run:()=>this.xrayAction('restart')},
      {id:'backup',icon:'💾',label:'Open Platform Center',hint:'backup / jobs',run:()=>this.goTab('platform')},
      {id:'bbr',icon:'🚀',label:'Open Network Optimizer',hint:'BBR / qdisc',run:()=>this.goTab('fabric')}
    ]; },
    runCommand(c) { const raw=(this.commandQuery||'').trim(); const x=c||this.filteredCommands()[0]; if(!x){ if(raw.length>=2){ this.enterpriseSearch(); } return; } this.commandPalette=false; if(x.run)x.run(); },
    async loadDashboardPro(force=false) {
      // Even an explicit refresh must not start a second six-request batch
      // while the previous dashboard batch is still in flight.
      if (this._dashLoading) return; this._dashLoading=true;
      try {
        // Online sessions have their own tab/modal polling. Fetching them here
        // duplicated Xray/API work even when the dashboard was the only visible tab.
        const calls=[this.api('/pro/control/performance'),this.api('/pro/control/advisor'),this.api('/pro/platform/summary'),this.api('/pro/platform/security'),this.api('/pro/ops/health'),this.api('/pro/ops/alerts')];
        const [perf,adv,sum,sec,health]=await Promise.all(calls);
        const rows=(perf&&perf.success&&Array.isArray(perf.obj))?perf.obj:[];
        this.fabric.performance=rows; this.fabric.advisor=(adv&&adv.success&&Array.isArray(adv.obj))?adv.obj:[];
        if(sum&&sum.success)this.platform.summary=sum.obj||{}; if(sec&&sec.success)this.platform.security=sec.obj||{};
        this.dashboard.nodeRows=rows.map(x=>({id:x.node?.id||x.agent?.nodeId,name:x.node?.name||x.node?.address||'Node',healthy:!!x.healthy,rtt:Number(x.performance?.rttMs||0),packetLoss:Number(x.performance?.packetLoss||0),congestion:x.performance?.congestion||'',cpu:Number(x.agent?.cpuPercent||0),load:Number(x.agent?.cpuPercent||0),traffic:Number(x.performance?.rxBytes||0)+Number(x.performance?.txBytes||0),rx:Number(x.performance?.rxBytes||0),tx:Number(x.performance?.txBytes||0)}));
        this.dashboard.nodesTotal=rows.length; this.dashboard.nodesHealthy=rows.filter(x=>x.healthy).length;
        const counts={}; (this.inbounds||[]).forEach(i=>{const k=(i.protocol||'unknown').toUpperCase();counts[k]=(counts[k]||0)+1;}); const total=Object.values(counts).reduce((a,b)=>a+b,0); this.dashboard.protocolTotal=total; this.dashboard.protocols=Object.entries(counts).map(([name,count])=>({name,count,pct:total?Math.round(count/total*100):0})).sort((a,b)=>b.count-a.count);
        const alerts=[]; const serverAlerts=(calls[5]&&calls[5].success&&calls[5].obj&&Array.isArray(calls[5].obj.alerts))?calls[5].obj.alerts:[]; serverAlerts.forEach((a,i)=>alerts.push({id:'server-'+i,icon:a.severity==='critical'?'🔴':'🟡',title:a.type||'Alert',detail:a.message||''})); this.dashboard.nodeRows.filter(n=>!n.healthy).forEach(n=>alerts.push({id:'node-'+n.id,icon:'🔴',title:n.name+' offline',detail:'Agent heartbeat or health check failed'})); if(Number(this.overview.cpuPercent||0)>85)alerts.push({id:'cpu',icon:'🟡',title:'High panel CPU',detail:Number(this.overview.cpuPercent).toFixed(0)+'% utilization'}); if(this.diskPct()>85)alerts.push({id:'disk',icon:'🟡',title:'Disk usage high',detail:this.diskPct().toFixed(0)+'% used'}); if(sec&&sec.success&&sec.obj&&sec.obj.weakJwtSecret)alerts.push({id:'jwt',icon:'🔐',title:'Weak JWT secret',detail:'Change the production secret'}); this.dashboard.alerts=alerts;
        this.dashboardLastUpdate=new Date().toLocaleTimeString();
        if(health&&health.success)this.ops.health=health.obj||{};
      } finally { this._dashLoading=false; }
    },
    async syncTraffic() {
      await this.api('/dashboard/sync-traffic', { method: 'POST' });
      if (this.tab === 'inbounds') this.loadInbounds();
    },
    async resetAllTraffic() {
      const data = await this.api('/dashboard/reset-all-traffic', { method: 'POST' });
      if (data && data.success) { this.toast('success', 'All traffic counters reset'); this.loadInbounds(); }
      else this.toast('error', 'Could not reset traffic');
    },
    async xrayAction(action) {
      const data = await this.api('/dashboard/xray/' + action, { method: 'POST' });
      this.refreshOverview();
      if (data && data.success) this.toast('success', 'Xray ' + action + ' ok');
      else this.toast('error', (data && data.msg) || 'Xray action failed');
    },
    async loadLogs() {
      if (this._logsLoading) return;
      this._logsLoading = true;
      try {
        const data = await this.api('/dashboard/xray-logs');
        if (data && data.success) {
          const next = Array.isArray(data.obj) ? data.obj : [];
          // Skip the re-render (and the scroll jump) when nothing changed.
          if (next.length !== this.logs.length || next[next.length - 1] !== this.logs[this.logs.length - 1]) {
            this.logs = next;
            if (this.logsFollow) this.scrollLogsToEnd();
          }
        }
      } finally {
        this._logsLoading = false;
      }
    },
    scrollLogsToEnd() {
      this.$nextTick(() => {
        const box = this.$refs && this.$refs.logBox;
        if (box) box.scrollTop = box.scrollHeight;
      });
    },
    // The Chart.js instance is kept outside Alpine's reactive state: proxying a
    // Chart object is slow and can break its internal bookkeeping. The dashboard
    // lives inside an x-if template, so the canvas is re-created every time the
    // tab is opened; ensureNetChart() rebinds the chart to the current canvas.
    ensureNetChart() {
      const el = document.getElementById('netChart');
      let chart = window.__electruNetChart;
      if (!el || !window.Chart) return null;
      if (chart && chart.canvas === el) return chart;
      if (chart) { try { chart.destroy(); } catch (_) {} window.__electruNetChart = null; }
      chart = this.setupChart();
      return chart;
    },
    setupChart() {
      const el = document.getElementById('netChart');
      if (!el || !window.Chart) return null;
      const fmt = (v) => this.fmtBytes(Number(v) || 0) + '/s';
      const chart = new Chart(el, {
        type: 'line',
        data: {
          labels: this.netHistory.labels.slice(),
          datasets: [
            { label: 'Upload', data: this.netHistory.up.slice(), borderColor: '#ff3347', backgroundColor: 'rgba(255,51,71,0.16)', tension: 0.35, fill: true, pointRadius: 0 },
            { label: 'Download', data: this.netHistory.down.slice(), borderColor: '#ff9a8f', backgroundColor: 'rgba(255,154,143,0.10)', tension: 0.35, fill: true, pointRadius: 0 },
          ],
        },
        options: {
          responsive: true,
          animation: { duration: 300 },
          plugins: {
            legend: { labels: { color: '#8b93a7', boxWidth: 10 } },
            tooltip: { callbacks: { label: (ctx) => ctx.dataset.label + ': ' + fmt(ctx.parsed.y) } },
          },
          scales: {
            x: { ticks: { color: '#8b93a7', maxTicksLimit: 6 }, grid: { color: 'rgba(127,127,127,0.08)' } },
            y: { beginAtZero: true, ticks: { color: '#8b93a7', callback: (v) => fmt(v) }, grid: { color: 'rgba(127,127,127,0.08)' } },
          },
        },
      });
      window.__electruNetChart = chart;
      return chart;
    },
    // up/down are host network speeds in bytes per second.
    pushNetPoint(up, down) {
      const t = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
      this.netHistory.labels.push(t);
      this.netHistory.up.push(Math.max(0, Number(up) || 0));
      this.netHistory.down.push(Math.max(0, Number(down) || 0));
      while (this.netHistory.labels.length > 30) {
        this.netHistory.labels.shift();
        this.netHistory.up.shift();
        this.netHistory.down.shift();
      }
      const chart = this.ensureNetChart();
      if (chart) {
        chart.data.labels = this.netHistory.labels.slice();
        chart.data.datasets[0].data = this.netHistory.up.slice();
        chart.data.datasets[1].data = this.netHistory.down.slice();
        chart.update('none');
      }
    },
    // Online users shown on the dashboard come from the live overview poll.
    onlineNow() {
      const fromOverview = this.overview && this.overview.onlineCount;
      if (fromOverview != null) return Number(fromOverview) || 0;
      return Number(this.onlineUsers && this.onlineUsers.count) || 0;
    },

    // ---------- inbounds ----------
    
    async applyAntiDpi(ib, profile) {
      if (!ib?.id) return;
      const p = profile || 'balanced';
      if (!confirm('اعمال پروفایل Anti-DPI «' + p + '» روی «' + (ib.remark||ib.id) + '»؟\nکلید Reality حفظ می‌شود.')) return;
      const d = await this.api('/inbounds/' + ib.id + '/anti-dpi', { method: 'POST', body: JSON.stringify({ profile: p }) });
      if (d && d.success) {
        let msg = 'Anti-DPI ' + p + ' اعمال شد';
        if (d.obj && d.obj.runtimeApplied === false) msg += ' — runtime: ' + (d.obj.runtimeError || 'fail');
        this.toast(d.obj && d.obj.runtimeApplied === false ? 'error' : 'success', msg);
        await this.loadInbounds();
      } else this.toast('error', (d && d.msg) || 'اعمال Anti-DPI ناموفق');
    },
    async applyAntiDpiBulk(profile) {
      const ids = this.selectedInboundIds || [];
      if (!ids.length) return this.toast('error', 'ابتدا اینباندها را انتخاب کنید');
      const p = profile || 'balanced';
      if (!confirm('اعمال «' + p + '» روی ' + ids.length + ' اینباند؟')) return;
      const d = await this.api('/inbounds/anti-dpi/bulk', { method: 'POST', body: JSON.stringify({ profile: p, ids }) });
      if (d && d.success) {
        const o = d.obj || {};
        let msg = 'به‌روز شد: ' + (o.updated||0);
        if (o.failed && o.failed.length) msg += ' · خطا: ' + o.failed.length;
        if (o.runtimeApplied === false) msg += ' · runtime fail';
        this.toast(o.runtimeApplied === false ? 'error' : 'success', msg);
        await this.loadInbounds();
      } else this.toast('error', (d && d.msg) || 'bulk ناموفق');
    },
    async loadInbounds() {
      if (this.inboundSearchActive && this.inboundSearch.trim()) return this.searchInbounds(false);
      const data = await this.api('/inbounds?page='+this.inboundPage+'&pageSize='+this.inboundPageSize);
      if (data && data.success) {
        this.inbounds = data.obj || [];
        this.inboundTotal = Number(data.total || 0);
        this.inboundHasMore = !!data.hasMore;
        this.selectedInboundIds = this.selectedInboundIds.filter(id => this.inbounds.some(x => x.id === id));
        if (this.expandedInboundId) { const ib=this.inbounds.find(x=>x.id===this.expandedInboundId); if (ib) this.loadInboundClients(ib, Number(this.inboundClientPages[ib.id]?.page||1)); else this.expandedInboundId=null; }
      }
      this.loadClientGroups();
    },
    async nextInboundPage() { if(!this.inboundHasMore) return; this.inboundPage++; await this.loadInbounds(); },
    async previousInboundPage() { if(this.inboundPage<=1) return; this.inboundPage--; await this.loadInbounds(); },
    async loadInboundClients(ib, page=1) {
      if (!ib?.id || this.inboundClientLoading[ib.id]) return;
      this.inboundClientLoading[ib.id]=true;
      try {
        const d=await this.api(`/inbounds/${ib.id}/clients?page=${page}&pageSize=50`);
        if(d?.success){ ib.clients=d.obj||[]; this.inboundClientPages={...this.inboundClientPages,[ib.id]:{page:Number(d.page||page),pageSize:Number(d.pageSize||50),total:Number(d.total||0),hasMore:!!d.hasMore}}; ib.clientCount=Number(d.total||ib.clientCount||0); this.inbounds=this.inbounds.map(x=>x.id===ib.id?{...x,clients:ib.clients,clientCount:ib.clientCount}:x); }
        else if (d && d.msg) this.toast('error', d.msg);
      } catch (e) {
        this.toast('error', 'بارگذاری کلاینت‌های اینباند ناموفق');
      } finally { this.inboundClientLoading[ib.id]=false; }
    },
    async refreshExpandedInbound() { await this.loadInbounds(); const ib=this.inbounds.find(x=>x.id===this.expandedInboundId); if(ib) await this.loadInboundClients(ib, Number(this.inboundClientPages[ib.id]?.page||1)); },
    async searchInbounds(resetPage=true) {
      const q = this.inboundSearch.trim();
      if (!q) {
        this.inboundSearchActive = false;
        this.inboundPage = 1;
        this.inboundSearchPage = 1;
        this.inboundSearchTotal = 0;
        this.inboundSearchHasMore = false;
        return this.loadInbounds();
      }
      if (resetPage) this.inboundSearchPage = 1;
      this.inboundSearchLoading = true;
      try {
        const params = new URLSearchParams({ q, page: String(this.inboundSearchPage), pageSize: String(this.inboundSearchPageSize) });
        if (this.inboundProtocolFilter !== 'all') params.set('protocol', this.inboundProtocolFilter);
        const data = await this.api('/inbounds/search?' + params.toString());
        if (data && data.success) {
          this.inboundSearchActive = true;
          this.inbounds = data.obj || [];
          this.inboundSearchTotal = Number(data.total || 0);
          this.inboundSearchHasMore = !!data.hasMore;
          this.selectedInboundIds = this.selectedInboundIds.filter(id => this.inbounds.some(x => x.id === id));
        }
      } finally {
        this.inboundSearchLoading = false;
      }
    },
    clearInboundSearch() {
      this.inboundSearch = '';
      this.inboundSearchActive = false;
      this.inboundSearchPage = 1;
      this.inboundSearchTotal = 0;
      this.inboundSearchHasMore = false;
      this.loadInbounds();
    },
    async nextInboundSearchPage() {
      if (!this.inboundSearchHasMore || this.inboundSearchLoading) return;
      this.inboundSearchPage += 1;
      await this.searchInbounds(false);
    },
    async previousInboundSearchPage() {
      if (this.inboundSearchPage <= 1 || this.inboundSearchLoading) return;
      this.inboundSearchPage -= 1;
      await this.searchInbounds(false);
    },
    filteredInbounds() {
      const q = this.inboundSearch.trim().toLowerCase();
      return (this.inbounds || []).filter(ib => {
        const labels = (ib.labels || '').toLowerCase();
        const text = [ib.remark, ib.protocol, ib.network, ib.security, String(ib.port), labels].join(' ').toLowerCase();
        const matchQ = !q || text.includes(q);
        const matchP = this.inboundProtocolFilter === 'all' || ib.protocol === this.inboundProtocolFilter;
        const running = !!(ib.runtime && ib.runtime.running);
        const matchS = this.inboundStatusFilter === 'all' || (this.inboundStatusFilter === 'running' ? running : this.inboundStatusFilter === 'disabled' ? !ib.enable : this.inboundStatusFilter === 'offline' ? (ib.enable && !running) : true);
        return matchQ && matchP && matchS;
      });
    },
    toggleInboundSelection(id) { this.selectedInboundIds = this.selectedInboundIds.includes(id) ? this.selectedInboundIds.filter(x=>x!==id) : [...this.selectedInboundIds,id]; },
    toggleSelectAllInbounds() { const ids=this.filteredInbounds().map(x=>x.id); const all=ids.length && ids.every(id=>this.selectedInboundIds.includes(id)); this.selectedInboundIds=all?this.selectedInboundIds.filter(id=>!ids.includes(id)):[...new Set([...this.selectedInboundIds,...ids])]; },
    openQuickGrpc() { this.quickGrpc={remark:'',clientName:'',totalGB:0,days:30, inboundTotalGB:0, inboundDays:0,port:0,ipMode:'ipv4',addressType:'ipv4',saving:false,result:null}; this.showQuickGrpcModal=true; },
    openQuickTransport(network) { const labels={ws:'WebSocket',httpupgrade:'HTTPUpgrade',xhttp:'XHTTP'}; this.quickTransport={network,label:labels[network]||'XHTTP',remark:'',clientName:'',totalGB:0,days:30, inboundTotalGB:0, inboundDays:0,port:0,ipMode:'ipv4',addressType:'ipv4',saving:false,result:null}; this.showQuickTransportModal=true; },
    quickTransportIpChanged() { const m=this.quickTransport.ipMode; this.quickTransport.addressType=m==='ipv6'?'ipv6':(m==='dual'?'all':'ipv4'); },
    async randomQuickTransportPort() { const q=this.quickTransport; const d=await this.api('/inbounds/port-availability?port=0&strategy=random&network='+encodeURIComponent(q.network)+'&ipMode='+encodeURIComponent(q.ipMode)); if(d?.success){q.port=Number(d.port);this.toast('success','Free random port: '+d.port)}else this.toast('error',d?.msg||'Could not find a free port'); },
    async createQuickTransport() { const q=this.quickTransport; const port=Number(q.port||0), gb=Number(q.totalGB||0), days=Number(q.days||0); if(gb<0||days<0||days>3650||port<0||port>65535)return this.toast('error','Check volume, days and port'); q.saving=true; try { const ep=q.network==='ws'?'/inbounds/quick-websocket':(q.network==='httpupgrade'?'/inbounds/quick-httpupgrade':'/inbounds/quick-xhttp'); const d=await this.api(ep,{method:'POST',body:JSON.stringify({remark:q.remark,clientName:q.clientName,totalGB:gb,days,inboundTotalGB:Number(q.inboundTotalGB||0),inboundDays:Number(q.inboundDays||0),port,ipMode:q.ipMode,addressType:q.addressType})}); if(!d?.success) throw new Error(d?.msg||'Creation failed'); q.result=d.obj; this.toast('success',q.label+' config created'); await this.loadInbounds(); } catch(e){this.toast('error',e.message||'Creation failed')} finally {q.saving=false;} },
    async openQuickVLESSReality(){ this.quickTransport={network:'tcp',label:'VLESS TCP + REALITY Vision',remark:'',clientName:'',totalGB:0,days:30, inboundTotalGB:0, inboundDays:0,port:0,ipMode:'ipv4',addressType:'ipv4',saving:false,result:null}; this.showQuickTransportModal=true; },
    async createQuickVLESSReality(){ const q=this.quickTransport; q.saving=true; try{const d=await this.api('/inbounds/quick-vless-reality',{method:'POST',body:JSON.stringify({remark:q.remark,clientName:q.clientName,totalGB:Number(q.totalGB||0),days:Number(q.days||0),inboundTotalGB:Number(q.inboundTotalGB||0),inboundDays:Number(q.inboundDays||0),port:Number(q.port||0),ipMode:q.ipMode,addressType:q.addressType})}); if(!d?.success)throw new Error(d?.msg||'Creation failed'); q.result=d.obj; this.toast('success','VLESS TCP + REALITY created'); await this.loadInbounds(); }catch(e){this.toast('error',e.message||'Creation failed')}finally{q.saving=false;} },
    async copyQuickVLESSRealityLink(){const l=this.quickTransport.result?.link||'';if(l)await this.copyText(l,'Link copied');},
    async copyQuickTransportLink() { const link=this.quickTransport.result?.link||''; if(!link)return this.toast('error','No public link is available yet'); await this.copyText(link,this.quickTransport.label+' link copied'); },
    quickGrpcIpChanged() { const m=this.quickGrpc.ipMode; this.quickGrpc.addressType=m==='ipv6'?'ipv6':(m==='dual'?'all':'ipv4'); },
    async randomQuickGrpcPort() { const d=await this.api('/inbounds/port-availability?port=0&strategy=random&network=grpc&ipMode='+encodeURIComponent(this.quickGrpc.ipMode)); if(d?.success){this.quickGrpc.port=Number(d.port);this.toast('success','Free random port: '+d.port)}else this.toast('error',d?.msg||'Could not find a free port'); },
    async createQuickGrpc() {
      const q=this.quickGrpc; const port=Number(q.port||0), gb=Number(q.totalGB||0), days=Number(q.days||0);
      if(!Number.isInteger(port)||port<0||port>65535||!Number.isInteger(gb)||gb<0||!Number.isInteger(days)||days<0||days>3650){this.toast('error','Port/volume/days values are invalid');return;}
      q.saving=true;
      try {
        const d=await this.api('/inbounds/quick-grpc',{method:'POST',body:JSON.stringify({remark:q.remark,clientName:q.clientName,totalGB:gb,days,inboundTotalGB:Number(q.inboundTotalGB||0),inboundDays:Number(q.inboundDays||0),port,ipMode:q.ipMode,addressType:q.addressType})});
        if(d?.success){q.result=d.obj||{};this.toast('success','Simple gRPC config created on port '+(d.obj?.inbound?.port||port));this.loadInbounds();}
        else this.toast('error',d?.msg||'Could not create gRPC config');
      } finally { q.saving=false; }
    },
    async copyQuickGrpcLink() { const link=this.quickGrpc.result?.link||''; if(!link)return this.toast('error','No public link is available yet'); await this.copyText(link,'gRPC link copied'); },
    async bulkInbound(action) { if(!this.selectedInboundIds.length)return this.toast('error','Select at least one inbound'); const d=await this.api('/inbounds/bulk',{method:'POST',body:JSON.stringify({ids:this.selectedInboundIds,action})}); if(d?.success){this.selectedInboundIds=[];this.toast('success',`Bulk ${action} completed`);this.loadInbounds()}else this.toast('error',d?.msg||'Bulk action failed'); },
    openMassProvision() { this.massProvision={sourceInboundId:(this.inbounds[0]?.id||0),count:10,namePrefix:'bulk-inbound',startPort:0,includeClients:false,dryRun:false,busy:false,result:null}; this.showMassProvisionModal=true; },
    async runMassProvision(dryRun) { const m=this.massProvision; if(!m.sourceInboundId||!m.namePrefix.trim()) return this.toast('error','Choose a source inbound and name prefix'); m.busy=true; m.result=null; const d=await this.api('/inbounds/mass-provision',{method:'POST',body:JSON.stringify({sourceInboundId:Number(m.sourceInboundId),count:Number(m.count),namePrefix:m.namePrefix.trim(),startPort:Number(m.startPort||0),includeClients:!!m.includeClients,dryRun:!!dryRun})}); m.busy=false; m.result=d; if(d?.success){ this.toast('success',dryRun?'Dry run passed':'Mass provisioning completed'); if(!dryRun){ this.showMassProvisionModal=false; this.loadInbounds(); } } else this.toast('error',d?.msg||'Mass provisioning failed'); },
    async previewInbound(ib) { const d=await this.api('/inbounds/'+ib.id+'/preview'); this.inboundPreview={show:true,title:ib.remark,engine:d?.engine||'',config:d?.config||'',valid:!!d?.valid,msg:d?.msg||''}; },
    async liveInbound(ib) { clearTimeout(this.inboundLiveTimer); const d=await this.api('/inbounds/'+ib.id+'/live'); this.inboundLive={show:true,title:ib.remark,data:d?.obj||{},id:ib.id}; this.inboundLiveTimer=setTimeout(()=>this.refreshInboundLive(ib.id),5000); },
    async refreshInboundLive(id) { if(!this.inboundLive.show || this.inboundLive.id!==id)return; if(!this.polling){this.inboundLiveTimer=setTimeout(()=>this.refreshInboundLive(id),5000);return;} const d=await this.api('/inbounds/'+id+'/live'); if(d?.success)this.inboundLive.data=d.obj||{}; if(this.inboundLive.show && this.inboundLive.id===id)this.inboundLiveTimer=setTimeout(()=>this.refreshInboundLive(id),5000); },
    async resetInboundTraffic(ib) { this.confirmAction('Reset inbound traffic?', 'Only this inbound will be reset. Other inbounds and clients are preserved.', async () => { const d=await this.api(`/inbounds/${ib.id}/reset-traffic`,{method:'POST'}); if(d?.success){this.toast('success','Inbound traffic reset');this.loadInbounds()}else this.toast('error',d?.msg||'Traffic reset failed'); }); },
    async cloneInbound(ib) { this.confirmAction('Clone inbound?', 'Choose whether the cloned configuration should also include fresh client credentials.', async () => { const d=await this.api(`/inbounds/${ib.id}/clone`,{method:'POST',body:JSON.stringify({includeClients:true})}); if(d?.success){this.toast('success','Inbound cloned with clients');this.loadInbounds()}else this.toast('error',d?.msg||'Clone failed'); }, async () => { const d=await this.api(`/inbounds/${ib.id}/clone`,{method:'POST',body:JSON.stringify({includeClients:false})}); if(d?.success){this.toast('success','Inbound configuration cloned');this.loadInbounds()}else this.toast('error',d?.msg||'Clone failed'); }, 'Clone + clients', 'Config only'); },
    async toggleMaintenance(ib) { const body=ib.maintenance?{maintenance:false,enable:true}:{maintenance:true,enable:false}; const d=await this.api(`/inbounds/${ib.id}/update`,{method:'POST',body:JSON.stringify(body)}); if(d?.success){this.toast('success',ib.maintenance?'Maintenance disabled':'Maintenance enabled');this.loadInbounds()}else this.toast('error',d?.msg||'Maintenance update failed'); },
    async openInboundAnalytics(ib) { const [a,h]=await Promise.all([this.api(`/inbounds/${ib.id}/analytics?days=7`),this.api(`/inbounds/${ib.id}/health`)]); this.inboundAnalytics={show:true,title:ib.remark,days:7,points:a?.obj?.points||[],health:h?.obj||null}; },
    async rollbackInbound(ib) { this.confirmAction('Rollback inbound?', 'Restore this inbound to its latest saved revision?', async () => { const d=await this.api('/inbounds/'+ib.id+'/rollback',{method:'POST'}); if(d?.success){this.toast('success','Inbound rolled back');this.loadInbounds()}else this.toast('error',d?.msg||'Rollback failed'); }); },
    exportInbound(ib) { const url=(window.__ELECTRU_BASE_PATH__||'')+'/api/inbounds/'+ib.id+'/export'; window.open(url,'_blank'); },
    async importInboundFile(ev) { const f=ev.target.files?.[0]; if(!f)return; try { const body=await f.text(); const d=await this.api('/inbounds/import',{method:'POST',body}); if(d?.success){this.toast('success','Inbound imported');this.loadInbounds()}else this.toast('error',d?.msg||'Import failed'); } catch(e){this.toast('error','Could not read import file')} ev.target.value=''; },
    async import3XUIFile(ev) { const f=ev.target.files?.[0]; if(!f)return; try { const form=new FormData(); form.append('file',f,f.name); const res=await fetch((window.__ELECTRU_BASE_PATH__||'')+'/api/inbounds/import-3x-ui',{method:'POST',credentials:'same-origin',body:form}); if(res.status===401){window.location.replace((window.__ELECTRU_BASE_PATH__||'')+'/login');return;} const d=await res.json().catch(()=>({success:false,msg:'Server returned an invalid response'})); const obj=d?.obj||{}; if(d?.success){const skipped=Array.isArray(obj.skipped)?obj.skipped.length:0; this.toast('success',`3x-ui import: ${obj.importedInbounds||0} inbounds, ${obj.importedClients||0} clients${skipped?`, ${skipped} skipped`:''}`); this.loadInbounds(); if(skipped){const detail=obj.skipped.slice(0,8).map(x=>`${x.remark||'Inbound'}: ${x.reason}`).join('\n'); window.alert('Some entries were skipped:\n'+detail+(skipped>8?`\n...and ${skipped-8} more`:''));}}else{this.toast('error',d?.msg||'3x-ui import failed'); if(obj.skipped?.length){window.alert('Import details:\n'+obj.skipped.slice(0,10).map(x=>`${x.remark||'Inbound'}: ${x.reason}`).join('\n'));}} }catch(e){this.toast('error','Could not upload 3x-ui backup')}finally{ev.target.value='';} },
    applyInboundTemplate(id) {
      const t=this.inboundTemplates.find(x=>String(x.id)===String(id));
      if(!t) return;
      this.newInbound.protocol=t.protocol; this.newInbound.network=t.network; this.newInbound.security=t.security; this.newInbound.labels=t.labels||'';
      try {
        const ss=typeof t.streamSettings==='string'?JSON.parse(t.streamSettings):t.streamSettings||{};
        const st=typeof t.settings==='string'?JSON.parse(t.settings):t.settings||{};
        Object.assign(this.newInbound,{wsPath:ss.wsPath||'/ws',wsHost:ss.wsHost||'',grpcService:ss.grpcServiceName||'electruvpn',xhttpPath:ss.xhttpPath||'/xhttp',xhttpHost:ss.xhttpHost||'',xhttpMode:ss.xhttpMode||'auto',httpUpgradePath:ss.httpUpgradePath||'/',httpUpgradeHost:ss.httpUpgradeHost||'',acceptProxyProtocol:!!ss.acceptProxyProtocol,certFile:ss.tlsSettings?.certFile||'',keyFile:ss.tlsSettings?.keyFile||'',sni:ss.tlsSettings?.sni||'',dest:ss.realitySettings?.dest||'www.microsoft.com:443',serverName:(Array.isArray(ss.realitySettings?.serverNames)?ss.realitySettings.serverNames.join(','):(ss.realitySettings?.serverNames||''))||'',privateKey:ss.realitySettings?.privateKey||'',publicKey:ss.realitySettings?.publicKey||'',shortId:(Array.isArray(ss.realitySettings?.shortIds)?ss.realitySettings.shortIds.join(','):(ss.realitySettings?.shortIds||''))||'',ssMethod:st.method||'2022-blake3-aes-256-gcm',upMbps:(st.upMbps!=null?st.upMbps:0),downMbps:(st.downMbps!=null?st.downMbps:0),obfsType:st.obfsType||'salamander',obfsPassword:st.obfsPassword||'',congestionControl:st.congestionControl||'bbr',wgPrivateKey:st.privateKey||'',wgPublicKey:st.publicKey||'',wgAddress:st.address||'10.77.1.1/24',wgMtu:st.mtu||1420});
      } catch (_) {}
      this.protocolChanged(); this.toast('success','Template applied');
    },
    saveInboundTemplate(name) {
      name=String(name||'').trim(); if(!name) return false;
      const {streamSettings,settings}=this.buildInboundPayload();
      const item={id:Date.now(),name,protocol:this.newInbound.protocol,network:this.newInbound.network,security:this.newInbound.security,streamSettings,settings,labels:this.newInbound.labels||''};
      this.inboundTemplates=[item,...this.inboundTemplates.filter(x=>x.name.toLowerCase()!==name.toLowerCase())].slice(0,30);
      localStorage.setItem('electruvpn.inboundTemplates',JSON.stringify(this.inboundTemplates));
      this.toast('success','Template saved locally'); return true;
    },
    
    
    
    async loadShop() {
      try {
        await Promise.all([this.loadShopPlans(), this.loadShopOrders(), this.loadShopSettings(), this.loadTrialSettings(), this.loadWinbackSettings(), this.loadCustomGB()]);
      } catch (e) {
        console.error('loadShop', e);
        this.toast('error', 'بارگذاری فروشگاه ناموفق بود');
      }
    },
    async loadFinance() {
      if (!this.finance) this.finance = { from:'', to:'', loading:false, summary:null, planSales:[], monthly:[], churn:null, transactions:[], chartMonthly:null, chartPlans:null };
      if (!this.finance.from) {
        const to = new Date();
        const from = new Date(); from.setDate(from.getDate() - 30);
        this.finance.from = from.toISOString().slice(0,10);
        this.finance.to = to.toISOString().slice(0,10);
      }
      this.finance.loading = true;
      try {
        const q = `from=${encodeURIComponent(this.finance.from)}&to=${encodeURIComponent(this.finance.to)}`;
        const safe = async (path) => { try { return await this.api(path); } catch(e) { return { success:false }; } };
        const [sum, plans, monthly, churn, tx] = await Promise.all([
          safe('/pro/finance/summary'),
          safe('/pro/finance/plan-sales?' + q),
          safe('/pro/finance/monthly-trend'),
          safe('/pro/finance/churn?days=30'),
          safe('/pro/finance/transactions?' + q + '&status=all&limit=100')
        ]);
        if (sum?.success) this.finance.summary = sum.obj;
        if (plans?.success) this.finance.planSales = plans.obj || [];
        if (monthly?.success) this.finance.monthly = monthly.obj || [];
        if (churn?.success) this.finance.churn = churn.obj;
        if (tx?.success) this.finance.transactions = tx.obj || [];
        this.$nextTick(() => this.renderFinanceCharts());
      } catch (e) {
        console.error('loadFinance', e);
        try { this.toast('error', 'بارگذاری مالی ناموفق بود'); } catch(_){}
      } finally { this.finance.loading = false; }
    },
    formatToman(n) {
      const v = Number(n||0);
      return v.toLocaleString('fa-IR') + ' تومان';
    },
    renderFinanceCharts() {
      if (typeof Chart === 'undefined') return;
      const monthly = this.finance.monthly || [];
      const plans = this.finance.planSales || [];
      const mEl = document.getElementById('financeMonthlyChart');
      const pEl = document.getElementById('financePlansChart');
      // Chart.js instances must live outside Alpine's reactive proxy (a proxied chart throws
      // "reading 'save'" of null after destroy/re-create), so keep them on window.
      const C = (window.__electruCharts = window.__electruCharts || {});
      if (mEl) {
        if (C.fm) { try { C.fm.destroy(); } catch (_) {} C.fm = null; }
        C.fm = new Chart(mEl, {
          type: 'bar',
          data: {
            labels: monthly.map(x => x.month),
            datasets: [{ label: 'درآمد (تومان)', data: monthly.map(x => x.revenue), backgroundColor: 'rgba(56,189,248,0.55)', borderRadius: 6 }]
          },
          options: { responsive: true, plugins: { legend: { display: false } }, scales: { x: { ticks: { color: '#94a3b8' } }, y: { ticks: { color: '#94a3b8' } } } }
        });
      }
      if (!plans.length && C.fp) { try { C.fp.destroy(); } catch (_) {} C.fp = null; }
      if (pEl && plans.length) {
        if (C.fp) { try { C.fp.destroy(); } catch (_) {} C.fp = null; }
        C.fp = new Chart(pEl, {
          type: 'doughnut',
          data: {
            labels: plans.map(x => x.planName || '—'),
            datasets: [{ data: plans.map(x => x.revenue), backgroundColor: ['#38bdf8','#a78bfa','#34d399','#fbbf24','#f472b6','#fb7185','#67e8f9','#c4b5fd'] }]
          },
          options: { responsive: true, plugins: { legend: { position: 'bottom', labels: { color: '#cbd5e1', boxWidth: 12 } } } }
        });
      }
    },
    financeExportCSV() {
      const q = `from=${encodeURIComponent(this.finance.from)}&to=${encodeURIComponent(this.finance.to)}&status=approved`;
      const base = (window.__ELECTRU_BASE_PATH__ || '') + '/api/pro/finance/export.csv?' + q;
      window.open(base, '_blank');
    },
    financeExportPDF() {
      const q = `from=${encodeURIComponent(this.finance.from)}&to=${encodeURIComponent(this.finance.to)}`;
      const base = (window.__ELECTRU_BASE_PATH__ || '') + '/api/pro/finance/export-report?' + q;
      window.open(base, '_blank');
    },

    async loadShopPlans() {
      const d = await this.api('/pro/shop/plans');
      if (d && d.success) this.shop.plans = d.obj || [];
    },
    async loadShopOrders() {
      const d = await this.api('/pro/shop/orders?status=' + encodeURIComponent(this.shop.orderFilter || 'pending_review'));
      if (d && d.success) this.shop.orders = d.obj || [];
    },
    async loadShopSettings() {
      const d = await this.api('/pro/shop/settings');
      if (d && d.success && d.obj) {
        this.shop.settings.cardNumber = d.obj.cardNumber || '';
        this.shop.settings.cardHolder = d.obj.cardHolder || '';
        this.shop.settings.instructions = d.obj.instructions || '';
        this.shop.settings.enabled = d.obj.enabled !== false;
      }
    },
    async saveShopSettings() {
      const d = await this.api('/pro/shop/settings', { method: 'POST', body: JSON.stringify(this.shop.settings) });
      if (d && d.success) this.toast('success', 'تنظیمات فروشگاه ذخیره شد');
      else this.toast('error', (d && d.msg) || 'خطا');
    },
    async loadCustomGB() {
      const d = await this.api('/pro/shop/custom-gb');
      if (d && d.success && d.obj) {
        const o = d.obj;
        this.shop.customGB.enabled = !!(o.enabled ?? o.Enabled);
        this.shop.customGB.pricePerGB = Number(o.pricePerGB ?? o.PricePerGB ?? 0);
        this.shop.customGB.minGB = Number(o.minGB ?? o.MinGB ?? 5);
        this.shop.customGB.maxGB = Number(o.maxGB ?? o.MaxGB ?? 500);
        this.shop.customGB.days = Number(o.days ?? o.Days ?? 30);
        this.shop.customGB.inboundId = Number(o.inboundId ?? o.InboundID ?? 0);
      }
    },
    async saveCustomGB() {
      const cfg = this.shop.customGB;
      const d = await this.api('/pro/shop/custom-gb', { method: 'POST', body: JSON.stringify({
        enabled: !!cfg.enabled,
        pricePerGB: Number(cfg.pricePerGB)||0,
        minGB: Number(cfg.minGB)||5,
        maxGB: Number(cfg.maxGB)||500,
        days: Number(cfg.days)||30,
        inboundId: Number(cfg.inboundId)||0
      })});
      if (d && d.success) { this.toast('success', 'تنظیمات حجم دلخواه ذخیره شد'); if (d.obj) Object.assign(this.shop.customGB, { enabled:!!d.obj.enabled, pricePerGB:Number(d.obj.pricePerGB||0), minGB:Number(d.obj.minGB||5), maxGB:Number(d.obj.maxGB||500), days:Number(d.obj.days||30), inboundId:Number(d.obj.inboundId||0) }); }
      else this.toast('error', (d && d.msg) || 'ذخیره حجم دلخواه ناموفق');
    },
    async saveShopPlan() {
      const f = this.shop.planForm;
      const inboundId = Number(f.inboundId)||0;
      if (!f.name || !inboundId) return this.toast('error', 'نام و Inbound ID معتبر لازم است');
      const d = await this.api('/pro/shop/plans', { method: 'POST', body: JSON.stringify({
        name: f.name,
        totalGB: Number(f.totalGB)||0,
        durationDays: Number(f.durationDays)||30,
        priceToman: Number(f.priceToman)||0,
        inboundId,
        enabled: true,
      }) });
      if (d && d.success) { this.toast('success', 'پلن ذخیره شد'); this.shop.planForm.name=''; this.loadShopPlans(); }
      else this.toast('error', (d && d.msg) || 'ذخیره پلن ناموفق');
    },
    async deleteShopPlan(p) {
      if (!confirm('حذف پلن؟')) return;
      const d = await this.api('/pro/shop/plans/' + p.id + '/delete', { method: 'POST', body: '{}' });
      if (d && d.success) { this.toast('success', 'حذف شد'); this.loadShopPlans(); }
    },
    async approveShopOrder(o) {
      const d = await this.api('/pro/shop/orders/' + o.id + '/approve', { method: 'POST', body: '{}' });
      if (d && d.success) {
        let msg = 'تأیید شد';
        if (d.obj && d.obj.subId) msg += ' · Sub ' + d.obj.subId;
        if (d.obj && d.obj.code) msg += ' · Gift ' + d.obj.code;
        if (d.obj && d.obj.runtimeApplied === false) {
          this.toast('error', msg + ' — اما Xray اعمال نشد: ' + (d.obj.runtimeError || 'unknown'));
        } else {
          this.toast('success', msg);
        }
        this.loadShopOrders();
      } else this.toast('error', (d && d.msg) || 'خطا در تأیید سفارش');
    },
    async rejectShopOrder(o) {
      const d = await this.api('/pro/shop/orders/' + o.id + '/reject', { method: 'POST', body: '{}' });
      if (d && d.success) { this.toast('success', 'رد شد'); this.loadShopOrders(); }
    },
    async loadTrialSettings() {
      const d = await this.api('/pro/shop/trial');
      if (d && d.success && d.obj) {
        this.shop.trial.enabled = !!(d.obj.Enabled ?? d.obj.enabled);
        this.shop.trial.mb = d.obj.MB ?? d.obj.mb ?? 500;
        this.shop.trial.hours = d.obj.Hours ?? d.obj.hours ?? 2;
        this.shop.trial.inboundId = d.obj.InboundID ?? d.obj.inboundId ?? 0;
      }
    },
    async saveTrialSettings() {
      const d = await this.api('/pro/shop/trial', { method: 'POST', body: JSON.stringify({ enabled: this.shop.trial.enabled, mb: this.shop.trial.mb, hours: this.shop.trial.hours, inboundId: this.shop.trial.inboundId }) });
      if (d && d.success) this.toast('success', 'تنظیمات تست ذخیره شد');
      else this.toast('error', (d && d.msg) || 'خطا');
    },
    async loadWinbackSettings() {
      const d = await this.api('/pro/shop/winback');
      if (d && d.success && d.obj) {
        this.shop.winback.enabled = !!(d.obj.enabled ?? d.obj.Enabled);
        this.shop.winback.daysAfter = Number(d.obj.daysAfter ?? d.obj.DaysAfter ?? 3);
        this.shop.winback.discountPct = Number(d.obj.discountPct ?? d.obj.DiscountPct ?? 30);
        const plans = d.obj.planIds ?? d.obj.PlanIDs ?? '';
        this.shop.winback.planIds = Array.isArray(plans) ? plans.join(',') : String(plans || '');
        this.shop.winback.message = d.obj.message ?? d.obj.Message ?? '';
      }
    },
    async saveWinbackSettings() {
      const w = this.shop.winback;
      const d = await this.api('/pro/shop/winback', { method: 'POST', body: JSON.stringify({
        enabled: !!w.enabled,
        daysAfter: Number(w.daysAfter)||3,
        discountPct: Number(w.discountPct)||0,
        planIds: String(w.planIds||''),
        message: String(w.message||'')
      })});
      if (d && d.success) {
        this.toast('success', 'تنظیمات Winback ذخیره شد');
        if (d.obj) {
          this.shop.winback.enabled = !!(d.obj.enabled);
          this.shop.winback.daysAfter = Number(d.obj.daysAfter||3);
          this.shop.winback.discountPct = Number(d.obj.discountPct||0);
          this.shop.winback.planIds = String(d.obj.planIds||'');
          this.shop.winback.message = String(d.obj.message||'');
        }
      } else this.toast('error', (d && d.msg) || 'ذخیره Winback ناموفق');
    },
    async shopBroadcast() {
      if (!this.shop.broadcast.trim()) return;
      const d = await this.api('/pro/shop/broadcast', { method: 'POST', body: JSON.stringify({ message: this.shop.broadcast, target: this.shop.broadcastTarget || 'all', minUsedGB: 10 }) });
      if (d && d.success) this.toast('success', 'ارسال: ' + d.obj.ok + ' ok / ' + d.obj.fail + ' fail');
      else this.toast('error', (d && d.msg) || 'خطا');
    },

    async loadTelegramConfig() {
      const d = await this.api('/pro/telegram/config');
      if (d && d.success && d.obj) {
        this.telegram.adminBotTokenHint = d.obj.adminBotTokenHint || '';
        this.telegram.userBotTokenHint = d.obj.userBotTokenHint || '';
        this.telegram.adminIds = d.obj.adminIds || '';
        this.telegram.adminRoles = d.obj.adminRoles || '';
      }
    },
    async saveTelegramConfig() {
      const body = {};
      const adminTok = String(this.telegram.adminBotToken || '').trim();
      const userTok = String(this.telegram.userBotToken || '').trim();
      if (adminTok) body.adminBotToken = adminTok;
      if (userTok) body.userBotToken = userTok;
      // Always persist admin IDs / roles (empty string clears intentionally)
      body.adminIds = String(this.telegram.adminIds || '').trim();
      body.adminRoles = String(this.telegram.adminRoles || '').trim();
      // Validate numeric admin IDs when set
      if (body.adminIds) {
        const parts = body.adminIds.split(/[,\s]+/).map(s => s.trim()).filter(Boolean);
        const bad = parts.filter(p => !/^\d{5,15}$/.test(p));
        if (bad.length) {
          this.toast('error', 'آیدی ادمین باید عددی باشد (از @userinfobot): ' + bad.join(', '));
          return;
        }
        body.adminIds = parts.join(',');
      }
      if (adminTok && userTok && adminTok === userTok) {
        this.toast('error', 'توکن ربات ادمین و کاربر نباید یکسان باشد');
        return;
      }
      if (!adminTok && !userTok && body.adminIds === '' && body.adminRoles === '' && !this.telegram.adminBotTokenHint && !this.telegram.userBotTokenHint) {
        this.toast('error', 'حداقل توکن یا آیدی ادمین را وارد کنید');
        return;
      }
      const d = await this.api('/pro/telegram/config', { method: 'POST', body: JSON.stringify(body) });
      if (d && d.success) {
        this.toast('success', 'تنظیمات تلگرام ذخیره شد — ربات‌ها خودکار وصل می‌شوند');
        this.telegram.adminBotToken = '';
        this.telegram.userBotToken = '';
        await this.loadTelegramConfig();
      } else this.toast('error', (d && d.msg) || 'ذخیره ناموفق');
    },
    async testAdminBot() {
      const d = await this.api('/pro/telegram/test-admin', { method: 'POST', body: '{}' });
      if (d && d.success) this.toast('success', 'Admin bot OK @' + (d.obj && d.obj.username || ''));
      else this.toast('error', (d && d.msg) || 'Test failed');
    },
    async testUserBot() {
      const d = await this.api('/pro/telegram/test-user', { method: 'POST', body: '{}' });
      if (d && d.success) this.toast('success', 'User bot OK @' + (d.obj && d.obj.username || ''));
      else this.toast('error', (d && d.msg) || 'Test failed');
    },

    subscriptionTrafficPct(cl) {
      if (!cl || !cl.totalGB) return 0;
      const used = (cl.up||0)+(cl.down||0);
      const limit = cl.totalGB * 1024 * 1024 * 1024;
      return Math.min(100, Math.round(used / limit * 1000) / 10);
    },
    subscriptionTrafficLabel(cl) {
      if (!cl) return '—';
      if (!cl.totalGB) return 'Unlimited';
      return this.subscriptionTrafficPct(cl) + '%';
    },
    subscriptionTrafficTone(cl) {
      const p = this.subscriptionTrafficPct(cl);
      if (p >= 90) return 'danger';
      if (p >= 70) return 'warn';
      return 'ok';
    },
    subscriptionDaysLeft(cl) {
      if (!cl || !cl.expiryTime) return -1;
      const ms = cl.expiryTime - Date.now();
      if (ms <= 0) return 0;
      return Math.max(1, Math.ceil(ms / 86400000));
    },
    subscriptionDaysPct(cl) {
      if (!cl || !cl.expiryTime) return 100;
      const left = this.subscriptionDaysLeft(cl);
      if (left <= 0) return 0;
      // Prefer purchased→expiry window when available
      let total = 30;
      if (cl.purchasedAt) {
        const start = new Date(cl.purchasedAt).getTime();
        const end = cl.expiryTime;
        if (end > start) total = Math.max(1, Math.ceil((end - start) / 86400000));
      }
      return Math.min(100, Math.round(left / total * 1000) / 10);
    },
    subscriptionDaysLabel(cl) {
      if (!cl) return '—';
      if (!cl.expiryTime) return 'Unlimited';
      const d = this.subscriptionDaysLeft(cl);
      return d <= 0 ? 'Expired' : d + ' days';
    },
    subscriptionDaysTone(cl) {
      const d = this.subscriptionDaysLeft(cl);
      if (d < 0) return 'ok';
      if (d <= 0) return 'danger';
      if (d <= 7) return 'warn';
      return 'ok';
    },
    async loadTickets() {
      if (!this.tickets) this.tickets = { list: [], selected: null, messages: [], reply: '', filter: 'all', newSubject: '', newBody: '', loading: false };
      const st = this.tickets.filter || 'all';
      const d = await this.api('/pro/tickets?status=' + encodeURIComponent(st) + '&limit=50');
      if (d && d.success) this.tickets.list = d.obj || [];
    },
    async openTicket(tk) {
      this.tickets.selected = tk;
      this.tickets.reply = '';
      const d = await this.api('/pro/tickets/' + tk.id);
      if (d && d.success) {
        this.tickets.selected = d.obj.ticket;
        this.tickets.messages = d.obj.messages || [];
      }
    },
    async replyTicket() {
      if (!this.tickets.selected || !this.tickets.reply.trim()) return;
      const d = await this.api('/pro/tickets/' + this.tickets.selected.id + '/reply', { method: 'POST', body: JSON.stringify({ body: this.tickets.reply }) });
      if (d && d.success) { this.tickets.reply = ''; this.toast('success','Reply sent'); this.openTicket(this.tickets.selected); this.loadTickets(); }
      else this.toast('error', (d&&d.msg)||'Reply failed');
    },
    async closeTicket(tk) {
      if (!tk || !confirm('Close ticket #' + tk.id + '?')) return;
      const d = await this.api('/pro/tickets/' + tk.id + '/close', { method: 'POST', body: '{}' });
      if (d && d.success) { this.toast('success','Ticket closed'); this.loadTickets(); if (this.tickets.selected) this.openTicket(this.tickets.selected); }
    },
    async createTicketForSelected() {
      const cl = this.subscription.selected;
      if (!cl) return this.toast('error','Select a client first');
      if (!this.tickets.newSubject.trim() || !this.tickets.newBody.trim()) return this.toast('error','Subject and body required');
      const d = await this.api('/pro/tickets', { method: 'POST', body: JSON.stringify({ clientId: cl.id, subject: this.tickets.newSubject, body: this.tickets.newBody, priority: 'normal' }) });
      if (d && d.success) {
        this.tickets.newSubject = ''; this.tickets.newBody = '';
        this.toast('success','Ticket opened');
        this.loadTickets();
      } else this.toast('error', (d&&d.msg)||'Failed');
    },

    inboundPresets() {
      return [
        {id:'anti-filter', label:'★ Anti-Filter 2026 (VLESS+REALITY+Vision+Fragment)', protocol:'vless', network:'tcp', security:'reality', fragment:true, mux:true},
        {id:'vless-xhttp-reality', label:'★ VLESS + XHTTP + REALITY (stream-one)', protocol:'vless', network:'xhttp', security:'reality', fragment:true, mux:true},
        {id:'vless-reality', label:'VLESS + TCP + REALITY Vision', protocol:'vless', network:'tcp', security:'reality', fragment:true, mux:true},
        {id:'vless-xhttp-tls', label:'VLESS + XHTTP + TLS (CDN-friendly)', protocol:'vless', network:'xhttp', security:'tls', fragment:true},
        {id:'vless-httpupgrade-tls', label:'VLESS + HTTPUpgrade + TLS', protocol:'vless', network:'httpupgrade', security:'tls'},
        {id:'vless-grpc-reality', label:'VLESS + gRPC + REALITY', protocol:'vless', network:'grpc', security:'reality', fragment:true, mux:true},
        {id:'vless-ws-tls', label:'VLESS + WebSocket + TLS (legacy)', protocol:'vless', network:'ws', security:'tls'},
        {id:'vless-grpc-tls', label:'VLESS + gRPC + TLS', protocol:'vless', network:'grpc', security:'tls'},
        {id:'vmess-ws-tls', label:'VMess + WebSocket + TLS', protocol:'vmess', network:'ws', security:'tls'},
        {id:'trojan-tls', label:'Trojan + TLS', protocol:'trojan', network:'tcp', security:'tls'},
        {id:'trojan-reality', label:'Trojan + REALITY', protocol:'trojan', network:'tcp', security:'reality', fragment:true},
        {id:'hysteria2-tls', label:'Hysteria2 + TLS (weak-net / gaming)', protocol:'hysteria2', network:'quic', security:'tls'},
        {id:'tuic-tls', label:'TUIC v5 + TLS', protocol:'tuic', network:'quic', security:'tls'},
        {id:'anytls-tls', label:'AnyTLS + TLS (modern)', protocol:'anytls', network:'tcp', security:'tls'},
        {id:'shadowtls', label:'ShadowTLS', protocol:'shadowtls', network:'tcp', security:'none'},
        {id:'ss2022', label:'Shadowsocks 2022 (blake3-aes-256)', protocol:'shadowsocks', network:'tcp', security:'none'},
        {id:'shadowsocks', label:'Shadowsocks AEAD (aes-256-gcm)', protocol:'shadowsocks', network:'tcp', security:'none'},
        {id:'wireguard', label:'WireGuard', protocol:'wireguard', network:'udp', security:'none'},
      ];
    },
    applyInboundPreset(id) {
      const preset = this.inboundPresets().find(x => x.id === id);
      if (!preset) return;
      this.newInbound.protocol = preset.protocol;
      this.newInbound.network = preset.network;
      this.newInbound.security = preset.security;
      this.protocolChanged();
      if (preset.fragment) {
        this.newInbound.fragmentEnabled = true;
        this.newInbound.fragmentPackets = 'tlshello';
        this.newInbound.fragmentLength = '50-120';
        this.newInbound.fragmentInterval = '10-20';
      }
      if (preset.mux) {
        this.newInbound.muxEnabled = true;
        this.newInbound.muxConcurrency = 8;
      }
      if (preset.id === 'anti-filter' || preset.id === 'vless-reality' || preset.id === 'vless-xhttp-reality') {
        this.newInbound.dest = 'www.microsoft.com:443';
        this.newInbound.serverName = 'www.microsoft.com,microsoft.com,www.cloudflare.com';
        this.newInbound.spiderX = '/';
        this.newInbound.antiDpiProfile = preset.id === 'anti-filter' ? 'aggressive' : 'balanced';
      }
      if (preset.protocol === 'hysteria2') {
        this.newInbound.antiDpiProfile = 'balanced';
        this.newInbound.obfsType = this.newInbound.obfsType || 'salamander';
      }
      if (preset.id === 'ss2022') this.newInbound.ssMethod = '2022-blake3-aes-256-gcm';
      if (preset.id === 'shadowsocks') this.newInbound.ssMethod = 'aes-256-gcm';
      if (preset.id === 'vless-xhttp-reality') this.newInbound.xhttpMode = 'stream-one';
      if (preset.id === 'vless-ws-tls' || preset.id === 'vmess-ws-tls') this.newInbound.wsPath = '/ws';
      if (preset.id === 'vless-grpc-tls') this.newInbound.grpcService = 'electruvpn';
      this.toast('success', preset.label + ' preset applied');
    },
    async openQuickAntiFilter() {
      this.quickTransport = { network: 'tcp', label: 'Anti-Filter (VLESS+REALITY+Fragment)', remark: '', clientName: '', totalGB: 0, days: 30, inboundTotalGB:0, inboundDays:0, port: 0, ipMode: 'ipv4', addressType: 'ipv4', saving: false, result: null };
      this.showQuickTransportModal = true;
      this._quickAntiFilter = true;
    },
    async createQuickAntiFilter() {
      const q = this.quickTransport;
      q.saving = true;
      try {
        const d = await this.api('/inbounds/quick-anti-filter', { method: 'POST', body: JSON.stringify({ remark: q.remark, clientName: q.clientName, totalGB: Number(q.totalGB||0), days: Number(q.days||0), inboundTotalGB: Number(q.inboundTotalGB||0), inboundDays: Number(q.inboundDays||0), port: Number(q.port||0), ipMode: q.ipMode, addressType: q.addressType }) });
        if (!d?.success) throw new Error(d?.msg || 'Creation failed');
        q.result = d.obj;
        this.toast('success', 'Anti-filter inbound created (REALITY + Fragment + Mux)');
        await this.loadInbounds();
      } catch (e) { this.toast('error', e.message || 'Creation failed'); }
      finally { q.saving = false; this._quickAntiFilter = false; }
    },
    async rotateRealityKeys(ib) {
      if (!confirm('Rotate REALITY keys for "' + (ib.remark||ib.id) + '"? Existing client links will need a subscription refresh.')) return;
      const d = await this.api('/inbounds/' + ib.id + '/rotate-reality', { method: 'POST', body: '{}' });
      if (d?.success) {
        this.toast('success', 'REALITY keys rotated — clients should refresh subscription');
        await this.loadInbounds();
      } else this.toast('error', d?.msg || 'Rotation failed');
    },
    async openInboundHealth(ib) {
      const d = await this.api('/inbounds/'+ib.id+'/health');
      this.inboundHealth = {show:true,title:ib.remark,data:d?.obj||null};
      if (!d?.success) this.toast('error', d?.msg || 'Health check failed');
    },
    openInboundAdvanced(ib) {
      const payload = {remark:ib.remark, labels:ib.labels||'', port:ib.port, protocol:ib.protocol, network:ib.network, security:ib.security, sniffing:!!ib.sniffing, settings:ib.settings||'{}', streamSettings:ib.streamSettings||'{}'};
      this.inboundAdvanced = {show:true,title:ib.remark,id:ib.id,json:JSON.stringify(payload,null,2),error:'',mode:'edit'};
    },
    async validateAdvancedInbound() {
      try {
        const v=JSON.parse(this.inboundAdvanced.json);
        const body={remark:v.remark,port:Number(v.port),protocol:v.protocol,network:v.network,security:v.security,sniffing:v.sniffing!==false,settings:typeof v.settings==='string'?v.settings:JSON.stringify(v.settings||{}),streamSettings:typeof v.streamSettings==='string'?v.streamSettings:JSON.stringify(v.streamSettings||{}),labels:v.labels||''};
        const d=await this.api('/inbounds/validate-draft',{method:'POST',body:JSON.stringify(body)});
        if(d?.success){ this.inboundAdvanced.error='✓ Configuration is valid'; this.inboundPreview={show:true,title:'Advanced configuration validation',engine:d.engine,config:d.config,valid:true,msg:'Configuration validated successfully'}; }
        else this.inboundAdvanced.error='✕ '+(d?.msg||'Validation failed');
      } catch(e) { this.inboundAdvanced.error='✕ Invalid JSON: '+e.message; }
    },
    async saveAdvancedInbound() {
      try {
        const v=JSON.parse(this.inboundAdvanced.json);
        const body={remark:String(v.remark||'').trim(),labels:v.labels||'',settings:typeof v.settings==='string'?v.settings:JSON.stringify(v.settings||{}),streamSettings:typeof v.streamSettings==='string'?v.streamSettings:JSON.stringify(v.streamSettings||{})};
        if(!body.remark) throw new Error('Remark is required');
        const check=await this.api('/inbounds/validate-draft',{method:'POST',body:JSON.stringify({...v,port:Number(v.port),settings:body.settings,streamSettings:body.streamSettings})});
        if(!check?.success){ this.inboundAdvanced.error='✕ '+(check?.msg||'Validation failed'); return; }
        const d=await this.api('/inbounds/'+this.inboundAdvanced.id+'/update',{method:'POST',body:JSON.stringify(body)});
        if(d?.success){ this.inboundAdvanced.show=false; this.toast('success','Advanced configuration saved'); this.loadInbounds(); } else this.inboundAdvanced.error='✕ '+(d?.msg||'Save failed');
      } catch(e) { this.inboundAdvanced.error='✕ '+e.message; }
    },
    async generateRealityKeys() { try { const d=await this.api('/inbounds/reality-keypair'); if(!d?.success) throw new Error(d?.msg||'Could not generate keypair'); this.newInbound.privateKey=d.privateKey||''; this.newInbound.publicKey=d.publicKey||''; const ids=Array.isArray(d.shortIds)?d.shortIds.filter(Boolean):[]; this.newInbound.shortId=(ids.length?ids.join(','):(d.shortId||'')); if(!this.newInbound.privateKey||!this.newInbound.publicKey) throw new Error('Empty keys returned from server'); this.toast('success','REALITY keypair generated'); } catch(e){ this.toast('error', e.message||'Could not generate keypair'); } },
    async suggestAvailablePort(strategy='random') { const d=await this.api('/inbounds/port-availability?port=0&strategy='+strategy+'&network='+encodeURIComponent(this.newInbound.network)+'&ipMode='+encodeURIComponent(this.newInbound.ipMode||'ipv4')); if(d?.success){this.newInbound.port=Number(d.port);this.portStatus={state:'available',message:'Port '+d.port+' is available',port:Number(d.port)}} else this.toast('error',d?.msg||'Could not find an available port'); },
    inboundStepError(step) {
      const ni = this.newInbound || {};
      if (step === 1) {
        if (!String(ni.remark || '').trim()) return 'Enter a name for this inbound.';
        const port = Number(ni.port || 0);
        if (!Number.isInteger(port) || port < 1 || port > 65535) return 'Enter a valid port between 1 and 65535.';
        if (!ni.protocol) return 'Choose a protocol.';
        if (!String(ni.ipMode || '').match(/^(ipv4|ipv6|dual)$/)) return 'Choose a supported IP mode.';
        if (!String(ni.protocol).match(/^(vless|vmess|trojan|shadowsocks|hysteria2|tuic|wireguard|anytls|shadowtls)$/)) return 'Unsupported protocol selected.';
      }
      if (step === 2) {
        if (!ni.network) return 'Choose a transport / network.';
        if (ni.network === 'ws' && !String(ni.wsPath || '').trim()) return 'Enter a WebSocket path.';
        if (ni.network === 'grpc' && !String(ni.grpcService || '').trim()) return 'Enter a gRPC service name.';
        if (ni.network === 'xhttp') {
          if (!String(ni.xhttpPath || '').trim()) return 'XHTTP path is required (e.g. /xhttp).';
          const mode = String(ni.xhttpMode || 'auto');
          if (!['auto','stream-one','stream-up','packet-up',''].includes(mode)) return 'Unsupported XHTTP mode.';
          if (ni.xhttpExtraJson) {
            try { JSON.parse(ni.xhttpExtraJson); } catch (_) { return 'XHTTP Advanced must be valid JSON.'; }
          }
        }
        if (ni.network === 'httpupgrade') {
          if (!String(ni.httpUpgradePath || '').trim()) return 'HTTPUpgrade path is required (e.g. /).';
          if (ni.httpUpgradeHeadersJson) {
            try { JSON.parse(ni.httpUpgradeHeadersJson); } catch (_) { return 'HTTPUpgrade Headers must be valid JSON.'; }
          }
        }
      }
      if (step === 3) {
        if (ni.security === 'reality') {
          if (!['tcp','xhttp','grpc'].includes(String(ni.network||''))) {
            return 'REALITY فقط با TCP / XHTTP / gRPC مجاز است. برای HTTPUpgrade یا WebSocket از TLS استفاده کنید.';
          }
          if (!String(ni.dest || '').trim() || !/^[^:\s]+:\d{1,5}$/.test(String(ni.dest).trim())) return 'Enter a REALITY destination in host:port format.';
          const destPort = Number(String(ni.dest).trim().split(':').pop());
          if (!Number.isInteger(destPort) || destPort < 1 || destPort > 65535) return 'REALITY destination port must be between 1 and 65535.';
          if (!String(ni.serverName || '').trim()) return 'Enter at least one REALITY server name / SNI.';
          // privateKey optional — backend auto-generates if empty
        }
        if (ni.security === 'tls' && (!String(ni.certFile || '').trim() || !String(ni.keyFile || '').trim())) return 'TLS requires both certificate and private key file paths.';
      }
      return '';
    },
    goInboundStep(step) {
      const target = Math.max(1, Math.min(4, Number(step) || 1));
      if (target > this.inboundStep) {
        for (let current = 1; current < target; current++) {
          const error = this.inboundStepError(current);
          if (error) { this.toast('error', error); this.inboundStep = current; return; }
        }
      }
      this.inboundStep = target;
      if (target === 4) this.validateDraft();
      this.$nextTick(() => {
        const body = document.querySelector('.inbound-wizard-body');
        if (body) body.scrollTo({ top: 0, behavior: 'smooth' });
      });
    },
    async nextInboundStep() {
      const error = this.inboundStepError(this.inboundStep);
      if (error) { this.toast('error', error); return; }
      if (this.inboundStep < 4) this.goInboundStep(this.inboundStep + 1);
    },
    async validateInboundDraft({showPreview=true}={}) {
      const ni=this.newInbound;
      const localError=[1,2,3].map(step=>this.inboundStepError(step)).find(Boolean);
      if(localError){ if(showPreview)this.toast('error',localError); return false; }
      const {streamSettings,settings}=this.buildInboundPayload();
      const body={remark:String(ni.remark||'').trim(),port:Number(ni.port),protocol:ni.protocol,network:ni.network,security:ni.security,sniffing:ni.sniffing!==false,settings:JSON.stringify(settings),streamSettings:JSON.stringify(streamSettings),labels:ni.labels||''};
      this.inboundValidating=true;
      try {
        const d=await this.api('/inbounds/validate-draft',{method:'POST',body:JSON.stringify(body)});
        const valid=!!d?.success;
        this.inboundPreview={show:showPreview,title:'Draft validation',engine:d?.engine||'',config:d?.config||'',valid,msg:valid?'Configuration validated successfully':(d?.msg||'Validation failed')};
        if(valid)this.inboundStep=4;
        else if(showPreview)this.toast('error',d?.msg||'Configuration validation failed');
        return valid;
      } catch(e) {
        this.inboundPreview={show:showPreview,title:'Draft validation',engine:'',config:'',valid:false,msg:e?.message||'Validation request failed'};
        if(showPreview)this.toast('error',e?.message||'Configuration validation failed');
        return false;
      } finally { this.inboundValidating=false; }
    },
    async validateDraft() { return this.validateInboundDraft({showPreview:true}); },
    toggleExpand(ib) {
      const closing=this.expandedInboundId===ib.id;
      this.expandedInboundId = closing ? null : ib.id;
      this.bulkOpenFor = null;
      this.newClient = { email: '', totalGB: 0, limitIp: 0, expiryTime: 0, subscriptionAddressType: 'ipv4', subscriptionEnabled: true, subscriptionAutoUpdate: true };
      if(!closing && !(ib.clients||[]).length && Number(ib.clientCount||0)>0) this.loadInboundClients(ib, Number(this.inboundClientPages[ib.id]?.page||1));
      else if(!closing && !(ib.clients||[]).length) this.loadInboundClients(ib,1);
    },
    async openInboundModalWithProfile(profile) {
      const p = profile || 'balanced';
      await this.openInboundModal();
      this.newInbound.antiDpiProfile = p;
      this.newInbound.protocol = 'vless';
      this.newInbound.network = 'tcp';
      this.newInbound.security = 'reality';
      this.newInbound.dest = 'www.microsoft.com:443';
      this.newInbound.serverName = 'www.microsoft.com,microsoft.com,www.cloudflare.com';
      this.newInbound.spiderX = '/';
      this.newInbound.fragmentEnabled = true;
      this.newInbound.fragmentPackets = 'tlshello';
      this.newInbound.fragmentLength = p === 'light' ? '50-100' : '100-200';
      this.newInbound.fragmentInterval = '10-20';
      this.newInbound.muxEnabled = true;
      this.newInbound.muxConcurrency = 8;
      if (typeof this.protocolChanged === 'function') this.protocolChanged();
      if (!String(this.newInbound.privateKey || '').trim()) {
        try { await this.generateRealityKeys(); } catch (_) {}
      }
      this.toast('success', 'فرم ساخت با Anti-DPI: ' + p);
    },
    async openInboundModal(ib) {
      if (ib && ib.id) { this.openEditInbound(ib); return; }
      this.editingInboundId = null;
      this.newInbound = inboundDefaults();
      this.selectedInboundPreset = '';
      this.inboundTemplateName = '';
      this.inboundStep = 1;
      this.portStatus = { state: 'checking', message: 'Finding a free port…', port: 0 };
      this.showInboundModal = true;
      await this.suggestAvailablePort();
      // Auto-generate REALITY keys so user is not blocked on step 3
      if (this.newInbound.security === 'reality' && !String(this.newInbound.privateKey || '').trim()) {
        try { await this.generateRealityKeys(); } catch (_) {}
      }
    },
    async checkInboundPort() {
      if (this.editingInboundId) return;
      const port = Number(this.newInbound.port || 0);
      if (!Number.isInteger(port) || port < 1 || port > 65535) {
        this.portStatus = { state: 'invalid', message: 'Enter a port between 1 and 65535', port };
        return;
      }
      this.portStatus = { state: 'checking', message: 'Checking port…', port };
      const data = await this.api('/inbounds/port-availability?port=' + encodeURIComponent(port) + '&network=' + encodeURIComponent(this.newInbound.network) + '&ipMode=' + encodeURIComponent(this.newInbound.ipMode||'ipv4'));
      if (data && data.success && data.available) {
        this.portStatus = { state: 'available', message: `Port ${port} is available`, port };
      } else {
        this.portStatus = { state: 'busy', message: (data && data.msg) || `Port ${port} is busy`, port };
      }
    },
    openEditInbound(ib) {
      this.editingInboundId = ib.id;
      this.portStatus = { state: 'available', message: `Port ${ib.port} is assigned to this config`, port: ib.port };
      let stream = {};
      try { stream = JSON.parse(ib.streamSettings || '{}'); } catch (e) {}
      let settings = {};
      try { settings = JSON.parse(ib.settings || '{}'); } catch (e) {}

      // Support both panel-flat streamSettings and nested Xray-style settings (wsSettings/grpcSettings/...)
      const ws = stream.wsSettings || {};
      const grpc = stream.grpcSettings || {};
      const xhttp = stream.xhttpSettings || {};
      const httpup = stream.httpupgradeSettings || stream.httpUpgradeSettings || {};
      const tls = stream.tlsSettings || {};
      const reality = stream.realitySettings || {};
      const frag = stream.fragment || {};
      const sock = stream.sockopt || {};
      const mux = stream.mux || {};
      const sniff = stream.sniffing || {};
      const masks = Array.isArray(stream.tcpMasks) && stream.tcpMasks.length ? stream.tcpMasks[0] : {};
      const fallbacks = Array.isArray(stream.fallbacks) && stream.fallbacks.length ? stream.fallbacks : [];

      const serverNames = Array.isArray(reality.serverNames) ? reality.serverNames.filter(Boolean) : [];
      const shortIds = Array.isArray(reality.shortIds) ? reality.shortIds.filter(Boolean) : [];
      const alpn = tls.alpn ? (Array.isArray(tls.alpn) ? tls.alpn.join(',') : String(tls.alpn)) : '';

      this.inboundStep = 1;
      this.newInbound = {
        ...inboundDefaults(),
        antiDpiProfile: (ib.labels && String(ib.labels).match(/anti-dpi[=:]([^\s,]+)/i)?.[1]) || 'off',
        remark: ib.remark,
        inboundTotalGB: Number(ib.totalGB || 0),
        inboundDays: '',
        inboundExpiryTime: Number(ib.expiryTime || 0),
        inboundLimitReached: !!ib.limitReached,
        ipMode: ib.ipMode || 'ipv4',
        labels: ib.labels || '',
        subscriptionEnabled: ib.subscriptionEnabled !== false,
        subscriptionName: ib.subscriptionName || '',
        subscriptionPreset: ib.subscriptionPreset || 'standard',
        subscriptionSmartEnabled: !!ib.subscriptionSmartEnabled,
        subscriptionDefaultAddress: ib.subscriptionDefaultAddress || ((ib.ipMode||'ipv4')==='ipv6'?'ipv6':((ib.ipMode||'ipv4')==='dual'?'all':'ipv4')),
        port: ib.port,
        protocol: ib.protocol,
        network: ib.network || stream.network || 'tcp',
        security: ib.security || stream.security || 'none',
        // transport (flat panel keys OR nested Xray keys)
        wsPath: stream.wsPath || ws.path || '/ws',
        wsHost: stream.wsHost || ws.host || '',
        grpcService: stream.grpcServiceName || grpc.serviceName || 'electruvpn',
        xhttpPath: stream.xhttpPath || xhttp.path || '/xhttp',
        xhttpHost: stream.xhttpHost || xhttp.host || '',
        xhttpMode: stream.xhttpMode || xhttp.mode || 'auto',
        xhttpExtraJson: stream.xhttpExtraJson || '',
        httpUpgradePath: stream.httpUpgradePath || httpup.path || '/',
        httpUpgradeHost: stream.httpUpgradeHost || httpup.host || '',
        httpUpgradeHeadersJson: stream.httpUpgradeHeadersJson || (httpup.headers ? JSON.stringify(httpup.headers) : ''),
        acceptProxyProtocol: !!(stream.acceptProxyProtocol ?? httpup.acceptProxyProtocol),
        // TLS / REALITY
        certFile: tls.certFile || '',
        keyFile: tls.keyFile || '',
        sni: tls.sni || tls.serverName || '',
        alpn: alpn || 'h2,http/1.1',
        dest: reality.dest || 'www.microsoft.com:443',
        serverName: serverNames.length ? serverNames.join(',') : (reality.serverName || 'www.microsoft.com,microsoft.com,www.cloudflare.com'),
        spiderX: reality.spiderX || '/',
        privateKey: reality.privateKey || '',
        publicKey: reality.publicKey || '',
        shortId: shortIds.length ? shortIds.join(',') : '',
        // advanced
        fragmentEnabled: frag.enabled !== false && (frag.enabled === true || !!frag.packets || !!frag.type),
        fragmentType: frag.type || 'tlshello',
        fragmentPackets: frag.packets || 'tlshello',
        fragmentLength: frag.length || '100-200',
        fragmentInterval: frag.interval || '10-20',
        fragmentMaxSplit: frag.maxSplit || '100-200',
        muxEnabled: !!(mux.enabled),
        muxConcurrency: Number(mux.concurrency != null ? mux.concurrency : 8),
        muxSessionTTL: Number(mux.sessionTTL != null ? mux.sessionTTL : 300),
        muxXudpConcurrency: Number(mux.xudpConcurrency != null ? mux.xudpConcurrency : 16),
        muxPadding: mux.padding !== false,
        sockoptEnabled: !!(sock.enabled || sock.tcpKeepAliveIdle || sock.tcpNoDelay),
        tcpKeepAlive: Number(sock.tcpKeepAliveIdle != null ? sock.tcpKeepAliveIdle : 30),
        tcpNoDelay: sock.tcpNoDelay !== false,
        tcpFastOpen: !!sock.tcpFastOpen,
        tcpMaskEnabled: !!masks.type,
        tcpMask1Type: masks.type || 'fragment',
        tcpMask1Packets: masks.packets || '1-3',
        tcpMask1Lengths: masks.lengths || '100-200',
        tcpMask1Delays: masks.delays || '10-20',
        tcpMask1MaxSplit: masks.maxSplit || '100-200',
        sniffing: ib.sniffing !== false,
        sniffRouteOnly: sniff.routeOnly !== false,
        sniffMetadataOnly: !!sniff.metadataOnly,
        sniffDestOverride: Array.isArray(sniff.destOverride) ? sniff.destOverride.join(',') : (sniff.destOverride || 'http,tls,quic'),
        fallbackDest: fallbacks[0]?.dest || '',
        // protocol-specific settings / methods
        ssMethod: settings.method || '2022-blake3-aes-256-gcm',
        upMbps: (settings.upMbps != null ? settings.upMbps : 0),
        downMbps: (settings.downMbps != null ? settings.downMbps : 0),
        obfsType: settings.obfsType || 'salamander',
        obfsPassword: settings.obfsPassword || '',
        congestionControl: settings.congestionControl || 'bbr',
        wgPrivateKey: settings.privateKey || '',
        wgPublicKey: settings.publicKey || '',
        wgAddress: settings.address || '10.77.1.1/24',
        wgMtu: settings.mtu || 1420,
        wgTlsCoverEnabled: !!settings.tlsCoverEnabled,
        wgTlsCoverSni: settings.tlsCoverSni || '',
        wgTlsCoverPort: Number(settings.tlsCoverPort || 0) || 0,
      };
      this.showInboundModal = true;
      // Soft-fix incompatible transport/security without wiping loaded paths
      if (this.newInbound.security === 'reality' && !['tcp','xhttp','grpc'].includes(this.newInbound.network)) {
        this.newInbound.security = (this.newInbound.network === 'ws' || this.newInbound.network === 'httpupgrade') ? 'tls' : 'none';
      }
      if (this.newInbound.network === 'xhttp' && !String(this.newInbound.xhttpPath||'').trim()) this.newInbound.xhttpPath = '/xhttp';
      if (this.newInbound.network === 'httpupgrade' && !String(this.newInbound.httpUpgradePath||'').trim()) this.newInbound.httpUpgradePath = '/';
    },
    ipModeChanged() { const mode=this.newInbound.ipMode; const current=this.newInbound.subscriptionDefaultAddress; if(mode==='ipv4' && ['ipv6','dual','all'].includes(current)) this.newInbound.subscriptionDefaultAddress='ipv4'; else if(mode==='ipv6' && ['ipv4','dual','all'].includes(current)) this.newInbound.subscriptionDefaultAddress='ipv6'; else if(mode==='dual' && ['ipv4','ipv6'].includes(current)) this.newInbound.subscriptionDefaultAddress='all'; },
    protocolChanged() {
      const ni = this.newInbound;
      const p = ni.protocol;
      if (p==='hysteria2'||p==='tuic') { ni.network='quic'; ni.security='tls'; }
      else if (p==='wireguard') { ni.network='udp'; ni.security='none'; }
      else if (p==='anytls'||p==='shadowtls') { ni.network='tcp'; ni.security='tls'; }
      else if (p==='vless' || p==='vmess' || p==='trojan') {
        if (['quic','udp'].includes(ni.network)) ni.network='tcp';
      }
      // Transport ↔ security compatibility
      // REALITY only works with tcp / xhttp / grpc
      if (ni.security === 'reality' && !['tcp','xhttp','grpc'].includes(ni.network)) {
        ni.security = (ni.network === 'ws' || ni.network === 'httpupgrade') ? 'tls' : 'none';
      }
      // Defaults when switching transport
      if (ni.network === 'xhttp') {
        if (!String(ni.xhttpPath||'').trim()) ni.xhttpPath = '/xhttp';
        if (!ni.xhttpMode) ni.xhttpMode = (ni.security === 'reality') ? 'stream-one' : 'auto';
      }
      if (ni.network === 'httpupgrade') {
        if (!String(ni.httpUpgradePath||'').trim()) ni.httpUpgradePath = '/';
      }
      if (ni.network === 'ws' && !String(ni.wsPath||'').trim()) ni.wsPath = '/ws';
      if (ni.network === 'grpc' && !String(ni.grpcService||'').trim()) ni.grpcService = 'electruvpn';
    },
    buildInboundPayload() {
      const ni = this.newInbound;
      const streamSettings = {
        network: ni.network, security: ni.security,
        wsPath: ni.wsPath || (ni.network==='ws' ? '/ws' : ''),
        wsHost: ni.wsHost || '',
        grpcServiceName: ni.grpcService || (ni.network==='grpc' ? 'electruvpn' : ''),
        xhttpPath: ni.xhttpPath || (ni.network==='xhttp' ? '/xhttp' : ''),
        xhttpHost: ni.xhttpHost || '',
        xhttpMode: ni.xhttpMode || (ni.network==='xhttp' ? (ni.security==='reality' ? 'stream-one' : 'auto') : ''),
        xhttpExtraJson: ni.xhttpExtraJson || '',
        httpUpgradePath: ni.httpUpgradePath || (ni.network==='httpupgrade' ? '/' : ''),
        httpUpgradeHost: ni.httpUpgradeHost || '',
        httpUpgradeHeadersJson: ni.httpUpgradeHeadersJson || '',
        acceptProxyProtocol: !!ni.acceptProxyProtocol,
      };
      if (ni.security === 'tls') {
        streamSettings.tlsSettings = { certFile: ni.certFile, keyFile: ni.keyFile, sni: ni.sni, alpn: ni.alpn || '' };
      }
      if (ni.security === 'reality') {
        const names = String(ni.serverName || '').split(',').map(s => s.trim()).filter(Boolean);
        const shortIds = String(ni.shortId || '').split(',').map(s => s.trim()).filter(Boolean);
        streamSettings.realitySettings = {
          dest: ni.dest,
          serverNames: names.length ? names : ['www.microsoft.com'],
          privateKey: ni.privateKey,
          publicKey: ni.publicKey,
          shortIds: shortIds.length ? shortIds : [],
          spiderX: ni.spiderX || '/',
        };
      }
      if (ni.fragmentEnabled) {
        streamSettings.fragment = {
          enabled: true,
          type: ni.fragmentType || 'tlshello',
          packets: ni.fragmentPackets || 'tlshello',
          length: ni.fragmentLength || '100-200',
          interval: ni.fragmentInterval || '10-20',
          maxSplit: ni.fragmentMaxSplit || '100-200'
        };
      }
      if (ni.tcpMaskEnabled) {
        streamSettings.tcpMasks = [{
          type: ni.tcpMask1Type || 'fragment',
          packets: ni.tcpMask1Packets || '1-3',
          lengths: ni.tcpMask1Lengths || '100-200',
          delays: ni.tcpMask1Delays || '10-20',
          maxSplit: ni.tcpMask1MaxSplit || '100-200'
        }];
      }
      if (ni.sockoptEnabled) {
        streamSettings.sockopt = {
          enabled: true,
          tcpKeepAliveIdle: Number(ni.tcpKeepAlive || 0) || undefined,
          tcpNoDelay: ni.tcpNoDelay !== false,
          tcpFastOpen: !!ni.tcpFastOpen
        };
      }
      if (ni.muxEnabled) {
        streamSettings.mux = {
          enabled: true,
          concurrency: Number(ni.muxConcurrency || 8),
          sessionTTL: Number(ni.muxSessionTTL || 0) || undefined,
          xudpConcurrency: Number(ni.muxXudpConcurrency || 0) || undefined,
          padding: !!ni.muxPadding,
          xudpProxyUDP443: 'reject'
        };
      }
      if (ni.sniffing !== false) {
        const dests = String(ni.sniffDestOverride || 'http,tls,quic').split(',').map(s => s.trim()).filter(Boolean);
        streamSettings.sniffing = {
          destOverride: dests.length ? dests : ['http','tls','quic'],
          routeOnly: ni.sniffRouteOnly !== false,
          metadataOnly: !!ni.sniffMetadataOnly
        };
      }
      if (ni.fallbackDest && String(ni.fallbackDest).trim()) {
        streamSettings.fallbacks = [{ dest: String(ni.fallbackDest).trim() }];
      }
      let settings = {};
      if (ni.protocol === 'vless' || ni.protocol === 'vmess' || ni.protocol === 'trojan') {
        settings = { clients: [], decryption: ni.protocol === 'vless' ? 'none' : undefined };
        if (ni.protocol !== 'vless') delete settings.decryption;
      }
      if (ni.protocol === 'shadowsocks') settings = { method: ni.ssMethod || '2022-blake3-aes-256-gcm' };
      if (ni.protocol === 'hysteria2') settings = { upMbps: ni.upMbps||0, downMbps: ni.downMbps||0, obfsType: ni.obfsType||'', obfsPassword: ni.obfsPassword||'' };
      if (ni.protocol === 'tuic') settings = { congestionControl: ni.congestionControl || 'bbr' };
      if (ni.protocol === 'wireguard') settings = { privateKey: ni.wgPrivateKey||'', publicKey: ni.wgPublicKey||'', address: ni.wgAddress||'10.77.1.1/24', mtu: ni.wgMtu||1420, tlsCoverEnabled: !!ni.wgTlsCoverEnabled, tlsCoverSni: ni.wgTlsCoverSni||'', tlsCoverPort: Number(ni.wgTlsCoverPort||0)||0 };

      return { streamSettings, settings };
    },
    async createInbound() {
      if (this.inboundSaving || this.inboundValidating) return;
      const ni = this.newInbound;
      const localError = [1,2,3].map(step => this.inboundStepError(step)).find(Boolean);
      if (localError) { this.toast('error', localError); const badStep=[1,2,3].find(step=>this.inboundStepError(step)); if(badStep)this.inboundStep=badStep; return; }
      const port = Number(ni.port || 0);
      if (!Number.isInteger(port) || port < 1 || port > 65535) {
        this.toast('error', 'Enter a valid port between 1 and 65535');
        return;
      }
      await this.checkInboundPort();
      if (this.portStatus.state !== 'available' || this.portStatus.port !== port) {
        this.toast('error', this.portStatus.message || 'Selected port is not available');
        return;
      }
      const draftValid = await this.validateInboundDraft({showPreview:false});
      if (!draftValid) { this.inboundPreview.show=true; this.inboundStep=4; this.toast('error', this.inboundPreview.msg || 'Configuration validation failed'); return; }
      const ibLimitErr = this.inboundLimitError(ni);
      if (ibLimitErr) { this.inboundStep = 4; this.toast('error', ibLimitErr); return; }
      const { streamSettings, settings } = this.buildInboundPayload();
      const body = {
        totalGB: Number(ni.inboundTotalGB || 0), days: Number(ni.inboundDays || 0),
        remark: String(ni.remark||'').trim(), ipMode: ni.ipMode || 'ipv4', subscriptionEnabled: ni.subscriptionEnabled !== false, subscriptionName: ni.subscriptionName || '', subscriptionPreset: ni.subscriptionPreset || 'standard', subscriptionSmartEnabled: !!ni.subscriptionSmartEnabled, subscriptionDefaultAddress: ni.subscriptionDefaultAddress || 'ipv4', listen: ni.ipMode==='ipv4' ? '0.0.0.0' : (ni.ipMode==='ipv6' ? '::' : ''), port: ni.port, protocol: ni.protocol, network: ni.network, security: ni.security,
        sniffing: ni.sniffing !== false, labels: ni.labels || '', settings: JSON.stringify(settings), streamSettings: JSON.stringify(streamSettings),
        clientCount: ni.clientCount || 0, clientPrefix: ni.clientPrefix, clientTotalGB: ni.clientTotalGB || 0, clientDays: Number(ni.clientDays || 0), clientLimitIp: Number(ni.clientLimitIp || 0),
        antiDpiProfile: ni.antiDpiProfile || 'off',
      };
      this.inboundSaving = true;
      let data;
      try { data = await this.api('/inbounds', { method: 'POST', body: JSON.stringify(body) }); }
      catch (e) { this.toast('error', e?.message || 'Failed to create config'); return; }
      finally { this.inboundSaving = false; }
      if (data && data.success) {
        this.showInboundModal = false;
        this.inboundStep = 1;
        this.editingInboundId = null;
        const n = (data.obj && data.obj.clients || []).length;
        this.toast('success', n > 0 ? `Config created with ${n} client${n > 1 ? 's' : ''}` : 'Config created');
        await this.loadInbounds();
        if (data.obj && data.obj.id) {
          this.expandedInboundId = data.obj.id;
          const ib = this.inbounds.find(x => x.id === data.obj.id);
          if (ib) await this.loadInboundClients(ib, 1);
        }
      } else {
        this.toast('error', (data && data.msg) || 'Failed to create config');
      }
    },
    inboundLimitError(ni) {
      const gb = Number(ni.inboundTotalGB === '' || ni.inboundTotalGB == null ? 0 : ni.inboundTotalGB);
      if (!Number.isInteger(gb) || gb < 0 || gb > 10000000) return 'Inbound volume must be a whole number of GB (0 = unlimited).';
      if (ni.inboundDays !== '' && ni.inboundDays != null) {
        const d = Number(ni.inboundDays);
        if (!Number.isInteger(d) || d < 0 || d > 3650) return 'Inbound days must be a whole number between 0 and 3650 (0 = never expires).';
      }
      return '';
    },
    inboundExpiryLabel(ts) {
      if (!ts) return 'No expiry';
      const left = Math.ceil((Number(ts) - Date.now()) / 86400000);
      return left <= 0 ? 'Expired' : (left + ' days left');
    },
    async saveInboundEdit() {
      const ni = this.newInbound;
      // Validate transport/security before save (prevents broken XHTTP/HTTPUpgrade/REALITY edits)
      const localError = [1,2,3].map(step => this.inboundStepError(step)).find(Boolean);
      if (localError) {
        this.toast('error', localError);
        const badStep = [1,2,3].find(step => this.inboundStepError(step));
        if (badStep) this.inboundStep = badStep;
        return;
      }
      // Fix incompatible REALITY + transport before payload build
      if (typeof this.protocolChanged === 'function') this.protocolChanged();
      const ibLimitErr = this.inboundLimitError(ni);
      if (ibLimitErr) { this.inboundStep = 4; this.toast('error', ibLimitErr); return; }
      const { streamSettings, settings } = this.buildInboundPayload();
      const body = {
        totalGB: Number(ni.inboundTotalGB || 0),
        remark: ni.remark,
        protocol: ni.protocol,
        network: ni.network,
        security: ni.security,
        subscriptionEnabled: ni.subscriptionEnabled !== false,
        subscriptionName: ni.subscriptionName || '',
        subscriptionDefaultAddress: ni.subscriptionDefaultAddress || 'ipv4',
        labels: ni.labels || '',
        sniffing: ni.sniffing !== false,
        settings: JSON.stringify(settings),
        streamSettings: JSON.stringify(streamSettings),
      };
      // Days are counted from now. Blank = keep the current expiry, 0 = never expires.
      if (ni.inboundDays !== '' && ni.inboundDays != null) body.days = Number(ni.inboundDays);
      const data = await this.api(`/inbounds/${this.editingInboundId}/update`, { method: 'POST', body: JSON.stringify(body) });
      if (data && data.success) {
        this.showInboundModal = false;
        this.toast('success', 'Inbound updated');
        this.loadInbounds();
      } else {
        this.toast('error', (data && data.msg) || 'Failed to update inbound');
      }
    },
    async toggleInbound(ib) {
      if (!ib || !ib.id) return;
      const next = !ib.enable;
      const d = await this.api(`/inbounds/${ib.id}/update`, { method: 'POST', body: JSON.stringify({ enable: next }) });
      if (d && d.success) {
        ib.enable = next;
        if (next && ib.maintenance) ib.maintenance = false;
        this.toast('success', next ? ('روشن شد: ' + (ib.remark || ib.id)) : ('خاموش شد: ' + (ib.remark || ib.id)));
        await this.loadInbounds();
      } else {
        this.toast('error', (d && d.msg) || 'تغییر وضعیت اینباند ناموفق بود');
      }
    },
    async deleteInbound(ib) {
      if (!ib || !ib.id) return;
      const d = await this.api(`/inbounds/${ib.id}/delete`, { method: 'POST' });
      if (d && d.success) {
        this.toast('success', 'اینباند حذف شد');
        if (this.expandedInboundId === ib.id) this.expandedInboundId = null;
        await this.loadInbounds();
      } else this.toast('error', (d && d.msg) || 'حذف اینباند ناموفق بود');
    },

    // ---------- clients (managed inline within the inbound row) ----------
    openClientWizard(ib) {
      if (!ib || !Number.isInteger(Number(ib.id)) || Number(ib.id) < 1) {
        this.toast('error', 'Select a valid inbound first.');
        return;
      }
      this.newClient = { email: '', totalGB: 0, limitIp: 0, expiryTime: 0, subscriptionAddressType: 'ipv4', subscriptionEnabled: true, subscriptionAutoUpdate: true };
      this.clientWizard = { show: true, inboundId: Number(ib.id), step: 1, expiryDays: 0 };
    },
    clientWizardError(step) {
      const c = this.newClient || {};
      if (step === 1) {
        const email = String(c.email || '').trim();
        if (!email) return 'Enter a client label / email.';
        if (email.length > 128 || /[\r\n\0]/.test(email)) return 'Client label must be 1–128 characters on a single line.';
      }
      if (step === 2) {
        const totalGB = Number(c.totalGB || 0);
        const limitIp = Number(c.limitIp || 0);
        const expiryDays = Number(this.clientWizard.expiryDays || 0);
        if (!Number.isSafeInteger(totalGB) || totalGB < 0) return 'Traffic limit must be a whole number of GB; 0 means unlimited.';
        if (!Number.isInteger(limitIp) || limitIp < 0 || limitIp > 10000) return 'IP/device limit must be a whole number between 0 and 10000.';
        if (!Number.isInteger(expiryDays) || expiryDays < 0 || expiryDays > 36500) return 'Expiry must be a whole number of days between 0 and 36500.';
      }
      return '';
    },
    goClientStep(step) {
      const target = Math.max(1, Math.min(3, Number(step) || 1));
      if (target > this.clientWizard.step) {
        for (let current = this.clientWizard.step; current < target; current++) {
          const error = this.clientWizardError(current);
          if (error) { this.toast('error', error); this.clientWizard.step = current; return; }
        }
      }
      this.clientWizard.step = target;
      this.$nextTick(() => {
        const body = document.querySelector('.client-wizard-body');
        if (body) body.scrollTo({ top: 0, behavior: 'smooth' });
      });
    },
    async submitClientWizard() {
      if (this.clientSaving) return;
      const error = [1, 2].map(step => this.clientWizardError(step)).find(Boolean);
      if (error) { this.toast('error', error); this.clientWizard.step = this.clientWizardError(1) ? 1 : 2; return; }
      const ib = (this.inbounds || []).find(x => Number(x.id) === Number(this.clientWizard.inboundId));
      if (!ib) { this.toast('error', 'The selected inbound is no longer available. Refresh the inbound list and try again.'); return; }
      const days = Number(this.clientWizard.expiryDays || 0);
      this.newClient.expiryTime = days > 0 ? Date.now() + days * 86400000 : 0;
      await this.createClient(ib);
    },
    async generateClient(ib) {
      const previous = this.newClient.email;
      if (!String(this.newClient.email || '').trim()) {
        this.newClient.email = `client-${Date.now()}`;
      }
      await this.createClient(ib);
      if (previous) this.newClient.email = previous;
    },
    async createClient(ib) {
      if (this.clientSaving) return;
      const email = String(this.newClient.email || '').trim();
      const totalGB = Number(this.newClient.totalGB || 0);
      const limitIp = Number(this.newClient.limitIp || 0);
      const expiryTime = Number(this.newClient.expiryTime || 0);
      if (!ib || !Number.isInteger(Number(ib.id)) || Number(ib.id) < 1) { this.toast('error', 'Select a valid inbound first.'); return; }
      if (!email) { this.toast('error', 'Enter a client label / email first.'); return; }
      if (email.length > 128 || /[\r\n\0]/.test(email)) { this.toast('error', 'Client label must be 1–128 characters on a single line.'); return; }
      if (!Number.isSafeInteger(totalGB) || totalGB < 0) { this.toast('error', 'Traffic limit must be a whole number of GB; 0 means unlimited.'); return; }
      if (!Number.isFinite(limitIp) || limitIp < 0 || !Number.isInteger(limitIp) || limitIp > 10000) { this.toast('error', 'IP/device limit must be a whole number between 0 and 10000.'); return; }
      if (!Number.isFinite(expiryTime) || expiryTime < 0) { this.toast('error', 'Expiry must be zero (no expiry) or a valid future timestamp.'); return; }
      const days = Number(this.clientWizard?.expiryDays || 0);
      const body = { inboundId: Number(ib.id), ...this.newClient, email, totalGB, limitIp, expiryTime, days: days > 0 ? days : undefined, subscriptionAddressType: this.newClient.subscriptionAddressType || 'ipv4', subscriptionEnabled: this.newClient.subscriptionEnabled !== false, subscriptionAutoUpdate: this.newClient.subscriptionAutoUpdate !== false };
      if (this.me && this.me.role === 'reseller' && Number(this.newClient.totalGB||0) > 0) {
        const q = await this.quoteWholesale(this.newClient.totalGB);
        if (q && !q.sufficient) {
          this.toast('error', 'اعتبار ناکافی برای عمده: نیاز ' + q.cost + ' (موجودی ' + q.creditBalance + ')');
          return;
        }
        if (q && q.cost > 0 && !confirm('هزینه عمده این کلاینت: ' + q.cost + ' (هر GB ' + q.pricePerGB + '). ادامه؟')) return;
      }

      this.clientSaving = true;
      let data;
      try { data = await this.api('/clients', { method: 'POST', body: JSON.stringify(body) }); }
      catch (e) { this.toast('error', e?.message || 'Failed to create client'); return; }
      finally { this.clientSaving = false; }
      if (data && data.success) {
        this.newClient = { email: '', totalGB: 0, limitIp: 0, expiryTime: 0, subscriptionAddressType: 'ipv4', subscriptionEnabled: true, subscriptionAutoUpdate: true };
        this.clientWizard = { show: false, inboundId: 0, step: 1, expiryDays: 0 };
        this.toast('success', 'Client added');
        await this.refreshExpandedInbound();
      } else {
        this.toast('error', (data && data.msg) || 'Failed to create client');
      }
    },
    toggleClientSelection(id) { this.selectedClientIds=this.selectedClientIds.includes(id)?this.selectedClientIds.filter(x=>x!==id):[...this.selectedClientIds,id]; },
    async bulkClientAction(action) { if(!this.selectedClientIds.length)return; const d=await this.api('/clients/bulk-action',{method:'POST',body:JSON.stringify({ids:this.selectedClientIds,action})}); if(d?.success){this.toast('success',`Bulk client ${action} completed`);this.selectedClientIds=[];this.refreshExpandedInbound()}else this.toast('error',d?.msg||'Bulk client action failed'); },
    async loadClientGroups() {
      const d = await this.api('/clients/groups');
      if (d?.success) this.clientGroups = d.obj || [];
    },
    async bulkSetClientGroup() {
      if (!this.selectedClientIds.length) return this.toast('error', 'ابتدا چند کلاینت را انتخاب کنید');
      const existing = (this.clientGroups || []).filter(g => g.name && g.name !== '(ungrouped)').map(g => g.name);
      const hint = existing.length ? '\nگروه‌های موجود: ' + existing.slice(0, 12).join(' · ') : '';
      const name = prompt('نام گروه را وارد کنید (برای حذف از گروه خالی بگذارید):' + hint, existing[0] || '');
      if (name === null) return;
      const d = await this.api('/clients/bulk-action', {
        method: 'POST',
        body: JSON.stringify({ ids: this.selectedClientIds, action: 'set_group', group: (name || '').trim() })
      });
      if (d?.success) {
        this.toast('success', (d.changed || this.selectedClientIds.length) + ' کلاینت به گروه «' + ((name||'').trim() || 'بدون گروه') + '» اضافه شد');
        this.selectedClientIds = [];
        await this.loadClientGroups();
        this.refreshExpandedInbound();
      } else this.toast('error', d?.msg || 'تنظیم گروه ناموفق بود');
    },
    async renameClientGroup(fromName) {
      const to = prompt('نام جدید گروه «' + fromName + '»:', fromName === '(ungrouped)' ? '' : fromName);
      if (to === null) return;
      const d = await this.api('/clients/groups/rename', {
        method: 'POST',
        body: JSON.stringify({ from: fromName, to: (to || '').trim() })
      });
      if (d?.success) {
        this.toast('success', 'گروه تغییر نام داد (' + (d.changed||0) + ' کلاینت)');
        await this.loadClientGroups();
        this.refreshExpandedInbound();
      } else this.toast('error', d?.msg || 'تغییر نام ناموفق');
    },
    filteredInboundClients(ib) {
      const list = ib.clients || [];
      const f = (this.clientGroupFilter || '').trim();
      if (!f) return list;
      if (f === '(ungrouped)') return list.filter(c => !c.group);
      return list.filter(c => (c.group || '') === f);
    },
    async createBulkClients(ib) {
      const bulkDays = Number(this.bulk.days || 0);
      if (!Number.isInteger(bulkDays) || bulkDays < 0 || bulkDays > 3650) { this.toast('error', 'Days must be a whole number between 0 and 3650 (0 = never expires).'); return; }
      const body = { inboundId: ib.id, count: this.bulk.count, prefix: this.bulk.prefix, totalGB: this.bulk.totalGB, days: bulkDays };
      const data = await this.api('/clients/bulk', { method: 'POST', body: JSON.stringify(body) });
      if (data && data.success) {
        this.bulkOpenFor = null;
        this.toast('success', `${(data.obj || []).length} clients created`);
        this.refreshExpandedInbound();
      } else {
        this.toast('error', (data && data.msg) || 'Bulk create failed');
      }
    },
    async deleteClient(cl) {
      await this.api(`/clients/${cl.id}/delete`, { method: 'POST' });
      this.toast('success', 'Client removed');
      this.refreshExpandedInbound();
    },
    async resetClientTraffic(cl) { const d=await this.api(`/clients/${cl.id}/reset`, { method: 'POST' }); if(d?.success){this.toast('success','Traffic reset for '+cl.email);this.refreshExpandedInbound()}else this.toast('error',d?.msg||'Traffic reset failed'); },
    async rotateClientSecret(cl) { this.confirmAction('Rotate client secret?', 'The current UUID/password will stop working after runtime reload.', async () => { const d=await this.api(`/clients/${cl.id}/rotate-secret`,{method:'POST'}); if(d?.success){this.toast('success','Client secret rotated');this.refreshExpandedInbound()}else this.toast('error',d?.msg||'Secret rotation failed'); }); },
    openEditClient(cl) {
      const localDate = (ts) => {
        const d = new Date(ts);
        if (isNaN(d.getTime())) return '';
        const y = d.getFullYear();
        const m = String(d.getMonth() + 1).padStart(2, '0');
        const day = String(d.getDate()).padStart(2, '0');
        return y + '-' + m + '-' + day;
      };
      this.editClient = {
        id: cl.id, email: cl.email,
        totalGB: Number(cl.totalGB || 0),
        limitIp: Number(cl.limitIp || 0),
        days: '',
        expiryDate: cl.expiryTime ? localDate(cl.expiryTime) : '',
        enable: cl.enable !== false,
        group: cl.group || '',
        autoExtendGB: Number(cl.autoExtendGB || 0),
        autoExtendDays: Number(cl.autoExtendDays || 0),
        subscriptionEnabled: cl.subscriptionEnabled !== false,
        subscriptionName: cl.subscriptionName || '',
        subscriptionGroup: cl.subscriptionGroup || '',
        subscriptionRefreshMin: Number(cl.subscriptionRefreshMin || 15),
        subscriptionAddressType: cl.subscriptionAddressType || 'ipv4',
        subscriptionAutoUpdate: cl.subscriptionAutoUpdate !== false,
        subscriptionSmart: !!cl.subscriptionSmart,
      };
      this.showEditClientModal = true;
    },
    async saveClientEdit() {
      const ec = this.editClient;
      if (!ec?.id) return;
      const totalGB = Number(ec.totalGB || 0);
      const limitIp = Number(ec.limitIp || 0);
      if (!Number.isFinite(totalGB) || totalGB < 0 || !Number.isInteger(totalGB)) {
        this.toast('error', 'حجم باید عدد صحیح GB باشد (0 = نامحدود)'); return;
      }
      if (!Number.isFinite(limitIp) || limitIp < 0 || !Number.isInteger(limitIp) || limitIp > 10000) {
        this.toast('error', 'تعداد دستگاه باید بین 0 و 10000 باشد'); return;
      }
      const body = {
        enable: !!ec.enable,
        totalGB,
        limitIp,
        group: ec.group || '',
        subscriptionEnabled: ec.subscriptionEnabled !== false,
        subscriptionName: ec.subscriptionName || '',
        subscriptionGroup: ec.subscriptionGroup || '',
        subscriptionRefreshMin: Number(ec.subscriptionRefreshMin || 15),
        subscriptionAddressType: ec.subscriptionAddressType || 'ipv4',
        subscriptionAutoUpdate: ec.subscriptionAutoUpdate !== false,
        subscriptionSmart: !!ec.subscriptionSmart,
        autoExtendGB: Number(ec.autoExtendGB || 0),
        autoExtendDays: Number(ec.autoExtendDays || 0),
      };
      // Days from now win over date. Blank days = use date field (or clear if date empty).
      if (ec.days !== '' && ec.days != null) {
        const d = Number(ec.days);
        if (!Number.isInteger(d) || d < 0 || d > 3650) {
          this.toast('error', 'روز باید عدد صحیح 0 تا 3650 باشد (0 = بدون انقضا)'); return;
        }
        body.days = d;
      } else {
        body.expiryTime = ec.expiryDate ? (function(d){
          const p = String(d).split('-').map(Number);
          if (p.length !== 3 || !p[0]) return 0;
          return new Date(p[0], p[1]-1, p[2], 23, 59, 59, 999).getTime();
        })(ec.expiryDate) : 0;
      }
      const data = await this.api(`/clients/${ec.id}/update`, { method: 'POST', body: JSON.stringify(body) });
      if (data && data.success) {
        this.showEditClientModal = false;
        this.toast('success', 'کلاینت ذخیره شد');
        await this.refreshExpandedInbound();
      } else {
        this.toast('error', (data && data.msg) || 'Failed to update client');
      }
    },
    async manageClientProfiles(cl){
      const list=(this.inbounds||[]).filter(x=>x.protocol==='vless'&&x.id!==cl.inboundId).map(x=>x.id+' = '+x.remark+' ['+x.network+'/'+x.security+']').join('\n');
      if(!list)return this.toast('error','No other VLESS inbound is available');
      const pick=prompt('Choose target VLESS inbound ID:\n'+list); if(!pick)return;
      const inboundId=Number(pick); if(!Number.isFinite(inboundId)||inboundId<=0)return this.toast('error','Invalid inbound ID');
      const name=prompt('Profile name (optional)', 'profile-'+inboundId)||('profile-'+inboundId);
      const d=await this.api('/clients/'+cl.id+'/profiles',{method:'POST',body:JSON.stringify({inboundId,name,priority:100})});
      if(d?.success){this.toast('success','Config profile added; subscription will include it');}else this.toast('error',d?.msg||'Could not add profile');
    },
    async clientDoctor(cl){
      const d=await this.api('/clients/'+cl.id+'/doctor');
      if(!d?.success)return this.toast('error',d?.msg||'Doctor failed');
      const bad=(d.obj?.checks||[]).filter(x=>!x.ok);
      this.toast(bad.length?'error':'success',bad.length?('Doctor: '+bad.map(x=>x.name+': '+x.message).join(' | ')):'Doctor: all checks passed');
    },
    async openClientLive(cl) {
      this.clientLive = { client: cl, inbound: {}, live: {}, link: '' };
      this.showClientLiveModal = true;
      await this.refreshClientLive();
      if (this.clientLiveTimer) clearInterval(this.clientLiveTimer);
      this.clientLiveTimer = setInterval(() => { if (this.showClientLiveModal && this.polling) this.refreshClientLive(); }, 5000);
    },
    async refreshClientLive() {
      const id = this.clientLive?.client?.id;
      if (!id) return;
      const data = await this.api(`/clients/${id}/live`);
      if (data && data.success) this.clientLive = data.obj || this.clientLive;
    },
    closeClientLive() {
      this.showClientLiveModal = false;
      if (this.clientLiveTimer) { clearInterval(this.clientLiveTimer); this.clientLiveTimer = null; }
    },
    async copyClientConfig() {
      const link = this.clientLive?.link || '';
      if (!link) return;
      await this.copyText(link, 'Config copied');
    },

    async showQr(cl, family='ipv4', ib=null) {
      if (!family || family === 'auto') { const mode=String(ib?.ipMode||'ipv4').toLowerCase(); family=mode==='ipv6'?'ipv6':(mode==='dual'?'ipv4':mode); }
      const data = await this.api(`/clients/${cl.id}/link?family=${encodeURIComponent(family)}`);
      if (!data || !data.success) { this.toast('error', data?.msg || 'Could not build link'); return; }
      family = data.obj.family || family;
      this.qrFamily = family;
      this.qrClientId = cl.id;
      this.qrClientEmail = cl.email || this.clientLive?.client?.email || '';
      this.qrLinks = data.obj.links || {[family]: data.obj.link};
      this.qrLink = data.obj.link || this.qrLinks[family] || '';
      this.qrSubscription = data.obj.subscription || '';
      this.qrSubscriptionLinks = data.obj.subscriptionLinks || {};
      this.showQrModal = true;
      this.$nextTick(() => {
        const el = document.getElementById('qrcode');
        el.innerHTML = '';
        if (this.qrLink) new QRCode(el, { text: this.qrLink, width: 180, height: 180 });
      });
    },
    async changeQrFamily(family) {
      const link = this.qrLinks?.[family];
      if (link) { this.qrFamily = family; this.qrLink = link; this.qrSubscription = this.qrSubscriptionLinks?.[family] || this.qrSubscription; this.$nextTick(() => { const el=document.getElementById('qrcode'); el.innerHTML=''; new QRCode(el,{text:this.qrLink,width:180,height:180}); }); return; }
      const clId = this.qrClientId || this.clientLive?.client?.id;
      if (!clId) return;
      await this.showQr({id:clId}, family);
    },
    async copyText(text, successMessage='Copied') {
      if (!text) { this.toast('error', 'Nothing to copy'); return false; }
      let done = false;
      // Async Clipboard API only exists in secure contexts and may be denied; fall back to execCommand.
      if (navigator.clipboard && window.isSecureContext) {
        try { await navigator.clipboard.writeText(text); done = true; } catch (_) { done = false; }
      }
      if (!done) {
        const ta = document.createElement('textarea');
        try {
          ta.value = text;
          ta.setAttribute('readonly', '');
          ta.style.position = 'fixed';
          ta.style.opacity = '0';
          document.body.appendChild(ta);
          ta.focus();
          ta.select();
          done = !!document.execCommand('copy');
        } catch (_) { done = false; }
        finally { ta.remove(); }
      }
      this.toast(done ? 'success' : 'error', done ? successMessage : 'Could not copy');
      return done;
    },
    async copyLink() { return this.copyText(this.qrLink, 'Config link copied'); },
    async copySubscriptionLink() { return this.copyText(this.qrSubscription, 'Subscription link copied'); },

    async showMobileDelivery(cl, ib) {
      // Prefer the client's own address type (ipv4 / domain / …), then inbound mode.
      let family = String(cl?.subscriptionAddressType || '').toLowerCase();
      if (!family || family === 'all' || family === 'dual') {
        const mode = String(ib?.ipMode || 'ipv4').toLowerCase();
        family = mode === 'dual' ? 'ipv4' : (mode === 'ipv6' ? 'ipv6' : 'ipv4');
      }
      const data = await this.api(`/clients/${cl.id}/link?family=${encodeURIComponent(family)}`);
      if (!data || !data.success) { this.toast('error', data?.msg || 'Could not build mobile subscription'); return; }
      const ua = navigator.userAgent || '';
      const platform = /iPhone|iPad|iPod/i.test(ua) ? 'ios' : (/Android/i.test(ua) ? 'android' : 'android');
      this.mobileDelivery = {
        show: true, platform, family, client: cl, inbound: ib,
        link: data.obj?.link || '', subscription: data.obj?.subscription || '',
        links: data.obj?.links || {}, showQr: false,
      };
      this.$nextTick(() => this.renderMobileSubscriptionQr());
    },
    async changeMobileAddress(addressType) {
      const cl=this.mobileDelivery.client||{};
      if (!cl.id) return;
      const data=await this.api(`/clients/${cl.id}/link?family=${encodeURIComponent(addressType)}`);
      if (!data || !data.success) { this.toast('error', data?.msg || 'Address type unavailable'); return; }
      this.mobileDelivery.family=addressType;
      this.mobileDelivery.link=data.obj?.link||'';
      this.mobileDelivery.subscription=data.obj?.subscription||'';
      this.mobileDelivery.links=data.obj?.links||{};
      if (this.mobileDelivery.showQr) this.$nextTick(()=>this.renderMobileSubscriptionQr());
    },
    setMobilePlatform(platform) { this.mobileDelivery.platform = platform; },
    async copyMobileSubscription() { return this.copyText(this.mobileDelivery.subscription, 'Subscription link copied'); },
    async copyMobileConfig() { return this.copyText(this.mobileDelivery.link, 'Config link copied'); }, async openV2BoxSubscription(){const u=this.mobileDelivery.subscription;if(!u)return;const deep='v2box://install-config?url='+encodeURIComponent(u);window.location.href=deep;setTimeout(()=>{window.open('https://apps.apple.com/us/app/v2box-v2ray-client/id6446814690','_blank','noopener,noreferrer')},1200);}, async shareMobileSubscription(){if(!this.mobileDelivery.subscription)return; if(navigator.share){try{await navigator.share({title:this.mobileDelivery.client.email||'Subscription',text:this.mobileDelivery.subscription,url:this.mobileDelivery.subscription})}catch(e){} }else await this.copyMobileSubscription();},
    renderMobileSubscriptionQr() {
      if (!this.mobileDelivery.showQr) return;
      const el = document.getElementById('mobile-sub-qrcode');
      if (!el) return;
      el.innerHTML = '';
      if (this.mobileDelivery.subscription) new QRCode(el, { text: this.mobileDelivery.subscription, width: 190, height: 190 });
    },
    toggleMobileSubscriptionQr() {
      this.mobileDelivery.showQr = !this.mobileDelivery.showQr;
      this.$nextTick(() => this.renderMobileSubscriptionQr());
    },

    // ---------- operations center ----------
    async loadOps() {
      const safe = async (path) => { try { return await this.api(path); } catch (e) { return null; } };
      const [h,s,set,t,n,r,st,cc,pf,rc] = await Promise.all([
        safe('/pro/ops/health'), safe('/pro/ops/system'), safe('/pro/ops/settings'),
        safe('/pro/ops/templates'), safe('/pro/ops/notifications'), safe('/pro/automation/rules'),
        safe('/pro/scheduler/tasks'), safe('/pro/ops/command-center'), safe('/pro/ops/preflight'), safe('/pro/ops/recovery')
      ]);
      if (h && h.success) this.ops.health = h.obj || {};
      if (s && s.success) this.ops.system = s.obj || {};
      if (set && set.success) {
        const o = set.obj || {};
        this.ops.settings.publicHost = o.publicHost || o['settings.publicHost'] || this.ops.settings.publicHost || '';
        this.ops.settings.timezone = o.timezone || o['settings.timezone'] || this.ops.settings.timezone || '';
        if (o.language || o['settings.language']) this.uiLang = o.language || o['settings.language'];
      }
      if (t && t.success) this.ops.templates = Array.isArray(t.obj) ? t.obj : [];
      if (n && n.success) this.ops.notifications = Array.isArray(n.obj) ? n.obj : [];
      if (r && r.success) this.ops.rules = Array.isArray(r.obj) ? r.obj : [];
      if (st && st.success) this.ops.tasks = Array.isArray(st.obj) ? st.obj : [];
      this.ops.commandCenter = (cc && cc.success && cc.obj) ? cc.obj : (this.ops.commandCenter || {});
      this.ops.preflight = (pf && pf.success && pf.obj) ? pf.obj : (this.ops.preflight || {});
      this.ops.recovery = (rc && rc.success && rc.obj) ? rc.obj : (this.ops.recovery || {});
    },
    async refreshV35Ops(){ await this.loadOps(); this.toast('success','Operations Center refreshed'); },
    async runV35Preflight(){ const d=await this.api('/pro/ops/preflight'); if(d?.success){this.ops.preflight=d.obj||{};this.toast(d.obj.ready?'success':'info',d.obj.ready?'Preflight passed':'Preflight found items to review')} else this.toast('error',d?.msg||'Preflight failed'); },
    async verifyV35Recovery(){ const d=await this.api('/pro/ops/recovery'); if(d?.success){this.ops.recovery=d.obj||{};this.toast(d.obj.verified?'success':'info',d.obj.verified?'Latest backup verified':'Recovery check needs attention')} else this.toast('error',d?.msg||'Recovery check failed'); },
    async opsService(action){const d=await this.api('/pro/ops/service/'+action,{method:'POST'});if(d&&d.success){this.toast('success','Service action completed');this.loadOps();this.refreshOverview()}else this.toast('error',(d&&d.msg)||'Service action failed')},
    async validateConfig(kind){const d=await this.api('/pro/ops/validate',{method:'POST',body:JSON.stringify({kind})});if(d&&d.success)this.toast('success',kind+' configuration is valid');else this.toast('error',(d&&d.msg)||'Configuration validation failed')},
    async runDiagnostics(){const q=new URLSearchParams({host:this.ops.diag.host,port:String(this.ops.diag.port||443)});const d=await this.api('/pro/ops/diagnostics?'+q.toString());if(d&&d.success){this.ops.diag.result=d.obj;this.toast('success','Diagnostics completed')}else this.toast('error',(d&&d.msg)||'Diagnostics failed')},
    async saveOpsSettings(){
      const host = String(this.ops.settings.publicHost || '').trim();
      const tz = String(this.ops.settings.timezone || '').trim();
      const d = await this.api('/pro/ops/settings', { method: 'POST', body: JSON.stringify({ publicHost: host, timezone: tz }) });
      if (!(d && d.success)) { this.toast('error', (d && d.msg) || this.t('settings.saveFail')); return; }
      // Apply host to live panel config (settings table alone does not drive share links)
      if (host) {
        const isV4 = /^\d{1,3}(?:\.\d{1,3}){3}$/.test(host);
        const apply = isV4
          ? await this.api('/pro/ops/public-host/ipv4', { method: 'POST', body: JSON.stringify({ ipv4: host }) })
          : await this.api('/pro/ops/public-host/domain', { method: 'POST', body: JSON.stringify({ domain: host }) });
        if (!(apply && apply.success)) {
          this.toast('error', (apply && apply.msg) || 'تنظیمات ذخیره شد ولی اعمال Public Host ناموفق بود');
          return;
        }
        if (apply.obj) this.ops.publicHostStatus = apply.obj;
      }
      this.toast('success', this.t('settings.saved'));
      try { await this.loadPublicHostStatus(); } catch (_) {}
    },
    async createTemplatePrompt(){const name=prompt('Template name');if(!name)return;const protocol=prompt('Protocol (vless/vmess/trojan/hysteria2/tuic/wireguard)','vless')||'vless';const network=prompt('Network (tcp/xhttp/httpupgrade/ws/grpc)','xhttp')||'xhttp';const security=prompt('Security (none/tls/reality)','tls')||'tls';const payload=prompt('JSON payload for advanced defaults','{}')||'{}';const d=await this.api('/pro/ops/templates',{method:'POST',body:JSON.stringify({name,protocol,network,security,payload})});if(d&&d.success){this.toast('success','Template created');this.loadOps()}else this.toast('error',(d&&d.msg)||'Template creation failed')},
    async deleteTemplate(t){const d=await this.api('/pro/ops/templates/'+t.id,{method:'DELETE'});if(d&&d.success){this.toast('success','Template deleted');this.loadOps()}},
    async createNotificationPrompt(){const name=prompt('Rule name');if(!name)return;const event=prompt('Event (client_expiry/quota/server_down/certificate_expiry)','server_down')||'server_down';const channel=prompt('Channel (telegram/webhook)','telegram')||'telegram';const target=prompt('Target (chat id or webhook URL)','')||'';const d=await this.api('/pro/ops/notifications',{method:'POST',body:JSON.stringify({name,event,channel,target,enabled:true})});if(d&&d.success){this.toast('success','Notification rule created');this.loadOps()}else this.toast('error',(d&&d.msg)||'Could not create rule')},
    async deleteNotification(n){const d=await this.api('/pro/ops/notifications/'+n.id,{method:'DELETE'});if(d&&d.success){this.toast('success','Rule deleted');this.loadOps()}},
    async createAutomationRule(){const name=prompt('Rule name');if(!name)return;const event=prompt('Event: node_health / client_expiry / certificate_expiry / always','node_health')||'node_health';const action=prompt('Action: notify_telegram / refresh_alerts / cleanup_sessions / snapshot_usage','notify_telegram')||'notify_telegram';const condition=prompt('Condition JSON','{\"staleSeconds\":90}')||'{}';const payload=prompt('Action payload JSON','{\"message\":\"ElectruVPN automation alert\"}')||'{}';const d=await this.api('/pro/automation/rules',{method:'POST',body:JSON.stringify({name,enabled:true,event,condition,action,payload,cooldownSec:300})});if(d?.success){this.toast('success','Automation rule created');this.loadOps()}else this.toast('error',d?.msg||'Could not create automation rule')},
    async deleteAutomationRule(r){const d=await this.api('/pro/automation/rules/'+r.id,{method:'DELETE'});if(d?.success){this.toast('success','Automation rule deleted');this.loadOps()}},
    async createScheduledTask(){const name=prompt('Task name');if(!name)return;const schedule=prompt('Schedule: @every 15m / @hourly / @daily','@every 15m')||'@every 15m';const kind=prompt('Task: snapshot_usage / cleanup_sessions','snapshot_usage')||'snapshot_usage';const d=await this.api('/pro/scheduler/tasks',{method:'POST',body:JSON.stringify({name,enabled:true,schedule,kind,payload:'{}'})});if(d?.success){this.toast('success','Scheduled task created');this.loadOps()}else this.toast('error',d?.msg||'Could not create scheduled task')},
    async deleteScheduledTask(t){const d=await this.api('/pro/scheduler/tasks/'+t.id,{method:'DELETE'});if(d?.success){this.toast('success','Scheduled task deleted');this.loadOps()}},
    async runSchedulerNow(){const d=await this.api('/pro/scheduler/run-now',{method:'POST'});if(d?.success){this.toast('success','Scheduler tick executed');this.loadOps()}else this.toast('error',d?.msg||'Scheduler failed')},

    // ---------- subscription command center ----------
    ensureSubscriptionState() {
      if (!this.subscription || typeof this.subscription !== 'object') {
        this.subscription = {
          clients: [], devices: [], groups: [], presets: [], overview: {}, logs: [], plans: [],
          inboundMap: {}, selected: null, search: '', filter: 'all', refreshing: false, chart: null,
        };
      }
      const s = this.subscription;
      if (!Array.isArray(s.clients)) s.clients = [];
      if (!Array.isArray(s.devices)) s.devices = [];
      if (!Array.isArray(s.groups)) s.groups = [];
      if (!Array.isArray(s.presets)) s.presets = [];
      if (!Array.isArray(s.logs)) s.logs = [];
      if (!Array.isArray(s.plans)) s.plans = [];
      if (!s.overview || typeof s.overview !== 'object') s.overview = {};
      if (!s.inboundMap || typeof s.inboundMap !== 'object') s.inboundMap = {};
      if (typeof s.search !== 'string') s.search = '';
      if (typeof s.filter !== 'string') s.filter = 'all';
      if (typeof s.refreshing !== 'boolean') s.refreshing = false;
    },
    async loadSubscriptionDashboard(force=false) {
      this.ensureSubscriptionState();
      if (this.subscription.refreshing && !force) return;
      this.subscription.refreshing = true;
      try {
        const safe = async (path) => {
          try { return await this.api(path); } catch (e) { return { success: false, msg: String(e) }; }
        };
        const [inb, devices, groups, presets, overview, logs, plans] = await Promise.all([
          safe('/inbounds?page=1&pageSize=100'),
          safe('/pro/devices'),
          safe('/pro/subscriptions/groups'),
          safe('/pro/subscriptions/presets'),
          safe('/pro/subscriptions/analytics/overview'),
          safe('/pro/subscriptions/access-log?limit=80'),
          safe('/pro/plans')
        ]);
        const inboundRows = inb?.success ? (inb.obj || []) : [];
        const inboundMap = Object.fromEntries(inboundRows.map(x => [x.id, x]));
        const clients = [];
        // Subscription center no longer depends on the inbound list preloading clients.
        // Pull client pages directly, so a large inbound does not force a giant nested payload.
        let inboundCursor=100; while(inboundCursor<Number(inb?.total||inboundRows.length)){ const pg=await this.api('/inbounds?page='+Math.floor(inboundCursor/100+1)+'&pageSize=100'); if(!pg?.success)break; (pg.obj||[]).forEach(x=>{inboundMap[x.id]=x;}); inboundCursor+=100; if(!pg.hasMore)break; }
        const firstClients = await this.api('/clients?limit=500&offset=0');
        if(firstClients?.success){
          const pushClient = (cl) => {
            const ib = inboundMap[cl.inboundId];
            clients.push({
              ...cl,
              inboundRemark: ib?.remark || ('inbound#'+(cl.inboundId||'?')),
              inbound: ib || null,
              protocol: ib?.protocol || cl.protocol || '—',
              ipMode: ib?.ipMode || 'ipv4',
            });
          };
          (firstClients.data||[]).forEach(pushClient);
          // The current API exposes pagination metadata; continue only when another page exists.
          const total=Number(firstClients.pagination?.total||0), limit=Number(firstClients.pagination?.limit||500);
          for(let offset=limit; offset<total && offset<10000; offset+=limit){
            const pg=await this.api('/clients?limit='+limit+'&offset='+offset);
            if(!pg?.success) break;
            (pg.data||[]).forEach(pushClient);
          }
        }
        this.subscription.clients = clients;
        this.subscription.devices = devices?.success ? (devices.obj || devices.data || []) : [];
        this.subscription.groups = groups?.success ? (groups.obj || groups.data || []) : [];
        this.subscription.presets = presets?.success ? (presets.obj || presets.data || []) : [];
        this.subscription.overview = overview?.success ? (overview.obj || {}) : {};
        this.subscription.logs = logs?.success ? (logs.obj || logs.data || []) : [];
        this.subscription.plans = plans?.success ? (plans.obj || plans.data || []) : [];
        this.subscription.inboundMap = inboundMap;
        // Keep selection stable across refresh
        if (this.subscription.selected && this.subscription.selected.id) {
          const keep = clients.find(c => Number(c.id) === Number(this.subscription.selected.id));
          this.subscription.selected = keep || null;
        }
        this.$nextTick(() => this.renderSubscriptionChart());
      } catch (e) {
        console.error('loadSubscriptionDashboard', e);
        try { this.toast && this.toast('error', 'Subscription dashboard failed to load'); } catch (_) {}
      } finally { this.subscription.refreshing = false; }
    },
    subscriptionClients() {
      this.ensureSubscriptionState();
      const q = (this.subscription.search || '').trim().toLowerCase();
      return this.subscription.clients.filter(cl => {
        const status = this.subscriptionClientStatus(cl);
        const matchStatus = this.subscription.filter === 'all' || status === this.subscription.filter;
        const hay = [cl.email, cl.subscriptionName, cl.subscriptionGroup, cl.inboundRemark, cl.protocol, cl.subscriptionAddressType].join(' ').toLowerCase();
        return matchStatus && (!q || hay.includes(q));
      });
    },
    subscriptionDevices(cl) { return this.subscription.devices.filter(d => d.clientId === cl.id); },
    subscriptionDeviceOnline(d) { return !!d && new Date(d.lastSeenAt).getTime() >= Date.now()-5*60*1000; },
    subscriptionDeviceModel(d) { if(!d)return 'Unknown device'; return [d.manufacturer,d.model].filter(Boolean).join(' ') || (d.name && d.name!==d.deviceId ? d.name : 'Unknown device'); },
    subscriptionDeviceIcon(d) { const os=String(d?.os||'').toLowerCase(); if(os.includes('android'))return '◈'; if(os.includes('ios')||os.includes('ipados'))return ''; if(os.includes('windows'))return '▣'; if(os.includes('macos'))return '⌘'; if(os.includes('linux'))return '◉'; return '▱'; },
    subscriptionDeviceMeta(d) { if(!d)return ''; return [d.os && (d.osVersion ? d.os+' '+d.osVersion : d.os),d.architecture].filter(Boolean).join(' · ') || 'OS not reported'; },
    subscriptionPhoneModels() { const out=[]; const seen=new Set(); this.subscription.devices.forEach(d=>{const os=String(d?.os||'').toLowerCase();if(os.includes('android')||os.includes('ios')||os.includes('ipados')){const m=this.subscriptionDeviceModel(d);if(m && m!=='Unknown device' && !seen.has(m)){seen.add(m);out.push(m);}}});return out; },
    subscriptionOnlineDevices(cl) { const cutoff = Date.now() - 5*60*1000; return this.subscriptionDevices(cl).filter(d => d.active !== false && new Date(d.lastSeenAt).getTime() >= cutoff); },
    subscriptionClientStatus(cl) {
      if (!cl.enable || cl.subscriptionStatus === 'revoked' || cl.subscriptionStatus === 'disabled' || cl.subscriptionEnabled === false) return 'offline';
      if (cl.expiryTime && Number(cl.expiryTime) < Date.now()) return 'expired';
      if (cl.totalGB && (Number(cl.up||0)+Number(cl.down||0)) >= Number(cl.totalGB)*1024*1024*1024) return 'quota';
      return this.subscriptionOnlineDevices(cl).length ? 'online' : 'offline';
    },
    subscriptionStats() {
      this.ensureSubscriptionState();
      const cs=this.subscription.clients;
      const online=cs.filter(c=>this.subscriptionClientStatus(c)==='online').length;
      const expired=cs.filter(c=>this.subscriptionClientStatus(c)==='expired').length;
      const active=cs.filter(c=>{const expired=!!c.expiryTime&&Number(c.expiryTime)<Date.now();return c.subscriptionEnabled!==false&&c.subscriptionStatus==='active'&&!expired&&c.enable!==false;}).length;
      const up=cs.reduce((n,c)=>n+Number(c.up||0),0), down=cs.reduce((n,c)=>n+Number(c.down||0),0);
      const devices=this.subscription.devices.length, onlineDevices=this.subscription.devices.filter(d=>d.active!==false && new Date(d.lastSeenAt).getTime()>=Date.now()-5*60*1000).length;
      let first=null,last=null;
      this.subscription.devices.forEach(d=>{const a=new Date(d.createdAt),b=new Date(d.lastSeenAt);if(!first||a<first)first=a;if(!last||b>last)last=b;});
      return {total:cs.length,online,active,expired,up,down,totalTraffic:up+down,devices,onlineDevices,firstOnline:first,lastOnline:last};
    },
    subscriptionClientPlan(cl) { return this.subscription.plans.find(p=>Number(p.id)===Number(cl.planId)) || null; },
    subscriptionPurchaseDate(cl) { return cl.purchasedAt || cl.createdAt || null; },
    subscriptionExpiryDate(cl) { return cl.expiryTime ? new Date(Number(cl.expiryTime)) : null; },
    subscriptionPercent(cl) { const limit=Number(cl.totalGB||0)*1024*1024*1024; if(!limit)return 0; return Math.min(100,Math.round((Number(cl.up||0)+Number(cl.down||0))/limit*100)); },
    subscriptionFmtDate(v) { if(!v)return 'Never'; const d=new Date(v); return Number.isNaN(d.getTime())?'—':d.toLocaleString([], {year:'numeric',month:'short',day:'2-digit',hour:'2-digit',minute:'2-digit'}); },
    subscriptionFmtBytes(v) { const n=Number(v||0); if(!n)return '0 B'; const u=['B','KB','MB','GB','TB']; const i=Math.min(u.length-1,Math.floor(Math.log(n)/Math.log(1024))); return (n/Math.pow(1024,i)).toFixed(i?1:0)+' '+u[i]; },
    subscriptionAddressLabel(v) { return ({ipv4:'IPv4',ipv6:'IPv6',domain:'Domain',dual:'Dual',all:'All'})[v] || 'All'; },
    subscriptionAddressCounts() { const out={ipv4:0,ipv6:0,domain:0,dual:0,all:0}; this.subscription.clients.forEach(c=>{const k=c.subscriptionAddressType||'ipv4';out[k]=(out[k]||0)+1;}); return out; },
    subscriptionGroupCount(name) { return this.subscription.clients.filter(c=>c.subscriptionGroup===name).length; },
    subscriptionPlanLabel(cl) { const p=this.subscriptionClientPlan(cl); return p ? p.name : 'Custom / Unassigned'; },
    subscriptionLinkFor(cl, family) {
      const base=window.location.origin+(window.__ELECTRU_BASE_PATH__||'');
      return base+'/sub/'+encodeURIComponent(cl.subId)+'?family='+encodeURIComponent(family || cl.subscriptionAddressType || 'ipv4');
    },
    copyClientLink(cl) {
      if (!cl) return;
      return this.subscriptionCopy(cl, 'subscription');
    },
    async subscriptionCopy(cl, kind='subscription') {
      const family=cl.subscriptionAddressType||'ipv4';
      if(kind==='config') { const d=await this.api('/clients/'+cl.id+'/link?family='+encodeURIComponent(family)); const link=d?.obj?.link||d?.link||''; if(link)return this.copyText(link,'Config copied'); }
      return this.copyText(this.subscriptionLinkFor(cl,family),'Subscription link copied');
    },
    async subscriptionOpenInNpv(cl) {
      if(!cl?.id) return;
      const family=cl.subscriptionAddressType||'ipv4';
      const d=await this.api('/clients/'+cl.id+'/link?family='+encodeURIComponent(family));
      const link=d?.obj?.link||d?.link||'';
      if(!link){ this.toast('error','Could not generate config'); return; }
      const copied=await this.copyText(link,'Config copied — opening Npv Tunnel');
      if(!copied){ this.toast('error','Could not copy config'); return; }
      const ua=navigator.userAgent||'';
      const isAndroid=/Android/i.test(ua);
      const isIOS=/iPhone|iPad|iPod/i.test(ua) || (navigator.platform==='MacIntel' && navigator.maxTouchPoints>1);
      if(isAndroid){
        // Android: copy first, then launch the installed Npv Tunnel package.
        // Npv Tunnel supports clipboard/link imports; some Android releases auto-detect
        // the copied URI when the app opens, while others require one Import from Clipboard tap.
        const fallback='https://play.google.com/store/apps/details?id=com.napsternetlabs.napsternetv';
        const intent='intent:#Intent;action=android.intent.action.MAIN;category=android.intent.category.LAUNCHER;package=com.napsternetlabs.napsternetv;S.browser_fallback_url='+encodeURIComponent(fallback)+';end';
        window.location.href=intent;
        window.setTimeout(()=>this.toast('info','Npv Tunnel opened. If your version does not auto-import, choose Configs → + → Import from Clipboard.'),1600);
        return;
      }
      if(isIOS){
        // No public Npv Tunnel custom URL scheme was found; use the native share sheet when available.
        if(navigator.share){
          try { await navigator.share({title:'Npv Tunnel Config',text:link}); return; } catch(e) {}
        }
        window.setTimeout(()=>this.toast('info','Config copied. Open Npv Tunnel → Configs → + → Import from Clipboard.'),350);
        return;
      }
      window.setTimeout(()=>this.toast('info','Config copied. Open Npv Tunnel and import it from Clipboard.'),350);
    },
    async subscriptionRotate(cl) { const d=await this.api('/pro/clients/'+cl.id+'/subscription/rotate',{method:'POST'}); if(d?.success){this.toast('success','Subscription token rotated');await this.loadSubscriptionDashboard(true)} else this.toast('error',d?.msg||'Could not rotate token'); },
    async subscriptionToggle(cl) { const disable=cl.subscriptionEnabled!==false; const d=await this.api('/clients/'+cl.id+'/update',{method:'POST',body:JSON.stringify({subscriptionEnabled:!disable})}); if(d?.success){this.toast('success',disable?'Subscription disabled':'Subscription enabled');await this.loadSubscriptionDashboard(true)}else this.toast('error',d?.msg||'Could not change subscription'); },
    async subscriptionSetOption(cl, key, value) { const body={}; body[key]=!!value; const d=await this.api('/clients/'+cl.id+'/update',{method:'POST',body:JSON.stringify(body)}); if(d?.success){cl[key]=!!value;this.toast('success','Subscription option updated')}else this.toast('error',d?.msg||'Could not update subscription option'); },
    async subscriptionRevoke(cl) { const d=await this.api('/pro/clients/'+cl.id+'/subscription/revoke',{method:'POST'}); if(d?.success){this.toast('success','Subscription revoked');await this.loadSubscriptionDashboard(true)}else this.toast('error',d?.msg||'Could not revoke subscription'); },
    async subscriptionEnable(cl) { const d=await this.api('/pro/clients/'+cl.id+'/subscription/enable',{method:'POST'}); if(d?.success){this.toast('success','Subscription enabled');await this.loadSubscriptionDashboard(true)}else this.toast('error',d?.msg||'Could not enable subscription'); },
    async subscriptionApplyPreset(cl,p) { const d=await this.api('/pro/clients/'+cl.id+'/subscription/apply-preset',{method:'POST',body:JSON.stringify({presetId:p.id})}); if(d?.success){this.toast('success','Preset applied');await this.loadSubscriptionDashboard(true)}else this.toast('error',d?.msg||'Could not apply preset'); },
    async subscriptionSavePolicy(cl) { const body={gracePeriodHours:Number(cl.gracePeriodHours||0),maxActiveSessions:Number(cl.maxActiveSessions||0),deviceBindingEnabled:!!cl.deviceBindingEnabled,newDeviceApproval:!!cl.newDeviceApproval,linkLeakThreshold:Number(cl.linkLeakThreshold||0),primaryAddressType:cl.primaryAddressType||cl.subscriptionAddressType||'domain',renewalAuto:!!cl.renewalAuto,renewalDays:Number(cl.renewalDays||0)}; const d=await this.api('/pro/clients/'+cl.id+'/subscription/policy',{method:'POST',body:JSON.stringify(body)}); if(d?.success){Object.assign(cl,body);this.toast('success','Subscription policy saved')}else this.toast('error',d?.msg||'Could not save policy'); },
    async subscriptionRenew(cl) { const days=Number(cl.renewalDays||30); const d=await this.api('/pro/clients/'+cl.id+'/subscription/renew',{method:'POST',body:JSON.stringify({days})}); if(d?.success){cl.expiryTime=d.obj.expiryTime;cl.renewalGraceUsed=false;this.toast('success','Subscription renewed');await this.loadSubscriptionDashboard(true)}else this.toast('error',d?.msg||'Could not renew subscription'); },
    async subscriptionDeviceAction(d, action) { const endpoint=action==='approve'?'/pro/devices/'+d.id+'/approve':'/pro/devices/'+d.id+'/revoke'; const r=await this.api(endpoint,{method:'POST'}); if(r?.success){this.toast('success',action==='approve'?'Device approved':'Device revoked');await this.loadSubscriptionDashboard(true)}else this.toast('error',r?.msg||'Device action failed'); },
    subscriptionShowClient(cl) { this.subscription.selected=cl; this.$nextTick(()=>this.renderSubscriptionChart()); },
    subscriptionClearClient() { this.subscription.selected=null; },
    renderSubscriptionChart() {
      this.ensureSubscriptionState();
      const el=document.getElementById('subscriptionTrafficChart'); if(!el||!window.Chart)return;
      const CH=(window.__electruCharts=window.__electruCharts||{}); if(CH.sub){try{CH.sub.destroy()}catch(e){} CH.sub=null;}
      const cs=this.subscription.selected?[this.subscription.selected]:(this.subscription.clients||[]);
      if(!cs.length){ try{ const ctx=el.getContext('2d'); ctx&&ctx.clearRect(0,0,el.width,el.height);}catch(e){} return; }
      const days=[]; const up=[],down=[]; for(let i=6;i>=0;i--){const d=new Date();d.setDate(d.getDate()-i);days.push(d.toLocaleDateString([], {month:'short',day:'numeric'}));}
      // Client model has cumulative traffic; keep the chart honest by showing a smooth current usage profile rather than inventing historical values.
      const totalUp=cs.reduce((n,c)=>n+Number(c.up||0),0), totalDown=cs.reduce((n,c)=>n+Number(c.down||0),0);
      for(let i=0;i<7;i++){const f=(i+1)/7;up.push(Math.round(totalUp*f));down.push(Math.round(totalDown*f));}
      CH.sub=new Chart(el,{type:'line',data:{labels:days,datasets:[{label:'Upload',data:up,borderColor:'#6ee7ff',backgroundColor:'rgba(110,231,255,.08)',fill:true,tension:.4},{label:'Download',data:down,borderColor:'#a78bfa',backgroundColor:'rgba(167,139,250,.08)',fill:true,tension:.4}]},options:{responsive:true,maintainAspectRatio:false,plugins:{legend:{labels:{color:'#9aa7bd',boxWidth:10}}},scales:{x:{ticks:{color:'#74809a'},grid:{color:'rgba(255,255,255,.04)'}},y:{ticks:{color:'#74809a',callback:v=>this.subscriptionFmtBytes(v)},grid:{color:'rgba(255,255,255,.04)'}}}}});
    },
    // ---------- pro center ----------
    
    async loadTunnelChain() {
      const [d, hops] = await Promise.all([this.api('/pro/tunnel-chain'), this.api('/pro/tunnel-chain/hops')]);
      if (hops && hops.success) {
        const nodes = (hops.obj?.nodes || []).map(n => {
          const r = (hops.obj?.ranked || []).find(x => x.nodeId === n.id || x.NodeID === n.id);
          return { ...n, healthy: r ? (r.healthy !== false && r.Healthy !== false) : true };
        });
        this.tunnelChain.hopNodes = nodes;
      }
      if (!d || !d.success) return;
      const rows = d.obj || [];
      if (rows.length) {
        const r = rows.find(x => x.enabled) || rows[0];
        Object.assign(this.tunnelChain, {
          id: r.id, enabled: !!r.enabled,
          selectionMode: r.selectionMode || 'manual', autoStrategy: r.autoStrategy || 'least_load',
          midNodeId: r.midNodeId || 0, mid2NodeId: r.mid2NodeId || 0, exitNodeId: r.exitNodeId || 0,
          midAddress: r.midAddress || '', midPort: r.midPort || 443, midProtocol: r.midProtocol || 'vless',
          midUUID: r.midUUID || r.midPassword || '', midPublicKey: r.midPublicKey || '', midShortId: r.midShortId || '', midSni: r.midSni || '',
          mid2Address: r.mid2Address || '', mid2Port: r.mid2Port || 0, mid2Protocol: r.mid2Protocol || 'vless',
          mid2UUID: r.mid2UUID || r.mid2Password || '', mid2PublicKey: r.mid2PublicKey || '', mid2ShortId: r.mid2ShortId || '', mid2Sni: r.mid2Sni || '',
          exitAddress: r.exitAddress || '', exitPort: r.exitPort || 443, exitProtocol: r.exitProtocol || 'vless',
          exitUUID: r.exitUUID || r.exitPassword || '', exitPublicKey: r.exitPublicKey || '',
          exitShortId: r.exitShortId || '', exitSni: r.exitSni || '',
          rotateAddresses: r.rotateAddresses || '', rotateOnFail: r.rotateOnFail !== false, confirmBeforeFailover: r.confirmBeforeFailover !== false,
          activeExitHost: r.activeExitHost || r.exitAddress || '',
          activeMidHost: r.activeMidHost || r.midAddress || '',
          activeMid2Host: r.activeMid2Host || r.mid2Address || '',
          lastError: r.lastError || ''
        });
      }
    },
    onHopNodePick(which) {
      const idMap = { mid: 'midNodeId', mid2: 'mid2NodeId', exit: 'exitNodeId' };
      const id = Number(this.tunnelChain[idMap[which] || 'exitNodeId'] || 0);
      const n = (this.tunnelChain.hopNodes || []).find(x => Number(x.id) === id);
      if (!n) return;
      if (which === 'mid') {
        this.tunnelChain.midAddress = n.address || '';
        if (n.port) this.tunnelChain.midPort = n.port;
      } else if (which === 'mid2') {
        this.tunnelChain.mid2Address = n.address || '';
        if (n.port) this.tunnelChain.mid2Port = n.port;
      } else {
        this.tunnelChain.exitAddress = n.address || '';
        if (n.port) this.tunnelChain.exitPort = n.port;
      }
    },
    _hopCred(protocol, uuidField) {
      const ss = protocol === 'shadowsocks' || protocol === 'trojan' || protocol === 'wireguard';
      return { uuid: ss ? '' : uuidField, password: ss ? uuidField : '' };
    },
    async saveTunnelChain() {
      const tc = this.tunnelChain;
      const midCred = this._hopCred(tc.midProtocol, tc.midUUID);
      const mid2Cred = this._hopCred(tc.mid2Protocol, tc.mid2UUID);
      const exitCred = this._hopCred(tc.exitProtocol, tc.exitUUID);
      const body = {
        id: tc.id || 0, name: 'default-chain', enabled: !!tc.enabled,
        selectionMode: tc.selectionMode || 'manual', autoStrategy: tc.autoStrategy || 'least_load',
        midNodeId: Number(tc.midNodeId || 0), mid2NodeId: Number(tc.mid2NodeId || 0), exitNodeId: Number(tc.exitNodeId || 0),
        midAddress: tc.midAddress || '', midPort: Number(tc.midPort || 0), midProtocol: tc.midProtocol || 'vless',
        midUUID: midCred.uuid, midPassword: midCred.password,
        midPublicKey: tc.midPublicKey || '', midShortId: tc.midShortId || '', midSni: tc.midSni || '',
        midNetwork: 'tcp', midFingerprint: 'chrome',
        mid2Address: tc.mid2Address || '', mid2Port: Number(tc.mid2Port || 0), mid2Protocol: tc.mid2Protocol || 'vless',
        mid2UUID: mid2Cred.uuid, mid2Password: mid2Cred.password,
        mid2PublicKey: tc.mid2PublicKey || '', mid2ShortId: tc.mid2ShortId || '', mid2Sni: tc.mid2Sni || '',
        mid2Network: 'tcp', mid2Fingerprint: 'chrome',
        exitAddress: tc.exitAddress || '', exitPort: Number(tc.exitPort || 443), exitProtocol: tc.exitProtocol || 'vless',
        exitUUID: exitCred.uuid, exitPassword: exitCred.password,
        exitPublicKey: tc.exitPublicKey || '', exitShortId: tc.exitShortId || '', exitSni: tc.exitSni || '',
        exitNetwork: 'tcp', exitFingerprint: 'chrome',
        rotateAddresses: tc.rotateAddresses || '', rotateOnFail: !!tc.rotateOnFail, confirmBeforeFailover: !!tc.confirmBeforeFailover,
        activeExitHost: tc.activeExitHost || tc.exitAddress || '',
        activeMidHost: tc.activeMidHost || tc.midAddress || '',
        activeMid2Host: tc.activeMid2Host || tc.mid2Address || ''
      };
      const d = await this.api('/pro/tunnel-chain', { method: 'POST', body: JSON.stringify(body) });
      if (d && d.success) { this.toast('success', 'زنجیره Multi-Hop ذخیره و اعمال شد'); this.loadTunnelChain(); }
      else this.toast('error', (d && d.msg) || 'save failed');
    },
    async rotateTunnelChain() {
      if (!this.tunnelChain.id) return this.toast('error', 'save chain first');
      const d = await this.api('/pro/tunnel-chain/' + this.tunnelChain.id + '/rotate', { method: 'POST', body: '{}' });
      if (d && d.success) { this.toast('success', 'Rotated to ' + (d.obj?.activeExitHost || '')); this.loadTunnelChain(); }
      else this.toast('error', (d && d.msg) || 'rotate failed');
    },

async loadProAudit() {
      const f = this.pro.auditFilter || {};
      const q = new URLSearchParams();
      if (f.username) q.set('username', f.username);
      if (f.role) q.set('role', f.role);
      if (f.action) q.set('action', f.action);
      if (f.source) q.set('source', f.source);
      q.set('limit', '150');
      const d = await this.api('/pro/audit?' + q.toString());
      if (d && d.success) this.pro.audit = d.obj || [];
    },
    async loadPro() {
      const safe = async (path) => { try { return await this.api(path); } catch (e) { return { success: false }; } };
      const [sys,audit,nodes,keys,backups,users,subscriptionGroups,subscriptionPresets,subscriptionOverview,subscriptionLogs] = await Promise.all([
        safe('/pro/system'), safe('/pro/audit'), safe('/pro/nodes'), safe('/pro/api-keys'), safe('/pro/backups'),
        safe('/pro/users'), safe('/pro/subscriptions/groups'), safe('/pro/subscriptions/presets'),
        safe('/pro/subscriptions/analytics/overview'), safe('/pro/subscriptions/access-log?limit=50')
      ]);
      if (sys && sys.success) this.pro.system = sys.obj || {};
      if (audit && audit.success) this.pro.audit = audit.obj || [];
      if (nodes && nodes.success) this.pro.nodes = nodes.obj || [];
      if (keys && keys.success) this.pro.apiKeys = keys.obj || [];
      if (backups && backups.success) this.pro.backups = backups.obj || [];
      if (users && users.success) this.pro.users = users.obj || [];
      if (subscriptionGroups && subscriptionGroups.success) this.pro.subscriptionGroups = subscriptionGroups.obj || [];
      if (subscriptionPresets?.success) this.pro.subscriptionPresets = subscriptionPresets.obj || [];
      if (subscriptionOverview?.success) this.pro.subscriptionOverview = subscriptionOverview.obj || {};
      if (subscriptionLogs?.success) this.pro.subscriptionLogs = subscriptionLogs.obj || [];
    },

    
    openResellerEdit(r) {
      this.resellerEdit = {
        id: r.id, username: r.username,
        maxTotalGB: r.maxTotalGB||0, maxGBPerClient: r.maxGBPerClient||0, maxClients: r.maxClients||0,
        allowSubResellers: !!r.allowSubResellers, maxSubResellers: r.maxSubResellers||0,
        commissionPercent: r.commissionPercent||0, wholesalePricePerGB: r.wholesalePricePerGB||0,
        notes: r.notes||'', enabled: !!r.enabled,
      };
    },
    async saveResellerEdit() {
      const r = this.resellerEdit; if (!r || !r.id) return;
      const d = await this.api('/resellers/' + r.id + '/quotas', { method: 'POST', body: JSON.stringify(r) });
      if (d && d.success) { this.toast('success', 'سهمیه/قیمت به‌روز شد'); this.resellerEdit = null; this.loadResellers(); }
      else this.toast('error', (d && d.msg) || 'به‌روزرسانی ناموفق');
    },
    async loadResellerLedger(r) {
      const d = await this.api('/resellers/' + r.id + '/ledger');
      if (d && d.success) { this.resellerLedger = d.obj || []; this.toast('success', 'دفترکل ' + r.username); }
      else this.toast('error', (d && d.msg) || 'خطا');
    },
    async quoteWholesale(totalGB) {
      const d = await this.api('/resellers/quote', { method: 'POST', body: JSON.stringify({ totalGB: Number(totalGB)||0 }) });
      if (d && d.success) return d.obj;
      return null;
    },
    async loadResellers() {
      try {
        const [list, sum, me, stats, topups, wh] = await Promise.all([
          this.api('/resellers'),
          this.api('/resellers/summary'),
          this.api('/resellers/me'),
          this.api('/resellers/stats'),
          this.api('/resellers/topups'),
          this.api('/resellers/settings/wholesale'),
        ]);
        if (list && list.success) this.resellers = list.obj || [];
        if (sum && sum.success) this.resellerSummary = sum.obj || {};
        if (me && me.success) this.resellerMe = me.obj || {};
        if (stats && stats.success) this.resellerStats = stats.obj || [];
        if (topups && topups.success) this.resellerTopups = topups.obj || [];
        if (wh && wh.success) this.resellerWholesale = wh.obj || {};
      } catch (e) { this.toast('error', 'Failed to load resellers'); }
    },
    async loadResellerStats() {
      const d = await this.api('/resellers/stats');
      if (d && d.success) this.resellerStats = d.obj || [];
    },
    async saveResellerWholesale() {
      const d = await this.api('/resellers/settings/wholesale', { method: 'POST', body: JSON.stringify({
        wholesalePricePerGB: this.resellerWholesale.wholesalePricePerGB || 0,
        topupMinToman: this.resellerWholesale.topupMinToman || 10000,
      })});
      if (d && d.success) this.toast('success', 'تنظیمات عمده ذخیره شد');
      else this.toast('error', (d && d.msg) || 'ذخیره ناموفق');
    },
    async submitResellerTopup() {
      const amount = Number(this.resellerTopupAmount);
      if (!Number.isFinite(amount) || amount <= 0) { this.toast('error', 'مبلغ نامعتبر'); return; }
      const d = await this.api('/resellers/topups', { method: 'POST', body: JSON.stringify({ amount, receiptNote: this.resellerTopupNote || '' }) });
      if (d && d.success) { this.toast('success', 'درخواست شارژ ثبت شد'); this.resellerTopupAmount=0; this.resellerTopupNote=''; this.loadResellers(); }
      else this.toast('error', (d && d.msg) || 'ثبت ناموفق');
    },
    async approveResellerTopup(o) {
      const d = await this.api('/resellers/topups/' + o.id + '/approve', { method: 'POST', body: JSON.stringify({ adminNote: '' }) });
      if (d && d.success) { this.toast('success', 'تأیید شد'); this.loadResellers(); }
      else this.toast('error', (d && d.msg) || 'ناموفق');
    },
    async rejectResellerTopup(o) {
      const d = await this.api('/resellers/topups/' + o.id + '/reject', { method: 'POST', body: JSON.stringify({ adminNote: 'rejected' }) });
      if (d && d.success) { this.toast('success', 'رد شد'); this.loadResellers(); }
      else this.toast('error', (d && d.msg) || 'ناموفق');
    },
    async createReseller() {
      const f = this.resellerForm;
      if (!f.username || !f.password || f.password.length < 8) {
        this.toast('error', 'username and 8+ char password required');
        return;
      }
      const d = await this.api('/resellers', { method: 'POST', body: JSON.stringify(f) });
      if (d && d.success) {
        this.toast('success', 'Reseller created');
        this.resellerFormOpen = false;
        this.resellerForm = { username:'', password:'', parentUserId:0, initialCredit:0, maxTotalGB:0, maxGBPerClient:0, maxClients:0, allowSubResellers:false, maxSubResellers:0, commissionPercent:0, wholesalePricePerGB:0, notes:'' }; this.resellerEdit = null;
        this.loadResellers();
      } else this.toast('error', (d && d.msg) || 'Create failed');
    },
    async creditReseller(r) {
      const raw = prompt('Credit amount for ' + r.username, '100000');
      if (raw == null) return;
      const amount = Number(raw);
      if (!Number.isFinite(amount) || amount <= 0) { this.toast('error', 'Invalid amount'); return; }
      const d = await this.api('/resellers/' + r.id + '/credit', { method: 'POST', body: JSON.stringify({ amount, note: 'panel topup' }) });
      if (d && d.success) { this.toast('success', 'Credit applied'); this.loadResellers(); }
      else this.toast('error', (d && d.msg) || 'Credit failed');
    },
    async toggleResellerEnabled(r) {
      const d = await this.api('/resellers/' + r.id + '/quotas', { method: 'POST', body: JSON.stringify({
        maxTotalGB: r.maxTotalGB, maxGBPerClient: r.maxGBPerClient, maxClients: r.maxClients,
        allowSubResellers: r.allowSubResellers, maxSubResellers: r.maxSubResellers,
        commissionPercent: r.commissionPercent, enabled: !r.enabled,
      })});
      if (d && d.success) { this.toast('success', 'Updated'); this.loadResellers(); }
      else this.toast('error', (d && d.msg) || 'Update failed');
    },
    async createBackup() { const d=await this.api('/pro/backups',{method:'POST'}); if(d&&d.success){this.toast('success','Backup created');this.loadPro()}else this.toast('error',(d&&d.msg)||'Backup failed') },
    async deleteBackup(b){const d=await this.api('/pro/backups/'+b.id,{method:'DELETE'});if(d&&d.success){this.toast('success','Backup deleted');this.loadPro()}},
    async createApiKey(){const name=prompt('API key name');if(!name)return;const d=await this.api('/pro/api-keys',{method:'POST',body:JSON.stringify({name,scopes:['system:read','inbounds:read','clients:read','nodes:read']})});if(d&&d.success){this.toast('success','API key: '+d.obj.secret);this.loadPro()}else this.toast('error',(d&&d.msg)||'Could not create API key')},
    async deleteApiKey(k){const d=await this.api('/pro/api-keys/'+k.id,{method:'DELETE'});if(d&&d.success){this.toast('success','API key deleted');this.loadPro()}},
    async addNode(){const n=this.pro.node;const d=await this.api('/pro/nodes',{method:'POST',body:JSON.stringify(n)});if(d&&d.success){this.pro.node={name:'',address:'',port:22,agentEndpoint:'',role:'node'};this.toast('success','Node registered');this.loadPro()}else this.toast('error',(d&&d.msg)||'Could not add node')},
    async deleteNode(n){const d=await this.api('/pro/nodes/'+n.id,{method:'DELETE'});if(d&&d.success){this.toast('success','Node removed');this.loadPro()}},
    async registerAgent(n){const d=await this.api('/pro/platform/nodes/agent-token',{method:'POST',body:JSON.stringify({nodeId:n.id})});if(d?.success){this.pro.agentBootstrap=d.obj;this.toast('success','Agent token generated once');}else this.toast('error',d?.msg||'Could not generate agent token')},
    async rotateAgentToken(n){const d=await this.api('/pro/platform/nodes/'+n.id+'/agent-token/rotate',{method:'POST'});if(d?.success){this.pro.agentBootstrap=d.obj;this.toast('success','Agent token rotated');}else this.toast('error',d?.msg||'Could not rotate agent token')},
    async toggleAgent(n){const enabled=!(n.agentEnabled===true);const d=await this.api('/pro/platform/nodes/'+n.id+'/agent-enabled',{method:'POST',body:JSON.stringify({enabled})});if(d?.success){this.toast('success',enabled?'Agent enabled':'Agent disabled');this.loadPro()}else this.toast('error',d?.msg||'Could not change agent state')},
    async nodeConnection(n){const d=await this.api('/pro/platform/nodes/'+n.id+'/connection');if(d?.success){const a=d.obj.agent||{};this.toast(d.obj.healthy?'success':'error',(d.obj.healthy?'Agent healthy':'Agent not healthy')+' · '+(a.hostname||n.name));}else this.toast('error',d?.msg||'Could not inspect node')},
    async deployNodeConfig(n,engine){this.confirmAction('Deploy configuration?', 'The current '+engine+' configuration will be validated remotely before restart on '+n.name+'.', async () => {const d=await this.api('/pro/platform/nodes/'+n.id+'/deploy-config',{method:'POST',body:JSON.stringify({engine})});if(d?.success){this.toast('success',engine.toUpperCase()+' config queued for '+n.name);this.loadPlatform()}else this.toast('error',d?.msg||'Could not queue config deployment')});},
    async createSubscriptionGroup(){const name=prompt('Subscription group name');if(!name)return;const addressType=prompt('Address type: ipv4 / ipv6 / domain / dual / all','all')||'all';const smart=confirm('Enable smart health filtering for this group?');const maxLinks=Number(prompt('Max links (0 = unlimited)','0')||0);const d=await this.api('/pro/subscriptions/groups',{method:'POST',body:JSON.stringify({name,addressType,refreshMin:15,smartEnabled:smart,maxLinks:Number.isFinite(maxLinks)&&maxLinks>0?maxLinks:0,preset:smart?'multi-node':'standard'})});if(d?.success){this.toast('success','Subscription group created');this.loadPro()}else this.toast('error',d?.msg||'Could not create subscription group')},
    async deleteSubscriptionGroup(g){const d=await this.api('/pro/subscriptions/groups/'+g.id,{method:'DELETE'});if(d?.success){this.toast('success','Subscription group deleted');this.loadPro()}}, async rotateSubscriptionGroup(g){const d=await this.api('/pro/subscriptions/groups/'+g.id+'/rotate',{method:'POST'});if(d?.success){this.toast('success','Group token rotated');this.loadPro()}else this.toast('error',d?.msg||'Could not rotate group token')}, async toggleSubscriptionGroup(g){const d=await this.api('/pro/subscriptions/groups/'+g.id+'/toggle',{method:'POST'});if(d?.success){this.loadPro()}}, async revokeSubscription(cl){const d=await this.api('/pro/clients/'+cl.id+'/subscription/revoke',{method:'POST'});if(d?.success){this.toast('success','Subscription revoked');this.loadInbounds()}}, async enableSubscription(cl){const d=await this.api('/pro/clients/'+cl.id+'/subscription/enable',{method:'POST'});if(d?.success){this.toast('success','Subscription enabled');this.loadInbounds()}}, async applySubscriptionPreset(cl,p){const d=await this.api('/pro/clients/'+cl.id+'/subscription/apply-preset',{method:'POST',body:JSON.stringify({presetId:p.id})});if(d?.success){this.toast('success','Subscription preset applied');this.loadInbounds()}}, async chooseSubscriptionPreset(cl){if(!this.pro.subscriptionPresets.length){await this.loadPro()}const names=this.pro.subscriptionPresets.map((p,i)=>(i+1)+'. '+p.name+' ['+p.addressType+']').join('\n');const n=Number(prompt('Choose subscription preset:\n'+names,'1'));if(!Number.isFinite(n)||n<1||n>this.pro.subscriptionPresets.length)return;await this.applySubscriptionPreset(cl,this.pro.subscriptionPresets[n-1])},
    subscriptionGroupURL(g){return g.url || (window.location.origin+(window.__ELECTRU_BASE_PATH__||'')+'/sub-group/'+g.token+'?family='+encodeURIComponent(g.addressType||'all'))},
    async createUser(){const username=prompt('Username');if(!username)return;const password=prompt('Temporary password (8+ chars)');if(!password)return;const role=prompt('Role: owner/admin/operator/viewer','operator')||'operator';const d=await this.api('/pro/users',{method:'POST',body:JSON.stringify({username,password,role})});if(d&&d.success){this.toast('success','User created');this.loadPro()}else this.toast('error',(d&&d.msg)||'Could not create user')},
    async changeRole(u,role){const d=await this.api('/pro/users/'+u.id+'/role',{method:'POST',body:JSON.stringify({role})});if(d&&d.success){this.toast('success','Role updated')}else this.loadPro()},

    // ---------- node fabric / performance ----------
    async loadFabric() {
      const [performance, routing, advisor, deployments] = await Promise.all([
        this.api('/pro/control/performance'),
        this.api('/pro/control/routing'),
        this.api('/pro/control/advisor'),
        this.api('/pro/control/deployments')
      ]);
      if (performance?.success) this.fabric.performance = performance.obj || [];
      if (routing?.success) this.fabric.routing = routing.obj || [];
      if (advisor?.success) this.fabric.advisor = advisor.obj || [];
      if (deployments?.success) this.fabric.deployments = deployments.obj || [];
    },
    async selectFabricNode() {
      const strategy = prompt('Strategy: latency / load / capacity', 'latency') || 'latency';
      const d = await this.api('/pro/control/select-node?strategy='+encodeURIComponent(strategy));
      if (d?.success) { this.fabric.selected = d.obj; this.toast('success', 'Selected node: '+(d.obj.node?.name||'—')); }
      else this.toast('error', d?.msg || 'No healthy node available');
    },
    async createRoutingPolicy() {
      const name = prompt('Policy name'); if (!name) return;
      const strategy = prompt('Strategy: latency / load / capacity / weighted', 'latency') || 'latency';
      const ids = (prompt('Node IDs, comma separated (blank = managed nodes)', '') || '').split(',').map(x=>Number(x.trim())).filter(Boolean);
      const d = await this.api('/pro/control/routing',{method:'POST',body:JSON.stringify({name,strategy,nodeIds:ids,failover:true,enabled:true})});
      if(d?.success){this.toast('success','Routing policy created');this.loadFabric()} else this.toast('error',d?.msg||'Could not create policy');
    },
    async deleteRoutingPolicy(r){const d=await this.api('/pro/control/routing/'+r.id,{method:'DELETE'});if(d?.success){this.toast('success','Policy deleted');this.loadFabric()}},
    async createDeployment(){
      const name=prompt('Deployment name');if(!name)return;
      const target=prompt('Target Node ID');if(!target)return;
      const kind=prompt('Allowed action: config-validate / xray-restart / xray-status / singbox-restart / singbox-status / bbr-apply / bbr-status / network-status','config-validate')||'config-validate';
      const d=await this.api('/pro/control/deployments',{method:'POST',body:JSON.stringify({name,target,kind,rollbackOnFail:true})});
      if(d?.success){this.toast('success','Safe deployment queued');this.loadFabric()}else this.toast('error',d?.msg||'Could not create deployment');
    },
    async rollbackDeployment(d){const x=await this.api('/pro/control/deployments/'+d.id+'/rollback',{method:'POST'});if(x?.success){this.toast('success','Rollback requested');this.loadFabric()}else this.toast('error',x?.msg||'Rollback failed')},

    // ---------- platform center ----------
    async loadPlatform() {
      const safe = async (path) => { try { return await this.api(path); } catch (e) { return { success: false }; } };
      const [summary, security, jobs, bp, orders, webhooks, certs, plugins, operations, domains, nodeGroups] = await Promise.all([
        safe('/pro/platform/summary'), safe('/pro/platform/security'), safe('/pro/platform/jobs'), safe('/pro/platform/billing/plans'), safe('/pro/platform/billing/orders'), safe('/pro/platform/webhooks'), safe('/pro/platform/certificates'), safe('/pro/platform/plugins'), safe('/pro/platform/operations'), safe('/pro/platform/domains'), safe('/pro/platform/node-groups')
      ]);
      if(summary?.success)this.platform.summary=summary.obj||{}; if(security?.success)this.platform.security=security.obj||{}; if(jobs?.success)this.platform.jobs=jobs.obj||[]; if(bp?.success)this.platform.billingPlans=bp.obj||[]; if(orders?.success)this.platform.orders=orders.obj||[]; if(webhooks?.success)this.platform.webhooks=webhooks.obj||[]; if(certs?.success)this.platform.certificates=certs.obj||[]; if(plugins?.success)this.platform.plugins=plugins.obj||[]; if(operations?.success)this.platform.operations=operations.obj||[]; if(domains?.success)this.platform.domains=domains.obj||[]; if(nodeGroups?.success)this.platform.nodeGroups=nodeGroups.obj||[];
      this.loadBBR();
    },
    async createDomain(){const domain=prompt('Domain');if(!domain)return;const priority=Number(prompt('Priority','100'))||100;const d=await this.api('/pro/platform/domains',{method:'POST',body:JSON.stringify({domain,provider:'manual',challenge:'http-01',priority,enabled:true})});if(d?.success){this.toast('success','Domain added');this.loadPlatform()}else this.toast('error',d?.msg||'Could not add domain')},
    async checkDomain(d){const x=await this.api('/pro/platform/domains/'+d.id+'/check',{method:'POST'});if(x?.success){this.toast(x.obj.status==='healthy'?'success':'error',d.domain+' · '+x.obj.status+(x.obj.error?' · '+x.obj.error:''));this.loadPlatform()}},
    async failoverDomain(d){this.confirmAction('Switch primary domain?', 'Move primary traffic to the next healthy domain if one is available.', async () => {const x=await this.api('/pro/platform/domains/'+d.id+'/failover',{method:'POST'});if(x?.success){this.toast('success','Domain failover: '+x.obj.domain);this.loadPlatform()}else this.toast('error',x?.msg||'Failover unavailable')});},
    async runAgentDiagnostics(n){const x=await this.api('/pro/platform/nodes/'+n.id+'/agent-diagnostics',{method:'POST'});if(x?.success){this.toast('success','Diagnostics queued · '+x.obj.operationId);this.loadPlatform()}else this.toast('error',x?.msg||'Could not queue diagnostics')},
    async setNodeDrain(n){const drain=n.drainState!=='draining';const x=await this.api('/pro/platform/nodes/'+n.id+'/drain',{method:'POST',body:JSON.stringify({drain})});if(x?.success){this.toast('success',drain?'Node draining':'Node active');this.loadPro()}else this.toast('error',x?.msg||'Could not change node state')},
    async createPlatformJob(){const kind=prompt('Job: xray-restart / xray-status / singbox-restart / singbox-status / bbr-apply / bbr-status / network-status','xray-status');if(!kind)return;const target=prompt('Target node ID','')||'';if(!target){this.toast('error','Target node ID is required');return;}const d=await this.api('/pro/platform/jobs',{method:'POST',body:JSON.stringify({kind,target,payload:'{}'})});if(d?.success){this.toast('success','Job queued');this.loadPlatform()}else this.toast('error',d?.msg||'Could not create job')},
    async createBillingPlan(){const name=prompt('Billing plan name');if(!name)return;const amount=Number(prompt('Amount in cents','999'));const days=Number(prompt('Period days','30'));const gb=Number(prompt('Traffic GB (0 unlimited)','100'));const dev=Number(prompt('Device limit (0 unlimited)','2'));const d=await this.api('/pro/platform/billing/plans',{method:'POST',body:JSON.stringify({name,currency:'EUR',amountCents:amount,periodDays:days,trafficGB:gb,deviceLimit:dev})});if(d?.success){this.toast('success','Billing plan created');this.loadPlatform()}else this.toast('error',d?.msg||'Could not create plan')},
    async createCertificate(){const name=prompt('Certificate name');if(!name)return;const domain=prompt('Domain');if(!domain)return;const d=await this.api('/pro/platform/certificates',{method:'POST',body:JSON.stringify({name,domain,status:'managed',autoRenew:true})});if(d?.success){this.toast('success','Certificate record created');this.loadPlatform()}else this.toast('error',d?.msg||'Could not create certificate')},
    async createWebhook(){const name=prompt('Webhook name');if(!name)return;const url=prompt('HTTPS URL');if(!url)return;const d=await this.api('/pro/platform/webhooks',{method:'POST',body:JSON.stringify({name,url,events:'*'})});if(d?.success){this.toast('success','Webhook created. Secret: '+d.obj.secret);this.loadPlatform()}else this.toast('error',d?.msg||'Could not create webhook')},
    async deleteWebhook(w){const d=await this.api('/pro/platform/webhooks/'+w.id,{method:'DELETE'});if(d?.success){this.toast('success','Webhook deleted');this.loadPlatform()}},
    async registerPlugin(){const name=prompt('Plugin name');if(!name)return;const version=prompt('Version','1.0.0')||'1.0.0';const d=await this.api('/pro/platform/plugins',{method:'POST',body:JSON.stringify({name,version,description:'',enabled:true,config:'{}'})});if(d?.success){this.toast('success','Plugin registered');this.loadPlatform()}else this.toast('error',d?.msg||'Could not register plugin')},

    // ---------- ultra control center ----------
    async loadUltra() {
      if (!this.ultra) this.ultra = { clients: [], devices: [], plans: [], snapshots: [], selectedClient: null, plan: { name: '', description: '', totalGB: 0, limitIp: 0, durationDay: 30 } };
      const safe = async (path) => { try { return await this.api(path); } catch (e) { return { success: false }; } };
      const [clients, devices, plans, snaps] = await Promise.all([
        safe('/clients?limit=500&offset=0'),
        safe('/pro/devices'),
        safe('/pro/plans'),
        safe('/pro/usage/snapshots')
      ]);
      // /clients returns { data, pagination } — not obj
      let list = [];
      if (clients && clients.success) {
        list = clients.data || clients.obj || [];
        const total = Number(clients.pagination?.total || list.length);
        const limit = Number(clients.pagination?.limit || 500);
        for (let offset = limit; offset < total && offset < 5000; offset += limit) {
          const pg = await safe('/clients?limit=' + limit + '&offset=' + offset);
          if (!pg?.success) break;
          list = list.concat(pg.data || pg.obj || []);
        }
      }
      this.ultra.clients = list;
      if (devices && devices.success) this.ultra.devices = devices.obj || devices.data || [];
      if (plans && plans.success) this.ultra.plans = plans.obj || plans.data || [];
      if (snaps && snaps.success) this.ultra.snapshots = snaps.obj || snaps.data || [];
    },
    async openSubscriptionClinic(cl) {
      this.subscriptionClinic={show:true,client:cl,data:null,loading:true};
      const [clinic, analytics]=await Promise.all([this.api('/pro/subscriptions/clinic?clientId='+encodeURIComponent(cl.id)),this.api('/pro/subscriptions/analytics?clientId='+encodeURIComponent(cl.id))]);
      this.subscriptionClinic.data={clinic:clinic?.obj||null,analytics:analytics?.obj||null};
      this.subscriptionClinic.loading=false;
    },
    async rotateSubscription(cl) { const d=await this.api('/pro/clients/'+cl.id+'/subscription/rotate',{method:'POST'}); if(d?.success){this.toast('success','Subscription rotated'); this.openSubscriptionClinic(cl);} else this.toast('error',d?.msg||'Could not rotate subscription') },
    async resetClientDevices(cl) {
      const d = await this.api('/pro/clients/' + cl.id + '/devices/reset', { method: 'POST' });
      if (d && d.success) { this.toast('success', 'Devices reset'); this.loadUltra(); }
    },
    async deleteDevice(dv) {
      const d = await this.api('/pro/devices/' + dv.id, { method: 'DELETE' });
      if (d && d.success) { this.toast('success', 'Device removed'); this.loadUltra(); }
    },
    async applyPlan(cl, plan) {
      const d = await this.api('/pro/clients/' + cl.id + '/plan', { method: 'POST', body: JSON.stringify({ planId: plan.id }) });
      if (d && d.success) { this.toast('success', 'Plan applied'); this.loadUltra(); this.loadInbounds(); }
      else this.toast('error', (d && d.msg) || 'Could not apply plan');
    },
    async createPlan() {
      const p = this.ultra.plan;
      if (!p.name) return this.toast('error', 'Plan name is required');
      const d = await this.api('/pro/plans', { method: 'POST', body: JSON.stringify(p) });
      if (d && d.success) { this.ultra.plan = { name:'', description:'', totalGB:0, limitIp:0, durationDay:30 }; this.toast('success','Plan created'); this.loadUltra(); }
      else this.toast('error', (d && d.msg) || 'Could not create plan');
    },
    async deletePlan(plan) {
      const d = await this.api('/pro/plans/' + plan.id, { method: 'DELETE' });
      if (d && d.success) { this.toast('success','Plan deleted'); this.loadUltra(); }
    },
    exportClient(cl, format) {
      const url = (window.__ELECTRU_BASE_PATH__ || '') + '/api/pro/clients/' + cl.id + '/export?format=' + encodeURIComponent(format);
      if (format === 'link' || format === 'base64') window.open(url, '_blank');
      else this.api('/pro/clients/' + cl.id + '/export?format=json').then(d => {
        if (d && d.success !== false) this.copyText(d.link || JSON.stringify(d), 'Configuration copied');
        else this.toast('error', (d && d.msg) || 'Export failed');
      });
    },

    // ---------- settings / account ----------
    async loadEnterprise() {
      const [m,a] = await Promise.all([this.api('/pro/enterprise/metrics'), this.api('/pro/enterprise/alerts?open=1')]);
      if(m?.success) this.enterprise.metrics=m.obj||{};
      if(a?.success) this.enterprise.alerts=a.obj||[];
    },
    async refreshEnterpriseAlerts() {
      const d=await this.api('/pro/enterprise/alerts/refresh',{method:'POST'});
      if(d?.success){await this.loadEnterprise();this.toast('success','Alert engine refreshed')} else this.toast('error',d?.msg||'Alert refresh failed');
    },
    async resolveEnterpriseAlert(id){const d=await this.api('/pro/enterprise/alerts/'+id+'/resolve',{method:'POST'});if(d?.success)this.loadEnterprise();else this.toast('error',d?.msg||'Could not resolve alert')},
    async enterpriseSearch(){const q=(this.commandQuery||'').trim();if(q.length<2)return;const d=await this.api('/pro/enterprise/search?q='+encodeURIComponent(q));if(d?.success){this.enterprise.search={q,obj:d.obj};this.commandPalette=false;this.toast('success','Search complete')}else this.toast('error',d?.msg||'Search failed')},


    async loadMe() {
      const data = await this.api('/me');
      if (data && data.success) { this.me = data.obj; this.credentials.username = data.obj.username || ''; }
    },
    async loadPanelSettings() {
      const data = await this.api('/panel/settings');
      if (data && data.success) {
        this.panelSettings = { ...this.panelSettings, ...data.obj, certificatePEM: '', privateKeyPEM: '', clearCertificate: false };
      }
    },
    async loadBBR() {
      const data = await this.api('/pro/network/bbr');
      if (data && data.success) {
        this.bbr.status = data.obj || {};
      } else if (data && data.msg) {
        this.toast('error', data.msg);
      }
    },

    async loadPublicHostStatus() {
      const d = await this.api('/pro/ops/public-host');
      if (d && d.success) this.ops.publicHostStatus = d.obj || null;
    },
    async savePublicDomain() {
      const domain = (this.ops.newDomain || '').trim();
      if (!domain) return this.toast('error', 'دامنه را وارد کنید');
      const d = await this.api('/pro/ops/public-host/domain', { method: 'POST', body: JSON.stringify({ domain }) });
      if (d && d.success) {
        this.toast('success', 'دامنه جدید ذخیره شد');
        this.ops.newDomain = '';
        this.ops.publicHostStatus = d.obj || null;
      } else this.toast('error', (d && d.msg) || 'ذخیره دامنه ناموفق');
    },
    async forceAdaptPublicIP() {
      const enabled = await this.api('/pro/ops/public-host/auto', { method: 'POST', body: JSON.stringify({ enabled: true }) });
      if (enabled && enabled.success) {
        const d = await this.api('/pro/ops/public-host');
        if (d && d.success) {
          this.ops.publicHostStatus = d.obj || null;
          this.toast('success', (d.obj && d.obj.message) || 'تشخیص خودکار IP فعال شد');
          return;
        }
      }
      this.toast('error', (enabled && enabled.msg) || 'فعال‌سازی تشخیص خودکار ناموفق بود');
    },
    async savePublicIPv4() {
      const ipv4 = (this.ops.newIPv4 || '').trim();
      if (!ipv4) return this.toast('error', 'IPv4 را وارد کنید');
      const d = await this.api('/pro/ops/public-host/ipv4', { method: 'POST', body: JSON.stringify({ ipv4 }) });
      if (d && d.success) {
        this.ops.publicHostStatus = d.obj || null;
        this.ops.newIPv4 = '';
        this.toast('success', 'IPv4 دستی ذخیره شد؛ تشخیص خودکار تا فعال‌سازی مجدد خاموش است');
      } else this.toast('error', (d && d.msg) || 'ذخیره IPv4 ناموفق بود');
    },
    async loadDNSProvider() {
      try {
        const d = await this.api('/pro/dns');
        if (d && d.success && d.obj) {
          this.settingsCenter.dnsProvider = d.obj.provider || 'auto';
          this.settingsCenter.dnsCustomResolvers = d.obj.customResolvers || '';
          const eff = d.obj.effective || [];
          if (eff[0]) this.settingsCenter.dnsPrimary = String(eff[0]).replace(/^https\+local:\/\//,'').replace(/^tls\+local:\/\//,'').replace(/\/dns-query$/,'').replace(/:853$/,'');
          if (eff[1]) this.settingsCenter.dnsSecondary = String(eff[1]).replace(/^https\+local:\/\//,'').replace(/^tls\+local:\/\//,'').replace(/\/dns-query$/,'').replace(/:853$/,'');
        }
      } catch (e) {}
    },
    async saveDNSProvider() {
      try {
        const body = {
          provider: this.settingsCenter.dnsProvider || 'auto',
          customResolvers: this.settingsCenter.dnsCustomResolvers || '',
          applyNow: true
        };
        const d = await this.api('/pro/dns', { method: 'POST', body: JSON.stringify(body) });
        if (d && d.success) {
          this.toast('success', d.msg || 'DNS provider applied to Xray/sing-box');
          await this.loadDNSProvider();
        } else {
          this.toast('error', (d && d.msg) || 'DNS save failed');
        }
      } catch (e) {
        this.toast('error', String(e));
      }
    },
    async loadSettingsCenter() {
      // Load independent settings sources without allowing one optional endpoint
      // to prevent the core settings form from rendering.
      try { await this.loadShop(); } catch (e) { console.warn('Could not load shop settings', e); }
      try { await this.loadTelegramConfig(); } catch (e) { console.warn('Could not load Telegram settings', e); }

      const data = await this.api('/pro/ops/settings');
      if (!data || !data.success) return;
      const raw = data.obj || {};
      const out = { ...this.settingsCenter };
      Object.keys(out).forEach(k => {
        if (raw['settings.' + k] === undefined) return;
        const v = raw['settings.' + k];
        if (typeof out[k] === 'boolean') out[k] = v === 'true' || v === '1';
        else if (typeof out[k] === 'number') out[k] = Number(v) || 0;
        else out[k] = v;
      });
      this.settingsCenter = out;
      if (typeof this.loadAdvancedSettings === 'function') {
        try { await this.loadAdvancedSettings(); } catch (e) { console.warn('Could not load advanced settings', e); }
      }
    },
    async saveSettingsCenter() {
      if (this.settingsSaving) return;
      this.settingsSaving = true;
      const vals = {};
      Object.entries(this.settingsCenter).forEach(([k,v]) => vals['settings.'+k] = String(v));
      const data = await this.api('/pro/ops/settings', { method:'POST', body:JSON.stringify(vals) });
      this.settingsSaving = false;
      if (data && data.success) this.toast('success', this.t('settings.saved'));
      else this.toast('error',(data&&data.msg)||this.t('settings.saveFail'));
    },
    confirmSaveSettingsCenter() {
      this.confirmAction('ذخیره تنظیمات؟','مطمئنید می‌خواهید تنظیمات را ذخیره کنید؟ هر تغییر اشتباه می‌تواند باعث اختلال در پنل یا سرویس‌ها شود. تنظیمات حساس را بعد از بررسی دقیق اعمال کنید.',()=>this.saveSettingsCenter());
    },
    async revokeAllSessions() {
      this.confirmAction('خروج از همه نشست‌ها؟','تمام نشست‌های فعال این حساب باطل می‌شوند و باید دوباره وارد شوید.',async()=>{
        const d=await this.api('/sessions/revoke-all',{method:'POST'});
        if(d&&d.success){ this.toast('success','همه نشست‌ها باطل شدند'); setTimeout(()=>window.location.replace((window.__ELECTRU_BASE_PATH__||'')+'/login'),700); }
        else this.toast('error',(d&&d.msg)||'عملیات انجام نشد');
      });
    },
    async loadAdvancedSettings() {
      const [sec,ses,run,em] = await Promise.all([this.api('/pro/ops/security-scan'),this.api('/pro/ops/sessions'),this.api('/pro/ops/runtime'),this.api('/pro/ops/emergency')]);
      if(sec&&sec.success)this.advancedSettings.security=sec.obj||{};
      if(ses&&ses.success)this.advancedSettings.sessions=ses.obj||[];
      if(run&&run.success)this.advancedSettings.runtime=run.obj||{};
      if(em&&em.success)this.advancedSettings.emergency=em.obj||{};
    },
    async loadAdvancedLogs() { const d=await this.api('/pro/ops/log-tail?lines=120'); if(d&&d.success)this.advancedSettings.logs=d.obj?.text||''; },
    async revokeSession(id) { this.confirmAction('لغو نشست؟','این نشست فوراً باطل می‌شود.',async()=>{const d=await this.api('/pro/ops/sessions/'+id+'/revoke',{method:'POST'});if(d&&d.success){this.toast('success','نشست لغو شد');this.loadAdvancedSettings()}else this.toast('error',(d&&d.msg)||'عملیات ناموفق')}); },
    async verifyBackup(id) { const d=await this.api('/pro/ops/backups/'+id+'/verify',{method:'POST'}); if(d&&d.success)this.toast('success','Backup معتبر است'); else this.toast('error',(d&&d.msg)||'Backup معتبر نیست'); },
    async dbAction(action) { this.confirmAction('عملیات دیتابیس؟','قبل از VACUUM/ANALYZE/Integrity Check یک Backup توصیه می‌شود.',async()=>{const d=await this.api('/pro/ops/database/'+action,{method:'POST'});if(d&&d.success)this.toast('success','Database '+action+' completed');else this.toast('error',(d&&d.msg)||'عملیات ناموفق')}); },
    async securityScan() { await this.loadAdvancedSettings(); this.toast('success','Security scan refreshed'); },
    async setEmergency(field,value) { const body={}; body[field]=value; const d=await this.api('/pro/ops/emergency',{method:'POST',body:JSON.stringify(body)}); if(d&&d.success){this.toast('success','Emergency policy updated');this.loadAdvancedSettings()}else this.toast('error',(d&&d.msg)||'تغییر انجام نشد'); },

    async createManualBackup() {
      const d=await this.api('/pro/backups',{method:'POST'});
      if(d&&d.success){this.toast('success','Backup ساخته شد');this.loadPro();} else this.toast('error',(d&&d.msg)||'ساخت Backup ناموفق بود');
    },
    restoreBackup(id) {
      this.confirmAction('بازیابی Backup؟','قبل از Restore یک Backup ایمنی ساخته می‌شود. با تأیید، پنل Restart خواهد شد و اطلاعات Backup انتخاب‌شده جایگزین وضعیت فعلی می‌شود.',async()=>{
        const d=await this.api('/pro/backups/'+id+'/restore',{method:'POST'});
        if(d&&d.success){this.toast('success','بازیابی شروع شد؛ پنل Restart می‌شود');setTimeout(()=>window.location.reload(),5000);} else this.toast('error',(d&&d.msg)||'Restore ناموفق بود');
      });
    },
    uploadBackupFile(ev) {
      const input = ev && ev.target;
      const file = input && input.files && input.files[0];
      if (!file) return;
      if (!/\.zip$/i.test(file.name)) {
        this.toast('error', 'فقط فایل zip بکاپ پنل مجاز است');
        if (input) input.value = '';
        return;
      }
      this.confirmAction(
        'آپلود و بازیابی بکاپ؟',
        'فایل «' + file.name + '» روی این سرور ذخیره و بلافاصله Restore می‌شود. وضعیت فعلی پنل با محتوای بکاپ جایگزین می‌شود و پنل Restart خواهد شد. برای مهاجرت از سرور قدیم این همان عملیات درست است.',
        async () => {
          try {
            const fd = new FormData();
            fd.append('file', file);
            fd.append('restore', '1');
            const base = (window.__ELECTRU_BASE_PATH__ || '').replace(/\/$/, '');
            // Do not set Content-Type — browser sets multipart boundary. Auth is cookie/session (same-origin).
            const res = await fetch(base + '/api/pro/backups/upload', {
              method: 'POST',
              body: fd,
              credentials: 'same-origin',
            });
            let d = null;
            try { d = await res.json(); } catch (_) {}
            if (d && d.success) {
              this.toast('success', d.msg || 'بکاپ آپلود شد؛ بازیابی شروع شد');
              if (d.restartRequired) setTimeout(() => window.location.reload(), 5000);
              else this.loadPro();
            } else {
              this.toast('error', (d && d.msg) || ('آپلود ناموفق (' + res.status + ')'));
            }
          } catch (e) {
            this.toast('error', 'خطا در آپلود: ' + (e && e.message ? e.message : e));
          } finally {
            if (input) input.value = '';
          }
        }
      );
    },
    async testBBR() { const d=await this.api('/pro/network/bbr/test',{method:'POST'}); if(d&&d.success){this.bbr.test=d.obj||{};this.toast('success','تست شبکه انجام شد')}else this.toast('error',(d&&d.msg)||'تست ناموفق'); },
    async applyBBR() { this.confirmAction('اعمال BBR؟','تنظیمات BBR و qdisc روی سیستم اعمال می‌شود. قبل از تغییر وضعیت فعلی را بررسی کنید.',async()=>{const d=await this.api('/pro/network/bbr/apply',{method:'POST',body:JSON.stringify({})});if(d&&d.success){this.toast('success','BBR اعمال شد');this.loadBBR()}else this.toast('error',(d&&d.msg)||'اعمال BBR ناموفق بود')}); },
    async rollbackBBR() { this.confirmAction('بازگردانی BBR؟','تنظیمات قبلی congestion control و qdisc بازگردانده می‌شود.',async()=>{const d=await this.api('/pro/network/bbr/rollback',{method:'POST'});if(d&&d.success){this.toast('success','تنظیمات BBR بازگردانده شد');this.loadBBR()}else this.toast('error',(d&&d.msg)||'بازگردانی BBR ناموفق بود')}); },
    confirmSavePanelSettings() {
      this.confirmAction('ذخیره تنظیمات پنل؟', 'مطمئنید می‌خواهید تنظیمات را ذخیره کنید؟ هر تغییر اشتباه می‌تواند باعث اختلال در پنل شود. تنظیمات ابتدا اعتبارسنجی می‌شوند و در صورت بالا نیامدن پنل، پیکربندی قبلی به‌صورت خودکار برگردانده می‌شود.', () => this.savePanelSettings());
    },
    async savePanelSettings() {
      const payload = {
        port: Number(this.panelSettings.port),
        tlsEnabled: !!this.panelSettings.tlsEnabled,
        tlsHost: this.panelSettings.tlsHost || '',
        certificatePEM: this.panelSettings.certificatePEM || '',
        privateKeyPEM: this.panelSettings.privateKeyPEM || '',
        clearCertificate: !!this.panelSettings.clearCertificate,
      };
      const data = await this.api('/panel/settings/apply', { method: 'POST', body: JSON.stringify(payload) });
      if (data && data.success) {
        this.toast('success', 'تنظیمات ذخیره شد؛ پنل در حال راه‌اندازی مجدد است');
        setTimeout(() => window.location.reload(), 2500);
      } else this.toast('error', (data && data.msg) || 'ذخیره تنظیمات انجام نشد');
    },
    generateRandomPanelPassword() {
      const alphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%^&*_-+=';
      const values = new Uint32Array(24);
      if (window.crypto?.getRandomValues) window.crypto.getRandomValues(values);
      else for (let i = 0; i < values.length; i++) values[i] = Math.floor(Math.random() * 0xffffffff);
      this.credentials.password = Array.from(values, n => alphabet[n % alphabet.length]).join('');
      this.toast('success', 'رمز عبور تصادفی ساخته شد؛ آن را ذخیره کنید');
    },
    async changeCredentials() {
      const username = (this.credentials.username || '').trim();
      const password = this.credentials.password || '';
      if (!username || !password || password.length < 12) {
        this.toast('error', 'نام کاربری و رمز عبور معتبر وارد کنید؛ رمز باید حداقل ۱۲ کاراکتر باشد');
        return;
      }
      this.confirmAction('تغییر اطلاعات ورود؟', 'با تأیید، نام کاربری و رمز عبور جدید ذخیره می‌شود و برای امنیت، نشست فعلی بسته خواهد شد. سپس باید با اطلاعات جدید دوباره وارد شوید.', async () => {
        const data = await this.api('/change-credentials', {
          method: 'POST',
          body: JSON.stringify({ currentPassword: this.credentials.current, username, newPassword: password }),
        });
        if (data && data.success) {
          this.credentials = { current: '', username: '', password: '' };
          this.toast('success', 'اطلاعات ورود ذخیره شد؛ در حال انتقال به صفحه ورود');
          setTimeout(() => { window.location.replace((window.__ELECTRU_BASE_PATH__ || '') + '/login'); }, 900);
        } else this.toast('error', (data && data.msg) || 'تغییر اطلاعات ورود انجام نشد');
      });
    },
    async changePassword() {
      const data = await this.api('/change-password', {
        method: 'POST',
        body: JSON.stringify({ currentPassword: this.pw.current, newPassword: this.pw.next }),
      });
      if (data && data.success) { this.pw = { current: '', next: '' }; this.toast('success', 'Password updated — sign in again'); setTimeout(() => { window.location.replace((window.__ELECTRU_BASE_PATH__ || '') + '/login'); }, 900); }
      else this.toast('error', (data && data.msg) || 'Could not update password');
    },

    async logout() {
      await this.api('/logout', { method: 'POST' });
      window.location.replace((window.__ELECTRU_BASE_PATH__ || '') + '/login');
    },
  };
}

function inboundDefaults() {
  return {
    antiDpiProfile: 'off', remark: '', ipMode: 'ipv4', subscriptionEnabled: true, subscriptionName: '', subscriptionPreset:'standard', subscriptionSmartEnabled:false, subscriptionDefaultAddress: 'ipv4', port: 443, protocol: 'vless', network: 'tcp', security: 'reality',
    wsPath: '/ws', wsHost: '', grpcService: 'electruvpn', xhttpPath: '/xhttp', xhttpHost: '', xhttpMode: 'auto', xhttpExtraJson: '', httpUpgradePath: '/', httpUpgradeHost: '', httpUpgradeHeadersJson: '', acceptProxyProtocol: false,
    certFile: '', keyFile: '', sni: '', alpn: 'h2,http/1.1',
    dest: 'www.microsoft.com:443', serverName: 'www.microsoft.com,microsoft.com,www.cloudflare.com', spiderX: '/',
    privateKey: '', publicKey: '', shortId: '', ssMethod: '2022-blake3-aes-256-gcm',
    fragmentEnabled: false, fragmentType: 'tlshello', fragmentPackets: 'tlshello', fragmentLength: '100-200', fragmentInterval: '10-20', fragmentMaxSplit: '100-200',
    muxEnabled: false, muxConcurrency: 8, muxSessionTTL: 300, muxXudpConcurrency: 16, muxPadding: true,
    sockoptEnabled: true, tcpKeepAlive: 30, tcpNoDelay: true, tcpFastOpen: false,
    tcpMaskEnabled: false, tcpMask1Type: 'fragment', tcpMask1Packets: '1-3', tcpMask1Lengths: '100-200', tcpMask1Delays: '10-20', tcpMask1MaxSplit: '100-200',
    sniffing: true, sniffRouteOnly: true, sniffMetadataOnly: false, sniffDestOverride: 'http,tls,quic', fallbackDest: '',
    clientCount: 1, clientPrefix: '', clientTotalGB: 0, clientDays: 0, clientLimitIp: 0, labels: '',
    inboundTotalGB: 0, inboundDays: 0, inboundExpiryTime: 0, inboundLimitReached: false,
    upMbps: 0, downMbps: 0, obfsType: 'salamander', obfsPassword: '', congestionControl: 'bbr',
    wgPrivateKey: '', wgPublicKey: '', wgAddress: '10.77.1.1/24', wgMtu: 1420,
  };
}
