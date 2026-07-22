export default {
  common: {
    loading: 'Loading...',
    refreshing: 'Refreshing…',
    submitting: 'Submitting...',
    justNow: 'just now',
    peakRateTooltip: 'Peak rate: {window}',
    peakRateImageNote: '; image tokens billed as tokens are also affected, per-image billing is unaffected',
    save: 'Save',
    saved: 'Saved successfully',
    savedViews: 'Saved views',
    savedViewsPlaceholder: 'Select a saved filter view',
    saveView: 'Save current view',
    savedViewName: 'View name',
    savedViewNamePlaceholder: 'For example: Active Anthropic accounts',
    savedViewDescription: 'Save the current search, filters, sort, and pagination for quick reuse.',
    deleteSavedView: 'Delete saved view',
    deleted: 'Deleted successfully',
    cancel: 'Cancel',
    delete: 'Delete',
    edit: 'Edit',
    create: 'Create',
    update: 'Update',
    confirm: 'Confirm',
    reset: 'Reset',
    search: 'Search',
    filter: 'Filter',
    export: 'Export',
    import: 'Import',
    actions: 'Actions',
    status: 'Status',
    name: 'Name',
    email: 'Email',
    password: 'Password',
    submit: 'Submit',
    back: 'Back',
    next: 'Next',
    yes: 'Yes',
    no: 'No',
    all: 'All',
    none: 'None',
    selectAll: 'Select all',
    selectRow: 'Select {name}',
    noData: 'No data',
    expand: 'Expand',
    collapse: 'Collapse',
    success: 'Success',
    error: 'Error',
    critical: 'Critical',
    warning: 'Warning',
    info: 'Info',
    active: 'Active',
    inactive: 'Inactive',
    enable: 'Enable',
    disable: 'Disable',
    more: 'More',
    menu: 'Menu',
    toggleMenu: 'Toggle menu',
    userMenu: 'User menu',
    pageNotFound: 'Page not found',
    close: 'Close',
    clear: 'Clear',
    enabled: 'Enabled',
    disabled: 'Disabled',
	    total: 'Total',
	    balance: 'Balance',
	    availableBalance: 'Available balance',
	    frozenBalance: 'Frozen balance',
	    totalBalance: 'Total balance',
	    available: 'Available',
    copiedToClipboard: 'Copied to clipboard',
    copied: 'Copied',
    copyFailed: 'Failed to copy',
    verifying: 'Verifying...',
    processing: 'Processing...',
    contactSupport: 'Contact Support',
    add: 'Add',
    invalidEmail: 'Please enter a valid email address',
    optional: 'optional',
    selectOption: 'Select an option',
    searchPlaceholder: 'Search...',
    noOptionsFound: 'No options found',
    noGroupsAvailable: 'No groups available',
    unknownError: 'Unknown error occurred',
    saving: 'Saving...',
    selectedCount: '({count} selected)',
    refresh: 'Refresh',
    retry: 'Retry',
    autoRefresh: {
      title: 'Auto Refresh',
      enable: 'Enable auto refresh',
      countdown: 'Auto refresh: {seconds}s',
      seconds: '{n} seconds',
    },
    view: 'View',
    settings: 'Settings',
    chooseFile: 'Choose File',
    copy: 'Copy',
    notAvailable: 'N/A',
    now: 'Now',
    today: 'Today',
    tomorrow: 'Tomorrow',
    unknown: 'Unknown',
    minutes: 'min',
    time: {
      never: 'Never',
      justNow: 'Just now',
      minutesAgo: '{n}m ago',
      hoursAgo: '{n}h ago',
      daysAgo: '{n}d ago',
      countdown: {
        daysHours: '{d}d {h}h',
        hoursMinutes: '{h}h {m}m',
        minutes: '{m}m',
        withSuffix: '{time} to lift'
      }
    }
  },

  adminCompliance: {
    title: 'Deployment and Operation Compliance Acknowledgment',
    blockingNotice: 'Deployment and operation compliance acknowledgment is required before continuing to use the console.',
    riskNotice: 'This acknowledgment provides clear, conspicuous, and reproducible notice of compliance obligations and operation risks for self-hosted instances.',
    version: 'Document Version',
    openDocument: 'Open the GitHub document',
    documentSource: 'The agreement text comes from Markdown files in this project repository. When the agreement content changes, the document version must be incremented; acknowledgments of older versions become invalid and console users must acknowledge again.',
    inputLabel: 'Type the following confirmation phrase exactly',
    inputPlaceholder: 'Type the confirmation phrase to continue',
    inputMismatch: 'The confirmation phrase does not match. Type the displayed text exactly.',
    legalNote: 'This acknowledgment defines the no-affiliation relationship and responsibility boundary between self-hosted instances and the open-source project, copyright holders, contributors, and maintainers. The party that deploys, operates, or controls the relevant instance remains independently responsible for its applicable obligations.',
    logout: 'Log out',
    accept: 'Acknowledge and Continue',
    accepted: 'Compliance acknowledgment recorded',
    acceptFailed: 'Failed to submit acknowledgment'
  },

  legal: {
    loadFailed: 'Failed to load document',
    retryLater: 'Refresh the page and try again later.',
    notFound: 'Document not found',
    notFoundDescription: 'This legal document does not exist or has been removed by an administrator.',
    updatedAt: 'Updated: {date}',
    empty: 'No content',
    loginAgreement: 'Login Agreement',
    adminCompliance: 'Deployment and Operation Compliance Commitment',
    loginAgreementPrompt: {
      checkboxPrefix: 'I have read and agree to ',
      documentSeparator: ', ',
      noticeTitle: 'Accept the latest terms before continuing.',
      noticeDescription: 'Account/password login and quick sign-in stay disabled until you accept.',
      viewTerms: 'View terms',
      dialogTitle: 'Terms Update Notice',
      dialogDescription: 'Our service terms were updated on {date}. Please read and accept the following terms before continuing.',
      recently: 'recently',
      relatedDocuments: 'Related documents',
      reject: 'Reject',
      accept: 'Accept and continue',
      loginRejectedWarning: 'Account/password login and quick sign-in are disabled until you accept the latest terms.',
      loginRequiredWarning: 'Please read and accept the latest terms before logging in.',
      registerRejectedWarning: 'Registration and quick sign-in are disabled until you accept the latest terms.',
      registerRequiredWarning: 'Please read and accept the latest terms before registering.'
    }
  },

  // Navigation
  nav: {
    dashboard: 'Dashboard',
    announcements: 'Announcements',
    apiKeys: 'API Keys',
    batchImage: 'Batch Images',
    usage: 'Usage',
    redeem: 'Redeem',
    affiliate: 'Affiliate Rebates',
    affiliateManagement: 'Affiliate Rebates',
    affiliateInviteRecords: 'Invite Records',
    affiliateRebateRecords: 'Rebate Records',
    affiliateTransferRecords: 'Transfer Records',
    profile: 'Profile',
    users: 'Users',
    groups: 'Groups',
    channels: 'Channels',
    availableChannels: 'Available Channels',
    subscriptions: 'Subscriptions',
    virtualCurrency: 'Assets',
    accountAllocations: 'Assigned Accounts',
    citySimulation: 'City Simulation',
    cityVisualPacks: 'City Visual Packs',
    virtualCurrencies: 'Virtual Currencies',
    virtualCurrencyIntegrations: 'Currency Integrations',
    accounts: 'Accounts',
    proxies: 'Proxies',
    redeemCodes: 'Redeem Codes',
    ops: 'Ops',
    promoCodes: 'Promo Codes',
    settings: 'Settings',
    myAccount: 'My Account',
    lightMode: 'Light Mode',
    darkMode: 'Dark Mode',
    collapse: 'Collapse',
    expand: 'Expand',
    logout: 'Logout',
    github: 'GitHub',
    mySubscriptions: 'My Subscriptions',
    buySubscription: 'Recharge / Subscription',
    docs: 'Docs',
    myOrders: 'My Orders',
    orderManagement: 'Orders',
    paymentDashboard: 'Payment Dashboard',
    paymentConfig: 'Payment Config',
    paymentPlans: 'Plans',
    channelManagement: 'Channels',
    channelPricing: 'Channel Pricing',
    channelMonitor: 'Channel Monitor',
    channelStatus: 'Channel Status',
    riskControl: 'Risk Control',
    securityAudit: 'Security Audit',
    contentModeration: 'Content Moderation',
    promptAudit: 'Prompt Audit',
    auditLogs: 'Audit Logs',
  },

  accountAllocations: {
    title: 'Assigned Accounts',
    description: 'Review administrator-assigned capacity, status, and your own usage.',
    readOnlyTitle: 'Managed account summary',
    readOnlyDescription: 'This page exposes only safe usage summaries. Account names, credentials, proxies, IPs, and other sensitive settings are never shown or controllable here.',
    emptyTitle: 'No assigned accounts',
    emptyDescription: 'An administrator has not assigned an upstream account to you yet. You can still use other groups you are authorized to access.',
    capacity: 'Concurrency',
    concurrentRequests: 'concurrent requests',
    requestUsage: 'Your requests',
    tokenUsage: 'Your tokens',
    requests: 'requests',
    tokens: 'tokens',
    assignedAt: 'Assigned',
    coolingUntil: 'Cooling down until {time}.',
    unavailableHint: 'Currently not schedulable; the administrator policy may replenish it automatically.',
    readyHint: 'Currently schedulable.',
    loadFailed: 'Failed to load assigned accounts',
    status: {
      ready: 'Ready',
      cooling: 'Cooling',
      unavailable: 'Unavailable'
    }
  },

  virtualCurrency: {
    title: 'My Assets',
    description: 'View available virtual currencies, eligible groups, and the full ledger.',
    refresh: 'Refresh assets',
    available: 'Available',
    reserved: 'Reserved',
    integerUnits: 'Whole units',
    precision: '{count} decimal places',
    otherAssets: 'Other assets',
    assetCount: '{count} custom assets',
    groups: 'Eligible groups',
    noGroups: 'No eligible groups',
    ledger: 'Asset ledger',
    viewLedger: 'View ledger',
    noWallets: 'No virtual currencies are available to you yet.',
    noLedger: 'No ledger entries yet.',
    createdAt: 'Time',
    type: 'Type',
    amount: 'Change',
    balanceAfter: 'Balance',
    source: 'Source',
    reason: 'Reason',
    loading: 'Loading assets...',
    loadFailed: 'Failed to load assets',
    ledgerFailed: 'Failed to load ledger',
    earnHint: 'Assets can be granted by redeem codes, activities, missions, referrals, or games.'
  },

  citySpatial: {
    title: 'City Spatial Simulation',
    description: 'Inspect verified Overmap and Chunk facts, terrain layers, and immutable spatial changes.',
    classic: 'CLASSIC spatial terminal',
    loading: 'Loading city space',
    loadingDescription: 'Validating the rule set, Overmap, and visible Chunks…',
    viewportAria: 'City character map. Use arrow keys to pan, the wheel to zoom, and brackets to change Z level.',
    empty: {
      eyebrow: 'No world established',
      title: 'Create the first verifiable city',
      description: 'A city world binds a fixed rule set, generator, and seed. The map displays only spatial facts generated and posted by the server.',
      waitingForWorld: 'An administrator has not added you to a playable city yet. Once assigned, you can create a character, explore, and progress it here.'
    },
    controls: {
      world: 'City world', selectWorld: 'Select a city world', viewMode: 'View mode',
      overmap: 'Overmap', localMap: 'Local map', zoomOut: 'Zoom out', zoomIn: 'Zoom in',
      refresh: 'Refresh verified spatial state', export: 'Export current Chunk as text', depth: 'Z level',
      layerUp: 'Move one level up', layerDown: 'Move one level down',
      dragHint: 'Drag the map or use arrow keys to pan', modeHint: 'Toggle Overmap/local map',
      depthHint: 'Change Z level', helpHint: 'Shortcuts'
    },
    legend: { generated: 'Generated', structure: 'Buildings and zoning', selected: 'Selected', unloaded: 'Unloaded' },
    mapHeader: {
      overmap: 'OVERMAP / REGIONAL SURVEY', local: 'LOCAL / CLASSIC CELL MAP',
      overmapSubtitle: '{count} server-backed region tiles · {buildings} buildings',
      localSubtitle: '{count} verified Chunks cached · {buildings} buildings on this level'
    },
    live: {
      overmap: 'Overmap, selected Chunk coordinate {coordinate}',
      local: 'Local map, selected coordinate {coordinate}, {layers} content layers'
    },
    inspector: {
      eyebrow: 'Spatial facts', title: 'Inspector', chunk: 'Chunk coordinate', district: 'District',
      terrain: 'Base terrain', variant: 'Generator variant', roadMask: 'Road connections', riverMask: 'River connections',
      state: 'Projection state', generated: 'Generated', notGenerated: 'Not generated', tileHash: 'Tile hash',
      worldCoordinate: 'World coordinate X / Y / Z', localCoordinate: 'Local Chunk coordinate', revision: 'Revision',
      generatedTick: 'Generated at tick', cellStack: 'Cell content stack',
      movementCost: 'Movement cost: {value}', payloadHash: 'Payload hash', none: 'None',
      parcels: 'Parcels', buildings: 'Buildings', zoning: 'Land-use zoning',
      floorSummary: '{count} floors · {capacity} capacity', landStack: 'Land and building facts',
      parcel: 'Parcel', building: 'Building', area: 'Statutory area', version: 'Fact version',
      floors: 'Z range', floorArea: 'Gross floor area', occupancy: 'Occupied / capacity', quality: 'Quality',
      allocations: '{count} housing allocations', actorsHere: 'Actors at this location', focusActor: 'Locate',
      unavailableTitle: 'No facts are available for this Cell',
      unavailableDescription: 'The target Chunk is not generated, is not loaded, or has no data on this Z level.',
      emptyTitle: 'Select a map position', emptyDescription: 'Select a regional Tile or local Cell to inspect the full server response.'
    },
    development: {
      eyebrow: 'Deterministic development facts', title: 'Development projects',
      description: 'Projects pass through application, review, and mobilisation, then progress only through city ticks.',
      unavailable: 'This world does not use the F7.4 development protocol yet.',
      tileProjects: 'Regional projects', adjustments: 'Posted adjustments',
      addedFloors: 'Added floors', addedCapacity: 'Added capacity', qualityGain: 'Quality gain',
      all: 'All', active: 'Active', newProject: 'Submit development request',
      noProjects: 'No development projects match this filter.', selectedBuilding: 'Target building',
      selectBuildingHint: 'Select a building on the local map before submitting a project.',
      projectName: 'Project name', projectNamePlaceholder: 'Optional project name',
      projectType: 'Project type', developer: 'Developer', targetFloors: 'Target total floors',
      targetQuality: 'Target quality', targetQualityHint: 'Enter milli-units; 1150 represents 115%.',
      resources: 'Deterministic requirement estimate', basicMaterial: 'Basic material', capitalGoods: 'Capital goods',
      labor: 'Labor', duration: 'Duration', ticks: '{count} ticks',
      serverAuthoritative: 'The bound server policy recalculates final cost, capacity, and duration.',
      progress: 'Construction progress', submittedAt: 'Submitted T{tick}', completionAt: 'Due T{tick}',
      reviewNote: 'Review note', cancellationReason: 'Cancellation reason',
      actionPrompt: 'Record the factual reason for this transition.',
      status: {
        submitted: 'Awaiting review', approved: 'Approved', rejected: 'Rejected',
        under_construction: 'Under construction', completed: 'Completed', cancelled: 'Cancelled'
      },
      type: { vertical_expansion: 'Vertical expansion', renovation: 'Renovation' },
      action: {
        submit: 'Submit request', approve: 'Approve', reject: 'Reject', start: 'Start',
        cancel: 'Cancel project', confirm: 'Confirm action', processing: 'Posting fact…'
      },
      commandSuccess: 'The development fact was posted through a city tick.',
      commandFailed: 'Development command failed'
    },
    enterprise: {
      eyebrow: 'Enterprise spatial facts', title: 'Enterprise sites and relocation',
      description: 'Inspect real building occupancy and execute openings, resizing, closure, and cross-district relocation through city ticks.',
      unavailable: 'This world does not use the F7.5 enterprise-location protocol yet.',
      noSites: 'No enterprise sites match these filters.', primary: 'Primary site',
      version: 'Fact version v{version}',
      filter: {
        firm: 'Firm', district: 'District', type: 'Site type', status: 'Status',
        allFirms: 'All firms', allDistricts: 'All districts', allTypes: 'All types', allStatuses: 'All statuses'
      },
      columns: { site: 'Site', firm: 'Firm entity', location: 'Building and pool', capacity: 'Occupied units', status: 'Status' },
      siteType: { headquarters: 'Headquarters', office: 'Office', production: 'Production site', warehouse: 'Warehouse', retail: 'Retail site' },
      status: { active: 'Active', closed: 'Closed' },
      factType: { opened: 'Opened', resized: 'Occupancy resized', closed: 'Closed', relocated: 'Cross-district relocation' },
      action: { open: 'Open site', resize: 'Resize occupancy', close: 'Close site', relocate: 'Relocate firm', confirm: 'Confirm and submit' },
      form: {
        firm: 'Firm entity', siteType: 'Site type', pool: 'Target unit pool', name: 'Site name',
        occupiedUnits: 'Target occupied units', policyMinimum: 'Leave blank for policy minimum', reason: 'Factual reason',
        currentDistrict: 'Current primary district', targetDistrict: 'Target district',
        headquartersPool: 'New headquarters pool', productionPool: 'New production pool',
        serverAuthoritative: 'The bound server policy recalculates minimum occupancy, use compatibility, capacity, and required-site constraints.',
        relocationWarning: 'Relocation atomically closes the old primary sites, opens a new headquarters and production site, and conserves every non-zero firm inventory balance.'
      },
      facts: { title: 'Immutable enterprise-location facts', count: '{count} records', multiSite: 'Multiple primary sites', empty: 'No enterprise-location changes have been posted.' },
      inspector: { tileSites: 'Regional enterprise sites', poolCapacity: 'Pool {occupied} / {effective}' },
      commandSuccess: 'The enterprise-location fact was posted through a city tick.',
      commandFailed: 'Enterprise-location command failed'
    },
    services: {
      eyebrow: 'City service facts', title: 'Public facilities and city services',
      description: 'Build an auditable service network from real buildings, capacities, subjects, connections, and per-tick settlements.',
      loading: 'Loading public-service projections…', queryFailed: 'Public-service query failed',
      commandSuccess: 'The public-service fact was posted through a city tick.', commandFailed: 'Public-service command failed',
      unsupported: { title: 'This world does not enable the public-service foundation', description: 'Pause and upgrade the world to {version} before creating facilities, demands, and connections. The upgrade creates no synthetic business data.' },
      tabs: { catalog: 'Catalog', facilities: 'Facilities', demands: 'Demands', connections: 'Connections', networks: 'Physical networks', settlements: 'Settlements' },
      metrics: {
        facilities: 'Facilities', operational: '{count} operational', capacity: 'Dispatch capacity', capacityLines: '{count} active capacity lines',
        demand: 'Demand per tick', activeDemands: '{count} active demands', delivered: 'Latest delivered', shortage: 'Latest shortage',
        quality: 'Weighted quality', requested: 'Requested {value}', requestedLabel: 'Requested', noTick: 'No settlements yet'
      },
      catalog: {
        services: 'Service definitions', servicesDescription: 'Units, directions, and semantics are constrained by a versioned catalog.',
        facilityTypes: 'Facility types', facilityTypesDescription: 'A facility type constrains allowed services and minimum building area.',
        category: 'Category', unit: 'Unit', immutable: 'Immutable within version', minimumArea: 'Minimum floor area {value} m²'
      },
      filters: { service: 'Service', status: 'Status', district: 'District', facility: 'Facility', demand: 'Demand', apply: 'Apply filters' },
      columns: {
        facility: 'Facility', location: 'Spatial anchor', status: 'Status', capacities: 'Service capacities', demand: 'Demand', subject: 'Service subject',
        request: 'Request and priority', latestSettlement: 'Latest settlement', connection: 'Connection', route: 'Supply route', flowLimit: 'Flow limit', policy: 'Loss and preference'
      },
      actions: {
        operations: 'Public-service operations', registerFacility: 'Register facility', capacity: 'Configure capacity', transition: 'Transition status',
        configureDemand: 'Configure demand', configureConnection: 'Configure connection', confirm: 'Confirm and post', processing: 'Posting…'
      },
      form: {
        code: 'Stable code', name: 'Facility name', facilityType: 'Facility type', building: 'Building', ownerEntity: 'Owner economic entity (optional)',
        reliability: 'Reliability (milli)', facility: 'Facility', service: 'Service', installedCapacity: 'Installed capacity', availability: 'Availability (milli)',
        effectiveCapacity: 'Computed available capacity', targetStatus: 'Target status', currentStatus: 'Current status', subjectKind: 'Subject kind', subject: 'Real subject',
        requestedUnits: 'Requested units per tick', priority: 'Priority (0–1000)', status: 'Status', version: 'Current version', demand: 'Demand',
        flowLimit: 'Maximum flow per tick', loss: 'Loss (milli)', preference: 'Connection preference (0–1000)',
        registerNote: 'A new facility starts offline. It supplies nothing until capacity is configured and its status is explicitly transitioned to operational.',
        casNote: 'The command carries the current version. The server rejects concurrent overwrites and recalculates available capacity.',
        statusNote: 'A status transition affects only settlements posted after this fact; existing settlements remain immutable.',
        demandNote: 'A demand must bind a real subject in this world. Its subject and service cannot be changed after creation.',
        connectionNote: 'A connection can only join capacity and demand for the same service. Flow, loss, and preference drive deterministic allocation.'
      },
      status: { offline: 'Offline', operational: 'Operational', degraded: 'Degraded', retired: 'Retired', active: 'Active', suspended: 'Suspended' },
      subjectKind: { district: 'District', building: 'Building', household: 'Household cohort', enterprise: 'Enterprise', actor: 'Actor' },
      empty: { facilities: 'No facilities match these filters.', demands: 'No service demands match these filters.', connections: 'No service connections match these filters.', settlements: 'No settlement facts match these filters.' },
      pagination: { loaded: '{count} records loaded', more: 'Load more' },
      reliability: 'Reliability {value}', priority: 'Priority {value}', preference: 'Preference {value}', loss: 'Loss {value}',
      shortage: 'Shortage {value}', lossUnits: 'Loss units {value}', allocations: '{count} allocation lines', noCapacity: 'No service capacity configured', noSettlement: 'No settlement yet',
      network: {
        title: 'City-service physical networks', loading: 'Loading physical-network projections…', queryFailed: 'Physical-network query failed',
        unsupported: {
          title: 'This world does not enable the physical-network protocol',
          description: 'Pause and upgrade the world to {version} before managing nodes, edges, capacity, fault isolation, and segment-level flow facts.'
        },
        tabs: { topology: 'Topology and capacity', flows: 'Path flows', diagnostics: 'Network diagnostics', facts: 'Network facts' },
        metrics: {
          networks: 'Active networks', nodes: '{count} active nodes', edges: 'Active edges',
          edgeExceptions: 'Isolated {isolated} · failed {failed}', capacity: 'Available edge capacity', installed: 'Installed {value}',
          dispatched: 'Latest dispatched', received: 'Network received', lossUnits: 'Network loss {value}', deliveryRatio: 'Network delivery ratio'
        },
        filters: { network: 'Physical network', allNetworks: 'All networks', edgeStatus: 'Edge status', phase: 'Fact phase' },
        actions: { network: 'Configure network', node: 'Configure node', edge: 'Configure edge', transition: 'Transition status' },
        topology: {
          graphLabel: 'Physical-network topology showing nodes, edges, capacities, and operating states', selectedNode: 'Selected node',
          selectedEdge: 'Selected edge', edgeInventory: 'Edge asset inventory'
        },
        diagnostics: {
          summary: '{components} components · {bottlenecks} bottleneck edges',
          selectNetwork: 'Select one physical network to load live topology diagnostics.',
          loading: 'Computing connectivity, islands, edge utilization, and route probes…',
          routeProbe: 'Capacity-constrained route probe',
          routeProbeHint: 'Read-only reuse of the production deterministic router; it writes no flow, fact, or residual capacity.',
          source: 'Source node', sink: 'Sink node', probeUnits: 'Probe units', runProbe: 'Run probe',
          components: 'Weak components', activeAssets: '{nodes} active nodes · {edges} active edges',
          isolatedNodes: 'Isolated nodes', serviceIslands: '{count} service islands',
          bottlenecks: 'Bottleneck edges', saturated: '{count} saturated', latestFlow: 'Utilization sample tick',
          componentAssets: '{nodes} nodes · {edges} edges', island: 'Supply/demand island', edgeLoad: 'Latest edge utilization',
          truncated: '{count} lower-priority edges are not shown.', routeResult: 'Route-probe result',
          reasons: {
            reachable: 'Reachable', no_capacity_path: 'No residual-capacity path',
            source_not_active: 'Source node is inactive', sink_not_active: 'Sink node is inactive'
          }
        },
        columns: {
          role: 'Node role', status: 'Status', binding: 'Business binding', version: 'Fact version', route: 'Route',
          available: 'Available / installed', loss: 'Loss', condition: 'Condition', paths: 'Paths / segments',
          tick: 'Tick', fact: 'Fact', subject: 'Subject', source: 'Source'
        },
        empty: { networks: 'No physical networks match these filters.', nodes: 'This network has no displayable nodes.', flows: 'No path-flow facts match these filters.', facts: 'No network facts match these filters.' },
        pagination: {
          topology: '{networks} networks · {nodes} nodes · {edges} edges',
          networks: 'Load more networks', nodes: 'Load more nodes', edges: 'Load more edges'
        },
        operations: { network: 'Physical-network configuration', node: 'Network-node configuration', edge: 'Network-edge configuration', edgeStatus: 'Network-edge status transition' },
        form: {
          topologyRevision: 'Topology revision', networkNote: 'Each network binds one city service. Every change advances its topology revision and optimistic-lock version.',
          capacityBinding: 'Facility service capacity', demandBinding: 'Service demand', anchorCoordinates: 'Bind authoritative XYZ coordinates',
          nodeNote: 'Supply nodes require a facility capacity and demand nodes require a service demand. Junction, storage, and gateway nodes bind neither.',
          fromNode: 'From node', toNode: 'To node', direction: 'Direction', installedCapacity: 'Installed edge capacity', baseCost: 'Base route cost',
          edgeNote: 'Available capacity is integer-derived from installed capacity and availability. Both directions of a bidirectional edge share one tick capacity.',
          transitionNote: 'Isolation, failure, and recovery post immutable facts. The server revalidates the network and endpoints before activation.'
        },
        direction: { directed: 'Directed', bidirectional: 'Bidirectional shared capacity' },
        role: { supply: 'Supply', demand: 'Demand', junction: 'Junction', storage: 'Storage', gateway: 'Gateway' },
        status: { active: 'Active', suspended: 'Suspended', retired: 'Retired', isolated: 'Isolated', failed: 'Failed', offline: 'Offline' },
        phase: { command: 'Command', pre_network: 'Pre-network', settlement: 'Settlement' },
        factType: {
          'network.configured': 'Network configured', 'node.configured': 'Node configured',
          'edge.configured': 'Edge configured', 'edge.state_changed': 'Edge status transitioned',
          'network.topology_synchronized': 'Topology synchronized', 'network.flow_settled': 'Network flow settled'
        },
        facts: { automatic: 'Automatic phase' }
      },
      serviceNames: {
        electric_power: 'Electric power', potable_water: 'Potable water', wastewater: 'Wastewater treatment', solid_waste: 'Solid waste',
        education: 'Education', healthcare: 'Healthcare', fire_response: 'Fire response', police_response: 'Police response'
      },
      facilityTypeNames: {
        service_hub: 'Service hub', power_plant: 'Power plant', water_works: 'Water works', wastewater_plant: 'Wastewater plant',
        waste_processing: 'Waste processing', school: 'School', hospital: 'Hospital', fire_station: 'Fire station', police_station: 'Police station'
      }
    },
    runtime: {
      eyebrow: 'Open-world runtime', title: 'Characters, progression, and rules',
      description: 'Server-posted facts determine character state, actions, role transitions, rule cases, and sanctions.',
      unavailable: 'This world does not enable the open-world runtime.', actorSelection: 'Select character', unknown: 'Unknown definition',
      processing: 'Posting…', perform: 'Perform', transition: 'Change role',
      attributes: 'Character attributes', activities: 'Available actions', roles: 'Identity and profession', statuses: 'Statuses and sanctions',
      cases: 'Rule cases', rules: 'Public rules', facts: 'Character fact history',
      serverAuthoritative: 'Values are computed by the server', activitiesHint: 'Actions change attributes and may trigger world rules',
      rolesHint: 'Transitions require the necessary attributes, history, and identity',
      statusSummary: '{stacks} stacks · intensity {intensity}', noStatuses: 'This character has no status or sanction records.',
      noCases: 'This character has no rule cases.', commandSuccess: 'The character fact was posted through the city tick.',
      commandQueued: 'The character command is queued for the world scheduler to post in a tick.',
      commandFailed: 'Open-world operation failed',
      lifecycle: {
        queued: 'The world-scheduler command is queued.',
        success: 'World scheduling was updated through a city tick.',
        failed: 'World scheduling operation failed'
      },
      counters: { actors: 'Actors', facts: 'Facts', cases: 'Cases' },
      members: {
        title: 'World members and duties', count: '{count} active members', identity: 'Email or username',
        identityPlaceholder: 'Enter an exact email or username', role: 'World duty', add: 'Add member', remove: 'Remove',
        addSuccess: 'World member added', updateSuccess: 'World member updated', mutationFailed: 'World member operation failed',
        roles: { owner: 'Owner', planner: 'Planner', treasurer: 'Treasurer', trader: 'Trader', viewer: 'Viewer' }
      },
      receipts: {
        title: 'Command processing receipts', awaitingTick: 'Awaiting scheduler tick',
        status: { pending: 'Pending', applied: 'Applied', rejected: 'Rejected' }
      },
      creation: {
        eyebrow: 'Character initialization', title: 'Choose a starter character and enter the world', archetype: 'Starter character',
        capacity: 'Character slots {current} / {maximum}', name: 'Character name',
        namePlaceholder: 'Enter a character name', confirm: 'Create character'
      },
      roleState: {
        active: 'Currently active', eligible: 'Transition requirements satisfied', requirements: 'Requirements not yet satisfied',
        cooldown: 'Wait {count} more ticks'
      },
      location: {
        title: 'Location and movement', showOnMap: 'Locate on map', jurisdiction: 'Jurisdiction', space: 'Space', anchor: 'Spatial anchor',
        noAnchor: 'No anchor', movement: 'Eight-direction movement control', levelUp: 'Level up', levelDown: 'Level down',
        movementHint: 'Planar movement is limited to adjacent Cells. Z transitions require a server-registered bidirectional portal.',
        unavailable: 'This actor has no authoritative location fact yet.',
        direction: {
          northWest: 'Move north-west', north: 'Move north', northEast: 'Move north-east', west: 'Move west',
          east: 'Move east', southWest: 'Move south-west', south: 'Move south', southEast: 'Move south-east'
        }
      },
      navigation: {
        title: 'Deterministic path planning', preview: 'Plan to selected Cell', planning: 'Planning…', clear: 'Clear path',
        noTarget: 'No target selected', hint: 'Select a Cell on the local map. The server computes a reproducible route from terrain, building entrances, stairs, and actor occupancy.',
        status: 'Result', steps: 'Moves', cost: 'Total cost', expanded: 'Expanded', route: 'Planned route',
        reachable: 'Reachable', arrived: 'Already at target', unreachable: 'Unreachable', moveNext: 'Execute next move',
        failed: 'Path planning failed',
        reasons: {
          outside_world: 'Target is outside world bounds', chunk_not_generated: 'A required Chunk has not been generated',
          terrain_blocked: 'Target terrain is impassable', furniture_blocked: 'Target is blocked by furniture',
          building_wall: 'Building boundary cannot be crossed here', void: 'No traversable space exists on this Z level',
          actor_occupied: 'Another actor occupies the target Cell', portal_required: 'A registered entrance or stair is required',
          portal_closed: 'A required entrance is closed', portal_locked: 'A required entrance is locked',
          portal_access_denied: 'The actor does not satisfy a required entrance policy',
          corner_blocked: 'Diagonal corner cutting is not allowed', search_limit: 'Route exceeds the step or node search bound',
          unreachable: 'No route exists in the current spatial topology'
        }
      },
      navigationIntent: {
        title: 'Persistent movement intent', loading: 'Synchronizing movement scheduler state…', reservationCount: '{count} move reservations in the current tick',
        empty: 'This actor has no movement intent. Use a selected map Cell as the destination and let later world ticks advance it.',
        destination: 'Destination XYZ', nextAttempt: 'Next attempt', priority: 'Priority', blockedAttempts: 'Blocked attempts',
        maxSteps: 'Search bound', onBlocked: 'Blocked policy', budget: 'Action budget', useSelectedCell: 'Use selected map Cell',
        create: 'Create movement intent', replace: 'Replace movement intent', cancel: 'Cancel movement intent', stepCost: 'Step cost {cost}',
        statuses: { active: 'Active', blocked: 'Blocked', arrived: 'Arrived', cancelled: 'Cancelled', failed: 'Failed' },
        onBlockedOptions: { retry: 'Retry with deterministic backoff', cancel: 'Cancel on first blockage' },
        reasons: {
          budget_insufficient: 'Action budget is insufficient; accrue it in a later tick', target_reached: 'The actor reached the destination',
          user_cancelled: 'The movement intent was cancelled by the user', target_invalid: 'The destination is no longer valid',
          reservation_target_conflict: 'Another Actor reserved the target Cell in this tick',
          reservation_edge_conflict: 'Another Actor reserved this movement edge in this tick',
          outside_world: 'Target is outside world bounds', chunk_not_generated: 'A required Chunk has not been generated',
          terrain_blocked: 'Target terrain is impassable', furniture_blocked: 'Target is blocked by furniture',
          building_wall: 'Building boundary cannot be crossed here', void: 'No traversable space exists on this Z level',
          actor_occupied: 'Another actor occupies the target Cell', portal_required: 'A registered entrance or stair is required',
          portal_closed: 'A required entrance is closed', portal_locked: 'A required entrance is locked',
          portal_access_denied: 'The actor does not satisfy a required entrance policy', corner_blocked: 'Diagonal corner cutting is not allowed',
          search_limit: 'Route exceeds the step or node search bound', unreachable: 'No route exists in the current spatial topology'
        }
      },
      portals: {
        title: 'Portal state and access control', loading: 'Loading portal projection…', count: '{count} portals', empty: 'This world has no active portals.',
        direction: 'Direction', version: 'Version', policy: 'Access policy', actorAccess: 'Selected actor', fixedOpen: 'Stairs remain open',
        outOfRange: 'Move the actor next to this portal', endpointRequired: 'Move the actor onto this portal endpoint', traverse: 'Traverse portal', editPolicy: 'Edit policy', policyEditor: 'Portal access policy editor',
        policyHint: 'The server validates and posts each policy as a fact. Saving replaces the portal’s complete access requirement.', portal: 'Target portal',
        policyMode: 'Policy condition', noDefinitions: 'No definitions available', minimumStacks: 'Minimum status stacks', factType: 'Fact type',
        windowTicks: 'Window (ticks)', scaledThreshold: 'Attribute threshold', integerThreshold: 'Integer threshold', savePolicy: 'Save access policy',
        requirementFailure: '{condition}: current {actual}, required {required}',
        states: { open: 'Open', closed: 'Closed', locked: 'Locked' },
        actions: { open: 'Open', close: 'Close', lock: 'Lock', unlock: 'Unlock' },
        access: { allowed: 'Pass allowed', denied: 'Policy denied', closed: 'Portal closed', locked: 'Portal locked', notEvaluated: 'No actor selected' },
        policyModes: {
          unchanged: 'Keep current complex policy', public: 'Public access', roleActive: 'Require active role', roleInactive: 'Require inactive role',
          attributeGte: 'Attribute at least threshold', attributeLte: 'Attribute at most threshold', experienceGte: 'Attribute experience at least threshold',
          statusPresent: 'Require status present', statusAbsent: 'Require status absent', factCountGte: 'Fact count in time window', worldTickGte: 'World tick threshold'
        },
        definitionKinds: { attribute: 'Actor attribute', role: 'Identity or profession', status: 'Actor status' },
        requirements: { and: 'AND', or: 'OR', not: 'NOT', public: 'Public access', active: 'active', inactive: 'inactive', absent: 'absent' }
      },
      control: {
        title: 'Actor control delegation', memberUserID: 'City member', memberUserIDPlaceholder: 'Select an active member',
        memberSearchPlaceholder: 'Search username, email, or ID', noEligibleMembers: 'No eligible active members',
        capabilities: 'Delegated capabilities', actorCommand: 'Issue actor commands', manageControl: 'Manage control grants', grant: 'Grant',
        owner: 'Actor owner', delegate: 'Delegated member', readOnly: 'This member has read-only access without actor-command or control-management capability.',
        noDelegations: 'No active visible control grants.',
        revokeCapability: 'Revoke “{capability}” from user {user}'
      }
    },
    landUse: { residential: 'Residential', commercial: 'Commercial', industrial: 'Industrial' },
    portalType: { entrance: 'Entrance', stair: 'Stair' },
    context: {
      chunk: 'Chunk {x}, {y}, {z}', inspect: 'Open local map', generate: 'Generate verified Chunk',
      generating: 'Generating…', generatedSuccess: 'Chunk generated through a city tick and loaded',
      generateFailed: 'Chunk generation failed'
    },
    changes: {
      eyebrow: 'Immutable fact stream', title: 'Spatial change log', count: '{count} records',
      empty: 'This world has no spatial changes yet.'
    },
    createWorld: {
      action: 'New city', title: 'Create city world', name: 'City name',
      namePlaceholder: 'For example: Harbor Test City', timezone: 'IANA time zone',
      timezoneHint: 'Used for city calendar boundaries, for example Asia/Shanghai.', creating: 'Creating…',
      mode: 'World mode', standardMode: 'Standard open world', realtimeMode: 'Shared realtime pixel world',
      standardHint: 'Uses the existing open-world runtime for compatibility testing and established city features.',
      realtimeHint: 'Requires a server-owned, trusted NTP/NTS clock. The server pins the clock, engine, and content versions.',
      style: 'World-generation style', stylePlaceholder: 'Choose a style',
      styleHint: 'Creation freezes the generator, regional style, and glyph rule set; it does not reuse the legacy fixed F7 map.',
      styleLoadFailed: 'Unable to load the open-world style catalog',
      confirm: 'Create open world', success: 'City world created', failed: 'Failed to create city world'
    },
    realtime: {
      world: 'City world', sharedWorld: 'Shared realtime world', worldTime: 'World time',
      zoomOut: 'Zoom out pixel map', zoomIn: 'Zoom in pixel map', refresh: 'Synchronize world projection', returnToSpawn: 'Return to spawn',
      mapTitle: 'PIXEL WORLD / Shared local map', mapSubtitle: '{chunks} cached Chunks · {actors} visible actors · {cursor}',
      viewportAria: 'Shared pixel world map. Drag or use arrow keys to pan, use the wheel to zoom, and select a Cell to inspect world facts.',
      loading: 'Loading shared pixel world', loadingDescription: 'Verifying server spatial hashes and loading visible Chunks…',
      loadingChunks: 'Loading nearby Chunks', loadFailed: 'Unable to load shared realtime world',
      rendererDisabledTitle: 'Shared pixel rendering is disabled',
      rendererDisabledDescription: 'An administrator has temporarily closed this realtime world’s pixel content plane. World data and its timeline remain intact and become available again after re-enabling it.',
      dragHint: 'Drag to inspect the same city; viewport caching never resets world state.', panHint: 'Pan', zoomHint: 'Zoom', spawnHint: 'Return to spawn',
      clock: { live: 'Server live projection', committed: 'Committed timeline' },
      character: {
        eyebrow: 'My character', title: 'Shared-world identity', loading: 'Loading your character state…',
        label: 'Public character name', labelPlaceholder: 'For example: Haru Tanaka', createHint: 'Appears only on the shared map; it never discloses your account, prompt, or Agent configuration.',
        create: 'Create character', creating: 'Creating character…', createFailed: 'Unable to create character', loadFailed: 'Unable to load your character state',
        runtimeUnavailable: 'This historic world is not bound to the character runtime. You can keep viewing it, but cannot create a character in it.',
        selectTarget: 'Select an adjacent visible Cell as the movement target.', selectedTarget: 'Target: {x} / {y} / {z}',
        move: 'Move to target', moving: 'Submitting movement…', moveFailed: 'Unable to move character', adjacentOnly: 'Movement is limited to one adjacent Cell on the same layer.',
        portals: {
          title: 'Portal transition', serverAuthoritative: 'Static route verified by server',
          completed: '{direction} completed', failed: 'Unable to traverse portal',
          directions: { enter: 'Enter building', exit: 'Exit building', ascend: 'Go upstairs', descend: 'Go downstairs' }
        },
        interior: {
          title: 'Interior navigation', floor: 'Floor {floor}', cellDescription: '{kind} at {x} / {y} / {z}',
          kinds: { wall: 'Wall', window: 'Window', floor: 'Floor', door: 'Door', furniture: 'Furnishing' }
        },
        life: {
          title: 'Life state', localOnly: 'Local to this city; not redeemable platform balance',
          energy: 'Energy', satiety: 'Satiety', morale: 'Morale', standing: 'Civic standing',
          cityCredit: 'City credit', rations: 'Rations'
        },
        progression: {
          title: 'Character progression', private: 'Only visible to you · sealed by server', archetypeSelect: 'Starting archetype', archetype: 'Archetype',
          experience: 'Total experience', currentRoles: 'Current roles', available: 'Eligible', active: 'Active role',
          change: 'Change role', changing: 'Changing role…', changeFailed: 'Unable to change role', none: 'None',
          changed: 'Role changed from “{from}” to “{to}”',
          unavailable: {
            civic_standing: 'Civic standing requirement not met', experience: 'Experience requirement not met',
            role: 'Required role not held', attribute: 'Attribute requirement not met', requirements: 'Requirements not met'
          },
          requirements: {
            none: 'No additional requirements', standing: 'Civic standing {value}', experience: 'Experience {value}',
            attribute: '{attribute} {value}', role: 'Requires {role}'
          },
          attributes: { communication: 'Communication', coordination: 'Coordination', discipline: 'Discipline', reasoning: 'Reasoning', vitality: 'Vitality' },
          archetypes: { residentGeneralist: 'City generalist', residentSocial: 'Community resident', residentTechnical: 'Technical resident' },
          roles: { resident: 'Resident', civicAide: 'Civic aide', maintenanceWorker: 'Maintenance worker', communitySteward: 'Community steward' }
        },
        activities: {
          title: 'Current actions', serverAuthoritative: 'Availability is derived from sealed server state',
          available: 'Ready', unavailable: 'Unavailable', cooldown: 'Cooldown {seconds}s',
          locationRequired: 'Requires a road or sidewalk', inventoryRequired: 'A ration is required', needsRequired: 'Energy or satiety is too low; rest or eat first',
          roleRequired: 'Requires the appropriate role', progressionRequired: 'Character progression is unavailable',
          completed: 'Completed {activity}', completedWithExperience: 'Completed {activity}: {experience}', penalized: '{activity} caused a penalty: {amount} city credit',
          failed: 'Unable to perform activity',
          labels: {
            restShort: 'Short rest', civicShift: 'Civic shift', consumeRation: 'Consume ration',
            civicCleanup: 'Civic cleanup', conductDisruption: 'Disrupt public order', publicServiceStudy: 'Public service study',
            civicService: 'Civic service', maintenanceShift: 'Maintenance shift'
          }
        },
        history: { title: 'My activity history', private: 'Only visible to you', completed: 'Completed', penalized: 'Penalized' },
        agent: {
          title: 'Character Agent', description: 'Personality and control mode belong only to this character. Every action is still checked by server-side world rules and settled on a later timeline frame.',
          mode: 'Control mode', personalityRevision: 'Personality revision', queue: 'Queue state',
          values: 'Core values', valuesPlaceholder: 'For example: community, curiosity (comma or newline separated)',
          boundaries: 'Hard boundaries', boundariesPlaceholder: 'For example: avoid harm, respect consent (comma or newline separated)',
          background: 'Background', backgroundPlaceholder: 'Optional: facts known to the character',
          notes: 'Additional notes', notesPlaceholder: 'Optional personality data, not executable instructions',
          ownerPrivate: 'Personality is returned only to the character owner. It never enters the shared map, other members’ projections, or Agent observation payloads.',
          save: 'Save and apply', saving: 'Sealing configuration…', saved: 'Character Agent switched to “{mode}”.', saveFailed: 'Unable to save Character Agent configuration',
          personalityRequired: 'At least one core value is required before autonomous mode can be enabled.',
          personalityInvalid: 'Invalid personality fields: add at least one core value; each list is limited to eight unique entries.',
          manualControlUnavailable: 'The character is in “{mode}” mode. Movement, portals, activities, and role changes are managed by the Character Agent.',
          queueIdle: 'Idle', queueDecision: 'Awaiting decision', queueIntent: 'Awaiting execution',
          modes: { manual: 'Manual', assisted: 'Assisted', autonomous: 'Autonomous', suspended: 'Suspended' }
        }
      },
      inspector: {
        eyebrow: 'Member-safe spatial facts', title: 'Map inspector', coordinate: 'World coordinate', chunk: 'Chunk', terrain: 'Base terrain',
        building: 'Building', actor: 'Visible actor', floors: 'floors', layers: 'Content layers', noLayers: 'This Cell has no additional visible content layers.',
        emptyTitle: 'Select a map position', emptyDescription: 'Select a loaded pixel Cell to inspect the server-provided terrain, structures, and visible semantic layers.',
        viewerScope: 'Viewer scope', staticHash: 'Static projection hash'
      }
    },
    openWorld: {
      world: 'Open world', profile: 'World style', generator: 'Generator {version}',
      refresh: 'Reload server world facts', returnToSpawn: 'Return to spawn', nearestInterior: 'Focus nearest materialized interior',
      materializeSector: 'Generate and seal the sector at the current view', materializeFailed: 'The sector-materialization command was not applied',
      lifecycle: {
        title: 'World scheduler', tick: 'T{tick} · {cadence}', speed: 'Play cadence',
        start: 'Start world', pause: 'Pause world',
        pausedHint: 'This world is paused. Persistent movement and automatic actions continue after an administrator starts it.',
        status: { running: 'Running', paused: 'Paused' }
      },
	  verifyCurrentRegion: 'Verify sealed map facts for the current Region', regionVerified: 'Current Region map facts verified',
	  worldVerified: 'Whole-world state and canonical hash verified', verificationSummary: '{sectors} sector(s) · {chunks} Chunk(s)',
	  verificationFailed: 'Map verification returned a mismatched scope',
      mapTitle: 'OPEN WORLD / Local glyph map',
      mapSubtitle: '{chunks} materialized Chunks · {buildings} buildings with interior facts',
      viewportAria: 'Open-world glyph map. Use arrow keys to move and the wheel to zoom.',
      dragHint: 'Drag the map or use arrow keys to move the view', panHint: 'Pan', zoomHint: 'Zoom',
      loadFailed: 'Failed to load open-world data',
      hovered: 'Hovered coordinate {x} / {y} / {z}',
      inspector: {
        eyebrow: 'Materialized world facts', title: 'Cell inspector', coordinate: 'World coordinate',
        chunk: 'Chunk', terrain: 'Base terrain', stack: 'Content stack', building: 'Building and interior',
        floors: '{count} floors', interiorReady: '{count} visible interior floor fact(s) sealed.',
        interiorUnavailable: 'No readable ground-floor interior fact is available for this building.', openInterior: 'View interior glyph map', focusInterior: 'Focus interior entrance',
        emptyTitle: 'Select a map Cell', emptyDescription: 'Click the map to inspect server-sealed terrain, walls, doors, and building facts.',
        seed: 'World seed', hash: 'Genesis hash'
      },
      interior: {
        title: 'Building interior / glyph map', floor: 'Floor {floor} · Z {z}', layout: 'Layout version',
        openFloor: 'Open floor {floor}', connections: 'Available connections',
        cells: '{count} sealed Cells', loading: 'Reading server-sealed interior facts…',
        loadFailed: 'Failed to load interior facts', verificationFailed: 'Interior content hash does not match the building manifest',
        viewportAria: 'Building interior glyph map. Drag or use arrow keys to move, and the wheel to zoom.',
        dragHint: 'Shows only server-sealed rooms, doors, stairs, and furnishings',
        serverFacts: 'Server-sealed facts', selectCell: 'Select a glyph to inspect the complete Cell stack.'
      }
    },
    help: {
      action: 'View shortcuts', title: 'CLASSIC map controls', pan: 'Pan by one Cell', zoom: 'Scale glyph size',
      depth: 'Change the current Z level', surface: 'Return to surface Z=0', mode: 'Toggle Overmap and local map',
      inspect: 'Open a selected region or inspect a Cell', back: 'Return to Overmap', openHelp: 'Open this help',
      note: 'Map shortcuts are disabled while an input has focus. Every shortcut also has an on-screen control.'
    },
    export: { unavailable: 'Select a loaded Chunk first', success: 'Chunk text exported' }
  },

  worldRuntime: {
    actorTypes: { character: 'Character' },
    attributes: {
      vitality: 'Vitality', reasoning: 'Reasoning', coordination: 'Coordination', communication: 'Communication', discipline: 'Discipline'
    },
    archetypes: {
      residentGeneralist: 'Resident generalist', residentGeneralistDescription: 'A balanced resident with an apprentice profession.',
      urbanApprentice: 'Urban apprentice', urbanApprenticeDescription: 'Strong reasoning and discipline for technical career progression.',
      fieldSurvivor: 'Field survivor', fieldSurvivorDescription: 'High vitality and coordination for demanding action and exploration.'
    },
    roles: {
      resident: 'Resident', residentDescription: 'A foundational identity in the city community.',
      apprentice: 'Apprentice', apprenticeDescription: 'Builds professional capability through study and practice.',
      technician: 'Technician', technicianDescription: 'A technical profession unlocked through reasoning, coordination, and apprenticeship.'
    },
    activities: {
      technicalStudy: 'Technical study', technicalStudyDescription: 'Improves reasoning, discipline, and relevant experience.',
      physicalTraining: 'Physical training', physicalTrainingDescription: 'Improves vitality, coordination, and relevant experience.',
      communityService: 'Community service', communityServiceDescription: 'Improves discipline and communication through public service.',
      disruptiveNoise: 'Create disruptive noise', disruptiveNoiseDescription: 'Reduces discipline and triggers public-order rules.'
    },
    statuses: {
      civicWarning: 'Civic warning', civicWarningDescription: 'A time-limited warning for a public-order violation.',
      communityServiceOrder: 'Community service order', communityServiceOrderDescription: 'A community-service sanction for repeated violations.'
    },
    rules: {
      publicOrderNoise: 'Public order: noise', publicOrderNoiseDescription: 'Escalates disruptive-noise consequences by occurrences in a time window.'
    },
    facts: {
      actor_created: 'Character created', actor_activity_performed: 'Action performed',
      actor_role_transitioned: 'Role transitioned', actor_status_expired: 'Status expired',
      rule_consequence_applied: 'Rule consequence applied', actor_location_moved: 'Actor location moved',
      actor_control_granted: 'Actor control granted', actor_control_revoked: 'Actor control revoked',
      portal_state_changed: 'Portal state changed', portal_access_changed: 'Portal access policy changed'
    }
  },

  // Auth
  auth: {
    welcomeBack: 'Welcome Back',
    signInToAccount: 'Sign in to your account to continue',
    signIn: 'Sign In',
    signingIn: 'Signing in...',
    createAccount: 'Create Account',
    signUpToStart: 'Sign up to start using {siteName}',
    signUp: 'Sign up',
    processing: 'Processing...',
    continue: 'Continue',
    rememberMe: 'Remember me',
    dontHaveAccount: "Don't have an account?",
    alreadyHaveAccount: 'Already have an account?',
    registrationDisabled: 'Registration is currently disabled. Please contact the administrator.',
    emailLabel: 'Email',
    emailPlaceholder: 'Enter your email',
    passwordLabel: 'Password',
    passwordPlaceholder: 'Enter your password',
    createPasswordPlaceholder: 'Create a strong password',
    passwordHint: 'At least 6 characters',
    emailRequired: 'Email is required',
    invalidEmail: 'Please enter a valid email address',
    passwordRequired: 'Password is required',
    passwordMinLength: 'Password must be at least 6 characters',
    loginFailed: 'Login failed. Please check your credentials and try again.',
    errors: {
      USER_NOT_ACTIVE: 'Account has been disabled.',
    },
    registrationFailed: 'Registration failed. Please try again.',
    emailSuffixNotAllowed: 'This email domain is not allowed for registration.',
    emailSuffixNotAllowedWithAllowed:
      'This email domain is not allowed. Allowed domains: {suffixes}',
    emailSuffixAllowedMore: 'and {count} more',
    loginSuccess: 'Login successful! Welcome back.',
    accountCreatedSuccess: 'Account created successfully! Welcome to {siteName}.',
    reloginRequired: 'Session expired. Please log in again.',
    turnstileExpired: 'Verification expired, please try again',
    turnstileFailed: 'Verification failed, please try again',
    completeVerification: 'Please complete the verification',
    verifyYourEmail: 'Verify Your Email',
    sessionExpired: 'Session expired',
    sessionExpiredDesc: 'Please go back to the registration page and start again.',
    verificationCode: 'Verification Code',
    verificationCodeHint: 'Enter the 6-digit code sent to your email',
    sendingCode: 'Sending...',
    sendCode: 'Send code',
    clickToResend: 'Click to resend code',
    resendCode: 'Resend verification code',
    sendCodeDesc: "We'll send a verification code to",
    codeSentSuccess: 'Verification code sent! Please check your inbox.',
    verifying: 'Verifying...',
    verifyAndCreate: 'Verify & Create Account',
    resendCountdown: 'Resend code in {countdown}s',
    backToRegistration: 'Back to registration',
    sendCodeFailed: 'Failed to send verification code. Please try again.',
    verifyFailed: 'Verification failed. Please try again.',
    codeRequired: 'Verification code is required',
    invalidCode: 'Please enter a valid 6-digit code',
    promoCodeLabel: 'Promo Code',
    promoCodePlaceholder: 'Enter promo code (optional)',
    promoCodeValid: 'Valid! You will receive ${amount} bonus balance',
    promoCodeInvalid: 'Invalid promo code',
    promoCodeNotFound: 'Promo code not found',
    promoCodeExpired: 'This promo code has expired',
    promoCodeDisabled: 'This promo code is disabled',
    promoCodeMaxUsed: 'This promo code has reached its usage limit',
    promoCodeAlreadyUsed: 'You have already used this promo code',
    promoCodeValidating: 'Promo code is being validated, please wait',
    promoCodeInvalidCannotRegister: 'Invalid promo code. Please check and try again or clear the promo code field',
    invitationCodeLabel: 'Invitation Code',
    invitationCodePlaceholder: 'Enter invitation code',
    invitationCodeRequired: 'Invitation code is required',
    invitationCodeValid: 'Invitation code is valid',
    invitationCodeInvalid: 'Invalid or used invitation code',
    invitationCodeValidating: 'Validating invitation code...',
    invitationCodeInvalidCannotRegister: 'Invalid invitation code. Please check and try again',
    oauthOrContinue: 'or continue with others',
    linuxdo: {
      signIn: 'Continue with Linux.do',
      orContinue: 'or continue with email',
      callbackTitle: 'Signing you in',
      callbackProcessing: 'Completing login, please wait...',
      callbackHint: 'If you are not redirected automatically, go back to the login page and try again.',
      callbackMissingToken: 'Missing login token, please try again.',
      backToLogin: 'Back to Login',
      invitationRequired: 'This Linux.do account is not yet registered. The site requires an invitation code — please enter one to complete registration.',
      invalidPendingToken: 'The registration token has expired. Please sign in with Linux.do again.',
      completeRegistration: 'Complete Registration',
      completing: 'Completing registration…',
      completeRegistrationFailed: 'Registration failed. Please check your invitation code and try again.'
    },
    dingtalk: {
      signIn: 'Continue with DingTalk',
      callbackTitle: 'Signing you in with DingTalk',
      callbackProcessing: 'Completing DingTalk login, please wait...',
      callbackHint: 'If you are not redirected automatically, go back to the login page and try again.',
      callbackMissingToken: 'Missing login token, please try again.',
      backToLogin: 'Back to Login',
      invitationRequired: 'This DingTalk account is not yet registered. The site requires an invitation code — please enter one to complete registration.',
      invalidPendingToken: 'The registration token has expired. Please sign in with DingTalk again.',
      completeRegistration: 'Complete Registration',
      completing: 'Completing registration…',
      completeRegistrationFailed: 'Registration failed. Please check your invitation code and try again.',
      createAccountTitle: 'Create DingTalk Account',
      registrationDisabledRedirectToBind: 'New account registration is currently disabled. Please bind to your existing account with its email and password.',
      error: {
        title: 'DingTalk Sign-in Failed',
        csrf: 'Login session expired, please scan again',
        corp_rejected: 'Your DingTalk account is not part of this organization. Please contact administrator',
        dingtalk_not_enabled: 'DingTalk login is not enabled',
        upstream_error: 'DingTalk service is temporarily unavailable. Please try again later',
        missing_browser_session: 'Browser session lost. Please login again',
        missing_params: 'Request parameters are incomplete',
        invalid_state: 'Invalid login state',
        provider_error: 'DingTalk authorization failed',
        session_error: 'Failed to create session. Please retry',
        retry: 'Retry Login'
      }
    },
    emailOAuth: {
      signIn: 'Continue with {providerName}'
    },
    oidc: {
      signIn: 'Continue with {providerName}',
      callbackTitle: 'Signing you in with {providerName}',
      callbackProcessing: 'Completing login with {providerName}, please wait...',
      callbackHint: 'If you are not redirected automatically, go back to the login page and try again.',
      callbackMissingToken: 'Missing login token, please try again.',
      backToLogin: 'Back to Login',
      invitationRequired:
        'This {providerName} account is not yet registered. The site requires an invitation code — please enter one to complete registration.',
      invalidPendingToken: 'The registration token has expired. Please sign in again.',
      completeRegistration: 'Complete Registration',
      completing: 'Completing registration…',
      completeRegistrationFailed: 'Registration failed. Please check your invitation code and try again.'
    },
    oauthFlow: {
      profileDetailsTitle: 'Use {providerName} profile details',
      profileDetailsDescription: 'Choose whether to apply the nickname or avatar from {providerName} to this account.',
      useDisplayName: 'Use display name',
      useAvatar: 'Use avatar',
      avatarAlt: '{providerName} avatar',
      reviewProfileBeforeContinue: 'Review the {providerName} profile details before continuing.',
      chooseHowToContinue: 'Choose how to continue',
      chooseAccountActionHint: 'Choose whether to bind an existing account or create a new one.',
      suggestedEmail: 'Suggested email: {email}',
      bindExistingAccount: 'Bind existing account',
      createNewAccount: 'Create new account',
      createAccountHint: 'Enter an email address to create your account and continue.',
      bindLoginHint: 'Log in to an existing account to bind this {providerName} sign-in.',
      signInThenBindDescription: 'Sign in to an existing account, then bind this {providerName} sign-in to it.',
      bindSignInToExistingAccount: 'Bind this {providerName} sign-in to an existing account.',
      bindCurrentAccountTitle: 'Bind the current account',
      bindCurrentAccountDescription: 'Bind this {providerName} sign-in to the account currently signed in on this browser.',
      bindCurrentAccount: 'Bind current account',
      logInAndBind: 'Log in and bind',
      useDifferentEmail: 'Use a different email',
      backToOptions: 'Back to options',
      yourAccount: 'your account',
      totpHint: 'Enter the 6-digit verification code for {account} to finish binding this {providerName} sign-in.',
      verifyAndContinue: 'Verify and continue',
      wechatAvailabilityUnknown: 'WeChat sign-in availability could not be confirmed. Refresh and retry.',
      wechatSystemBrowserOnly: 'This WeChat sign-in flow is only available in your system browser.',
      wechatBrowserOnly: 'This WeChat sign-in flow is only available inside the WeChat browser.',
      wechatNotConfigured: 'WeChat sign-in is not configured yet.'
    },
    linuxdoCallbackPageTitle: 'LinuxDo Sign-In Callback',
    dingtalkCallbackPageTitle: 'DingTalk Sign-In Callback',
    dingtalkProviderName: 'DingTalk',
    oidcCallbackPageTitle: 'OIDC Sign-In Callback',
    oauthCallbackPageTitle: 'OAuth Callback',
    wechatProviderName: 'WeChat',
    wechatCallbackPageTitle: 'WeChat Sign-In Callback',
    wechatPaymentCallbackPageTitle: 'WeChat Payment Callback',
    wechatPayment: {
      callbackTitle: 'Resuming WeChat payment',
      callbackProcessing: 'Resuming WeChat payment...',
      backToPayment: 'Back to payment',
      callbackMissingResumeToken: 'The WeChat payment callback is missing the resume token.'
    },
    oauth: {
      callbackTitle: 'OAuth Callback',
      callbackHint: 'Copy the code and state back to the admin authorization flow when needed.',
      invalidCallbackTitle: 'Invalid sign-in callback',
      invalidCallbackHint: 'This page does not contain a valid authorization result. Return to the login page and start quick sign-in again.',
      code: 'Code',
      state: 'State',
      fullUrl: 'Full URL'
    },
    // Forgot password
    forgotPassword: 'Forgot password?',
    forgotPasswordTitle: 'Reset Your Password',
    forgotPasswordHint: 'Enter your email address and we will send you a link to reset your password.',
    sendResetLink: 'Send Reset Link',
    sendingResetLink: 'Sending...',
    sendResetLinkFailed: 'Failed to send reset link. Please try again.',
    resetEmailSent: 'Reset Link Sent',
    resetEmailSentHint: 'If an account exists with this email, you will receive a password reset link shortly. Please check your inbox and spam folder.',
    backToLogin: 'Back to Login',
    rememberedPassword: 'Remembered your password?',
    // Reset password
    resetPasswordTitle: 'Set New Password',
    resetPasswordHint: 'Enter your new password below.',
    newPassword: 'New Password',
    newPasswordPlaceholder: 'Enter your new password',
    confirmPassword: 'Confirm Password',
    confirmPasswordPlaceholder: 'Confirm your new password',
    confirmPasswordRequired: 'Please confirm your password',
    passwordsDoNotMatch: 'Passwords do not match',
    resetPassword: 'Reset Password',
    resettingPassword: 'Resetting...',
    resetPasswordFailed: 'Failed to reset password. Please try again.',
    passwordResetSuccess: 'Password Reset Successful',
    passwordResetSuccessHint: 'Your password has been reset. You can now sign in with your new password.',
    invalidResetLink: 'Invalid Reset Link',
    invalidResetLinkHint: 'This password reset link is invalid or has expired. Please request a new one.',
    requestNewResetLink: 'Request New Reset Link',
    invalidOrExpiredToken: 'The password reset link is invalid or has expired. Please request a new one.'
  },

  // Step-up (sudo) 2FA prompt
  stepUp: {
    title: 'Two-Factor Verification Required',
    hint: 'Enter the 6-digit code from your authenticator app to continue this sensitive operation.',
    verifyFailed: 'Verification failed, please try again',
    notEnabled: 'This operation requires two-factor authentication. Please enable TOTP in your profile first.',
    adminApiKeyForbidden: 'Admin API keys cannot perform this operation. Use a two-factor verified admin session.'
  },

  // Dashboard
}
